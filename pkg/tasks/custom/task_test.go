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

package custom

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
	"github.com/crossplane/crossplane-runtime/pkg/test"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"

	"github.com/kubevela/pkg/cue/cuex"
	cuexruntime "github.com/kubevela/pkg/cue/cuex/runtime"
	monitorContext "github.com/kubevela/pkg/monitor/context"
	pkgruntime "github.com/kubevela/pkg/util/runtime"
	"github.com/kubevela/pkg/util/singleton"

	"github.com/kubevela/workflow/api/v1alpha1"
	wfContext "github.com/kubevela/workflow/pkg/context"
	"github.com/kubevela/workflow/pkg/cue/process"
	"github.com/kubevela/workflow/pkg/providers"
	providertypes "github.com/kubevela/workflow/pkg/providers/types"
	"github.com/kubevela/workflow/pkg/types"
)

func TestTaskLoader(t *testing.T) {
	wfCtx := newWorkflowContextForTest(t)
	r := require.New(t)
	compiler := cuex.NewCompilerWithInternalPackages(
		pkgruntime.Must(cuexruntime.NewInternalPackage("test", "", map[string]cuexruntime.ProviderFn{
			"output": cuexruntime.NativeProviderFn(func(ctx context.Context, v cue.Value) (cue.Value, error) {
				return v.FillPath(cue.ParsePath("myIP.value"), "1.1.1.1"), nil
			}),
			"input": cuexruntime.NativeProviderFn(func(ctx context.Context, v cue.Value) (cue.Value, error) {
				val := v.LookupPath(cue.ParsePath("set.prefixIP"))
				str, err := val.String()
				r.NoError(err)
				r.Equal(str, "1.1.1.1")
				return v, nil
			}),
			"templateError": cuexruntime.NativeProviderFn(func(ctx context.Context, v cue.Value) (cue.Value, error) {
				return v.Context().CompileString("output: xxx"), nil
			}),
			"wait": providertypes.LegacyGenericProviderFn[any, any](func(ctx context.Context, val *providertypes.LegacyParams[any]) (*any, error) {
				val.RuntimeParams.Action.Wait("I am waiting")
				return nil, nil
			}),
			"terminate": providertypes.LegacyGenericProviderFn[any, any](func(ctx context.Context, val *providertypes.LegacyParams[any]) (*any, error) {
				val.RuntimeParams.Action.Terminate("I am terminated")
				return nil, nil
			}),
			"suspend": providertypes.LegacyGenericProviderFn[any, any](func(ctx context.Context, val *providertypes.LegacyParams[any]) (*any, error) {
				val.RuntimeParams.Action.Suspend("I am suspended")
				return nil, nil
			}),
			"resume": providertypes.LegacyGenericProviderFn[any, any](func(ctx context.Context, val *providertypes.LegacyParams[any]) (*any, error) {
				val.RuntimeParams.Action.Resume("I am resumed")
				return nil, nil
			}),
			"executeFailed": providertypes.LegacyGenericProviderFn[any, any](func(ctx context.Context, val *providertypes.LegacyParams[any]) (*any, error) {
				return nil, errors.New("execute error")
			}),
			"ok": providertypes.LegacyGenericProviderFn[any, any](func(ctx context.Context, val *providertypes.LegacyParams[any]) (*any, error) {
				return nil, nil
			}),
		},
		)),
	)

	pCtx := process.NewContext(process.ContextData{
		Name:      "app",
		Namespace: "default",
	})
	tasksLoader := NewTaskLoader(mockLoadTemplate, 0, pCtx, compiler)

	steps := []v1alpha1.WorkflowStep{
		{
			WorkflowStepBase: v1alpha1.WorkflowStepBase{
				Name: "output",
				Type: "output",
				Outputs: v1alpha1.StepOutputs{{
					ValueFrom: "myIP.value",
					Name:      "podIP",
				}},
			},
		},
		{
			WorkflowStepBase: v1alpha1.WorkflowStepBase{
				Name: "input",
				Type: "input",
				Inputs: v1alpha1.StepInputs{{
					From:         "podIP",
					ParameterKey: "set.prefixIP",
				}},
			},
		},
		{
			WorkflowStepBase: v1alpha1.WorkflowStepBase{
				Name: "wait",
				Type: "wait",
			},
		},
		{
			WorkflowStepBase: v1alpha1.WorkflowStepBase{
				Name: "terminate",
				Type: "terminate",
			},
		},
		{
			WorkflowStepBase: v1alpha1.WorkflowStepBase{
				Name: "template",
				Type: "templateError",
			},
		},
		{
			WorkflowStepBase: v1alpha1.WorkflowStepBase{
				Name: "execute",
				Type: "executeFailed",
			},
		},
		{
			WorkflowStepBase: v1alpha1.WorkflowStepBase{
				Name: "ok",
				Type: "ok",
			},
		},
	}

	for _, step := range steps {
		gen, err := tasksLoader.GetTaskGenerator(context.Background(), step.Type)
		r.NoError(err)
		run, err := gen(step, &types.TaskGeneratorOptions{})
		r.NoError(err)
		status, action, err := run.Run(wfCtx, &types.TaskRunOptions{})
		r.NoError(err)
		if step.Name == "wait" {
			r.Equal(status.Phase, v1alpha1.WorkflowStepPhaseRunning)
			r.Equal(status.Reason, types.StatusReasonWait)
			r.Equal(status.Message, "I am waiting")
			continue
		}
		if step.Name == "terminate" {
			r.Equal(action.Terminated, true)
			r.Equal(status.Reason, types.StatusReasonTerminate)
			r.Equal(status.Message, "I am terminated")
			continue
		}
		if step.Name == "template" {
			r.Equal(status.Phase, v1alpha1.WorkflowStepPhaseFailed)
			r.Equal(status.Reason, types.StatusReasonExecute)
			continue
		}
		if step.Name == "execute" {
			r.Equal(status.Phase, v1alpha1.WorkflowStepPhaseFailed)
			r.Equal(status.Reason, types.StatusReasonExecute)
			continue
		}
		r.Equal(status.Phase, v1alpha1.WorkflowStepPhaseSucceeded)
	}

}

