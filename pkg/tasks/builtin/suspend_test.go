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
	"testing"

	"github.com/stretchr/testify/require"

	monitorContext "github.com/kubevela/pkg/monitor/context"
	"github.com/kubevela/workflow/api/v1alpha1"
	"github.com/kubevela/workflow/pkg/types"
)

func TestSuspendStep(t *testing.T) {
	r := require.New(t)
	ctx := newWorkflowContextForTest(t)
	runner, err := Suspend(v1alpha1.WorkflowStep{
		WorkflowStepBase: v1alpha1.WorkflowStepBase{
			Name:      "test",
			DependsOn: []string{"depend"},
		},
	}, &types.TaskGeneratorOptions{ID: "124"})
	r.NoError(err)
	r.Equal(runner.Name(), "test")

	// test pending
	logCtx := monitorContext.NewTraceContext(context.Background(), "test-app")
	p, _ := runner.Pending(logCtx, ctx, nil)
	r.Equal(p, true)
	ss := map[string]v1alpha1.StepStatus{
		"depend": {
			Phase: v1alpha1.WorkflowStepPhaseSucceeded,
		},
	}
	p, _ = runner.Pending(logCtx, ctx, ss)
	r.Equal(p, false)

	// test skip
	status, operations, err := runner.Run(ctx, &types.TaskRunOptions{
		PreCheckHooks: []types.TaskPreCheckHook{
			func(step v1alpha1.WorkflowStep, options *types.PreCheckOptions) (*types.PreCheckResult, error) {
				return &types.PreCheckResult{Skip: true}, nil
			},
		},
	})
	r.NoError(err)
	r.Equal(status.Phase, v1alpha1.WorkflowStepPhaseSkipped)
	r.Equal(status.Reason, types.StatusReasonSkip)
	r.Equal(operations.Suspend, false)
	r.Equal(operations.Skip, true)

	// test timeout
	status, operations, err = runner.Run(ctx, &types.TaskRunOptions{
		PreCheckHooks: []types.TaskPreCheckHook{
			func(step v1alpha1.WorkflowStep, options *types.PreCheckOptions) (*types.PreCheckResult, error) {
				return &types.PreCheckResult{Timeout: true}, nil
			},
		},
	})
	r.NoError(err)
	r.Equal(status.Phase, v1alpha1.WorkflowStepPhaseFailed)
	r.Equal(status.Reason, types.StatusReasonTimeout)
	r.Equal(operations.Suspend, false)
	r.Equal(operations.Terminated, true)

	// test run
	status, act, err := runner.Run(ctx, &types.TaskRunOptions{})
	r.NoError(err)
	r.Equal(act.Suspend, true)
	r.Equal(status.ID, "124")
	r.Equal(status.Name, "test")
	r.Equal(status.Phase, v1alpha1.WorkflowStepPhaseRunning)
}
