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

package builtin

import (
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/util/validation/field"

	"github.com/kubevela/workflow/api/v1alpha1"
	"github.com/kubevela/workflow/pkg/types"

	oamv1alpha1 "github.com/kubevela/pkg/apis/oam/v1alpha1"
)

func TestValidateForEachSteps(t *testing.T) {
	loop := func(forEach oamv1alpha1.ForEach) oamv1alpha1.WorkflowStep {
		return oamv1alpha1.WorkflowStep{
			WorkflowStepBase: oamv1alpha1.WorkflowStepBase{Name: "scale", Type: "scale-component"},
			ForEach:          &forEach,
		}
	}
	plain := func(name string) oamv1alpha1.WorkflowStep {
		return oamv1alpha1.WorkflowStep{WorkflowStepBase: oamv1alpha1.WorkflowStepBase{Name: name, Type: "t"}}
	}
	group := oamv1alpha1.WorkflowStep{
		WorkflowStepBase: oamv1alpha1.WorkflowStepBase{Name: "rollout", Type: types.WorkflowStepTypeStepGroup},
		ForEach:          &oamv1alpha1.ForEach{Items: itemsJSON(t, []string{"a"})},
		SubSteps:         []oamv1alpha1.WorkflowStepBase{{Name: "deploy", Type: "deploy"}},
	}

	cases := map[string]struct {
		steps      []oamv1alpha1.WorkflowStep
		expression bool
		want       []string
	}{
		"literal items":   {steps: []oamv1alpha1.WorkflowStep{loop(oamv1alpha1.ForEach{Items: itemsJSON(t, []any{"a", 1})})}},
		"from":            {steps: []oamv1alpha1.WorkflowStep{loop(oamv1alpha1.ForEach{From: "regions.names"})}},
		"both modes":      {steps: []oamv1alpha1.WorkflowStep{loop(oamv1alpha1.ForEach{From: "r", Mode: v1alpha1.WorkflowModeDAG}), group}},
		"no forEach":      {steps: []oamv1alpha1.WorkflowStep{plain("a")}},
		"neither":         {steps: []oamv1alpha1.WorkflowStep{loop(oamv1alpha1.ForEach{})}, want: []string{"steps[0].forEach: Invalid value: \"scale\": needs exactly one of items and from"}},
		"both":            {steps: []oamv1alpha1.WorkflowStep{loop(oamv1alpha1.ForEach{From: "r", Items: itemsJSON(t, []int{1})})}, want: []string{"needs exactly one of items and from"}},
		"lowercase mode":  {steps: []oamv1alpha1.WorkflowStep{loop(oamv1alpha1.ForEach{From: "r", Mode: "dag"})}, want: []string{"steps[0].forEach.mode: Unsupported value: \"dag\""}},
		"items an object": {steps: []oamv1alpha1.WorkflowStep{loop(oamv1alpha1.ForEach{Items: itemsJSON(t, map[string]int{"a": 1})})}, want: []string{"steps[0].forEach.items", "must be a list"}},
		"items expression, not allowed": {
			steps: []oamv1alpha1.WorkflowStep{loop(oamv1alpha1.ForEach{Items: itemsJSON(t, "$(source.inv.clusters)")})},
			want:  []string{"must be a list", "EnableCelExpressions"},
		},
		"items expression, allowed": {
			steps:      []oamv1alpha1.WorkflowStep{loop(oamv1alpha1.ForEach{Items: itemsJSON(t, "$(source.inv.clusters)")})},
			expression: true,
		},
		"a plain string is never allowed": {
			steps:      []oamv1alpha1.WorkflowStep{loop(oamv1alpha1.ForEach{Items: itemsJSON(t, "east")})},
			expression: true,
			want:       []string{"must be a list"},
		},
		"over the limit": {
			steps: []oamv1alpha1.WorkflowStep{loop(oamv1alpha1.ForEach{Items: itemsJSON(t, make([]int, types.MaxForEachItems+1))})},
			want:  []string{"steps[0].forEach.items", "over the limit of 50"},
		},
		"output named loop": {
			steps: []oamv1alpha1.WorkflowStep{func() oamv1alpha1.WorkflowStep {
				s := loop(oamv1alpha1.ForEach{From: "r"})
				s.Outputs = oamv1alpha1.StepOutputs{{Name: "loop", ValueFrom: "v"}}
				return s
			}()},
			want: []string{"steps[0].outputs[0].name", "reserved inside a forEach"},
		},
		"sub-step output named loop": {
			steps: []oamv1alpha1.WorkflowStep{func() oamv1alpha1.WorkflowStep {
				g := *group.DeepCopy()
				g.SubSteps[0].Outputs = oamv1alpha1.StepOutputs{{Name: "loop", ValueFrom: "v"}}
				return g
			}()},
			want: []string{"steps[0].subSteps[0].outputs[0].name", "reserved inside a forEach"},
		},
		"step named like a single step's pass": {
			steps: []oamv1alpha1.WorkflowStep{loop(oamv1alpha1.ForEach{From: "r"}), plain("scale-3")},
			want:  []string{"steps[1].name", "scale-3", "clashes with the names forEach step scale generates"},
		},
		"sub-step named like a group's pass": {
			steps: []oamv1alpha1.WorkflowStep{group, {
				WorkflowStepBase: oamv1alpha1.WorkflowStepBase{Name: "other", Type: types.WorkflowStepTypeStepGroup},
				SubSteps:         []oamv1alpha1.WorkflowStepBase{{Name: "rollout-0-deploy", Type: "t"}},
			}},
			want: []string{"steps[1].subSteps[0].name", "clashes with the names forEach step rollout generates"},
		},
		"items null": {
			steps: []oamv1alpha1.WorkflowStep{loop(oamv1alpha1.ForEach{Items: itemsJSON(t, nil)})},
			want:  []string{"steps[0].forEach.items", "must be a list"},
		},
		"outputs on a looped group": {
			steps: []oamv1alpha1.WorkflowStep{func() oamv1alpha1.WorkflowStep {
				g := *group.DeepCopy()
				g.Outputs = oamv1alpha1.StepOutputs{{Name: "result", ValueFrom: "v"}}
				return g
			}()},
			want: []string{"steps[0].outputs", "declare them on its sub-steps"},
		},
		"an output named like a pass's variable": {
			steps: []oamv1alpha1.WorkflowStep{loop(oamv1alpha1.ForEach{From: "r"}), func() oamv1alpha1.WorkflowStep {
				s := plain("other")
				s.Outputs = oamv1alpha1.StepOutputs{{Name: "scale-0-endpoint", ValueFrom: "v"}}
				return s
			}()},
			want: []string{"steps[1].outputs[0].name", "clashes with the variables forEach step scale keeps"},
		},
		"two groups whose passes would share names": {
			steps: []oamv1alpha1.WorkflowStep{
				{
					WorkflowStepBase: oamv1alpha1.WorkflowStepBase{Name: "a", Type: types.WorkflowStepTypeStepGroup},
					ForEach:          &oamv1alpha1.ForEach{From: "r"},
					SubSteps:         []oamv1alpha1.WorkflowStepBase{{Name: "1-b", Type: "t"}},
				},
				{
					WorkflowStepBase: oamv1alpha1.WorkflowStepBase{Name: "a-0", Type: types.WorkflowStepTypeStepGroup},
					ForEach:          &oamv1alpha1.ForEach{From: "r"},
					SubSteps:         []oamv1alpha1.WorkflowStepBase{{Name: "b", Type: "t"}},
				},
			},
			want: []string{"steps[1].name", "a-0", "is named like the passes of forEach step a"},
		},
		"a similar name that cannot clash": {
			steps: []oamv1alpha1.WorkflowStep{loop(oamv1alpha1.ForEach{From: "r"}), plain("scale-x"), plain("scale-01")},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			r := require.New(t)
			errs := ValidateForEachSteps(field.NewPath("steps"), tc.steps, tc.expression)
			if len(tc.want) == 0 {
				r.Empty(errs)
				return
			}
			r.Len(errs, 1, errs.ToAggregate())
			for _, want := range tc.want {
				r.Contains(errs[0].Error(), want)
			}
		})
	}
}
