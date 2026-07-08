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

package httpguard

import (
	"context"
	"net"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestLoadConfigMap(t *testing.T) {
	t.Cleanup(func() {
		SetDenyFragment(Policy{ExactHosts: map[string]struct{}{}})
	})
	scheme := testScheme(t)
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "workflow-http-deny", Namespace: "vela-system"},
		Data: map[string]string{
			ConfigMapKeyDenyCIDRs: "10.0.0.0/8",
			ConfigMapKeyDenyHosts: "metadata.google.internal",
		},
	}
	cli := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cm).Build()
	require.NoError(t, LoadConfigMap(context.Background(), cli, "workflow-http-deny", "vela-system"))
	p := Current()
	require.True(t, p.Blocked(net.ParseIP("10.1.1.1")))
	require.Error(t, p.BlockedHost("metadata.google.internal"))
}

func TestLoadConfigMap_invalid(t *testing.T) {
	scheme := testScheme(t)
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "bad", Namespace: "vela-system"},
		Data: map[string]string{
			ConfigMapKeyDenyCIDRs: "invalid-cidr",
		},
	}
	cli := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cm).Build()
	err := LoadConfigMap(context.Background(), cli, "bad", "vela-system")
	require.Error(t, err)
}

func testScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(s))
	return s
}
