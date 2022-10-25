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
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kubevela/workflow/api/v1alpha1"
	wfTypes "github.com/kubevela/workflow/pkg/types"
)

func TestSuspendWorkflowRun(t *testing.T) {
	ctx := context.Background()

	testCases := map[string]struct {
		run *v1alpha1.WorkflowRun
	}{
		"already suspend": {
			run: &v1alpha1.WorkflowRun{
				ObjectMeta: metav1.ObjectMeta{
					Name: "already-suspend",
				},
				Status: v1alpha1.WorkflowRunStatus{
					Suspend: true,
				},
			},
		},
		"not suspend": {
			run: &v1alpha1.WorkflowRun{
				ObjectMeta: metav1.ObjectMeta{
					Name: "not-suspend",
				},
				Status: v1alpha1.WorkflowRunStatus{
					Suspend: false,
				},
			},
		},
	}
	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			r := require.New(t)
			err := cli.Create(ctx, tc.run)
			r.NoError(err)
			defer func() {
				err = cli.Delete(ctx, tc.run)
				r.NoError(err)
			}()
			operator := NewWorkflowRunOperator(cli, nil, tc.run)
			err = operator.Suspend(ctx)
			r.NoError(err)
			run := &v1alpha1.WorkflowRun{}
			err = cli.Get(ctx, client.ObjectKey{Name: tc.run.Name}, run)
			r.NoError(err)
			r.Equal(true, run.Status.Suspend)
		})
	}
}

func TestTerminateWorkflowRun(t *testing.T) {
	ctx := context.Background()

	testCases := map[string]struct {
		run      *v1alpha1.WorkflowRun
		expected *v1alpha1.WorkflowRun
	}{
		"suspend": {
			run: &v1alpha1.WorkflowRun{
				ObjectMeta: metav1.ObjectMeta{
					Name: "suspend",
				},
				Status: v1alpha1.WorkflowRunStatus{
					Suspend: true,
				},
			},
			expected: &v1alpha1.WorkflowRun{
				Status: v1alpha1.WorkflowRunStatus{
					Terminated: true,
				},
			},
		},
		"running step": {
			run: &v1alpha1.WorkflowRun{
				ObjectMeta: metav1.ObjectMeta{
					Name: "running-step",
				},
				Status: v1alpha1.WorkflowRunStatus{
					Steps: []v1alpha1.WorkflowStepStatus{
						{
							StepStatus: v1alpha1.StepStatus{
								Phase: v1alpha1.WorkflowStepPhaseRunning,
							},
							SubStepsStatus: []v1alpha1.StepStatus{
								{
									Phase: v1alpha1.WorkflowStepPhaseRunning,
								},
								{
									Phase:  v1alpha1.WorkflowStepPhaseFailed,
									Reason: wfTypes.StatusReasonFailedAfterRetries,
								},
								{
									Phase:  v1alpha1.WorkflowStepPhaseFailed,
									Reason: wfTypes.StatusReasonTimeout,
								},
								{
									Phase:  v1alpha1.WorkflowStepPhaseFailed,
									Reason: wfTypes.StatusReasonExecute,
								},
							},
						},
						{
							StepStatus: v1alpha1.StepStatus{
								Phase:  v1alpha1.WorkflowStepPhaseFailed,
								Reason: wfTypes.StatusReasonFailedAfterRetries,
							},
						},
						{
							StepStatus: v1alpha1.StepStatus{
								Phase:  v1alpha1.WorkflowStepPhaseFailed,
								Reason: wfTypes.StatusReasonTimeout,
							},
						},
						{
							StepStatus: v1alpha1.StepStatus{
								Phase:  v1alpha1.WorkflowStepPhaseFailed,
								Reason: wfTypes.StatusReasonExecute,
							},
						},
					},
				},
			},
			expected: &v1alpha1.WorkflowRun{
				Status: v1alpha1.WorkflowRunStatus{
					Steps: []v1alpha1.WorkflowStepStatus{
						{
							StepStatus: v1alpha1.StepStatus{
								Phase:  v1alpha1.WorkflowStepPhaseFailed,
								Reason: wfTypes.StatusReasonTerminate,
							},
							SubStepsStatus: []v1alpha1.StepStatus{
								{
									Phase:  v1alpha1.WorkflowStepPhaseFailed,
									Reason: wfTypes.StatusReasonTerminate,
								},
								{
									Phase:  v1alpha1.WorkflowStepPhaseFailed,
									Reason: wfTypes.StatusReasonFailedAfterRetries,
								},
								{
									Phase:  v1alpha1.WorkflowStepPhaseFailed,
									Reason: wfTypes.StatusReasonTimeout,
								},
								{
									Phase:  v1alpha1.WorkflowStepPhaseFailed,
									Reason: wfTypes.StatusReasonTerminate,
								},
							},
						},
						{
							StepStatus: v1alpha1.StepStatus{
								Phase:  v1alpha1.WorkflowStepPhaseFailed,
								Reason: wfTypes.StatusReasonFailedAfterRetries,
							},
						},
						{
							StepStatus: v1alpha1.StepStatus{
								Phase:  v1alpha1.WorkflowStepPhaseFailed,
								Reason: wfTypes.StatusReasonTimeout,
							},
						},
						{
							StepStatus: v1alpha1.StepStatus{
								Phase:  v1alpha1.WorkflowStepPhaseFailed,
								Reason: wfTypes.StatusReasonTerminate,
							},
						},
					},
					Terminated: true,
				},
			},
		},
	}
	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			r := require.New(t)
			err := cli.Create(ctx, tc.run)
			r.NoError(err)
			defer func() {
				err = cli.Delete(ctx, tc.run)
				r.NoError(err)
			}()
			operator := NewWorkflowRunOperator(cli, nil, tc.run)
			err = operator.Terminate(ctx)
			r.NoError(err)
			run := &v1alpha1.WorkflowRun{}
			err = cli.Get(ctx, client.ObjectKey{Name: tc.run.Name}, run)
			r.NoError(err)
			r.Equal(false, run.Status.Suspend)
			r.Equal(true, run.Status.Terminated)
			r.Equal(tc.expected.Status, run.Status)
		})
	}
}

