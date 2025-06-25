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

package executor

import (
	"context"
	"encoding/json"
	"math"
	"time"

	"github.com/kubevela/workflow/pkg/features"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	featuregatetesting "k8s.io/component-base/featuregate/testing"

	"cuelang.org/go/cue/cuecontext"
	"github.com/google/go-cmp/cmp"
	"github.com/kubevela/pkg/util/slices"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/yaml"

	monitorContext "github.com/kubevela/pkg/monitor/context"
	"github.com/kubevela/workflow/api/v1alpha1"
	wfContext "github.com/kubevela/workflow/pkg/context"
	"github.com/kubevela/workflow/pkg/cue/process"
	"github.com/kubevela/workflow/pkg/providers"
	"github.com/kubevela/workflow/pkg/tasks/builtin"
	"github.com/kubevela/workflow/pkg/tasks/custom"
	"github.com/kubevela/workflow/pkg/types"
)

var _ = Describe("Test Workflow", func() {

	defaultMode := v1alpha1.WorkflowExecuteMode{
		Steps:    v1alpha1.WorkflowModeStep,
		SubSteps: v1alpha1.WorkflowModeDAG,
	}

	dagMode := v1alpha1.WorkflowExecuteMode{
		Steps:    v1alpha1.WorkflowModeDAG,
		SubSteps: v1alpha1.WorkflowModeDAG,
	}

	BeforeEach(func() {
		cm := &corev1.ConfigMap{}
		cmJson, err := yaml.YAMLToJSON([]byte(cmYaml))
		Expect(err).ToNot(HaveOccurred())
		err = json.Unmarshal(cmJson, cm)
		Expect(err).ToNot(HaveOccurred())
		err = k8sClient.Create(context.Background(), cm)
		if err != nil && !kerrors.IsAlreadyExists(err) {
			Expect(err).ToNot(HaveOccurred())
		}
	})
	It("Workflow test for failed", func() {
		instance, runners := makeTestCase([]v1alpha1.WorkflowStep{
			{
				WorkflowStepBase: v1alpha1.WorkflowStepBase{
					Name: "s1",
					Type: "success",
				},
			},
			{
				WorkflowStepBase: v1alpha1.WorkflowStepBase{
					Name: "s2",
					Type: "failed",
				},
			},
			{
				WorkflowStepBase: v1alpha1.WorkflowStepBase{
					Name: "s3",
					Type: "success",
				},
			},
		})
		ctx := monitorContext.NewTraceContext(context.Background(), "test-app")
		wf := New(instance)
		state, err := wf.ExecuteRunners(ctx, runners)
		Expect(err).ToNot(HaveOccurred())
		Expect(state).Should(BeEquivalentTo(v1alpha1.WorkflowStateExecuting))
		workflowStatus := instance.Status
		Expect(workflowStatus.ContextBackend.Name).Should(BeEquivalentTo("workflow-" + instance.Name + "-context"))
		workflowStatus.ContextBackend = nil
		cleanStepTimeStamp(&workflowStatus)
		Expect(cmp.Diff(workflowStatus, v1alpha1.WorkflowRunStatus{
			Mode: defaultMode,
			Steps: []v1alpha1.WorkflowStepStatus{
				{
					StepStatus: v1alpha1.StepStatus{
						Name:  "s1",
						Type:  "success",
						Phase: v1alpha1.WorkflowStepPhaseSucceeded,
					},
				}, {
					StepStatus: v1alpha1.StepStatus{
						Name:  "s2",
						Type:  "failed",
						Phase: v1alpha1.WorkflowStepPhaseFailed,
					},
				},
			},
		})).Should(BeEquivalentTo(""))

		instance, runners = makeTestCase([]v1alpha1.WorkflowStep{
			{
				WorkflowStepBase: v1alpha1.WorkflowStepBase{
					Name: "s1",
					Type: "success",
				},
			},
			{
				WorkflowStepBase: v1alpha1.WorkflowStepBase{
					Name: "s2",
					Type: "success",
				},
			},
			{
				WorkflowStepBase: v1alpha1.WorkflowStepBase{
					Name: "s3",
					Type: "success",
				},
			},
		})

		instance.Status = v1alpha1.WorkflowRunStatus{}
		wf = New(instance)
		state, err = wf.ExecuteRunners(ctx, runners)
		Expect(err).ToNot(HaveOccurred())
		Expect(state).Should(BeEquivalentTo(v1alpha1.WorkflowStateSucceeded))
		instance.Status.ContextBackend = nil
		cleanStepTimeStamp(&instance.Status)
		Expect(cmp.Diff(instance.Status, v1alpha1.WorkflowRunStatus{
			Mode: defaultMode,
			Steps: []v1alpha1.WorkflowStepStatus{{
				StepStatus: v1alpha1.StepStatus{
					Name:  "s1",
					Type:  "success",
					Phase: v1alpha1.WorkflowStepPhaseSucceeded,
				},
			}, {
				StepStatus: v1alpha1.StepStatus{
					Name:  "s2",
					Type:  "success",
					Phase: v1alpha1.WorkflowStepPhaseSucceeded,
				},
			}, {
				StepStatus: v1alpha1.StepStatus{
					Name:  "s3",
					Type:  "success",
					Phase: v1alpha1.WorkflowStepPhaseSucceeded,
				},
			}},
		})).Should(BeEquivalentTo(""))
	})

	It("Workflow test failed with sub steps", func() {
		By("Test failed with step group")
		instance, runners := makeTestCase([]v1alpha1.WorkflowStep{
			{
				WorkflowStepBase: v1alpha1.WorkflowStepBase{
					Name: "s1",
					Type: "success",
				},
			},
			{
				WorkflowStepBase: v1alpha1.WorkflowStepBase{
					Name: "s2",
					Type: "step-group",
				},
				SubSteps: []v1alpha1.WorkflowStepBase{
					{
						Name: "s2-sub1",
						Type: "success",
					},
					{
						Name: "s2-sub2",
						Type: "failed",
					},
				},
			},
			{
				WorkflowStepBase: v1alpha1.WorkflowStepBase{
					Name: "s3",
					Type: "success",
				},
			},
		})
		wf := New(instance)
		ctx := monitorContext.NewTraceContext(context.Background(), "test-app")
		state, err := wf.ExecuteRunners(ctx, runners)
		Expect(err).ToNot(HaveOccurred())
		Expect(state).Should(BeEquivalentTo(v1alpha1.WorkflowStateExecuting))
		instance.Status.ContextBackend = nil
		cleanStepTimeStamp(&instance.Status)
		Expect(cmp.Diff(instance.Status, v1alpha1.WorkflowRunStatus{
			Mode: defaultMode,
			Steps: []v1alpha1.WorkflowStepStatus{{
				StepStatus: v1alpha1.StepStatus{
					Name:  "s1",
					Type:  "success",
					Phase: v1alpha1.WorkflowStepPhaseSucceeded,
				},
			}, {
				StepStatus: v1alpha1.StepStatus{
					Name:  "s2",
					Type:  "step-group",
					Phase: v1alpha1.WorkflowStepPhaseFailed,
				},
				SubStepsStatus: []v1alpha1.StepStatus{
					{
						Name:  "s2-sub1",
						Type:  "success",
						Phase: v1alpha1.WorkflowStepPhaseSucceeded,
					}, {
						Name:  "s2-sub2",
						Type:  "failed",
						Phase: v1alpha1.WorkflowStepPhaseFailed,
					},
				},
			}},
		})).Should(BeEquivalentTo(""))
	})

	It("Workflow test for timeout", func() {
		instance, runners := makeTestCase([]v1alpha1.WorkflowStep{
			{
				WorkflowStepBase: v1alpha1.WorkflowStepBase{
					Name: "s1",
					Type: "success",
				},
			},
			{
				WorkflowStepBase: v1alpha1.WorkflowStepBase{
					Name:    "s2",
					If:      "status.s1.succeeded",
					Type:    "running",
					Timeout: "1s",
				},
			},
			{
				WorkflowStepBase: v1alpha1.WorkflowStepBase{
					Name: "s3",
					Type: "success",
				},
			},
			{
				WorkflowStepBase: v1alpha1.WorkflowStepBase{
					Name: "s4",
					If:   "status.s2.timeout",
					Type: "success",
				},
			},
		})
		ctx := monitorContext.NewTraceContext(context.Background(), "test-app")
		wf := New(instance)
		state, err := wf.ExecuteRunners(ctx, runners)
		Expect(err).ToNot(HaveOccurred())
		Expect(state).Should(BeEquivalentTo(v1alpha1.WorkflowStateExecuting))
		time.Sleep(1 * time.Second)
		state, err = wf.ExecuteRunners(ctx, runners)
		Expect(err).ToNot(HaveOccurred())
		Expect(state).Should(BeEquivalentTo(v1alpha1.WorkflowStateFailed))
		workflowStatus := instance.Status
		Expect(workflowStatus.ContextBackend.Name).Should(BeEquivalentTo("workflow-" + instance.Name + "-context"))
		workflowStatus.ContextBackend = nil
		cleanStepTimeStamp(&workflowStatus)
		Expect(cmp.Diff(workflowStatus, v1alpha1.WorkflowRunStatus{
			Mode:       defaultMode,
			Terminated: true,
			Steps: []v1alpha1.WorkflowStepStatus{
				{
					StepStatus: v1alpha1.StepStatus{
						Name:  "s1",
						Type:  "success",
						Phase: v1alpha1.WorkflowStepPhaseSucceeded,
					},
				}, {
					StepStatus: v1alpha1.StepStatus{
						Name:   "s2",
						Type:   "running",
						Phase:  v1alpha1.WorkflowStepPhaseFailed,
						Reason: types.StatusReasonTimeout,
					},
				}, {
					StepStatus: v1alpha1.StepStatus{
						Name:   "s3",
						Type:   "success",
						Phase:  v1alpha1.WorkflowStepPhaseSkipped,
						Reason: types.StatusReasonSkip,
					},
				}, {
					StepStatus: v1alpha1.StepStatus{
						Name:  "s4",
						Type:  "success",
						Phase: v1alpha1.WorkflowStepPhaseSucceeded,
					},
				},
			},
		})).Should(BeEquivalentTo(""))
	})

	It("Workflow test for timeout with suspend", func() {
		instance, runners := makeTestCase([]v1alpha1.WorkflowStep{
			{
				WorkflowStepBase: v1alpha1.WorkflowStepBase{
					Name: "s1",
					Type: "success",
				},
			},
			{
				WorkflowStepBase: v1alpha1.WorkflowStepBase{
					Name:    "s2",
					If:      "status.s1.timeout",
					Type:    "suspend",
					Timeout: "1s",
				},
			},
			{
				WorkflowStepBase: v1alpha1.WorkflowStepBase{
					Name: "s3",
					Type: "success",
				},
			},
			{
				WorkflowStepBase: v1alpha1.WorkflowStepBase{
					Name:    "s4",
					If:      "status.s1.succeeded",
					Type:    "suspend",
					Timeout: "1s",
				},
			},
			{
				WorkflowStepBase: v1alpha1.WorkflowStepBase{
					Name: "s5",
					Type: "success",
				},
			},
		})
		ctx := monitorContext.NewTraceContext(context.Background(), "test-app")
		wf := New(instance)
		state, err := wf.ExecuteRunners(ctx, runners)
		Expect(err).ToNot(HaveOccurred())
		Expect(state).Should(BeEquivalentTo(v1alpha1.WorkflowStateSuspending))
		time.Sleep(1 * time.Second)
		state, err = wf.ExecuteRunners(ctx, runners)
		Expect(err).ToNot(HaveOccurred())
		Expect(state).Should(BeEquivalentTo(v1alpha1.WorkflowStateFailed))
		workflowStatus := instance.Status
		Expect(workflowStatus.ContextBackend.Name).Should(BeEquivalentTo("workflow-" + instance.Name + "-context"))
		workflowStatus.ContextBackend = nil
		cleanStepTimeStamp(&workflowStatus)
		Expect(cmp.Diff(workflowStatus, v1alpha1.WorkflowRunStatus{
			Mode:       defaultMode,
			Terminated: true,
			Steps: []v1alpha1.WorkflowStepStatus{
				{
					StepStatus: v1alpha1.StepStatus{
						Name:  "s1",
						Type:  "success",
						Phase: v1alpha1.WorkflowStepPhaseSucceeded,
					},
				}, {
					StepStatus: v1alpha1.StepStatus{
						Name:   "s2",
						Type:   "suspend",
						Phase:  v1alpha1.WorkflowStepPhaseSkipped,
						Reason: types.StatusReasonSkip,
					},
				}, {
					StepStatus: v1alpha1.StepStatus{
						Name:  "s3",
						Type:  "success",
						Phase: v1alpha1.WorkflowStepPhaseSucceeded,
					},
				}, {
					StepStatus: v1alpha1.StepStatus{
						Name:   "s4",
						Type:   "suspend",
						Phase:  v1alpha1.WorkflowStepPhaseFailed,
						Reason: types.StatusReasonTimeout,
					},
				}, {
					StepStatus: v1alpha1.StepStatus{
						Name:   "s5",
						Type:   "success",
						Phase:  v1alpha1.WorkflowStepPhaseSkipped,
						Reason: types.StatusReasonSkip,
					},
				},
			},
		})).Should(BeEquivalentTo(""))
	})

	It("Workflow test for timeout with sub steps", func() {
		instance, runners := makeTestCase([]v1alpha1.WorkflowStep{
			{
				WorkflowStepBase: v1alpha1.WorkflowStepBase{
					Name: "s1",
					Type: "success",
				},
			},
			{
				WorkflowStepBase: v1alpha1.WorkflowStepBase{
					Name: "s2",
					Type: "step-group",
				},
				SubSteps: []v1alpha1.WorkflowStepBase{
					{
						Name: "s2-sub1",
						Type: "success",
					},
					{
						Name:    "s2-sub2",
						Type:    "running",
						Timeout: "1s",
					},
					{
						Name:    "s2-suspend",
						Type:    "suspend",
						Timeout: "1s",
					},
				},
			},
			{
				WorkflowStepBase: v1alpha1.WorkflowStepBase{
					Name: "s3",
					Type: "success",
				},
			},
		})
		ctx := monitorContext.NewTraceContext(context.Background(), "test-app")
		wf := New(instance)
		state, err := wf.ExecuteRunners(ctx, runners)
		Expect(err).ToNot(HaveOccurred())
		Expect(state).Should(BeEquivalentTo(v1alpha1.WorkflowStateSuspending))
		time.Sleep(1 * time.Second)
		state, err = wf.ExecuteRunners(ctx, runners)
		Expect(err).ToNot(HaveOccurred())
		Expect(state).Should(BeEquivalentTo(v1alpha1.WorkflowStateFailed))
		workflowStatus := instance.Status
		Expect(workflowStatus.ContextBackend.Name).Should(BeEquivalentTo("workflow-" + instance.Name + "-context"))
		workflowStatus.ContextBackend = nil
		cleanStepTimeStamp(&workflowStatus)
		Expect(cmp.Diff(workflowStatus, v1alpha1.WorkflowRunStatus{
			Mode:       defaultMode,
			Terminated: true,
			Steps: []v1alpha1.WorkflowStepStatus{{
				StepStatus: v1alpha1.StepStatus{
					Name:  "s1",
					Type:  "success",
					Phase: v1alpha1.WorkflowStepPhaseSucceeded,
				},
			}, {
				StepStatus: v1alpha1.StepStatus{
					Name:   "s2",
					Type:   "step-group",
					Phase:  v1alpha1.WorkflowStepPhaseFailed,
					Reason: types.StatusReasonTimeout,
				},
				SubStepsStatus: []v1alpha1.StepStatus{
					{
						Name:  "s2-sub1",
						Type:  "success",
						Phase: v1alpha1.WorkflowStepPhaseSucceeded,
					}, {
						Name:   "s2-sub2",
						Type:   "running",
						Phase:  v1alpha1.WorkflowStepPhaseFailed,
						Reason: types.StatusReasonTimeout,
					}, {
						Name:   "s2-suspend",
						Type:   "suspend",
						Phase:  v1alpha1.WorkflowStepPhaseFailed,
						Reason: types.StatusReasonTimeout,
					},
				},
			}, {
				StepStatus: v1alpha1.StepStatus{
					Name:   "s3",
					Type:   "success",
					Phase:  v1alpha1.WorkflowStepPhaseSkipped,
					Reason: types.StatusReasonSkip,
				},
			}},
		})).Should(BeEquivalentTo(""))

		instance, runners = makeTestCase([]v1alpha1.WorkflowStep{
			{
				WorkflowStepBase: v1alpha1.WorkflowStepBase{
					Name: "s1",
					Type: "success",
				},
			},
			{
				WorkflowStepBase: v1alpha1.WorkflowStepBase{
					Name:    "s2",
					Type:    "step-group",
					Timeout: "1s",
				},
				SubSteps: []v1alpha1.WorkflowStepBase{
					{
						Name: "s2-sub1",
						Type: "success",
					},
					{
						Name: "s2-sub2",
						Type: "running",
					},
					{
						Name: "s2-suspend",
						Type: "suspend",
					},
				},
			},
			{
				WorkflowStepBase: v1alpha1.WorkflowStepBase{
					Name: "s3",
					Type: "success",
				},
			},
		})
		ctx = monitorContext.NewTraceContext(context.Background(), "test-app")
		wf = New(instance)
		state, err = wf.ExecuteRunners(ctx, runners)
		Expect(err).ToNot(HaveOccurred())
		Expect(state).Should(BeEquivalentTo(v1alpha1.WorkflowStateSuspending))
		time.Sleep(1 * time.Second)
		state, err = wf.ExecuteRunners(ctx, runners)
		Expect(err).ToNot(HaveOccurred())
		Expect(state).Should(BeEquivalentTo(v1alpha1.WorkflowStateFailed))
		workflowStatus = instance.Status
		Expect(workflowStatus.ContextBackend.Name).Should(BeEquivalentTo("workflow-" + instance.Name + "-context"))
		workflowStatus.ContextBackend = nil
		cleanStepTimeStamp(&workflowStatus)
		Expect(cmp.Diff(workflowStatus, v1alpha1.WorkflowRunStatus{
			Mode:       defaultMode,
			Terminated: true,
			Steps: []v1alpha1.WorkflowStepStatus{{
				StepStatus: v1alpha1.StepStatus{
					Name:  "s1",
					Type:  "success",
					Phase: v1alpha1.WorkflowStepPhaseSucceeded,
				},
			}, {
				StepStatus: v1alpha1.StepStatus{
					Name:   "s2",
					Type:   "step-group",
					Phase:  v1alpha1.WorkflowStepPhaseFailed,
					Reason: types.StatusReasonTimeout,
				},
				SubStepsStatus: []v1alpha1.StepStatus{
					{
						Name:  "s2-sub1",
						Type:  "success",
						Phase: v1alpha1.WorkflowStepPhaseSucceeded,
					}, {
						Name:   "s2-sub2",
						Type:   "running",
						Phase:  v1alpha1.WorkflowStepPhaseFailed,
						Reason: types.StatusReasonTimeout,
					}, {
						Name:   "s2-suspend",
						Type:   "suspend",
						Phase:  v1alpha1.WorkflowStepPhaseFailed,
						Reason: types.StatusReasonTimeout,
					},
				},
			}, {
				StepStatus: v1alpha1.StepStatus{
					Name:   "s3",
					Type:   "success",
					Phase:  v1alpha1.WorkflowStepPhaseSkipped,
					Reason: types.StatusReasonSkip,
				},
			}},
		})).Should(BeEquivalentTo(""))
	})

	It("Workflow test skipped with sub steps", func() {
		By("Test skipped with step group")
		instance, runners := makeTestCase([]v1alpha1.WorkflowStep{
			{
				WorkflowStepBase: v1alpha1.WorkflowStepBase{
					Name: "s1",
					Type: "failed-after-retries",
				},
			},
			{
				WorkflowStepBase: v1alpha1.WorkflowStepBase{
					Name: "s2",
					Type: "step-group",
				},
				SubSteps: []v1alpha1.WorkflowStepBase{
					{
						Name: "s2-sub1",
						Type: "success",
					},
					{
						Name: "s2-sub2",
						Type: "failed",
					},
				},
			},
			{
				WorkflowStepBase: v1alpha1.WorkflowStepBase{
					Name: "s3",
					Type: "success",
				},
			},
		})
		wf := New(instance)
		ctx := monitorContext.NewTraceContext(context.Background(), "test-app")
		state, err := wf.ExecuteRunners(ctx, runners)
		Expect(err).ToNot(HaveOccurred())
		Expect(state).Should(BeEquivalentTo(v1alpha1.WorkflowStateFailed))
		instance.Status.ContextBackend = nil
		cleanStepTimeStamp(&instance.Status)
		Expect(cmp.Diff(instance.Status, v1alpha1.WorkflowRunStatus{
			Mode:       defaultMode,
			Terminated: true,
			Steps: []v1alpha1.WorkflowStepStatus{{
				StepStatus: v1alpha1.StepStatus{
					Name:   "s1",
					Type:   "failed-after-retries",
					Phase:  v1alpha1.WorkflowStepPhaseFailed,
					Reason: types.StatusReasonFailedAfterRetries,
				},
			}, {
				StepStatus: v1alpha1.StepStatus{
					Name:   "s2",
					Type:   "step-group",
					Phase:  v1alpha1.WorkflowStepPhaseSkipped,
					Reason: types.StatusReasonSkip,
				},
				SubStepsStatus: []v1alpha1.StepStatus{
					{
						Name:   "s2-sub1",
						Type:   "success",
						Phase:  v1alpha1.WorkflowStepPhaseSkipped,
						Reason: types.StatusReasonSkip,
					}, {
						Name:   "s2-sub2",
						Type:   "failed",
						Phase:  v1alpha1.WorkflowStepPhaseSkipped,
						Reason: types.StatusReasonSkip,
					},
				},
			}, {
				StepStatus: v1alpha1.StepStatus{
					Name:   "s3",
					Type:   "success",
					Phase:  v1alpha1.WorkflowStepPhaseSkipped,
					Reason: types.StatusReasonSkip,
				},
			}},
		})).Should(BeEquivalentTo(""))
	})

	It("Workflow test if-always with sub steps", func() {
		By("Test if-always with step group")
		instance, runners := makeTestCase([]v1alpha1.WorkflowStep{
			{
				WorkflowStepBase: v1alpha1.WorkflowStepBase{
					Name: "s1",
					Type: "failed-after-retries",
				},
			},
			{
				WorkflowStepBase: v1alpha1.WorkflowStepBase{
					Name: "s2",
					If:   "always",
					Type: "step-group",
				},
				SubSteps: []v1alpha1.WorkflowStepBase{
					{
						Name:      "s2-sub1",
						DependsOn: []string{"s2-sub2"},
						If:        "always",
						Type:      "success",
					},
					{
						Name: "s2-sub2",
						Type: "failed-after-retries",
					},
					{
						Name: "s2-sub3",
						Type: "terminate",
					},
				},
			},
			{
				WorkflowStepBase: v1alpha1.WorkflowStepBase{
					Name: "s3",
					Type: "success",
				},
			},
		})
		wf := New(instance)
		ctx := monitorContext.NewTraceContext(context.Background(), "test-app")
		state, err := wf.ExecuteRunners(ctx, runners)
		Expect(err).ToNot(HaveOccurred())
		Expect(state).Should(BeEquivalentTo(v1alpha1.WorkflowStateFailed))
		instance.Status.ContextBackend = nil
		cleanStepTimeStamp(&instance.Status)
		Expect(cmp.Diff(instance.Status, v1alpha1.WorkflowRunStatus{
			Mode:       defaultMode,
			Terminated: true,
			Steps: []v1alpha1.WorkflowStepStatus{{
				StepStatus: v1alpha1.StepStatus{
					Name:   "s1",
					Type:   "failed-after-retries",
					Phase:  v1alpha1.WorkflowStepPhaseFailed,
					Reason: types.StatusReasonFailedAfterRetries,
				},
			}, {
				StepStatus: v1alpha1.StepStatus{
					Name:   "s2",
					Type:   "step-group",
					Phase:  v1alpha1.WorkflowStepPhaseFailed,
					Reason: types.StatusReasonFailedAfterRetries,
				},
				SubStepsStatus: []v1alpha1.StepStatus{
					{
						Name:  "s2-sub1",
						Type:  "success",
						Phase: v1alpha1.WorkflowStepPhaseSucceeded,
					}, {
						Name:   "s2-sub2",
						Type:   "failed-after-retries",
						Phase:  v1alpha1.WorkflowStepPhaseFailed,
						Reason: types.StatusReasonFailedAfterRetries,
					}, {
						Name:   "s2-sub3",
						Type:   "terminate",
						Phase:  v1alpha1.WorkflowStepPhaseFailed,
						Reason: types.StatusReasonTerminate,
					},
				},
			}, {
				StepStatus: v1alpha1.StepStatus{
					Name:   "s3",
					Type:   "success",
					Phase:  v1alpha1.WorkflowStepPhaseSkipped,
					Reason: types.StatusReasonSkip,
				},
			}},
		})).Should(BeEquivalentTo(""))
	})

	It("Workflow test success with sub steps", func() {
		By("Test success with step group")
		instance, runners := makeTestCase([]v1alpha1.WorkflowStep{
			{
				WorkflowStepBase: v1alpha1.WorkflowStepBase{
					Name: "s1",
					Type: "success",
				},
			},
			{
				WorkflowStepBase: v1alpha1.WorkflowStepBase{
					Name: "s2",
					Type: "step-group",
				},
				SubSteps: []v1alpha1.WorkflowStepBase{
					{
						Name: "s2-sub1",
						Type: "success",
					},
					{
						Name: "s2-sub2",
						Type: "success",
					},
				},
			},
			{
				WorkflowStepBase: v1alpha1.WorkflowStepBase{
					Name: "s3",
					Type: "success",
				},
			},
		})
		wf := New(instance)
		ctx := monitorContext.NewTraceContext(context.Background(), "test-app")
		state, err := wf.ExecuteRunners(ctx, runners)
		Expect(err).ToNot(HaveOccurred())
		Expect(state).Should(BeEquivalentTo(v1alpha1.WorkflowStateSucceeded))
		instance.Status.ContextBackend = nil
		cleanStepTimeStamp(&instance.Status)
		Expect(cmp.Diff(instance.Status, v1alpha1.WorkflowRunStatus{
			Mode: defaultMode,
			Steps: []v1alpha1.WorkflowStepStatus{{
				StepStatus: v1alpha1.StepStatus{
					Name:  "s1",
					Type:  "success",
					Phase: v1alpha1.WorkflowStepPhaseSucceeded,
				},
			}, {
				StepStatus: v1alpha1.StepStatus{
					Name:  "s2",
					Type:  "step-group",
					Phase: v1alpha1.WorkflowStepPhaseSucceeded,
				},
				SubStepsStatus: []v1alpha1.StepStatus{
					{
						Name:  "s2-sub1",
						Type:  "success",
						Phase: v1alpha1.WorkflowStepPhaseSucceeded,
					}, {
						Name:  "s2-sub2",
						Type:  "success",
						Phase: v1alpha1.WorkflowStepPhaseSucceeded,
					},
				},
			}, {
				StepStatus: v1alpha1.StepStatus{
					Name:  "s3",
					Type:  "success",
					Phase: v1alpha1.WorkflowStepPhaseSucceeded,
				},
			}},
		})).Should(BeEquivalentTo(""))

		By("Test success with step group and empty subSteps")
		instance, runners = makeTestCase([]v1alpha1.WorkflowStep{
			{
				WorkflowStepBase: v1alpha1.WorkflowStepBase{
					Name: "s1",
					Type: "success",
				},
			},
			{
				WorkflowStepBase: v1alpha1.WorkflowStepBase{
					Name: "s2",
					Type: "step-group",
				},
				SubSteps: []v1alpha1.WorkflowStepBase{},
			},
			{
				WorkflowStepBase: v1alpha1.WorkflowStepBase{
					Name: "s3",
					Type: "success",
				},
			},
		})
		wf = New(instance)
		ctx = monitorContext.NewTraceContext(context.Background(), "test-app")
		state, err = wf.ExecuteRunners(ctx, runners)
		Expect(err).ToNot(HaveOccurred())
		Expect(state).Should(BeEquivalentTo(v1alpha1.WorkflowStateSucceeded))
		instance.Status.ContextBackend = nil
		cleanStepTimeStamp(&instance.Status)
		Expect(cmp.Diff(instance.Status, v1alpha1.WorkflowRunStatus{
			Mode: defaultMode,
			Steps: []v1alpha1.WorkflowStepStatus{{
				StepStatus: v1alpha1.StepStatus{
					Name:  "s1",
					Type:  "success",
					Phase: v1alpha1.WorkflowStepPhaseSucceeded,
				},
			}, {
				StepStatus: v1alpha1.StepStatus{
					Name:  "s2",
					Type:  "step-group",
					Phase: v1alpha1.WorkflowStepPhaseSucceeded,
				},
			}, {
				StepStatus: v1alpha1.StepStatus{
					Name:  "s3",
					Type:  "success",
					Phase: v1alpha1.WorkflowStepPhaseSucceeded,
				},
			}},
		})).Should(BeEquivalentTo(""))
	})

	It("Workflow test for failed after retries with suspend", func() {
		By("Test failed-after-retries in StepByStep mode with suspend")
		featuregatetesting.SetFeatureGateDuringTest(GinkgoT(), utilfeature.DefaultFeatureGate, features.EnableSuspendOnFailure, true)
		instance, runners := makeTestCase([]v1alpha1.WorkflowStep{
			{
				WorkflowStepBase: v1alpha1.WorkflowStepBase{
					Name: "s1",
					Type: "success",
				},
			},
			{
				WorkflowStepBase: v1alpha1.WorkflowStepBase{
					Name: "s2",
					Type: "failed-after-retries",
				},
			},
			{
				WorkflowStepBase: v1alpha1.WorkflowStepBase{
					Name: "s3",
					Type: "success",
				},
			},
		})
		ctx := monitorContext.NewTraceContext(context.Background(), "test-app")
		wf := New(instance)
		state, err := wf.ExecuteRunners(ctx, runners)
		Expect(err).ToNot(HaveOccurred())
		Expect(state).Should(BeEquivalentTo(v1alpha1.WorkflowStateSuspending))
		workflowStatus := instance.Status
		Expect(workflowStatus.ContextBackend.Name).Should(BeEquivalentTo("workflow-" + instance.Name + "-context"))
		workflowStatus.ContextBackend = nil
		cleanStepTimeStamp(&workflowStatus)
		Expect(cmp.Diff(workflowStatus, v1alpha1.WorkflowRunStatus{
			Mode:    defaultMode,
			Message: types.MessageSuspendFailedAfterRetries,
			Suspend: true,
			Steps: []v1alpha1.WorkflowStepStatus{{
				StepStatus: v1alpha1.StepStatus{
					Name:  "s1",
					Type:  "success",
					Phase: v1alpha1.WorkflowStepPhaseSucceeded,
				},
			}, {
				StepStatus: v1alpha1.StepStatus{
					Name:   "s2",
					Type:   "failed-after-retries",
					Phase:  v1alpha1.WorkflowStepPhaseFailed,
					Reason: types.StatusReasonFailedAfterRetries,
				},
			}},
		})).Should(BeEquivalentTo(""))

		By("Test failed-after-retries in DAG mode with suspend")
		instance, runners = makeTestCase([]v1alpha1.WorkflowStep{
			{
				WorkflowStepBase: v1alpha1.WorkflowStepBase{
					Name: "s1",
					Type: "success",
				},
			},
			{
				WorkflowStepBase: v1alpha1.WorkflowStepBase{
					Name: "s2",
					Type: "failed-after-retries",
				},
			},
			{
				WorkflowStepBase: v1alpha1.WorkflowStepBase{
					Name: "s3",
					Type: "success",
				},
			},
		})
		instance.Mode = &v1alpha1.WorkflowExecuteMode{
			Steps: v1alpha1.WorkflowModeDAG,
		}
		ctx = monitorContext.NewTraceContext(context.Background(), "test-app")
		wf = New(instance)
		state, err = wf.ExecuteRunners(ctx, runners)
		Expect(err).ToNot(HaveOccurred())
		Expect(state).Should(BeEquivalentTo(v1alpha1.WorkflowStateSuspending))
		workflowStatus = instance.Status
		Expect(workflowStatus.ContextBackend.Name).Should(BeEquivalentTo("workflow-" + instance.Name + "-context"))
		workflowStatus.ContextBackend = nil
		cleanStepTimeStamp(&workflowStatus)
		Expect(cmp.Diff(workflowStatus, v1alpha1.WorkflowRunStatus{
			Mode:    dagMode,
			Message: types.MessageSuspendFailedAfterRetries,
			Suspend: true,
			Steps: []v1alpha1.WorkflowStepStatus{{
				StepStatus: v1alpha1.StepStatus{
					Name:  "s1",
					Type:  "success",
					Phase: v1alpha1.WorkflowStepPhaseSucceeded,
				},
			}, {
				StepStatus: v1alpha1.StepStatus{
					Name:   "s2",
					Type:   "failed-after-retries",
					Phase:  v1alpha1.WorkflowStepPhaseFailed,
					Reason: types.StatusReasonFailedAfterRetries,
				},
			}, {
				StepStatus: v1alpha1.StepStatus{
					Name:  "s3",
					Type:  "success",
					Phase: v1alpha1.WorkflowStepPhaseSucceeded,
				},
			}},
		})).Should(BeEquivalentTo(""))
	})

	It("Workflow test if always", func() {
		By("Test if always in StepByStep mode")
		instance, runners := makeTestCase([]v1alpha1.WorkflowStep{
			{
				WorkflowStepBase: v1alpha1.WorkflowStepBase{
					Name: "s1",
					Type: "success",
				},
			},
			{
				WorkflowStepBase: v1alpha1.WorkflowStepBase{
					Name: "s2",
					Type: "failed-after-retries",
				},
			},
			{
				WorkflowStepBase: v1alpha1.WorkflowStepBase{
					Name: "s3",
					If:   "always",
					Type: "success",
				},
			},
			{
				WorkflowStepBase: v1alpha1.WorkflowStepBase{
					Name: "s4",
					Type: "success",
				},
			},
			{
				WorkflowStepBase: v1alpha1.WorkflowStepBase{
					Name: "s5",
					If:   "always",
					Type: "success",
				},
			},
		})
		ctx := monitorContext.NewTraceContext(context.Background(), "test-app")
		wf := New(instance)
		state, err := wf.ExecuteRunners(ctx, runners)
		Expect(err).ToNot(HaveOccurred())
		Expect(state).Should(BeEquivalentTo(v1alpha1.WorkflowStateFailed))
		workflowStatus := instance.Status
		Expect(workflowStatus.ContextBackend.Name).Should(BeEquivalentTo("workflow-" + instance.Name + "-context"))
		workflowStatus.ContextBackend = nil
		cleanStepTimeStamp(&workflowStatus)
		Expect(cmp.Diff(workflowStatus, v1alpha1.WorkflowRunStatus{
			Mode:       defaultMode,
			Suspend:    false,
			Terminated: true,
			Steps: []v1alpha1.WorkflowStepStatus{{
				StepStatus: v1alpha1.StepStatus{
					Name:  "s1",
					Type:  "success",
					Phase: v1alpha1.WorkflowStepPhaseSucceeded,
				},
			}, {
				StepStatus: v1alpha1.StepStatus{
					Name:   "s2",
					Type:   "failed-after-retries",
					Phase:  v1alpha1.WorkflowStepPhaseFailed,
					Reason: types.StatusReasonFailedAfterRetries,
				},
			}, {
				StepStatus: v1alpha1.StepStatus{
					Name:  "s3",
					Type:  "success",
					Phase: v1alpha1.WorkflowStepPhaseSucceeded,
				},
			}, {
				StepStatus: v1alpha1.StepStatus{
					Name:   "s4",
					Type:   "success",
					Phase:  v1alpha1.WorkflowStepPhaseSkipped,
					Reason: types.StatusReasonSkip,
				},
			}, {
				StepStatus: v1alpha1.StepStatus{
					Name:  "s5",
					Type:  "success",
					Phase: v1alpha1.WorkflowStepPhaseSucceeded,
				},
			}},
		})).Should(BeEquivalentTo(""))

		By("Test if always in DAG mode")
		instance, runners = makeTestCase([]v1alpha1.WorkflowStep{
			{
				WorkflowStepBase: v1alpha1.WorkflowStepBase{
					Name: "s1",
					Type: "success",
				},
			},
			{
				WorkflowStepBase: v1alpha1.WorkflowStepBase{
					Name: "s2",
					Type: "failed-after-retries",
				},
			},
			{
				WorkflowStepBase: v1alpha1.WorkflowStepBase{
					Name:      "s3",
					DependsOn: []string{"s2"},
					If:        "always",
					Type:      "success",
				},
			},
			{
				WorkflowStepBase: v1alpha1.WorkflowStepBase{
					Name:      "s4",
					DependsOn: []string{"s3"},
					Type:      "success",
				},
			},
			{
				WorkflowStepBase: v1alpha1.WorkflowStepBase{
					Name:      "s5",
					DependsOn: []string{"s3"},
					If:        "always",
					Type:      "success",
				},
			},
			{
				WorkflowStepBase: v1alpha1.WorkflowStepBase{
					Name:      "s6",
					DependsOn: []string{"s1", "s5"},
					Type:      "success",
				},
			},
		})
		instance.Mode = &v1alpha1.WorkflowExecuteMode{
			Steps: v1alpha1.WorkflowModeDAG,
		}
		ctx = monitorContext.NewTraceContext(context.Background(), "test-app")
		wf = New(instance)
		state, err = wf.ExecuteRunners(ctx, runners)
		Expect(err).ToNot(HaveOccurred())
		Expect(state).Should(BeEquivalentTo(v1alpha1.WorkflowStateFailed))
		workflowStatus = instance.Status
		Expect(workflowStatus.ContextBackend.Name).Should(BeEquivalentTo("workflow-" + instance.Name + "-context"))
		workflowStatus.ContextBackend = nil
		cleanStepTimeStamp(&workflowStatus)
		Expect(cmp.Diff(workflowStatus, v1alpha1.WorkflowRunStatus{
			Mode:       dagMode,
			Terminated: true,
			Steps: []v1alpha1.WorkflowStepStatus{{
				StepStatus: v1alpha1.StepStatus{
					Name:  "s1",
					Type:  "success",
					Phase: v1alpha1.WorkflowStepPhaseSucceeded,
				},
			}, {
				StepStatus: v1alpha1.StepStatus{
					Name:   "s2",
					Type:   "failed-after-retries",
					Phase:  v1alpha1.WorkflowStepPhaseFailed,
					Reason: types.StatusReasonFailedAfterRetries,
				},
			}, {
				StepStatus: v1alpha1.StepStatus{
					Name:  "s3",
					Type:  "success",
					Phase: v1alpha1.WorkflowStepPhaseSucceeded,
				},
			}, {
				StepStatus: v1alpha1.StepStatus{
					Name:   "s4",
					Type:   "success",
					Phase:  v1alpha1.WorkflowStepPhaseSkipped,
					Reason: types.StatusReasonSkip,
				},
			}, {
				StepStatus: v1alpha1.StepStatus{
					Name:  "s5",
					Type:  "success",
					Phase: v1alpha1.WorkflowStepPhaseSucceeded,
				},
			}, {
				StepStatus: v1alpha1.StepStatus{
					Name:   "s6",
					Type:   "success",
					Phase:  v1alpha1.WorkflowStepPhaseSkipped,
					Reason: types.StatusReasonSkip,
				},
			}},
		})).Should(BeEquivalentTo(""))
	})

	It("Workflow test if expressions", func() {
		By("Test if expressions in StepByStep mode")
		instance, runners := makeTestCase([]v1alpha1.WorkflowStep{
			{
				WorkflowStepBase: v1alpha1.WorkflowStepBase{
					Name: "s1",
					Type: "success",
				},
			},
			{
				WorkflowStepBase: v1alpha1.WorkflowStepBase{
					Name: "s2",
					If:   "status.s1.failed",
					Type: "success",
				},
			},
			{
				WorkflowStepBase: v1alpha1.WorkflowStepBase{
					Name: "s3",
					If:   "status.s1.succeeded",
					Type: "success",
					Outputs: v1alpha1.StepOutputs{
						{
							Name:      "test",
							ValueFrom: "context.name",
						},
					},
				},
			},
			{
				WorkflowStepBase: v1alpha1.WorkflowStepBase{
					Name: "s4",
					Inputs: v1alpha1.StepInputs{
						{
							From:         "test",
							ParameterKey: "",
						},
					},
					If:   `inputs.test == "app"`,
					Type: "success",
				},
			},
		})
		ctx := monitorContext.NewTraceContext(context.Background(), "test-app")
		wf := New(instance)
		state, err := wf.ExecuteRunners(ctx, runners)
		Expect(err).ToNot(HaveOccurred())
		Expect(state).Should(BeEquivalentTo(v1alpha1.WorkflowStateSucceeded))
		workflowStatus := instance.Status
		Expect(workflowStatus.ContextBackend.Name).Should(BeEquivalentTo("workflow-" + instance.Name + "-context"))
		workflowStatus.ContextBackend = nil
		cleanStepTimeStamp(&workflowStatus)
		Expect(cmp.Diff(workflowStatus, v1alpha1.WorkflowRunStatus{
			Mode:    defaultMode,
			Suspend: false,
			Steps: []v1alpha1.WorkflowStepStatus{{
				StepStatus: v1alpha1.StepStatus{
					Name:  "s1",
					Type:  "success",
					Phase: v1alpha1.WorkflowStepPhaseSucceeded,
				},
			}, {
				StepStatus: v1alpha1.StepStatus{
					Name:   "s2",
					Type:   "success",
					Phase:  v1alpha1.WorkflowStepPhaseSkipped,
					Reason: types.StatusReasonSkip,
				},
			}, {
				StepStatus: v1alpha1.StepStatus{
					Name:  "s3",
					Type:  "success",
					Phase: v1alpha1.WorkflowStepPhaseSucceeded,
				},
			}, {
				StepStatus: v1alpha1.StepStatus{
					Name:  "s4",
					Type:  "success",
					Phase: v1alpha1.WorkflowStepPhaseSucceeded,
				},
			}},
		})).Should(BeEquivalentTo(""))

		By("Test if expressions in DAG mode")
		instance, runners = makeTestCase([]v1alpha1.WorkflowStep{
			{
				WorkflowStepBase: v1alpha1.WorkflowStepBase{
					Name: "s1",
					Type: "success",
				},
			},
			{
				WorkflowStepBase: v1alpha1.WorkflowStepBase{
					Name: "s2",
					If:   "status.s1.failed",
					Type: "success",
				},
			},
			{
				WorkflowStepBase: v1alpha1.WorkflowStepBase{
					Name:      "s3",
					DependsOn: []string{"s2"},
					If:        "status.s1.succeeded",
					Type:      "success",
					Outputs: v1alpha1.StepOutputs{
						{
							Name:      "test",
							ValueFrom: "context.name",
						},
					},
				},
			},
			{
				WorkflowStepBase: v1alpha1.WorkflowStepBase{
					Name:      "s4",
					DependsOn: []string{"s3"},
					Inputs: v1alpha1.StepInputs{
						{
							From:         "test",
							ParameterKey: "",
						},
					},
					If:   `inputs.test == "app"`,
					Type: "success",
				},
			},
		})
		instance.Mode = &v1alpha1.WorkflowExecuteMode{
			Steps: v1alpha1.WorkflowModeDAG,
		}
		ctx = monitorContext.NewTraceContext(context.Background(), "test-app")
		wf = New(instance)
		state, err = wf.ExecuteRunners(ctx, runners)
		Expect(err).ToNot(HaveOccurred())
		Expect(state).Should(BeEquivalentTo(v1alpha1.WorkflowStateSucceeded))
		workflowStatus = instance.Status
		Expect(workflowStatus.ContextBackend.Name).Should(BeEquivalentTo("workflow-" + instance.Name + "-context"))
		workflowStatus.ContextBackend = nil
		cleanStepTimeStamp(&workflowStatus)
		Expect(cmp.Diff(workflowStatus, v1alpha1.WorkflowRunStatus{
			Mode: dagMode,
			Steps: []v1alpha1.WorkflowStepStatus{{
				StepStatus: v1alpha1.StepStatus{
					Name:  "s1",
					Type:  "success",
					Phase: v1alpha1.WorkflowStepPhaseSucceeded,
				},
			}, {
				StepStatus: v1alpha1.StepStatus{
					Name:   "s2",
					Type:   "success",
					Phase:  v1alpha1.WorkflowStepPhaseSkipped,
					Reason: types.StatusReasonSkip,
				},
			}, {
				StepStatus: v1alpha1.StepStatus{
					Name:  "s3",
					Type:  "success",
					Phase: v1alpha1.WorkflowStepPhaseSucceeded,
				},
			}, {
				StepStatus: v1alpha1.StepStatus{
					Name:  "s4",
					Type:  "success",
					Phase: v1alpha1.WorkflowStepPhaseSucceeded,
				},
			}},
		})).Should(BeEquivalentTo(""))
	})

	It("Workflow test if expressions with sub steps", func() {
		By("Test if expressions with step group")
		instance, runners := makeTestCase([]v1alpha1.WorkflowStep{
			{
				WorkflowStepBase: v1alpha1.WorkflowStepBase{
					Name: "s1",
					Type: "success",
				},
			},
			{
				WorkflowStepBase: v1alpha1.WorkflowStepBase{
					Name: "s2",
					If:   "status.s1.timeout",
					Type: "step-group",
				},
				SubSteps: []v1alpha1.WorkflowStepBase{
					{
						Name: "s2_sub1",
						If:   "always",
						Type: "success",
					},
					{
						Name: "s2_sub2",
						Type: "failed-after-retries",
					},
				},
			},
			{
				WorkflowStepBase: v1alpha1.WorkflowStepBase{
					Name: "s3",
					If:   "status.s1.succeeded",
					Type: "step-group",
				},
				SubSteps: []v1alpha1.WorkflowStepBase{
					{
						Name: "s3_sub1",
						If:   "status.s2_sub1.skipped",
						Type: "success",
					},
					{
						Name: "s3_sub2",
						Type: "failed-after-retries",
					},
				},
			},
		})
		wf := New(instance)
		ctx := monitorContext.NewTraceContext(context.Background(), "test-app")
		state, err := wf.ExecuteRunners(ctx, runners)
		Expect(err).ToNot(HaveOccurred())
		Expect(state).Should(BeEquivalentTo(v1alpha1.WorkflowStateFailed))
		instance.Status.ContextBackend = nil
		cleanStepTimeStamp(&instance.Status)
		Expect(cmp.Diff(instance.Status, v1alpha1.WorkflowRunStatus{
			Mode:       defaultMode,
			Terminated: true,
			Steps: []v1alpha1.WorkflowStepStatus{{
				StepStatus: v1alpha1.StepStatus{
					Name:  "s1",
					Type:  "success",
					Phase: v1alpha1.WorkflowStepPhaseSucceeded,
				},
			}, {
				StepStatus: v1alpha1.StepStatus{
					Name:   "s2",
					Type:   "step-group",
					Phase:  v1alpha1.WorkflowStepPhaseSkipped,
					Reason: types.StatusReasonSkip,
				},
				SubStepsStatus: []v1alpha1.StepStatus{
					{
						Name:   "s2_sub1",
						Type:   "success",
						Phase:  v1alpha1.WorkflowStepPhaseSkipped,
						Reason: types.StatusReasonSkip,
					}, {
						Name:   "s2_sub2",
						Type:   "failed-after-retries",
						Phase:  v1alpha1.WorkflowStepPhaseSkipped,
						Reason: types.StatusReasonSkip,
					},
				},
			}, {
				StepStatus: v1alpha1.StepStatus{
					Name:   "s3",
					Type:   "step-group",
					Phase:  v1alpha1.WorkflowStepPhaseFailed,
					Reason: types.StatusReasonFailedAfterRetries,
				},
				SubStepsStatus: []v1alpha1.StepStatus{
					{
						Name:  "s3_sub1",
						Type:  "success",
						Phase: v1alpha1.WorkflowStepPhaseSucceeded,
					}, {
						Name:   "s3_sub2",
						Type:   "failed-after-retries",
						Phase:  v1alpha1.WorkflowStepPhaseFailed,
						Reason: types.StatusReasonFailedAfterRetries,
					},
				},
			}},
		})).Should(BeEquivalentTo(""))
	})

	It("Test failed after retries with sub steps", func() {
		By("Test failed-after-retries with step group in StepByStep mode")
		featuregatetesting.SetFeatureGateDuringTest(GinkgoT(), utilfeature.DefaultFeatureGate, features.EnableSuspendOnFailure, true)
		instance, runners := makeTestCase([]v1alpha1.WorkflowStep{
			{
				WorkflowStepBase: v1alpha1.WorkflowStepBase{
					Name: "s1",
					Type: "success",
				},
			},
			{
				WorkflowStepBase: v1alpha1.WorkflowStepBase{
					Name: "s2",
					Type: "step-group",
				},
				SubSteps: []v1alpha1.WorkflowStepBase{
					{
						Name: "s2-sub1",
						Type: "success",
					},
					{
						Name: "s2-sub2",
						Type: "failed-after-retries",
					},
				},
			},
			{
				WorkflowStepBase: v1alpha1.WorkflowStepBase{
					Name: "s3",
					Type: "success",
				},
			},
		})
		wf := New(instance)
		ctx := monitorContext.NewTraceContext(context.Background(), "test-app")
		state, err := wf.ExecuteRunners(ctx, runners)
		Expect(err).ToNot(HaveOccurred())
		Expect(state).Should(BeEquivalentTo(v1alpha1.WorkflowStateSuspending))
		instance.Status.ContextBackend = nil
		cleanStepTimeStamp(&instance.Status)
		Expect(cmp.Diff(instance.Status, v1alpha1.WorkflowRunStatus{
			Mode:    defaultMode,
			Message: types.MessageSuspendFailedAfterRetries,
			Suspend: true,
			Steps: []v1alpha1.WorkflowStepStatus{{
				StepStatus: v1alpha1.StepStatus{
					Name:  "s1",
					Type:  "success",
					Phase: v1alpha1.WorkflowStepPhaseSucceeded,
				},
			}, {
				StepStatus: v1alpha1.StepStatus{
					Name:   "s2",
					Type:   "step-group",
					Phase:  v1alpha1.WorkflowStepPhaseFailed,
					Reason: types.StatusReasonFailedAfterRetries,
				},
				SubStepsStatus: []v1alpha1.StepStatus{
					{
						Name:  "s2-sub1",
						Type:  "success",
						Phase: v1alpha1.WorkflowStepPhaseSucceeded,
					}, {
						Name:   "s2-sub2",
						Type:   "failed-after-retries",
						Phase:  v1alpha1.WorkflowStepPhaseFailed,
						Reason: types.StatusReasonFailedAfterRetries,
					},
				},
			}},
		})).Should(BeEquivalentTo(""))
	})

	It("Test get backoff time and clean", func() {
		instance, runners := makeTestCase([]v1alpha1.WorkflowStep{
			{
				WorkflowStepBase: v1alpha1.WorkflowStepBase{
					Name: "s1",
					Type: "wait-with-set-var",
				},
			},
		})
		ctx := monitorContext.NewTraceContext(context.Background(), "test-app")
		wf := New(instance)
		_, err := wf.ExecuteRunners(ctx, runners)
		Expect(err).ToNot(HaveOccurred())
		wfCtx, err := wfContext.LoadContext(ctx, instance.Namespace, instance.Name, instance.Status.ContextBackend.Name)
		Expect(err).ToNot(HaveOccurred())
		e := &engine{
			status: &instance.Status,
			wfCtx:  wfCtx,
		}
		interval := e.getBackoffWaitTime()
		Expect(interval).Should(BeEquivalentTo(minWorkflowBackoffWaitTime))

		By("Test get backoff time")
		for i := 0; i < 4; i++ {
			_, err = wf.ExecuteRunners(ctx, runners)
			Expect(err).ToNot(HaveOccurred())
			interval := e.getBackoffWaitTime()
			Expect(interval).Should(BeEquivalentTo(minWorkflowBackoffWaitTime))
		}

		for i := 0; i < 6; i++ {
			_, err = wf.ExecuteRunners(ctx, runners)
			Expect(err).ToNot(HaveOccurred())
			interval := e.getBackoffWaitTime()
			Expect(interval).Should(BeEquivalentTo(int(0.05 * math.Pow(2, float64(i+5)))))
		}

		for i := 0; i < 10; i++ {
			_, err = wf.ExecuteRunners(ctx, runners)
			Expect(err).ToNot(HaveOccurred())
			interval = e.getBackoffWaitTime()
			Expect(interval).Should(BeEquivalentTo(types.MaxWorkflowWaitBackoffTime))
		}

		By("Test get backoff time after clean")
		wfContext.CleanupMemoryStore(instance.Name, instance.Namespace)
		_, err = wf.ExecuteRunners(ctx, runners)
		Expect(err).ToNot(HaveOccurred())
		wfCtx, err = wfContext.LoadContext(ctx, instance.Namespace, instance.Name, instance.Status.ContextBackend.Name)
		Expect(err).ToNot(HaveOccurred())
		e = &engine{
			status: &instance.Status,
			wfCtx:  wfCtx,
		}
		interval = e.getBackoffWaitTime()
		Expect(interval).Should(BeEquivalentTo(minWorkflowBackoffWaitTime))
	})

	It("Test get backoff time with timeout", func() {
		instance, runners := makeTestCase([]v1alpha1.WorkflowStep{
			{
				WorkflowStepBase: v1alpha1.WorkflowStepBase{
					Name:    "s1",
					Timeout: "30s",
					Type:    "wait-with-set-var",
				},
			},
		})
		ctx := monitorContext.NewTraceContext(context.Background(), "test-app")
		wf := New(instance)
		_, err := wf.ExecuteRunners(ctx, runners)
		Expect(err).ToNot(HaveOccurred())
		_, err = wf.ExecuteRunners(ctx, runners)
		Expect(err).ToNot(HaveOccurred())

		By("Test get backoff time")
		for i := 0; i < 10; i++ {
			_, err = wf.ExecuteRunners(ctx, runners)
			Expect(err).ToNot(HaveOccurred())
		}

		Expect(int(math.Ceil(wf.GetBackoffWaitTime().Seconds()))).Should(Equal(30))
	})

	It("Test get suspend backoff time", func() {
		By("if there's no timeout and duration, return 0")
		instance, runners := makeTestCase([]v1alpha1.WorkflowStep{
			{
				WorkflowStepBase: v1alpha1.WorkflowStepBase{
					Name: "s1",
					Type: "suspend",
				},
			},
		})
		ctx := monitorContext.NewTraceContext(context.Background(), "test-app")
		wf := New(instance)
		_, err := wf.ExecuteRunners(ctx, runners)
		Expect(err).ToNot(HaveOccurred())
		_, err = wf.ExecuteRunners(ctx, runners)
		Expect(err).ToNot(HaveOccurred())
		Expect(int(math.Ceil(wf.GetSuspendBackoffWaitTime().Seconds()))).Should(Equal(0))

		By("return timeout if it's specified")
		instance, runners = makeTestCase([]v1alpha1.WorkflowStep{
			{
				WorkflowStepBase: v1alpha1.WorkflowStepBase{
					Name:    "s1",
					Type:    "suspend",
					Timeout: "1m",
				},
			},
		})
		ctx = monitorContext.NewTraceContext(context.Background(), "test-app")
		wf = New(instance)
		_, err = wf.ExecuteRunners(ctx, runners)
		Expect(err).ToNot(HaveOccurred())
		_, err = wf.ExecuteRunners(ctx, runners)
		Expect(err).ToNot(HaveOccurred())
		Expect(int(math.Ceil(wf.GetSuspendBackoffWaitTime().Seconds()))).Should(Equal(60))

		By("return duration if it's specified")
		instance, runners = makeTestCase([]v1alpha1.WorkflowStep{
			{
				WorkflowStepBase: v1alpha1.WorkflowStepBase{
					Name:       "s1",
					Type:       "suspend",
					Properties: &runtime.RawExtension{Raw: []byte(`{"duration":"30s"}`)},
				},
			},
		})
		ctx = monitorContext.NewTraceContext(context.Background(), "test-app")
		wf = New(instance)
		_, err = wf.ExecuteRunners(ctx, runners)
		Expect(err).ToNot(HaveOccurred())
		_, err = wf.ExecuteRunners(ctx, runners)
		Expect(err).ToNot(HaveOccurred())
		Expect(int(math.Ceil(wf.GetSuspendBackoffWaitTime().Seconds()))).Should(Equal(30))

		By("return the minimum of the timeout and duration")
		instance, runners = makeTestCase([]v1alpha1.WorkflowStep{
			{
				WorkflowStepBase: v1alpha1.WorkflowStepBase{
					Name:       "s1",
					Type:       "suspend",
					Timeout:    "1m",
					Properties: &runtime.RawExtension{Raw: []byte(`{"duration":"30s"}`)},
				},
			},
		})
		ctx = monitorContext.NewTraceContext(context.Background(), "test-app")
		wf = New(instance)
		_, err = wf.ExecuteRunners(ctx, runners)
		Expect(err).ToNot(HaveOccurred())
		_, err = wf.ExecuteRunners(ctx, runners)
		Expect(err).ToNot(HaveOccurred())
		Expect(int(math.Ceil(wf.GetSuspendBackoffWaitTime().Seconds()))).Should(Equal(30))

		instance, runners = makeTestCase([]v1alpha1.WorkflowStep{
			{
				WorkflowStepBase: v1alpha1.WorkflowStepBase{
					Name:       "s1",
					Type:       "suspend",
					Timeout:    "30s",
					Properties: &runtime.RawExtension{Raw: []byte(`{"duration":"1m"}`)},
				},
			},
		})
		ctx = monitorContext.NewTraceContext(context.Background(), "test-app")
		wf = New(instance)
		_, err = wf.ExecuteRunners(ctx, runners)
		Expect(err).ToNot(HaveOccurred())
		_, err = wf.ExecuteRunners(ctx, runners)
		Expect(err).ToNot(HaveOccurred())
		Expect(int(math.Ceil(wf.GetSuspendBackoffWaitTime().Seconds()))).Should(Equal(30))

		By("return 0 if the value is invalid")
		instance, runners = makeTestCase([]v1alpha1.WorkflowStep{
			{
				WorkflowStepBase: v1alpha1.WorkflowStepBase{
					Name:    "s1",
					Type:    "suspend",
					Timeout: "test",
				},
			},
		})
		ctx = monitorContext.NewTraceContext(context.Background(), "test-app")
		wf = New(instance)
		_, err = wf.ExecuteRunners(ctx, runners)
		Expect(err).ToNot(HaveOccurred())
		Expect(int(math.Ceil(wf.GetSuspendBackoffWaitTime().Seconds()))).Should(Equal(0))
	})

	It("test for suspend", func() {
		instance, runners := makeTestCase([]v1alpha1.WorkflowStep{
			{
				WorkflowStepBase: v1alpha1.WorkflowStepBase{
					Name: "s1",
					Type: "success",
				},
			},
			{
				WorkflowStepBase: v1alpha1.WorkflowStepBase{
					Name: "s2",
					Type: "suspend",
				},
			},
			{
				WorkflowStepBase: v1alpha1.WorkflowStepBase{
					Name: "s3",
					Type: "success",
				},
			},
		})
		ctx := monitorContext.NewTraceContext(context.Background(), "test-app")
		wf := New(instance)
		state, err := wf.ExecuteRunners(ctx, runners)
		Expect(err).ToNot(HaveOccurred())
		Expect(state).Should(BeEquivalentTo(v1alpha1.WorkflowStateSuspending))
		wfStatus := instance.Status
		wfStatus.ContextBackend = nil
		cleanStepTimeStamp(&wfStatus)
		Expect(cmp.Diff(wfStatus, v1alpha1.WorkflowRunStatus{
			Mode:    defaultMode,
			Suspend: true,
			Steps: []v1alpha1.WorkflowStepStatus{{
				StepStatus: v1alpha1.StepStatus{
					Name:  "s1",
					Type:  "success",
					Phase: v1alpha1.WorkflowStepPhaseSucceeded,
				},
			}, {
				StepStatus: v1alpha1.StepStatus{
					Name:   "s2",
					ID:     "s2",
					Type:   "suspend",
					Reason: types.StatusReasonSuspend,
					Phase:  v1alpha1.WorkflowStepPhaseSuspending,
				},
			}},
		})).Should(BeEquivalentTo(""))

		// check suspend...
		state, err = wf.ExecuteRunners(ctx, runners)
		Expect(err).ToNot(HaveOccurred())
		Expect(state).Should(BeEquivalentTo(v1alpha1.WorkflowStateSuspending))

		// check resume
		instance.Status.Suspend = false
		instance.Status.Steps[1].Phase = v1alpha1.WorkflowStepPhaseSucceeded
		// check app meta changed
		instance.Labels = map[string]string{"for-test": "changed"}
		state, err = wf.ExecuteRunners(ctx, runners)
		Expect(err).ToNot(HaveOccurred())
		Expect(state).Should(BeEquivalentTo(v1alpha1.WorkflowStateSucceeded))
		instance.Status.ContextBackend = nil
		cleanStepTimeStamp(&instance.Status)
		Expect(cmp.Diff(instance.Status, v1alpha1.WorkflowRunStatus{
			Mode: defaultMode,
			Steps: []v1alpha1.WorkflowStepStatus{{
				StepStatus: v1alpha1.StepStatus{
					Name:  "s1",
					Type:  "success",
					Phase: v1alpha1.WorkflowStepPhaseSucceeded,
				},
			}, {
				StepStatus: v1alpha1.StepStatus{
					Name:   "s2",
					ID:     "s2",
					Type:   "suspend",
					Reason: types.StatusReasonSuspend,
					Phase:  v1alpha1.WorkflowStepPhaseSucceeded,
				},
			}, {
				StepStatus: v1alpha1.StepStatus{
					Name:  "s3",
					Type:  "success",
					Phase: v1alpha1.WorkflowStepPhaseSucceeded,
				},
			}},
		})).Should(BeEquivalentTo(""))

		state, err = wf.ExecuteRunners(ctx, runners)
		Expect(err).ToNot(HaveOccurred())
		Expect(state).Should(BeEquivalentTo(v1alpha1.WorkflowStateSucceeded))
	})

	It("test for suspend with sub steps", func() {
		By("Test suspend with step group")
		instance, runners := makeTestCase([]v1alpha1.WorkflowStep{
			{
				WorkflowStepBase: v1alpha1.WorkflowStepBase{
					Name: "s1",
					Type: "success",
				},
			},
			{
				WorkflowStepBase: v1alpha1.WorkflowStepBase{
					Name: "s2",
					Type: "step-group",
				},
				SubSteps: []v1alpha1.WorkflowStepBase{
					{
						Name: "s2-sub1",
						Type: "success",
					},
					{
						Name: "s2-sub2",
						Type: "suspend",
					},
				},
			},
			{
				WorkflowStepBase: v1alpha1.WorkflowStepBase{
					Name: "s3",
					Type: "success",
				},
			},
		})
		wf := New(instance)
		ctx := monitorContext.NewTraceContext(context.Background(), "test-app")
		state, err := wf.ExecuteRunners(ctx, runners)
		Expect(err).ToNot(HaveOccurred())
		Expect(state).Should(BeEquivalentTo(v1alpha1.WorkflowStateSuspending))
		instance.Status.ContextBackend = nil
		cleanStepTimeStamp(&instance.Status)
		Expect(cmp.Diff(instance.Status, v1alpha1.WorkflowRunStatus{
			Mode:    defaultMode,
			Suspend: true,
			Steps: []v1alpha1.WorkflowStepStatus{{
				StepStatus: v1alpha1.StepStatus{
					Name:  "s1",
					Type:  "success",
					Phase: v1alpha1.WorkflowStepPhaseSucceeded,
				},
			}, {
				StepStatus: v1alpha1.StepStatus{
					Name:  "s2",
					Type:  "step-group",
					Phase: v1alpha1.WorkflowStepPhaseSuspending,
				},
				SubStepsStatus: []v1alpha1.StepStatus{
					{
						Name:  "s2-sub1",
						Type:  "success",
						Phase: v1alpha1.WorkflowStepPhaseSucceeded,
					}, {
						Name:   "s2-sub2",
						ID:     "s2-sub2",
						Type:   "suspend",
						Reason: types.StatusReasonSuspend,
						Phase:  v1alpha1.WorkflowStepPhaseSuspending,
					},
				},
			}},
		})).Should(BeEquivalentTo(""))
	})

	It("test for terminate", func() {
		instance, runners := makeTestCase([]v1alpha1.WorkflowStep{
			{
				WorkflowStepBase: v1alpha1.WorkflowStepBase{
					Name: "s1",
					Type: "success",
				},
			},
			{
				WorkflowStepBase: v1alpha1.WorkflowStepBase{
					Name: "s2",
					Type: "terminate",
				},
			},
		})
		ctx := monitorContext.NewTraceContext(context.Background(), "test-app")
		wf := New(instance)
		state, err := wf.ExecuteRunners(ctx, runners)
		Expect(err).ToNot(HaveOccurred())
		Expect(state).Should(BeEquivalentTo(v1alpha1.WorkflowStateTerminated))
		instance.Status.ContextBackend = nil
		cleanStepTimeStamp(&instance.Status)
		Expect(cmp.Diff(instance.Status, v1alpha1.WorkflowRunStatus{
			Mode:       defaultMode,
			Terminated: true,
			Steps: []v1alpha1.WorkflowStepStatus{{
				StepStatus: v1alpha1.StepStatus{
					Name:  "s1",
					Type:  "success",
					Phase: v1alpha1.WorkflowStepPhaseSucceeded,
				},
			}, {
				StepStatus: v1alpha1.StepStatus{
					Name:   "s2",
					Type:   "terminate",
					Phase:  v1alpha1.WorkflowStepPhaseFailed,
					Reason: types.StatusReasonTerminate,
				},
			}},
		})).Should(BeEquivalentTo(""))

		state, err = wf.ExecuteRunners(ctx, runners)
		Expect(err).ToNot(HaveOccurred())
		Expect(state).Should(BeEquivalentTo(v1alpha1.WorkflowStateTerminated))
	})

	It("test for terminate with sub steps", func() {

		By("Test terminate with step group")
		instance, runners := makeTestCase([]v1alpha1.WorkflowStep{
			{
				WorkflowStepBase: v1alpha1.WorkflowStepBase{
					Name: "s1",
					Type: "success",
				},
			},
			{
				WorkflowStepBase: v1alpha1.WorkflowStepBase{
					Name: "s2",
					Type: "step-group",
				},
				SubSteps: []v1alpha1.WorkflowStepBase{
					{
						Name: "s2-sub1",
						Type: "success",
					},
					{
						Name: "s2-sub2",
						Type: "terminate",
					},
				},
			},
		})
		ctx := monitorContext.NewTraceContext(context.Background(), "test-app")
		wf := New(instance)
		state, err := wf.ExecuteRunners(ctx, runners)
		Expect(err).ToNot(HaveOccurred())
		Expect(state).Should(BeEquivalentTo(v1alpha1.WorkflowStateTerminated))
		instance.Status.ContextBackend = nil
		cleanStepTimeStamp(&instance.Status)
		Expect(cmp.Diff(instance.Status, v1alpha1.WorkflowRunStatus{
			Mode:       defaultMode,
			Terminated: true,
			Steps: []v1alpha1.WorkflowStepStatus{{
				StepStatus: v1alpha1.StepStatus{
					Name:  "s1",
					Type:  "success",
					Phase: v1alpha1.WorkflowStepPhaseSucceeded,
				},
			}, {
				StepStatus: v1alpha1.StepStatus{
					Name:   "s2",
					Type:   "step-group",
					Phase:  v1alpha1.WorkflowStepPhaseFailed,
					Reason: types.StatusReasonTerminate,
				},
				SubStepsStatus: []v1alpha1.StepStatus{
					{
						Name:  "s2-sub1",
						Type:  "success",
						Phase: v1alpha1.WorkflowStepPhaseSucceeded,
					}, {
						Name:   "s2-sub2",
						Type:   "terminate",
						Phase:  v1alpha1.WorkflowStepPhaseFailed,
						Reason: types.StatusReasonTerminate,
					},
				},
			}},
		})).Should(BeEquivalentTo(""))

		state, err = wf.ExecuteRunners(ctx, runners)
		Expect(err).ToNot(HaveOccurred())
		Expect(state).Should(BeEquivalentTo(v1alpha1.WorkflowStateTerminated))
	})

	It("test for error", func() {
		instance, runners := makeTestCase([]v1alpha1.WorkflowStep{
			{
				WorkflowStepBase: v1alpha1.WorkflowStepBase{
					Name: "s1",
					Type: "success",
				},
			},
			{
				WorkflowStepBase: v1alpha1.WorkflowStepBase{
					Name: "s2",
					Type: "error",
				},
			},
		})
		ctx := monitorContext.NewTraceContext(context.Background(), "test-app")
		wf := New(instance)
		state, err := wf.ExecuteRunners(ctx, runners)
		Expect(err).To(HaveOccurred())
		Expect(state).Should(BeEquivalentTo(v1alpha1.WorkflowStateExecuting))
		instance.Status.ContextBackend = nil
		cleanStepTimeStamp(&instance.Status)
		Expect(cmp.Diff(instance.Status, v1alpha1.WorkflowRunStatus{
			Mode: defaultMode,
			Steps: []v1alpha1.WorkflowStepStatus{{
				StepStatus: v1alpha1.StepStatus{
					Name:  "s1",
					Type:  "success",
					Phase: v1alpha1.WorkflowStepPhaseSucceeded,
				},
			}},
		})).Should(BeEquivalentTo(""))
	})

	It("skip workflow", func() {
		instance, runners := makeTestCase([]v1alpha1.WorkflowStep{})
		ctx := monitorContext.NewTraceContext(context.Background(), "test-app")
		wf := New(instance)
		state, err := wf.ExecuteRunners(ctx, runners)
		Expect(err).ToNot(HaveOccurred())
		Expect(state).Should(BeEquivalentTo(v1alpha1.WorkflowStateSucceeded))
	})

	It("test for DAG", func() {
		instance, runners := makeTestCase([]v1alpha1.WorkflowStep{
			{
				WorkflowStepBase: v1alpha1.WorkflowStepBase{
					Name: "s1",
					Type: "success",
				},
			},
			{
				WorkflowStepBase: v1alpha1.WorkflowStepBase{
					Name: "s2",
					Type: "pending",
				},
			},
			{
				WorkflowStepBase: v1alpha1.WorkflowStepBase{
					Name: "s3",
					Type: "success",
				},
			},
		})
		pending = true //nolint
		instance.Mode = &v1alpha1.WorkflowExecuteMode{
			Steps: v1alpha1.WorkflowModeDAG,
		}
		wf := New(instance)
		ctx := monitorContext.NewTraceContext(context.Background(), "test-app")
		state, err := wf.ExecuteRunners(ctx, runners)
		Expect(err).ToNot(HaveOccurred())
		Expect(state).Should(BeEquivalentTo(v1alpha1.WorkflowStateExecuting))
		instance.Status.ContextBackend = nil
		cleanStepTimeStamp(&instance.Status)
		Expect(cmp.Diff(instance.Status, v1alpha1.WorkflowRunStatus{
			Mode: dagMode,
			Steps: []v1alpha1.WorkflowStepStatus{
				{
					StepStatus: v1alpha1.StepStatus{
						Name:  "s1",
						Type:  "success",
						Phase: v1alpha1.WorkflowStepPhaseSucceeded,
					},
				},
				{
					StepStatus: v1alpha1.StepStatus{
						Name:  "s3",
						Type:  "success",
						Phase: v1alpha1.WorkflowStepPhaseSucceeded,
					},
				},
				{
					StepStatus: v1alpha1.StepStatus{
						Name:  "s2",
						Type:  "pending",
						Phase: v1alpha1.WorkflowStepPhasePending,
					},
				},
			},
		})).Should(BeEquivalentTo(""))

		state, err = wf.ExecuteRunners(ctx, runners)
		Expect(err).ToNot(HaveOccurred())
		Expect(state).Should(BeEquivalentTo(v1alpha1.WorkflowStateExecuting))

		pending = false
		state, err = wf.ExecuteRunners(ctx, runners)
		Expect(err).ToNot(HaveOccurred())
		Expect(state).Should(BeEquivalentTo(v1alpha1.WorkflowStateSucceeded))
		instance.Status.ContextBackend = nil
		cleanStepTimeStamp(&instance.Status)
		Expect(cmp.Diff(instance.Status, v1alpha1.WorkflowRunStatus{
			Mode: dagMode,
			Steps: []v1alpha1.WorkflowStepStatus{
				{
					StepStatus: v1alpha1.StepStatus{
						Name:  "s1",
						Type:  "success",
						Phase: v1alpha1.WorkflowStepPhaseSucceeded,
					},
				},
				{
					StepStatus: v1alpha1.StepStatus{
						Name:  "s3",
						Type:  "success",
						Phase: v1alpha1.WorkflowStepPhaseSucceeded,
					},
				},
				{
					StepStatus: v1alpha1.StepStatus{
						Name:  "s2",
						Type:  "pending",
						Phase: v1alpha1.WorkflowStepPhaseSucceeded,
					},
				},
			},
		})).Should(BeEquivalentTo(""))
	})

	It("step commit data without success", func() {
		instance, runners := makeTestCase([]v1alpha1.WorkflowStep{
			{
				WorkflowStepBase: v1alpha1.WorkflowStepBase{
					Name: "s1",
					Type: "wait-with-set-var",
				},
			},
			{
				WorkflowStepBase: v1alpha1.WorkflowStepBase{
					Name: "s2",
					Type: "success",
				},
			},
		})
		ctx := monitorContext.NewTraceContext(context.Background(), "test-app")
		wf := New(instance)
		state, err := wf.ExecuteRunners(ctx, runners)
		Expect(err).ToNot(HaveOccurred())
		Expect(state).Should(BeEquivalentTo(v1alpha1.WorkflowStateExecuting))
		Expect(instance.Status.Steps[0].Phase).Should(BeEquivalentTo(v1alpha1.WorkflowStepPhaseRunning))
		wfCtx, err := wfContext.LoadContext(ctx, instance.Namespace, instance.Name, instance.Status.ContextBackend.Name)
		Expect(err).ToNot(HaveOccurred())
		v, err := wfCtx.GetVar("saved")
		Expect(err).ToNot(HaveOccurred())
		saved, err := v.Bool()
		Expect(err).ToNot(HaveOccurred())
		Expect(saved).Should(BeEquivalentTo(true))
	})
})

