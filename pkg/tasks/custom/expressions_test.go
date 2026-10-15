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

package custom

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"cuelang.org/go/cue"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime"

	oamv1alpha1 "github.com/kubevela/pkg/apis/oam/v1alpha1"

	"github.com/kubevela/workflow/api/v1alpha1"
	wfContext "github.com/kubevela/workflow/pkg/context"
	"github.com/kubevela/workflow/pkg/cue/process"
	"github.com/kubevela/workflow/pkg/expressions"
	"github.com/kubevela/workflow/pkg/providers"
	"github.com/kubevela/workflow/pkg/types"
)

// echoTemplate renders the step's msg parameter as its result.
func echoTemplate(_ context.Context, _ string) (string, error) {
	return "parameter: {msg: string}\nresult: parameter.msg\n", nil
}

// newWorkflowContextForTest sets the var "test" to "yes"; build declares it.
var exprSteps = []oamv1alpha1.WorkflowStep{
	{WorkflowStepBase: oamv1alpha1.WorkflowStepBase{Name: "build", Type: "build",
		Outputs: oamv1alpha1.StepOutputs{{Name: "test", ValueFrom: "output.test"}}}},
}

type exprRun struct {
	status v1alpha1.StepStatus
	result string
}

func runWithExpressions(t *testing.T, msg string, evaluate bool, status map[string]v1alpha1.StepStatus,
	preCheck ...types.TaskPreCheckHook) exprRun {
	t.Helper()
	step := oamv1alpha1.WorkflowStep{WorkflowStepBase: oamv1alpha1.WorkflowStepBase{
		Name: "notify", Type: "echo",
		Properties: &runtime.RawExtension{Raw: []byte(`{"msg":` + msg + `}`)}}}
	// The context first: it sets the kube client the default compiler reads.
	wfCtx := newWorkflowContextForTest(t)
	pCtx := process.NewContext(process.ContextData{Name: "app", Namespace: "default"})
	loader := NewTaskLoader(echoTemplate, 0, pCtx, providers.DefaultCompiler.Get())
	gen, err := loader.GetTaskGenerator(context.Background(), step.Type)
	require.NoError(t, err)
	opts := &types.TaskGeneratorOptions{}
	if evaluate {
		ev, err := expressions.NewEvaluator(types.ExpressionOptions{}, exprSteps, "")
		require.NoError(t, err)
		opts.Expressions = ev
	}
	runner, err := gen(step, opts)
	require.NoError(t, err)

	var out exprRun
	s, _, err := runner.Run(wfCtx, &types.TaskRunOptions{
		StepStatus:    status,
		PreCheckHooks: preCheck,
		PostStopHooks: []types.TaskPostStopHook{
			func(_ wfContext.Context, taskv cue.Value, _ oamv1alpha1.WorkflowStep, _ v1alpha1.StepStatus,
				_ map[string]v1alpha1.StepStatus) error {
				out.result, _ = taskv.LookupPath(cue.ParsePath("result")).String()
				return nil
			}},
	})
	require.NoError(t, err)
	out.status = s
	return out
}

var buildSucceeded = map[string]v1alpha1.StepStatus{"build": {Phase: v1alpha1.WorkflowStepPhaseSucceeded}}

func TestStepPropertiesResolveWhenTheStepRuns(t *testing.T) {
	got := runWithExpressions(t, `"build said $(steps.build.outputs.test) to $(context.name)"`, true, buildSucceeded)
	require.Equal(t, v1alpha1.WorkflowStepPhaseSucceeded, got.status.Phase, got.status.Message)
	require.Equal(t, "build said yes to app", got.result)
}

func TestStepWaitsForAReadNotReady(t *testing.T) {
	got := runWithExpressions(t, `"$(steps.build.outputs.test)"`, true, nil)
	require.Equal(t, v1alpha1.WorkflowStepPhaseRunning, got.status.Phase)
	require.Equal(t, types.StatusReasonWait, got.status.Reason)
	require.Contains(t, got.status.Message, "waiting for step build")
}

