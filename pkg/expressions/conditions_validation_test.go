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
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/util/validation/field"

	v1alpha1 "github.com/kubevela/pkg/apis/oam/v1alpha1"
	"github.com/kubevela/pkg/cel"
	wfv1alpha1 "github.com/kubevela/workflow/api/v1alpha1"
	"github.com/kubevela/workflow/pkg/types"
)

func condStep(name, cond string, dependsOn ...string) v1alpha1.WorkflowStep {
	return v1alpha1.WorkflowStep{WorkflowStepBase: v1alpha1.WorkflowStepBase{
		Name: name, Type: "notify", If: cond, DependsOn: dependsOn,
		Outputs: v1alpha1.StepOutputs{{Name: name + "-out", ValueFrom: "value"}}}}
}

func TestValidateChecksConditions(t *testing.T) {
	dag := &v1alpha1.WorkflowExecuteMode{Steps: wfv1alpha1.WorkflowModeDAG, SubSteps: wfv1alpha1.WorkflowModeDAG}
	loopGroup := func(cond string) v1alpha1.WorkflowStep {
		return v1alpha1.WorkflowStep{
			WorkflowStepBase: v1alpha1.WorkflowStepBase{Name: "per-region", Type: types.WorkflowStepTypeStepGroup},
			ForEach:          &v1alpha1.ForEach{Items: &apiextensionsv1.JSON{Raw: []byte(`["eu","us"]`)}},
			SubSteps:         []v1alpha1.WorkflowStepBase{{Name: "deploy", Type: "notify", If: cond}},
		}
	}
	loopStep := func(cond string) v1alpha1.WorkflowStep {
		s := condStep("per-region", cond)
		s.ForEach = &v1alpha1.ForEach{Items: &apiextensionsv1.JSON{Raw: []byte(`["eu","us"]`)}}
		return s
	}
	for _, tc := range []struct {
		name  string
		steps []v1alpha1.WorkflowStep
		mode  *v1alpha1.WorkflowExecuteMode
		want  []string // "<field>: <fragment>", or none
	}{
		{name: "a condition reading an earlier step's status and output",
			steps: []v1alpha1.WorkflowStep{condStep("build", ""), condStep("notify", "$(steps.build.failed || steps.build.outputs[\"build-out\"] == \"x\")")}},
		{name: "a CUE if is not read",
			steps: []v1alpha1.WorkflowStep{condStep("notify", "status.nope.succeeded && inputs.x == \"$(y)\"")}},
		{name: "a step that does not exist",
			steps: []v1alpha1.WorkflowStep{condStep("notify", "$(steps.nope.succeeded)")},
			want:  []string{`steps[0].if: no step "nope"`}},
		{name: "a field steps do not have",
			steps: []v1alpha1.WorkflowStep{condStep("build", ""), condStep("notify", "$(steps.build.colour)")},
			want:  []string{"steps[1].if: read steps.build.outputs.<name>, or steps.build.<phase|"}},
		{name: "a condition that does not compile",
			steps: []v1alpha1.WorkflowStep{condStep("notify", "$(steps.build.)")},
			want:  []string{"steps[0].if: Syntax error"}},
		{name: "a later step in a workflow run in order",
			steps: []v1alpha1.WorkflowStep{condStep("notify", "$(steps.build.succeeded)"), condStep("build", "")},
			want:  []string{"steps[0].if: build runs after notify, and a workflow run in order cannot wait for it"}},
		{name: "a step's own status",
			steps: []v1alpha1.WorkflowStep{condStep("notify", "$(steps.notify.succeeded)")},
			want:  []string{"steps[0].if: step notify cannot read its own outputs"}},
		{name: "a cycle through dependsOn",
			steps: []v1alpha1.WorkflowStep{condStep("build", "", "notify"), condStep("notify", "$(steps.build.succeeded)")},
			mode:  dag,
			want:  []string{"steps[1].if: reading build closes a cycle through dependsOn"}},
		{name: "each in a loop step's own if, which is decided once for the loop",
			steps: []v1alpha1.WorkflowStep{loopStep("$(each.index == 0)")},
			want:  []string{"steps[0].if: each is only set inside a forEach body"}},
		{name: "each in a sub-step of a looping group, decided each pass",
			steps: []v1alpha1.WorkflowStep{loopGroup("$(each.item == \"eu\")")}},
		{name: "each outside any loop",
			steps: []v1alpha1.WorkflowStep{condStep("notify", "$(each.item == \"eu\")")},
			want:  []string{"steps[0].if: each is only set inside a forEach body"}},
		{name: "a root the workflow does not offer",
			steps: []v1alpha1.WorkflowStep{condStep("notify", "$(source.cfg.enabled)")},
			want:  []string{"steps[0].if: undeclared reference to 'source'"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			errs := Validate(field.NewPath("steps"), tc.steps, tc.mode)
			var got []string
			for _, e := range errs {
				got = append(got, e.Field+": "+e.Detail)
			}
			require.Len(t, got, len(tc.want), "errors: %v", got)
			for i, w := range tc.want {
				require.True(t, strings.HasPrefix(got[i], strings.SplitN(w, ": ", 2)[0]+":"), "field of %q in %v", w, got)
				require.Contains(t, got[i], strings.SplitN(w, ": ", 2)[1], "errors: %v", got)
			}
		})
	}

	t.Run("a host root is offered to a condition", func(t *testing.T) {
		require.Empty(t, Validate(field.NewPath("steps"),
			[]v1alpha1.WorkflowStep{condStep("notify", "$(source.cfg.enabled)")}, nil, sourceRoot))
	})
}

func TestAConditionsReadsAreDependencies(t *testing.T) {
	dag := &v1alpha1.WorkflowExecuteMode{Steps: wfv1alpha1.WorkflowModeDAG, SubSteps: wfv1alpha1.WorkflowModeDAG}
	steps := []v1alpha1.WorkflowStep{condStep("build", ""), condStep("notify", "$(steps.build.failed)")}

	t.Run("in a DAG, the condition waits for what it reads", func(t *testing.T) {
		ev, err := NewEvaluator(types.ExpressionOptions{}, steps, "")
		require.NoError(t, err)
		out, inferred := ev.WithReadDependencies(steps, dag)
		require.Equal(t, []string{"build"}, out[1].DependsOn)
		require.Equal(t, map[string][]string{"notify": {"build"}}, inferred)
	})
	t.Run("in order, it is recorded and the order answers it", func(t *testing.T) {
		ev, err := NewEvaluator(types.ExpressionOptions{}, steps, "")
		require.NoError(t, err)
		out, inferred := ev.WithReadDependencies(steps, nil)
		require.Empty(t, out[1].DependsOn)
		require.Equal(t, map[string][]string{"notify": {"build"}}, inferred)
	})
	t.Run("a CUE if implies nothing", func(t *testing.T) {
		cue := []v1alpha1.WorkflowStep{condStep("build", ""), condStep("notify", "status.build.failed")}
		out := StepsWithReadDependencies(cue, dag)
		require.Empty(t, out[1].DependsOn)
	})
}

// sourceRoot is a root a host answers.
var sourceRoot = cel.Root{Name: "source"}
