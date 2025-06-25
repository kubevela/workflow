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

package controllers

import (
	"context"
	"fmt"
	"path/filepath"
	sysruntime "runtime"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	featuregatetesting "k8s.io/component-base/featuregate/testing"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/kubevela/pkg/util/test/definition"

	"github.com/kubevela/workflow/api/v1alpha1"
	"github.com/kubevela/workflow/pkg/debug"
	"github.com/kubevela/workflow/pkg/features"
	wfTypes "github.com/kubevela/workflow/pkg/types"
	"github.com/kubevela/workflow/pkg/utils"
)

var _ = Describe("Test Workflow", func() {
	ctx := context.Background()
	namespace := "test-ns"

	wrTemplate := &v1alpha1.WorkflowRun{
		TypeMeta: metav1.TypeMeta{
			Kind:       "WorkflowRun",
			APIVersion: "core.oam.dev/v1alpha1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "wr",
			Namespace: namespace,
		},
		Spec: v1alpha1.WorkflowRunSpec{
			WorkflowSpec: &v1alpha1.WorkflowSpec{
				Steps: []v1alpha1.WorkflowStep{
					{
						WorkflowStepBase: v1alpha1.WorkflowStepBase{
							Name: "step-1",
							Type: "suspend",
						},
					},
				},
			},
		},
	}
	testDefinitions := []string{"test-apply", "apply-object", "failed-render", "suspend-and-deploy", "multi-suspend", "save-process-context"}

	BeforeEach(func() {
		setupNamespace(ctx, namespace)
		setupTestDefinitions(ctx, testDefinitions, namespace)
		By("[TEST] Set up definitions before integration test")
	})

	AfterEach(func() {
		Expect(k8sClient.DeleteAllOf(ctx, &v1alpha1.WorkflowRun{}, client.InNamespace(namespace))).Should(Succeed())
		Expect(k8sClient.DeleteAllOf(ctx, &appsv1.Deployment{}, client.InNamespace(namespace))).Should(Succeed())
		Expect(k8sClient.DeleteAllOf(ctx, &corev1.ConfigMap{}, client.InNamespace(namespace))).Should(Succeed())
	})

	It("get steps from workflow ref", func() {
		workflow := &v1alpha1.Workflow{
			TypeMeta: metav1.TypeMeta{
				Kind:       "Workflow",
				APIVersion: "core.oam.dev/v1alpha1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "workflow",
				Namespace: namespace,
			},
			Mode: &v1alpha1.WorkflowExecuteMode{
				Steps: v1alpha1.WorkflowModeDAG,
			},
			WorkflowSpec: v1alpha1.WorkflowSpec{
				Steps: []v1alpha1.WorkflowStep{
					{
						WorkflowStepBase: v1alpha1.WorkflowStepBase{
							Name: "step-1",
							Type: "suspend",
						},
					},
				},
			},
		}
		wr := wrTemplate.DeepCopy()
		wr.Spec = v1alpha1.WorkflowRunSpec{
			WorkflowRef: "workflow",
		}
		Expect(k8sClient.Create(ctx, workflow)).Should(BeNil())
		Expect(k8sClient.Create(ctx, wr)).Should(BeNil())

		tryReconcile(reconciler, wr.Name, wr.Namespace)

		wrObj := &v1alpha1.WorkflowRun{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{
			Name:      wr.Name,
			Namespace: wr.Namespace,
		}, wrObj)).Should(BeNil())

		Expect(wrObj.Status.Suspend).Should(BeTrue())
		Expect(wrObj.Status.Phase).Should(BeEquivalentTo(v1alpha1.WorkflowStateSuspending))
		Expect(wrObj.Status.Mode.Steps).Should(BeEquivalentTo(v1alpha1.WorkflowModeDAG))
		Expect(wrObj.Status.Mode.SubSteps).Should(BeEquivalentTo(v1alpha1.WorkflowModeDAG))

		wr2 := wrTemplate.DeepCopy()
		wr2.Name = "wr-template-with-mode"
		wr2.Spec = v1alpha1.WorkflowRunSpec{
			WorkflowRef: "workflow",
			Mode: &v1alpha1.WorkflowExecuteMode{
				Steps:    v1alpha1.WorkflowModeStep,
				SubSteps: v1alpha1.WorkflowModeStep,
			},
		}
		Expect(k8sClient.Create(ctx, wr2)).Should(BeNil())

		tryReconcile(reconciler, wr2.Name, wr2.Namespace)

		wrObj = &v1alpha1.WorkflowRun{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{
			Name:      wr2.Name,
			Namespace: wr2.Namespace,
		}, wrObj)).Should(BeNil())

		Expect(wrObj.Status.Suspend).Should(BeTrue())
		Expect(wrObj.Status.Phase).Should(BeEquivalentTo(v1alpha1.WorkflowStateSuspending))
		Expect(wrObj.Status.Mode.Steps).Should(BeEquivalentTo(v1alpha1.WorkflowModeStep))
		Expect(wrObj.Status.Mode.SubSteps).Should(BeEquivalentTo(v1alpha1.WorkflowModeStep))
	})

	It("get failed to generate", func() {
		wr := wrTemplate.DeepCopy()
		wr.Name = "failed-generate"
		wr.Spec.WorkflowSpec.Steps = []v1alpha1.WorkflowStep{
			{
				WorkflowStepBase: v1alpha1.WorkflowStepBase{
					Name: "failed-generate",
					Type: "invalid",
				},
			}}
		Expect(k8sClient.Create(ctx, wr)).Should(BeNil())

		err := reconcileWithReturn(reconciler, wr.Name, wr.Namespace)
		Expect(err).ShouldNot(BeNil())

		events, err := recorder.GetEventsWithName(wr.Name)
		Expect(err).Should(BeNil())
		Expect(len(events)).Should(Equal(1))
		Expect(events[0].EventType).Should(Equal(corev1.EventTypeWarning))
		Expect(events[0].Reason).Should(Equal(v1alpha1.ReasonGenerate))
		Expect(events[0].Message).Should(ContainSubstring(v1alpha1.MessageFailedGenerate))
	})

	It("should create workflow context ConfigMap", func() {
		wr := wrTemplate.DeepCopy()
		Expect(k8sClient.Create(ctx, wr)).Should(BeNil())

		tryReconcile(reconciler, wr.Name, wr.Namespace)

		cm := &corev1.ConfigMap{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{
			Name:      "workflow-wr-context",
			Namespace: namespace,
		}, cm)).Should(BeNil())
	})

	It("test workflow suspend", func() {
		wr := wrTemplate.DeepCopy()
		wr.Name = "test-wr-suspend"
		Expect(k8sClient.Create(ctx, wr)).Should(BeNil())

		tryReconcile(reconciler, wr.Name, wr.Namespace)

		wrObj := &v1alpha1.WorkflowRun{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{
			Name:      wr.Name,
			Namespace: wr.Namespace,
		}, wrObj)).Should(BeNil())

		Expect(wrObj.Status.Suspend).Should(BeTrue())
		Expect(wrObj.Status.Phase).Should(BeEquivalentTo(v1alpha1.WorkflowStateSuspending))
		Expect(wrObj.Status.Steps[0].Phase).Should(BeEquivalentTo(v1alpha1.WorkflowStepPhaseSuspending))
		Expect(wrObj.Status.Steps[0].ID).ShouldNot(BeEquivalentTo(""))
		// resume
		Expect(utils.ResumeWorkflow(ctx, k8sClient, wrObj, "")).Should(BeNil())
		Expect(wrObj.Status.Suspend).Should(BeFalse())

		tryReconcile(reconciler, wr.Name, wr.Namespace)

		Expect(k8sClient.Get(ctx, client.ObjectKey{
			Name:      wr.Name,
			Namespace: wr.Namespace,
		}, wrObj)).Should(BeNil())
		Expect(wrObj.Status.Suspend).Should(BeFalse())
		Expect(wrObj.Status.Phase).Should(BeEquivalentTo(v1alpha1.WorkflowStateSucceeded))
	})

	It("test workflow suspend in sub steps", func() {
		wr := wrTemplate.DeepCopy()
		wr.Name = "test-wr-sub-suspend"
		wr.Spec.WorkflowSpec.Steps = []v1alpha1.WorkflowStep{
			{
				WorkflowStepBase: v1alpha1.WorkflowStepBase{
					Name: "group",
					Type: "step-group",
				},
				SubSteps: []v1alpha1.WorkflowStepBase{
					{
						Name: "suspend",
						Type: "suspend",
					},
					{
						Name:       "step1",
						Type:       "test-apply",
						Properties: &runtime.RawExtension{Raw: []byte(`{"cmd":["sleep","1000"],"image":"busybox"}`)},
					},
				},
			}}
		Expect(k8sClient.Create(ctx, wr)).Should(BeNil())

		tryReconcile(reconciler, wr.Name, wr.Namespace)

		wrObj := &v1alpha1.WorkflowRun{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{
			Name:      wr.Name,
			Namespace: wr.Namespace,
		}, wrObj)).Should(BeNil())

		Expect(wrObj.Status.Suspend).Should(BeTrue())
		Expect(wrObj.Status.Phase).Should(BeEquivalentTo(v1alpha1.WorkflowStateSuspending))
		Expect(wrObj.Status.Steps[0].SubStepsStatus[0].Phase).Should(BeEquivalentTo(v1alpha1.WorkflowStepPhaseSuspending))
		Expect(wrObj.Status.Steps[0].SubStepsStatus[0].ID).ShouldNot(BeEquivalentTo(""))
		// resume
		Expect(utils.ResumeWorkflow(ctx, k8sClient, wrObj, "")).Should(BeNil())
		Expect(wrObj.Status.Suspend).Should(BeFalse())
		expDeployment := &appsv1.Deployment{}
		step1Key := types.NamespacedName{Namespace: wr.Namespace, Name: "step1"}
		Expect(k8sClient.Get(ctx, step1Key, expDeployment)).Should(BeNil())
		expDeployment.Status.Replicas = 1
		expDeployment.Status.ReadyReplicas = 1
		expDeployment.Status.Conditions = []appsv1.DeploymentCondition{{
			Message: "hello",
		}}
		Expect(k8sClient.Status().Update(ctx, expDeployment)).Should(BeNil())

		tryReconcile(reconciler, wr.Name, wr.Namespace)

		Expect(k8sClient.Get(ctx, client.ObjectKey{
			Name:      wr.Name,
			Namespace: wr.Namespace,
		}, wrObj)).Should(BeNil())
		Expect(wrObj.Status.Suspend).Should(BeFalse())
		Expect(wrObj.Status.Phase).Should(BeEquivalentTo(v1alpha1.WorkflowStateSucceeded))
	})

	It("test workflow terminate a suspend workflow", func() {
		wr := wrTemplate.DeepCopy()
		wr.Name = "test-terminate-suspend-wr"
		wr.Spec.WorkflowSpec.Steps = []v1alpha1.WorkflowStep{
			{
				WorkflowStepBase: v1alpha1.WorkflowStepBase{
					Name: "suspend",
					Type: "suspend",
				},
			},
			{
				WorkflowStepBase: v1alpha1.WorkflowStepBase{
					Name: "suspend-1",
					Type: "suspend",
				},
			}}
		Expect(k8sClient.Create(ctx, wr)).Should(BeNil())

		tryReconcile(reconciler, wr.Name, wr.Namespace)

		wrObj := &v1alpha1.WorkflowRun{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{
			Name:      wr.Name,
			Namespace: wr.Namespace,
		}, wrObj)).Should(BeNil())

		Expect(wrObj.Status.Suspend).Should(BeTrue())
		Expect(wrObj.Status.Phase).Should(BeEquivalentTo(v1alpha1.WorkflowStateSuspending))

		// terminate the workflow
		Expect(utils.TerminateWorkflow(ctx, k8sClient, wrObj)).Should(BeNil())

		tryReconcile(reconciler, wr.Name, wr.Namespace)

		Expect(k8sClient.Get(ctx, client.ObjectKey{
			Name:      wr.Name,
			Namespace: wr.Namespace,
		}, wrObj)).Should(BeNil())

		Expect(wrObj.Status.Suspend).Should(BeFalse())
		Expect(wrObj.Status.Terminated).Should(BeTrue())
		Expect(wrObj.Status.Phase).Should(BeEquivalentTo(v1alpha1.WorkflowStateTerminated))
	})

	It("test input/output in step mode", func() {
		wr := wrTemplate.DeepCopy()
		wr.Name = "wr-with-inout"
		wr.Spec.WorkflowSpec.Steps = []v1alpha1.WorkflowStep{
			{
				WorkflowStepBase: v1alpha1.WorkflowStepBase{
					Name:       "step1",
					Type:       "test-apply",
					Properties: &runtime.RawExtension{Raw: []byte(`{"cmd":["sleep","1000"],"image":"busybox"}`)},
					Outputs: v1alpha1.StepOutputs{
						{
							Name:      "message",
							ValueFrom: `"message: " +output.$returns.value.status.conditions[0].message`,
						},
					},
				},
			},
			{
				WorkflowStepBase: v1alpha1.WorkflowStepBase{
					Name:       "step2",
					Type:       "test-apply",
					Properties: &runtime.RawExtension{Raw: []byte(`{"cmd":["sleep","1000"],"image":"busybox","message":"test"}`)},
					Inputs: v1alpha1.StepInputs{
						{
							From:         "message",
							ParameterKey: "message",
						},
					},
				},
			}}

		Expect(k8sClient.Create(context.Background(), wr)).Should(BeNil())
		wrKey := types.NamespacedName{Namespace: wr.Namespace, Name: wr.Name}

		tryReconcile(reconciler, wr.Name, wr.Namespace)

		checkRun := &v1alpha1.WorkflowRun{}
		Expect(k8sClient.Get(ctx, wrKey, checkRun)).Should(BeNil())
		Expect(checkRun.Status.Phase).Should(BeEquivalentTo(v1alpha1.WorkflowStateExecuting))
		expDeployment := &appsv1.Deployment{}
		step2Key := types.NamespacedName{Namespace: wr.Namespace, Name: "step2"}
		Expect(k8sClient.Get(ctx, step2Key, expDeployment)).Should(utils.NotFoundMatcher{})
		step1Key := types.NamespacedName{Namespace: wr.Namespace, Name: "step1"}
		Expect(k8sClient.Get(ctx, step1Key, expDeployment)).Should(BeNil())

		expDeployment.Status.Replicas = 1
		expDeployment.Status.ReadyReplicas = 1
		expDeployment.Status.Conditions = []appsv1.DeploymentCondition{{
			Message: "hello",
		}}
		Expect(k8sClient.Status().Update(ctx, expDeployment)).Should(BeNil())

		tryReconcile(reconciler, wr.Name, wr.Namespace)

		Expect(k8sClient.Get(ctx, step2Key, expDeployment)).Should(BeNil())
		Expect(expDeployment.Spec.Template.Spec.Containers[0].Env[0].Value).Should(Equal("message: hello"))
		expDeployment.Status.Replicas = 1
		expDeployment.Status.ReadyReplicas = 1
		Expect(k8sClient.Status().Update(ctx, expDeployment)).Should(BeNil())

		tryReconcile(reconciler, wr.Name, wr.Namespace)

		Expect(k8sClient.Get(ctx, wrKey, checkRun)).Should(BeNil())
		Expect(checkRun.Status.Mode.Steps).Should(BeEquivalentTo(v1alpha1.WorkflowModeStep))
		Expect(checkRun.Status.Phase).Should(BeEquivalentTo(v1alpha1.WorkflowStateSucceeded))
	})

	It("test input/output in dag mode", func() {
		wr := wrTemplate.DeepCopy()
		wr.Name = "wr-with-inout"
		wr.Spec.WorkflowSpec.Steps = []v1alpha1.WorkflowStep{
			{
				WorkflowStepBase: v1alpha1.WorkflowStepBase{
					Name:       "step2",
					Type:       "test-apply",
					Properties: &runtime.RawExtension{Raw: []byte(`{"cmd":["sleep","1000"],"image":"busybox"}`)},
					Inputs: v1alpha1.StepInputs{
						{
							From:         "message",
							ParameterKey: "message",
						},
					},
				},
			},
			{
				WorkflowStepBase: v1alpha1.WorkflowStepBase{
					Name:       "step1",
					Type:       "test-apply",
					Properties: &runtime.RawExtension{Raw: []byte(`{"cmd":["sleep","1000"],"image":"busybox"}`)},
					Outputs: v1alpha1.StepOutputs{
						{
							Name:      "message",
							ValueFrom: `"message: " +output.$returns.value.status.conditions[0].message`,
						},
					},
				},
			},
		}
		wr.Spec.Mode = &v1alpha1.WorkflowExecuteMode{
			Steps: v1alpha1.WorkflowModeDAG,
		}

		Expect(k8sClient.Create(context.Background(), wr)).Should(BeNil())
		wrKey := types.NamespacedName{Namespace: wr.Namespace, Name: wr.Name}

		tryReconcile(reconciler, wr.Name, wr.Namespace)

		checkRun := &v1alpha1.WorkflowRun{}
		Expect(k8sClient.Get(ctx, wrKey, checkRun)).Should(BeNil())
		Expect(checkRun.Status.Phase).Should(BeEquivalentTo(v1alpha1.WorkflowStateExecuting))
		expDeployment := &appsv1.Deployment{}
		step2Key := types.NamespacedName{Namespace: wr.Namespace, Name: "step2"}
		Expect(k8sClient.Get(ctx, step2Key, expDeployment)).Should(utils.NotFoundMatcher{})
		step1Key := types.NamespacedName{Namespace: wr.Namespace, Name: "step1"}
		Expect(k8sClient.Get(ctx, step1Key, expDeployment)).Should(BeNil())
		expDeployment.Status.Replicas = 1
		expDeployment.Status.ReadyReplicas = 1
		expDeployment.Status.Conditions = []appsv1.DeploymentCondition{{
			Message: "hello",
		}}
		Expect(k8sClient.Status().Update(ctx, expDeployment)).Should(BeNil())

		tryReconcile(reconciler, wr.Name, wr.Namespace)

		Expect(k8sClient.Get(ctx, step2Key, expDeployment)).Should(BeNil())
		Expect(expDeployment.Spec.Template.Spec.Containers[0].Env[0].Value).Should(Equal("message: hello"))
		expDeployment.Status.Replicas = 1
		expDeployment.Status.ReadyReplicas = 1
		Expect(k8sClient.Status().Update(ctx, expDeployment)).Should(BeNil())

		tryReconcile(reconciler, wr.Name, wr.Namespace)

		Expect(k8sClient.Get(ctx, wrKey, checkRun)).Should(BeNil())
		Expect(checkRun.Status.Mode.Steps).Should(BeEquivalentTo(v1alpha1.WorkflowModeDAG))
		Expect(checkRun.Status.Phase).Should(BeEquivalentTo(v1alpha1.WorkflowStateSucceeded))
	})

	It("test depends on in step mode", func() {
		wr := wrTemplate.DeepCopy()
		wr.Name = "wr-depends-on"
		wr.Spec.WorkflowSpec.Steps = []v1alpha1.WorkflowStep{
			{
				WorkflowStepBase: v1alpha1.WorkflowStepBase{
					Name:       "step1",
					Type:       "test-apply",
					Properties: &runtime.RawExtension{Raw: []byte(`{"cmd":["sleep","1000"],"image":"busybox"}`)},
				},
			},
			{
				WorkflowStepBase: v1alpha1.WorkflowStepBase{
					Name:       "step2",
					Type:       "test-apply",
					Properties: &runtime.RawExtension{Raw: []byte(`{"cmd":["sleep","1000"],"image":"busybox"}`)},
					DependsOn:  []string{"step1"},
				},
			},
		}

		Expect(k8sClient.Create(context.Background(), wr)).Should(BeNil())
		wrKey := types.NamespacedName{Namespace: wr.Namespace, Name: wr.Name}

		tryReconcile(reconciler, wr.Name, wr.Namespace)

		expDeployment := &appsv1.Deployment{}
		step1Key := types.NamespacedName{Namespace: wr.Namespace, Name: "step1"}
		step2Key := types.NamespacedName{Namespace: wr.Namespace, Name: "step2"}
		Expect(k8sClient.Get(ctx, step2Key, expDeployment)).Should(utils.NotFoundMatcher{})

		checkRun := &v1alpha1.WorkflowRun{}
		Expect(k8sClient.Get(ctx, wrKey, checkRun)).Should(BeNil())
		Expect(k8sClient.Get(ctx, step1Key, expDeployment)).Should(BeNil())

		expDeployment.Status.Replicas = 1
		expDeployment.Status.ReadyReplicas = 1
		Expect(k8sClient.Status().Update(ctx, expDeployment)).Should(BeNil())

		tryReconcile(reconciler, wr.Name, wr.Namespace)

		expDeployment = &appsv1.Deployment{}
		Expect(k8sClient.Get(ctx, step2Key, expDeployment)).Should(BeNil())
		expDeployment.Status.Replicas = 1
		expDeployment.Status.ReadyReplicas = 1
		Expect(k8sClient.Status().Update(ctx, expDeployment)).Should(BeNil())

		tryReconcile(reconciler, wr.Name, wr.Namespace)

		Expect(k8sClient.Get(ctx, wrKey, checkRun)).Should(BeNil())
		Expect(checkRun.Status.Mode.Steps).Should(BeEquivalentTo(v1alpha1.WorkflowModeStep))
		Expect(checkRun.Status.Phase).Should(BeEquivalentTo(v1alpha1.WorkflowStateSucceeded))
	})

	It("test depends on in dag mode", func() {
		wr := wrTemplate.DeepCopy()
		wr.Name = "wr-depends-on"
		wr.Spec.WorkflowSpec.Steps = []v1alpha1.WorkflowStep{
			{
				WorkflowStepBase: v1alpha1.WorkflowStepBase{
					Name:       "step2",
					Type:       "test-apply",
					Properties: &runtime.RawExtension{Raw: []byte(`{"cmd":["sleep","1000"],"image":"busybox"}`)},
					DependsOn:  []string{"step1"},
				},
			},
			{
				WorkflowStepBase: v1alpha1.WorkflowStepBase{
					Name:       "step1",
					Type:       "test-apply",
					Properties: &runtime.RawExtension{Raw: []byte(`{"cmd":["sleep","1000"],"image":"busybox"}`)},
				},
			},
			{
				WorkflowStepBase: v1alpha1.WorkflowStepBase{
					Name: "step3",
					Type: "suspend",
					If:   "false",
				},
			},
			{
				WorkflowStepBase: v1alpha1.WorkflowStepBase{
					Name:       "step4",
					Type:       "test-apply",
					Properties: &runtime.RawExtension{Raw: []byte(`{"cmd":["sleep","1000"],"image":"busybox"}`)},
					DependsOn:  []string{"step3"},
				},
			},
		}
		wr.Spec.Mode = &v1alpha1.WorkflowExecuteMode{
			Steps: v1alpha1.WorkflowModeDAG,
		}

		Expect(k8sClient.Create(context.Background(), wr)).Should(BeNil())
		wrKey := types.NamespacedName{Namespace: wr.Namespace, Name: wr.Name}

		tryReconcile(reconciler, wr.Name, wr.Namespace)

		expDeployment := &appsv1.Deployment{}
		step1Key := types.NamespacedName{Namespace: wr.Namespace, Name: "step1"}
		step2Key := types.NamespacedName{Namespace: wr.Namespace, Name: "step2"}
		step4Key := types.NamespacedName{Namespace: wr.Namespace, Name: "step4"}
		Expect(k8sClient.Get(ctx, step2Key, expDeployment)).Should(utils.NotFoundMatcher{})

		checkRun := &v1alpha1.WorkflowRun{}
		Expect(k8sClient.Get(ctx, wrKey, checkRun)).Should(BeNil())
		Expect(k8sClient.Get(ctx, step1Key, expDeployment)).Should(BeNil())

		expDeployment.Status.Replicas = 1
		expDeployment.Status.ReadyReplicas = 1
		Expect(k8sClient.Status().Update(ctx, expDeployment)).Should(BeNil())

		tryReconcile(reconciler, wr.Name, wr.Namespace)

		expDeployment = &appsv1.Deployment{}
		Expect(k8sClient.Get(ctx, step2Key, expDeployment)).Should(BeNil())
		expDeployment.Status.Replicas = 1
		expDeployment.Status.ReadyReplicas = 1
		Expect(k8sClient.Status().Update(ctx, expDeployment)).Should(BeNil())

		tryReconcile(reconciler, wr.Name, wr.Namespace)

		Expect(k8sClient.Get(ctx, step4Key, expDeployment)).Should(utils.NotFoundMatcher{})

		Expect(k8sClient.Get(ctx, wrKey, checkRun)).Should(BeNil())
		Expect(checkRun.Status.Mode.Steps).Should(BeEquivalentTo(v1alpha1.WorkflowModeDAG))
		Expect(checkRun.Status.Phase).Should(BeEquivalentTo(v1alpha1.WorkflowStateSucceeded))
	})

	It("test failed after retries in step mode with suspend on failure", func() {
		featuregatetesting.SetFeatureGateDuringTest(GinkgoT(), utilfeature.DefaultFeatureGate, features.EnableSuspendOnFailure, true)
		wr := wrTemplate.DeepCopy()
		wr.Name = "wr-failed-after-retries"
		wr.Spec.WorkflowSpec.Steps = []v1alpha1.WorkflowStep{
			{
				WorkflowStepBase: v1alpha1.WorkflowStepBase{
					Name:       "step1",
					Type:       "test-apply",
					Properties: &runtime.RawExtension{Raw: []byte(`{"cmd":["sleep","1000"],"image":"busybox"}`)},
				},
			},
			{
				WorkflowStepBase: v1alpha1.WorkflowStepBase{
					Name:       "step2-failed",
					Type:       "apply-object",
					Properties: &runtime.RawExtension{Raw: []byte(`{"value":[{"apiVersion":"v1","kind":"invalid","metadata":{"name":"test1"}}]}`)},
				},
			},
		}

		Expect(k8sClient.Create(ctx, wr)).Should(BeNil())
		wrKey := types.NamespacedName{Namespace: wr.Namespace, Name: wr.Name}
		checkRun := &v1alpha1.WorkflowRun{}
		Expect(k8sClient.Get(ctx, wrKey, checkRun)).Should(BeNil())

		tryReconcile(reconciler, wr.Name, wr.Namespace)

		expDeployment := &appsv1.Deployment{}
		step1Key := types.NamespacedName{Namespace: wr.Namespace, Name: "step1"}
		Expect(k8sClient.Get(ctx, step1Key, expDeployment)).Should(BeNil())

		expDeployment.Status.Replicas = 1
		expDeployment.Status.ReadyReplicas = 1
		Expect(k8sClient.Status().Update(ctx, expDeployment)).Should(BeNil())

		By("verify the first ten reconciles")
		for i := 0; i < wfTypes.MaxWorkflowStepErrorRetryTimes; i++ {
			tryReconcile(reconciler, wr.Name, wr.Namespace)
			Expect(k8sClient.Get(ctx, wrKey, checkRun)).Should(BeNil())
			Expect(checkRun.Status.Message).Should(BeEquivalentTo(""))
			Expect(checkRun.Status.Phase).Should(BeEquivalentTo(v1alpha1.WorkflowStateExecuting))
			Expect(checkRun.Status.Steps[1].Phase).Should(BeEquivalentTo(v1alpha1.WorkflowStepPhaseFailed))
		}

		By("workflowrun should be suspended after failed max reconciles")
		tryReconcile(reconciler, wr.Name, wr.Namespace)
		Expect(k8sClient.Get(ctx, wrKey, checkRun)).Should(BeNil())
		Expect(checkRun.Status.Phase).Should(BeEquivalentTo(v1alpha1.WorkflowStateSuspending))
		Expect(checkRun.Status.Message).Should(BeEquivalentTo(wfTypes.MessageSuspendFailedAfterRetries))
		Expect(checkRun.Status.Steps[1].Phase).Should(BeEquivalentTo(v1alpha1.WorkflowStepPhaseFailed))
		Expect(checkRun.Status.Steps[1].Reason).Should(BeEquivalentTo(wfTypes.StatusReasonFailedAfterRetries))

		By("resume the suspended workflow run")
		Expect(utils.ResumeWorkflow(ctx, k8sClient, checkRun, "")).Should(BeNil())

		tryReconcile(reconciler, wr.Name, wr.Namespace)
		Expect(k8sClient.Get(ctx, wrKey, checkRun)).Should(BeNil())
		Expect(checkRun.Status.Message).Should(BeEquivalentTo(""))
		Expect(checkRun.Status.Phase).Should(BeEquivalentTo(v1alpha1.WorkflowStateExecuting))
		Expect(checkRun.Status.Steps[1].Phase).Should(BeEquivalentTo(v1alpha1.WorkflowStepPhaseFailed))
	})

	It("test reconcile with patch status at once", func() {
		featuregatetesting.SetFeatureGateDuringTest(GinkgoT(), utilfeature.DefaultFeatureGate, features.EnableSuspendOnFailure, true)
		featuregatetesting.SetFeatureGateDuringTest(GinkgoT(), utilfeature.DefaultFeatureGate, features.EnablePatchStatusAtOnce, true)
		wr := wrTemplate.DeepCopy()
		wr.Name = "wr-failed-after-retries"
		wr.Spec.WorkflowSpec.Steps = []v1alpha1.WorkflowStep{
			{
				WorkflowStepBase: v1alpha1.WorkflowStepBase{
					Name:       "step1",
					Type:       "test-apply",
					Properties: &runtime.RawExtension{Raw: []byte(`{"cmd":["sleep","1000"],"image":"busybox"}`)},
				},
			},
			{
				WorkflowStepBase: v1alpha1.WorkflowStepBase{
					Name:       "step2-failed",
					Type:       "apply-object",
					Properties: &runtime.RawExtension{Raw: []byte(`{"value":[{"apiVersion":"v1","kind":"invalid","metadata":{"name":"test1"}}]}`)},
				},
			},
		}

		Expect(k8sClient.Create(ctx, wr)).Should(BeNil())
		wrKey := types.NamespacedName{Namespace: wr.Namespace, Name: wr.Name}
		checkRun := &v1alpha1.WorkflowRun{}
		Expect(k8sClient.Get(ctx, wrKey, checkRun)).Should(BeNil())

		tryReconcile(reconciler, wr.Name, wr.Namespace)

		expDeployment := &appsv1.Deployment{}
		step1Key := types.NamespacedName{Namespace: wr.Namespace, Name: "step1"}
		Expect(k8sClient.Get(ctx, step1Key, expDeployment)).Should(BeNil())

		expDeployment.Status.Replicas = 1
		expDeployment.Status.ReadyReplicas = 1
		Expect(k8sClient.Status().Update(ctx, expDeployment)).Should(BeNil())

		By("verify the first ten reconciles")
		for i := 0; i < wfTypes.MaxWorkflowStepErrorRetryTimes; i++ {
			tryReconcile(reconciler, wr.Name, wr.Namespace)
			Expect(k8sClient.Get(ctx, wrKey, checkRun)).Should(BeNil())
			Expect(checkRun.Status.Message).Should(BeEquivalentTo(""))
			Expect(checkRun.Status.Phase).Should(BeEquivalentTo(v1alpha1.WorkflowStateExecuting))
			Expect(checkRun.Status.Steps[1].Phase).Should(BeEquivalentTo(v1alpha1.WorkflowStepPhaseFailed))
		}

		By("workflowrun should be suspended after failed max reconciles")
		tryReconcile(reconciler, wr.Name, wr.Namespace)
		Expect(k8sClient.Get(ctx, wrKey, checkRun)).Should(BeNil())
		Expect(checkRun.Status.Phase).Should(BeEquivalentTo(v1alpha1.WorkflowStateSuspending))
		Expect(checkRun.Status.Message).Should(BeEquivalentTo(wfTypes.MessageSuspendFailedAfterRetries))
		Expect(checkRun.Status.Steps[1].Phase).Should(BeEquivalentTo(v1alpha1.WorkflowStepPhaseFailed))
		Expect(checkRun.Status.Steps[1].Reason).Should(BeEquivalentTo(wfTypes.StatusReasonFailedAfterRetries))

		By("resume the suspended workflow run")
		Expect(utils.ResumeWorkflow(ctx, k8sClient, checkRun, "")).Should(BeNil())

		tryReconcile(reconciler, wr.Name, wr.Namespace)
		Expect(k8sClient.Get(ctx, wrKey, checkRun)).Should(BeNil())
		Expect(checkRun.Status.Message).Should(BeEquivalentTo(""))
		Expect(checkRun.Status.Phase).Should(BeEquivalentTo(v1alpha1.WorkflowStateExecuting))
		Expect(checkRun.Status.Steps[1].Phase).Should(BeEquivalentTo(v1alpha1.WorkflowStepPhaseFailed))
	})

	It("test failed after retries in dag mode with running step and suspend on failure", func() {
		featuregatetesting.SetFeatureGateDuringTest(GinkgoT(), utilfeature.DefaultFeatureGate, features.EnableSuspendOnFailure, true)
		wr := wrTemplate.DeepCopy()
		wr.Name = "wr-failed-after-retries"
		wr.Spec.WorkflowSpec.Steps = []v1alpha1.WorkflowStep{
			{
				WorkflowStepBase: v1alpha1.WorkflowStepBase{
					Name:       "step1",
					Type:       "test-apply",
					Properties: &runtime.RawExtension{Raw: []byte(`{"cmd":["sleep","1000"],"image":"busybox"}`)},
				},
			},
			{
				WorkflowStepBase: v1alpha1.WorkflowStepBase{
					Name:       "step2-failed",
					Type:       "apply-object",
					Properties: &runtime.RawExtension{Raw: []byte(`{"value":[{"apiVersion":"v1","kind":"invalid","metadata":{"name":"test1"}}]}`)},
				},
			},
		}
		wr.Spec.Mode = &v1alpha1.WorkflowExecuteMode{
			Steps: v1alpha1.WorkflowModeDAG,
		}

		Expect(k8sClient.Create(ctx, wr)).Should(BeNil())
		wrKey := types.NamespacedName{Namespace: wr.Namespace, Name: wr.Name}
		checkRun := &v1alpha1.WorkflowRun{}
		Expect(k8sClient.Get(ctx, wrKey, checkRun)).Should(BeNil())

		By("verify the first ten reconciles")
		for i := 0; i < wfTypes.MaxWorkflowStepErrorRetryTimes; i++ {
			tryReconcile(reconciler, wr.Name, wr.Namespace)
			Expect(k8sClient.Get(ctx, wrKey, checkRun)).Should(BeNil())
			Expect(checkRun.Status.Message).Should(BeEquivalentTo(""))
			Expect(checkRun.Status.Phase).Should(BeEquivalentTo(v1alpha1.WorkflowStateExecuting))
			Expect(checkRun.Status.Steps[1].Phase).Should(BeEquivalentTo(v1alpha1.WorkflowStepPhaseFailed))
		}

		By("workflowrun should not be suspended after failed max reconciles because of running step")
		tryReconcile(reconciler, wr.Name, wr.Namespace)
		Expect(k8sClient.Get(ctx, wrKey, checkRun)).Should(BeNil())
		Expect(checkRun.Status.Message).Should(BeEquivalentTo(""))
		Expect(checkRun.Status.Phase).Should(BeEquivalentTo(v1alpha1.WorkflowStateExecuting))
		Expect(checkRun.Status.Steps[1].Phase).Should(BeEquivalentTo(v1alpha1.WorkflowStepPhaseFailed))
		Expect(checkRun.Status.Steps[1].Reason).Should(BeEquivalentTo(wfTypes.StatusReasonFailedAfterRetries))
	})

	It("test failed render", func() {
		wr := wrTemplate.DeepCopy()
		wr.Name = "wr-failed-render"
		wr.Spec.WorkflowSpec.Steps = []v1alpha1.WorkflowStep{
			{
				WorkflowStepBase: v1alpha1.WorkflowStepBase{
					Name:       "step1",
					Type:       "failed-render",
					Properties: &runtime.RawExtension{Raw: []byte(`{"cmd":["sleep","1000"],"image":"busybox"}`)},
				},
			},
			{
				WorkflowStepBase: v1alpha1.WorkflowStepBase{
					Name:       "step2",
					Type:       "test-apply",
					Properties: &runtime.RawExtension{Raw: []byte(`{"cmd":["sleep","1000"],"image":"busybox"}`)},
				},
			},
		}

		Expect(k8sClient.Create(ctx, wr)).Should(BeNil())
		wrKey := types.NamespacedName{Namespace: wr.Namespace, Name: wr.Name}
		checkRun := &v1alpha1.WorkflowRun{}
		Expect(k8sClient.Get(ctx, wrKey, checkRun)).Should(BeNil())

		for i := 0; i < wfTypes.MaxWorkflowStepErrorRetryTimes; i++ {
			tryReconcile(reconciler, wr.Name, wr.Namespace)
			Expect(k8sClient.Get(ctx, wrKey, checkRun)).Should(BeNil())
			Expect(checkRun.Status.Message).Should(BeEquivalentTo(""))
			Expect(checkRun.Status.Phase).Should(BeEquivalentTo(v1alpha1.WorkflowStateExecuting))
			Expect(checkRun.Status.Steps[0].Phase).Should(BeEquivalentTo(v1alpha1.WorkflowStepPhaseFailed))
		}

		tryReconcile(reconciler, wr.Name, wr.Namespace)
		Expect(k8sClient.Get(ctx, wrKey, checkRun)).Should(BeNil())
		Expect(checkRun.Status.Phase).Should(BeEquivalentTo(v1alpha1.WorkflowStateFailed))
		Expect(checkRun.Status.Steps[0].Phase).Should(BeEquivalentTo(v1alpha1.WorkflowStepPhaseFailed))
		Expect(checkRun.Status.Steps[0].Reason).Should(BeEquivalentTo(wfTypes.StatusReasonFailedAfterRetries))
		Expect(checkRun.Status.Steps[1].Phase).Should(BeEquivalentTo(v1alpha1.WorkflowStepPhaseSkipped))
	})

	It("test workflow run with mode", func() {
		wr := wrTemplate.DeepCopy()
		wr.Name = "wr-mode"
		wr.Spec.WorkflowSpec.Steps = []v1alpha1.WorkflowStep{
			{
				WorkflowStepBase: v1alpha1.WorkflowStepBase{
					Name:       "step1",
					Type:       "test-apply",
					Properties: &runtime.RawExtension{Raw: []byte(`{"cmd":["sleep","1000"],"image":"busybox"}`)},
				},
			},
			{
				WorkflowStepBase: v1alpha1.WorkflowStepBase{
					Name: "group",
					Type: "step-group",
				},
				SubSteps: []v1alpha1.WorkflowStepBase{
					{
						Name:       "step2",
						Type:       "test-apply",
						Properties: &runtime.RawExtension{Raw: []byte(`{"cmd":["sleep","1000"],"image":"busybox"}`)},
					},
					{
						Name:       "step3",
						Type:       "test-apply",
						Properties: &runtime.RawExtension{Raw: []byte(`{"cmd":["sleep","1000"],"image":"busybox"}`)},
					},
				},
			},
		}
		wr.Spec.Mode = &v1alpha1.WorkflowExecuteMode{
			Steps:    v1alpha1.WorkflowModeDAG,
			SubSteps: v1alpha1.WorkflowModeStep,
		}

		Expect(k8sClient.Create(context.Background(), wr)).Should(BeNil())
		wrKey := types.NamespacedName{Namespace: wr.Namespace, Name: wr.Name}

		tryReconcile(reconciler, wr.Name, wr.Namespace)

		expDeployment := &appsv1.Deployment{}
		step3Key := types.NamespacedName{Namespace: wr.Namespace, Name: "step3"}
		Expect(k8sClient.Get(ctx, step3Key, expDeployment)).Should(utils.NotFoundMatcher{})

		checkRun := &v1alpha1.WorkflowRun{}
		Expect(k8sClient.Get(ctx, wrKey, checkRun)).Should(BeNil())
		step1Key := types.NamespacedName{Namespace: wr.Namespace, Name: "step1"}
		Expect(k8sClient.Get(ctx, step1Key, expDeployment)).Should(BeNil())
		expDeployment.Status.Replicas = 1
		expDeployment.Status.ReadyReplicas = 1
		Expect(k8sClient.Status().Update(ctx, expDeployment)).Should(BeNil())
		step2Key := types.NamespacedName{Namespace: wr.Namespace, Name: "step2"}
		Expect(k8sClient.Get(ctx, step2Key, expDeployment)).Should(BeNil())
		expDeployment.Status.Replicas = 1
		expDeployment.Status.ReadyReplicas = 1
		Expect(k8sClient.Status().Update(ctx, expDeployment)).Should(BeNil())

		tryReconcile(reconciler, wr.Name, wr.Namespace)

		Expect(k8sClient.Get(ctx, step3Key, expDeployment)).Should(BeNil())
		expDeployment.Status.Replicas = 1
		expDeployment.Status.ReadyReplicas = 1
		Expect(k8sClient.Status().Update(ctx, expDeployment)).Should(BeNil())

		tryReconcile(reconciler, wr.Name, wr.Namespace)

		Expect(k8sClient.Get(ctx, wrKey, checkRun)).Should(BeNil())
		Expect(checkRun.Status.Phase).Should(BeEquivalentTo(v1alpha1.WorkflowStateSucceeded))
		Expect(checkRun.Status.Mode).Should(BeEquivalentTo(v1alpha1.WorkflowExecuteMode{
			Steps:    v1alpha1.WorkflowModeDAG,
			SubSteps: v1alpha1.WorkflowModeStep,
		}))
	})

	It("test workflow run with mode in step groups", func() {
		wr := wrTemplate.DeepCopy()
		wr.Name = "wr-group-mode"
		wr.Spec.WorkflowSpec.Steps = []v1alpha1.WorkflowStep{
			{
				WorkflowStepBase: v1alpha1.WorkflowStepBase{
					Name:       "step1",
					Type:       "test-apply",
					Properties: &runtime.RawExtension{Raw: []byte(`{"cmd":["sleep","1000"],"image":"busybox"}`)},
				},
			},
			{
				WorkflowStepBase: v1alpha1.WorkflowStepBase{
					Name: "group",
					Type: "step-group",
				},
				Mode: v1alpha1.WorkflowModeStep,
				SubSteps: []v1alpha1.WorkflowStepBase{
					{
						Name:       "step2",
						Type:       "test-apply",
						Properties: &runtime.RawExtension{Raw: []byte(`{"cmd":["sleep","1000"],"image":"busybox"}`)},
					},
					{
						Name:       "step3",
						Type:       "test-apply",
						Properties: &runtime.RawExtension{Raw: []byte(`{"cmd":["sleep","1000"],"image":"busybox"}`)},
					},
				},
			},
		}
		wr.Spec.Mode = &v1alpha1.WorkflowExecuteMode{
			Steps: v1alpha1.WorkflowModeDAG,
		}

		Expect(k8sClient.Create(context.Background(), wr)).Should(BeNil())
		wrKey := types.NamespacedName{Namespace: wr.Namespace, Name: wr.Name}

		tryReconcile(reconciler, wr.Name, wr.Namespace)

		expDeployment := &appsv1.Deployment{}
		step3Key := types.NamespacedName{Namespace: wr.Namespace, Name: "step3"}
		Expect(k8sClient.Get(ctx, step3Key, expDeployment)).Should(utils.NotFoundMatcher{})

		checkRun := &v1alpha1.WorkflowRun{}
		Expect(k8sClient.Get(ctx, wrKey, checkRun)).Should(BeNil())
		step1Key := types.NamespacedName{Namespace: wr.Namespace, Name: "step1"}
		Expect(k8sClient.Get(ctx, step1Key, expDeployment)).Should(BeNil())
		expDeployment.Status.Replicas = 1
		expDeployment.Status.ReadyReplicas = 1
		Expect(k8sClient.Status().Update(ctx, expDeployment)).Should(BeNil())
		step2Key := types.NamespacedName{Namespace: wr.Namespace, Name: "step2"}
		Expect(k8sClient.Get(ctx, step2Key, expDeployment)).Should(BeNil())
		expDeployment.Status.Replicas = 1
		expDeployment.Status.ReadyReplicas = 1
		Expect(k8sClient.Status().Update(ctx, expDeployment)).Should(BeNil())

		tryReconcile(reconciler, wr.Name, wr.Namespace)

		Expect(k8sClient.Get(ctx, step3Key, expDeployment)).Should(BeNil())
		expDeployment.Status.Replicas = 1
		expDeployment.Status.ReadyReplicas = 1
		Expect(k8sClient.Status().Update(ctx, expDeployment)).Should(BeNil())

		tryReconcile(reconciler, wr.Name, wr.Namespace)

		Expect(k8sClient.Get(ctx, wrKey, checkRun)).Should(BeNil())
		Expect(checkRun.Status.Phase).Should(BeEquivalentTo(v1alpha1.WorkflowStateSucceeded))
		Expect(checkRun.Status.Mode).Should(BeEquivalentTo(v1alpha1.WorkflowExecuteMode{
			Steps:    v1alpha1.WorkflowModeDAG,
			SubSteps: v1alpha1.WorkflowModeDAG,
		}))
	})

	It("test sub steps", func() {
		wr := wrTemplate.DeepCopy()
		wr.Name = "wr-substeps"
		wr.Spec.WorkflowSpec.Steps = []v1alpha1.WorkflowStep{
			{
				WorkflowStepBase: v1alpha1.WorkflowStepBase{
					Name:       "step1",
					Type:       "test-apply",
					Properties: &runtime.RawExtension{Raw: []byte(`{"cmd":["sleep","1000"],"image":"busybox"}`)},
				},
			},
			{
				WorkflowStepBase: v1alpha1.WorkflowStepBase{
					Name: "group",
					Type: "step-group",
				},
				SubSteps: []v1alpha1.WorkflowStepBase{
					{
						Name:       "step2",
						Type:       "test-apply",
						DependsOn:  []string{"step3"},
						Properties: &runtime.RawExtension{Raw: []byte(`{"cmd":["sleep","1000"],"image":"busybox"}`)},
					},
					{
						Name:       "step3",
						Type:       "test-apply",
						Properties: &runtime.RawExtension{Raw: []byte(`{"cmd":["sleep","1000"],"image":"busybox"}`)},
					},
				},
			},
		}

		Expect(k8sClient.Create(context.Background(), wr)).Should(BeNil())
		wrKey := types.NamespacedName{Namespace: wr.Namespace, Name: wr.Name}

		tryReconcile(reconciler, wr.Name, wr.Namespace)

		expDeployment := &appsv1.Deployment{}
		step2Key := types.NamespacedName{Namespace: wr.Namespace, Name: "step2"}
		Expect(k8sClient.Get(ctx, step2Key, expDeployment)).Should(utils.NotFoundMatcher{})
		step3Key := types.NamespacedName{Namespace: wr.Namespace, Name: "step3"}
		Expect(k8sClient.Get(ctx, step3Key, expDeployment)).Should(utils.NotFoundMatcher{})
		checkRun := &v1alpha1.WorkflowRun{}
		Expect(k8sClient.Get(ctx, wrKey, checkRun)).Should(BeNil())
		step1Key := types.NamespacedName{Namespace: wr.Namespace, Name: "step1"}
		Expect(k8sClient.Get(ctx, step1Key, expDeployment)).Should(BeNil())
		expDeployment.Status.Replicas = 1
		expDeployment.Status.ReadyReplicas = 1
		Expect(k8sClient.Status().Update(ctx, expDeployment)).Should(BeNil())

		tryReconcile(reconciler, wr.Name, wr.Namespace)

		Expect(k8sClient.Get(ctx, step2Key, expDeployment)).Should(utils.NotFoundMatcher{})
		Expect(k8sClient.Get(ctx, step3Key, expDeployment)).Should(BeNil())
		expDeployment.Status.Replicas = 1
		expDeployment.Status.ReadyReplicas = 1
		Expect(k8sClient.Status().Update(ctx, expDeployment)).Should(BeNil())

		tryReconcile(reconciler, wr.Name, wr.Namespace)

		Expect(k8sClient.Get(ctx, step2Key, expDeployment)).Should(BeNil())
		expDeployment.Status.Replicas = 1
		expDeployment.Status.ReadyReplicas = 1
		Expect(k8sClient.Status().Update(ctx, expDeployment)).Should(BeNil())

		tryReconcile(reconciler, wr.Name, wr.Namespace)
		Expect(k8sClient.Get(ctx, wrKey, checkRun)).Should(BeNil())
		Expect(checkRun.Status.Phase).Should(BeEquivalentTo(v1alpha1.WorkflowStateSucceeded))
	})

	It("test failed step's outputs", func() {
		wr := wrTemplate.DeepCopy()
		wr.Name = "wr-timeout-output"
		wr.Spec.WorkflowSpec.Steps = []v1alpha1.WorkflowStep{
			{
				WorkflowStepBase: v1alpha1.WorkflowStepBase{
					Name:    "step1",
					Type:    "test-apply",
					Timeout: "1s",
					Outputs: v1alpha1.StepOutputs{
						{
							Name:      "output",
							ValueFrom: "context.name",
						},
					},
					Properties: &runtime.RawExtension{Raw: []byte(`{"cmd":["sleep","1000"],"image":"busybox"}`)},
				},
			},
			{
				WorkflowStepBase: v1alpha1.WorkflowStepBase{
					Name: "step2",
					Inputs: v1alpha1.StepInputs{
						{
							From:         "output",
							ParameterKey: "",
						},
					},
					If:         `inputs.output == "wr-timeout-output"`,
					Type:       "test-apply",
					Properties: &runtime.RawExtension{Raw: []byte(`{"cmd":["sleep","1000"],"image":"busybox"}`)},
				},
			},
		}

		Expect(k8sClient.Create(context.Background(), wr)).Should(BeNil())
		wrKey := types.NamespacedName{Namespace: wr.Namespace, Name: wr.Name}
		tryReconcile(reconciler, wr.Name, wr.Namespace)

		expDeployment := &appsv1.Deployment{}
		step1Key := types.NamespacedName{Namespace: wr.Namespace, Name: "step1"}
		Expect(k8sClient.Get(ctx, step1Key, expDeployment)).Should(BeNil())
		step2Key := types.NamespacedName{Namespace: wr.Namespace, Name: "step2"}
		Expect(k8sClient.Get(ctx, step2Key, expDeployment)).Should(utils.NotFoundMatcher{})

		time.Sleep(time.Second)
		tryReconcile(reconciler, wr.Name, wr.Namespace)

		Expect(k8sClient.Get(ctx, step2Key, expDeployment)).Should(BeNil())
		expDeployment.Status.Replicas = 1
		expDeployment.Status.ReadyReplicas = 1
		Expect(k8sClient.Status().Update(ctx, expDeployment)).Should(BeNil())

		tryReconcile(reconciler, wr.Name, wr.Namespace)

		checkRun := &v1alpha1.WorkflowRun{}
		Expect(k8sClient.Get(ctx, wrKey, checkRun)).Should(BeNil())
		Expect(checkRun.Status.Phase).Should(BeEquivalentTo(v1alpha1.WorkflowStateFailed))
	})

	It("test if always", func() {
		wr := wrTemplate.DeepCopy()
		wr.Name = "wr-if-always"
		wr.Spec.WorkflowSpec.Steps = []v1alpha1.WorkflowStep{
			{
				WorkflowStepBase: v1alpha1.WorkflowStepBase{
					Name:       "step-failed",
					Type:       "apply-object",
					Properties: &runtime.RawExtension{Raw: []byte(`{"value":[{"apiVersion":"v1","kind":"invalid","metadata":{"name":"test1"}}]}`)},
				},
			},
			{
				WorkflowStepBase: v1alpha1.WorkflowStepBase{
					Name:       "step1",
					Type:       "test-apply",
					If:         "always",
					Properties: &runtime.RawExtension{Raw: []byte(`{"cmd":["sleep","1000"],"image":"busybox"}`)},
				},
			},
			{
				WorkflowStepBase: v1alpha1.WorkflowStepBase{
					Name:       "step2",
					Type:       "test-apply",
					Properties: &runtime.RawExtension{Raw: []byte(`{"cmd":["sleep","1000"],"image":"busybox"}`)},
				},
			},
		}
		Expect(k8sClient.Create(context.Background(), wr)).Should(BeNil())
		wrKey := types.NamespacedName{Namespace: wr.Namespace, Name: wr.Name}

		By("verify the first ten reconciles")
		for i := 0; i < wfTypes.MaxWorkflowStepErrorRetryTimes; i++ {
			tryReconcile(reconciler, wr.Name, wr.Namespace)
		}

		expDeployment := &appsv1.Deployment{}
		step1Key := types.NamespacedName{Namespace: wr.Namespace, Name: "step1"}
		Expect(k8sClient.Get(ctx, step1Key, expDeployment)).Should(utils.NotFoundMatcher{})
		step2Key := types.NamespacedName{Namespace: wr.Namespace, Name: "step2"}
		Expect(k8sClient.Get(ctx, step2Key, expDeployment)).Should(utils.NotFoundMatcher{})

		tryReconcile(reconciler, wr.Name, wr.Namespace)

		Expect(k8sClient.Get(ctx, step1Key, expDeployment)).Should(BeNil())
		expDeployment.Status.Replicas = 1
		expDeployment.Status.ReadyReplicas = 1
		Expect(k8sClient.Status().Update(ctx, expDeployment)).Should(BeNil())
		Expect(k8sClient.Get(ctx, step2Key, expDeployment)).Should(utils.NotFoundMatcher{})

		tryReconcile(reconciler, wr.Name, wr.Namespace)

		Expect(k8sClient.Get(ctx, step2Key, expDeployment)).Should(utils.NotFoundMatcher{})

		checkRun := &v1alpha1.WorkflowRun{}
		Expect(k8sClient.Get(ctx, wrKey, checkRun)).Should(BeNil())
		Expect(checkRun.Status.Phase).Should(BeEquivalentTo(v1alpha1.WorkflowStateFailed))
	})

	It("test if always in sub steps", func() {
		wr := wrTemplate.DeepCopy()
		wr.Name = "wr-if-always-substeps"
		wr.Spec.WorkflowSpec.Steps = []v1alpha1.WorkflowStep{
			{
				WorkflowStepBase: v1alpha1.WorkflowStepBase{
					Name: "group",
					Type: "step-group",
				},
				SubSteps: []v1alpha1.WorkflowStepBase{
					{
						Name:       "sub1",
						Type:       "test-apply",
						If:         "always",
						DependsOn:  []string{"sub2-failed"},
						Properties: &runtime.RawExtension{Raw: []byte(`{"cmd":["sleep","1000"],"image":"busybox"}`)},
					},
					{
						Name:       "sub2-failed",
						Type:       "apply-object",
						Properties: &runtime.RawExtension{Raw: []byte(`{"value":[{"apiVersion":"v1","kind":"invalid","metadata":{"name":"test1"}}]}`)},
					},
					{
						Name:       "sub3",
						Type:       "test-apply",
						DependsOn:  []string{"sub1"},
						Properties: &runtime.RawExtension{Raw: []byte(`{"cmd":["sleep","1000"],"image":"busybox"}`)},
					},
				},
			},
			{
				WorkflowStepBase: v1alpha1.WorkflowStepBase{
					Name:       "step2",
					Type:       "test-apply",
					If:         "always",
					Properties: &runtime.RawExtension{Raw: []byte(`{"cmd":["sleep","1000"],"image":"busybox"}`)},
				},
			},
			{
				WorkflowStepBase: v1alpha1.WorkflowStepBase{
					Name:       "step3",
					Type:       "test-apply",
					Properties: &runtime.RawExtension{Raw: []byte(`{"cmd":["sleep","1000"],"image":"busybox"}`)},
				},
			},
		}

		Expect(k8sClient.Create(context.Background(), wr)).Should(BeNil())
		wrKey := types.NamespacedName{Namespace: wr.Namespace, Name: wr.Name}

		By("verify the first ten reconciles")
		for i := 0; i < wfTypes.MaxWorkflowStepErrorRetryTimes; i++ {
			tryReconcile(reconciler, wr.Name, wr.Namespace)
		}

		expDeployment := &appsv1.Deployment{}
		sub1Key := types.NamespacedName{Namespace: wr.Namespace, Name: "sub1"}
		Expect(k8sClient.Get(ctx, sub1Key, expDeployment)).Should(utils.NotFoundMatcher{})
		sub3Key := types.NamespacedName{Namespace: wr.Namespace, Name: "sub3"}
		Expect(k8sClient.Get(ctx, sub3Key, expDeployment)).Should(utils.NotFoundMatcher{})
		step2Key := types.NamespacedName{Namespace: wr.Namespace, Name: "step2"}
		Expect(k8sClient.Get(ctx, step2Key, expDeployment)).Should(utils.NotFoundMatcher{})
		step3Key := types.NamespacedName{Namespace: wr.Namespace, Name: "step3"}
		Expect(k8sClient.Get(ctx, step3Key, expDeployment)).Should(utils.NotFoundMatcher{})

		tryReconcile(reconciler, wr.Name, wr.Namespace)

		Expect(k8sClient.Get(ctx, sub1Key, expDeployment)).Should(BeNil())
		expDeployment.Status.Replicas = 1
		expDeployment.Status.ReadyReplicas = 1
		Expect(k8sClient.Status().Update(ctx, expDeployment)).Should(BeNil())
		Expect(k8sClient.Get(ctx, sub3Key, expDeployment)).Should(utils.NotFoundMatcher{})
		Expect(k8sClient.Get(ctx, step2Key, expDeployment)).Should(utils.NotFoundMatcher{})
		Expect(k8sClient.Get(ctx, step3Key, expDeployment)).Should(utils.NotFoundMatcher{})

		tryReconcile(reconciler, wr.Name, wr.Namespace)

		Expect(k8sClient.Get(ctx, sub3Key, expDeployment)).Should(utils.NotFoundMatcher{})
		Expect(k8sClient.Get(ctx, step2Key, expDeployment)).Should(BeNil())
		expDeployment.Status.Replicas = 1
		expDeployment.Status.ReadyReplicas = 1
		Expect(k8sClient.Status().Update(ctx, expDeployment)).Should(BeNil())
		Expect(k8sClient.Get(ctx, step3Key, expDeployment)).Should(utils.NotFoundMatcher{})

		tryReconcile(reconciler, wr.Name, wr.Namespace)

		Expect(k8sClient.Get(ctx, sub3Key, expDeployment)).Should(utils.NotFoundMatcher{})
		Expect(k8sClient.Get(ctx, step3Key, expDeployment)).Should(utils.NotFoundMatcher{})

		checkRun := &v1alpha1.WorkflowRun{}
		Expect(k8sClient.Get(ctx, wrKey, checkRun)).Should(BeNil())
		Expect(checkRun.Status.Phase).Should(BeEquivalentTo(v1alpha1.WorkflowStateFailed))
	})

	It("test if expressions", func() {
		wr := wrTemplate.DeepCopy()
		wr.Name = "wr-if-expressions"
		wr.Spec.Context = &runtime.RawExtension{Raw: []byte(`{"mycontext":{"a":1,"b":2,"c":["hello", "world"]}}`)}
		wr.Spec.WorkflowSpec.Steps = []v1alpha1.WorkflowStep{
			{
				WorkflowStepBase: v1alpha1.WorkflowStepBase{
					Name:    "suspend",
					Type:    "suspend",
					Timeout: "1s",
					Outputs: v1alpha1.StepOutputs{
						{
							Name:      "suspend-output",
							ValueFrom: "context.name",
						},
						{
							Name:      "custom-output",
							ValueFrom: "context.mycontext.a",
						},
					},
				},
			},
			{
				WorkflowStepBase: v1alpha1.WorkflowStepBase{
					Name:       "step2",
					Type:       "test-apply",
					Properties: &runtime.RawExtension{Raw: []byte(`{"cmd":["sleep","1000"],"image":"busybox"}`)},
					Inputs: v1alpha1.StepInputs{
						{
							From:         "suspend-output",
							ParameterKey: "",
						},
						{
							From:         "custom-output",
							ParameterKey: "",
						},
						{
							From:         "context.mycontext.c[1]",
							ParameterKey: "",
						},
					},
					If: `status.suspend.timeout && inputs["suspend-output"] == "wr-if-expressions" && inputs["custom-output"] == 1 && context.mycontext.b == 2 && context.mycontext.c[0] == "hello" && inputs["context.mycontext.c[1]"] == "world"`,
				},
			},
			{
				WorkflowStepBase: v1alpha1.WorkflowStepBase{
					Name:       "step3",
					If:         "status.suspend.succeeded",
					Type:       "test-apply",
					Properties: &runtime.RawExtension{Raw: []byte(`{"cmd":["sleep","1000"],"image":"busybox"}`)},
				},
			},
		}

		Expect(k8sClient.Create(context.Background(), wr)).Should(BeNil())
		wrKey := types.NamespacedName{Namespace: wr.Namespace, Name: wr.Name}
		tryReconcile(reconciler, wr.Name, wr.Namespace)

		checkRun := &v1alpha1.WorkflowRun{}
		Expect(k8sClient.Get(ctx, wrKey, checkRun)).Should(BeNil())
		Expect(checkRun.Status.Phase).Should(BeEquivalentTo(v1alpha1.WorkflowStateSuspending))

		expDeployment := &appsv1.Deployment{}
		step2Key := types.NamespacedName{Namespace: wr.Namespace, Name: "step2"}
		Expect(k8sClient.Get(ctx, step2Key, expDeployment)).Should(utils.NotFoundMatcher{})
		step3Key := types.NamespacedName{Namespace: wr.Namespace, Name: "step3"}
		Expect(k8sClient.Get(ctx, step3Key, expDeployment)).Should(utils.NotFoundMatcher{})

		time.Sleep(time.Second)
		tryReconcile(reconciler, wr.Name, wr.Namespace)
		Expect(k8sClient.Get(ctx, step2Key, expDeployment)).Should(BeNil())
		expDeployment.Status.Replicas = 1
		expDeployment.Status.ReadyReplicas = 1
		Expect(k8sClient.Status().Update(ctx, expDeployment)).Should(BeNil())
		Expect(k8sClient.Get(ctx, step3Key, expDeployment)).Should(utils.NotFoundMatcher{})

		tryReconcile(reconciler, wr.Name, wr.Namespace)

		Expect(k8sClient.Get(ctx, step3Key, expDeployment)).Should(utils.NotFoundMatcher{})

		Expect(k8sClient.Get(ctx, wrKey, checkRun)).Should(BeNil())
		Expect(checkRun.Status.Phase).Should(BeEquivalentTo(v1alpha1.WorkflowStateFailed))
	})

	It("test if expressions in sub steps", func() {
		wr := wrTemplate.DeepCopy()
		wr.Name = "wr-if-expressions-substeps"
		wr.Spec.Context = &runtime.RawExtension{Raw: []byte(`{"mycontext":{"a":1,"b":2,"c":["hello", "world"]}}`)}
		wr.Spec.WorkflowSpec.Steps = []v1alpha1.WorkflowStep{
			{
				WorkflowStepBase: v1alpha1.WorkflowStepBase{
					Name: "group",
					Type: "step-group",
					Inputs: v1alpha1.StepInputs{
						{
							From:         "context.mycontext.c[1]",
							ParameterKey: "",
						},
					},
					If: `context.mycontext.b == 2`,
				},
				SubSteps: []v1alpha1.WorkflowStepBase{
					{
						Name:       "sub1",
						Type:       "test-apply",
						If:         "status.sub2.timeout",
						DependsOn:  []string{"sub2"},
						Properties: &runtime.RawExtension{Raw: []byte(`{"cmd":["sleep","1000"],"image":"busybox"}`)},
					},
					{
						Name:       "sub2",
						Type:       "suspend",
						Properties: &runtime.RawExtension{Raw: []byte(`{"duration":"1s"}`)},
						If:         `inputs["context.mycontext.c[0]"] == "hello" && context.mycontext.c[1] == "world"`,
						Outputs: v1alpha1.StepOutputs{
							{
								Name:      "suspend-output",
								ValueFrom: "context.name",
							},
							{
								Name:      "suspend-custom-output",
								ValueFrom: "context.mycontext.a",
							},
						},
						Inputs: v1alpha1.StepInputs{
							{
								From:         "context.mycontext.c[0]",
								ParameterKey: "",
							},
						},
					},
					{
						Name:      "sub3",
						Type:      "test-apply",
						DependsOn: []string{"sub1"},
						Inputs: v1alpha1.StepInputs{
							{
								From:         "suspend-output",
								ParameterKey: "",
							},
						},
						If:         `status.sub1.timeout || inputs["suspend-output"] == "wr-if-expressions-substeps"`,
						Properties: &runtime.RawExtension{Raw: []byte(`{"cmd":["sleep","1000"],"image":"busybox"}`)},
					},
				},
			},
			{
				WorkflowStepBase: v1alpha1.WorkflowStepBase{
					Name:       "step2",
					Type:       "test-apply",
					Properties: &runtime.RawExtension{Raw: []byte(`{"cmd":["sleep","1000"],"image":"busybox"}`)},
					If:         "status.group.failed",
				},
			},
			{
				WorkflowStepBase: v1alpha1.WorkflowStepBase{
					Name:       "step3",
					Type:       "test-apply",
					Properties: &runtime.RawExtension{Raw: []byte(`{"cmd":["sleep","1000"],"image":"busybox"}`)},
				},
			},
			{
				WorkflowStepBase: v1alpha1.WorkflowStepBase{
					Name: "group2",
					Type: "step-group",
				},
				SubSteps: []v1alpha1.WorkflowStepBase{
					{
						Name:    "group2-sub",
						Type:    "suspend",
						Timeout: "1s",
					},
				},
			},
		}
		Expect(k8sClient.Create(context.Background(), wr)).Should(BeNil())
		wrKey := types.NamespacedName{Namespace: wr.Namespace, Name: wr.Name}
		tryReconcile(reconciler, wr.Name, wr.Namespace)

		checkRun := &v1alpha1.WorkflowRun{}
		Expect(k8sClient.Get(ctx, wrKey, checkRun)).Should(BeNil())
		Expect(checkRun.Status.Phase).Should(BeEquivalentTo(v1alpha1.WorkflowStateSuspending))

		expDeployment := &appsv1.Deployment{}
		sub1Key := types.NamespacedName{Namespace: wr.Namespace, Name: "sub1"}
		Expect(k8sClient.Get(ctx, sub1Key, expDeployment)).Should(utils.NotFoundMatcher{})
		sub3Key := types.NamespacedName{Namespace: wr.Namespace, Name: "sub3"}
		Expect(k8sClient.Get(ctx, sub3Key, expDeployment)).Should(utils.NotFoundMatcher{})
		step2Key := types.NamespacedName{Namespace: wr.Namespace, Name: "step2"}
		Expect(k8sClient.Get(ctx, step2Key, expDeployment)).Should(utils.NotFoundMatcher{})
		step3Key := types.NamespacedName{Namespace: wr.Namespace, Name: "step3"}
		Expect(k8sClient.Get(ctx, step3Key, expDeployment)).Should(utils.NotFoundMatcher{})

		time.Sleep(time.Second)
		tryReconcile(reconciler, wr.Name, wr.Namespace)

		Expect(k8sClient.Get(ctx, wrKey, checkRun)).Should(BeNil())
		Expect(checkRun.Status.Phase).Should(BeEquivalentTo(v1alpha1.WorkflowStateExecuting))
		Expect(k8sClient.Get(ctx, sub1Key, expDeployment)).Should(utils.NotFoundMatcher{})
		Expect(k8sClient.Get(ctx, sub3Key, expDeployment)).Should(BeNil())
		expDeployment.Status.Replicas = 1
		expDeployment.Status.ReadyReplicas = 1
		Expect(k8sClient.Status().Update(ctx, expDeployment)).Should(BeNil())
		Expect(k8sClient.Get(ctx, step2Key, expDeployment)).Should(utils.NotFoundMatcher{})
		Expect(k8sClient.Get(ctx, step3Key, expDeployment)).Should(utils.NotFoundMatcher{})

		tryReconcile(reconciler, wr.Name, wr.Namespace)

		Expect(k8sClient.Get(ctx, step2Key, expDeployment)).Should(utils.NotFoundMatcher{})
		Expect(k8sClient.Get(ctx, step3Key, expDeployment)).Should(BeNil())
		expDeployment.Status.Replicas = 1
		expDeployment.Status.ReadyReplicas = 1
		Expect(k8sClient.Status().Update(ctx, expDeployment)).Should(BeNil())

		tryReconcile(reconciler, wr.Name, wr.Namespace)

		Expect(k8sClient.Get(ctx, wrKey, checkRun)).Should(BeNil())
		Expect(checkRun.Status.Phase).Should(BeEquivalentTo(v1alpha1.WorkflowStateSuspending))

		time.Sleep(time.Second)
		tryReconcile(reconciler, wr.Name, wr.Namespace)

		Expect(k8sClient.Get(ctx, wrKey, checkRun)).Should(BeNil())
		Expect(checkRun.Status.Phase).Should(BeEquivalentTo(v1alpha1.WorkflowStateFailed))
	})

	It("test suspend and deploy", func() {
		wr := wrTemplate.DeepCopy()
		wr.Name = "wr-suspend-and-deploy"
		wr.Spec.WorkflowSpec.Steps = []v1alpha1.WorkflowStep{
			{
				WorkflowStepBase: v1alpha1.WorkflowStepBase{
					Name:       "step1",
					Type:       "suspend-and-deploy",
					Properties: &runtime.RawExtension{Raw: []byte(`{"cmd":["sleep","1000"],"image":"busybox"}`)},
				},
			},
		}
		Expect(k8sClient.Create(context.Background(), wr)).Should(BeNil())
		wrKey := types.NamespacedName{Namespace: wr.Namespace, Name: wr.Name}
		tryReconcile(reconciler, wr.Name, wr.Namespace)

		checkRun := &v1alpha1.WorkflowRun{}
		Expect(k8sClient.Get(ctx, wrKey, checkRun)).Should(BeNil())
		Expect(checkRun.Status.Phase).Should(BeEquivalentTo(v1alpha1.WorkflowStateSuspending))

		expDeployment := &appsv1.Deployment{}
		step1Key := types.NamespacedName{Namespace: wr.Namespace, Name: "step1"}
		Expect(k8sClient.Get(ctx, step1Key, expDeployment)).Should(utils.NotFoundMatcher{})

		time.Sleep(time.Second)
		tryReconcile(reconciler, wr.Name, wr.Namespace)

		Expect(k8sClient.Get(ctx, wrKey, checkRun)).Should(BeNil())
		Expect(checkRun.Status.Phase).Should(BeEquivalentTo(v1alpha1.WorkflowStateExecuting))
		Expect(k8sClient.Get(ctx, step1Key, expDeployment)).Should(BeNil())
		expDeployment.Status.Replicas = 1
		expDeployment.Status.ReadyReplicas = 1
		Expect(k8sClient.Status().Update(ctx, expDeployment)).Should(BeNil())

		tryReconcile(reconciler, wr.Name, wr.Namespace)

		Expect(k8sClient.Get(ctx, wrKey, checkRun)).Should(BeNil())
		Expect(checkRun.Status.Phase).Should(BeEquivalentTo(v1alpha1.WorkflowStateSucceeded))
	})

	It("test multiple suspend", func() {
		wr := wrTemplate.DeepCopy()
		wr.Name = "wr-multi-suspend"
		wr.Spec.WorkflowSpec.Steps = []v1alpha1.WorkflowStep{
			{
				WorkflowStepBase: v1alpha1.WorkflowStepBase{
					Name: "step1",
					Type: "multi-suspend",
				},
			},
		}
		Expect(k8sClient.Create(context.Background(), wr)).Should(BeNil())
		wrKey := types.NamespacedName{Namespace: wr.Namespace, Name: wr.Name}
		tryReconcile(reconciler, wr.Name, wr.Namespace)

		checkRun := &v1alpha1.WorkflowRun{}
		Expect(k8sClient.Get(ctx, wrKey, checkRun)).Should(BeNil())
		Expect(checkRun.Status.Phase).Should(BeEquivalentTo(v1alpha1.WorkflowStateSuspending))

		Expect(utils.ResumeWorkflow(ctx, k8sClient, checkRun, "")).Should(BeNil())
		Expect(checkRun.Status.Suspend).Should(BeFalse())

		tryReconcile(reconciler, wr.Name, wr.Namespace)

		Expect(k8sClient.Get(ctx, wrKey, checkRun)).Should(BeNil())
		// suspended by the second suspend
		Expect(checkRun.Status.Suspend).Should(BeTrue())

		Expect(utils.ResumeWorkflow(ctx, k8sClient, checkRun, "")).Should(BeNil())
		Expect(checkRun.Status.Suspend).Should(BeFalse())

		tryReconcile(reconciler, wr.Name, wr.Namespace)

		Expect(k8sClient.Get(ctx, wrKey, checkRun)).Should(BeNil())
		Expect(checkRun.Status.Suspend).Should(BeFalse())
		Expect(checkRun.Status.Phase).Should(BeEquivalentTo(v1alpha1.WorkflowStateSucceeded))
	})

	It("test timeout", func() {
		wr := wrTemplate.DeepCopy()
		wr.Name = "wr-timeout"
		wr.Spec.WorkflowSpec.Steps = []v1alpha1.WorkflowStep{
			{
				WorkflowStepBase: v1alpha1.WorkflowStepBase{
					Name:       "step1",
					Type:       "test-apply",
					Timeout:    "1s",
					Properties: &runtime.RawExtension{Raw: []byte(`{"cmd":["sleep","1000"],"image":"busybox"}`)},
				},
			},
			{
				WorkflowStepBase: v1alpha1.WorkflowStepBase{
					Name:       "step2",
					Type:       "test-apply",
					If:         "always",
					Properties: &runtime.RawExtension{Raw: []byte(`{"cmd":["sleep","1000"],"image":"busybox"}`)},
				},
			},
			{
				WorkflowStepBase: v1alpha1.WorkflowStepBase{
					Name:       "step3",
					Type:       "test-apply",
					Properties: &runtime.RawExtension{Raw: []byte(`{"cmd":["sleep","1000"],"image":"busybox"}`)},
				},
			},
		}

		Expect(k8sClient.Create(context.Background(), wr)).Should(BeNil())
		wrKey := types.NamespacedName{Namespace: wr.Namespace, Name: wr.Name}
		tryReconcile(reconciler, wr.Name, wr.Namespace)

		expDeployment := &appsv1.Deployment{}
		step1Key := types.NamespacedName{Namespace: wr.Namespace, Name: "step1"}
		Expect(k8sClient.Get(ctx, step1Key, expDeployment)).Should(BeNil())
		step2Key := types.NamespacedName{Namespace: wr.Namespace, Name: "step2"}
		Expect(k8sClient.Get(ctx, step2Key, expDeployment)).Should(utils.NotFoundMatcher{})
		step3Key := types.NamespacedName{Namespace: wr.Namespace, Name: "step3"}
		Expect(k8sClient.Get(ctx, step3Key, expDeployment)).Should(utils.NotFoundMatcher{})

		time.Sleep(time.Second)

		tryReconcile(reconciler, wr.Name, wr.Namespace)

		Expect(k8sClient.Get(ctx, step2Key, expDeployment)).Should(BeNil())
		expDeployment.Status.Replicas = 1
		expDeployment.Status.ReadyReplicas = 1
		Expect(k8sClient.Status().Update(ctx, expDeployment)).Should(BeNil())
		Expect(k8sClient.Get(ctx, step3Key, expDeployment)).Should(utils.NotFoundMatcher{})

		tryReconcile(reconciler, wr.Name, wr.Namespace)
		Expect(k8sClient.Get(ctx, step3Key, expDeployment)).Should(utils.NotFoundMatcher{})

		checkRun := &v1alpha1.WorkflowRun{}
		Expect(k8sClient.Get(ctx, wrKey, checkRun)).Should(BeNil())
		Expect(checkRun.Status.Steps[0].Reason).Should(Equal(wfTypes.StatusReasonTimeout))
		Expect(checkRun.Status.Steps[1].Phase).Should(Equal(v1alpha1.WorkflowStepPhaseSucceeded))
		Expect(checkRun.Status.Steps[2].Reason).Should(Equal(wfTypes.StatusReasonSkip))
		Expect(checkRun.Status.Phase).Should(BeEquivalentTo(v1alpha1.WorkflowStateFailed))
	})

	It("test timeout in sub steps", func() {
		wr := wrTemplate.DeepCopy()
		wr.Name = "wr-timeout-substeps"
		wr.Spec.WorkflowSpec.Steps = []v1alpha1.WorkflowStep{
			{
				WorkflowStepBase: v1alpha1.WorkflowStepBase{
					Name: "group",
					Type: "step-group",
				},
				SubSteps: []v1alpha1.WorkflowStepBase{
					{
						Name:       "sub1",
						Type:       "test-apply",
						If:         "always",
						DependsOn:  []string{"sub2"},
						Properties: &runtime.RawExtension{Raw: []byte(`{"cmd":["sleep","1000"],"image":"busybox"}`)},
					},
					{
						Name:       "sub2",
						Type:       "test-apply",
						Timeout:    "1s",
						Properties: &runtime.RawExtension{Raw: []byte(`{"cmd":["sleep","1000"],"image":"busybox"}`)},
					},
					{
						Name:       "sub3",
						Type:       "test-apply",
						DependsOn:  []string{"sub1"},
						Properties: &runtime.RawExtension{Raw: []byte(`{"cmd":["sleep","1000"],"image":"busybox"}`)},
					},
				},
			},
			{
				WorkflowStepBase: v1alpha1.WorkflowStepBase{
					Name:       "step2",
					Type:       "test-apply",
					If:         "always",
					Properties: &runtime.RawExtension{Raw: []byte(`{"cmd":["sleep","1000"],"image":"busybox"}`)},
				},
			},
			{
				WorkflowStepBase: v1alpha1.WorkflowStepBase{
					Name:       "step3",
					Type:       "test-apply",
					Properties: &runtime.RawExtension{Raw: []byte(`{"cmd":["sleep","1000"],"image":"busybox"}`)},
				},
			},
			{
				WorkflowStepBase: v1alpha1.WorkflowStepBase{
					Name:    "group2",
					If:      "always",
					Type:    "step-group",
					Timeout: "1s",
				},
				SubSteps: []v1alpha1.WorkflowStepBase{
					{
						Name: "group2-sub",
						Type: "suspend",
					},
				},
			},
		}

		Expect(k8sClient.Create(context.Background(), wr)).Should(BeNil())
		wrKey := types.NamespacedName{Namespace: wr.Namespace, Name: wr.Name}
		tryReconcile(reconciler, wr.Name, wr.Namespace)

		expDeployment := &appsv1.Deployment{}
		sub1Key := types.NamespacedName{Namespace: wr.Namespace, Name: "sub1"}
		Expect(k8sClient.Get(ctx, sub1Key, expDeployment)).Should(utils.NotFoundMatcher{})
		sub2Key := types.NamespacedName{Namespace: wr.Namespace, Name: "sub2"}
		Expect(k8sClient.Get(ctx, sub2Key, expDeployment)).Should(BeNil())
		sub3Key := types.NamespacedName{Namespace: wr.Namespace, Name: "sub3"}
		Expect(k8sClient.Get(ctx, sub3Key, expDeployment)).Should(utils.NotFoundMatcher{})
		step2Key := types.NamespacedName{Namespace: wr.Namespace, Name: "step2"}
		Expect(k8sClient.Get(ctx, step2Key, expDeployment)).Should(utils.NotFoundMatcher{})
		step3Key := types.NamespacedName{Namespace: wr.Namespace, Name: "step3"}
		Expect(k8sClient.Get(ctx, step3Key, expDeployment)).Should(utils.NotFoundMatcher{})

		time.Sleep(time.Second)
		tryReconcile(reconciler, wr.Name, wr.Namespace)

		Expect(k8sClient.Get(ctx, sub1Key, expDeployment)).Should(BeNil())
		expDeployment.Status.Replicas = 1
		expDeployment.Status.ReadyReplicas = 1
		Expect(k8sClient.Status().Update(ctx, expDeployment)).Should(BeNil())
		Expect(k8sClient.Get(ctx, sub3Key, expDeployment)).Should(utils.NotFoundMatcher{})
		Expect(k8sClient.Get(ctx, step2Key, expDeployment)).Should(utils.NotFoundMatcher{})
		Expect(k8sClient.Get(ctx, step3Key, expDeployment)).Should(utils.NotFoundMatcher{})

		tryReconcile(reconciler, wr.Name, wr.Namespace)

		Expect(k8sClient.Get(ctx, sub3Key, expDeployment)).Should(utils.NotFoundMatcher{})
		Expect(k8sClient.Get(ctx, step2Key, expDeployment)).Should(BeNil())
		expDeployment.Status.Replicas = 1
		expDeployment.Status.ReadyReplicas = 1
		Expect(k8sClient.Status().Update(ctx, expDeployment)).Should(BeNil())
		Expect(k8sClient.Get(ctx, step3Key, expDeployment)).Should(utils.NotFoundMatcher{})

		tryReconcile(reconciler, wr.Name, wr.Namespace)

		Expect(k8sClient.Get(ctx, step3Key, expDeployment)).Should(utils.NotFoundMatcher{})

		checkRun := &v1alpha1.WorkflowRun{}
		Expect(k8sClient.Get(ctx, wrKey, checkRun)).Should(BeNil())
		Expect(checkRun.Status.Phase).Should(BeEquivalentTo(v1alpha1.WorkflowStateSuspending))

		time.Sleep(time.Second)
		tryReconcile(reconciler, wr.Name, wr.Namespace)

		Expect(k8sClient.Get(ctx, wrKey, checkRun)).Should(BeNil())
		Expect(checkRun.Status.Steps[0].Reason).Should(Equal(wfTypes.StatusReasonTimeout))
		Expect(checkRun.Status.Steps[1].Phase).Should(Equal(v1alpha1.WorkflowStepPhaseSucceeded))
		Expect(checkRun.Status.Steps[2].Reason).Should(Equal(wfTypes.StatusReasonSkip))
		Expect(checkRun.Status.Steps[3].Reason).Should(Equal(wfTypes.StatusReasonTimeout))
		Expect(checkRun.Status.Phase).Should(BeEquivalentTo(v1alpha1.WorkflowStateFailed))
	})

	It("test terminate manually", func() {
		wr := wrTemplate.DeepCopy()
		wr.Name = "wr-terminate-manually"
		wr.Spec.WorkflowSpec.Steps = []v1alpha1.WorkflowStep{
			{
				WorkflowStepBase: v1alpha1.WorkflowStepBase{
					Name: "step1",
					Type: "suspend",
				},
			},
			{
				WorkflowStepBase: v1alpha1.WorkflowStepBase{
					Name:       "step2",
					Type:       "test-apply",
					Properties: &runtime.RawExtension{Raw: []byte(`{"cmd":["sleep","1000"],"image":"busybox"}`)},
				},
			},
		}

		Expect(k8sClient.Create(context.Background(), wr)).Should(BeNil())
		wrKey := types.NamespacedName{Namespace: wr.Namespace, Name: wr.Name}
		tryReconcile(reconciler, wr.Name, wr.Namespace)

		expDeployment := &appsv1.Deployment{}
		step2Key := types.NamespacedName{Namespace: wr.Namespace, Name: "step2"}
		Expect(k8sClient.Get(ctx, step2Key, expDeployment)).Should(utils.NotFoundMatcher{})

		// terminate manually
		checkRun := &v1alpha1.WorkflowRun{}
		Expect(k8sClient.Get(ctx, wrKey, checkRun)).Should(BeNil())
		Expect(utils.TerminateWorkflow(ctx, k8sClient, checkRun)).Should(BeNil())

		tryReconcile(reconciler, wr.Name, wr.Namespace)

		Expect(k8sClient.Get(ctx, step2Key, expDeployment)).Should(utils.NotFoundMatcher{})

		Expect(k8sClient.Get(ctx, wrKey, checkRun)).Should(BeNil())
		Expect(checkRun.Status.Steps[1].Phase).Should(Equal(v1alpha1.WorkflowStepPhaseSkipped))
		Expect(checkRun.Status.Phase).Should(BeEquivalentTo(v1alpha1.WorkflowStateTerminated))
	})

	It("test debug", func() {
		wr := wrTemplate.DeepCopy()
		wr.Name = "wr-debug"
		wr.Spec.WorkflowSpec.Steps = []v1alpha1.WorkflowStep{
			{
				WorkflowStepBase: v1alpha1.WorkflowStepBase{
					Name:       "step1",
					Type:       "test-apply",
					Properties: &runtime.RawExtension{Raw: []byte(`{"cmd":["sleep","1000"],"image":"busybox"}`)},
				},
			},
			{
				WorkflowStepBase: v1alpha1.WorkflowStepBase{
					Name: "step2",
					Type: "step-group",
				},
				SubSteps: []v1alpha1.WorkflowStepBase{
					{
						Name:       "step2-sub",
						Type:       "test-apply",
						Properties: &runtime.RawExtension{Raw: []byte(`{"cmd":["sleep","1000"],"image":"busybox"}`)},
					},
				},
			},
		}
		wr.Annotations = map[string]string{
			wfTypes.AnnotationWorkflowRunDebug: "true",
		}
		Expect(k8sClient.Create(ctx, wr)).Should(BeNil())
		tryReconcile(reconciler, wr.Name, wr.Namespace)

		expDeployment := &appsv1.Deployment{}
		step1Key := types.NamespacedName{Namespace: wr.Namespace, Name: "step1"}
		Expect(k8sClient.Get(ctx, step1Key, expDeployment)).Should(BeNil())
		expDeployment.Status.Replicas = 1
		expDeployment.Status.ReadyReplicas = 1
		Expect(k8sClient.Status().Update(ctx, expDeployment)).Should(BeNil())

		wrKey := client.ObjectKey{
			Name:      wr.Name,
			Namespace: wr.Namespace,
		}
		tryReconcile(reconciler, wr.Name, wr.Namespace)

		subKey := types.NamespacedName{Namespace: wr.Namespace, Name: "step2-sub"}
		Expect(k8sClient.Get(ctx, subKey, expDeployment)).Should(BeNil())
		expDeployment.Status.Replicas = 1
		expDeployment.Status.ReadyReplicas = 1
		Expect(k8sClient.Status().Update(ctx, expDeployment)).Should(BeNil())

		tryReconcile(reconciler, wr.Name, wr.Namespace)

		By("Check WorkflowRun running successfully")
		curRun := &v1alpha1.WorkflowRun{}
		Expect(k8sClient.Get(ctx, wrKey, curRun)).Should(BeNil())
		Expect(curRun.Status.Phase).Should(Equal(v1alpha1.WorkflowStateSucceeded))

		By("Check debug Config Map is created")
		debugCM := &corev1.ConfigMap{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{
			Name:      debug.GenerateContextName(wr.Name, curRun.Status.Steps[0].ID, string(curRun.UID)),
			Namespace: wr.Namespace,
		}, debugCM)).Should(BeNil())
		Expect(k8sClient.Get(ctx, types.NamespacedName{
			Name:      debug.GenerateContextName(wr.Name, curRun.Status.Steps[1].SubStepsStatus[0].ID, string(curRun.UID)),
			Namespace: wr.Namespace,
		}, debugCM)).Should(BeNil())
	})

	It("test step context data", func() {
		wr := wrTemplate.DeepCopy()
		wr.Name = "test-step-context-data"
		wr.Spec.WorkflowSpec.Steps = []v1alpha1.WorkflowStep{{
			WorkflowStepBase: v1alpha1.WorkflowStepBase{
				Name: "group1",
				Type: "step-group",
			},
			SubSteps: []v1alpha1.WorkflowStepBase{
				{
					Name:       "step1",
					Type:       "save-process-context",
					Properties: &runtime.RawExtension{Raw: []byte(`{"name":"process-context-step1"}`)},
				},
				{
					Name:       "step2",
					Type:       "save-process-context",
					Properties: &runtime.RawExtension{Raw: []byte(`{"name":"process-context-step2"}`)},
				},
			},
		}, {
			WorkflowStepBase: v1alpha1.WorkflowStepBase{
				Name: "group2",
				Type: "step-group",
			},
			SubSteps: []v1alpha1.WorkflowStepBase{
				{
					Name:       "step3",
					Type:       "save-process-context",
					Properties: &runtime.RawExtension{Raw: []byte(`{"name":"process-context-step3"}`)},
				},
				{
					Name:       "step4",
					Type:       "save-process-context",
					Properties: &runtime.RawExtension{Raw: []byte(`{"name":"process-context-step4"}`)},
				},
			},
		}, {
			WorkflowStepBase: v1alpha1.WorkflowStepBase{
				Name:       "step5",
				Type:       "save-process-context",
				Properties: &runtime.RawExtension{Raw: []byte(`{"name":"process-context-step5"}`)},
			},
		}}
		Expect(k8sClient.Create(ctx, wr)).Should(BeNil())

		tryReconcile(reconciler, wr.Name, wr.Namespace)
		wrObj := &v1alpha1.WorkflowRun{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{
			Name:      wr.Name,
			Namespace: wr.Namespace,
		}, wrObj)).Should(BeNil())
		cmList := new(corev1.ConfigMapList)
		labels := &metav1.LabelSelector{
			MatchLabels: map[string]string{
				"process.context.data": "true",
			},
		}
		selector, err := metav1.LabelSelectorAsSelector(labels)
		Expect(err).Should(BeNil())
		Expect(k8sClient.List(ctx, cmList, &client.ListOptions{
			LabelSelector: selector,
		})).Should(BeNil())

		processCtxMap := make(map[string]map[string]string)
		for _, cm := range cmList.Items {
			processCtxMap[cm.Name] = cm.Data
		}
		step1Ctx := processCtxMap["process-context-step1"]
		step2Ctx := processCtxMap["process-context-step2"]
		step3Ctx := processCtxMap["process-context-step3"]
		step4Ctx := processCtxMap["process-context-step4"]
		step5Ctx := processCtxMap["process-context-step5"]

		By("check context.stepName")
		Expect(step1Ctx["stepName"]).Should(Equal("step1"))
		Expect(step2Ctx["stepName"]).Should(Equal("step2"))
		Expect(step3Ctx["stepName"]).Should(Equal("step3"))
		Expect(step4Ctx["stepName"]).Should(Equal("step4"))
		Expect(step5Ctx["stepName"]).Should(Equal("step5"))

		By("check context.stepGroupName")
		Expect(step1Ctx["stepGroupName"]).Should(Equal("group1"))
		Expect(step2Ctx["stepGroupName"]).Should(Equal("group1"))
		Expect(step3Ctx["stepGroupName"]).Should(Equal("group2"))
		Expect(step4Ctx["stepGroupName"]).Should(Equal("group2"))
		Expect(step5Ctx["stepGroupName"]).Should(Equal(""))

		By("check context.spanID")
		spanID := strings.Split(step1Ctx["spanID"], ".")[0]
		for _, pCtx := range processCtxMap {
			Expect(pCtx["spanID"]).Should(ContainSubstring(spanID))
		}
	})
})

