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

package stepsecret

import (
	"context"
	"testing"

	"github.com/crossplane/crossplane-runtime/v2/pkg/test"
	"github.com/stretchr/testify/require"
	authorizationv1 "k8s.io/api/authorization/v1"
	"k8s.io/apiserver/pkg/authentication/user"
	"k8s.io/apiserver/pkg/endpoints/request"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestKey(t *testing.T) {
	ctx := context.Background()
	for _, c := range []struct {
		name, ref, namespace string
		want                 client.ObjectKey
	}{
		{"a name, in the step's namespace", "certs", "", client.ObjectKey{Namespace: "team-a", Name: "certs"}},
		{"a name and the step's namespace", "certs", "team-a", client.ObjectKey{Namespace: "team-a", Name: "certs"}},
		{"namespace/name in the step's namespace", "team-a/certs", "", client.ObjectKey{Namespace: "team-a", Name: "certs"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			got, err := Key(ctx, nil, c.ref, c.namespace, "team-a")
			require.NoError(t, err)
			require.Equal(t, c.want, got)
		})
	}

	_, err := Key(ctx, nil, "certs", "", "")
	require.ErrorContains(t, err, "the step has no namespace", "a step with no namespace reads nothing")

	for _, c := range []struct{ name, ref, namespace string }{
		{"another namespace by field", "certs", "team-b"},
		{"another namespace by namespace/name", "team-b/certs", ""},
	} {
		t.Run("refused: "+c.name, func(t *testing.T) {
			_, err := Key(ctx, nil, c.ref, c.namespace, "team-a")
			require.ErrorContains(t, err, "secret team-b/certs is outside the step's namespace team-a")
		})
	}
}

// With a user to ask for, another namespace's Secret is theirs to read or not.
func TestKeyReviewsAnotherNamespaceForTheUser(t *testing.T) {
	asAlice := request.WithUser(context.Background(), &user.DefaultInfo{Name: "alice", Groups: []string{"team-a"}})
	review := func(allowed bool, asked *authorizationv1.SubjectAccessReview) client.Client {
		return &test.MockClient{MockCreate: func(ctx context.Context, obj client.Object, _ ...client.CreateOption) error {
			if u, ok := request.UserFrom(ctx); ok && u != nil {
				t.Fatalf("the review is created as the controller, not as %s", u.GetName())
			}
			sar := obj.(*authorizationv1.SubjectAccessReview)
			*asked = *sar
			sar.Status.Allowed = allowed
			return nil
		}}
	}

	var asked authorizationv1.SubjectAccessReview
	got, err := Key(asAlice, review(true, &asked), "certs", "team-b", "team-a")
	require.NoError(t, err)
	require.Equal(t, client.ObjectKey{Namespace: "team-b", Name: "certs"}, got)
	require.Equal(t, "alice", asked.Spec.User)
	require.Equal(t, []string{"team-a"}, asked.Spec.Groups)
	require.Equal(t, &authorizationv1.ResourceAttributes{Verb: "get", Resource: "secrets", Namespace: "team-b", Name: "certs"}, asked.Spec.ResourceAttributes)

	_, err = Key(asAlice, review(false, &asked), "certs", "team-b", "team-a")
	require.ErrorContains(t, err, "alice may not")
}