func TestStepFailsOnAReadThatNeverWillBeReady(t *testing.T) {
	got := runWithExpressions(t, `"$(steps.build.outputs.test)"`, true,
		map[string]v1alpha1.StepStatus{"build": {Phase: v1alpha1.WorkflowStepPhaseSkipped}})
	require.Equal(t, v1alpha1.WorkflowStepPhaseFailed, got.status.Phase)
	require.Equal(t, types.StatusReasonRendering, got.status.Reason)
	require.Contains(t, got.status.Message, "step build was skipped")
}

func TestStepSkippedByItsCheckDoesNotWaitOnItsReads(t *testing.T) {
	skip := func(oamv1alpha1.WorkflowStep, *types.PreCheckOptions) (*types.PreCheckResult, error) {
		return &types.PreCheckResult{Skip: true}, nil
	}
	got := runWithExpressions(t, `"$(steps.build.outputs.test)"`, true, nil, skip)
	require.Equal(t, v1alpha1.WorkflowStepPhaseSkipped, got.status.Phase)
}

func TestStepPropertiesStayAsWrittenWithoutExpressions(t *testing.T) {
	got := runWithExpressions(t, `"$(steps.build.outputs.test)"`, false, buildSucceeded)
	require.Equal(t, "$(steps.build.outputs.test)", got.result)

	got = runWithExpressions(t, `"costs $$(5)"`, true, nil)
	require.Equal(t, "costs $(5)", got.result, "an escaped $( is unescaped")
}

// embedsComponent converts a step the way KubeVela's apply-component convertor
// does: it carries text in the step's properties that belongs to something else
// and is evaluated there, not by the step.
func embedsComponent(step oamv1alpha1.WorkflowStep) (oamv1alpha1.WorkflowStep, error) {
	var props struct {
		Msg string `json:"msg"`
	}
	if err := json.Unmarshal(step.Properties.Raw, &props); err != nil {
		return step, err
	}
	raw, err := json.Marshal(map[string]string{"msg": props.Msg + " carries $(component.db.output)"})
	if err != nil {
		return step, err
	}
	step.Properties = &runtime.RawExtension{Raw: raw}
	return step, nil
}

func TestAStepsOwnPropertiesResolveBeforeItIsConverted(t *testing.T) {
	step := oamv1alpha1.WorkflowStep{WorkflowStepBase: oamv1alpha1.WorkflowStepBase{
		Name: "notify", Type: "echo", Properties: &runtime.RawExtension{Raw: []byte(`{"msg":"$(context.name)"}`)}}}
	// The context first: it sets the kube client the default compiler reads.
	wfCtx := newWorkflowContextForTest(t)
	pCtx := process.NewContext(process.ContextData{Name: "app", Namespace: "default"})
	loader := NewTaskLoader(echoTemplate, 0, pCtx, providers.DefaultCompiler.Get())
	gen, err := loader.GetTaskGenerator(context.Background(), step.Type)
	require.NoError(t, err)
	ev, err := expressions.NewEvaluator(types.ExpressionOptions{}, exprSteps, "")
	require.NoError(t, err)
	runner, err := gen(step, &types.TaskGeneratorOptions{Expressions: ev, StepConvertor: embedsComponent})
	require.NoError(t, err)

	var result string
	status, _, err := runner.Run(wfCtx, &types.TaskRunOptions{
		PostStopHooks: []types.TaskPostStopHook{
			func(_ wfContext.Context, taskv cue.Value, _ oamv1alpha1.WorkflowStep, _ v1alpha1.StepStatus,
				_ map[string]v1alpha1.StepStatus) error {
				result, _ = taskv.LookupPath(cue.ParsePath("result")).String()
				return nil
			}},
	})
	require.NoError(t, err)
	require.Equal(t, v1alpha1.WorkflowStepPhaseSucceeded, status.Phase, status.Message)
	require.Equal(t, "app carries $(component.db.output)", result,
		"what the convertor adds is not the step's to evaluate")
}

// namesAComponent converts a step the way KubeVela's apply-component convertor
// does: it looks the component up by the name in its properties, which an
// expression may give.
func namesAComponent(step oamv1alpha1.WorkflowStep) (oamv1alpha1.WorkflowStep, error) {
	var props struct {
		Msg string `json:"msg"`
	}
	if err := json.Unmarshal(step.Properties.Raw, &props); err != nil {
		return step, err
	}
	if props.Msg != "app" {
		return step, fmt.Errorf("component %s not found", props.Msg)
	}
	step.Properties = &runtime.RawExtension{Raw: []byte(`{"msg":"the app component"}`)}
	return step, nil
}

