/*
Copyright 2026 The KubeVela Authors.

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

package expressions

import (
	"context"
	"errors"
	"testing"

	"github.com/kubevela/pkg/apis/oam/v1alpha1"
	"github.com/kubevela/pkg/cel"
	"github.com/stretchr/testify/require"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/runtime"

	wfv1alpha1 "github.com/kubevela/workflow/api/v1alpha1"
	"github.com/kubevela/workflow/pkg/types"
)

func TestReadDependencies(t *testing.T) {
	out := func(name string) v1alpha1.StepOutputs { return v1alpha1.StepOutputs{{Name: name, ValueFrom: "value"}} }
	step := func(name string, props string, dependsOn ...string) v1alpha1.WorkflowStepBase {
		s := v1alpha1.WorkflowStepBase{Name: name, Type: "echo", DependsOn: dependsOn, Outputs: out(name + "-out")}
		if props != "" {
			s.Properties = &runtime.RawExtension{Raw: []byte(props)}
		}
		return s
	}
	reads := func(producer string) string {
		return `{"x":"$(steps[\"` + producer + `\"].outputs[\"` + producer + `-out\"])"}`
	}
	top := func(s v1alpha1.WorkflowStepBase) v1alpha1.WorkflowStep {
		return v1alpha1.WorkflowStep{WorkflowStepBase: s}
	}
	group := func(name string, subs ...v1alpha1.WorkflowStepBase) v1alpha1.WorkflowStep {
		return v1alpha1.WorkflowStep{WorkflowStepBase: v1alpha1.WorkflowStepBase{Name: name, Type: types.WorkflowStepTypeStepGroup}, SubSteps: subs}
	}
	loop := func(s v1alpha1.WorkflowStep, items string) v1alpha1.WorkflowStep {
		s.ForEach = &v1alpha1.ForEach{Items: &apiextensionsv1.JSON{Raw: []byte(items)}}
		return s
	}
	dag := &v1alpha1.WorkflowExecuteMode{Steps: wfv1alpha1.WorkflowModeDAG, SubSteps: wfv1alpha1.WorkflowModeDAG}
	subInOrder := &v1alpha1.WorkflowExecuteMode{Steps: wfv1alpha1.WorkflowModeDAG, SubSteps: wfv1alpha1.WorkflowModeStep}

	type deps map[string][]string
	for _, tc := range []struct {
		name  string
		mode  *v1alpha1.WorkflowExecuteMode
		steps []v1alpha1.WorkflowStep
		want  deps
	}{
		{name: "a read of an earlier step",
			mode:  dag,
			steps: []v1alpha1.WorkflowStep{top(step("build", "")), top(step("notify", reads("build")))},
			want:  deps{"notify": {"build"}}},
		{name: "a written dependsOn is kept and not repeated",
			mode: dag,
			steps: []v1alpha1.WorkflowStep{top(step("build", "")), top(step("cfg", "")),
				top(step("notify", reads("build"), "cfg", "build"))},
			want: deps{"notify": {"cfg", "build"}}},
		{name: "in order by default: no edge to a later step",
			steps: []v1alpha1.WorkflowStep{top(step("notify", reads("build"))), top(step("build", ""))},
			want:  deps{}},
		{name: "in order by default: an earlier step needs no edge, the order puts it first",
			steps: []v1alpha1.WorkflowStep{top(step("build", "")), top(step("notify", reads("build")))},
			want:  deps{}},
		{name: "an edge that would close a cycle is left out",
			mode:  dag,
			steps: []v1alpha1.WorkflowStep{top(step("build", "", "notify")), top(step("notify", reads("build")))},
			want:  deps{"build": {"notify"}}},
		{name: "sibling sub-steps",
			mode:  dag,
			steps: []v1alpha1.WorkflowStep{group("g", step("build", ""), step("notify", reads("build")))},
			want:  deps{"notify": {"build"}}},
		{name: "sibling sub-steps in order: no edge to a later sibling",
			mode:  subInOrder,
			steps: []v1alpha1.WorkflowStep{group("g", step("notify", reads("build")), step("build", ""))},
			want:  deps{}},
		{name: "a sub-step reading another step's output: the group waits",
			mode:  dag,
			steps: []v1alpha1.WorkflowStep{top(step("build", "")), group("g", step("notify", reads("build")))},
			want:  deps{"g": {"build"}}},
		{name: "a step reading a body step's output waits on the loop",
			mode: dag,
			steps: []v1alpha1.WorkflowStep{loop(group("per-tenant", step("greet", "")), `[1]`),
				top(step("summary", reads("greet")))},
			want: deps{"summary": {"per-tenant"}}},
		{name: "body siblings, as forEach scopes them per iteration",
			mode:  dag,
			steps: []v1alpha1.WorkflowStep{loop(group("per-tenant", step("fetch", ""), step("greet", reads("fetch"))), `[1]`)},
			want:  deps{"greet": {"fetch"}}},
		{name: "a loop whose items read a step",
			mode: dag,
			steps: []v1alpha1.WorkflowStep{top(step("list", "")),
				loop(group("per-tenant", step("greet", "")), `"$(steps.list.outputs[\"list-out\"])"`)},
			want: deps{"per-tenant": {"list"}}},
		{name: "an edge lifted to the reader's group that would wait on the group's own sub-step is left out",
			mode: dag,
			steps: []v1alpha1.WorkflowStep{
				group("g", step("setup", ""), step("use", reads("t"))),
				top(step("t", "", "setup")),
			},
			want: deps{"t": {"setup"}}},
		{name: "a step reading itself gets no edge",
			mode:  dag,
			steps: []v1alpha1.WorkflowStep{top(step("notify", reads("notify")))},
			want:  deps{}},
		{name: "a read of no step gets no edge",
			mode:  dag,
			steps: []v1alpha1.WorkflowStep{top(step("notify", reads("nope")))},
			want:  deps{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in := make([]v1alpha1.WorkflowStep, len(tc.steps))
			for i := range tc.steps {
				in[i] = *tc.steps[i].DeepCopy()
			}
			ev, err := NewEvaluator(types.ExpressionOptions{}, in, "")
			require.NoError(t, err)
			out, _ := ev.WithReadDependencies(in, tc.mode)
			require.Equal(t, tc.steps, in, "the steps passed in are not changed")

			got := deps{}
			for _, s := range out {
				if len(s.DependsOn) > 0 {
					got[s.Name] = s.DependsOn
				}
				for _, sub := range s.SubSteps {
					if len(sub.DependsOn) > 0 {
						got[sub.Name] = sub.DependsOn
					}
				}
			}
			require.Equal(t, tc.want, got)
		})
	}
}

func TestReadDependenciesReportsWhichEdgesAreInferred(t *testing.T) {
	steps := []v1alpha1.WorkflowStep{
		{WorkflowStepBase: v1alpha1.WorkflowStepBase{Name: "build", Type: "echo",
			Outputs: v1alpha1.StepOutputs{{Name: "image", ValueFrom: "value"}}}},
		{WorkflowStepBase: v1alpha1.WorkflowStepBase{Name: "cfg", Type: "echo"}},
		{WorkflowStepBase: v1alpha1.WorkflowStepBase{Name: "notify", Type: "echo", DependsOn: []string{"cfg"},
			Properties: &runtime.RawExtension{Raw: []byte(`{"x":"$(steps.build.outputs.image)"}`)}}},
	}
	ev, err := NewEvaluator(types.ExpressionOptions{}, steps, "")
	require.NoError(t, err)
	out, inferred := ev.WithReadDependencies(steps, nil)
	require.Equal(t, map[string][]string{"notify": {"build"}}, inferred, "not cfg, which was written")
	require.Equal(t, []string{"cfg"}, out[2].DependsOn, "in order, build is first anyway: no edge")
}

// Admission refuses a read that could never be answered. Where admission did not
// run, the step fails on it rather than waiting forever.
func TestAReadThatCouldNeverBeAnsweredFailsWhenItsStepRuns(t *testing.T) {
	read := `{"x":"$(steps.build.outputs.image)"}`
	build := v1alpha1.WorkflowStep{WorkflowStepBase: v1alpha1.WorkflowStepBase{Name: "build", Type: "echo",
		Outputs: v1alpha1.StepOutputs{{Name: "image", ValueFrom: "value"}}}}
	notify := v1alpha1.WorkflowStep{WorkflowStepBase: v1alpha1.WorkflowStepBase{Name: "notify", Type: "echo",
		Properties: &runtime.RawExtension{Raw: []byte(read)}}}
	cyclic := *build.DeepCopy()
	cyclic.DependsOn = []string{"notify"}
	dag := &v1alpha1.WorkflowExecuteMode{Steps: wfv1alpha1.WorkflowModeDAG}

	for name, tc := range map[string]struct {
		steps []v1alpha1.WorkflowStep
		mode  *v1alpha1.WorkflowExecuteMode
		want  string
	}{
		"a later step in order": {steps: []v1alpha1.WorkflowStep{notify, build}, want: "build runs after notify"},
		"a cycle":               {steps: []v1alpha1.WorkflowStep{cyclic, notify}, mode: dag, want: "closes a cycle"},
	} {
		t.Run(name, func(t *testing.T) {
			ev, err := NewEvaluator(types.ExpressionOptions{}, tc.steps, "")
			require.NoError(t, err)
			ev.WithReadDependencies(tc.steps, tc.mode)
			_, err = ev.Properties(context.Background(), types.ExpressionStep{Name: "notify", Type: "echo"},
				&runtime.RawExtension{Raw: []byte(read)}, wfVars{vars: vars{}}, running("notify"), nil)
			require.Error(t, err)
			require.False(t, errors.Is(err, cel.ErrNotReady), "it would wait forever")
			require.ErrorContains(t, err, tc.want)
		})
	}
}
