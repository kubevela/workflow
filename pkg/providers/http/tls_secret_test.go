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

package http

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"math/big"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// selfSigned returns a certificate and its key, PEM-encoded.
func selfSigned(t *testing.T) (certPEM, keyPEM []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "test"},
		NotBefore: time.Now(), NotAfter: time.Now().Add(time.Hour), IsCA: true, BasicConstraintsValid: true}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	require.NoError(t, err)
	keyDER, err := x509.MarshalECPrivateKey(key)
	require.NoError(t, err)
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
}

// A TLS Secret may hold its PEM as is, as a kubernetes.io/tls Secret does, or base64-encoded once
// more, as the step has always read it; a CA alone configures the server's trust only, and a
// Secret with none of the keys fails the step.
func TestTLSSecretTakesRawOrEncodedPEMAndACAAlone(t *testing.T) {
	certPEM, keyPEM := selfSigned(t)
	enc := func(b []byte) []byte { return []byte(base64.StdEncoding.EncodeToString(b)) }
	transport := func(data map[string][]byte) (*tls.Config, error) {
		secret := &v1.Secret{Data: data}
		secret.Name, secret.Namespace = "tls", "default"
		cli := fake.NewClientBuilder().WithObjects(secret).Build()
		tr, err := getTransport(context.Background(), cli, client.ObjectKeyFromObject(secret))
		if err != nil {
			return nil, err
		}
		return tr.TLSClientConfig, nil
	}
	caOnly := func(name string, data map[string][]byte) {
		cfg, err := transport(data)
		require.NoError(t, err, name)
		require.NotNil(t, cfg.RootCAs, name)
		require.Empty(t, cfg.Certificates, name)
	}
	keypair := func(name string, data map[string][]byte, withCA bool) {
		cfg, err := transport(data)
		require.NoError(t, err, name)
		require.Len(t, cfg.Certificates, 1, name)
		require.Equal(t, withCA, cfg.RootCAs != nil, name)
	}

	caOnly("a raw CA alone", map[string][]byte{"ca.crt": certPEM})
	caOnly("an encoded CA alone", map[string][]byte{"ca.crt": enc(certPEM)})
	keypair("a raw keypair with a CA", map[string][]byte{"ca.crt": certPEM, "client.crt": certPEM, "client.key": keyPEM}, true)
	keypair("an encoded keypair", map[string][]byte{"client.crt": enc(certPEM), "client.key": enc(keyPEM)}, false)
	_, err := transport(map[string][]byte{"client.crt": certPEM})
	require.Error(t, err, "half a keypair")
	_, err = transport(map[string][]byte{"tls.crt": certPEM, "tls.key": keyPEM})
	require.ErrorContains(t, err, "none of ca.crt, client.crt or client.key", "no key the step reads")
	_, err = transport(nil)
	require.Error(t, err, "an empty Secret")
}
