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
	"testing"

	"github.com/crossplane/crossplane-runtime/pkg/test"
	"github.com/kubevela/pkg/util/singleton"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"
)

func TestLoad(t *testing.T) {
	cli := &test.MockClient{
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
)