func TestErrCases(t *testing.T) {
	wfCtx := newWorkflowContextForTest(t)
	r := require.New(t)
	closeVar := cuecontext.New().CompileString(`
	close({
		x: 100
 })
 `)
	err := wfCtx.SetVar(closeVar, "score")
	r.NoError(err)
	compiler := cuex.NewCompilerWithInternalPackages(
		// legacy packages
		pkgruntime.Must(cuexruntime.NewInternalPackage("test", "", map[string]cuexruntime.ProviderFn{
			"ok": providertypes.LegacyGenericProviderFn[any, any](func(ctx context.Context, val *providertypes.LegacyParams[any]) (*any, error) {
				return nil, nil
			}),
			"error": providertypes.LegacyGenericProviderFn[any, any](func(ctx context.Context, val *providertypes.LegacyParams[any]) (*any, error) {
				return nil, errors.New("mock error")
			}),
			"input": cuexruntime.NativeProviderFn(func(ctx context.Context, v cue.Value) (cue.Value, error) {
				val := v.LookupPath(cue.ParsePath("set.prefixIP"))
				str, err := val.String()
				r.NoError(err)
				r.Equal(str, "1.1.1.1")
				return v, nil
			}),
		},
		)),
	)
	pCtx := process.NewContext(process.ContextData{
		Name:      "app",
		Namespace: "default",
	})
	tasksLoader := NewTaskLoader(mockLoadTemplate, 0, pCtx, compiler)

	steps := []v1alpha1.WorkflowStep{
		{
			WorkflowStepBase: v1alpha1.WorkflowStepBase{
				Name: "input-replace",
				Type: "ok",
				Properties: &runtime.RawExtension{Raw: []byte(`
{"score": {"x": 101}}
		`)},
				Inputs: v1alpha1.StepInputs{{
					From:         "score",
					ParameterKey: "score",
				}},
			},
		},
		{
			WorkflowStepBase: v1alpha1.WorkflowStepBase{
				Name: "input",
				Type: "input",
				Inputs: v1alpha1.StepInputs{{
					From:         "podIP",
					ParameterKey: "prefixIP",
				}},
			},
		},
		{
			WorkflowStepBase: v1alpha1.WorkflowStepBase{
				Name: "output-var-conflict",
				Type: "ok",
				Outputs: v1alpha1.StepOutputs{{
					Name:      "score",
					ValueFrom: "name",
				}},
			},
		},
		{
			WorkflowStepBase: v1alpha1.WorkflowStepBase{
				Name: "wait",
				Type: "wait",
			},
		},
		{
			WorkflowStepBase: v1alpha1.WorkflowStepBase{
				Name: "err",
				Type: "error",
			},
		},
		{
			WorkflowStepBase: v1alpha1.WorkflowStepBase{
				Name: "failed-after-retries",
				Type: "error",
			},
		},
	}
	for _, step := range steps {
		gen, err := tasksLoader.GetTaskGenerator(context.Background(), step.Type)
		r.NoError(err)
		run, err := gen(step, &types.TaskGeneratorOptions{})
		r.NoError(err)
		status, operation, _ := run.Run(wfCtx, &types.TaskRunOptions{})
		switch step.Name {
		case "input-replace":
			r.Equal(status.Message, "")
			r.Equal(operation.Waiting, false)
			r.Equal(status.Phase, v1alpha1.WorkflowStepPhaseSucceeded)
			r.Equal(status.Reason, "")
		case "input":
			r.Equal(status.Message, "get input from [podIP]: failed to lookup value: var(path=podIP) not exist")
			r.Equal(operation.Waiting, false)
			r.Equal(status.Phase, v1alpha1.WorkflowStepPhaseFailed)
			r.Equal(status.Reason, types.StatusReasonInput)
		case "output-var-conflict":
			r.Contains(status.Message, "conflict")
			r.Equal(operation.Waiting, false)
			r.Equal(status.Phase, v1alpha1.WorkflowStepPhaseSucceeded)
		case "failed-after-retries":
			wfContext.CleanupMemoryStore("app-v1", "default")
			newCtx := newWorkflowContextForTest(t)
			for i := 0; i < types.MaxWorkflowStepErrorRetryTimes; i++ {
				status, operation, err = run.Run(newCtx, &types.TaskRunOptions{Compiler: compiler})
				r.NoError(err)
				r.Equal(operation.Waiting, true)
				r.Equal(operation.FailedAfterRetries, false)
				r.Equal(status.Phase, v1alpha1.WorkflowStepPhaseFailed)
			}
			status, operation, err = run.Run(newCtx, &types.TaskRunOptions{Compiler: compiler})
			r.NoError(err)
			r.Equal(operation.Waiting, false)
			r.Equal(operation.FailedAfterRetries, true)
			r.Equal(status.Phase, v1alpha1.WorkflowStepPhaseFailed)
			r.Equal(status.Reason, types.StatusReasonFailedAfterRetries)
		default:
			r.Equal(operation.Waiting, true)
			r.Equal(status.Phase, v1alpha1.WorkflowStepPhaseFailed)
		}
	}
}

