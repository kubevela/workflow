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

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
	"github.com/crossplane/crossplane-runtime/pkg/test"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kubevela/pkg/util/singleton"
	"github.com/kubevela/workflow/api/v1alpha1"
	wfContext "github.com/kubevela/workflow/pkg/context"
)

func TestInput(t *testing.T) {
	wfCtx := mockContext(t)
	r := require.New(t)
	cuectx := cuecontext.New()
	paramValue := cuectx.CompileString(`"name": "foo"`)
	err := wfCtx.SetVar(cuectx.CompileString(`score: 99`), "foo")
	r.NoError(err)
	val, err := Input(wfCtx, paramValue, v1alpha1.WorkflowStep{
		WorkflowStepBase: v1alpha1.WorkflowStepBase{
			DependsOn: []string{"mystep"},
			Inputs: v1alpha1.StepInputs{{
				From:         "foo.score",
				ParameterKey: "myscore",
			}},
		},
	})
	r.NoError(err)
	result := val.LookupPath(cue.ParsePath("parameter.myscore"))
	resultInt, err := result.Int64()
	r.NoError(err)
	r.Equal(int(resultInt), 99)

	// test set value
	paramValue = cuectx.CompileString(`parameter: {myscore: "test"}`)
	r.NoError(err)
	val, err = Input(wfCtx, paramValue, v1alpha1.WorkflowStep{
		WorkflowStepBase: v1alpha1.WorkflowStepBase{
			DependsOn: []string{"mystep"},
			Inputs: v1alpha1.StepInputs{{
				From:         "foo.score",
				ParameterKey: "myscore",
			}},
		},
	})
	r.NoError(err)
	result = val.LookupPath(cue.ParsePath("parameter.myscore"))
	resultInt, err = result.Int64()
	r.NoError(err)
	r.Equal(int(resultInt), 99)
	paramValue = cuectx.CompileString(`context: {name: "test"}`)
	val, err = Input(wfCtx, paramValue, v1alpha1.WorkflowStep{
		WorkflowStepBase: v1alpha1.WorkflowStepBase{
			Inputs: v1alpha1.StepInputs{{
				From:         "context.name",
				ParameterKey: "contextname",
			}},
		},
	})
	r.NoError(err)
	result = val.LookupPath(cue.ParsePath("parameter.contextname"))
	r.NoError(err)
	s, err := result.String()
	r.NoError(err)
	r.Equal(s, "test")
}

func TestOutput(t *testing.T) {
	wfCtx := mockContext(t)
	r := require.New(t)
	cuectx := cuecontext.New()
	taskValue := cuectx.CompileString(`output: score: 99`)
	stepStatus := make(map[string]v1alpha1.StepStatus)
	err := Output(wfCtx, taskValue, v1alpha1.WorkflowStep{
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
	resultInt, err := result.Int64()
	r.NoError(err)
	r.Equal(int(resultInt), 99)
	r.Equal(stepStatus["mystep"].Phase, v1alpha1.WorkflowStepPhaseSucceeded)

	taskValue = cuectx.CompileString(`output: $returns: score: 99`)
	stepStatus = make(map[string]v1alpha1.StepStatus)
	err = Output(wfCtx, taskValue, v1alpha1.WorkflowStep{
		WorkflowStepBase: v1alpha1.WorkflowStepBase{
			Properties: &runtime.RawExtension{
				Raw: []byte("{\"name\":\"mystep\"}"),
			},
			Outputs: v1alpha1.StepOutputs{{
				ValueFrom: "output.$returns.score",
				Name:      "myscore2",
			}},
		},
	}, v1alpha1.StepStatus{
		Phase: v1alpha1.WorkflowStepPhaseSucceeded,
	}, stepStatus)
	r.NoError(err)
	result, err = wfCtx.GetVar("myscore2")
	r.NoError(err)
	resultInt, err = result.Int64()
	r.NoError(err)
	r.Equal(int(resultInt), 99)
	r.Equal(stepStatus["mystep"].Phase, v1alpha1.WorkflowStepPhaseSucceeded)

	taskValue = cuectx.CompileString(`output: $returns: score: 99`)
	stepStatus = make(map[string]v1alpha1.StepStatus)
	err = Output(wfCtx, taskValue, v1alpha1.WorkflowStep{
		WorkflowStepBase: v1alpha1.WorkflowStepBase{
			Properties: &runtime.RawExtension{
				Raw: []byte("{\"name\":\"mystep\"}"),
			},
			Outputs: v1alpha1.StepOutputs{{
				ValueFrom: "output.score",
				Name:      "myscore3",
			}},
		},
	}, v1alpha1.StepStatus{
		Phase: v1alpha1.WorkflowStepPhaseSucceeded,
	}, stepStatus)
	r.NoError(err)
	result, err = wfCtx.GetVar("myscore3")
	r.NoError(err)
	resultInt, err = result.Int64()
	r.NoError(err)
	r.Equal(int(resultInt), 99)
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
	singleton.KubeClient.Set(cli)
	wfCtx, err := wfContext.NewContext(context.Background(), "default", "v1", nil)
	require.NoError(t, err)
	return wfCtx
}