func reconcileWithReturn(r *WorkflowRunReconciler, name, ns string) error {
	wrKey := client.ObjectKey{
		Name:      name,
		Namespace: ns,
	}
	_, err := r.Reconcile(context.TODO(), reconcile.Request{NamespacedName: wrKey})
	return err
}

func tryReconcile(r *WorkflowRunReconciler, name, ns string) {
	wrKey := client.ObjectKey{
		Name:      name,
		Namespace: ns,
	}
	_, err := r.Reconcile(context.TODO(), reconcile.Request{NamespacedName: wrKey})
	if err != nil {
		By(fmt.Sprintf("reconcile err: %+v ", err))
	}
	Expect(err).Should(BeNil())
}

func setupNamespace(ctx context.Context, namespace string) {
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: namespace},
	}
	Expect(k8sClient.Create(ctx, ns)).Should(SatisfyAny(BeNil(), &utils.AlreadyExistMatcher{}))
}

func setupTestDefinitions(ctx context.Context, defs []string, namespace string) {
	_, file, _, _ := sysruntime.Caller(0)
	for _, def := range defs {
		Expect(definition.InstallDefinitionFromYAML(ctx, k8sClient, filepath.Join(filepath.Dir(filepath.Dir(file)), fmt.Sprintf("./controllers/testdata/%s.yaml", def)), func(s string) string {
			return strings.ReplaceAll(s, "vela-system", namespace)
		})).Should(SatisfyAny(BeNil(), &utils.AlreadyExistMatcher{}))
	}
}
