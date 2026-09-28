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

package builtin

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"cuelang.org/go/cue/cuecontext"
	"github.com/stretchr/testify/require"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/runtime"

	monitorContext "github.com/kubevela/pkg/monitor/context"
	"github.com/kubevela/workflow/api/v1alpha1"
	wfContext "github.com/kubevela/workflow/pkg/context"
	"github.com/kubevela/workflow/pkg/cue/model"
	"github.com/kubevela/workflow/pkg/cue/process"
	"github.com/kubevela/workflow/pkg/types"

	oamv1alpha1 "github.com/kubevela/pkg/apis/oam/v1alpha1"
)

func rawJSON(t *testing.T, v any) *runtime.RawExtension {
	b, err := json.Marshal(v)
	require.NoError(t, err)
	return &runtime.RawExtension{Raw: b}
}

func itemsJSON(t *testing.T, v any) *apiextensionsv1.JSON {
	b, err := json.Marshal(v)
	require.NoError(t, err)
	return &apiextensionsv1.JSON{Raw: b}
}

func TestExpandSingleStep(t *testing.T) {
	r := require.New(t)
	step := oamv1alpha1.WorkflowStep{
		WorkflowStepBase: oamv1alpha1.WorkflowStepBase{
			Name:       "scale",
			Type:       "scale-component",
			If:         "status.a.succeeded",
			Timeout:    "1m",
			DependsOn:  []string{"a"},
			Properties: rawJSON(t, map[string]any{"component": "web"}),
			Inputs: oamv1alpha1.StepInputs{
				{From: "loop.item.cluster", ParameterKey: "cluster"},
				{From: "token", ParameterKey: "token"},
			},
			Outputs: oamv1alpha1.StepOutputs{{Name: "endpoint", ValueFrom: "output.value"}},
		},
		ForEach: &oamv1alpha1.ForEach{},
	}

	iterations, err := expandIterations(step, []any{map[string]any{"cluster": "c1"}, map[string]any{"cluster": "c2"}})
	r.NoError(err)
	r.Len(iterations, 2)
	r.Len(iterations[1].steps, 1)
	r.Equal(oamv1alpha1.WorkflowStepBase{
		Name:       "scale-1",
		Type:       "scale-component",
		Properties: rawJSON(t, map[string]any{"component": "web"}),
		Inputs: oamv1alpha1.StepInputs{
			{From: "scale-1-loop.item.cluster", ParameterKey: "cluster"},
			{From: "token", ParameterKey: "token"},
		},
		Outputs: oamv1alpha1.StepOutputs{{Name: "scale-1-endpoint", ValueFrom: "output.value"}},
	}, iterations[1].steps[0], "if, timeout and dependsOn stay on the loop")
	r.Equal("scale-1-loop", iterations[1].loopVar)

	// the step itself is left untouched
	r.Equal("loop.item.cluster", step.Inputs[0].From)
	r.Equal("endpoint", step.Outputs[0].Name)
}

func TestExpandGroup(t *testing.T) {
	r := require.New(t)
	step := oamv1alpha1.WorkflowStep{
		WorkflowStepBase: oamv1alpha1.WorkflowStepBase{Name: "rollout", Type: types.WorkflowStepTypeStepGroup},
		ForEach:          &oamv1alpha1.ForEach{},
		SubSteps: []oamv1alpha1.WorkflowStepBase{
			{
				Name:    "deploy",
				Type:    "deploy",
				Inputs:  oamv1alpha1.StepInputs{{From: "loop.item", ParameterKey: "region"}},
				Outputs: oamv1alpha1.StepOutputs{{Name: "endpoint", ValueFrom: "output.value"}},
			},
			{
				Name:      "verify",
				Type:      "check",
				DependsOn: []string{"deploy", "outside"},
				Inputs: oamv1alpha1.StepInputs{
					{From: "endpoint.host", ParameterKey: "host"},
					{From: "loop.index", ParameterKey: "wave"},
				},
			},
		},
	}

	iterations, err := expandIterations(step, []any{"east", "west"})
	r.NoError(err)
	deploy, verify := iterations[1].steps[0], iterations[1].steps[1]
	r.Equal("rollout-1-deploy", deploy.Name)
	r.Equal(oamv1alpha1.StepInputs{{From: "rollout-1-loop.item", ParameterKey: "region"}}, deploy.Inputs)
	r.Equal("rollout-1-endpoint", deploy.Outputs[0].Name)
	r.Equal("rollout-1-verify", verify.Name)
	r.Equal([]string{"rollout-1-deploy", "outside"}, verify.DependsOn)
	r.Equal(oamv1alpha1.StepInputs{
		{From: "rollout-1-endpoint.host", ParameterKey: "host"},
		{From: "rollout-1-loop.index", ParameterKey: "wave"},
	}, verify.Inputs)

	r.Equal("deploy", step.SubSteps[0].Name)
	r.Equal([]string{"deploy", "outside"}, step.SubSteps[1].DependsOn)
}

