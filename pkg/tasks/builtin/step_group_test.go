package builtin

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kubevela/workflow/api/v1alpha1"
	"github.com/kubevela/workflow/pkg/types"
)

type testEngine struct {
	stepStatus v1alpha1.WorkflowStepStatus
	operation  *types.Operation
}

func (e *testEngine) Run(taskRunners []types.TaskRunner, dag bool) error {
	return nil
}

func (e *testEngine) GetStepStatus(stepName string) v1alpha1.WorkflowStepStatus {
	return e.stepStatus
}

func (e *testEngine) GetCommonStepStatus(stepName string) v1alpha1.StepStatus {
	return v1alpha1.StepStatus{}
}

func (e *testEngine) SetParentRunner(name string) {
}

func (e *testEngine) GetOperation() *types.Operation {
	return e.operation
}

func TestStepGroupStep(t *testing.T) {
	r := require.New(t)
	subRunner, err := StepGroup(v1alpha1.WorkflowStep{
		WorkflowStepBase: v1alpha1.WorkflowStepBase{
			Name: "sub",
		},
	}, &types.TaskGeneratorOptions{ID: "1"})
	r.NoError(err)
	runner, err := StepGroup(v1alpha1.WorkflowStep{
		WorkflowStepBase: v1alpha1.WorkflowStepBase{
			Name:      "test",
			DependsOn: []string{"depend"},
		},
	}, &types.TaskGeneratorOptions{ID: "124", SubTaskRunners: []types.TaskRunner{subRunner}})
	r.NoError(err)
	r.Equal(runner.Name(), "test")

	// test pending
	r.Equal(runner.Pending(nil, nil), true)
	ss := map[string]v1alpha1.StepStatus{
		"depend": {
			Phase: v1alpha1.WorkflowStepPhaseSucceeded,
		},
	}
	r.Equal(runner.Pending(nil, ss), false)

	// test skip
	status, operations, err := runner.Run(nil, &types.TaskRunOptions{
		PreCheckHooks: []types.TaskPreCheckHook{
			func(step v1alpha1.WorkflowStep, options *types.PreCheckOptions) (*types.PreCheckResult, error) {
				return &types.PreCheckResult{Skip: true}, nil
			},
		},
		StepStatus: map[string]v1alpha1.StepStatus{},
		Engine: &testEngine{
			stepStatus: v1alpha1.WorkflowStepStatus{},
			operation:  &types.Operation{},
		},
	})
	r.NoError(err)
	r.Equal(status.Phase, v1alpha1.WorkflowStepPhaseSkipped)
	r.Equal(status.Reason, types.StatusReasonSkip)
	r.Equal(operations.Skip, true)

	// test timeout
	status, operations, err = runner.Run(nil, &types.TaskRunOptions{
		PreCheckHooks: []types.TaskPreCheckHook{
			func(step v1alpha1.WorkflowStep, options *types.PreCheckOptions) (*types.PreCheckResult, error) {
				return &types.PreCheckResult{Timeout: true}, nil
			},
		},
		StepStatus: map[string]v1alpha1.StepStatus{},
		Engine: &testEngine{
			stepStatus: v1alpha1.WorkflowStepStatus{},
			operation:  &types.Operation{},
		},
	})
	r.NoError(err)
	r.Equal(status.Phase, v1alpha1.WorkflowStepPhaseFailed)
	r.Equal(status.Reason, types.StatusReasonTimeout)
	r.Equal(operations.Terminated, true)

	// test run
	testCases := []struct {
		name          string
		engine        *testEngine
		expectedPhase v1alpha1.WorkflowStepPhase
	}{
		{
			name: "running1",
			engine: &testEngine{
				stepStatus: v1alpha1.WorkflowStepStatus{},
				operation:  &types.Operation{},
			},
			expectedPhase: v1alpha1.WorkflowStepPhaseRunning,
		},
		{
			name: "running2",
			engine: &testEngine{
				stepStatus: v1alpha1.WorkflowStepStatus{
					SubStepsStatus: []v1alpha1.StepStatus{
						{
							Phase: v1alpha1.WorkflowStepPhaseRunning,
						},
					},
				},
				operation: &types.Operation{},
			},
			expectedPhase: v1alpha1.WorkflowStepPhaseRunning,
		},
		{
			name: "stop",
			engine: &testEngine{
				stepStatus: v1alpha1.WorkflowStepStatus{
					SubStepsStatus: []v1alpha1.StepStatus{
						{
							Phase: v1alpha1.WorkflowStepPhaseStopped,
						},
						{
							Phase: v1alpha1.WorkflowStepPhaseFailed,
						},
					},
				},
				operation: &types.Operation{},
			},
			expectedPhase: v1alpha1.WorkflowStepPhaseStopped,
		},
		{
			name: "fail",
			engine: &testEngine{
				stepStatus: v1alpha1.WorkflowStepStatus{
					SubStepsStatus: []v1alpha1.StepStatus{
						{
							Phase: v1alpha1.WorkflowStepPhaseFailed,
						},
						{
							Phase: v1alpha1.WorkflowStepPhaseSucceeded,
						},
					},
				},
				operation: &types.Operation{},
			},
			expectedPhase: v1alpha1.WorkflowStepPhaseFailed,
		},
		{
			name: "success",
			engine: &testEngine{
				stepStatus: v1alpha1.WorkflowStepStatus{
					SubStepsStatus: []v1alpha1.StepStatus{
						{
							Phase: v1alpha1.WorkflowStepPhaseSucceeded,
						},
					},
				},
				operation: &types.Operation{},
			},
			expectedPhase: v1alpha1.WorkflowStepPhaseSucceeded,
		},
		{
			name: "operation",
			engine: &testEngine{
				stepStatus: v1alpha1.WorkflowStepStatus{
					SubStepsStatus: []v1alpha1.StepStatus{
						{
							Phase: v1alpha1.WorkflowStepPhaseSucceeded,
						},
					},
				},
				operation: &types.Operation{
					Suspend:            true,
					Terminated:         true,
					FailedAfterRetries: true,
					Waiting:            true,
				},
			},
			expectedPhase: v1alpha1.WorkflowStepPhaseSucceeded,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			status, act, err := runner.Run(nil, &types.TaskRunOptions{
				Engine: tc.engine,
			})
			r.NoError(err)
			r.Equal(status.ID, "124")
			r.Equal(status.Name, "test")
			r.Equal(act.Suspend, tc.engine.operation.Suspend)
			r.Equal(status.Phase, tc.expectedPhase)
		})
	}
}