func makeTestCase(steps []v1alpha1.WorkflowStep) (*types.WorkflowInstance, []types.TaskRunner) {
	instance := &types.WorkflowInstance{
		WorkflowMeta: types.WorkflowMeta{
			ChildOwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: v1alpha1.SchemeGroupVersion.String(),
					Kind:       v1alpha1.WorkflowRunKind,
					Name:       "app",
					UID:        "test-uid",
					Controller: ptr.To(true),
				},
			},
			Name:      "app",
			Namespace: "default",
		},
		Steps:  steps,
		Status: v1alpha1.WorkflowRunStatus{},
	}
	runners := []types.TaskRunner{}
	for _, step := range steps {
		if step.SubSteps != nil {
			subStepRunners := []types.TaskRunner{}
			for _, subStep := range step.SubSteps {
				step := v1alpha1.WorkflowStep{
					WorkflowStepBase: subStep,
				}
				subStepRunners = append(subStepRunners, makeRunner(step, nil))
			}
			runners = append(runners, makeRunner(step, subStepRunners))
		} else {
			runners = append(runners, makeRunner(step, nil))
		}
	}
	return instance, runners
}

var pending bool

func makeRunner(step v1alpha1.WorkflowStep, subTaskRunners []types.TaskRunner) types.TaskRunner {
	var run func(ctx wfContext.Context, options *types.TaskRunOptions) (v1alpha1.StepStatus, *types.Operation, error)
	switch step.Type {
	case "suspend":
		run = func(ctx wfContext.Context, options *types.TaskRunOptions) (v1alpha1.StepStatus, *types.Operation, error) {
			if step.Properties != nil {
				var v map[string]string
				b, _ := json.Marshal(step.Properties)
				_ = json.Unmarshal(b, &v)
				if v["duration"] != "" {
					d, _ := time.ParseDuration(v["duration"])
					ctx.SetMutableValue(time.Now().Add(d).Format(time.RFC3339), step.Name, "resumeTimeStamp")
				}
			}
			return v1alpha1.StepStatus{
					Name:   step.Name,
					Type:   "suspend",
					ID:     step.Name,
					Phase:  v1alpha1.WorkflowStepPhaseSuspending,
					Reason: types.StatusReasonSuspend,
				}, &types.Operation{
					Suspend: true,
				}, nil
		}
	case "terminate":
		run = func(ctx wfContext.Context, options *types.TaskRunOptions) (v1alpha1.StepStatus, *types.Operation, error) {
			return v1alpha1.StepStatus{
					Name:   step.Name,
					Type:   "terminate",
					Phase:  v1alpha1.WorkflowStepPhaseFailed,
					Reason: types.StatusReasonTerminate,
				}, &types.Operation{
					Terminated: true,
				}, nil
		}
	case "success":
		run = func(ctx wfContext.Context, options *types.TaskRunOptions) (v1alpha1.StepStatus, *types.Operation, error) {
			v := cuecontext.New().CompileString(`"app"`)
			if err := ctx.SetVar(v, "test"); err != nil {
				return v1alpha1.StepStatus{}, nil, err
			}
			return v1alpha1.StepStatus{
				Name:  step.Name,
				Type:  "success",
				Phase: v1alpha1.WorkflowStepPhaseSucceeded,
			}, &types.Operation{}, nil
		}
	case "failed":
		run = func(ctx wfContext.Context, options *types.TaskRunOptions) (v1alpha1.StepStatus, *types.Operation, error) {
			return v1alpha1.StepStatus{
				Name:  step.Name,
				Type:  "failed",
				Phase: v1alpha1.WorkflowStepPhaseFailed,
			}, &types.Operation{}, nil
		}
	case "failed-after-retries":
		run = func(ctx wfContext.Context, options *types.TaskRunOptions) (v1alpha1.StepStatus, *types.Operation, error) {
			return v1alpha1.StepStatus{
					Name:   step.Name,
					Type:   "failed-after-retries",
					Phase:  v1alpha1.WorkflowStepPhaseFailed,
					Reason: types.StatusReasonFailedAfterRetries,
				}, &types.Operation{
					FailedAfterRetries: true,
				}, nil
		}
	case "error":
		run = func(ctx wfContext.Context, options *types.TaskRunOptions) (v1alpha1.StepStatus, *types.Operation, error) {
			return v1alpha1.StepStatus{
				Name:  step.Name,
				Type:  "error",
				Phase: v1alpha1.WorkflowStepPhaseRunning,
			}, &types.Operation{}, errors.New("error for test")
		}
	case "wait-with-set-var":
		run = func(ctx wfContext.Context, options *types.TaskRunOptions) (v1alpha1.StepStatus, *types.Operation, error) {
			v := cuecontext.New().CompileString(`saved: true`)
			err := ctx.SetVar(v)
			return v1alpha1.StepStatus{
				Name:  step.Name,
				Type:  "wait-with-set-var",
				Phase: v1alpha1.WorkflowStepPhaseRunning,
			}, &types.Operation{}, err
		}
	case "step-group":
		group, _ := builtin.StepGroup(step, &types.TaskGeneratorOptions{SubTaskRunners: subTaskRunners, ProcessContext: process.NewContext(process.ContextData{})})
		run = group.Run
	case "running":
		run = func(ctx wfContext.Context, options *types.TaskRunOptions) (v1alpha1.StepStatus, *types.Operation, error) {
			return v1alpha1.StepStatus{
				Name:  step.Name,
				Type:  "running",
				Phase: v1alpha1.WorkflowStepPhaseRunning,
			}, &types.Operation{}, nil
		}
	default:
		run = func(ctx wfContext.Context, options *types.TaskRunOptions) (v1alpha1.StepStatus, *types.Operation, error) {
			return v1alpha1.StepStatus{
				Name:  step.Name,
				Type:  step.Type,
				Phase: v1alpha1.WorkflowStepPhaseSucceeded,
			}, &types.Operation{}, nil
		}

	}
	return &testTaskRunner{
		step: step,
		run:  run,
		fillContext: func(ctx monitorContext.Context, processCtx process.Context) types.ContextDataResetter {
			metas := []process.StepMetaKV{process.WithName(step.Name), process.WithSessionID("id"), process.WithSpanID(ctx.GetID())}
			manager := process.NewStepRunTimeMeta()
			manager.Fill(processCtx, metas)
			return func(processCtx process.Context) {
				manager.Remove(processCtx, slices.Map(metas,
					func(t process.StepMetaKV) string {
						return t.Key
					}))
			}
		},
		checkPending: func(ctx monitorContext.Context, wfCtx wfContext.Context, stepStatus map[string]v1alpha1.StepStatus) (bool, v1alpha1.StepStatus) {
			if step.Type != "pending" {
				return false, v1alpha1.StepStatus{}
			}
			if pending == true {
				return true, v1alpha1.StepStatus{
					Phase: v1alpha1.WorkflowStepPhasePending,
					Name:  step.Name,
					Type:  step.Type,
				}
			}
			return false, v1alpha1.StepStatus{}
		},
	}
}

