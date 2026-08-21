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
	"github.com/crossplane/crossplane-runtime/v2/pkg/test"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic/fake"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"

	cuexv1alpha1 "github.com/kubevela/pkg/apis/cue/v1alpha1"
	"github.com/kubevela/pkg/cue/cuex"
	cuexruntime "github.com/kubevela/pkg/cue/cuex/runtime"
	monitorContext "github.com/kubevela/pkg/monitor/context"
	pkgruntime "github.com/kubevela/pkg/util/runtime"
	"github.com/kubevela/pkg/util/singleton"

	oamv1alpha1 "github.com/kubevela/pkg/apis/oam/v1alpha1"

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

	steps := []oamv1alpha1.WorkflowStep{
		{
			WorkflowStepBase: oamv1alpha1.WorkflowStepBase{
				Name: "output",
				Type: "output",
				Outputs: oamv1alpha1.StepOutputs{{
					ValueFrom: "myIP.value",
					Name:      "podIP",
				}},
			},
		},
		{
			WorkflowStepBase: oamv1alpha1.WorkflowStepBase{
				Name: "input",
				Type: "input",
				Inputs: oamv1alpha1.StepInputs{{
					From:         "podIP",
					ParameterKey: "set.prefixIP",
				}},
			},
		},
		{
			WorkflowStepBase: oamv1alpha1.WorkflowStepBase{
				Name: "wait",
				Type: "wait",
			},
		},
		{
			WorkflowStepBase: oamv1alpha1.WorkflowStepBase{
				Name: "terminate",
				Type: "terminate",
			},
		},
		{
			WorkflowStepBase: oamv1alpha1.WorkflowStepBase{
				Name: "template",
				Type: "templateError",
			},
		},
		{
			WorkflowStepBase: oamv1alpha1.WorkflowStepBase{
				Name: "execute",
				Type: "executeFailed",
			},
		},
		{
			WorkflowStepBase: oamv1alpha1.WorkflowStepBase{
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

	steps := []oamv1alpha1.WorkflowStep{
		{
			WorkflowStepBase: oamv1alpha1.WorkflowStepBase{
				Name: "input-replace",
				Type: "ok",
				Properties: &runtime.RawExtension{Raw: []byte(`
{"score": {"x": 101}}
		`)},
				Inputs: oamv1alpha1.StepInputs{{
					From:         "score",
					ParameterKey: "score",
				}},
			},
		},
		{
			WorkflowStepBase: oamv1alpha1.WorkflowStepBase{
				Name: "input",
				Type: "input",
				Inputs: oamv1alpha1.StepInputs{{
					From:         "podIP",
					ParameterKey: "prefixIP",
				}},
			},
		},
		{
			WorkflowStepBase: oamv1alpha1.WorkflowStepBase{
				Name: "output-var-conflict",
				Type: "ok",
				Outputs: oamv1alpha1.StepOutputs{{
					Name:      "score",
					ValueFrom: "name",
				}},
			},
		},
		{
			WorkflowStepBase: oamv1alpha1.WorkflowStepBase{
				Name: "wait",
				Type: "wait",
			},
		},
		{
			WorkflowStepBase: oamv1alpha1.WorkflowStepBase{
				Name: "err",
				Type: "error",
			},
		},
		{
			WorkflowStepBase: oamv1alpha1.WorkflowStepBase{
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
	step := oamv1alpha1.WorkflowStep{
		WorkflowStepBase: oamv1alpha1.WorkflowStepBase{
			Name: "pending",
			Type: "ok",
			Inputs: oamv1alpha1.StepInputs{{
				From:         "score",
				ParameterKey: "score",
			}},
		},
	}
	pCtx := process.NewContext(process.ContextData{
		Name:      "app",
		Namespace: "default",
	})
	tasksLoader := NewTaskLoader(mockLoadTemplate, 0, pCtx, providers.DefaultCompiler.Get())
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
	step := oamv1alpha1.WorkflowStep{
		WorkflowStepBase: oamv1alpha1.WorkflowStepBase{
			Name:      "pending",
			Type:      "ok",
			DependsOn: []string{"depend"},
		},
	}
	pCtx := process.NewContext(process.ContextData{
		Name:      "app",
		Namespace: "default",
	})
	tasksLoader := NewTaskLoader(mockLoadTemplate, 0, pCtx, providers.DefaultCompiler.Get())
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
	step := oamv1alpha1.WorkflowStep{
		WorkflowStepBase: oamv1alpha1.WorkflowStepBase{
			Name: "skip",
			Type: "ok",
		},
	}
	pCtx := process.NewContext(process.ContextData{
		Name:      "app",
		Namespace: "default",
	})
	tasksLoader := NewTaskLoader(mockLoadTemplate, 0, pCtx, providers.DefaultCompiler.Get())
	gen, err := tasksLoader.GetTaskGenerator(context.Background(), step.Type)
	r.NoError(err)
	runner, err := gen(step, &types.TaskGeneratorOptions{})
	r.NoError(err)
	wfCtx := newWorkflowContextForTest(t)
	status, operations, err := runner.Run(wfCtx, &types.TaskRunOptions{
		PreCheckHooks: []types.TaskPreCheckHook{
			func(step oamv1alpha1.WorkflowStep, options *types.PreCheckOptions) (*types.PreCheckResult, error) {
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
	step := oamv1alpha1.WorkflowStep{
		WorkflowStepBase: oamv1alpha1.WorkflowStepBase{
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
			func(step oamv1alpha1.WorkflowStep, options *types.PreCheckOptions) (*types.PreCheckResult, error) {
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
	basicVal, err := MakeBasicValue(logCtx, providers.DefaultCompiler.Get(), &runtime.RawExtension{Raw: []byte(`{"key": "value"}`)}, pCtx)
	r.NoError(err)

	testCases := []struct {
		name        string
		step        oamv1alpha1.WorkflowStep
		status      map[string]v1alpha1.StepStatus
		expected    bool
		expectedErr string
	}{
		{
			name: "timeout true",
			step: oamv1alpha1.WorkflowStep{
				WorkflowStepBase: oamv1alpha1.WorkflowStepBase{
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
			step: oamv1alpha1.WorkflowStep{
				WorkflowStepBase: oamv1alpha1.WorkflowStepBase{
					If: `context.name == "app"`,
				},
			},
			expected: true,
		},
		{
			name: "context arr true",
			step: oamv1alpha1.WorkflowStep{
				WorkflowStepBase: oamv1alpha1.WorkflowStepBase{
					If: `context.arr[0] == "a"`,
				},
			},
			expected: true,
		},
		{
			name: "parameter true",
			step: oamv1alpha1.WorkflowStep{
				WorkflowStepBase: oamv1alpha1.WorkflowStepBase{
					If: `parameter.key == "value"`,
				},
			},
			expected: true,
		},
		{
			name: "failed true",
			step: oamv1alpha1.WorkflowStep{
				WorkflowStepBase: oamv1alpha1.WorkflowStepBase{
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
			step: oamv1alpha1.WorkflowStep{
				WorkflowStepBase: oamv1alpha1.WorkflowStepBase{
					If: `inputs.test == "yes"`,
					Inputs: oamv1alpha1.StepInputs{
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
			step: oamv1alpha1.WorkflowStep{
				WorkflowStepBase: oamv1alpha1.WorkflowStepBase{
					If: `inputs["context.arr[0]"] == "a"`,
					Inputs: oamv1alpha1.StepInputs{
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
			step: oamv1alpha1.WorkflowStep{
				WorkflowStepBase: oamv1alpha1.WorkflowStepBase{
					If: `inputs["test-input"] == "yes"`,
					Inputs: oamv1alpha1.StepInputs{
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
			step: oamv1alpha1.WorkflowStep{
				WorkflowStepBase: oamv1alpha1.WorkflowStepBase{
					If: `inputs["test-struct"].hello == "world"`,
					Inputs: oamv1alpha1.StepInputs{
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
			step: oamv1alpha1.WorkflowStep{
				WorkflowStepBase: oamv1alpha1.WorkflowStepBase{
					If: "status.step1-test.timeout",
				},
			},
			expectedErr: "invalid if value",
			expected:    false,
		},
		{
			name: "dash in status",
			step: oamv1alpha1.WorkflowStep{
				WorkflowStepBase: oamv1alpha1.WorkflowStepBase{
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
			step: oamv1alpha1.WorkflowStep{
				WorkflowStepBase: oamv1alpha1.WorkflowStepBase{
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
	scheme := runtime.NewScheme()
	r.NoError(cuexv1alpha1.AddToScheme(scheme))
	fakeDynamicClient := fake.NewSimpleDynamicClient(scheme)
	singleton.DynamicClient.Set(fakeDynamicClient)
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

// TestTemplateErrorPersistsAcrossReconciles guards against a step whose CUE template
// fails to compile reporting Succeeded on the second and later reconciles.
//
// The bug: the guard in makeTaskGenerator read exec.stepStatus (the PREVIOUS
// reconcile's phase) instead of exec.wfStatus (this reconcile's). Once pass 1
// persisted Failed, pass 2 saw Failed, skipped the taskv.Err() check entirely, and
// returned the untouched optimistic Succeeded default.
//
// Two things are essential to reaching the bug, and omitting either hides it:
//
//  1. Feed the returned status back in as options.StepStatus before the second run.
//     That is what pkg/executor/workflow.go:730 does. Without it, stepStatus stays at
//     its default and the guard is always true (see the "failed-after-retries" case in
//     TestTaskLoader, which loops but never feeds status back).
//  2. Call the generator again for the second pass. The executor struct -- and the
//     optimistic Succeeded seed on wfStatus -- is created per generator call, not per
//     Run call. A reconcile regenerates runners
//     (controllers/workflowrun_controller.go:134), so reusing one runner would carry
//     pass 1's Failed wfStatus into pass 2 and mask the defect.
func TestTemplateErrorPersistsAcrossReconciles(t *testing.T) {
	r := require.New(t)

	// Reset the in-memory failure counter. checkErrorTimes (action.go:145-152) keys it on
	// exec.wfStatus.ID, which is empty when TaskGeneratorOptions carries no ID -- so it is
	// shared with every other test using the app-v1/default context. At
	// MaxWorkflowStepErrorRetryTimes (10) the reason flips to StatusReasonFailedAfterRetries
	// and the assertions below would fail for an unrelated reason.
	wfContext.CleanupMemoryStore("app-v1", "default")
	wfCtx := newWorkflowContextForTest(t)

	compiler := cuex.NewCompilerWithInternalPackages(
		pkgruntime.Must(cuexruntime.NewInternalPackage("test", "", map[string]cuexruntime.ProviderFn{
			// Returns a value carrying an unresolved reference, so taskv.Err() != nil.
			"templateError": cuexruntime.NativeProviderFn(func(ctx context.Context, v cue.Value) (cue.Value, error) {
				return v.Context().CompileString("output: xxx"), nil
			}),
		})),
	)

	pCtx := process.NewContext(process.ContextData{
		Name:      "app",
		Namespace: "default",
	})
	tasksLoader := NewTaskLoader(mockLoadTemplate, 0, pCtx, compiler)

	step := oamv1alpha1.WorkflowStep{
		WorkflowStepBase: oamv1alpha1.WorkflowStepBase{
			Name: "template",
			Type: "templateError",
		},
	}

	gen, err := tasksLoader.GetTaskGenerator(context.Background(), step.Type)
	r.NoError(err)
	run, err := gen(step, &types.TaskGeneratorOptions{})
	r.NoError(err)

	// Pass 1: no persisted status yet.
	firstStatus, _, err := run.Run(wfCtx, &types.TaskRunOptions{})
	r.NoError(err)
	r.Equal(v1alpha1.WorkflowStepPhaseFailed, firstStatus.Phase, "first reconcile must fail")
	r.Equal(types.StatusReasonExecute, firstStatus.Reason)
	r.NotEmpty(firstStatus.Message, "first reconcile must report the compile error")

	// Pass 2: a fresh runner, as a reconcile produces, plus pass 1's status fed back.
	secondRun, err := gen(step, &types.TaskGeneratorOptions{})
	r.NoError(err)
	secondStatus, _, err := secondRun.Run(wfCtx, &types.TaskRunOptions{
		StepStatus: map[string]v1alpha1.StepStatus{
			step.Name: firstStatus,
		},
	})
	r.NoError(err)
	r.Equal(v1alpha1.WorkflowStepPhaseFailed, secondStatus.Phase,
		"template is still broken, so the second reconcile must still fail")
	r.Equal(types.StatusReasonExecute, secondStatus.Reason)
	r.NotEmpty(secondStatus.Message,
		"second reconcile must still report the compile error, not an empty-message success")
}

// TestInFlightStatusSurvivesSecondReconcile asserts that a step which reports itself as
// waiting or suspended keeps that phase on a later reconcile, with its previous status
// fed back in. A step mid-flight has a legitimately incomplete CUE value, so the
// template-error check at task.go must stay skipped for it.
func TestInFlightStatusSurvivesSecondReconcile(t *testing.T) {
	r := require.New(t)

	compiler := cuex.NewCompilerWithInternalPackages(
		pkgruntime.Must(cuexruntime.NewInternalPackage("test", "", map[string]cuexruntime.ProviderFn{
			"wait": providertypes.LegacyGenericProviderFn[any, any](func(ctx context.Context, val *providertypes.LegacyParams[any]) (*any, error) {
				val.RuntimeParams.Action.Wait("I am waiting")
				return nil, nil
			}),
			"suspend": providertypes.LegacyGenericProviderFn[any, any](func(ctx context.Context, val *providertypes.LegacyParams[any]) (*any, error) {
				val.RuntimeParams.Action.Suspend("I am suspended")
				return nil, nil
			}),
		})),
	)

	pCtx := process.NewContext(process.ContextData{
		Name:      "app",
		Namespace: "default",
	})
	tasksLoader := NewTaskLoader(mockLoadTemplate, 0, pCtx, compiler)

	testCases := []struct {
		stepType      string
		expectedPhase v1alpha1.WorkflowStepPhase
	}{
		{stepType: "wait", expectedPhase: v1alpha1.WorkflowStepPhaseRunning},
		{stepType: "suspend", expectedPhase: v1alpha1.WorkflowStepPhaseSuspending},
	}

	for _, tc := range testCases {
		t.Run(tc.stepType, func(t *testing.T) {
			// Isolate each subtest from the shared in-memory failure counter. See the
			// comment in TestTemplateErrorPersistsAcrossReconciles.
			wfContext.CleanupMemoryStore("app-v1", "default")
			wfCtx := newWorkflowContextForTest(t)

			step := oamv1alpha1.WorkflowStep{
				WorkflowStepBase: oamv1alpha1.WorkflowStepBase{
					Name: tc.stepType,
					Type: tc.stepType,
				},
			}

			gen, err := tasksLoader.GetTaskGenerator(context.Background(), step.Type)
			r.NoError(err)

			run, err := gen(step, &types.TaskGeneratorOptions{})
			r.NoError(err)
			firstStatus, _, err := run.Run(wfCtx, &types.TaskRunOptions{})
			r.NoError(err)
			r.Equal(tc.expectedPhase, firstStatus.Phase)

			// Fresh runner for the second pass, as a reconcile produces.
			secondRun, err := gen(step, &types.TaskGeneratorOptions{})
			r.NoError(err)
			secondStatus, _, err := secondRun.Run(wfCtx, &types.TaskRunOptions{
				StepStatus: map[string]v1alpha1.StepStatus{
					step.Name: firstStatus,
				},
			})
			r.NoError(err)
			r.Equal(tc.expectedPhase, secondStatus.Phase,
				"an in-flight step must not be flipped to Failed by the template-error check")
		})
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
