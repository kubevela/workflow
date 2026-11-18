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
	"testing"

	"github.com/crossplane/crossplane-runtime/v2/pkg/test"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kubevela/workflow/pkg/cue/process"
	"github.com/kubevela/workflow/pkg/providers/types"
)

// A step reads its TLS Secret as the controller, so with no user to ask for, only from its own
// namespace; and a TLS config it cannot apply fails the step rather than sending the request
// without it.
func TestHttpDoTLSSecretStaysInTheStepsNamespace(t *testing.T) {
	var read []client.ObjectKey
	missing := &test.MockClient{MockGet: func(_ context.Context, key client.ObjectKey, _ client.Object) error {
		read = append(read, key)
		return apierrors.NewNotFound(schema.GroupResource{Resource: "secrets"}, key.Name)
	}}
	do := func(tls *TLSConfig) error {
		_, err := Do(context.Background(), &DoParams{
			Params: RequestVars{Method: "GET", URL: "https://127.0.0.1:1/x", TLSConfig: tls},
			RuntimeParams: types.RuntimeParams{
				KubeClient:     missing,
				ProcessContext: process.NewContext(process.ContextData{Namespace: "my-ns"}),
			},
		})
		return err
	}
	require.ErrorContains(t, do(&TLSConfig{Secret: "certs", Namespace: "other"}), "secret other/certs is outside the step's namespace my-ns")
	require.ErrorContains(t, do(&TLSConfig{Secret: "other/certs"}), "secret other/certs is outside the step's namespace my-ns")
	require.Empty(t, read, "a Secret outside the step's namespace is never read")
	require.ErrorContains(t, do(&TLSConfig{Secret: "certs"}), "not found", "a missing Secret fails the step")
	require.Equal(t, []client.ObjectKey{{Namespace: "my-ns", Name: "certs"}}, read, "a bare name is read from the step's namespace")
}
