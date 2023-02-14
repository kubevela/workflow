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
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
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
		run         *v1alpha1.WorkflowRun
		step        string
		expected    *v1alpha1.WorkflowRun
		expectedErr string
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
				Status: v1alpha1.WorkflowRunStatus{},
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
		"step not found": {
			run: &v1alpha1.WorkflowRun{
				ObjectMeta: metav1.ObjectMeta{
					Name: "suspend",
				},
				Status: v1alpha1.WorkflowRunStatus{
					Suspend: true,
				},
			},
			step:        "not-found",
			expectedErr: "can not find step not-found",
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
		"resume the specific step": {
			step: "step1",
			run: &v1alpha1.WorkflowRun{
				ObjectMeta: metav1.ObjectMeta{
					Name: "resume-specific-step",
				},
				Status: v1alpha1.WorkflowRunStatus{
					Suspend: true,
					Steps: []v1alpha1.WorkflowStepStatus{
						{
							StepStatus: v1alpha1.StepStatus{
								Name:  "step1",
								Type:  wfTypes.WorkflowStepTypeSuspend,
								Phase: v1alpha1.WorkflowStepPhaseRunning,
							},
						},
						{
							StepStatus: v1alpha1.StepStatus{
								Name:  "step2",
								Type:  wfTypes.WorkflowStepTypeSuspend,
								Phase: v1alpha1.WorkflowStepPhaseRunning,
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
								Name:  "step1",
								Type:  wfTypes.WorkflowStepTypeSuspend,
								Phase: v1alpha1.WorkflowStepPhaseSucceeded,
							},
						},
						{
							StepStatus: v1alpha1.StepStatus{
								Name:  "step2",
								Type:  wfTypes.WorkflowStepTypeSuspend,
								Phase: v1alpha1.WorkflowStepPhaseRunning,
							},
						},
					},
				},
			},
		},
		"resume the specific sub step": {
			step: "sub-step1",
			run: &v1alpha1.WorkflowRun{
				ObjectMeta: metav1.ObjectMeta{
					Name: "resume-specific-sub-step",
				},
				Status: v1alpha1.WorkflowRunStatus{
					Suspend: true,
					Steps: []v1alpha1.WorkflowStepStatus{
						{
							StepStatus: v1alpha1.StepStatus{
								Name:  "step1",
								Type:  wfTypes.WorkflowStepTypeSuspend,
								Phase: v1alpha1.WorkflowStepPhaseRunning,
							},
							SubStepsStatus: []v1alpha1.StepStatus{
								{
									Name:  "sub-step1",
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
								Name:  "step1",
								Type:  wfTypes.WorkflowStepTypeSuspend,
								Phase: v1alpha1.WorkflowStepPhaseRunning,
							},
							SubStepsStatus: []v1alpha1.StepStatus{
								{
									Name:  "sub-step1",
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
			if tc.step == "" {
				operator := NewWorkflowRunOperator(cli, nil, tc.run)
				err = operator.Resume(ctx)
				if tc.expectedErr != "" {
					r.Error(err)
					r.Equal(tc.expectedErr, err.Error())
					return
				}
			} else {
				operator := NewWorkflowRunStepOperator(cli, nil, tc.run)
				err = operator.Resume(ctx, tc.step)
				if tc.expectedErr != "" {
					r.Error(err)
					r.Equal(tc.expectedErr, err.Error())
					return
				}
			}
			r.NoError(err)
			run := &v1alpha1.WorkflowRun{}
			err = cli.Get(ctx, client.ObjectKey{Name: tc.run.Name}, run)
			r.NoError(err)
			r.Equal(false, run.Status.Suspend)
			r.Equal(tc.expected.Status, run.Status, name)
		})
	}
}

func TestRollbackWorkflowRun(t *testing.T) {
	r := require.New(t)
	operator := NewWorkflowRunOperator(cli, nil, nil)
	err := operator.Rollback(context.Background())
	r.Equal("can not rollback a WorkflowRun", err.Error())
}

func TestRestartRunStep(t *testing.T) {
	ctx := context.Background()

	testCases := map[string]struct {
		run          *v1alpha1.WorkflowRun
		expected     *v1alpha1.WorkflowRun
		vars         map[string]any
		expectedVars string
		stepName     string
		expectedErr  string
	}{
		"no step name": {
			run: &v1alpha1.WorkflowRun{
				ObjectMeta: metav1.ObjectMeta{
					Name: "no-step-name",
				},
				Status: v1alpha1.WorkflowRunStatus{
					ContextBackend: &corev1.ObjectReference{
						Name:      "workflow-no-step-name-context",
						Namespace: "default",
					},
					Steps: []v1alpha1.WorkflowStepStatus{
						{
							StepStatus: v1alpha1.StepStatus{
								Name: "exist",
							},
						},
					},
				},
			},
			expected: &v1alpha1.WorkflowRun{
				ObjectMeta: metav1.ObjectMeta{
					Name: "no-step-name",
				},
				Status: v1alpha1.WorkflowRunStatus{},
			},
		},
		"not found": {
			run: &v1alpha1.WorkflowRun{
				ObjectMeta: metav1.ObjectMeta{
					Name: "not-found",
				},
				Status: v1alpha1.WorkflowRunStatus{},
			},
			expectedErr: "not found",
			stepName:    "not-found",
		},
		"not found2": {
			run: &v1alpha1.WorkflowRun{
				ObjectMeta: metav1.ObjectMeta{
					Name: "not-found",
				},
				Spec: v1alpha1.WorkflowRunSpec{
					WorkflowSpec: &v1alpha1.WorkflowSpec{
						Steps: []v1alpha1.WorkflowStep{},
					},
				},
				Status: v1alpha1.WorkflowRunStatus{},
			},
			expectedErr: "not found",
			stepName:    "not-found",
		},
		"step not failed": {
			run: &v1alpha1.WorkflowRun{
				ObjectMeta: metav1.ObjectMeta{
					Name: "step-not-failed",
				},
				Spec: v1alpha1.WorkflowRunSpec{
					WorkflowSpec: &v1alpha1.WorkflowSpec{
						Steps: []v1alpha1.WorkflowStep{},
					},
				},
				Status: v1alpha1.WorkflowRunStatus{
					Steps: []v1alpha1.WorkflowStepStatus{
						{
							StepStatus: v1alpha1.StepStatus{
								Name: "step-not-failed",
							},
						},
					},
				},
			},
			stepName:    "step-not-failed",
			expectedErr: "can not restart from a non-failed step",
		},
		"retry step in step-by-step": {
			stepName: "step2",
			vars: map[string]any{
				"step1-output1": "step1-output1",
				"step2-output1": "step2-output1",
				"step2-output2": "step2-output2",
				"step3-output1": "step3-output1",
				"step3-output2": "step3-output2",
			},
			expectedVars: "{\n\t\"step1-output1\": \"step1-output1\"\n}",
			run: &v1alpha1.WorkflowRun{
				ObjectMeta: metav1.ObjectMeta{
					Name: "retry-step",
				},
				Spec: v1alpha1.WorkflowRunSpec{
					WorkflowSpec: &v1alpha1.WorkflowSpec{
						Steps: []v1alpha1.WorkflowStep{
							{
								WorkflowStepBase: v1alpha1.WorkflowStepBase{
									Name: "step1",
									Outputs: v1alpha1.StepOutputs{
										{
											Name: "step1-output1",
										},
									},
								},
							},
							{
								WorkflowStepBase: v1alpha1.WorkflowStepBase{
									Name: "step2",
									Outputs: v1alpha1.StepOutputs{
										{
											Name: "step2-output1",
										},
										{
											Name: "step2-output2",
										},
									},
								},
							},
							{
								WorkflowStepBase: v1alpha1.WorkflowStepBase{
									Name: "step3",
									Outputs: v1alpha1.StepOutputs{
										{
											Name: "step3-output1",
										},
										{
											Name: "step3-output2",
										},
									},
								},
							},
						},
					},
				},
				Status: v1alpha1.WorkflowRunStatus{
					Terminated: true,
					Finished:   true,
					Suspend:    true,
					EndTime:    metav1.Time{Time: time.Now()},
					ContextBackend: &corev1.ObjectReference{
						Name:      "workflow-retry-step-context",
						Namespace: "default",
					},
					Mode: v1alpha1.WorkflowExecuteMode{
						Steps: v1alpha1.WorkflowModeStep,
					},
					Steps: []v1alpha1.WorkflowStepStatus{
						{
							StepStatus: v1alpha1.StepStatus{
								Name:  "step1",
								Phase: v1alpha1.WorkflowStepPhaseSucceeded,
							},
						},
						{
							StepStatus: v1alpha1.StepStatus{
								Name:  "step2",
								Phase: v1alpha1.WorkflowStepPhaseFailed,
							},
							SubStepsStatus: []v1alpha1.StepStatus{
								{
									Phase: v1alpha1.WorkflowStepPhaseRunning,
								},
							},
						},
						{
							StepStatus: v1alpha1.StepStatus{
								Name:  "step3",
								Phase: v1alpha1.WorkflowStepPhaseFailed,
							},
						},
					},
				},
			},
			expected: &v1alpha1.WorkflowRun{
				Status: v1alpha1.WorkflowRunStatus{
					Mode: v1alpha1.WorkflowExecuteMode{
						Steps: v1alpha1.WorkflowModeStep,
					},
					ContextBackend: &corev1.ObjectReference{
						Name:      "workflow-retry-step-context",
						Namespace: "default",
					},
					Steps: []v1alpha1.WorkflowStepStatus{
						{
							StepStatus: v1alpha1.StepStatus{
								Name:  "step1",
								Phase: v1alpha1.WorkflowStepPhaseSucceeded,
							},
						},
					},
				},
			},
		},
		"retry step in dag": {
			stepName: "step2",
			vars: map[string]any{
				"step2-output1": "step2-output1",
				"step2-output2": "step2-output2",
			},
			expectedVars: "{}",
			run: &v1alpha1.WorkflowRun{
				ObjectMeta: metav1.ObjectMeta{
					Name: "retry-step",
				},
				Spec: v1alpha1.WorkflowRunSpec{
					WorkflowSpec: &v1alpha1.WorkflowSpec{
						Steps: []v1alpha1.WorkflowStep{
							{
								WorkflowStepBase: v1alpha1.WorkflowStepBase{
									Name:      "step1",
									DependsOn: []string{"step3"},
								},
							},
							{
								WorkflowStepBase: v1alpha1.WorkflowStepBase{
									Name: "step2",
									Outputs: v1alpha1.StepOutputs{
										{
											Name: "step2-output1",
										},
										{
											Name: "step2-output2",
										},
									},
								},
							},
							{
								WorkflowStepBase: v1alpha1.WorkflowStepBase{
									Name: "step3",
									Inputs: v1alpha1.StepInputs{
										{
											From: "step2-output1",
										},
									},
								},
							},
						},
					},
				},
				Status: v1alpha1.WorkflowRunStatus{
					Terminated: true,
					Finished:   true,
					Suspend:    true,
					EndTime:    metav1.Time{Time: time.Now()},
					ContextBackend: &corev1.ObjectReference{
						Name:      "workflow-retry-step-context",
						Namespace: "default",
					},
					Mode: v1alpha1.WorkflowExecuteMode{
						Steps: v1alpha1.WorkflowModeDAG,
					},
					Steps: []v1alpha1.WorkflowStepStatus{
						{
							StepStatus: v1alpha1.StepStatus{
								Name:  "step1",
								Phase: v1alpha1.WorkflowStepPhaseSucceeded,
							},
						},
						{
							StepStatus: v1alpha1.StepStatus{
								Name:  "step2",
								Phase: v1alpha1.WorkflowStepPhaseFailed,
							},
							SubStepsStatus: []v1alpha1.StepStatus{
								{
									Phase: v1alpha1.WorkflowStepPhaseRunning,
								},
							},
						},
						{
							StepStatus: v1alpha1.StepStatus{
								Name:  "step3",
								Phase: v1alpha1.WorkflowStepPhaseFailed,
							},
						},
					},
				},
			},
			expected: &v1alpha1.WorkflowRun{
				Status: v1alpha1.WorkflowRunStatus{
					Mode: v1alpha1.WorkflowExecuteMode{
						Steps: v1alpha1.WorkflowModeDAG,
					},
					ContextBackend: &corev1.ObjectReference{
						Name:      "workflow-retry-step-context",
						Namespace: "default",
					},
				},
			},
		},
		"retry sub step in step-by-step": {
			stepName: "step2-sub2",
			vars: map[string]any{
				"step2-output1":      "step2-output1",
				"step2-output2":      "step2-output2",
				"step2-sub2-output1": "step2-sub2-output1",
			},
			expectedVars: "{\n\t\"step2-output1\": \"step2-output1\"\n\t\"step2-output2\": \"step2-output2\"\n}",
			run: &v1alpha1.WorkflowRun{
				ObjectMeta: metav1.ObjectMeta{
					Name: "retry-sub-step",
				},
				Spec: v1alpha1.WorkflowRunSpec{
					WorkflowSpec: &v1alpha1.WorkflowSpec{
						Steps: []v1alpha1.WorkflowStep{
							{
								WorkflowStepBase: v1alpha1.WorkflowStepBase{
									Name: "step1",
								},
							},
							{
								WorkflowStepBase: v1alpha1.WorkflowStepBase{
									Name: "step2",
									Outputs: v1alpha1.StepOutputs{
										{
											Name: "step2-output1",
										},
										{
											Name: "step2-output2",
										},
									},
								},
								SubSteps: []v1alpha1.WorkflowStepBase{
									{
										Name: "step2-sub1",
									},
									{
										Name: "step2-sub2",
										Outputs: v1alpha1.StepOutputs{
											{
												Name: "step2-sub2-output1",
											},
										},
									},
									{
										Name: "step2-sub3",
									},
								},
							},
							{
								WorkflowStepBase: v1alpha1.WorkflowStepBase{
									Name: "step3",
								},
							},
						},
					},
				},
				Status: v1alpha1.WorkflowRunStatus{
					Terminated: true,
					Finished:   true,
					Suspend:    true,
					EndTime:    metav1.Time{Time: time.Now()},
					Mode: v1alpha1.WorkflowExecuteMode{
						Steps:    v1alpha1.WorkflowModeStep,
						SubSteps: v1alpha1.WorkflowModeStep,
					},
					ContextBackend: &corev1.ObjectReference{
						Name:      "workflow-retry-sub-step-context",
						Namespace: "default",
					},
					Steps: []v1alpha1.WorkflowStepStatus{
						{
							StepStatus: v1alpha1.StepStatus{
								Name:  "step1",
								Phase: v1alpha1.WorkflowStepPhaseSucceeded,
							},
						},
						{
							StepStatus: v1alpha1.StepStatus{
								Name:  "step2",
								Phase: v1alpha1.WorkflowStepPhaseFailed,
							},
							SubStepsStatus: []v1alpha1.StepStatus{
								{
									Name:  "step2-sub1",
									Phase: v1alpha1.WorkflowStepPhaseFailed,
								},
								{
									Name:  "step2-sub2",
									Phase: v1alpha1.WorkflowStepPhaseFailed,
								},
								{
									Name:  "step2-sub3",
									Phase: v1alpha1.WorkflowStepPhaseFailed,
								},
							},
						},
						{
							StepStatus: v1alpha1.StepStatus{
								Name:  "step3",
								Phase: v1alpha1.WorkflowStepPhaseFailed,
							},
						},
					},
				},
			},
			expected: &v1alpha1.WorkflowRun{
				Status: v1alpha1.WorkflowRunStatus{
					Mode: v1alpha1.WorkflowExecuteMode{
						Steps:    v1alpha1.WorkflowModeStep,
						SubSteps: v1alpha1.WorkflowModeStep,
					},
					ContextBackend: &corev1.ObjectReference{
						Name:      "workflow-retry-sub-step-context",
						Namespace: "default",
					},
					Steps: []v1alpha1.WorkflowStepStatus{
						{
							StepStatus: v1alpha1.StepStatus{
								Name:  "step1",
								Phase: v1alpha1.WorkflowStepPhaseSucceeded,
							},
						},
						{
							StepStatus: v1alpha1.StepStatus{
								Name:  "step2",
								Phase: v1alpha1.WorkflowStepPhaseRunning,
							},
							SubStepsStatus: []v1alpha1.StepStatus{
								{
									Name:  "step2-sub1",
									Phase: v1alpha1.WorkflowStepPhaseFailed,
								},
							},
						},
					},
				},
			},
		},
		"retry sub step in dag": {
			stepName: "step2-sub2",
			vars: map[string]any{
				"step2-sub2-output1": "step2-sub2-output1",
			},
			expectedVars: "{}",
			run: &v1alpha1.WorkflowRun{
				ObjectMeta: metav1.ObjectMeta{
					Name: "retry-sub-step",
				},
				Spec: v1alpha1.WorkflowRunSpec{
					WorkflowSpec: &v1alpha1.WorkflowSpec{
						Steps: []v1alpha1.WorkflowStep{
							{
								WorkflowStepBase: v1alpha1.WorkflowStepBase{
									Name: "step1",
								},
							},
							{
								WorkflowStepBase: v1alpha1.WorkflowStepBase{
									Name: "step2",
								},
								SubSteps: []v1alpha1.WorkflowStepBase{
									{
										Name:      "step2-sub1",
										DependsOn: []string{"step2-sub2"},
									},
									{
										Name: "step2-sub2",
										Outputs: v1alpha1.StepOutputs{
											{
												Name: "step2-sub2-output1",
											},
										},
									},
									{
										Name: "step2-sub3",
										Inputs: v1alpha1.StepInputs{
											{
												From: "step2-sub2-output1",
											},
										},
									},
								},
							},
							{
								WorkflowStepBase: v1alpha1.WorkflowStepBase{
									Name:      "step3",
									DependsOn: []string{"step2"},
								},
							},
						},
					},
				},
				Status: v1alpha1.WorkflowRunStatus{
					Terminated: true,
					Finished:   true,
					Suspend:    true,
					EndTime:    metav1.Time{Time: time.Now()},
					ContextBackend: &corev1.ObjectReference{
						Name:      "workflow-retry-sub-step-context",
						Namespace: "default",
					},
					Mode: v1alpha1.WorkflowExecuteMode{
						Steps:    v1alpha1.WorkflowModeDAG,
						SubSteps: v1alpha1.WorkflowModeDAG,
					},
					Steps: []v1alpha1.WorkflowStepStatus{
						{
							StepStatus: v1alpha1.StepStatus{
								Name:  "step1",
								Phase: v1alpha1.WorkflowStepPhaseSucceeded,
							},
						},
						{
							StepStatus: v1alpha1.StepStatus{
								Name:  "step2",
								Phase: v1alpha1.WorkflowStepPhaseFailed,
							},
							SubStepsStatus: []v1alpha1.StepStatus{
								{
									Name:  "step2-sub1",
									Phase: v1alpha1.WorkflowStepPhaseFailed,
								},
								{
									Name:  "step2-sub2",
									Phase: v1alpha1.WorkflowStepPhaseFailed,
								},
								{
									Name:  "step2-sub3",
									Phase: v1alpha1.WorkflowStepPhaseFailed,
								},
							},
						},
						{
							StepStatus: v1alpha1.StepStatus{
								Name:  "step3",
								Phase: v1alpha1.WorkflowStepPhaseFailed,
							},
						},
					},
				},
			},
			expected: &v1alpha1.WorkflowRun{
				Status: v1alpha1.WorkflowRunStatus{
					Mode: v1alpha1.WorkflowExecuteMode{
						Steps:    v1alpha1.WorkflowModeDAG,
						SubSteps: v1alpha1.WorkflowModeDAG,
					},
					ContextBackend: &corev1.ObjectReference{
						Name:      "workflow-retry-sub-step-context",
						Namespace: "default",
					},
					Steps: []v1alpha1.WorkflowStepStatus{
						{
							StepStatus: v1alpha1.StepStatus{
								Name:  "step1",
								Phase: v1alpha1.WorkflowStepPhaseSucceeded,
							},
						},
						{
							StepStatus: v1alpha1.StepStatus{
								Name:  "step2",
								Phase: v1alpha1.WorkflowStepPhaseRunning,
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
			if tc.vars != nil {
				b, err := json.Marshal(tc.vars)
				r.NoError(err)
				cm := &corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Name:      tc.run.Status.ContextBackend.Name,
						Namespace: tc.run.Namespace,
					},
					Data: map[string]string{
						"vars": string(b),
					},
				}
				err = cli.Create(ctx, cm)
				r.NoError(err)
				defer func() {
					err = cli.Delete(ctx, cm)
					r.NoError(err)
				}()
			}
			if tc.stepName == "" {
				operator := NewWorkflowRunOperator(cli, nil, tc.run)
				err = operator.Restart(ctx)
				if tc.expectedErr != "" {
					r.Contains(err.Error(), tc.expectedErr)
					return
				}
				r.NoError(err)
			} else {
				operator := NewWorkflowRunStepOperator(cli, nil, tc.run)
				err = operator.Restart(ctx, tc.stepName)
				if tc.expectedErr != "" {
					r.Contains(err.Error(), tc.expectedErr)
					return
				}
				r.NoError(err)
			}
			run := &v1alpha1.WorkflowRun{}
			err = cli.Get(ctx, client.ObjectKey{Name: tc.run.Name}, run)
			r.NoError(err)
			r.Equal(false, run.Status.Suspend)
			r.Equal(false, run.Status.Terminated)
			r.Equal(false, run.Status.Finished)
			r.True(run.Status.EndTime.IsZero())
			r.Equal(tc.expected.Status, run.Status)
			if tc.vars != nil {
				cm := &corev1.ConfigMap{}
				err = cli.Get(ctx, client.ObjectKey{Name: tc.run.Status.ContextBackend.Name, Namespace: tc.run.Namespace}, cm)
				r.NoError(err)
				r.Equal(tc.expectedVars, cm.Data["vars"])
			}
		})
	}
}