type testTaskRunner struct {
	step         v1alpha1.WorkflowStep
	run          func(ctx wfContext.Context, options *types.TaskRunOptions) (v1alpha1.StepStatus, *types.Operation, error)
	checkPending func(ctx monitorContext.Context, wfCtx wfContext.Context, stepStatus map[string]v1alpha1.StepStatus) (bool, v1alpha1.StepStatus)
	fillContext  func(ctx monitorContext.Context, processCtx process.Context) types.ContextDataResetter
}

// Name return step name.
func (tr *testTaskRunner) Name() string {
	return tr.step.Name
}

// Run execute task.
func (tr *testTaskRunner) Run(ctx wfContext.Context, options *types.TaskRunOptions) (v1alpha1.StepStatus, *types.Operation, error) {
	logCtx := monitorContext.NewTraceContext(context.Background(), "test-app")
	if options.PCtx == nil {
		options.PCtx = process.NewContext(process.ContextData{})
	}
	resetter := tr.fillContext(logCtx, options.PCtx)
	defer resetter(options.PCtx)

	basicVal, err := custom.MakeBasicValue(logCtx, providers.DefaultCompiler.Get(), nil, options.PCtx)
	if err != nil {
		return v1alpha1.StepStatus{}, nil, err
	}
	if tr.step.Type != "step-group" && options != nil {
		for _, hook := range options.PreCheckHooks {
			result, err := hook(tr.step, &types.PreCheckOptions{
				BasicValue: basicVal,
			})
			if err != nil {
				return v1alpha1.StepStatus{
					Name:    tr.step.Name,
					Type:    tr.step.Type,
					Phase:   v1alpha1.WorkflowStepPhaseSkipped,
					Reason:  types.StatusReasonSkip,
					Message: err.Error(),
				}, &types.Operation{Skip: true}, nil
			}
			if result.Skip {
				return v1alpha1.StepStatus{
					Name:   tr.step.Name,
					Type:   tr.step.Type,
					Phase:  v1alpha1.WorkflowStepPhaseSkipped,
					Reason: types.StatusReasonSkip,
				}, &types.Operation{Skip: true}, nil
			}
			if result.Timeout {
				return v1alpha1.StepStatus{
					Name:   tr.step.Name,
					Type:   tr.step.Type,
					Phase:  v1alpha1.WorkflowStepPhaseFailed,
					Reason: types.StatusReasonTimeout,
				}, &types.Operation{Terminated: true}, nil
			}
		}
	}
	return tr.run(ctx, options)
}