func TestExpandRejectsOutputNamedLoop(t *testing.T) {
	step := oamv1alpha1.WorkflowStep{
		WorkflowStepBase: oamv1alpha1.WorkflowStepBase{
			Name:    "s",
			Type:    "t",
			Outputs: oamv1alpha1.StepOutputs{{Name: "loop", ValueFrom: "v"}},
		},
		ForEach: &oamv1alpha1.ForEach{},
	}
	_, err := expandIterations(step, []any{1})
	require.ErrorContains(t, err, "loop")
}

// runningEngine runs each runner in turn and records its status as a sub-step.
type runningEngine struct {
	wfCtx     wfContext.Context
	statuses  []v1alpha1.StepStatus
	dags      []bool
	parent    string
	dependsOn map[string][]string
}

func (e *runningEngine) Run(_ monitorContext.Context, runners []types.TaskRunner, dag bool) error {
	e.dags = append(e.dags, dag)
	for _, runner := range runners {
		if e.finished(runner.Name()) {
			continue
		}
		status, _, err := runner.Run(e.wfCtx, &types.TaskRunOptions{})
		if err != nil {
			return err
		}
		e.record(status)
		if !dag && !types.IsStepFinish(status.Phase, status.Reason) {
			return nil
		}
	}
	return nil
}

func (e *runningEngine) finished(name string) bool {
	for _, s := range e.statuses {
		if s.Name == name {
			return types.IsStepFinish(s.Phase, s.Reason)
		}
	}
	return false
}

func (e *runningEngine) record(status v1alpha1.StepStatus) {
	for i, s := range e.statuses {
		if s.Name == status.Name {
			e.statuses[i] = status
			return
		}
	}
	e.statuses = append(e.statuses, status)
}

func (e *runningEngine) GetStepStatus(string) v1alpha1.WorkflowStepStatus {
	return v1alpha1.WorkflowStepStatus{SubStepsStatus: e.statuses}
}

func (e *runningEngine) GetCommonStepStatus(string) v1alpha1.StepStatus { return v1alpha1.StepStatus{} }

func (e *runningEngine) SetParentRunner(name string) { e.parent = name }

func (e *runningEngine) GetOperation() *types.Operation { return &types.Operation{} }

func (e *runningEngine) SetDependsOn(name string, dependsOn []string) {
	if e.dependsOn == nil {
		e.dependsOn = map[string][]string{}
	}
	e.dependsOn[name] = dependsOn
}

// fakeRunner runs, terminates or errors as its type says, records the loop context it
// saw, and sets each of its outputs to the current loop item, or to an incomplete value
// for the type "open".
type fakeRunner struct {
	step oamv1alpha1.WorkflowStepBase
	pCtx process.Context
	seen *[]any
}

func (r *fakeRunner) Name() string { return r.step.Name }

func (r *fakeRunner) Pending(monitorContext.Context, wfContext.Context, map[string]v1alpha1.StepStatus) (bool, v1alpha1.StepStatus) {
	*r.seen = append(*r.seen, r.pCtx.GetData(model.ContextLoop))
	return false, v1alpha1.StepStatus{}
}

func (r *fakeRunner) Run(ctx wfContext.Context, _ *types.TaskRunOptions) (v1alpha1.StepStatus, *types.Operation, error) {
	loop := r.pCtx.GetData(model.ContextLoop)
	*r.seen = append(*r.seen, loop)
	if r.step.Type == "error" {
		return v1alpha1.StepStatus{}, nil, errors.New("engine failure")
	}
	b, err := json.Marshal(loop.(map[string]any)["item"])
	if err != nil {
		return v1alpha1.StepStatus{}, nil, err
	}
	if r.step.Type == "open" {
		b = []byte("string")
	}
	for _, o := range r.step.Outputs {
		if err := ctx.SetVar(cuecontext.New().CompileBytes(b), o.Name); err != nil {
			return v1alpha1.StepStatus{}, nil, err
		}
	}
	status := v1alpha1.StepStatus{Name: r.step.Name, Type: r.step.Type, Phase: v1alpha1.WorkflowStepPhaseSucceeded}
	switch r.step.Type {
	case "running":
		status.Phase = v1alpha1.WorkflowStepPhaseRunning
	case "terminate":
		status.Phase, status.Reason = v1alpha1.WorkflowStepPhaseFailed, types.StatusReasonTerminate
	}
	return status, &types.Operation{}, nil
}

func (r *fakeRunner) FillContextData(monitorContext.Context, process.Context) types.ContextDataResetter {
	return func(process.Context) {}
}

