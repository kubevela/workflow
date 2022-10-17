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
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/runtime"

	monitorContext "github.com/kubevela/pkg/monitor/context"

	"github.com/kubevela/workflow/api/v1alpha1"
	wfContext "github.com/kubevela/workflow/pkg/context"
	"github.com/kubevela/workflow/pkg/cue/model/value"
	"github.com/kubevela/workflow/pkg/cue/packages"
	"github.com/kubevela/workflow/pkg/cue/process"
	"github.com/kubevela/workflow/pkg/tasks/custom"
	"github.com/kubevela/workflow/pkg/types"
)

func Suspend(step v1alpha1.WorkflowStep, opt *types.TaskGeneratorOptions) (types.TaskRunner, error) {
	tr := &suspendTaskRunner{
		id:   opt.ID,
		step: step,
		pd:   opt.PackageDiscover,
		pCtx: opt.ProcessContext,
	}

	return tr, nil
}

type suspendTaskRunner struct {
	id   string
	step v1alpha1.WorkflowStep
	pd   *packages.PackageDiscover
	pCtx process.Context
}

// Name return suspend step name.
func (tr *suspendTaskRunner) Name() string {
	return tr.step.Name
}

// Run make workflow suspend.
func (tr *suspendTaskRunner) Run(ctx wfContext.Context, options *types.TaskRunOptions) (stepStatus v1alpha1.StepStatus, operations *types.Operation, rErr error) {
	stepStatus = v1alpha1.StepStatus{
		ID:      tr.id,
		Name:    tr.step.Name,
		Type:    types.WorkflowStepTypeSuspend,
		Phase:   v1alpha1.WorkflowStepPhaseRunning,
		Message: "",
	}
	operations = &types.Operation{Suspend: true}

	status := &stepStatus
	paramsStr, err := custom.GetParameterTemplate(tr.step)
	if err != nil {
		return stepStatus, operations, err
	}
	if options.GetTracer == nil {
		options.GetTracer = func(id string, step v1alpha1.WorkflowStep) monitorContext.Context {
			return monitorContext.NewTraceContext(context.Background(), "")
		}
	}
	tracer := options.GetTracer(tr.id, tr.step).AddTag("step_name", tr.step.Name, "step_type", types.WorkflowStepTypeStepGroup)
	basicVal, basicTemplate, err := custom.MakeBasicValue(tracer, ctx, tr.pd, tr.step.Name, tr.id, paramsStr, tr.pCtx)
	if err != nil {
		return stepStatus, operations, err
	}
	defer handleOutput(ctx, status, operations, tr.step, options.PostStopHooks, basicVal)

	for _, hook := range options.PreCheckHooks {
		result, err := hook(tr.step, &types.PreCheckOptions{
			PackageDiscover: tr.pd,
			BasicTemplate:   basicTemplate,
			BasicValue:      basicVal,
		})
		if err != nil {
			stepStatus.Phase = v1alpha1.WorkflowStepPhaseSkipped
			stepStatus.Reason = types.StatusReasonSkip
			stepStatus.Message = fmt.Sprintf("pre check error: %s", err.Error())
			operations.Suspend = false
			operations.Skip = true
			continue
		}
		switch {
		case result.Skip:
			stepStatus.Phase = v1alpha1.WorkflowStepPhaseSkipped
			stepStatus.Reason = types.StatusReasonSkip
			operations.Suspend = false
			operations.Skip = true
		case result.Timeout:
			stepStatus.Phase = v1alpha1.WorkflowStepPhaseFailed
			stepStatus.Reason = types.StatusReasonTimeout
			operations.Suspend = false
			operations.Terminated = true
		default:
			continue
		}
		return stepStatus, operations, nil
	}

	for _, input := range tr.step.Inputs {
		if input.ParameterKey == "duration" {
			inputValue, err := ctx.GetVar(strings.Split(input.From, ".")...)
			if err != nil {
				return v1alpha1.StepStatus{}, nil, errors.WithMessagef(err, "do preStartHook: get input from [%s]", input.From)
			}
			d, err := inputValue.String()
			if err != nil {
				return v1alpha1.StepStatus{}, nil, errors.WithMessagef(err, "do preStartHook: input value from [%s] is not a valid string", input.From)
			}
			tr.step.Properties = &runtime.RawExtension{Raw: []byte(`{"duration":` + d + `}`)}
		}
	}
	d, err := GetSuspendStepDurationWaiting(tr.step)
	if err != nil {
		stepStatus.Message = fmt.Sprintf("invalid suspend duration: %s", err.Error())
		return stepStatus, operations, nil
	}
	if d != 0 {
		e := options.Engine
		firstExecuteTime := time.Now()
		if ss := e.GetCommonStepStatus(tr.step.Name); !ss.FirstExecuteTime.IsZero() {
			firstExecuteTime = ss.FirstExecuteTime.Time
		}
		if time.Now().After(firstExecuteTime.Add(d)) {
			stepStatus.Phase = v1alpha1.WorkflowStepPhaseSucceeded
			operations.Suspend = false
		}
	}
	return stepStatus, operations, nil
}

// Pending check task should be executed or not.
func (tr *suspendTaskRunner) Pending(ctx monitorContext.Context, wfCtx wfContext.Context, stepStatus map[string]v1alpha1.StepStatus) (bool, v1alpha1.StepStatus) {
	basicVal, _, _ := custom.MakeBasicValue(ctx, wfCtx, tr.pd, tr.step.Name, tr.id, "", tr.pCtx)
	return custom.CheckPending(wfCtx, tr.step, tr.id, stepStatus, basicVal)
}

// GetSuspendStepDurationWaiting get suspend step wait duration
func GetSuspendStepDurationWaiting(step v1alpha1.WorkflowStep) (time.Duration, error) {
	if step.Properties.Size() > 0 {
		o := struct {
			Duration string `json:"duration"`
		}{}
		js, err := step.Properties.MarshalJSON()
		if err != nil {
			return 0, err
		}
		if err := json.Unmarshal(js, &o); err != nil {
			return 0, err
		}
		if o.Duration != "" {
			waitDuration, err := time.ParseDuration(o.Duration)
			return waitDuration, err
		}
	}

	return 0, nil
}

func handleOutput(ctx wfContext.Context, stepStatus *v1alpha1.StepStatus, operations *types.Operation, step v1alpha1.WorkflowStep, postStopHooks []types.TaskPostStopHook, basicVal *value.Value) {
	if len(step.Outputs) > 0 {
		for _, hook := range postStopHooks {
			if err := hook(ctx, basicVal, step, *stepStatus, nil); err != nil {
				stepStatus.Phase = v1alpha1.WorkflowStepPhaseFailed
				if stepStatus.Reason == "" {
					stepStatus.Reason = types.StatusReasonOutput
				}
				operations.Terminated = true
				stepStatus.Message = fmt.Sprintf("output error: %s", err.Error())
				return
			}
		}
	}
}
