package custom

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

import (
	"github.com/kubevela/pkg/cue/cuex"

	monitorContext "github.com/kubevela/pkg/monitor/context"
	"github.com/kubevela/workflow/api/v1alpha1"
	wfContext "github.com/kubevela/workflow/pkg/context"
	"github.com/kubevela/workflow/pkg/errors"
	"github.com/kubevela/workflow/pkg/types"
)

// ResolveActionBreak resolve action break error
func ResolveActionBreak(err error) error {
	fc, ok := err.(cuex.FunctionCallError)
	if !ok {
		return err
	}
	if _, ok := fc.Err.(errors.GenericActionError); ok {
		return nil
	}
	return err
}

type executor struct {
	wfStatus           v1alpha1.StepStatus
	stepStatus         v1alpha1.StepStatus
	suspend            bool
	terminated         bool
	failedAfterRetries bool
	wait               bool
	skip               bool

	tracer monitorContext.Context
}

// Suspend let workflow pause.
func (exec *executor) Suspend(message string) {
	if exec.wfStatus.Phase == v1alpha1.WorkflowStepPhaseFailed {
		return
	}
	exec.suspend = true
	exec.wfStatus.Phase = v1alpha1.WorkflowStepPhaseSuspending
	if message != "" {
		exec.wfStatus.Message = message
	}
	exec.wfStatus.Reason = types.StatusReasonSuspend
}

// Resume let workflow resume.
func (exec *executor) Resume(message string) {
	exec.suspend = false
	exec.wfStatus.Phase = v1alpha1.WorkflowStepPhaseSucceeded
	if message != "" {
		exec.wfStatus.Message = message
	}
}

// Terminate let workflow terminate.
func (exec *executor) Terminate(message string) {
	exec.terminated = true
	exec.wfStatus.Phase = v1alpha1.WorkflowStepPhaseSucceeded
	if message != "" {
		exec.wfStatus.Message = message
	}
	exec.wfStatus.Reason = types.StatusReasonTerminate
}

// Wait let workflow wait.
func (exec *executor) Wait(message string) {
	exec.wait = true
	if exec.wfStatus.Phase != v1alpha1.WorkflowStepPhaseFailed {
		exec.wfStatus.Phase = v1alpha1.WorkflowStepPhaseRunning
		exec.wfStatus.Reason = types.StatusReasonWait
		if message != "" {
			exec.wfStatus.Message = message
		}
	}
}

// Fail let the step fail, its status is failed and reason is Action
func (exec *executor) Fail(message string) {
	exec.terminated = true
	exec.wfStatus.Phase = v1alpha1.WorkflowStepPhaseFailed
	exec.wfStatus.Reason = types.StatusReasonAction
	if message != "" {
		exec.wfStatus.Message = message
	}
}

// Message writes message to step status, note that the message will be overwritten by the next message.
func (exec *executor) Message(message string) {
	if message != "" {
		exec.wfStatus.Message = message
	}
}

func (exec *executor) Skip(message string) {
	exec.skip = true
	exec.wfStatus.Phase = v1alpha1.WorkflowStepPhaseSkipped
	exec.wfStatus.Reason = types.StatusReasonSkip
	exec.wfStatus.Message = message
}

func (exec *executor) GetStatus() v1alpha1.StepStatus {
	return exec.stepStatus
}

func (exec *executor) timeout(message string) {
	exec.terminated = true
	exec.wfStatus.Phase = v1alpha1.WorkflowStepPhaseFailed
	exec.wfStatus.Reason = types.StatusReasonTimeout
	exec.wfStatus.Message = message
}

func (exec *executor) err(ctx wfContext.Context, wait bool, err error, reason string) {
	exec.wait = wait
	exec.wfStatus.Phase = v1alpha1.WorkflowStepPhaseFailed
	exec.wfStatus.Message = err.Error()
	if exec.wfStatus.Reason == "" {
		exec.wfStatus.Reason = reason
		if reason != types.StatusReasonExecute {
			exec.terminated = true
		}
	}
	exec.checkErrorTimes(ctx)
}

func (exec *executor) checkErrorTimes(ctx wfContext.Context) {
	times := ctx.IncreaseCountValueInMemory(types.ContextPrefixFailedTimes, exec.wfStatus.ID)
	if times >= types.MaxWorkflowStepErrorRetryTimes {
		exec.wait = false
		exec.failedAfterRetries = true
		exec.wfStatus.Reason = types.StatusReasonFailedAfterRetries
	}
}

func (exec *executor) operation() *types.Operation {
	return &types.Operation{
		Suspend:            exec.suspend,
		Terminated:         exec.terminated,
		Waiting:            exec.wait,
		Skip:               exec.skip,
		FailedAfterRetries: exec.failedAfterRetries,
	}
}

func (exec *executor) status() v1alpha1.StepStatus {
	return exec.wfStatus
}
