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
	"os"
	"path/filepath"
	"testing"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestWorkflowHTTPDenyConfigTemplate(t *testing.T) {
	t.Run("renders deny ConfigMap", func(t *testing.T) {
		value := loadWorkflowHTTPDenyTemplate(t, map[string]interface{}{
			"denyHosts": []string{
				"metadata.google.internal",
				"*.example.com",
				"169.254.169.254",
				"2001:db8::1",
			},
			"denyCIDRs": []string{
				"10.0.0.0/8",
				"2001:db8::/32",
				"127.0.0.1",
			},
		})
		require.NoError(t, value.Validate(cue.Concrete(true)))

		var metadata struct {
			Name        string `json:"name"`
			Alias       string `json:"alias"`
			Description string `json:"description"`
			Scope       string `json:"scope"`
			Sensitive   bool   `json:"sensitive"`
		}
		require.NoError(t, value.LookupPath(cue.ParsePath("metadata")).Decode(&metadata))
		require.Equal(t, "workflow-http-deny", metadata.Name)
		require.Equal(t, "Workflow HTTP Denylist", metadata.Alias)
		require.NotEmpty(t, metadata.Description)
		require.Equal(t, "system", metadata.Scope)
		require.False(t, metadata.Sensitive)

		var got corev1.ConfigMap
		output := value.LookupPath(cue.ParsePath("template.outputs.configMap"))
		require.NoError(t, output.Decode(&got))
		require.Equal(t, corev1.ConfigMap{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "v1",
				Kind:       "ConfigMap",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "workflow-http-deny",
				Namespace: "vela-system",
			},
			Data: map[string]string{
				ConfigMapKeyDenyHosts: "metadata.google.internal\n*.example.com\n169.254.169.254\n2001:db8::1",
				ConfigMapKeyDenyCIDRs: "10.0.0.0/8\n2001:db8::/32\n127.0.0.1",
			},
		}, got)
	})

	t.Run("renders empty lists", func(t *testing.T) {
		value := loadWorkflowHTTPDenyTemplate(t, map[string]interface{}{
			"denyHosts": []string{},
			"denyCIDRs": []string{},
		})
		require.NoError(t, value.Validate(cue.Concrete(true)))
		denyHosts, err := value.LookupPath(cue.ParsePath("template.outputs.configMap.data.denyHosts")).String()
		require.NoError(t, err)
		require.Empty(t, denyHosts)
		denyCIDRs, err := value.LookupPath(cue.ParsePath("template.outputs.configMap.data.denyCIDRs")).String()
		require.NoError(t, err)
		require.Empty(t, denyCIDRs)
	})

	for _, tc := range []struct {
		name      string
		denyHosts []string
		denyCIDRs []string
	}{
		{
			name:      "wildcard in non-leading position",
			denyHosts: []string{"api.*.example.com"},
			denyCIDRs: []string{},
		},
		{
			name:      "host with scheme",
			denyHosts: []string{"https://example.com"},
			denyCIDRs: []string{},
		},
		{
			name:      "invalid CIDR",
			denyHosts: []string{},
			denyCIDRs: []string{"10.0.0.0/99"},
		},
	} {
		t.Run("rejects "+tc.name, func(t *testing.T) {
			value := loadWorkflowHTTPDenyTemplate(t, map[string]interface{}{
				"denyHosts": tc.denyHosts,
				"denyCIDRs": tc.denyCIDRs,
			})
			require.Error(t, value.Validate(cue.Concrete(true)))
		})
	}
}

func loadWorkflowHTTPDenyTemplate(t *testing.T, parameter map[string]interface{}) cue.Value {
	t.Helper()

	path := filepath.Join("..", "..", "..", "charts", "vela-workflow", "config-templates", "workflow-http-deny.cue")
	source, err := os.ReadFile(path)
	require.NoError(t, err)
	source = append(source, []byte("\ncontext: {name: string, namespace: string}\n")...)

	value := cuecontext.New().CompileBytes(source)
	require.NoError(t, value.Err())
	value = value.FillPath(cue.ParsePath("context"), map[string]string{
		"name":      "workflow-http-deny",
		"namespace": "vela-system",
	})
	require.NoError(t, value.Err())
	value = value.FillPath(cue.ParsePath("template.parameter"), parameter)
	return value
}
