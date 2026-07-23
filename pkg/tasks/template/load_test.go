/*
Copyright 2022 The KubeVela Authors.

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

package template

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/kubevela/pkg/util/singleton"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"

	"github.com/kubevela/workflow/pkg/mock"
)

func TestLoad(t *testing.T) {
	cli := &mock.Client{
		MockGet: func(ctx context.Context, key client.ObjectKey, obj client.Object) error {
			o, ok := obj.(*unstructured.Unstructured)
			if !ok {
				return nil
			}
			var d map[string]interface{}
			js, err := yaml.YAMLToJSON([]byte(stepDefYaml))
			if err != nil {
				return err
			}
			if err := json.Unmarshal(js, &d); err != nil {
				return err
			}
			o.Object = d
			return nil
		},
	}
	singleton.KubeClient.Set(cli)
	loader := NewWorkflowStepTemplateLoader()

	r := require.New(t)
	tmpl, err := loader.LoadTemplate(context.Background(), "builtin-apply-component")
	r.NoError(err)
	expected, err := os.ReadFile("./static/builtin-apply-component.cue")
	r.NoError(err)
	r.Equal(tmpl, string(expected))

	tmpl, err = loader.LoadTemplate(context.Background(), "apply-oam-component")
	r.NoError(err)
	r.Equal(tmpl, `import (
	"vela/op"
)

// apply components and traits
apply: op.#ApplyComponent & {
	component: parameter.component
}
parameter: {
	// +usage=Declare the name of the component
	component: string
}`)
}

// TestLoadUpgradesLegacyCUESyntax verifies that LoadTemplate applies CUE version
// compatibility upgrades to templates fetched from the cluster, so legacy syntax
// (e.g. list1 + list2) is rewritten to canonical form (list.Concat) before the
// template reaches the workflow engine.
func TestLoadUpgradesLegacyCUESyntax(t *testing.T) {
	cli := &mock.Client{
		MockGet: func(ctx context.Context, key client.ObjectKey, obj client.Object) error {
			o, ok := obj.(*unstructured.Unstructured)
			if !ok {
				return nil
			}
			var d map[string]interface{}
			js, err := yaml.YAMLToJSON([]byte(legacyStepDefYaml))
			if err != nil {
				return err
			}
			if err := json.Unmarshal(js, &d); err != nil {
				return err
			}
			o.Object = d
			return nil
		},
	}
	singleton.KubeClient.Set(cli)
	loader := NewWorkflowStepTemplateLoader()

	r := require.New(t)
	tmpl, err := loader.LoadTemplate(context.Background(), "legacy-step")
	r.NoError(err)
	// Legacy list1 + list2 must be rewritten to list.Concat([list1, list2]).
	r.True(strings.Contains(tmpl, "list.Concat"), "expected upgraded template to contain list.Concat, got: %s", tmpl)
	r.False(strings.Contains(tmpl, "list1 + list2"), "expected legacy list arithmetic to be rewritten, got: %s", tmpl)
}

var (
	stepDefYaml = `apiVersion: core.oam.dev/v1beta1
kind: WorkflowStepDefinition
metadata:
  annotations:
    definition.oam.dev/description: Apply components and traits for your workflow steps
  name: apply-oam-component
  namespace: vela-system
spec:
  schematic:
    cue:
      template: |
        import (
        	"vela/op"
        )

        // apply components and traits
        apply: op.#ApplyComponent & {
        	component: parameter.component
        }
        parameter: {
        	// +usage=Declare the name of the component
        	component: string
        }`

	// legacyStepDefYaml simulates a WorkflowStepDefinition stored in the cluster
	// that was authored against old CUE syntax (list concatenation via +).
	legacyStepDefYaml = `apiVersion: core.oam.dev/v1beta1
kind: WorkflowStepDefinition
metadata:
  name: legacy-step
  namespace: vela-system
spec:
  schematic:
    cue:
      template: |
        list1: [1, 2, 3]
        list2: [4, 5, 6]
        combined: list1 + list2`
)