type forEachFixture struct {
	pCtx      process.Context
	seen      []any
	generated []oamv1alpha1.WorkflowStepBase
}

func newFixture() *forEachFixture {
	return &forEachFixture{pCtx: process.NewContext(process.ContextData{})}
}

func (f *forEachFixture) options(id string, bodyMode oamv1alpha1.WorkflowMode) *types.TaskGeneratorOptions {
	return &types.TaskGeneratorOptions{
		ID:                 id,
		ProcessContext:     f.pCtx,
		SubStepExecuteMode: bodyMode,
		SubTaskGenerator: func(step oamv1alpha1.WorkflowStepBase, _ string) (types.TaskRunner, error) {
			f.generated = append(f.generated, step)
			return &fakeRunner{step: step, pCtx: f.pCtx, seen: &f.seen}, nil
		},
	}
}

func (f *forEachFixture) names() []string {
	var names []string
	for _, s := range f.generated {
		names = append(names, s.Name)
	}
	return names
}

func runOptions(e types.Engine) *types.TaskRunOptions {
	return &types.TaskRunOptions{Engine: e, StepStatus: map[string]v1alpha1.StepStatus{}}
}

func singleStep(typ string, forEach oamv1alpha1.ForEach) oamv1alpha1.WorkflowStep {
	return oamv1alpha1.WorkflowStep{
		WorkflowStepBase: oamv1alpha1.WorkflowStepBase{
			Name:    "scale",
			Type:    typ,
			Outputs: oamv1alpha1.StepOutputs{{Name: "endpoint", ValueFrom: "output.value"}},
		},
		ForEach: &forEach,
	}
}

func TestForEachLiteralItems(t *testing.T) {
	r := require.New(t)
	wfCtx := newWorkflowContextForTest(t)
	f := newFixture()
	runner, err := ForEach(singleStep("scale-component", oamv1alpha1.ForEach{Items: itemsJSON(t, []string{"a", "b"})}), f.options("loop-id", ""))
	r.NoError(err)
	r.Equal("scale", runner.Name())

	e := &runningEngine{wfCtx: wfCtx}
	status, _, err := runner.Run(wfCtx, runOptions(e))
	r.NoError(err)
	r.Equal(v1alpha1.WorkflowStepPhaseSucceeded, status.Phase)
	r.Equal("scale-component", status.Type)
	r.Empty(e.parent, "parent runner is reset once the iterations have run")
	r.Equal([]string{"scale-0", "scale-1"}, f.names())

	r.Equal([]any{
		map[string]any{"item": "a", "index": 0},
		map[string]any{"item": "b", "index": 1},
	}, f.seen)
	r.Nil(f.pCtx.GetData(model.ContextLoop), "loop context is removed after the iteration runs")

	loopVar, err := wfCtx.GetVar("scale-1-loop")
	r.NoError(err)
	got, err := loopVar.MarshalJSON()
	r.NoError(err)
	r.JSONEq(`{"item": "b", "index": 1}`, string(got))

	collected, err := wfCtx.GetVar("endpoint")
	r.NoError(err)
	var endpoints []string
	r.NoError(collected.Decode(&endpoints))
	r.Equal([]string{"a", "b"}, endpoints)
}

func TestForEachItemsFromVarArePinned(t *testing.T) {
	r := require.New(t)
	wfCtx := newWorkflowContextForTest(t)
	r.NoError(wfCtx.SetVar(cuecontext.New().CompileString(`{names: ["x", "y", "z"]}`), "regions"))
	f := newFixture()
	runner, err := ForEach(singleStep("t", oamv1alpha1.ForEach{From: "regions.names"}), f.options("loop-id", ""))
	r.NoError(err)
	_, _, err = runner.Run(wfCtx, runOptions(&runningEngine{wfCtx: wfCtx}))
	r.NoError(err)
	r.Len(f.generated, 3)

	// a later reconcile resolves to a different list, but the pinned one is kept
	r.NoError(wfCtx.SetVar(cuecontext.New().CompileString(`["only"]`), "other"))
	runner, err = ForEach(singleStep("t", oamv1alpha1.ForEach{From: "other"}), f.options("loop-id", ""))
	r.NoError(err)
	f.generated = nil
	_, _, err = runner.Run(wfCtx, runOptions(&runningEngine{wfCtx: wfCtx}))
	r.NoError(err)
	r.Equal([]string{"scale-0", "scale-1", "scale-2"}, f.names())
}

