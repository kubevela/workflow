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

	"github.com/crossplane/crossplane-runtime/pkg/test"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"

	monitorContext "github.com/kubevela/pkg/monitor/context"

	"github.com/kubevela/workflow/api/v1alpha1"
	wfContext "github.com/kubevela/workflow/pkg/context"
	"github.com/kubevela/workflow/pkg/cue/model/value"
	"github.com/kubevela/workflow/pkg/cue/process"
	"github.com/kubevela/workflow/pkg/providers"
	"github.com/kubevela/workflow/pkg/types"
)

func TestTaskLoader(t *testing.T) {
	wfCtx := newWorkflowContextForTest(t)
	r := require.New(t)
	discover := providers.NewProviders()
	discover.Register("test", map[string]types.Handler{
		"output": func(mCtx monitorContext.Context, ctx wfContext.Context, v *value.Value, act types.Action) error {
			ip, _ := v.MakeValue(`
myIP: value: "1.1.1.1"            
`)
			return v.FillObject(ip)
		},
		"input": func(mCtx monitorContext.Context, ctx wfContext.Context, v *value.Value, act types.Action) error {
			val, err := v.LookupValue("set", "prefixIP")
			r.NoError(err)
			str, err := val.CueValue().String()
			r.NoError(err)
			r.Equal(str, "1.1.1.1")
			return nil
		},
		"wait": func(mCtx monitorContext.Context, ctx wfContext.Context, v *value.Value, act types.Action) error {
			act.Wait("I am waiting")
			return nil
		},
		"terminate": func(mCtx monitorContext.Context, ctx wfContext.Context, v *value.Value, act types.Action) error {
			act.Terminate("I am terminated")
			return nil
		},
		"executeFailed": func(mCtx monitorContext.Context, ctx wfContext.Context, v *value.Value, act types.Action) error {
			return errors.New("execute error")
		},
		"ok": func(mCtx monitorContext.Context, ctx wfContext.Context, v *value.Value, act types.Action) error {
			return nil
		},
	})

	pCtx := process.NewContext(process.ContextData{
		Name:      "app",
		Namespace: "default",
	})
	tasksLoader := NewTaskLoader(mockLoadTemplate, nil, discover, 0, pCtx)

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
				Name: "steps",
				Type: "steps",
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
	closeVar, err := value.NewValue(`
close({
   x: 100
})
`, nil, "", value.TagFieldOrder)
	r.NoError(err)
	err = wfCtx.SetVar(closeVar, "score")
	r.NoError(err)
	discover := providers.NewProviders()
	discover.Register("test", map[string]types.Handler{
		"input": func(mCtx monitorContext.Context, ctx wfContext.Context, v *value.Value, act types.Action) error {
			val, err := v.LookupValue("prefixIP")
			r.NoError(err)
			str, err := val.CueValue().String()
			r.NoError(err)
			r.Equal(str, "1.1.1.1")
			return nil
		},
		"ok": func(mCtx monitorContext.Context, ctx wfContext.Context, v *value.Value, act types.Action) error {
			return nil
		},
		"error": func(mCtx monitorContext.Context, ctx wfContext.Context, v *value.Value, act types.Action) error {
			return errors.New("mock error")
		},
	})
	pCtx := process.NewContext(process.ContextData{
		Name:      "app",
		Namespace: "default",
	})
	tasksLoader := NewTaskLoader(mockLoadTemplate, nil, discover, 0, pCtx)

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
				status, operation, err = run.Run(newCtx, &types.TaskRunOptions{})
				r.NoError(err)
				r.Equal(operation.Waiting, true)
				r.Equal(operation.FailedAfterRetries, false)
				r.Equal(status.Phase, v1alpha1.WorkflowStepPhaseFailed)
			}
			status, operation, err = run.Run(newCtx, &types.TaskRunOptions{})
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