func TestPendingInputCheck(t *testing.T) {
	wfCtx := newWorkflowContextForTest(t)
	r := require.New(t)
	step := v1alpha1.WorkflowStep{
		WorkflowStepBase: v1alpha1.WorkflowStepBase{
			Name: "pending",
			Type: "ok",
			Inputs: v1alpha1.StepInputs{{
				From:         "score",
				ParameterKey: "score",
			}},
		},
	}
	pCtx := process.NewContext(process.ContextData{
		Name:      "app",
		Namespace: "default",
	})
	tasksLoader := NewTaskLoader(mockLoadTemplate, 0, pCtx, providers.Compiler.Get())
	gen, err := tasksLoader.GetTaskGenerator(context.Background(), step.Type)
	r.NoError(err)
	run, err := gen(step, &types.TaskGeneratorOptions{})
	r.NoError(err)
	logCtx := monitorContext.NewTraceContext(context.Background(), "test-app")
	p, _ := run.Pending(logCtx, wfCtx, nil)
	r.Equal(p, true)
	score := cuecontext.New().CompileString(`100`)
	r.NoError(err)
	err = wfCtx.SetVar(score, "score")
	r.NoError(err)
	p, _ = run.Pending(logCtx, wfCtx, nil)
	r.Equal(p, false)
}