func TestForEachPendingOnFrom(t *testing.T) {
	r := require.New(t)
	wfCtx := newWorkflowContextForTest(t)
	f := newFixture()
	runner, err := ForEach(singleStep("t", oamv1alpha1.ForEach{From: "regions"}), f.options("loop-id", ""))
	r.NoError(err)
	ctx := monitorContext.NewTraceContext(context.Background(), "")

	pending, status := runner.Pending(ctx, wfCtx, map[string]v1alpha1.StepStatus{})
	r.True(pending)
	r.Contains(status.Message, "regions")

	r.NoError(wfCtx.SetVar(cuecontext.New().CompileString(`["a"]`), "regions"))
	pending, _ = runner.Pending(ctx, wfCtx, map[string]v1alpha1.StepStatus{})
	r.False(pending)
}

func TestForEachNotPendingOnLoopInputs(t *testing.T) {
	r := require.New(t)
	wfCtx := newWorkflowContextForTest(t)
	f := newFixture()
	step := singleStep("t", oamv1alpha1.ForEach{Items: itemsJSON(t, []string{"a"})})
	step.Inputs = oamv1alpha1.StepInputs{{From: "loop.item", ParameterKey: "region"}}
	runner, err := ForEach(step, f.options("loop-id", ""))
	r.NoError(err)
	pending, _ := runner.Pending(monitorContext.NewTraceContext(context.Background(), ""), wfCtx, map[string]v1alpha1.StepStatus{})
	r.False(pending, "inputs belong to the iterations, not the loop")
}

func TestForEachEmptyItemsSucceeds(t *testing.T) {
	r := require.New(t)
	wfCtx := newWorkflowContextForTest(t)
	f := newFixture()
	runner, err := ForEach(singleStep("t", oamv1alpha1.ForEach{Items: itemsJSON(t, []string{})}), f.options("loop-id", ""))
	r.NoError(err)
	status, _, err := runner.Run(wfCtx, runOptions(&runningEngine{wfCtx: wfCtx}))
	r.NoError(err)
	r.Equal(v1alpha1.WorkflowStepPhaseSucceeded, status.Phase)
	r.Empty(f.generated)
}

func TestForEachInvalidItems(t *testing.T) {
	cases := map[string]struct {
		forEach oamv1alpha1.ForEach
		message string
	}{
		"not a list":         {forEach: oamv1alpha1.ForEach{Items: itemsJSON(t, map[string]any{"a": 1})}, message: "must be a list"},
		"unresolved expr":    {forEach: oamv1alpha1.ForEach{Items: itemsJSON(t, "$(source.inventory.clusters)")}, message: "EnableCelExpressions"},
		"neither":            {forEach: oamv1alpha1.ForEach{}, message: "one of"},
		"both":               {forEach: oamv1alpha1.ForEach{Items: itemsJSON(t, []int{1}), From: "x"}, message: "one of"},
		"from is not a list": {forEach: oamv1alpha1.ForEach{From: "scalar"}, message: "must be a list"},
		"over the limit":     {forEach: oamv1alpha1.ForEach{Items: itemsJSON(t, make([]int, types.MaxForEachItems+1))}, message: "over the limit"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			r := require.New(t)
			wfCtx := newWorkflowContextForTest(t)
			r.NoError(wfCtx.SetVar(cuecontext.New().CompileString(`"s"`), "scalar"))
			f := newFixture()
			runner, err := ForEach(singleStep("t", tc.forEach), f.options("loop-id", ""))
			r.NoError(err)
			status, operation, err := runner.Run(wfCtx, runOptions(&runningEngine{wfCtx: wfCtx}))
			r.NoError(err)
			r.Equal(v1alpha1.WorkflowStepPhaseFailed, status.Phase)
			r.Equal(types.StatusReasonParameter, status.Reason)
			r.Contains(status.Message, tc.message)
			r.True(operation.Terminated)
			r.Empty(f.generated)
		})
	}
}

func TestForEachStepModeWaitsForEachIteration(t *testing.T) {
	r := require.New(t)
	wfCtx := newWorkflowContextForTest(t)
	f := newFixture()
	runner, err := ForEach(singleStep("running", oamv1alpha1.ForEach{Items: itemsJSON(t, []string{"a", "b"})}), f.options("loop-id", ""))
	r.NoError(err)
	e := &runningEngine{wfCtx: wfCtx}
	status, _, err := runner.Run(wfCtx, runOptions(e))
	r.NoError(err)
	r.Equal(v1alpha1.WorkflowStepPhaseRunning, status.Phase)
	r.Len(e.statuses, 1)
	r.Equal("scale-0", e.statuses[0].Name)
}

func TestForEachDagModeRunsEveryIteration(t *testing.T) {
	r := require.New(t)
	wfCtx := newWorkflowContextForTest(t)
	f := newFixture()
	runner, err := ForEach(singleStep("running", oamv1alpha1.ForEach{Items: itemsJSON(t, []string{"a", "b"}), Mode: v1alpha1.WorkflowModeDAG}), f.options("loop-id", ""))
	r.NoError(err)
	e := &runningEngine{wfCtx: wfCtx}
	_, _, err = runner.Run(wfCtx, runOptions(e))
	r.NoError(err)
	r.Len(e.statuses, 2)
}

