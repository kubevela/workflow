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