func TestPendingDependsOnCheck(t *testing.T) {
	wfCtx := newWorkflowContextForTest(t)
	r := require.New(t)
	step := v1alpha1.WorkflowStep{
		WorkflowStepBase: v1alpha1.WorkflowStepBase{
			Name:      "pending",
			Type:      "ok",
			DependsOn: []string{"depend"},
		},
	}
	pCtx := process.NewContext(process.ContextData{
		Name:      "app",
		Namespace: "default",
	})
	tasksLoader := NewTaskLoader(mockLoadTemplate, 0, pCtx, providers.Compiler.Get())
	gen, err := tasksLoader.GetTaskGenerator(context.Background(), step.Type)
	r.NoError(err)
	run, err := gen(step, &types.TaskGeneratorOptions{})
	r.NoError(err)
	logCtx := monitorContext.NewTraceContext(context.Background(), "test-app")
	p, _ := run.Pending(logCtx, wfCtx, nil)
	r.Equal(p, true)
	ss := map[string]v1alpha1.StepStatus{
		"depend": {
			Phase: v1alpha1.WorkflowStepPhaseSucceeded,
		},
	}
	p, _ = run.Pending(logCtx, wfCtx, ss)
	r.Equal(p, false)
}

func TestSkip(t *testing.T) {
	r := require.New(t)
	step := v1alpha1.WorkflowStep{
		WorkflowStepBase: v1alpha1.WorkflowStepBase{
			Name: "skip",
			Type: "ok",
		},
	}
	pCtx := process.NewContext(process.ContextData{
		Name:      "app",
		Namespace: "default",
	})
	tasksLoader := NewTaskLoader(mockLoadTemplate, 0, pCtx, providers.Compiler.Get())
	gen, err := tasksLoader.GetTaskGenerator(context.Background(), step.Type)
	r.NoError(err)
	runner, err := gen(step, &types.TaskGeneratorOptions{})
	r.NoError(err)
	wfCtx := newWorkflowContextForTest(t)
	status, operations, err := runner.Run(wfCtx, &types.TaskRunOptions{
		PreCheckHooks: []types.TaskPreCheckHook{
			func(step v1alpha1.WorkflowStep, options *types.PreCheckOptions) (*types.PreCheckResult, error) {
				return &types.PreCheckResult{Skip: true}, nil
			},
		},
	})
	r.NoError(err)
	r.Equal(status.Phase, v1alpha1.WorkflowStepPhaseSkipped)
	r.Equal(status.Reason, types.StatusReasonSkip)
	r.Equal(operations.Skip, true)
}

