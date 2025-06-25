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

package generator

import (
	"context"
	"strconv"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	monitorContext "github.com/kubevela/pkg/monitor/context"

	"github.com/kubevela/workflow/api/v1alpha1"
	"github.com/kubevela/workflow/pkg/types"
)

var _ = Describe("Test workflow step runner generator", func() {
	var namespaceName string
	var ns corev1.Namespace
	var ctx context.Context

	BeforeEach(func() {
		namespaceName = "generate-test-" + strconv.Itoa(time.Now().Second()) + "-" + strconv.Itoa(time.Now().Nanosecond())
		ctx = context.TODO()
		ns = corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: namespaceName,
			},
		}
		By("Create the Namespace for test")
		Expect(k8sClient.Create(ctx, &ns)).Should(Succeed())
	})

	AfterEach(func() {
		By("[TEST] Clean up resources after an integration test")
		Expect(k8sClient.Delete(context.TODO(), &ns)).Should(Succeed())
	})

	It("Test generate workflow step runners", func() {
		wr := &v1alpha1.WorkflowRun{
			TypeMeta: metav1.TypeMeta{
				Kind:       "WorkflowRun",
				APIVersion: "core.oam.dev/v1alpha1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "wr",
				Namespace: namespaceName,
			},
			Spec: v1alpha1.WorkflowRunSpec{
				WorkflowSpec: &v1alpha1.WorkflowSpec{
					Steps: []v1alpha1.WorkflowStep{
						{
							WorkflowStepBase: v1alpha1.WorkflowStepBase{
								Name: "step-1",
								Type: "suspend",
								Inputs: v1alpha1.StepInputs{
									{
										From:         "test",
										ParameterKey: "test",
									},
								},
							},
						},
					},
				},
			},
		}
		instance, err := GenerateWorkflowInstance(ctx, k8sClient, wr)
		Expect(err).Should(BeNil())
		ctx := monitorContext.NewTraceContext(ctx, "test-wr")
		runners, err := GenerateRunners(ctx, instance, types.StepGeneratorOptions{})
		Expect(err).Should(BeNil())
		Expect(len(runners)).Should(BeEquivalentTo(1))
		Expect(runners[0].Name()).Should(BeEquivalentTo("step-1"))
	})

	It("Test generate workflow step runners with sub steps", func() {
		wr := &v1alpha1.WorkflowRun{
			TypeMeta: metav1.TypeMeta{
				Kind:       "WorkflowRun",
				APIVersion: "core.oam.dev/v1alpha1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "wf-with-sub-steps",
				Namespace: namespaceName,
			},
			Spec: v1alpha1.WorkflowRunSpec{
				WorkflowSpec: &v1alpha1.WorkflowSpec{
					Steps: []v1alpha1.WorkflowStep{
						{
							WorkflowStepBase: v1alpha1.WorkflowStepBase{
								Name: "step-1",
								Type: "step-group",
							},
							SubSteps: []v1alpha1.WorkflowStepBase{
								{
									Name: "step-1-1",
									Type: "suspend",
								},
								{
									Name: "step-1-2",
									Type: "suspend",
								},
							},
						},
					},
				},
			},
		}
		ctx := monitorContext.NewTraceContext(ctx, "test-wr-sub")
		instance, err := GenerateWorkflowInstance(ctx, k8sClient, wr)
		Expect(err).Should(BeNil())
		runners, err := GenerateRunners(ctx, instance, types.StepGeneratorOptions{})
		Expect(err).Should(BeNil())
		Expect(len(runners)).Should(BeEquivalentTo(1))
		Expect(runners[0].Name()).Should(BeEquivalentTo("step-1"))
	})
})
