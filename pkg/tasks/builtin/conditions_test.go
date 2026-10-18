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
	"fmt"
	"testing"

	"cuelang.org/go/cue"
	oamv1alpha1 "github.com/kubevela/pkg/apis/oam/v1alpha1"
	"github.com/kubevela/pkg/cel"
	"github.com/stretchr/testify/require"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/runtime"

	"github.com/kubevela/workflow/api/v1alpha1"
	wfContext "github.com/kubevela/workflow/pkg/context"
	"github.com/kubevela/workflow/pkg/cue/process"
	"github.com/kubevela/workflow/pkg/types"
)

// conditionEvaluator answers every condition with run and err, recording the
// step and condition it was asked.
type conditionEvaluator struct {
	run   bool
	err   error
	asked *[]string
}

func (e conditionEvaluator) Properties(_ context.Context, _ types.ExpressionStep, p *runtime.RawExtension, _ wfContext.Context,
	_ process.Context, _ map[string]v1alpha1.StepStatus) (*runtime.RawExtension, error) {
	return p, nil
}

func (e conditionEvaluator) Condition(_ context.Context, step types.ExpressionStep, cond string, _ wfContext.Context,
	_ process.Context, _ map[string]v1alpha1.StepStatus) (bool, error) {
	*e.asked = append(*e.asked, step.Name+": "+cond)
	return e.run, e.err
}

// conditionHook decides as the executor does for a $( ) if: by the condition the
// runner offers.
func conditionHook(step oamv1alpha1.WorkflowStep, options *types.PreCheckOptions) (*types.PreCheckResult, error) {
	if options.Condition == nil {
		return &types.PreCheckResult{Message: "no condition offered"}, nil
	}
	run, err := options.Condition()
	if err != nil {
		return &types.PreCheckResult{Wait: true, Message: err.Error()}, nil
	}
	return &types.PreCheckResult{Skip: !run}, nil
}

func TestAGroupsConditionDecidesIt(t *testing.T) {
	ctx := newWorkflowContextForTest(t)
	group := oamv1alpha1.WorkflowStep{WorkflowStepBase: oamv1alpha1.WorkflowStepBase{
		Name: "deploy", Type: types.WorkflowStepTypeStepGroup, If: "$(steps.build.succeeded)"}}
	run := func(t *testing.T, step oamv1alpha1.WorkflowStep, ev types.PropertyEvaluator) (v1alpha1.StepStatus, *types.Operation) {
		t.Helper()
		runner, err := StepGroup(step, &types.TaskGeneratorOptions{ID: "1", Expressions: ev,
			ProcessContext: process.NewContext(process.ContextData{})})
		require.NoError(t, err)
		status, op, err := runner.Run(ctx, &types.TaskRunOptions{
			PreCheckHooks: []types.TaskPreCheckHook{conditionHook},
			StepStatus:    map[string]v1alpha1.StepStatus{},
			Engine:        &testEngine{stepStatus: v1alpha1.WorkflowStepStatus{}, operation: &types.Operation{}},
		})
		require.NoError(t, err)
		return status, op
	}

	t.Run("false skips it", func(t *testing.T) {
		var asked []string
		status, op := run(t, group, conditionEvaluator{run: false, asked: &asked})
		require.Equal(t, v1alpha1.WorkflowStepPhaseSkipped, status.Phase)
		require.True(t, op.Skip)
		require.Equal(t, []string{"deploy: $(steps.build.succeeded)"}, asked)
	})
	t.Run("a read to be answered holds it back, running", func(t *testing.T) {
		var asked []string
		status, op := run(t, group, conditionEvaluator{err: fmt.Errorf("x: %w", cel.ErrNotReady), asked: &asked})
		require.Equal(t, v1alpha1.WorkflowStepPhaseRunning, status.Phase)
		require.Equal(t, types.StatusReasonWait, status.Reason)
		require.Contains(t, status.Message, cel.ErrNotReady.Error())
		require.True(t, op.Waiting)
	})
	t.Run("a CUE if is offered no condition", func(t *testing.T) {
		var asked []string
		cue := group
		cue.If = "status.build.succeeded"
		run(t, cue, conditionEvaluator{run: false, asked: &asked})
		require.Empty(t, asked)
	})
	t.Run("without expressions, no condition is offered", func(t *testing.T) {
		status, _ := run(t, group, nil)
		require.NotEqual(t, v1alpha1.WorkflowStepPhaseSkipped, status.Phase)
	})
}