func TestForEachStepModeSkipsAfterAFailure(t *testing.T) {
	r := require.New(t)
	wfCtx := newWorkflowContextForTest(t)
	f := newFixture()
	runner, err := ForEach(singleStep("terminate", oamv1alpha1.ForEach{Items: itemsJSON(t, []string{"a", "b", "c"})}), f.options("loop-id", ""))
	r.NoError(err)
	e := &runningEngine{wfCtx: wfCtx}
	status, _, err := runner.Run(wfCtx, runOptions(e))
	r.NoError(err)
	r.Equal(v1alpha1.WorkflowStepPhaseFailed, status.Phase)
	r.Len(e.statuses, 3)
	r.Equal(v1alpha1.WorkflowStepPhaseFailed, e.statuses[0].Phase)
	r.Equal(v1alpha1.WorkflowStepPhaseSkipped, e.statuses[1].Phase)
	r.Equal(v1alpha1.WorkflowStepPhaseSkipped, e.statuses[2].Phase)
	r.Len(f.seen, 1, "skipped iterations never run their step")
}

func TestForEachGroupUsesItsModeWithinAnIteration(t *testing.T) {
	r := require.New(t)
	wfCtx := newWorkflowContextForTest(t)
	f := newFixture()
	step := oamv1alpha1.WorkflowStep{
		WorkflowStepBase: oamv1alpha1.WorkflowStepBase{Name: "rollout", Type: types.WorkflowStepTypeStepGroup},
		ForEach:          &oamv1alpha1.ForEach{Items: itemsJSON(t, []string{"a", "b"}), Mode: v1alpha1.WorkflowModeDAG},
		SubSteps: []oamv1alpha1.WorkflowStepBase{
			{Name: "deploy", Type: "deploy"},
			{Name: "verify", Type: "check", DependsOn: []string{"deploy"}},
		},
	}
	runner, err := ForEach(step, f.options("loop-id", v1alpha1.WorkflowModeStep))
	r.NoError(err)
	e := &runningEngine{wfCtx: wfCtx}
	status, _, err := runner.Run(wfCtx, runOptions(e))
	r.NoError(err)
	r.Equal(v1alpha1.WorkflowStepPhaseSucceeded, status.Phase)
	r.Equal(types.WorkflowStepTypeStepGroup, status.Type)
	r.Equal([]bool{false, false}, e.dags, "one engine pass per iteration, in the group's mode")
	r.Equal([]string{"rollout-1-deploy"}, e.dependsOn["rollout-1-verify"])
}

func TestIterationTemplate(t *testing.T) {
	single := oamv1alpha1.WorkflowStep{
		WorkflowStepBase: oamv1alpha1.WorkflowStepBase{Name: "scale", Type: "t", Timeout: "1m"},
		ForEach:          &oamv1alpha1.ForEach{},
	}
	group := oamv1alpha1.WorkflowStep{
		WorkflowStepBase: oamv1alpha1.WorkflowStepBase{Name: "rollout", Type: types.WorkflowStepTypeStepGroup},
		ForEach:          &oamv1alpha1.ForEach{},
		SubSteps: []oamv1alpha1.WorkflowStepBase{
			{Name: "deploy", Type: "d", Timeout: "2m"},
			{Name: "verify-all", Type: "v"},
		},
	}
	cases := map[string]struct {
		step    oamv1alpha1.WorkflowStep
		name    string
		want    string
		timeout string
	}{
		"a single step's pass":        {step: single, name: "scale-3", want: "scale"},
		"a group's pass":              {step: group, name: "rollout-12-deploy", want: "deploy", timeout: "2m"},
		"a sub-step with a hyphen":    {step: group, name: "rollout-0-verify-all", want: "verify-all"},
		"not a number":                {step: single, name: "scale-x"},
		"a single step with a suffix": {step: single, name: "scale-3-extra"},
		"an unknown sub-step":         {step: group, name: "rollout-0-nope"},
		"another step's name":         {step: single, name: "scaler-0"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got, ok := IterationTemplate(tc.step, tc.name)
			require.Equal(t, tc.want != "", ok)
			require.Equal(t, tc.want, got.Name)
			require.Equal(t, tc.timeout, got.Timeout, "a single step's timeout belongs to the loop")
		})
	}
}

