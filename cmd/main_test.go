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

package main

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/kubevela/workflow/pkg/utils/httpguard"
)

func TestResolveControllerNamespace(t *testing.T) {
	t.Setenv("POD_NAMESPACE", "")
	require.Equal(t, "vela-system", resolveControllerNamespace())

	t.Setenv("POD_NAMESPACE", "  my-ns  ")
	require.Equal(t, "my-ns", resolveControllerNamespace())

	t.Setenv("POD_NAMESPACE", "   ")
	require.Equal(t, "vela-system", resolveControllerNamespace())
}

func TestResolveControllerNamespace_unset(t *testing.T) {
	require.NoError(t, os.Unsetenv("POD_NAMESPACE"))
	require.Equal(t, "vela-system", resolveControllerNamespace())
}

func TestConfigureWorkflowHTTPDeny_emptyConfigMap(t *testing.T) {
	t.Cleanup(func() {
		httpguard.SetDenyFragment(httpguard.Policy{ExactHosts: map[string]struct{}{}})
		httpguard.SetEnhancer(nil)
	})
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	cli := fake.NewClientBuilder().WithScheme(scheme).Build()

	require.NoError(t, configureWorkflowHTTPDeny(context.Background(), cli, nil, "", "vela-system", false))
	require.True(t, httpguard.Current().BlockLinkLocal)
}

func TestConfigureWorkflowHTTPDeny_blockPrivateEnhancer(t *testing.T) {
	t.Cleanup(func() {
		httpguard.SetDenyFragment(httpguard.Policy{ExactHosts: map[string]struct{}{}})
		httpguard.SetEnhancer(nil)
	})
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	cli := fake.NewClientBuilder().WithScheme(scheme).Build()

	require.NoError(t, configureWorkflowHTTPDeny(context.Background(), cli, nil, "", "vela-system", true))
	require.True(t, httpguard.Current().BlockPrivate)
}

func TestConfigureWorkflowHTTPDeny_loadsConfigMap(t *testing.T) {
	t.Cleanup(func() {
		httpguard.SetDenyFragment(httpguard.Policy{ExactHosts: map[string]struct{}{}})
		httpguard.SetEnhancer(nil)
	})
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "workflow-http-deny", Namespace: "vela-system"},
		Data:       map[string]string{httpguard.ConfigMapKeyDenyHosts: "denied.example"},
	}
	cli := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cm).Build()

	require.NoError(t, httpguard.LoadConfigMap(context.Background(), cli, "workflow-http-deny", "vela-system"))
	require.Error(t, httpguard.Current().BlockedHost("denied.example"))
}

func TestConfigureWorkflowHTTPDeny_missingConfigMap(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	cli := fake.NewClientBuilder().WithScheme(scheme).Build()

	err := configureWorkflowHTTPDeny(context.Background(), cli, nil, "missing", "vela-system", false)
	require.Error(t, err)
	require.Contains(t, err.Error(), "initialize workflow HTTP deny ConfigMap")
}