func TestALoopsConditionDecidesItOnce(t *testing.T) {
	ctx := newWorkflowContextForTest(t)
	loop := oamv1alpha1.WorkflowStep{
		WorkflowStepBase: oamv1alpha1.WorkflowStepBase{Name: "per-region", Type: "notify", If: "$(context.name == \"prod\")"},
		ForEach:          &oamv1alpha1.ForEach{Items: &apiextensionsv1.JSON{Raw: []byte(`["eu","us"]`)}},
	}
	var asked []string
	runner, err := ForEach(loop, &types.TaskGeneratorOptions{ID: "1",
		Expressions:    conditionEvaluator{run: false, asked: &asked},
		ProcessContext: process.NewContext(process.ContextData{})})
	require.NoError(t, err)
	status, op, err := runner.Run(ctx, &types.TaskRunOptions{
		PreCheckHooks: []types.TaskPreCheckHook{conditionHook},
		StepStatus:    map[string]v1alpha1.StepStatus{},
		Engine:        &testEngine{stepStatus: v1alpha1.WorkflowStepStatus{}, operation: &types.Operation{}},
	})
	require.NoError(t, err)
	require.Equal(t, v1alpha1.WorkflowStepPhaseSkipped, status.Phase)
	require.True(t, op.Skip)
	require.Equal(t, []string{`per-region: $(context.name == "prod")`}, asked, "once, for the loop as a whole")
}

func TestATrueLoopConditionIsDecidedOnceAcrossItsPasses(t *testing.T) {
	wfCtx := newWorkflowContextForTest(t)
	f := newFixture()
	var asked []string
	loop := singleStep("scale-component", oamv1alpha1.ForEach{Items: itemsJSON(t, []string{"a", "b"})})
	loop.If = `$(context.name == "prod")`
	opts := f.options("loop-id", "")
	opts.Expressions = conditionEvaluator{run: true, asked: &asked}
	runner, err := ForEach(loop, opts)
	require.NoError(t, err)
	run := runOptions(&runningEngine{wfCtx: wfCtx})
	run.PreCheckHooks = []types.TaskPreCheckHook{conditionHook}
	_, _, err = runner.Run(wfCtx, run)
	require.NoError(t, err)
	require.Len(t, f.generated, 2, "both passes run")
	require.Equal(t, []string{`scale: $(context.name == "prod")`}, asked, "decided once, for the loop")
	for _, pass := range f.generated {
		require.Empty(t, pass.If, "a pass carries no condition of its own")
	}
}

func TestAGroupWhoseConditionErrsRunsNoSubSteps(t *testing.T) {
	ctx := newWorkflowContextForTest(t)
	group := oamv1alpha1.WorkflowStep{WorkflowStepBase: oamv1alpha1.WorkflowStepBase{
		Name: "deploy", Type: types.WorkflowStepTypeStepGroup, If: "$(steps.build.phase)"}}
	var asked []string
	runner, err := StepGroup(group, &types.TaskGeneratorOptions{ID: "1",
		Expressions:    conditionEvaluator{err: fmt.Errorf("steps.build.phase is string, but a step's if must be a bool"), asked: &asked},
		ProcessContext: process.NewContext(process.ContextData{})})
	require.NoError(t, err)
	stepStatus := map[string]v1alpha1.StepStatus{}
	failing := func(_ oamv1alpha1.WorkflowStep, options *types.PreCheckOptions) (*types.PreCheckResult, error) {
		_, err := options.Condition()
		return &types.PreCheckResult{Skip: true}, err
	}
	status, op, err := runner.Run(ctx, &types.TaskRunOptions{
		PreCheckHooks: []types.TaskPreCheckHook{failing},
		StepStatus:    stepStatus,
		Engine:        &testEngine{stepStatus: v1alpha1.WorkflowStepStatus{}, operation: &types.Operation{}},
	})
	require.NoError(t, err)
	require.Equal(t, v1alpha1.WorkflowStepPhaseSkipped, status.Phase)
	require.Contains(t, status.Message, "must be a bool")
	require.True(t, op.Skip)
	require.Equal(t, v1alpha1.WorkflowStepPhaseSkipped, stepStatus["deploy"].Phase,
		"the skip is recorded where the group's sub-steps look for it")
}

// A group whose pre-check fails it is failed, records that, and runs no sub-step.
func TestAGroupFailedByItsPreCheckRunsNoSubSteps(t *testing.T) {
	ctx := newWorkflowContextForTest(t)
	sub, err := StepGroup(oamv1alpha1.WorkflowStep{WorkflowStepBase: oamv1alpha1.WorkflowStepBase{Name: "rollout"}},
		&types.TaskGeneratorOptions{ID: "2"})
	require.NoError(t, err)
	group := oamv1alpha1.WorkflowStep{WorkflowStepBase: oamv1alpha1.WorkflowStepBase{
		Name: "deploy", Type: types.WorkflowStepTypeStepGroup, If: "$(steps.build.succeeded)"}}
	runner, err := StepGroup(group, &types.TaskGeneratorOptions{ID: "1", SubTaskRunners: []types.TaskRunner{sub},
		ProcessContext: process.NewContext(process.ContextData{})})
	require.NoError(t, err)
	stepStatus := map[string]v1alpha1.StepStatus{}
	failing := func(_ oamv1alpha1.WorkflowStep, _ *types.PreCheckOptions) (*types.PreCheckResult, error) {
		return &types.PreCheckResult{Fail: true, Message: "cannot evaluate"}, nil
	}
	engine := &testEngine{stepStatus: v1alpha1.WorkflowStepStatus{}, operation: &types.Operation{}}
	status, op, err := runner.Run(ctx, &types.TaskRunOptions{
		PreCheckHooks: []types.TaskPreCheckHook{failing},
		StepStatus:    stepStatus,
		Engine:        engine,
	})
	require.NoError(t, err)
	require.Equal(t, v1alpha1.WorkflowStepPhaseFailed, status.Phase)
	require.Equal(t, "cannot evaluate", status.Message)
	require.True(t, op.Terminated)
	require.Equal(t, v1alpha1.WorkflowStepPhaseFailed, stepStatus["deploy"].Phase)
	require.Empty(t, engine.ran, "no sub-step runs")
}