func TestForEachDoesNotRebuildFinishedIterations(t *testing.T) {
	r := require.New(t)
	wfCtx := newWorkflowContextForTest(t)
	f := newFixture()
	step := singleStep("t", oamv1alpha1.ForEach{Items: itemsJSON(t, []string{"a", "b", "c"})})
	e := &runningEngine{wfCtx: wfCtx}

	runner, err := ForEach(step, f.options("loop-id", ""))
	r.NoError(err)
	_, _, err = runner.Run(wfCtx, runOptions(e))
	r.NoError(err)
	r.Len(f.generated, 3)

	// the next reconcile: every pass has finished, so none is built again
	f.generated = nil
	runner, err = ForEach(step, f.options("loop-id", ""))
	r.NoError(err)
	status, _, err := runner.Run(wfCtx, runOptions(e))
	r.NoError(err)
	r.Equal(v1alpha1.WorkflowStepPhaseSucceeded, status.Phase)
	r.Empty(f.generated)
}

func TestForEachGroupBodyDefaultsToDAG(t *testing.T) {
	r := require.New(t)
	wfCtx := newWorkflowContextForTest(t)
	f := newFixture()
	step := oamv1alpha1.WorkflowStep{
		WorkflowStepBase: oamv1alpha1.WorkflowStepBase{Name: "rollout", Type: types.WorkflowStepTypeStepGroup},
		ForEach:          &oamv1alpha1.ForEach{Items: itemsJSON(t, []string{"a"})},
		SubSteps:         []oamv1alpha1.WorkflowStepBase{{Name: "deploy", Type: "deploy"}},
	}
	runner, err := ForEach(step, f.options("loop-id", ""))
	r.NoError(err)
	e := &runningEngine{wfCtx: wfCtx}
	_, _, err = runner.Run(wfCtx, runOptions(e))
	r.NoError(err)
	r.Equal([]bool{true}, e.dags, "an unset mode runs sub-steps as DAG, as a plain step-group does")
}

func TestForEachFromNullFails(t *testing.T) {
	r := require.New(t)
	wfCtx := newWorkflowContextForTest(t)
	r.NoError(wfCtx.SetVar(cuecontext.New().CompileString(`{clusters: null}`), "prior"))
	f := newFixture()
	runner, err := ForEach(singleStep("t", oamv1alpha1.ForEach{From: "prior.clusters"}), f.options("loop-id", ""))
	r.NoError(err)
	status, _, err := runner.Run(wfCtx, runOptions(&runningEngine{wfCtx: wfCtx}))
	r.NoError(err)
	r.Equal(v1alpha1.WorkflowStepPhaseFailed, status.Phase)
	r.Contains(status.Message, "must be a list")
}

func TestForEachGroupWaitsOnItsInputs(t *testing.T) {
	r := require.New(t)
	wfCtx := newWorkflowContextForTest(t)
	f := newFixture()
	step := oamv1alpha1.WorkflowStep{
		WorkflowStepBase: oamv1alpha1.WorkflowStepBase{
			Name:   "rollout",
			Type:   types.WorkflowStepTypeStepGroup,
			Inputs: oamv1alpha1.StepInputs{{From: "approval"}},
		},
		ForEach:  &oamv1alpha1.ForEach{Items: itemsJSON(t, []string{"a"})},
		SubSteps: []oamv1alpha1.WorkflowStepBase{{Name: "deploy", Type: "deploy"}},
	}
	runner, err := ForEach(step, f.options("loop-id", ""))
	r.NoError(err)
	pending, _ := runner.Pending(monitorContext.NewTraceContext(context.Background(), ""), wfCtx, map[string]v1alpha1.StepStatus{})
	r.True(pending, "a group's own inputs are waited on, as for a plain step-group")
}

func TestForEachTimedOutStartsNoPass(t *testing.T) {
	r := require.New(t)
	wfCtx := newWorkflowContextForTest(t)
	f := newFixture()
	runner, err := ForEach(singleStep("t", oamv1alpha1.ForEach{Items: itemsJSON(t, []string{"a", "b"})}), f.options("loop-id", ""))
	r.NoError(err)
	options := runOptions(&runningEngine{wfCtx: wfCtx})
	options.PreCheckHooks = []types.TaskPreCheckHook{func(oamv1alpha1.WorkflowStep, *types.PreCheckOptions) (*types.PreCheckResult, error) {
		return &types.PreCheckResult{Timeout: true}, nil
	}}
	status, operation, err := runner.Run(wfCtx, options)
	r.NoError(err)
	r.Equal(v1alpha1.WorkflowStepPhaseFailed, status.Phase)
	r.Equal(types.StatusReasonTimeout, status.Reason)
	r.True(operation.Terminated)
	r.Empty(f.generated, "a loop past its timeout starts no pass")
}

