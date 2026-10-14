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
	"k8s.io/apimachinery/pkg/runtime"
	k8stypes "k8s.io/apimachinery/pkg/types"

	"github.com/kubevela/pkg/cel"
	monitorContext "github.com/kubevela/pkg/monitor/context"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"

	"github.com/kubevela/workflow/api/v1alpha1"
	wfContext "github.com/kubevela/workflow/pkg/context"
	"github.com/kubevela/workflow/pkg/executor"
	"github.com/kubevela/workflow/pkg/expressions"
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
									{From: "each.item.name", ParameterKey: "region"},
									{From: "each.index", ParameterKey: "wave"},
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

	It("Test a sub-step's properties read an earlier step's output when it runs", func() {
		wr := &v1alpha1.WorkflowRun{
			TypeMeta:   metav1.TypeMeta{Kind: "WorkflowRun", APIVersion: "core.oam.dev/v1alpha1"},
			ObjectMeta: metav1.ObjectMeta{Name: "wr-expressions", Namespace: namespaceName, UID: "wr-expressions-uid"},
			Spec: v1alpha1.WorkflowRunSpec{
				WorkflowSpec: &oamv1alpha1.WorkflowSpec{
					Steps: []oamv1alpha1.WorkflowStep{
						{
							WorkflowStepBase: oamv1alpha1.WorkflowStepBase{
								Name:       "build",
								Type:       "echo",
								Properties: &runtime.RawExtension{Raw: []byte(`{"msg":"acme/api:1.2"}`)},
								Outputs:    oamv1alpha1.StepOutputs{{Name: "image", ValueFrom: "value"}},
							},
						},
						{
							WorkflowStepBase: oamv1alpha1.WorkflowStepBase{Name: "group", Type: "step-group"},
							SubSteps: []oamv1alpha1.WorkflowStepBase{{
								Name:       "notify",
								Type:       "echo",
								Properties: &runtime.RawExtension{Raw: []byte(`{"msg":"$(steps.build.outputs.image) for $(context.name)"}`)},
								Outputs:    oamv1alpha1.StepOutputs{{Name: "message", ValueFrom: "value"}},
							}},
						},
					},
				},
			},
		}
		loader := staticLoader{"echo": `
parameter: msg: string
value: parameter.msg
`}
		instance, err := GenerateWorkflowInstance(ctx, k8sClient, wr)
		Expect(err).Should(BeNil())
		mCtx := monitorContext.NewTraceContext(ctx, "test-wr")
		runners, err := GenerateRunners(mCtx, instance, types.StepGeneratorOptions{
			TemplateLoader: loader, Expressions: &types.ExpressionOptions{}})
		Expect(err).Should(BeNil())

		state, err := executor.New(instance).ExecuteRunners(mCtx, runners)
		Expect(err).Should(BeNil())
		Expect(state).Should(BeEquivalentTo(v1alpha1.WorkflowStateSucceeded))

		wfCtx, err := wfContext.LoadContext(ctx, namespaceName, wr.Name, instance.Status.ContextBackend.Name)
		Expect(err).Should(BeNil())
		message, err := wfCtx.GetVar("message")
		Expect(err).Should(BeNil())
		Expect(message.String()).Should(Equal("acme/api:1.2 for wr-expressions"))
	})

	It("Test a guarded read of a skipped step takes its default, in either mode", func() {
		loader := staticLoader{"echo": `
parameter: msg: string
value: parameter.msg
`}
		guarded := `$(has(steps.a.outputs.o) ? steps.a.outputs.o : \"none\")`
		for i, c := range []struct {
			mode      oamv1alpha1.WorkflowMode
			ifB       string
			dependsOn bool
			read      string
			state     v1alpha1.WorkflowRunPhase
			phase     v1alpha1.WorkflowStepPhase
			message   string
			fallback  bool
		}{
			{mode: v1alpha1.WorkflowModeStep, read: guarded, state: v1alpha1.WorkflowStateSucceeded,
				phase: v1alpha1.WorkflowStepPhaseSucceeded, fallback: true},
			{mode: v1alpha1.WorkflowModeDAG, ifB: "always", read: guarded, state: v1alpha1.WorkflowStateSucceeded,
				phase: v1alpha1.WorkflowStepPhaseSucceeded, fallback: true},
			// The read's implied dependsOn does not skip b as a written one would: b
			// runs, as it would reading through inputs.
			{mode: v1alpha1.WorkflowModeDAG, read: guarded, state: v1alpha1.WorkflowStateSucceeded,
				phase: v1alpha1.WorkflowStepPhaseSucceeded, fallback: true},
			{mode: v1alpha1.WorkflowModeDAG, read: `$(steps.a.outputs.o)`, state: v1alpha1.WorkflowStateFailed,
				phase: v1alpha1.WorkflowStepPhaseFailed, message: "step a was skipped"},
			// A dependsOn written in the spec still skips b, as it always has.
			{mode: v1alpha1.WorkflowModeDAG, dependsOn: true, read: guarded, state: v1alpha1.WorkflowStateSucceeded,
				phase: v1alpha1.WorkflowStepPhaseSkipped},
			{mode: v1alpha1.WorkflowModeStep, ifB: "always", read: `$(steps.a.outputs.o)`, state: v1alpha1.WorkflowStateFailed,
				phase: v1alpha1.WorkflowStepPhaseFailed, message: "step a was skipped"},
		} {
			name := fmt.Sprintf("wr-skipped-producer-%d", i)
			wr := &v1alpha1.WorkflowRun{
				TypeMeta:   metav1.TypeMeta{Kind: "WorkflowRun", APIVersion: "core.oam.dev/v1alpha1"},
				ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespaceName, UID: k8stypes.UID(name)},
				Spec: v1alpha1.WorkflowRunSpec{
					Mode: &oamv1alpha1.WorkflowExecuteMode{Steps: c.mode},
					WorkflowSpec: &oamv1alpha1.WorkflowSpec{Steps: []oamv1alpha1.WorkflowStep{
						{WorkflowStepBase: oamv1alpha1.WorkflowStepBase{Name: "a", Type: "echo", If: "false",
							Properties: &runtime.RawExtension{Raw: []byte(`{"msg":"x"}`)},
							Outputs:    oamv1alpha1.StepOutputs{{Name: "o", ValueFrom: "value"}}}},
						{WorkflowStepBase: oamv1alpha1.WorkflowStepBase{Name: "b", Type: "echo", If: c.ifB,
							Properties: &runtime.RawExtension{Raw: []byte(`{"msg":"` + c.read + `"}`)}}},
					}},
				},
			}
			if c.dependsOn {
				wr.Spec.WorkflowSpec.Steps[1].DependsOn = []string{"a"}
			}
			// b records what it read, where it reads at all.
			if c.fallback {
				wr.Spec.WorkflowSpec.Steps[1].Outputs = oamv1alpha1.StepOutputs{{Name: "got", ValueFrom: "value"}}
			}
			instance, err := GenerateWorkflowInstance(ctx, k8sClient, wr)
			Expect(err).Should(BeNil())
			mCtx := monitorContext.NewTraceContext(ctx, "test-wr")
			runners, err := GenerateRunners(mCtx, instance, types.StepGeneratorOptions{
				TemplateLoader: loader, Expressions: &types.ExpressionOptions{}})
			Expect(err).Should(BeNil())
			state, err := executor.New(instance).ExecuteRunners(mCtx, runners)
			Expect(err).Should(BeNil())
			b := instance.Status.Steps[1]
			Expect(state).Should(BeEquivalentTo(c.state), name)
			Expect(b.Phase).Should(BeEquivalentTo(c.phase), name)
			Expect(b.Message).Should(ContainSubstring(c.message), name)
			if c.fallback {
				wfCtx, err := wfContext.LoadContext(ctx, namespaceName, wr.Name, instance.Status.ContextBackend.Name)
				Expect(err).Should(BeNil())
				got, err := wfCtx.GetVar("got")
				Expect(err).Should(BeNil())
				Expect(got.String()).Should(Equal("none"), name)
			}
		}
	})

	It("Test a body step reading a skipped sibling in a forEach still runs", func() {
		wr := &v1alpha1.WorkflowRun{
			TypeMeta:   metav1.TypeMeta{Kind: "WorkflowRun", APIVersion: "core.oam.dev/v1alpha1"},
			ObjectMeta: metav1.ObjectMeta{Name: "wr-skipped-sibling", Namespace: namespaceName, UID: "wr-skipped-sibling-uid"},
			Spec: v1alpha1.WorkflowRunSpec{
				Mode: &oamv1alpha1.WorkflowExecuteMode{Steps: v1alpha1.WorkflowModeDAG, SubSteps: v1alpha1.WorkflowModeDAG},
				WorkflowSpec: &oamv1alpha1.WorkflowSpec{Steps: []oamv1alpha1.WorkflowStep{{
					WorkflowStepBase: oamv1alpha1.WorkflowStepBase{Name: "loop", Type: "step-group"},
					ForEach:          &oamv1alpha1.ForEach{Items: &apiextensionsv1.JSON{Raw: []byte(`["x"]`)}},
					SubSteps: []oamv1alpha1.WorkflowStepBase{
						{Name: "a", Type: "echo", If: "false", Properties: &runtime.RawExtension{Raw: []byte(`{"msg":"x"}`)},
							Outputs: oamv1alpha1.StepOutputs{{Name: "o", ValueFrom: "value"}}},
						{Name: "b", Type: "echo",
							Properties: &runtime.RawExtension{Raw: []byte(`{"msg":"$(has(steps.a.outputs.o) ? steps.a.outputs.o : \"none\")"}`)},
							Outputs:    oamv1alpha1.StepOutputs{{Name: "got", ValueFrom: "value"}}},
					},
				}}},
			},
		}
		loader := staticLoader{"echo": `
parameter: msg: string
value: parameter.msg
`}
		instance, err := GenerateWorkflowInstance(ctx, k8sClient, wr)
		Expect(err).Should(BeNil())
		mCtx := monitorContext.NewTraceContext(ctx, "test-wr")
		runners, err := GenerateRunners(mCtx, instance, types.StepGeneratorOptions{
			TemplateLoader: loader, Expressions: &types.ExpressionOptions{}})
		Expect(err).Should(BeNil())
		state, err := executor.New(instance).ExecuteRunners(mCtx, runners)
		Expect(err).Should(BeNil())
		Expect(state).Should(BeEquivalentTo(v1alpha1.WorkflowStateSucceeded), "%+v", instance.Status.Steps)
		Expect(subPhases(instance.Status.Steps[0])).Should(Equal(map[string]v1alpha1.WorkflowStepPhase{
			"loop-0-a": v1alpha1.WorkflowStepPhaseSkipped,
			"loop-0-b": v1alpha1.WorkflowStepPhaseSucceeded,
		}))
	})

	It("Test a host resolver sees one run across the run's reconciles", func() {
		wr := &v1alpha1.WorkflowRun{
			TypeMeta:   metav1.TypeMeta{Kind: "WorkflowRun", APIVersion: "core.oam.dev/v1alpha1"},
			ObjectMeta: metav1.ObjectMeta{Name: "wr-run-scope", Namespace: namespaceName, UID: "wr-run-scope-uid"},
			Spec: v1alpha1.WorkflowRunSpec{
				WorkflowSpec: &oamv1alpha1.WorkflowSpec{
					Steps: []oamv1alpha1.WorkflowStep{{
						WorkflowStepBase: oamv1alpha1.WorkflowStepBase{
							Name:       "pinned",
							Type:       "echo",
							Properties: &runtime.RawExtension{Raw: []byte(`{"msg":"$(probe.v)"}`)},
						},
					}},
				},
			},
		}
		loader := staticLoader{"echo": `
parameter: msg: string
value: parameter.msg
`}
		// Answers on its second call only, so the step waits into a second reconcile.
		var runs []string
		probe := cel.ResolverFunc(func(ctx context.Context, _ []cel.Read) (interface{}, error) {
			scope, ok := expressions.RunFrom(ctx)
			Expect(ok).Should(BeTrue())
			runs = append(runs, scope.Run)
			if len(runs) == 1 {
				return nil, fmt.Errorf("not yet: %w", cel.ErrNotReady)
			}
			return map[string]interface{}{"v": "ok"}, nil
		})
		opts := types.StepGeneratorOptions{TemplateLoader: loader, Expressions: &types.ExpressionOptions{
			Roots:     []cel.Root{{Name: "probe"}},
			Resolvers: map[string]cel.Resolver{"probe": probe},
		}}
		instance, err := GenerateWorkflowInstance(ctx, k8sClient, wr)
		Expect(err).Should(BeNil())
		mCtx := monitorContext.NewTraceContext(ctx, "test-wr")
		reconcile := func() v1alpha1.WorkflowRunPhase {
			runners, err := GenerateRunners(mCtx, instance, opts)
			Expect(err).Should(BeNil())
			state, err := executor.New(instance).ExecuteRunners(mCtx, runners)
			Expect(err).Should(BeNil())
			return state
		}

		Expect(reconcile()).ShouldNot(BeEquivalentTo(v1alpha1.WorkflowStateSucceeded))
		// The status the next reconcile reads has been through the API server, which
		// keeps a time to the second.
		instance.Status.StartTime = metav1.NewTime(instance.Status.StartTime.Truncate(time.Second))
		Expect(reconcile()).Should(BeEquivalentTo(v1alpha1.WorkflowStateSucceeded))

		Expect(runs).Should(HaveLen(2))
		Expect(runs[0]).ShouldNot(Equal(metav1.Time{}.UTC().Format(time.RFC3339)), "the run is known before its first reconcile")
		Expect(runs[1]).Should(Equal(runs[0]), "a resolver holding a value for the run finds it again")
	})

	It("Test forEach takes its items from an earlier step and its body reads each and its siblings", func() {
		props := func(s string) *runtime.RawExtension { return &runtime.RawExtension{Raw: []byte(s)} }
		wr := &v1alpha1.WorkflowRun{
			TypeMeta:   metav1.TypeMeta{Kind: "WorkflowRun", APIVersion: "core.oam.dev/v1alpha1"},
			ObjectMeta: metav1.ObjectMeta{Name: "wr-for-each-expressions", Namespace: namespaceName, UID: "wr-for-each-expressions-uid"},
			Spec: v1alpha1.WorkflowRunSpec{
				WorkflowSpec: &oamv1alpha1.WorkflowSpec{
					Steps: []oamv1alpha1.WorkflowStep{
						{WorkflowStepBase: oamv1alpha1.WorkflowStepBase{
							Name: "list", Type: "lister",
							Outputs: oamv1alpha1.StepOutputs{{Name: "tenants", ValueFrom: "value"}},
						}},
						{
							WorkflowStepBase: oamv1alpha1.WorkflowStepBase{Name: "per-tenant", Type: "step-group"},
							ForEach:          &oamv1alpha1.ForEach{Items: &apiextensionsv1.JSON{Raw: []byte(`"$(steps.list.outputs.tenants)"`)}},
							SubSteps: []oamv1alpha1.WorkflowStepBase{
								{Name: "fetch", Type: "echo", Properties: props(`{"msg":"$(each.item.name)-data"}`),
									Outputs: oamv1alpha1.StepOutputs{{Name: "data", ValueFrom: "value"}}},
								{Name: "greet", Type: "echo", DependsOn: []string{"fetch"},
									Properties: props(`{"msg":"hi $(steps.fetch.outputs.data) #$(each.index)"}`),
									Outputs:    oamv1alpha1.StepOutputs{{Name: "msg", ValueFrom: "value"}}},
							},
						},
						{WorkflowStepBase: oamv1alpha1.WorkflowStepBase{
							Name: "summary", Type: "echo",
							Properties: props(`{"msg":"$(steps.greet.outputs.msg.join(\", \"))"}`),
							Outputs:    oamv1alpha1.StepOutputs{{Name: "summary", ValueFrom: "value"}},
						}},
					},
				},
			},
		}
		loader := staticLoader{
			"lister": `
parameter: {}
value: [{name: "a"}, {name: "b"}]
`,
			"echo": `
parameter: msg: string
value: parameter.msg
`,
		}
		instance, err := GenerateWorkflowInstance(ctx, k8sClient, wr)
		Expect(err).Should(BeNil())
		mCtx := monitorContext.NewTraceContext(ctx, "test-wr")
		runners, err := GenerateRunners(mCtx, instance, types.StepGeneratorOptions{
			TemplateLoader: loader, Expressions: &types.ExpressionOptions{}})
		Expect(err).Should(BeNil())

		state, err := executor.New(instance).ExecuteRunners(mCtx, runners)
		Expect(err).Should(BeNil())
		Expect(state).Should(BeEquivalentTo(v1alpha1.WorkflowStateSucceeded), "%+v", instance.Status.Steps)

		wfCtx, err := wfContext.LoadContext(ctx, namespaceName, wr.Name, instance.Status.ContextBackend.Name)
		Expect(err).Should(BeNil())
		summary, err := wfCtx.GetVar("summary")
		Expect(err).Should(BeNil())
		Expect(summary.String()).Should(Equal("hi a-data #0, hi b-data #1"))
	})

	It("Test a step reading another's output in a DAG waits for it through dependsOn", func() {
		wr := &v1alpha1.WorkflowRun{
			TypeMeta:   metav1.TypeMeta{Kind: "WorkflowRun", APIVersion: "core.oam.dev/v1alpha1"},
			ObjectMeta: metav1.ObjectMeta{Name: "wr-read-dependencies", Namespace: namespaceName, UID: "wr-read-dependencies-uid"},
			Spec: v1alpha1.WorkflowRunSpec{
				Mode: &oamv1alpha1.WorkflowExecuteMode{Steps: v1alpha1.WorkflowModeDAG},
				WorkflowSpec: &oamv1alpha1.WorkflowSpec{
					Steps: []oamv1alpha1.WorkflowStep{
						{WorkflowStepBase: oamv1alpha1.WorkflowStepBase{
							Name: "notify", Type: "echo", DependsOn: []string{"cfg"},
							Properties: &runtime.RawExtension{Raw: []byte(`{"msg":"built $(steps.build.outputs.image)"}`)},
							Outputs:    oamv1alpha1.StepOutputs{{Name: "message", ValueFrom: "value"}},
						}},
						{WorkflowStepBase: oamv1alpha1.WorkflowStepBase{
							Name: "build", Type: "echo",
							Properties: &runtime.RawExtension{Raw: []byte(`{"msg":"acme/api:1.2"}`)},
							Outputs:    oamv1alpha1.StepOutputs{{Name: "image", ValueFrom: "value"}},
						}},
						{WorkflowStepBase: oamv1alpha1.WorkflowStepBase{
							Name: "cfg", Type: "echo",
							Properties: &runtime.RawExtension{Raw: []byte(`{"msg":"cfg"}`)},
						}},
					},
				},
			},
		}
		loader := staticLoader{"echo": `
parameter: msg: string
value: parameter.msg
`}
		instance, err := GenerateWorkflowInstance(ctx, k8sClient, wr)
		Expect(err).Should(BeNil())
		mCtx := monitorContext.NewTraceContext(ctx, "test-wr")
		runners, err := GenerateRunners(mCtx, instance, types.StepGeneratorOptions{
			TemplateLoader: loader, Expressions: &types.ExpressionOptions{}})
		Expect(err).Should(BeNil())
		Expect(instance.Steps[0].DependsOn).Should(Equal([]string{"cfg", "build"}))
		Expect(wr.Spec.WorkflowSpec.Steps[0].DependsOn).Should(Equal([]string{"cfg"}), "the spec is not changed")

		state, err := executor.New(instance).ExecuteRunners(mCtx, runners)
		Expect(err).Should(BeNil())
		Expect(state).Should(BeEquivalentTo(v1alpha1.WorkflowStateSucceeded), "%+v", instance.Status.Steps)

		By("the status records each step's dependencies and where each came from")
		Expect(instance.Status.Dependencies).Should(Equal([]v1alpha1.StepDependency{
			{Step: "notify", DependsOn: "cfg", Source: v1alpha1.StepDependencySourceDependsOn},
			{Step: "notify", DependsOn: "build", Source: v1alpha1.StepDependencySourceExpression},
		}))
	})

	It("Test a read in a workflow run in order does not run its step after an earlier failure", func() {
		props := func(s string) *runtime.RawExtension { return &runtime.RawExtension{Raw: []byte(s)} }
		wr := &v1alpha1.WorkflowRun{
			TypeMeta:   metav1.TypeMeta{Kind: "WorkflowRun", APIVersion: "core.oam.dev/v1alpha1"},
			ObjectMeta: metav1.ObjectMeta{Name: "wr-in-order-failure", Namespace: namespaceName, UID: "wr-in-order-failure-uid"},
			Spec: v1alpha1.WorkflowRunSpec{
				WorkflowSpec: &oamv1alpha1.WorkflowSpec{
					Steps: []oamv1alpha1.WorkflowStep{
						{WorkflowStepBase: oamv1alpha1.WorkflowStepBase{
							Name: "build", Type: "echo", Properties: props(`{"msg":"acme/api:1.2"}`),
							Outputs: oamv1alpha1.StepOutputs{{Name: "image", ValueFrom: "value"}},
						}},
						{WorkflowStepBase: oamv1alpha1.WorkflowStepBase{Name: "test", Type: "fail"}},
						{WorkflowStepBase: oamv1alpha1.WorkflowStepBase{
							Name: "deploy", Type: "echo", Properties: props(`{"msg":"$(steps.build.outputs.image)"}`),
							Outputs: oamv1alpha1.StepOutputs{{Name: "deployed", ValueFrom: "value"}},
						}},
					},
				},
			},
		}
		loader := staticLoader{
			"echo": `
parameter: msg: string
value: parameter.msg
`,
			"fail": `
import "vela/builtin"
parameter: {}
fail: builtin.#Fail & {$params: message: "tests failed"}
`,
		}
		instance, err := GenerateWorkflowInstance(ctx, k8sClient, wr)
		Expect(err).Should(BeNil())
		mCtx := monitorContext.NewTraceContext(ctx, "test-wr")
		runners, err := GenerateRunners(mCtx, instance, types.StepGeneratorOptions{
			TemplateLoader: loader, Expressions: &types.ExpressionOptions{}})
		Expect(err).Should(BeNil())
		Expect(instance.Steps[2].DependsOn).Should(BeEmpty(), "the order already puts build first")
		Expect(instance.Status.Dependencies).Should(BeEmpty())

		_, err = executor.New(instance).ExecuteRunners(mCtx, runners)
		Expect(err).Should(BeNil())
		phases := map[string]v1alpha1.WorkflowStepPhase{}
		for _, ss := range instance.Status.Steps {
			phases[ss.Name] = ss.Phase
		}
		Expect(phases["test"]).Should(Equal(v1alpha1.WorkflowStepPhaseFailed))
		Expect(phases["deploy"]).Should(Equal(v1alpha1.WorkflowStepPhaseSkipped),
			"as it would be with no read: a step after a failure in order is skipped")
		Expect(instance.Status.Dependencies).Should(Equal([]v1alpha1.StepDependency{
			{Step: "deploy", DependsOn: "build", Source: v1alpha1.StepDependencySourceExpression}}),
			"the read is still recorded")
	})

	It("Test a loop whose body reads a skipped step outside it still runs, with its guard", func() {
		props := func(s string) *runtime.RawExtension { return &runtime.RawExtension{Raw: []byte(s)} }
		wr := &v1alpha1.WorkflowRun{
			TypeMeta:   metav1.TypeMeta{Kind: "WorkflowRun", APIVersion: "core.oam.dev/v1alpha1"},
			ObjectMeta: metav1.ObjectMeta{Name: "wr-loop-reads-skipped", Namespace: namespaceName, UID: "wr-loop-reads-skipped-uid"},
			Spec: v1alpha1.WorkflowRunSpec{
				Mode: &oamv1alpha1.WorkflowExecuteMode{Steps: v1alpha1.WorkflowModeDAG},
				WorkflowSpec: &oamv1alpha1.WorkflowSpec{
					Steps: []oamv1alpha1.WorkflowStep{
						{WorkflowStepBase: oamv1alpha1.WorkflowStepBase{
							Name: "build", Type: "echo", If: "false", Properties: props(`{"msg":"acme/api:1.2"}`),
							Outputs: oamv1alpha1.StepOutputs{{Name: "image", ValueFrom: "value"}},
						}},
						{
							WorkflowStepBase: oamv1alpha1.WorkflowStepBase{Name: "per-tenant", Type: "step-group"},
							ForEach:          &oamv1alpha1.ForEach{Items: &apiextensionsv1.JSON{Raw: []byte(`["a"]`)}},
							SubSteps: []oamv1alpha1.WorkflowStepBase{{
								Name: "greet", Type: "echo",
								Properties: props(`{"msg":"$(has(steps.build.outputs.image) ? steps.build.outputs.image : \"none\") for $(each.item)"}`),
								Outputs:    oamv1alpha1.StepOutputs{{Name: "greeting", ValueFrom: "value"}},
							}},
						},
					},
				},
			},
		}
		loader := staticLoader{"echo": `
parameter: msg: string
value: parameter.msg
`}
		instance, err := GenerateWorkflowInstance(ctx, k8sClient, wr)
		Expect(err).Should(BeNil())
		mCtx := monitorContext.NewTraceContext(ctx, "test-wr")
		runners, err := GenerateRunners(mCtx, instance, types.StepGeneratorOptions{
			TemplateLoader: loader, Expressions: &types.ExpressionOptions{}})
		Expect(err).Should(BeNil())
		state, err := executor.New(instance).ExecuteRunners(mCtx, runners)
		Expect(err).Should(BeNil())
		Expect(state).Should(BeEquivalentTo(v1alpha1.WorkflowStateSucceeded), "%+v", instance.Status.Steps)
		wfCtx, err := wfContext.LoadContext(ctx, namespaceName, wr.Name, instance.Status.ContextBackend.Name)
		Expect(err).Should(BeNil())
		greeting, err := wfCtx.GetVar("greeting")
		Expect(err).Should(BeNil())
		got, err := greeting.MarshalJSON()
		Expect(err).Should(BeNil())
		Expect(string(got)).Should(Equal(`["none for a"]`), "the loop ran, and its guard answered the skipped read")
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

func subPhases(step v1alpha1.WorkflowStepStatus) map[string]v1alpha1.WorkflowStepPhase {
	out := map[string]v1alpha1.WorkflowStepPhase{}
	for _, sub := range step.SubStepsStatus {
		out[sub.Name] = sub.Phase
	}
	return out
}
