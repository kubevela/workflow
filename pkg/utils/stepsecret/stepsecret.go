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

// Package stepsecret resolves the Secrets a workflow step names, which it reads as the controller.
package stepsecret

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"fmt"
	"strings"

	authorizationv1 "k8s.io/api/authorization/v1"
	"k8s.io/apiserver/pkg/endpoints/request"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// RefKey parses a reference to a Secret: ref is a name or namespace/name, and namespace, when
// set, names its namespace. It checks nothing; a step's reference goes through Key.
func RefKey(ref, namespace string) client.ObjectKey {
	if ns, name, found := strings.Cut(ref, "/"); found {
		return client.ObjectKey{Namespace: ns, Name: name}
	}
	return client.ObjectKey{Namespace: namespace, Name: ref}
}

// Key resolves a step's reference to a Secret, as RefKey parses it, defaulting to stepNamespace.
// A step reads Secrets as the controller, so one outside stepNamespace is allowed only when the
// step runs for a user (the Application's, with authentication on) who may get it there;
// otherwise the reference is refused, as it is when the step has no namespace.
func Key(ctx context.Context, cli client.Client, ref, namespace, stepNamespace string) (client.ObjectKey, error) {
	key := RefKey(ref, namespace)
	if stepNamespace == "" {
		return key, fmt.Errorf("secret %q: the step has no namespace to read it from", ref)
	}
	if key.Namespace == "" {
		key.Namespace = stepNamespace
	}
	if key.Namespace == stepNamespace {
		return key, nil
	}
	reason := fmt.Sprintf("secret %s is outside the step's namespace %s", key, stepNamespace)
	u, ok := request.UserFrom(ctx)
	if !ok || u == nil || u.GetName() == "" {
		return key, fmt.Errorf("%s, and the step runs with no user to ask whether it may read it", reason)
	}
	extra := map[string]authorizationv1.ExtraValue{}
	for k, v := range u.GetExtra() {
		extra[k] = v
	}
	review := &authorizationv1.SubjectAccessReview{Spec: authorizationv1.SubjectAccessReviewSpec{
		User: u.GetName(), UID: u.GetUID(), Groups: u.GetGroups(), Extra: extra,
		ResourceAttributes: &authorizationv1.ResourceAttributes{Verb: "get", Resource: "secrets", Namespace: key.Namespace, Name: key.Name},
	}}
	// Asked as the controller: the user may not be allowed to create reviews themselves.
	if err := cli.Create(request.WithUser(ctx, nil), review); err != nil {
		return key, fmt.Errorf("%s: reviewing access for %s: %w", reason, u.GetName(), err)
	}
	if !review.Status.Allowed {
		return key, fmt.Errorf("%s, and %s may not get it", reason, u.GetName())
	}
	return key, nil
}

// ApplyTLS configures cfg from a step's TLS Secret data: ca.crt for the server's trust, and
// client.crt with client.key for a client certificate. A value is the PEM itself, as a
// kubernetes.io/tls Secret holds it, or the PEM base64-encoded once more. A Secret holding
// none of the three is an error.
func ApplyTLS(cfg *tls.Config, data map[string][]byte) error {
	ca, hasCA := data["ca.crt"]
	cert, hasCert := data["client.crt"]
	key, hasKey := data["client.key"]
	if !hasCA && !hasCert && !hasKey {
		return fmt.Errorf("the TLS Secret holds none of ca.crt, client.crt or client.key")
	}
	if hasCA {
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pemOf(ca)) {
			return fmt.Errorf("ca.crt holds no PEM certificate")
		}
		cfg.RootCAs = pool
	}
	if !hasCert && !hasKey {
		return nil
	}
	if hasCert != hasKey {
		return fmt.Errorf("client.crt and client.key must be set together")
	}
	pair, err := tls.X509KeyPair(pemOf(cert), pemOf(key))
	if err != nil {
		return fmt.Errorf("parse client keypair: %w", err)
	}
	cfg.Certificates = []tls.Certificate{pair}
	return nil
}

// pemOf is v as PEM: v itself, or v base64-decoded when it is not PEM already.
func pemOf(v []byte) []byte {
	if bytes.Contains(v, []byte("-----BEGIN")) {
		return v
	}
	if decoded, err := base64.StdEncoding.DecodeString(string(v)); err == nil {
		return decoded
	}
	return v
}