// Pending check task should be executed or not.
func (tr *testTaskRunner) Pending(ctx monitorContext.Context, wfCtx wfContext.Context, stepStatus map[string]v1alpha1.StepStatus) (bool, v1alpha1.StepStatus) {
	return tr.checkPending(ctx, wfCtx, stepStatus)
}

// FillRunTime fill runtime data to context.
func (tr *testTaskRunner) FillContextData(ctx monitorContext.Context, processCtx process.Context) types.ContextDataResetter {
	return tr.fillContext(ctx, processCtx)
}

func cleanStepTimeStamp(wfStatus *v1alpha1.WorkflowRunStatus) {
	wfStatus.StartTime = metav1.Time{}
	for index, step := range wfStatus.Steps {
		wfStatus.Steps[index].FirstExecuteTime = metav1.Time{}
		wfStatus.Steps[index].LastExecuteTime = metav1.Time{}
		if step.SubStepsStatus != nil {
			for indexSubStep := range step.SubStepsStatus {
				wfStatus.Steps[index].SubStepsStatus[indexSubStep].FirstExecuteTime = metav1.Time{}
				wfStatus.Steps[index].SubStepsStatus[indexSubStep].LastExecuteTime = metav1.Time{}
			}
		}
	}
}

const cmYaml = `apiVersion: v1
data:
  components: '{"server":"{\"Scopes\":null,\"StandardWorkload\":\"{\\\"apiVersion\\\":\\\"v1\\\",\\\"kind\\\":\\\"Pod\\\",\\\"metadata\\\":{\\\"labels\\\":{\\\"app\\\":\\\"nginx\\\"}},\\\"spec\\\":{\\\"containers\\\":[{\\\"env\\\":[{\\\"name\\\":\\\"APP\\\",\\\"value\\\":\\\"nginx\\\"}],\\\"image\\\":\\\"nginx:1.14.2\\\",\\\"imagePullPolicy\\\":\\\"IfNotPresent\\\",\\\"name\\\":\\\"main\\\",\\\"ports\\\":[{\\\"containerPort\\\":8080,\\\"protocol\\\":\\\"TCP\\\"}]}]}}\",\"Traits\":[\"{\\\"apiVersion\\\":\\\"v1\\\",\\\"kind\\\":\\\"Service\\\",\\\"metadata\\\":{\\\"name\\\":\\\"my-service\\\"},\\\"spec\\\":{\\\"ports\\\":[{\\\"port\\\":80,\\\"protocol\\\":\\\"TCP\\\",\\\"targetPort\\\":8080}],\\\"selector\\\":{\\\"app\\\":\\\"nginx\\\"}}}\"]}"}'
kind: ConfigMap
metadata:
  name: app-v1
  namespace: default
`