func TestTimeout(t *testing.T) {
	r := require.New(t)
	compiler := cuex.NewCompilerWithInternalPackages(
		// legacy packages
		pkgruntime.Must(cuexruntime.NewInternalPackage("test", "", map[string]cuexruntime.ProviderFn{
			"ok": providertypes.LegacyGenericProviderFn[any, any](func(ctx context.Context, val *providertypes.LegacyParams[any]) (*any, error) {
				return nil, nil
			}),
		})),
	)
	step := v1alpha1.WorkflowStep{
		WorkflowStepBase: v1alpha1.WorkflowStepBase{
			Name: "timeout",
			Type: "ok",
		},
	}
	pCtx := process.NewContext(process.ContextData{
		Name:      "app",
		Namespace: "default",
	})
	tasksLoader := NewTaskLoader(mockLoadTemplate, 0, pCtx, compiler)
	gen, err := tasksLoader.GetTaskGenerator(context.Background(), step.Type)
	r.NoError(err)
	runner, err := gen(step, &types.TaskGeneratorOptions{})
	r.NoError(err)
	ctx := newWorkflowContextForTest(t)
	status, _, err := runner.Run(ctx, &types.TaskRunOptions{
		PreCheckHooks: []types.TaskPreCheckHook{
			func(step v1alpha1.WorkflowStep, options *types.PreCheckOptions) (*types.PreCheckResult, error) {
				return &types.PreCheckResult{Timeout: true}, nil
			},
		},
	})
	r.NoError(err)
	r.Equal(status.Phase, v1alpha1.WorkflowStepPhaseFailed)
	r.Equal(status.Reason, types.StatusReasonTimeout)
}

func TestValidateIfValue(t *testing.T) {
	ctx := newWorkflowContextForTest(t)
	pCtx := process.NewContext(process.ContextData{
		Name:      "app",
		Namespace: "default",
		Data:      map[string]interface{}{"arr": []string{"a", "b"}},
	})

	r := require.New(t)
	logCtx := monitorContext.NewTraceContext(context.Background(), "test-app")
	basicVal, err := MakeBasicValue(logCtx, providers.Compiler.Get(), &runtime.RawExtension{Raw: []byte(`{"key": "value"}`)}, pCtx)
	r.NoError(err)

	testCases := []struct {
		name        string
		step        v1alpha1.WorkflowStep
		status      map[string]v1alpha1.StepStatus
		expected    bool
		expectedErr string
	}{
		{
			name: "timeout true",
			step: v1alpha1.WorkflowStep{
				WorkflowStepBase: v1alpha1.WorkflowStepBase{
					If: "status.step1.timeout",
				},
			},
			status: map[string]v1alpha1.StepStatus{
				"step1": {
					Reason: "Timeout",
				},
			},
			expected: true,
		},
		{
			name: "context true",
			step: v1alpha1.WorkflowStep{
				WorkflowStepBase: v1alpha1.WorkflowStepBase{
					If: `context.name == "app"`,
				},
			},
			expected: true,
		},
		{
			name: "context arr true",
			step: v1alpha1.WorkflowStep{
				WorkflowStepBase: v1alpha1.WorkflowStepBase{
					If: `context.arr[0] == "a"`,
				},
			},
			expected: true,
		},
		{
			name: "parameter true",
			step: v1alpha1.WorkflowStep{
				WorkflowStepBase: v1alpha1.WorkflowStepBase{
					If: `parameter.key == "value"`,
				},
			},
			expected: true,
		},
		{
			name: "failed true",
			step: v1alpha1.WorkflowStep{
				WorkflowStepBase: v1alpha1.WorkflowStepBase{
					If: `status.step1.phase != "failed"`,
				},
			},
			status: map[string]v1alpha1.StepStatus{
				"step1": {
					Phase: v1alpha1.WorkflowStepPhaseSucceeded,
				},
			},
			expected: true,
		},
		{
			name: "input true",
			step: v1alpha1.WorkflowStep{
				WorkflowStepBase: v1alpha1.WorkflowStepBase{
					If: `inputs.test == "yes"`,
					Inputs: v1alpha1.StepInputs{
						{
							From: "test",
						},
					},
				},
			},
			expected: true,
		},
		{
			name: "input with arr in context",
			step: v1alpha1.WorkflowStep{
				WorkflowStepBase: v1alpha1.WorkflowStepBase{
					If: `inputs["context.arr[0]"] == "a"`,
					Inputs: v1alpha1.StepInputs{
						{
							From: "context.arr[0]",
						},
					},
				},
			},
			expected: true,
		},
		{
			name: "input false with dash",
			step: v1alpha1.WorkflowStep{
				WorkflowStepBase: v1alpha1.WorkflowStepBase{
					If: `inputs["test-input"] == "yes"`,
					Inputs: v1alpha1.StepInputs{
						{
							From: "test-input",
						},
					},
				},
			},
			expectedErr: "invalid if value",
			expected:    false,
		},
		{
			name: "input value is struct",
			step: v1alpha1.WorkflowStep{
				WorkflowStepBase: v1alpha1.WorkflowStepBase{
					If: `inputs["test-struct"].hello == "world"`,
					Inputs: v1alpha1.StepInputs{
						{
							From: "test-struct",
						},
					},
				},
			},
			expected: true,
		},
		{
			name: "dash in if",
			step: v1alpha1.WorkflowStep{
				WorkflowStepBase: v1alpha1.WorkflowStepBase{
					If: "status.step1-test.timeout",
				},
			},
			expectedErr: "invalid if value",
			expected:    false,
		},
		{
			name: "dash in status",
			step: v1alpha1.WorkflowStep{
				WorkflowStepBase: v1alpha1.WorkflowStepBase{
					If: `status["step1-test"].timeout`,
				},
			},
			status: map[string]v1alpha1.StepStatus{
				"step1-test": {
					Reason: "Timeout",
				},
			},
			expected: true,
		},
		{
			name: "error if",
			step: v1alpha1.WorkflowStep{
				WorkflowStepBase: v1alpha1.WorkflowStepBase{
					If: `test == true`,
				},
			},
			expectedErr: "invalid if value",
			expected:    false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			v, err := ValidateIfValue(ctx, tc.step, tc.status, basicVal)
			if tc.expectedErr != "" {
				r.Contains(err.Error(), tc.expectedErr)
				r.Equal(v, false)
				return
			}
			r.NoError(err)
			r.Equal(v, tc.expected)
		})
	}
}

