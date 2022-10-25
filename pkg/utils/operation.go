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

package utils

import (
	"context"
	"fmt"
	"io"

	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kubevela/workflow/api/v1alpha1"
	wfTypes "github.com/kubevela/workflow/pkg/types"
)

// WorkflowOperator is operation handler for workflow's suspend/resume/rollback/restart/terminate
type WorkflowOperator interface {
	Suspend(ctx context.Context) error
	Resume(ctx context.Context) error
	Rollback(ctx context.Context) error
	Restart(ctx context.Context) error
	Terminate(ctx context.Context) error
}

type workflowRunOperator struct {
	cli          client.Client
	outputWriter io.Writer
	run          *v1alpha1.WorkflowRun
}

// NewApplicationWorkflowOperator get an workflow operator with k8sClient, ioWriter(optional, useful for cli) and application
func NewWorkflowRunOperator(cli client.Client, w io.Writer, run *v1alpha1.WorkflowRun) WorkflowOperator {
	return workflowRunOperator{
		cli:          cli,
		outputWriter: w,
		run:          run,
	}
}

// Suspend suspend workflow
func (wo workflowRunOperator) Suspend(ctx context.Context) error {
	run := wo.run
	runKey := client.ObjectKeyFromObject(run)
	if err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		if err := wo.cli.Get(ctx, runKey, run); err != nil {
			return err
		}
		// set the workflow suspend to true
		run.Status.Suspend = true
		return wo.cli.Status().Patch(ctx, run, client.Merge)
	}); err != nil {
		return err
	}

	return wo.writeOutputF("Successfully suspend workflow: %s\n", run.Name)
}

// Resume resume a suspended workflow
func (wo workflowRunOperator) Resume(ctx context.Context) error {
	run := wo.run
	if run.Status.Terminated {
		return fmt.Errorf("can not resume a terminated workflow")
	}

	if run.Status.Suspend {
		if err := ResumeWorkflow(ctx, wo.cli, run); err != nil {
			return err
		}
	}
	return wo.writeOutputF("Successfully resume workflow: %s\n", run.Name)
}

// ResumeWorkflow resume workflow
func ResumeWorkflow(ctx context.Context, cli client.Client, run *v1alpha1.WorkflowRun) error {
	run.Status.Suspend = false
	steps := run.Status.Steps
	for i, step := range steps {
		if step.Type == wfTypes.WorkflowStepTypeSuspend && step.Phase == v1alpha1.WorkflowStepPhaseRunning {
			steps[i].Phase = v1alpha1.WorkflowStepPhaseSucceeded
		}
		for j, sub := range step.SubStepsStatus {
			if sub.Type == wfTypes.WorkflowStepTypeSuspend && sub.Phase == v1alpha1.WorkflowStepPhaseRunning {
				steps[i].SubStepsStatus[j].Phase = v1alpha1.WorkflowStepPhaseSucceeded
			}
		}
	}
	if err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		return cli.Status().Patch(ctx, run, client.Merge)
	}); err != nil {
		return err
	}
	return nil
}

// Rollback is not supported for WorkflowRun
func (wo workflowRunOperator) Rollback(ctx context.Context) error {
	return fmt.Errorf("Can not rollback a WorkflowRun")
}

// Restart is not supported for WorkflowRun
func (wo workflowRunOperator) Restart(ctx context.Context) error {
	return fmt.Errorf("Can not restart a WorkflowRun")
}

// Terminate terminate workflow
func (wo workflowRunOperator) Terminate(ctx context.Context) error {
	run := wo.run
	if err := TerminateWorkflow(ctx, wo.cli, run); err != nil {
		return err
	}
	return wo.writeOutputF("Successfully terminate workflow: %s\n", run.Name)
}

// TerminateWorkflow terminate workflow
func TerminateWorkflow(ctx context.Context, cli client.Client, run *v1alpha1.WorkflowRun) error {
	// set the workflow terminated to true
	run.Status.Terminated = true
	// set the workflow suspend to false
	run.Status.Suspend = false
	steps := run.Status.Steps
	for i, step := range steps {
		switch step.Phase {
		case v1alpha1.WorkflowStepPhaseFailed:
			if step.Reason != wfTypes.StatusReasonFailedAfterRetries && step.Reason != wfTypes.StatusReasonTimeout {
				steps[i].Reason = wfTypes.StatusReasonTerminate
			}
		case v1alpha1.WorkflowStepPhaseRunning:
			steps[i].Phase = v1alpha1.WorkflowStepPhaseFailed
			steps[i].Reason = wfTypes.StatusReasonTerminate
		default:
		}
		for j, sub := range step.SubStepsStatus {
			switch sub.Phase {
			case v1alpha1.WorkflowStepPhaseFailed:
				if sub.Reason != wfTypes.StatusReasonFailedAfterRetries && sub.Reason != wfTypes.StatusReasonTimeout {
					steps[i].SubStepsStatus[j].Reason = wfTypes.StatusReasonTerminate
				}
			case v1alpha1.WorkflowStepPhaseRunning:
				steps[i].SubStepsStatus[j].Phase = v1alpha1.WorkflowStepPhaseFailed
				steps[i].SubStepsStatus[j].Reason = wfTypes.StatusReasonTerminate
			default:
			}
		}
	}

	if err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		return cli.Status().Patch(ctx, run, client.Merge)
	}); err != nil {
		return err
	}
	return nil
}

func (wo workflowRunOperator) writeOutputF(format string, a ...interface{}) error {
	if wo.outputWriter == nil {
		return nil
	}
	_, err := fmt.Fprintf(wo.outputWriter, format, a...)
	return err
}
