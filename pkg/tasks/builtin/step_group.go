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

package builtin

import (
	"context"
	"fmt"

	monitorContext "github.com/kubevela/pkg/monitor/context"

	"github.com/kubevela/workflow/api/v1alpha1"
	wfContext "github.com/kubevela/workflow/pkg/context"
	"github.com/kubevela/workflow/pkg/cue/packages"
	"github.com/kubevela/workflow/pkg/cue/process"
	"github.com/kubevela/workflow/pkg/tasks/custom"
	"github.com/kubevela/workflow/pkg/types"
)

// StepGroup is the step group runner
func StepGroup(step v1alpha1.WorkflowStep, opt *types.TaskGeneratorOptions) (types.TaskRunner, error) {
	return &stepGroupTaskRunner{
		id:             opt.ID,
		name:           step.Name,
		step:           step,
		subTaskRunners: opt.SubTaskRunners,
		mode:           opt.SubStepExecuteMode,
		pd:             opt.PackageDiscover,
		pCtx:           opt.ProcessContext,
	}, nil
}

type stepGroupTaskRunner struct {
	id             string
	name           string
	step           v1alpha1.WorkflowStep
	subTaskRunners []types.TaskRunner
	pd             *packages.PackageDiscover
	pCtx           process.Context
	mode           v1alpha1.WorkflowMode
}

// Name return suspend step name.
func (tr *stepGroupTaskRunner) Name() string {
	return tr.name
}

// Pending check task should be executed or not.
func (tr *stepGroupTaskRunner) Pending(ctx monitorContext.Context, wfCtx wfContext.Context, stepStatus map[string]v1alpha1.StepStatus) (bool, v1alpha1.StepStatus) {
	basicVal, _, _ := custom.MakeBasicValue(ctx, wfCtx, tr.pd, tr.name, tr.id, "", tr.pCtx)
	return custom.CheckPending(wfCtx, tr.step, tr.id, stepStatus, basicVal)
}

// Run make workflow step group.
func (tr *stepGroupTaskRunner) Run(ctx wfContext.Context, options *types.TaskRunOptions) (status v1alpha1.StepStatus, operations *types.Operation, rErr error) {
	status = v1alpha1.StepStatus{
		ID:      tr.id,
		Name:    tr.name,
		Type:    types.WorkflowStepTypeStepGroup,
		Message: "",
	}

	pStatus := &status
	if options.GetTracer == nil {
		options.GetTracer = func(id string, step v1alpha1.WorkflowStep) monitorContext.Context {
			return monitorContext.NewTraceContext(context.Background(), "")
		}
	}
	tracer := options.GetTracer(tr.id, tr.step).AddTag("step_name", tr.name, "step_type", types.WorkflowStepTypeStepGroup)
	basicVal, basicTemplate, err := custom.MakeBasicValue(tracer, ctx, tr.pd, tr.name, tr.id, "", tr.pCtx)
	if err != nil {
		return status, nil, err
	}
	defer handleOutput(ctx, pStatus, operations, tr.step, options.PostStopHooks, basicVal)

	for _, hook := range options.PreCheckHooks {
		result, err := hook(tr.step, &types.PreCheckOptions{
			PackageDiscover: tr.pd,
			BasicTemplate:   basicTemplate,
			BasicValue:      basicVal,
		})
		if err != nil {
			status.Phase = v1alpha1.WorkflowStepPhaseSkipped
			status.Reason = types.StatusReasonSkip
			status.Message = fmt.Sprintf("pre check error: %s", err.Error())
			continue
		}
		if result.Skip {
			status.Phase = v1alpha1.WorkflowStepPhaseSkipped
			status.Reason = types.StatusReasonSkip
			options.StepStatus[tr.step.Name] = status
			break
		}
		if result.Timeout {
			status.Phase = v1alpha1.WorkflowStepPhaseFailed
			status.Reason = types.StatusReasonTimeout
			options.StepStatus[tr.step.Name] = status
		}
	}
	// step-group has no properties so there is no need to fill in the properties with the input values
	// skip input handle here
	e := options.Engine
	if len(tr.subTaskRunners) > 0 {
		e.SetParentRunner(tr.name)
		dag := true
		if tr.mode == v1alpha1.WorkflowModeStep {
			dag = false
		}
		if err := e.Run(tracer, tr.subTaskRunners, dag); err != nil {
			return v1alpha1.StepStatus{
				ID:    tr.id,
				Name:  tr.name,
				Type:  types.WorkflowStepTypeStepGroup,
				Phase: v1alpha1.WorkflowStepPhaseRunning,
			}, e.GetOperation(), err
		}
		e.SetParentRunner("")
	}

	stepStatus := e.GetStepStatus(tr.name)
	status, operations = getStepGroupStatus(status, stepStatus, e.GetOperation(), len(tr.subTaskRunners))

	return status, operations, nil
}

func getStepGroupStatus(status v1alpha1.StepStatus, stepStatus v1alpha1.WorkflowStepStatus, operation *types.Operation, subTaskRunners int) (v1alpha1.StepStatus, *types.Operation) {
	subStepCounts := make(map[string]int)
	for _, subStepsStatus := range stepStatus.SubStepsStatus {
		subStepCounts[string(subStepsStatus.Phase)]++
		subStepCounts[subStepsStatus.Reason]++
	}
	switch {
	case status.Phase == v1alpha1.WorkflowStepPhaseSkipped:
		return status, &types.Operation{Skip: true}
	case status.Phase == v1alpha1.WorkflowStepPhaseFailed && status.Reason == types.StatusReasonTimeout:
		return status, &types.Operation{Terminated: true}
	case len(stepStatus.SubStepsStatus) < subTaskRunners:
		status.Phase = v1alpha1.WorkflowStepPhaseRunning
	case subStepCounts[string(v1alpha1.WorkflowStepPhaseRunning)] > 0:
		status.Phase = v1alpha1.WorkflowStepPhaseRunning
	case subStepCounts[string(v1alpha1.WorkflowStepPhasePending)] > 0:
		status.Phase = v1alpha1.WorkflowStepPhasePending
	case subStepCounts[string(v1alpha1.WorkflowStepPhaseFailed)] > 0:
		status.Phase = v1alpha1.WorkflowStepPhaseFailed
		switch {
		case subStepCounts[types.StatusReasonFailedAfterRetries] > 0:
			status.Reason = types.StatusReasonFailedAfterRetries
		case subStepCounts[types.StatusReasonTimeout] > 0:
			status.Reason = types.StatusReasonTimeout
		case subStepCounts[types.StatusReasonAction] > 0:
			status.Reason = types.StatusReasonAction
		case subStepCounts[types.StatusReasonTerminate] > 0:
			status.Reason = types.StatusReasonTerminate
		}
	case subStepCounts[string(v1alpha1.WorkflowStepPhaseSkipped)] > 0 && subStepCounts[string(v1alpha1.WorkflowStepPhaseSkipped)] == subTaskRunners:
		status.Phase = v1alpha1.WorkflowStepPhaseSkipped
		status.Reason = types.StatusReasonSkip
	default:
		status.Phase = v1alpha1.WorkflowStepPhaseSucceeded
	}
	return status, operation
}
