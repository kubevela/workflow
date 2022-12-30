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

package hooks

import (
	"context"
	"testing"

	"github.com/crossplane/crossplane-runtime/pkg/test"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kubevela/workflow/api/v1alpha1"
	wfContext "github.com/kubevela/workflow/pkg/context"
	"github.com/kubevela/workflow/pkg/cue/model/value"
)

func TestInput(t *testing.T) {
	wfCtx := mockContext(t)
	r := require.New(t)
	paramValue, err := wfCtx.MakeParameter(`"name": "foo"`)
	r.NoError(err)
	score, err := paramValue.MakeValue(`score: 99`)
	r.NoError(err)
	err = wfCtx.SetVar(score, "foo")
	r.NoError(err)
	err = Input(wfCtx, paramValue, v1alpha1.WorkflowStep{
		WorkflowStepBase: v1alpha1.WorkflowStepBase{
			DependsOn: []string{"mystep"},
			Inputs: v1alpha1.StepInputs{{
				From:         "foo.score",
				ParameterKey: "myscore",
			}},
		},
	})
	r.NoError(err)
	result, err := paramValue.LookupValue("parameter", "myscore")
	r.NoError(err)
	s, err := result.String()
	r.NoError(err)
	r.Equal(s, `99
`)
	// test set value
	paramValue, err = wfCtx.MakeParameter(`parameter: {myscore: "test"}`)
	r.NoError(err)
	err = Input(wfCtx, paramValue, v1alpha1.WorkflowStep{
		WorkflowStepBase: v1alpha1.WorkflowStepBase{
			DependsOn: []string{"mystep"},
			Inputs: v1alpha1.StepInputs{{
				From:         "foo.score",
				ParameterKey: "myscore",
			}},
		},
	})
	r.NoError(err)
	result, err = paramValue.LookupValue("parameter", "myscore")
	r.NoError(err)
	s, err = result.String()
	r.NoError(err)
	r.Equal(s, `99
`)
	paramValue, err = wfCtx.MakeParameter(`context: {name: "test"}`)
	r.NoError(err)
	err = Input(wfCtx, paramValue, v1alpha1.WorkflowStep{
		WorkflowStepBase: v1alpha1.WorkflowStepBase{
			Inputs: v1alpha1.StepInputs{{
				From:         "context.name",
				ParameterKey: "contextname",
			}},
		},
	})
	r.NoError(err)
	result, err = paramValue.LookupValue("parameter", "contextname")
	r.NoError(err)
	s, err = result.String()
	r.NoError(err)
	r.Equal(s, `"test"
`)
}

func TestOutput(t *testing.T) {
	wfCtx := mockContext(t)
	r := require.New(t)
	taskValue, err := value.NewValue(`
output: score: 99 
`, nil, "")
	r.NoError(err)
	stepStatus := make(map[string]v1alpha1.StepStatus)
	err = Output(wfCtx, taskValue, v1alpha1.WorkflowStep{
		WorkflowStepBase: v1alpha1.WorkflowStepBase{
			Properties: &runtime.RawExtension{
				Raw: []byte("{\"name\":\"mystep\"}"),
			},
			Outputs: v1alpha1.StepOutputs{{
				ValueFrom: "output.score",
				Name:      "myscore",
			}},
		},
	}, v1alpha1.StepStatus{
		Phase: v1alpha1.WorkflowStepPhaseSucceeded,
	}, stepStatus)
	r.NoError(err)
	result, err := wfCtx.GetVar("myscore")
	r.NoError(err)
	s, err := result.String()
	r.NoError(err)
	r.Equal(s, `99
`)
	r.Equal(stepStatus["mystep"].Phase, v1alpha1.WorkflowStepPhaseSucceeded)
}

func mockContext(t *testing.T) wfContext.Context {
	cli := &test.MockClient{
		MockCreate: func(ctx context.Context, obj client.Object, opts ...client.CreateOption) error {
			return nil
		},
		MockPatch: func(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
			return nil
		},
		MockGet: func(ctx context.Context, key client.ObjectKey, obj client.Object) error {
			return nil
		},
	}
	wfCtx, err := wfContext.NewContext(context.Background(), cli, "default", "v1", nil)
	require.NoError(t, err)
	return wfCtx
}