func TestForEachPreChecks(t *testing.T) {
	cases := map[string]struct {
		hook    types.TaskPreCheckHook
		message string
	}{
		"skipped by its if": {
			hook: func(oamv1alpha1.WorkflowStep, *types.PreCheckOptions) (*types.PreCheckResult, error) {
				return &types.PreCheckResult{Skip: true}, nil
			},
		},
		"skipped when the check errors": {
			hook: func(oamv1alpha1.WorkflowStep, *types.PreCheckOptions) (*types.PreCheckResult, error) {
				return nil, errors.New("bad if")
			},
			message: "pre check error: bad if",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			r := require.New(t)
			wfCtx := newWorkflowContextForTest(t)
			f := newFixture()
			runner, err := ForEach(singleStep("t", oamv1alpha1.ForEach{Items: itemsJSON(t, []string{"a"})}), f.options("loop-id", ""))
			r.NoError(err)
			options := runOptions(&runningEngine{wfCtx: wfCtx})
			options.PreCheckHooks = []types.TaskPreCheckHook{tc.hook}
			status, operation, err := runner.Run(wfCtx, options)
			r.NoError(err)
			r.Equal(v1alpha1.WorkflowStepPhaseSkipped, status.Phase)
			r.Equal(types.StatusReasonSkip, status.Reason)
			r.Equal(tc.message, status.Message)
			r.True(operation.Skip)
			r.Empty(f.generated, "a skipped loop starts no pass")
		})
	}
}

func TestForEachRunErrors(t *testing.T) {
	r := require.New(t)
	wfCtx := newWorkflowContextForTest(t)
	f := newFixture()

	options := f.options("loop-id", "")
	options.SubTaskGenerator = nil
	runner, err := ForEach(singleStep("t", oamv1alpha1.ForEach{Items: itemsJSON(t, []string{"a"})}), options)
	r.NoError(err)
	_, _, err = runner.Run(wfCtx, runOptions(&runningEngine{wfCtx: wfCtx}))
	r.ErrorContains(err, "no sub-task generator")

	options = f.options("loop-id", "")
	options.SubTaskGenerator = func(oamv1alpha1.WorkflowStepBase, string) (types.TaskRunner, error) {
		return nil, errors.New("unknown type")
	}
	runner, err = ForEach(singleStep("t", oamv1alpha1.ForEach{Items: itemsJSON(t, []string{"a"})}), options)
	r.NoError(err)
	_, _, err = runner.Run(wfCtx, runOptions(&runningEngine{wfCtx: wfCtx}))
	r.ErrorContains(err, "generate iteration 0 of forEach step scale: unknown type")

	runner, err = ForEach(singleStep("error", oamv1alpha1.ForEach{Items: itemsJSON(t, []string{"a"})}), f.options("loop-id", ""))
	r.NoError(err)
	status, _, err := runner.Run(wfCtx, runOptions(&runningEngine{wfCtx: wfCtx}))
	r.ErrorContains(err, "engine failure")
	r.Equal(v1alpha1.WorkflowStepPhaseRunning, status.Phase, "an engine error leaves the loop running, to be retried")
}

func TestForEachBodyOutputNamedLoopFails(t *testing.T) {
	r := require.New(t)
	wfCtx := newWorkflowContextForTest(t)
	f := newFixture()
	step := singleStep("t", oamv1alpha1.ForEach{Items: itemsJSON(t, []string{"a"})})
	step.Outputs = oamv1alpha1.StepOutputs{{Name: "loop", ValueFrom: "output.value"}}
	runner, err := ForEach(step, f.options("loop-id", ""))
	r.NoError(err)
	status, operation, err := runner.Run(wfCtx, runOptions(&runningEngine{wfCtx: wfCtx}))
	r.NoError(err)
	r.Equal(v1alpha1.WorkflowStepPhaseFailed, status.Phase)
	r.Equal(types.StatusReasonParameter, status.Reason)
	r.True(operation.Terminated)
}

func TestForEachUnreadableItems(t *testing.T) {
	cases := map[string]struct {
		forEach oamv1alpha1.ForEach
		pinned  string
		message string
	}{
		"corrupt pinned list": {forEach: oamv1alpha1.ForEach{Items: itemsJSON(t, []string{"a"})}, pinned: "{", message: "decode pinned items"},
		"from is missing":     {forEach: oamv1alpha1.ForEach{From: "absent"}, message: "read forEach.from absent"},
		"from is incomplete":  {forEach: oamv1alpha1.ForEach{From: "open"}, message: "evaluate forEach.from open"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			r := require.New(t)
			wfCtx := newWorkflowContextForTest(t)
			r.NoError(wfCtx.SetVar(cuecontext.New().CompileString(`string`), "open"))
			if tc.pinned != "" {
				wfCtx.SetMutableValue(tc.pinned, PinnedItemsKey("loop-id"))
			}
			f := newFixture()
			runner, err := ForEach(singleStep("t", tc.forEach), f.options("loop-id", ""))
			r.NoError(err)
			status, operation, err := runner.Run(wfCtx, runOptions(&runningEngine{wfCtx: wfCtx}))
			r.NoError(err)
			r.Equal(v1alpha1.WorkflowStepPhaseFailed, status.Phase)
			r.Equal(types.StatusReasonParameter, status.Reason)
			r.Contains(status.Message, tc.message)
			r.True(operation.Terminated)
		})
	}
}

