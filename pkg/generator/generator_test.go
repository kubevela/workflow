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
	"fmt"
	"strconv"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	monitorContext "github.com/kubevela/pkg/monitor/context"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"

	"github.com/kubevela/workflow/api/v1alpha1"
	wfContext "github.com/kubevela/workflow/pkg/context"
	"github.com/kubevela/workflow/pkg/executor"
	"github.com/kubevela/workflow/pkg/types"

	oamv1alpha1 "github.com/kubevela/pkg/apis/oam/v1alpha1"
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
				WorkflowSpec: &oamv1alpha1.WorkflowSpec{
					Steps: []oamv1alpha1.WorkflowStep{
						{
							WorkflowStepBase: oamv1alpha1.WorkflowStepBase{
								Name: "step-1",
								Type: "suspend",
								Inputs: oamv1alpha1.StepInputs{
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
				WorkflowSpec: &oamv1alpha1.WorkflowSpec{
					Steps: []oamv1alpha1.WorkflowStep{
						{
							WorkflowStepBase: oamv1alpha1.WorkflowStepBase{
								Name: "step-1",
								Type: "step-group",
							},
							SubSteps: []oamv1alpha1.WorkflowStepBase{
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

	It("Test forEach passes the loop item to a real step through inputs", func() {
		wr := &v1alpha1.WorkflowRun{
			TypeMeta: metav1.TypeMeta{
				Kind:       "WorkflowRun",
				APIVersion: "core.oam.dev/v1alpha1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "wr-for-each",
				Namespace: namespaceName,
				UID:       "wr-for-each-uid",
			},
			Spec: v1alpha1.WorkflowRunSpec{
				WorkflowSpec: &oamv1alpha1.WorkflowSpec{
					Steps: []oamv1alpha1.WorkflowStep{
						{
							WorkflowStepBase: oamv1alpha1.WorkflowStepBase{
								Name: "deploy",
								Type: "region-step",
								Inputs: oamv1alpha1.StepInputs{
									{From: "loop.item.name", ParameterKey: "region"},
									{From: "loop.index", ParameterKey: "wave"},
								},
								Outputs: oamv1alpha1.StepOutputs{{Name: "result", ValueFrom: "value"}},
							},
							ForEach: &oamv1alpha1.ForEach{Items: &apiextensionsv1.JSON{Raw: []byte(`[{"name":"a"},{"name":"b"}]`)}},
						},
						{
							WorkflowStepBase: oamv1alpha1.WorkflowStepBase{
								Name:    "join",
								Type:    "join-step",
								Inputs:  oamv1alpha1.StepInputs{{From: "result", ParameterKey: "results"}},
								Outputs: oamv1alpha1.StepOutputs{{Name: "joined", ValueFrom: "value"}},
							},
						},
					},
				},
			},
		}
		loader := staticLoader{
			"region-step": `
parameter: {region: string, wave: int}
value: "\(parameter.region)-\(parameter.wave)"
`,
			"join-step": `
import "strings"
parameter: results: [...string]
value: strings.Join(parameter.results, ",")
`,
		}
		instance, err := GenerateWorkflowInstance(ctx, k8sClient, wr)
		Expect(err).Should(BeNil())
		mCtx := monitorContext.NewTraceContext(ctx, "test-wr")
		runners, err := GenerateRunners(mCtx, instance, types.StepGeneratorOptions{TemplateLoader: loader})
		Expect(err).Should(BeNil())

		state, err := executor.New(instance).ExecuteRunners(mCtx, runners)
		Expect(err).Should(BeNil())
		Expect(state).Should(BeEquivalentTo(v1alpha1.WorkflowStateSucceeded))
		Expect(instance.Status.Steps[0].SubStepsStatus).Should(HaveLen(2))
		Expect(instance.Status.Steps[0].SubStepsStatus[1].Name).Should(Equal("deploy-1"))

		wfCtx, err := wfContext.LoadContext(ctx, namespaceName, wr.Name, instance.Status.ContextBackend.Name)
		Expect(err).Should(BeNil())
		joined, err := wfCtx.GetVar("joined")
		Expect(err).Should(BeNil())
		Expect(joined.String()).Should(Equal("a-0,b-1"))
	})

	It("Test generate workflow instance from a workflowRef in vela-system namespace", func() {
		systemNS := corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "vela-system"}}
		if err := k8sClient.Create(ctx, &systemNS); err != nil && !kerrors.IsAlreadyExists(err) {
			Expect(err).Should(BeNil())
		}

		workflow := &oamv1alpha1.Workflow{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "shared-workflow-" + namespaceName,
				Namespace: "vela-system",
			},
			Mode: &oamv1alpha1.WorkflowExecuteMode{
				Steps: v1alpha1.WorkflowModeDAG,
			},
			WorkflowSpec: oamv1alpha1.WorkflowSpec{
				Steps: []oamv1alpha1.WorkflowStep{
					{
						WorkflowStepBase: oamv1alpha1.WorkflowStepBase{
							Name: "step-1",
							Type: "suspend",
						},
					},
				},
			},
		}
		Expect(k8sClient.Create(ctx, workflow)).Should(Succeed())
		defer func() {
			Expect(k8sClient.Delete(ctx, workflow)).Should(Succeed())
		}()

		wr := &v1alpha1.WorkflowRun{
			TypeMeta: metav1.TypeMeta{
				Kind:       "WorkflowRun",
				APIVersion: "core.oam.dev/v1alpha1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "wr-ref-cross-ns",
				Namespace: namespaceName,
			},
			Spec: v1alpha1.WorkflowRunSpec{
				WorkflowRef: workflow.Name,
			},
		}
		instance, err := GenerateWorkflowInstance(ctx, k8sClient, wr)
		Expect(err).Should(BeNil())
		Expect(len(instance.Steps)).Should(BeEquivalentTo(1))
		Expect(instance.Steps[0].Name).Should(BeEquivalentTo("step-1"))
		Expect(instance.Mode.Steps).Should(BeEquivalentTo(v1alpha1.WorkflowModeDAG))
	})

	It("Test generate workflow instance from a missing workflowRef", func() {
		wr := &v1alpha1.WorkflowRun{
			TypeMeta: metav1.TypeMeta{
				Kind:       "WorkflowRun",
				APIVersion: "core.oam.dev/v1alpha1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "wr-ref-missing",
				Namespace: namespaceName,
			},
			Spec: v1alpha1.WorkflowRunSpec{
				WorkflowRef: "does-not-exist",
			},
		}
		_, err := GenerateWorkflowInstance(ctx, k8sClient, wr)
		Expect(err).ShouldNot(BeNil())
	})
})

type staticLoader map[string]string

func (l staticLoader) LoadTemplate(_ context.Context, name string) (string, error) {
	templ, ok := l[name]
	if !ok {
		return "", fmt.Errorf("no template for step type %q", name)
	}
	return templ, nil
}
