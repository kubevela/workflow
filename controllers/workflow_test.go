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
	"encoding/json"
	"fmt"
	"testing"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	featuregatetesting "k8s.io/component-base/featuregate/testing"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/yaml"

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
	testDefinitions := []string{wfStepApplyDefYaml, wfStepApplyObjectDefYaml, wfStepFailedRenderDefYaml}

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
		Expect(wrObj.Status.Phase).Should(BeEquivalentTo(v1alpha1.WorkflowRunSuspending))
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
		Expect(wrObj.Status.Phase).Should(BeEquivalentTo(v1alpha1.WorkflowRunSuspending))
		Expect(wrObj.Status.Steps[0].Phase).Should(BeEquivalentTo(v1alpha1.WorkflowStepPhaseRunning))
		Expect(wrObj.Status.Steps[0].ID).ShouldNot(BeEquivalentTo(""))
		// resume
		wrObj.Status.Suspend = false
		wrObj.Status.Steps[0].Phase = v1alpha1.WorkflowStepPhaseSucceeded
		Expect(k8sClient.Status().Patch(ctx, wrObj, client.Merge)).Should(BeNil())
		Expect(k8sClient.Get(ctx, client.ObjectKey{
			Name:      wr.Name,
			Namespace: wr.Namespace,
		}, wrObj)).Should(BeNil())
		Expect(wrObj.Status.Suspend).Should(BeFalse())

		tryReconcile(reconciler, wr.Name, wr.Namespace)

		Expect(k8sClient.Get(ctx, client.ObjectKey{
			Name:      wr.Name,
			Namespace: wr.Namespace,
		}, wrObj)).Should(BeNil())
		Expect(wrObj.Status.Suspend).Should(BeFalse())
		Expect(wrObj.Status.Phase).Should(BeEquivalentTo(v1alpha1.WorkflowRunSucceeded))
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
		Expect(wrObj.Status.Phase).Should(BeEquivalentTo(v1alpha1.WorkflowRunSuspending))

		// terminate the workflow
		wrObj.Status.Terminated = true
		wrObj.Status.Suspend = false
		wrObj.Status.Steps[0].Phase = v1alpha1.WorkflowStepPhaseFailed
		wrObj.Status.Steps[0].Reason = wfTypes.StatusReasonTerminate
		Expect(k8sClient.Status().Patch(ctx, wrObj, client.Merge)).Should(BeNil())

		tryReconcile(reconciler, wr.Name, wr.Namespace)

		Expect(k8sClient.Get(ctx, client.ObjectKey{
			Name:      wr.Name,
			Namespace: wr.Namespace,
		}, wrObj)).Should(BeNil())

		Expect(wrObj.Status.Suspend).Should(BeFalse())
		Expect(wrObj.Status.Terminated).Should(BeTrue())
		Expect(wrObj.Status.Phase).Should(BeEquivalentTo(v1alpha1.WorkflowRunTerminated))
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
							ValueFrom: `"message: " +output.value.status.conditions[0].message`,
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
		Expect(checkRun.Status.Phase).Should(BeEquivalentTo(v1alpha1.WorkflowRunExecuting))
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
		Expect(checkRun.Status.Phase).Should(BeEquivalentTo(v1alpha1.WorkflowRunSucceeded))
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
							ValueFrom: `"message: " +output.value.status.conditions[0].message`,
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
		Expect(checkRun.Status.Phase).Should(BeEquivalentTo(v1alpha1.WorkflowRunExecuting))
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
		Expect(checkRun.Status.Phase).Should(BeEquivalentTo(v1alpha1.WorkflowRunSucceeded))
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
		Expect(checkRun.Status.Phase).Should(BeEquivalentTo(v1alpha1.WorkflowRunSucceeded))
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
		Expect(checkRun.Status.Mode.Steps).Should(BeEquivalentTo(v1alpha1.WorkflowModeDAG))
		Expect(checkRun.Status.Phase).Should(BeEquivalentTo(v1alpha1.WorkflowRunSucceeded))
	})

	It("test failed after retries in step mode with suspend on failure", func() {
		defer featuregatetesting.SetFeatureGateDuringTest(&testing.T{}, utilfeature.DefaultFeatureGate, features.EnableSuspendOnFailure, true)()
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
			Expect(checkRun.Status.Phase).Should(BeEquivalentTo(v1alpha1.WorkflowRunExecuting))
			Expect(checkRun.Status.Steps[1].Phase).Should(BeEquivalentTo(v1alpha1.WorkflowStepPhaseFailed))
		}

		By("workflowrun should be suspended after failed max reconciles")
		tryReconcile(reconciler, wr.Name, wr.Namespace)
		Expect(k8sClient.Get(ctx, wrKey, checkRun)).Should(BeNil())
		Expect(checkRun.Status.Phase).Should(BeEquivalentTo(v1alpha1.WorkflowRunSuspending))
		Expect(checkRun.Status.Message).Should(BeEquivalentTo(wfTypes.MessageSuspendFailedAfterRetries))
		Expect(checkRun.Status.Steps[1].Phase).Should(BeEquivalentTo(v1alpha1.WorkflowStepPhaseFailed))
		Expect(checkRun.Status.Steps[1].Reason).Should(BeEquivalentTo(wfTypes.StatusReasonFailedAfterRetries))

		By("resume the suspended workflow run")
		Expect(k8sClient.Get(ctx, wrKey, checkRun)).Should(BeNil())
		checkRun.Status.Suspend = false
		Expect(k8sClient.Status().Patch(ctx, checkRun, client.Merge)).Should(BeNil())

		tryReconcile(reconciler, wr.Name, wr.Namespace)
		Expect(k8sClient.Get(ctx, wrKey, checkRun)).Should(BeNil())
		Expect(checkRun.Status.Message).Should(BeEquivalentTo(""))
		Expect(checkRun.Status.Phase).Should(BeEquivalentTo(v1alpha1.WorkflowRunExecuting))
		Expect(checkRun.Status.Steps[1].Phase).Should(BeEquivalentTo(v1alpha1.WorkflowStepPhaseFailed))
	})

	It("test failed after retries in dag mode with running step and suspend on failure", func() {
		defer featuregatetesting.SetFeatureGateDuringTest(&testing.T{}, utilfeature.DefaultFeatureGate, features.EnableSuspendOnFailure, true)()
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
			Expect(checkRun.Status.Phase).Should(BeEquivalentTo(v1alpha1.WorkflowRunExecuting))
			Expect(checkRun.Status.Steps[1].Phase).Should(BeEquivalentTo(v1alpha1.WorkflowStepPhaseFailed))
		}

		By("workflowrun should not be suspended after failed max reconciles because of running step")
		tryReconcile(reconciler, wr.Name, wr.Namespace)
		Expect(k8sClient.Get(ctx, wrKey, checkRun)).Should(BeNil())
		Expect(checkRun.Status.Message).Should(BeEquivalentTo(""))
		Expect(checkRun.Status.Phase).Should(BeEquivalentTo(v1alpha1.WorkflowRunExecuting))
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

		tryReconcile(reconciler, wr.Name, wr.Namespace)
		Expect(k8sClient.Get(ctx, wrKey, checkRun)).Should(BeNil())
		Expect(checkRun.Status.Phase).Should(BeEquivalentTo(v1alpha1.WorkflowRunTerminated))
		Expect(checkRun.Status.Steps[0].Phase).Should(BeEquivalentTo(v1alpha1.WorkflowStepPhaseFailed))
		Expect(checkRun.Status.Steps[0].Reason).Should(BeEquivalentTo(wfTypes.StatusReasonRendering))
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
		Expect(checkRun.Status.Phase).Should(BeEquivalentTo(v1alpha1.WorkflowRunSucceeded))
		Expect(checkRun.Status.Mode).Should(BeEquivalentTo(v1alpha1.WorkflowExecuteMode{
			Steps:    v1alpha1.WorkflowModeDAG,
			SubSteps: v1alpha1.WorkflowModeStep,
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
		Expect(checkRun.Status.Phase).Should(BeEquivalentTo(v1alpha1.WorkflowRunSucceeded))
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
		Expect(checkRun.Status.Phase).Should(BeEquivalentTo(v1alpha1.WorkflowRunTerminated))
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
		Expect(checkRun.Status.Phase).Should(BeEquivalentTo(v1alpha1.WorkflowRunTerminated))
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
		Expect(checkRun.Status.Phase).Should(BeEquivalentTo(v1alpha1.WorkflowRunTerminated))
	})

	It("test if expressions", func() {
		wr := wrTemplate.DeepCopy()
		wr.Name = "wr-if-expressions"
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
					},
					If: `status.suspend.timeout && inputs["suspend-output"] == "wr-if-expressions"`,
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
		Expect(checkRun.Status.Phase).Should(BeEquivalentTo(v1alpha1.WorkflowRunSuspending))

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
		Expect(checkRun.Status.Phase).Should(BeEquivalentTo(v1alpha1.WorkflowRunTerminated))
	})

	It("test if expressions in sub steps", func() {
		wr := wrTemplate.DeepCopy()
		wr.Name = "wr-if-expressions-substeps"
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
						If:         "status.sub2.timeout",
						DependsOn:  []string{"sub2"},
						Properties: &runtime.RawExtension{Raw: []byte(`{"cmd":["sleep","1000"],"image":"busybox"}`)},
					},
					{
						Name:       "sub2",
						Type:       "suspend",
						Properties: &runtime.RawExtension{Raw: []byte(`{"duration":"1s"}`)},
						Outputs: v1alpha1.StepOutputs{
							{
								Name:      "suspend-output",
								ValueFrom: "context.name",
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
		Expect(checkRun.Status.Phase).Should(BeEquivalentTo(v1alpha1.WorkflowRunSuspending))

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
		Expect(checkRun.Status.Phase).Should(BeEquivalentTo(v1alpha1.WorkflowRunExecuting))
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
		Expect(checkRun.Status.Phase).Should(BeEquivalentTo(v1alpha1.WorkflowRunSuspending))

		time.Sleep(time.Second)
		tryReconcile(reconciler, wr.Name, wr.Namespace)

		Expect(k8sClient.Get(ctx, wrKey, checkRun)).Should(BeNil())
		Expect(checkRun.Status.Phase).Should(BeEquivalentTo(v1alpha1.WorkflowRunTerminated))
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
		Expect(checkRun.Status.Phase).Should(BeEquivalentTo(v1alpha1.WorkflowRunTerminated))
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
		Expect(checkRun.Status.Phase).Should(BeEquivalentTo(v1alpha1.WorkflowRunSuspending))

		time.Sleep(time.Second)
		tryReconcile(reconciler, wr.Name, wr.Namespace)

		Expect(k8sClient.Get(ctx, wrKey, checkRun)).Should(BeNil())
		Expect(checkRun.Status.Steps[0].Reason).Should(Equal(wfTypes.StatusReasonTimeout))
		Expect(checkRun.Status.Steps[1].Phase).Should(Equal(v1alpha1.WorkflowStepPhaseSucceeded))
		Expect(checkRun.Status.Steps[2].Reason).Should(Equal(wfTypes.StatusReasonSkip))
		Expect(checkRun.Status.Steps[3].Reason).Should(Equal(wfTypes.StatusReasonTimeout))
		Expect(checkRun.Status.Phase).Should(BeEquivalentTo(v1alpha1.WorkflowRunTerminated))
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
		Expect(curRun.Status.Phase).Should(Equal(v1alpha1.WorkflowRunSucceeded))

		By("Check debug Config Map is created")
		debugCM := &corev1.ConfigMap{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{
			Name:      debug.GenerateContextName(wr.Name, "step1"),
			Namespace: wr.Namespace,
		}, debugCM)).Should(BeNil())
		Expect(k8sClient.Get(ctx, types.NamespacedName{
			Name:      debug.GenerateContextName(wr.Name, "step2-sub"),
			Namespace: wr.Namespace,
		}, debugCM)).Should(BeNil())
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

func setupTestDefinitions(ctx context.Context, defs []string, ns string) {
	setupNamespace(ctx, "vela-system")
	for _, def := range defs {
		defJson, err := yaml.YAMLToJSON([]byte(def))
		Expect(err).Should(BeNil())
		u := &unstructured.Unstructured{}
		Expect(json.Unmarshal(defJson, u)).Should(BeNil())
		u.SetNamespace("vela-system")
		Expect(k8sClient.Create(ctx, u)).Should(SatisfyAny(BeNil(), &utils.AlreadyExistMatcher{}))
	}
}

const (
	wfStepApplyDefYaml = `apiVersion: core.oam.dev/v1beta1
kind: WorkflowStepDefinition
metadata:
  name: test-apply
  namespace: vela-system
spec:
  schematic:
    cue:
      template: "import (\n\t\"vela/op\"\n)\n\noutput: op.#Apply & {\n\tvalue: {\n\t\tapiVersion:
        \"apps/v1\"\n\t\tkind:       \"Deployment\"\n\t\tmetadata: {\n\t\t\tname:
        \     context.stepName\n\t\t\tnamespace: context.namespace\n\t\t}\n\t\tspec:
        {\n\t\t\tselector: matchLabels: wr: context.stepName\n\t\t\ttemplate: {\n\t\t\t\tmetadata:
        labels: wr: context.stepName\n\t\t\t\tspec: containers: [{\n\t\t\t\t\tname:
        \ context.stepName\n\t\t\t\t\timage: parameter.image\n\t\t\t\t\tif parameter[\"cmd\"]
        != _|_ {\n\t\t\t\t\t\tcommand: parameter.cmd\n\t\t\t\t\t}\n\t\t\t\t\tif parameter[\"message\"]
        != _|_ {\n\t\t\t\t\t\tenv: [{\n\t\t\t\t\t\t\tname:  \"MESSAGE\"\n\t\t\t\t\t\t\tvalue:
        parameter.message\n\t\t\t\t\t\t}]\n\t\t\t\t\t}\n\t\t\t\t}]\n\t\t\t}\n\t\t}\n\t}\n}\nwait:
        op.#ConditionalWait & {\n\tcontinue: output.value.status.readyReplicas ==
        1\n}\nparameter: {\n\timage:    string\n\tcmd?:     [...string]\n\tmessage?: string\n}\n"
`

	wfStepApplyObjectDefYaml = `apiVersion: core.oam.dev/v1beta1
kind: WorkflowStepDefinition
metadata:
  name: apply-object
  namespace: vela-system
spec:
  schematic:
    cue:
      template: "import (\n\t\"vela/op\"\n)\n\napply: op.#Apply & {\n\tvalue:   parameter.value\n\tcluster:
        parameter.cluster\n}\nparameter: {\n\t// +usage=Specify the value of the object\n\tvalue:
        {...}\n\t// +usage=Specify the cluster of the object\n\tcluster: *\"\" | string\n}\n"`

	wfStepFailedRenderDefYaml = `apiVersion: core.oam.dev/v1beta1
kind: WorkflowStepDefinition
metadata:
  name: failed-render
  namespace: vela-system
spec:
  schematic:
    cue:
      template: ":"`
)
