/*
Copyright 2026 The KubeVela Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

	http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Package servingcert keeps a self-signed CA and a serving cert for an
// in-cluster Service in a Secret, and renews them.
package servingcert

import (
	"bytes"
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"time"

	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/cert"
	"k8s.io/client-go/util/keyutil"
	"k8s.io/client-go/util/retry"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// CAKey is the Secret key holding the PEM CA.
	CAKey = "ca"
	// CAKeyKey is the Secret key holding the PEM private key of the CA.
	CAKeyKey = "ca.key"
	// CertKey is the Secret key holding the PEM serving cert.
	CertKey = "tls.crt"
	// KeyKey is the Secret key holding the PEM private key of the serving cert.
	KeyKey = "tls.key"
	// PreviousCAKey is the Secret key holding the CA that was replaced last.
	PreviousCAKey = "ca.previous"
	// PreviousCAUntilAnnotation is the time until which PreviousCAKey is trusted.
	PreviousCAUntilAnnotation = "servingcert.oam.dev/previous-ca-until"

	validity        = 10 * 365 * 24 * time.Hour
	servingValidity = 365 * 24 * time.Hour
	renewBefore     = 30 * 24 * time.Hour
	caOverlap       = 24 * time.Hour
)

// Service is the Service a serving cert is for.
type Service struct {
	Namespace string
	Name      string
}

// RequiredHosts are the names that clients dial. The old certgen Job used
// these names, so its Secret remains valid after an upgrade.
func (s Service) RequiredHosts() []string {
	return []string{s.Name, s.Name + "." + s.Namespace + ".svc"}
}

// Hosts are the names put in a generated cert.
func (s Service) Hosts() []string {
	return append(s.RequiredHosts(), s.Name+"."+s.Namespace+".svc.cluster.local")
}

// Result is what EnsureSecret returns.
type Result struct {
	Cert, Key []byte
	CABundle  []byte
}

// EnsureSecret creates, renews, or reuses the serving cert Secret.
func EnsureSecret(ctx context.Context, c client.Client, namespace, name string, svc Service) (*Result, error) {
	var result *Result
	isRace := func(err error) bool { return apierrors.IsConflict(err) || apierrors.IsAlreadyExists(err) }
	err := retry.OnError(retry.DefaultRetry, isRace, func() error {
		now := time.Now()
		secret := &corev1.Secret{}
		err := c.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, secret)
		if err != nil && !apierrors.IsNotFound(err) {
			return errors.Wrapf(err, "failed to get secret %s/%s", namespace, name)
		}
		found := err == nil
		if found {
			reason := Check(secret.Data, svc.RequiredHosts(), now)
			if reason == nil {
				reason = Check(secret.Data, svc.RequiredHosts(), now.Add(renewBefore))
			}
			if reason == nil {
				result = resultFor(secret, now)
				return nil
			}
			klog.InfoS("Renewing serving cert", "secret", klog.KObj(secret), "reason", reason.Error())
		} else {
			secret = &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: name}}
		}

		if err = renew(secret, svc.Hosts(), now); err != nil {
			return err
		}
		if found {
			err = c.Update(ctx, secret)
		} else {
			err = c.Create(ctx, secret)
		}
		if err != nil {
			return err
		}
		klog.InfoS("Wrote serving cert secret", "secret", klog.KObj(secret), "created", !found)
		result = resultFor(secret, now)
		return nil
	})
	if err != nil {
		return nil, errors.Wrapf(err, "failed to ensure serving cert secret %s/%s", namespace, name)
	}
	return result, nil
}

func renew(secret *corev1.Secret, hosts []string, now time.Time) error {
	if secret.Data == nil {
		secret.Data = map[string][]byte{}
	}
	oldCA := secret.Data[CAKey]
	data := secret.Data
	if caUsable(data, now) != nil {
		ca, err := generateCA(hosts[0], now.Add(-time.Hour), now.Add(validity))
		if err != nil {
			return err
		}
		data = ca
		if len(certsIn(oldCA, now)) > 0 {
			secret.Data[PreviousCAKey] = oldCA
			setAnnotation(secret, PreviousCAUntilAnnotation, now.Add(caOverlap).Format(time.RFC3339))
		} else {
			delete(secret.Data, PreviousCAKey)
			delete(secret.Annotations, PreviousCAUntilAnnotation)
		}
	}
	issued, err := issue(data, hosts, now.Add(-time.Hour), now.Add(servingValidity))
	if err != nil {
		return err
	}
	for k, v := range issued {
		secret.Data[k] = v
	}
	return nil
}

func setAnnotation(secret *corev1.Secret, key, value string) {
	if secret.Annotations == nil {
		secret.Annotations = map[string]string{}
	}
	secret.Annotations[key] = value
}

func resultFor(secret *corev1.Secret, now time.Time) *Result {
	bundle := append([]byte{}, secret.Data[CAKey]...)
	if until, err := time.Parse(time.RFC3339, secret.Annotations[PreviousCAUntilAnnotation]); err == nil && now.Before(until) {
		if prev := secret.Data[PreviousCAKey]; len(certsIn(prev, now)) > 0 && !bytes.Equal(prev, secret.Data[CAKey]) {
			if len(bundle) > 0 && bundle[len(bundle)-1] != '\n' {
				bundle = append(bundle, '\n')
			}
			bundle = append(bundle, prev...)
		}
	}
	return &Result{Cert: secret.Data[CertKey], Key: secret.Data[KeyKey], CABundle: bundle}
}

// Check verifies a serving key pair against its CA, hosts, and time.
func Check(data map[string][]byte, hosts []string, validUntil time.Time) error {
	if _, err := tls.X509KeyPair(data[CertKey], data[KeyKey]); err != nil {
		return errors.Wrap(err, "invalid key pair")
	}
	roots, err := cert.NewPoolFromBytes(data[CAKey])
	if err != nil {
		return errors.Wrapf(err, "no CA certificate under key %q", CAKey)
	}
	block, _ := pem.Decode(data[CertKey])
	serving, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return errors.Wrap(err, "invalid serving certificate")
	}
	for _, host := range hosts {
		opts := x509.VerifyOptions{
			DNSName:     host,
			Roots:       roots,
			CurrentTime: validUntil,
			KeyUsages:   []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		}
		if _, err = serving.Verify(opts); err != nil {
			return errors.Wrapf(err, "serving certificate not valid for %s at %s", host, validUntil.Format(time.RFC3339))
		}
	}
	return nil
}

func caUsable(data map[string][]byte, now time.Time) error {
	caCert, _, err := parseCA(data)
	if err != nil {
		return err
	}
	if now.Before(caCert.NotBefore) {
		return fmt.Errorf("CA is not valid before %s", caCert.NotBefore.Format(time.RFC3339))
	}
	if now.Add(renewBefore).After(caCert.NotAfter) {
		return fmt.Errorf("CA expires at %s", caCert.NotAfter.Format(time.RFC3339))
	}
	return nil
}

func parseCA(data map[string][]byte) (*x509.Certificate, crypto.Signer, error) {
	pair, err := tls.X509KeyPair(data[CAKey], data[CAKeyKey])
	if err != nil {
		return nil, nil, errors.Wrap(err, "no usable CA key pair")
	}
	caCert, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		return nil, nil, errors.Wrap(err, "invalid CA certificate")
	}
	signer, ok := pair.PrivateKey.(crypto.Signer)
	if !ok || !caCert.IsCA {
		return nil, nil, fmt.Errorf("CA under key %q cannot sign certificates", CAKey)
	}
	return caCert, signer, nil
}

// WriteFileIfChanged writes data with owner-only permissions unless unchanged.
func WriteFileIfChanged(path string, data []byte) (bool, error) {
	if current, err := os.ReadFile(filepath.Clean(path)); err == nil {
		if err := os.Chmod(path, 0o600); err != nil {
			return false, errors.Wrapf(err, "failed to set permissions of %s", path)
		}
		if bytes.Equal(current, data) {
			return false, nil
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return false, errors.Wrapf(err, "failed to create directory for %s", path)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return false, errors.Wrapf(err, "failed to write %s", path)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return false, errors.Wrapf(err, "failed to set permissions of %s", path)
	}
	return true, nil
}

// Covers reports whether every certificate in bundle is also in trusted.
func Covers(trusted, bundle []byte) bool {
	if len(bytes.TrimSpace(bundle)) == 0 {
		return true
	}
	certs, err := cert.ParseCertsPEM(bundle)
	if err != nil {
		return false
	}
	have := map[string]bool{}
	if trustedCerts, err := cert.ParseCertsPEM(trusted); err == nil {
		for _, c := range trustedCerts {
			have[string(c.Raw)] = true
		}
	}
	for _, c := range certs {
		if !have[string(c.Raw)] {
			return false
		}
	}
	return true
}

func certsIn(bundle []byte, now time.Time) []*x509.Certificate {
	certs, err := cert.ParseCertsPEM(bundle)
	if err != nil {
		return nil
	}
	var valid []*x509.Certificate
	for _, c := range certs {
		if now.Before(c.NotAfter) {
			valid = append(valid, c)
		}
	}
	return valid
}

// Generate makes a self-signed CA and a serving cert for hosts.
func Generate(hosts []string, notBefore, notAfter time.Time) (map[string][]byte, error) {
	if len(hosts) == 0 {
		return nil, errors.New("no hosts to put in the serving cert")
	}
	ca, err := generateCA(hosts[0], notBefore, notAfter)
	if err != nil {
		return nil, err
	}
	return issue(ca, hosts, notBefore, notAfter)
}

func generateCA(name string, notBefore, notAfter time.Time) (map[string][]byte, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, errors.Wrap(err, "failed to generate CA key")
	}
	template := &x509.Certificate{
		Subject:               pkix.Name{CommonName: name + "-ca"},
		NotBefore:             notBefore,
		NotAfter:              notAfter,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	if template.SerialNumber, err = randomSerial(); err != nil {
		return nil, err
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create CA certificate")
	}
	keyPEM, err := keyutil.MarshalPrivateKeyToPEM(key)
	if err != nil {
		return nil, errors.Wrap(err, "failed to marshal private key")
	}
	return map[string][]byte{
		CAKey:    pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		CAKeyKey: keyPEM,
	}, nil
}

func issue(ca map[string][]byte, hosts []string, notBefore, notAfter time.Time) (map[string][]byte, error) {
	caCert, caKey, err := parseCA(ca)
	if err != nil {
		return nil, err
	}
	if notAfter.After(caCert.NotAfter) {
		notAfter = caCert.NotAfter
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, errors.Wrap(err, "failed to generate serving key")
	}
	template := &x509.Certificate{
		Subject:     pkix.Name{CommonName: hosts[0]},
		DNSNames:    hosts,
		NotBefore:   notBefore,
		NotAfter:    notAfter,
		KeyUsage:    x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	if template.SerialNumber, err = randomSerial(); err != nil {
		return nil, err
	}
	der, err := x509.CreateCertificate(rand.Reader, template, caCert, &key.PublicKey, caKey)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create serving certificate")
	}
	keyPEM, err := keyutil.MarshalPrivateKeyToPEM(key)
	if err != nil {
		return nil, errors.Wrap(err, "failed to marshal private key")
	}
	return map[string][]byte{
		CAKey:    ca[CAKey],
		CAKeyKey: ca[CAKeyKey],
		CertKey:  pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		KeyKey:   keyPEM,
	}, nil
}

func randomSerial() (*big.Int, error) {
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, errors.Wrap(err, "failed to generate certificate serial number")
	}
	return serial, nil
}