func TestAStepWhoseConvertorNeedsItsExpressionsResolved(t *testing.T) {
	wfCtx := newWorkflowContextForTest(t)
	step := oamv1alpha1.WorkflowStep{WorkflowStepBase: oamv1alpha1.WorkflowStepBase{
		Name: "apply", Type: "echo", Properties: &runtime.RawExtension{Raw: []byte(`{"msg":"$(context.name)"}`)}}}
	pCtx := process.NewContext(process.ContextData{Name: "app", Namespace: "default"})
	loader := NewTaskLoader(echoTemplate, 0, pCtx, providers.DefaultCompiler.Get())
	gen, err := loader.GetTaskGenerator(context.Background(), step.Type)
	require.NoError(t, err)
	ev, err := expressions.NewEvaluator(types.ExpressionOptions{}, exprSteps, "")
	require.NoError(t, err)
	runner, err := gen(step, &types.TaskGeneratorOptions{Expressions: ev, StepConvertor: namesAComponent})
	require.NoError(t, err, "the convertor runs once the expression is resolved")

	var result string
	status, _, err := runner.Run(wfCtx, &types.TaskRunOptions{
		PostStopHooks: []types.TaskPostStopHook{
			func(_ wfContext.Context, taskv cue.Value, _ oamv1alpha1.WorkflowStep, _ v1alpha1.StepStatus,
				_ map[string]v1alpha1.StepStatus) error {
				result, _ = taskv.LookupPath(cue.ParsePath("result")).String()
				return nil
			}},
	})
	require.NoError(t, err)
	require.Equal(t, v1alpha1.WorkflowStepPhaseSucceeded, status.Phase, status.Message)
	require.Equal(t, "the app component", result)
}

// embedsComponentInPlace is embedsComponent writing through the step's own
// properties, as a convertor may.
func embedsComponentInPlace(step oamv1alpha1.WorkflowStep) (oamv1alpha1.WorkflowStep, error) {
	converted, err := embedsComponent(step)
	if err != nil {
		return step, err
	}
	step.Properties.Raw = converted.Properties.Raw
	return step, nil
}

func TestAConvertorEditingTheStepInPlaceLeavesItAsWritten(t *testing.T) {
	step := oamv1alpha1.WorkflowStep{WorkflowStepBase: oamv1alpha1.WorkflowStepBase{
		Name: "notify", Type: "echo", Properties: &runtime.RawExtension{Raw: []byte(`{"msg":"$(context.name)"}`)}}}
	wfCtx := newWorkflowContextForTest(t)
	pCtx := process.NewContext(process.ContextData{Name: "app", Namespace: "default"})
	loader := NewTaskLoader(echoTemplate, 0, pCtx, providers.DefaultCompiler.Get())
	gen, err := loader.GetTaskGenerator(context.Background(), step.Type)
	require.NoError(t, err)
	ev, err := expressions.NewEvaluator(types.ExpressionOptions{}, exprSteps, "")
	require.NoError(t, err)
	runner, err := gen(step, &types.TaskGeneratorOptions{Expressions: ev, StepConvertor: embedsComponentInPlace})
	require.NoError(t, err)

	var result string
	status, _, err := runner.Run(wfCtx, &types.TaskRunOptions{
		PostStopHooks: []types.TaskPostStopHook{
			func(_ wfContext.Context, taskv cue.Value, _ oamv1alpha1.WorkflowStep, _ v1alpha1.StepStatus,
				_ map[string]v1alpha1.StepStatus) error {
				result, _ = taskv.LookupPath(cue.ParsePath("result")).String()
				return nil
			}},
	})
	require.NoError(t, err)
	require.Equal(t, v1alpha1.WorkflowStepPhaseSucceeded, status.Phase, status.Message)
	require.Equal(t, "app carries $(component.db.output)", result)
	require.JSONEq(t, `{"msg":"$(context.name)"}`, string(step.Properties.Raw), "the step given is not changed")
}
