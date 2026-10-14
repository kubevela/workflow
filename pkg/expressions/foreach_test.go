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

package expressions

import (
	"context"
	"errors"
	"testing"

	"cuelang.org/go/cue"
	"github.com/kubevela/pkg/apis/oam/v1alpha1"
	"github.com/kubevela/pkg/cel"
	"github.com/stretchr/testify/require"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/runtime"

	wfv1alpha1 "github.com/kubevela/workflow/api/v1alpha1"
	wfContext "github.com/kubevela/workflow/pkg/context"
	"github.com/kubevela/workflow/pkg/cue/process"
	"github.com/kubevela/workflow/pkg/types"
)

// wfVars is a workflow context that only has variables.
type wfVars struct {
	wfContext.Context
	vars
}

func (v wfVars) GetVar(paths ...string) (cue.Value, error) { return v.vars.GetVar(paths...) }

// A loop "per-tenant" over a group: fetch outputs data, greet outputs msg. A
// single-step loop "ping" outputs pong. summary runs after both.
var loopSteps = []v1alpha1.WorkflowStep{
	{WorkflowStepBase: v1alpha1.WorkflowStepBase{Name: "per-tenant", Type: types.WorkflowStepTypeStepGroup},
		ForEach: &v1alpha1.ForEach{Items: &apiextensionsv1.JSON{Raw: []byte(`["a","b"]`)}},
		SubSteps: []v1alpha1.WorkflowStepBase{
			{Name: "fetch", Type: "fetch", Outputs: v1alpha1.StepOutputs{{Name: "data", ValueFrom: "value"}}},
			{Name: "greet", Type: "greet", Outputs: v1alpha1.StepOutputs{{Name: "msg", ValueFrom: "value"}}},
		}},
	{WorkflowStepBase: v1alpha1.WorkflowStepBase{Name: "ping", Type: "ping",
		Outputs: v1alpha1.StepOutputs{{Name: "pong", ValueFrom: "value"}}},
		ForEach: &v1alpha1.ForEach{Items: &apiextensionsv1.JSON{Raw: []byte(`[1,2]`)}}},
	{WorkflowStepBase: v1alpha1.WorkflowStepBase{Name: "summary", Type: "echo"}},
}

// running is the process context of a step while it runs: its name, and the
// iteration when it is one.
func running(step string, index ...int) process.Context {
	pCtx := process.NewContext(process.ContextData{Name: "wr"})
	metas := []process.StepMetaKV{process.WithName(step)}
	if len(index) > 0 {
		metas = append(metas, process.WithLoop("a", index[0]))
	}
	process.NewStepRunTimeMeta().Fill(pCtx, metas)
	return pCtx
}

func resolve(t *testing.T, props string, pCtx process.Context, status map[string]wfv1alpha1.StepStatus,
	v vars) (interface{}, error) {
	t.Helper()
	ev, err := NewEvaluator(types.ExpressionOptions{}, loopSteps, "")
	require.NoError(t, err)
	out, err := ev.Properties(context.Background(), types.ExpressionStep{}, &runtime.RawExtension{Raw: []byte(props)}, wfVars{vars: v}, pCtx, status)
	if err != nil {
		return nil, err
	}
	return string(out.Raw), nil
}

var succeeded = wfv1alpha1.StepStatus{Phase: wfv1alpha1.WorkflowStepPhaseSucceeded}

func TestInsideAnIterationStepsReadsThatIteration(t *testing.T) {
	out, err := resolve(t, `{"x":"$(steps.fetch.outputs.data)"}`, running("per-tenant-1-greet", 1),
		map[string]wfv1alpha1.StepStatus{"per-tenant-0-fetch": succeeded, "per-tenant-1-fetch": succeeded},
		vars{"per-tenant-0-data": `"zero"`, "per-tenant-1-data": `"one"`, "data": `["zero","one"]`})
	require.NoError(t, err)
	require.JSONEq(t, `{"x":"one"}`, out.(string), "as inputs[].from scopes a body output")

	_, err = resolve(t, `{"x":"$(steps.fetch.outputs.data)"}`, running("per-tenant-1-greet", 1),
		map[string]wfv1alpha1.StepStatus{"per-tenant-0-fetch": succeeded}, vars{"per-tenant-0-data": `"zero"`})
	require.ErrorIs(t, err, cel.ErrNotReady, "another iteration finishing does not answer this one")
}

func TestOutsideTheLoopStepsReadsWhatTheLoopCollected(t *testing.T) {
	collected := vars{"msg": `["hi a","hi b"]`, "pong": `[1,2]`}
	out, err := resolve(t, `{"x":"$(steps.greet.outputs.msg.join(\", \"))","n":"$(steps.ping.outputs.pong.size())"}`,
		running("summary"), map[string]wfv1alpha1.StepStatus{"per-tenant": succeeded, "ping": succeeded}, collected)
	require.NoError(t, err)
	require.JSONEq(t, `{"x":"hi a, hi b","n":2}`, out.(string))

	_, err = resolve(t, `{"x":"$(steps.greet.outputs.msg)"}`, running("summary"),
		map[string]wfv1alpha1.StepStatus{"per-tenant": {Phase: wfv1alpha1.WorkflowStepPhaseRunning},
			"per-tenant-0-greet": succeeded}, collected)
	require.ErrorIs(t, err, cel.ErrNotReady)
	require.ErrorContains(t, err, "waiting for step per-tenant", "a body step's outputs are ready once its loop is")
}

func TestAStepCannotReadItsOwnOutputs(t *testing.T) {
	for name, pCtx := range map[string]process.Context{
		"a plain step":        running("summary"),
		"a single-step loop":  running("ping-0", 0),
		"a group's body step": running("per-tenant-0-greet", 0),
	} {
		t.Run(name, func(t *testing.T) {
			props := map[string]string{
				"a plain step":        `{"x":"$(steps.summary.outputs.y)"}`,
				"a single-step loop":  `{"x":"$(steps.ping.outputs.pong)"}`,
				"a group's body step": `{"x":"$(steps.greet.outputs.msg)"}`,
			}[name]
			_, err := resolve(t, props, pCtx, nil, vars{})
			require.Error(t, err)
			require.False(t, errors.Is(err, cel.ErrNotReady))
			require.ErrorContains(t, err, "cannot read its own outputs")
		})
	}
}

// A loop named like another's passes cannot take that loop's readers; admission
// refuses such names, and the reader still finds its own loop.
func TestTheReadersLoopIsTheLongestMatch(t *testing.T) {
	ws := newWorkflowSteps([]v1alpha1.WorkflowStep{
		{WorkflowStepBase: v1alpha1.WorkflowStepBase{Name: "a", Type: types.WorkflowStepTypeStepGroup},
			ForEach: &v1alpha1.ForEach{}, SubSteps: []v1alpha1.WorkflowStepBase{{Name: "x"}}},
		{WorkflowStepBase: v1alpha1.WorkflowStepBase{Name: "a-0-x", Type: types.WorkflowStepTypeStepGroup},
			ForEach: &v1alpha1.ForEach{}, SubSteps: []v1alpha1.WorkflowStepBase{{Name: "sub"}}},
	})
	for i := 0; i < 20; i++ {
		r := ws.readerOf(running("a-0-x-0-sub", 0))
		require.Equal(t, reader{name: "a-0-x-0-sub", written: "sub", loop: "a-0-x", index: 0}, r)
	}
}