func TestSteps(t *testing.T) {

	var (
		echo    string
		mockErr = errors.New("mock error")
	)

	wfCtx := newWorkflowContextForTest(t)
	r := require.New(t)
	discover := providers.NewProviders()
	discover.Register("test", map[string]types.Handler{
		"ok": func(mCtx monitorContext.Context, ctx wfContext.Context, v *value.Value, act types.Action) error {
			echo = echo + "ok"
			return nil
		},
		"error": func(mCtx monitorContext.Context, ctx wfContext.Context, v *value.Value, act types.Action) error {
			return mockErr
		},
	})
	exec := &executor{
		handlers: discover,
	}

	testCases := []struct {
		base     string
		expected string
		hasErr   bool
	}{
		{
			base: `
process: {
	#provider: "test"
	#do: "ok"
}

#up: [process]
`,
			expected: "okok",
		},
		{
			base: `
process: {
	#provider: "test"
	#do: "ok"
}

#up: [process,{
  #do: "steps"
  p1: process
  #up: [process]
}]
`,
			expected: "okokokok",
		},
		{
			base: `
process: {
	#provider: "test"
	#do: "ok"
}

#up: [process,{
  p1: process
  #up: [process]
}]
`,
			expected: "okok",
		},
		{
			base: `
process: {
	#provider: "test"
	#do: "ok"
}

#up: [process,{
  #do: "steps"
  err: {
    #provider: "test"
	#do: "error"
  } @step(1)
  #up: [{},process] @step(2)
}]
`,
			expected: "okok",
			hasErr:   true,
		},

		{
			base: `
	#provider: "test"
	#do: "ok"
`,
			expected: "ok",
		},
		{
			base: `
process: {
	#provider: "test"
	#do: "ok"
    err: true
}

if process.err {
  err: {
    #provider: "test"
	  #do: "error"
  }
}

apply: {
	#provider: "test"
	#do: "ok"
}

#up: [process,{}]
`,
			expected: "ok",
			hasErr:   true,
		},
	}

	for i, tc := range testCases {
		echo = ""
		v, err := value.NewValue(tc.base, nil, "", value.TagFieldOrder)
		r.NoError(err)
		err = exec.doSteps(nil, wfCtx, v)
		r.Equal(err != nil, tc.hasErr)
		r.Equal(tc.expected, echo, i)
	}

}

func TestPendingInputCheck(t *testing.T) {
	wfCtx := newWorkflowContextForTest(t)
	r := require.New(t)
	discover := providers.NewProviders()
	discover.Register("test", map[string]types.Handler{
		"ok": func(mCtx monitorContext.Context, ctx wfContext.Context, v *value.Value, act types.Action) error {
			return nil
		},
	})
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
	tasksLoader := NewTaskLoader(mockLoadTemplate, nil, discover, 0, pCtx)
	gen, err := tasksLoader.GetTaskGenerator(context.Background(), step.Type)
	r.NoError(err)
	run, err := gen(step, &types.TaskGeneratorOptions{})
	r.NoError(err)
	logCtx := monitorContext.NewTraceContext(context.Background(), "test-app")
	p, _ := run.Pending(logCtx, wfCtx, nil)
	r.Equal(p, true)
	score, err := value.NewValue(`
100
`, nil, "")
	r.NoError(err)
	err = wfCtx.SetVar(score, "score")
	r.NoError(err)
	p, _ = run.Pending(logCtx, wfCtx, nil)
	r.Equal(p, false)
}

func TestPendingDependsOnCheck(t *testing.T) {
	wfCtx := newWorkflowContextForTest(t)
	r := require.New(t)
	discover := providers.NewProviders()
	discover.Register("test", map[string]types.Handler{
		"ok": func(mCtx monitorContext.Context, ctx wfContext.Context, v *value.Value, act types.Action) error {
			return nil
		},
	})
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
	tasksLoader := NewTaskLoader(mockLoadTemplate, nil, discover, 0, pCtx)
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
	discover := providers.NewProviders()
	discover.Register("test", map[string]types.Handler{
		"ok": func(mCtx monitorContext.Context, ctx wfContext.Context, v *value.Value, act types.Action) error {
			return nil
		},
	})
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
	tasksLoader := NewTaskLoader(mockLoadTemplate, nil, discover, 0, pCtx)
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
	discover := providers.NewProviders()
	discover.Register("test", map[string]types.Handler{
		"ok": func(mCtx monitorContext.Context, ctx wfContext.Context, v *value.Value, act types.Action) error {
			return nil
		},
	})
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
	tasksLoader := NewTaskLoader(mockLoadTemplate, nil, discover, 0, pCtx)
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
	logCtx := monitorContext.NewTraceContext(context.Background(), "test-app")
	basicVal, basicTemplate, err := MakeBasicValue(logCtx, ctx, nil, "test-step", "id", `key: "value"`, pCtx)
	r := require.New(t)
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
			expectedErr: "not found",
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
			v, err := ValidateIfValue(ctx, tc.step, tc.status, &types.PreCheckOptions{
				BasicTemplate: basicTemplate,
				BasicValue:    basicVal,
			})
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
	wfCtx, err := wfContext.NewContext(context.Background(), cli, "default", "app-v1", nil)
	r.NoError(err)
	v, err := value.NewValue(`"yes"`, nil, "")
	r.NoError(err)
	r.NoError(wfCtx.SetVar(v, "test"))
	v, err = value.NewValue(`{hello: "world"}`, nil, "")
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
	case "input":
		return fmt.Sprintf(templ, "input"), nil
	case "wait":
		return fmt.Sprintf(templ, "wait"), nil
	case "terminate":
		return fmt.Sprintf(templ, "terminate"), nil
	case "templateError":
		return `
output: xx
`, nil
	case "executeFailed":
		return fmt.Sprintf(templ, "executeFailed"), nil
	case "ok":
		return fmt.Sprintf(templ, "ok"), nil
	case "error":
		return fmt.Sprintf(templ, "error"), nil
	case "steps":
		return `
#do: "steps"
ok: {
	#provider: "test"
	#do: "ok"
}
`, nil
	}

	return "", nil
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
