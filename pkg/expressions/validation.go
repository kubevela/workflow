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
	"fmt"
	"sort"
	"strings"

	"github.com/kubevela/pkg/apis/oam/v1alpha1"
	"github.com/kubevela/pkg/cel"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/validation/field"
)

// Validate checks every expression in a workflow's steps without evaluating
// any: that it compiles, reads only roots offered where it sits, and reads only
// step outputs that exist and that the step can wait for. host is the roots the
// host answers, as given to ExpressionOptions.
func Validate(base *field.Path, steps []v1alpha1.WorkflowStep, mode *v1alpha1.WorkflowExecuteMode,
	host ...cel.Root) field.ErrorList {
	engine, err := NewEngine(host...)
	if err != nil {
		return field.ErrorList{field.InternalError(base, err)}
	}
	ev := &Evaluator{engine: engine, steps: newWorkflowSteps(steps)}
	declarers := map[string][]string{}
	for _, s := range steps {
		for _, o := range s.Outputs {
			declarers[o.Name] = append(declarers[o.Name], s.Name)
		}
		for _, sub := range s.SubSteps {
			for _, o := range sub.Outputs {
				declarers[o.Name] = append(declarers[o.Name], sub.Name)
			}
		}
	}

	var errs field.ErrorList
	pathOf := func(r stepRef) *field.Path {
		if r.Sub == topLevel {
			return base.Index(r.Top)
		}
		return base.Index(r.Top).Child("subSteps").Index(r.Sub)
	}
	for i, step := range steps {
		loopBody := step.ForEach != nil && len(step.SubSteps) == 0
		errs = append(errs, ev.check(pathOf(stepRef{i, topLevel}).Child("properties"), step.Properties, loopBody, nil, declarers)...)
		// A step's own if is decided once, before any pass of its loop.
		errs = append(errs, ev.checkCondition(pathOf(stepRef{i, topLevel}).Child(fieldIf), step.If, false, nil, declarers)...)
		if step.ForEach != nil && step.ForEach.Items != nil {
			errs = append(errs, ev.check(pathOf(stepRef{i, topLevel}).Child("forEach", "items"),
				&runtime.RawExtension{Raw: step.ForEach.Items.Raw}, false, nil, declarers)...)
		}
		// In a loop's iteration a body step's outputs are that iteration's.
		var siblings map[string]bool
		if step.ForEach != nil {
			siblings = map[string]bool{}
			for _, sub := range step.SubSteps {
				siblings[sub.Name] = true
			}
		}
		for j, sub := range step.SubSteps {
			errs = append(errs, ev.check(pathOf(stepRef{i, j}).Child("properties"), sub.Properties, step.ForEach != nil,
				siblings, declarers)...)
			errs = append(errs, ev.checkCondition(pathOf(stepRef{i, j}).Child(fieldIf), sub.If, step.ForEach != nil,
				siblings, declarers)...)
		}
	}
	ev.readEdges(steps, mode, func(reader stepRef, in, _, reason string) {
		path := pathOf(reader).Child(fieldProperties)
		switch in {
		case fieldItems:
			path = pathOf(reader).Child("forEach", "items")
		case fieldIf:
			path = pathOf(reader).Child(fieldIf)
		}
		errs = append(errs, field.Invalid(path, nil, reason))
	})
	return errs
}

// checkCondition validates a $( ) if as check does a property; a CUE if is not
// read.
func (e *Evaluator) checkCondition(path *field.Path, cond string, loopBody bool,
	siblings map[string]bool, declarers map[string][]string) field.ErrorList {
	if !IsCondition(cond) {
		return nil
	}
	raw, err := conditionTree(cond)
	if err != nil {
		return field.ErrorList{field.Invalid(path, cond, err.Error())}
	}
	errs := e.check(path, &runtime.RawExtension{Raw: raw}, loopBody, siblings, declarers)
	for _, err := range errs {
		err.BadValue = cond
	}
	return errs
}

// check validates the expressions in one tree. each is offered only in a forEach
// body; a root neither the workflow nor its host offers does not compile. A read
// of one of siblings, the reader's fellow body steps, is scoped to the iteration.
func (e *Evaluator) check(path *field.Path, properties *runtime.RawExtension, loopBody bool,
	siblings map[string]bool, declarers map[string][]string) field.ErrorList {
	if properties == nil || !MayHoldExpression(properties.Raw) {
		return nil
	}
	plan, err := e.engine.PlanJSON(properties.Raw)
	if err != nil {
		return field.ErrorList{field.Invalid(path, string(properties.Raw), err.Error())}
	}
	var errs field.ErrorList
	if !loopBody {
		for _, r := range plan.Reads(RootEach) {
			errs = append(errs, field.Invalid(path, r.String(), "each is only set inside a forEach body"))
		}
	}
	for _, r := range plan.Reads(RootSteps) {
		if msg := e.checkStepRead(r, siblings, declarers); msg != "" {
			errs = append(errs, field.Invalid(path, r.String(), msg))
		}
	}
	return errs
}

func (e *Evaluator) checkStepRead(r cel.Read, siblings map[string]bool, declarers map[string][]string) string {
	if len(r.Path) >= 2 && isStatusField(r.Path[1]) {
		if _, ok := e.steps.declared[r.Path[0]]; !ok {
			return fmt.Sprintf("no step %q", r.Path[0])
		}
		if len(r.Path) > 2 {
			return fmt.Sprintf("steps.%s.%s is a value; read nothing below it", r.Path[0], r.Path[1])
		}
		return ""
	}
	if len(r.Path) < 3 || r.Path[1] != "outputs" {
		return readHint(r).Error()
	}
	step, output := r.Path[0], r.Path[2]
	outputs, ok := e.steps.declared[step]
	if !ok {
		return fmt.Sprintf("no step %q", step)
	}
	if !declaresOutput(outputs, output) {
		return fmt.Sprintf("step %s declares no output %q", step, output)
	}
	by := declarers[output]
	if siblings[step] {
		// A body output is scoped to its iteration, so only fellow body steps share it.
		var inLoop []string
		for _, d := range by {
			if siblings[d] {
				inLoop = append(inLoop, d)
			}
		}
		by = inLoop
	}
	if len(by) > 1 {
		sorted := append([]string(nil), by...)
		sort.Strings(sorted)
		return fmt.Sprintf("output %q is declared by more than one step (%s), so which one this reads is ambiguous",
			output, strings.Join(sorted, ", "))
	}
	return ""
}
