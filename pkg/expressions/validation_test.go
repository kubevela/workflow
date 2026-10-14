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

	"github.com/kubevela/pkg/apis/oam/v1alpha1"
	"github.com/kubevela/pkg/cel"
	"github.com/stretchr/testify/require"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/validation/field"

	wfv1alpha1 "github.com/kubevela/workflow/api/v1alpha1"
	"github.com/kubevela/workflow/pkg/types"
)

func TestValidate(t *testing.T) {
	props := func(s string) *runtime.RawExtension { return &runtime.RawExtension{Raw: []byte(s)} }
	echo := func(name, p string, outputs ...string) v1alpha1.WorkflowStepBase {
		s := v1alpha1.WorkflowStepBase{Name: name, Type: "echo"}
		if p != "" {
			s.Properties = props(p)
		}
		for _, o := range outputs {
			s.Outputs = append(s.Outputs, v1alpha1.OutputItem{Name: o, ValueFrom: "value"})
		}
		return s
	}
	top := func(s v1alpha1.WorkflowStepBase) v1alpha1.WorkflowStep {
		return v1alpha1.WorkflowStep{WorkflowStepBase: s}
	}
	loop := func(name, items string, subs ...v1alpha1.WorkflowStepBase) v1alpha1.WorkflowStep {
		return v1alpha1.WorkflowStep{
			WorkflowStepBase: v1alpha1.WorkflowStepBase{Name: name, Type: types.WorkflowStepTypeStepGroup},
			ForEach:          &v1alpha1.ForEach{Items: &apiextensionsv1.JSON{Raw: []byte(items)}},
			SubSteps:         subs,
		}
	}
	build := top(echo("build", "", "image"))
	dag := &v1alpha1.WorkflowExecuteMode{Steps: wfv1alpha1.WorkflowModeDAG}

	for _, tc := range []struct {
		name  string
		host  []cel.Root
		mode  *v1alpha1.WorkflowExecuteMode
		steps []v1alpha1.WorkflowStep
		want  []string // each a substring of one error, in order; none for valid
	}{
		{name: "valid reads",
			steps: []v1alpha1.WorkflowStep{build,
				top(echo("notify", `{"m":"$(steps.build.outputs.image) $(context.name)"}`)),
				loop("per-tenant", `"$(steps.build.outputs.image.split(\",\"))"`,
					echo("fetch", `{"m":"$(each.item)"}`, "data"),
					echo("greet", `{"m":"$(steps.fetch.outputs.data) #$(each.index)"}`)),
			}},
		{name: "does not compile",
			steps: []v1alpha1.WorkflowStep{top(echo("notify", `{"m":"$(steps.build.outputs.image +)"}`))},
			want:  []string{`spec.workflowSpec.steps[0].properties`}},
		{name: "a root neither the workflow nor its host offers is unknown",
			steps: []v1alpha1.WorkflowStep{top(echo("notify", `{"a":"$(source.cfg.data)"}`))},
			want:  []string{`steps[0].properties`, `undeclared reference to 'source'`}},
		{name: "each outside a forEach body",
			steps: []v1alpha1.WorkflowStep{top(echo("notify", `{"m":"$(each.item)"}`)),
				loop("l", `"$(each.item)"`, echo("x", ""))},
			want: []string{`steps[0].properties`, `each is only set inside a forEach body`,
				`steps[1].forEach.items`, `each is only set inside a forEach body`}},
		{name: "unknown step and output",
			steps: []v1alpha1.WorkflowStep{build, top(echo("notify", `{"a":"$(steps.nope.outputs.x)","b":"$(steps.build.outputs.tag)"}`))},
			want:  []string{`no step "nope"`, `step build declares no output "tag"`}},
		{name: "not an output read",
			steps: []v1alpha1.WorkflowStep{build, top(echo("notify", `{"a":"$(steps.build.phase)"}`))},
			want:  []string{`read steps.build.outputs.<name>`}},
		{name: "an output two steps declare",
			steps: []v1alpha1.WorkflowStep{build, top(echo("rebuild", "", "image")),
				top(echo("notify", `{"a":"$(steps.build.outputs.image)"}`))},
			want: []string{`output "image" is declared by more than one step (build, rebuild)`}},
		{name: "a body sibling's output is this iteration's, whoever else declares the name",
			steps: []v1alpha1.WorkflowStep{top(echo("other", "", "data")),
				loop("per-tenant", `[1]`, echo("fetch", "", "data"), echo("greet", `{"m":"$(steps.fetch.outputs.data)"}`))}},
		{name: "two body siblings declaring one output share the iteration's variable",
			steps: []v1alpha1.WorkflowStep{loop("per-tenant", `[1]`, echo("fetch", "", "data"), echo("refetch", "", "data"),
				echo("greet", `{"m":"$(steps.fetch.outputs.data)"}`))},
			want: []string{`output "data" is declared by more than one step (fetch, refetch)`}},
		{name: "a step reading itself",
			steps: []v1alpha1.WorkflowStep{top(echo("notify", `{"a":"$(steps.notify.outputs.x)"}`, "x"))},
			want:  []string{`cannot read its own outputs`}},
		{name: "a later step in a workflow run in order",
			steps: []v1alpha1.WorkflowStep{top(echo("notify", `{"a":"$(steps.build.outputs.image)"}`)), build},
			want:  []string{`build runs after notify`}},
		{name: "an unanswerable read in forEach.items is reported there",
			steps: []v1alpha1.WorkflowStep{loop("per-tenant", `"$(steps.build.outputs.image)"`, echo("x", "")), build},
			want:  []string{`spec.workflowSpec.steps[0].forEach.items`, `build runs after per-tenant`}},
		{name: "a later step is fine in a DAG",
			mode:  dag,
			steps: []v1alpha1.WorkflowStep{top(echo("notify", `{"a":"$(steps.build.outputs.image)"}`)), build}},
		{name: "roots a host offers are allowed",
			host: []cel.Root{{Name: "source"}, {Name: "component",
				Qualifiers: []cel.Qualifier{{Name: "cluster"}}, QualifiedAt: 1}},
			steps: []v1alpha1.WorkflowStep{top(echo("notify",
				`{"a":"$(source.cfg.data)","b":"$(component.db.cluster(\"east\").output)"}`))}},
		{name: "a read whose lifted edge would wait on the reader's own group",
			mode: dag,
			steps: []v1alpha1.WorkflowStep{
				{WorkflowStepBase: v1alpha1.WorkflowStepBase{Name: "g", Type: types.WorkflowStepTypeStepGroup},
					SubSteps: []v1alpha1.WorkflowStepBase{echo("setup", ""), echo("use", `{"a":"$(steps.t.outputs.o)"}`)}},
				{WorkflowStepBase: func() v1alpha1.WorkflowStepBase {
					s := echo("t", "", "o")
					s.DependsOn = []string{"setup"}
					return s
				}()},
			},
			want: []string{`steps[0].subSteps[1].properties`, `reading t closes a cycle`}},
		{name: "a read closing a cycle",
			mode: dag,
			steps: []v1alpha1.WorkflowStep{
				{WorkflowStepBase: func() v1alpha1.WorkflowStepBase {
					s := echo("build", "", "image")
					s.DependsOn = []string{"notify"}
					return s
				}()},
				top(echo("notify", `{"a":"$(steps.build.outputs.image)"}`))},
			want: []string{`reading build closes a cycle`}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			errs := Validate(field.NewPath("spec", "workflowSpec", "steps"), tc.steps, tc.mode, tc.host...)
			if len(tc.want) == 0 {
				require.Empty(t, errs)
				return
			}
			all := errs.ToAggregate().Error()
			at := 0
			for _, w := range tc.want {
				i := strings.Index(all[at:], w)
				require.GreaterOrEqual(t, i, 0, "%q, after %d, in %s", w, at, all)
				at += i + len(w)
			}
		})
	}
}