func TestResumeWorkflowRun(t *testing.T) {
	ctx := context.Background()

	testCases := map[string]struct {
		run      *v1alpha1.WorkflowRun
		expected *v1alpha1.WorkflowRun
	}{
		"not suspend": {
			run: &v1alpha1.WorkflowRun{
				ObjectMeta: metav1.ObjectMeta{
					Name: "suspend",
				},
				Status: v1alpha1.WorkflowRunStatus{
					Suspend: false,
				},
			},
			expected: &v1alpha1.WorkflowRun{
				Status: v1alpha1.WorkflowRunStatus{
					Suspend: false,
				},
			},
		},
		"suspend": {
			run: &v1alpha1.WorkflowRun{
				ObjectMeta: metav1.ObjectMeta{
					Name: "suspend",
				},
				Status: v1alpha1.WorkflowRunStatus{
					Suspend: true,
				},
			},
			expected: &v1alpha1.WorkflowRun{
				Status: v1alpha1.WorkflowRunStatus{
					Suspend: false,
				},
			},
		},
		"suspend step": {
			run: &v1alpha1.WorkflowRun{
				ObjectMeta: metav1.ObjectMeta{
					Name: "suspend-step",
				},
				Status: v1alpha1.WorkflowRunStatus{
					Suspend: true,
					Steps: []v1alpha1.WorkflowStepStatus{
						{
							StepStatus: v1alpha1.StepStatus{
								Type:  wfTypes.WorkflowStepTypeSuspend,
								Phase: v1alpha1.WorkflowStepPhaseRunning,
							},
							SubStepsStatus: []v1alpha1.StepStatus{
								{
									Type:  wfTypes.WorkflowStepTypeSuspend,
									Phase: v1alpha1.WorkflowStepPhaseRunning,
								},
							},
						},
					},
				},
			},
			expected: &v1alpha1.WorkflowRun{
				Status: v1alpha1.WorkflowRunStatus{
					Steps: []v1alpha1.WorkflowStepStatus{
						{
							StepStatus: v1alpha1.StepStatus{
								Type:  wfTypes.WorkflowStepTypeSuspend,
								Phase: v1alpha1.WorkflowStepPhaseSucceeded,
							},
							SubStepsStatus: []v1alpha1.StepStatus{
								{
									Type:  wfTypes.WorkflowStepTypeSuspend,
									Phase: v1alpha1.WorkflowStepPhaseSucceeded,
								},
							},
						},
					},
				},
			},
		},
	}
	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			r := require.New(t)
			err := cli.Create(ctx, tc.run)
			r.NoError(err)
			defer func() {
				err = cli.Delete(ctx, tc.run)
				r.NoError(err)
			}()
			operator := NewWorkflowRunOperator(cli, nil, tc.run)
			err = operator.Resume(ctx)
			r.NoError(err)
			run := &v1alpha1.WorkflowRun{}
			err = cli.Get(ctx, client.ObjectKey{Name: tc.run.Name}, run)
			r.NoError(err)
			r.Equal(false, run.Status.Suspend)
			r.Equal(tc.expected.Status, run.Status)
		})
	}
}

func TestRestartWorkflowRun(t *testing.T) {
	r := require.New(t)
	operator := NewWorkflowRunOperator(cli, nil, nil)
	err := operator.Restart(context.Background())
	r.Equal("Can not restart a WorkflowRun", err.Error())
}

func TestRollbackWorkflowRun(t *testing.T) {
	r := require.New(t)
	operator := NewWorkflowRunOperator(cli, nil, nil)
	err := operator.Rollback(context.Background())
	r.Equal("Can not rollback a WorkflowRun", err.Error())
}