func TestForEachIncompleteOutputFails(t *testing.T) {
	r := require.New(t)
	wfCtx := newWorkflowContextForTest(t)
	f := newFixture()
	runner, err := ForEach(singleStep("open", oamv1alpha1.ForEach{Items: itemsJSON(t, []string{"a"})}), f.options("loop-id", ""))
	r.NoError(err)
	status, operation, err := runner.Run(wfCtx, runOptions(&runningEngine{wfCtx: wfCtx}))
	r.NoError(err)
	r.Equal(v1alpha1.WorkflowStepPhaseFailed, status.Phase)
	r.Equal(types.StatusReasonOutput, status.Reason)
	r.Contains(status.Message, "read output endpoint of iteration 0")
	r.True(operation.Terminated)
}

func TestIterationRunnerPendingSeesTheLoop(t *testing.T) {
	r := require.New(t)
	f := newFixture()
	inner := &fakeRunner{step: oamv1alpha1.WorkflowStepBase{Name: "scale-1"}, pCtx: f.pCtx, seen: &f.seen}
	runner := &iterationRunner{TaskRunner: inner, pCtx: f.pCtx, item: "b", index: 1}
	pending, _ := runner.Pending(monitorContext.NewTraceContext(context.Background(), ""), nil, nil)
	r.False(pending)
	r.Equal([]any{map[string]any{"item": "b", "index": 1}}, f.seen)
	r.Nil(f.pCtx.GetData(model.ContextLoop), "loop context is removed once the check returns")
}

func TestSkippedRunner(t *testing.T) {
	r := require.New(t)
	runner := &skippedRunner{step: oamv1alpha1.WorkflowStepBase{Name: "scale-1", Type: "t"}, id: "id"}
	r.Equal("scale-1", runner.Name())
	pending, _ := runner.Pending(monitorContext.NewTraceContext(context.Background(), ""), nil, nil)
	r.False(pending, "a skip waits on nothing")
	runner.FillContextData(nil, nil)(nil)
	status, operation, err := runner.Run(nil, nil)
	r.NoError(err)
	r.Equal(v1alpha1.StepStatus{ID: "id", Name: "scale-1", Type: "t", Phase: v1alpha1.WorkflowStepPhaseSkipped, Reason: types.StatusReasonSkip}, status)
	r.True(operation.Skip)
}

func TestIterationNames(t *testing.T) {
	r := require.New(t)
	single := singleStep("t", oamv1alpha1.ForEach{From: "x"})
	group := oamv1alpha1.WorkflowStep{
		WorkflowStepBase: oamv1alpha1.WorkflowStepBase{Name: "rollout", Type: types.WorkflowStepTypeStepGroup},
		ForEach:          &oamv1alpha1.ForEach{From: "x"},
		SubSteps: []oamv1alpha1.WorkflowStepBase{
			{Name: "deploy", Type: "t"},
			{Name: "verify", Type: "t", Outputs: oamv1alpha1.StepOutputs{{Name: "health", ValueFrom: "v"}}},
		},
	}

	for name, want := range map[string]int{"scale-0": 0, "scale-12": 12} {
		index, ok := IterationIndex(single, name)
		r.True(ok, name)
		r.Equal(want, index, name)
	}
	for _, name := range []string{"scale", "scaler-0", "scale-01", "scale-x", "scale-0-deploy"} {
		_, ok := IterationIndex(single, name)
		r.False(ok, name)
	}
	index, ok := IterationIndex(group, "rollout-3-verify")
	r.True(ok)
	r.Equal(3, index)
	_, ok = IterationIndex(group, "rollout-3-other")
	r.False(ok, "a name the body does not hold")

	index, ok = IterationVarIndex(single, "scale-2-endpoint")
	r.True(ok)
	r.Equal(2, index)
	_, ok = IterationVarIndex(single, "scale-2")
	r.False(ok, "a step's own name is not one of its variables")

	r.Equal([]string{"endpoint"}, CollectedOutputs(single))
	r.Equal([]string{"health"}, CollectedOutputs(group))

	r.True(IsPinnedItemsKey(PinnedItemsKey("id")))
	r.False(IsPinnedItemsKey("id.other"))
}