func newWorkflowContextForTest(t *testing.T) wfContext.Context {
	r := require.New(t)
	cm := corev1.ConfigMap{}
	testCaseJson, err := yaml.YAMLToJSON([]byte(testCaseYaml))
	r.NoError(err)
	err = json.Unmarshal(testCaseJson, &cm)
	r.NoError(err)

	cli := &test.MockClient{
		MockGet: func(ctx context.Context, key client.ObjectKey, obj client.Object) error {
			o, ok := obj.(*corev1.ConfigMap)
			if ok {
				*o = cm
			}
			return nil
		},
		MockPatch: func(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
			return nil
		},
	}
	singleton.KubeClient.Set(cli)
	wfCtx, err := wfContext.NewContext(context.Background(), "default", "app-v1", nil)
	r.NoError(err)
	cuectx := cuecontext.New()
	v := cuectx.CompileString(`"yes"`)
	r.NoError(wfCtx.SetVar(v, "test"))
	v = cuectx.CompileString(`{hello: "world"}`)
	r.NoError(err)
	r.NoError(wfCtx.SetVar(v, "test-struct"))
	return wfCtx
}

func mockLoadTemplate(_ context.Context, name string) (string, error) {
	templ := `
parameter: {}
process: {
	#provider: "test"
	#do: "%s"
	parameter
}
// check injected context.
name: context.name
`
	switch name {
	case "output":
		return fmt.Sprintf(templ+`myIP: process.myIP`, "output"), nil
	default:
		return fmt.Sprintf(templ, name), nil
	}
}

var (
	testCaseYaml = `apiVersion: v1
data:
  test: ""
kind: ConfigMap
metadata:
  name: app-v1
`
)