// A group with outputs that its pre-check fails keeps the pre-check's message:
// it never ran, so there is nothing to export.
func TestAGroupFailedByItsPreCheckKeepsItsMessage(t *testing.T) {
	ctx := newWorkflowContextForTest(t)
	group := oamv1alpha1.WorkflowStep{WorkflowStepBase: oamv1alpha1.WorkflowStepBase{
		Name: "deploy", Type: types.WorkflowStepTypeStepGroup, If: "$(steps.build.succeeded)",
		Outputs: oamv1alpha1.StepOutputs{{Name: "endpoint", ValueFrom: "status.endpoint"}}}}
	runner, err := StepGroup(group, &types.TaskGeneratorOptions{ID: "1", ProcessContext: process.NewContext(process.ContextData{})})
	require.NoError(t, err)
	failing := func(_ oamv1alpha1.WorkflowStep, _ *types.PreCheckOptions) (*types.PreCheckResult, error) {
		return &types.PreCheckResult{Fail: true, Message: "cannot evaluate"}, nil
	}
	exportFails := func(wfContext.Context, cue.Value, oamv1alpha1.WorkflowStep, v1alpha1.StepStatus, map[string]v1alpha1.StepStatus) error {
		return fmt.Errorf("failed to get output endpoint")
	}
	status, op, err := runner.Run(ctx, &types.TaskRunOptions{
		PreCheckHooks: []types.TaskPreCheckHook{failing},
		PostStopHooks: []types.TaskPostStopHook{exportFails},
		StepStatus:    map[string]v1alpha1.StepStatus{},
		Engine:        &testEngine{stepStatus: v1alpha1.WorkflowStepStatus{}, operation: &types.Operation{}},
	})
	require.NoError(t, err)
	require.Equal(t, v1alpha1.WorkflowStepPhaseFailed, status.Phase)
	require.Equal(t, "cannot evaluate", status.Message)
	require.True(t, op.Terminated)
}

// A group whose sub-step failed is failed by the roll-up, not by its pre-check,
// and still exports its outputs.
func TestAGroupWhoseSubStepFailedStillExportsItsOutputs(t *testing.T) {
	ctx := newWorkflowContextForTest(t)
	sub, err := StepGroup(oamv1alpha1.WorkflowStep{WorkflowStepBase: oamv1alpha1.WorkflowStepBase{Name: "rollout"}},
		&types.TaskGeneratorOptions{ID: "2"})
	require.NoError(t, err)
	group := oamv1alpha1.WorkflowStep{WorkflowStepBase: oamv1alpha1.WorkflowStepBase{
		Name: "deploy", Type: types.WorkflowStepTypeStepGroup,
		Outputs: oamv1alpha1.StepOutputs{{Name: "endpoint", ValueFrom: "status.endpoint"}}}}
	runner, err := StepGroup(group, &types.TaskGeneratorOptions{ID: "1", SubTaskRunners: []types.TaskRunner{sub},
		ProcessContext: process.NewContext(process.ContextData{})})
	require.NoError(t, err)
	exports := 0
	export := func(wfContext.Context, cue.Value, oamv1alpha1.WorkflowStep, v1alpha1.StepStatus, map[string]v1alpha1.StepStatus) error {
		exports++
		return nil
	}
	engine := &testEngine{operation: &types.Operation{}, stepStatus: v1alpha1.WorkflowStepStatus{
		SubStepsStatus: []v1alpha1.StepStatus{{Name: "rollout", Phase: v1alpha1.WorkflowStepPhaseFailed, Reason: types.StatusReasonAction}}}}
	status, _, err := runner.Run(ctx, &types.TaskRunOptions{
		PostStopHooks: []types.TaskPostStopHook{export},
		StepStatus:    map[string]v1alpha1.StepStatus{},
		Engine:        engine,
	})
	require.NoError(t, err)
	require.Equal(t, v1alpha1.WorkflowStepPhaseFailed, status.Phase)
	require.Equal(t, 1, exports, "the outputs are exported")
}
