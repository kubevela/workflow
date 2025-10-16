package builtin

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/kubevela/workflow/pkg/cue/process"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/yaml"

	cuexv1alpha1 "github.com/kubevela/pkg/apis/cue/v1alpha1"
	monitorContext "github.com/kubevela/pkg/monitor/context"
	"github.com/kubevela/pkg/util/singleton"
	"github.com/kubevela/workflow/api/v1alpha1"
	wfContext "github.com/kubevela/workflow/pkg/context"
	"github.com/kubevela/workflow/pkg/types"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic/fake"

	oamv1alpha1 "github.com/kubevela/pkg/apis/oam/v1alpha1"
)

type testEngine struct {
	stepStatus v1alpha1.WorkflowStepStatus
	operation  *types.Operation
}

func (e *testEngine) Run(ctx monitorContext.Context, taskRunners []types.TaskRunner, dag bool) error {
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
	ctx := newWorkflowContextForTest(t)
	subRunner, err := StepGroup(oamv1alpha1.WorkflowStep{
		WorkflowStepBase: oamv1alpha1.WorkflowStepBase{
			Name: "sub",
		},
	}, &types.TaskGeneratorOptions{ID: "1"})
	r.NoError(err)
	runner, err := StepGroup(oamv1alpha1.WorkflowStep{
		WorkflowStepBase: oamv1alpha1.WorkflowStepBase{
			Name:      "test",
			DependsOn: []string{"depend"},
		},
	}, &types.TaskGeneratorOptions{ID: "124", SubTaskRunners: []types.TaskRunner{subRunner}, ProcessContext: process.NewContext(process.ContextData{})})
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
			func(step oamv1alpha1.WorkflowStep, options *types.PreCheckOptions) (*types.PreCheckResult, error) {
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
	status, operations, err = runner.Run(ctx, &types.TaskRunOptions{
		PreCheckHooks: []types.TaskPreCheckHook{
			func(step oamv1alpha1.WorkflowStep, options *types.PreCheckOptions) (*types.PreCheckResult, error) {
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
			status, act, err := runner.Run(ctx, &types.TaskRunOptions{
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

func newWorkflowContextForTest(t *testing.T) wfContext.Context {
	cm := corev1.ConfigMap{}
	r := require.New(t)
	testCaseJson, err := yaml.YAMLToJSON([]byte(testCaseYaml))
	r.NoError(err)
	err = json.Unmarshal(testCaseJson, &cm)
	r.NoError(err)

	scheme := runtime.NewScheme()
	r.NoError(cuexv1alpha1.AddToScheme(scheme))
	fakeDynamicClient := fake.NewSimpleDynamicClient(scheme)
	singleton.DynamicClient.Set(fakeDynamicClient)

	wfCtx := new(wfContext.WorkflowContext)
	err = wfCtx.LoadFromConfigMap(context.Background(), cm)
	r.NoError(err)
	return wfCtx
}

var (
	testCaseYaml = `apiVersion: v1
data:
  test: ""
kind: ConfigMap
metadata:
  name: app-v1
`
)
