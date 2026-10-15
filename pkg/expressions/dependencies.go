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
	"slices"

	"github.com/kubevela/pkg/apis/oam/v1alpha1"
	"github.com/kubevela/pkg/cel"
	"k8s.io/apimachinery/pkg/runtime"

	wfv1alpha1 "github.com/kubevela/workflow/api/v1alpha1"
	"github.com/kubevela/workflow/pkg/types"
)

// A read of steps.X is an implied dependsOn X, added where it cannot leave the
// reader waiting on something that never comes. A read that could never be
// answered gets no edge: admission refuses it, and a run fails it.

// stepRef is a step, or a sub-step of the step at Top.
type stepRef struct{ Top, Sub int }

const topLevel = -1

// The fields of a step an expression may sit in.
const (
	fieldProperties = "properties"
	fieldItems      = "forEach.items"
)

// WithReadDependencies returns a copy of steps in which each step dependsOn the
// steps its expressions read, and, by step name, the steps each one reads: the
// edges it added, and the earlier steps a workflow run in order puts first
// without one. A read of a forEach body step from outside the loop is a read of
// the loop.
//
// No edge is added for a step the order already puts first, the reader itself or
// its own loop, a later step in a workflow run in order, or a read that would
// close a cycle.
func (e *Evaluator) WithReadDependencies(steps []v1alpha1.WorkflowStep,
	mode *v1alpha1.WorkflowExecuteMode) ([]v1alpha1.WorkflowStep, map[string][]string) {
	e.refused = map[string]map[string]string{}
	return e.readEdges(steps, mode, func(reader stepRef, _, producer, reason string) {
		name := readerName(steps, reader)
		if e.refused[name] == nil {
			e.refused[name] = map[string]string{}
		}
		e.refused[name][producer] = reason
	})
}

// StepsWithReadDependencies is steps with the dependsOn their expressions' reads
// imply, as a run orders them, for code that works on a workflow's steps outside
// a run, such as restarting one from a step. host is the roots the host offers.
func StepsWithReadDependencies(steps []v1alpha1.WorkflowStep, mode *v1alpha1.WorkflowExecuteMode,
	host ...cel.Root) []v1alpha1.WorkflowStep {
	ev, err := NewEvaluator(types.ExpressionOptions{Roots: host}, steps, "")
	if err != nil {
		return steps
	}
	out, _ := ev.WithReadDependencies(steps, mode)
	return out
}

// readerName is the name, as written, of the step at r.
func readerName(steps []v1alpha1.WorkflowStep, r stepRef) string {
	if r.Sub == topLevel {
		return steps[r.Top].Name
	}
	return steps[r.Top].SubSteps[r.Sub].Name
}

// readEdges is WithReadDependencies, telling unanswerable each read of a step
// that gets no edge and would wait on something that never comes, with the
// field the read sits in: properties, or forEach.items.
func (e *Evaluator) readEdges(steps []v1alpha1.WorkflowStep, mode *v1alpha1.WorkflowExecuteMode,
	unanswerable func(reader stepRef, field, producer, reason string)) ([]v1alpha1.WorkflowStep, map[string][]string) {
	out := make([]v1alpha1.WorkflowStep, len(steps))
	for i := range steps {
		out[i] = *steps[i].DeepCopy()
	}
	steps = out
	inferred := map[string][]string{}

	named := map[string]stepRef{}
	for i, step := range steps {
		named[step.Name] = stepRef{i, topLevel}
		for j, sub := range step.SubSteps {
			named[sub.Name] = stepRef{i, j}
		}
	}
	base := func(r stepRef) *v1alpha1.WorkflowStepBase {
		if r.Sub == topLevel {
			return &steps[r.Top].WorkflowStepBase
		}
		return &steps[r.Top].SubSteps[r.Sub]
	}
	stepsInOrder := mode == nil || mode.Steps != wfv1alpha1.WorkflowModeDAG

	for i, step := range steps {
		subStepsInOrder := step.Mode == wfv1alpha1.WorkflowModeStep ||
			(step.Mode == "" && mode != nil && mode.SubSteps == wfv1alpha1.WorkflowModeStep)
		type reader struct {
			at    stepRef
			field string
			reads []string
		}
		readers := []reader{{stepRef{i, topLevel}, fieldProperties, e.stepsRead(step.Properties)}}
		if step.ForEach != nil && step.ForEach.Items != nil {
			readers = append(readers, reader{stepRef{i, topLevel}, fieldItems,
				e.stepsRead(&runtime.RawExtension{Raw: step.ForEach.Items.Raw})})
		}
		for j, sub := range step.SubSteps {
			readers = append(readers, reader{stepRef{i, j}, fieldProperties, e.stepsRead(sub.Properties)})
		}

		for _, r := range readers {
			from := r.at
			for _, producer := range r.reads {
				to, ok := e.producerOf(producer, from, steps, named)
				if !ok {
					continue
				}
				// A step reading itself, its own group, or its own sub-steps.
				if to.Top == from.Top && (to.Sub == topLevel || from.Sub == topLevel || to == from) {
					unanswerable(from, r.field, producer, fmt.Sprintf("step %s cannot read its own outputs", base(to).Name))
					continue
				}
				f, t := from, to
				inOrder := subStepsInOrder
				if f.Top != t.Top {
					f, t = stepRef{f.Top, topLevel}, stepRef{t.Top, topLevel}
					inOrder = stepsInOrder
				}
				if inOrder && (t.Top > f.Top || t.Top == f.Top && t.Sub > f.Sub) {
					unanswerable(from, r.field, producer, fmt.Sprintf("%s runs after %s, and a workflow run in order cannot wait for it",
						base(t).Name, base(f).Name))
					continue
				}
				reader, name := base(f), base(t).Name
				// In order, the earlier step finishes first anyway. A dependsOn would
				// only change when the reader is skipped: with one, a failure of any
				// other earlier step no longer skips it.
				if inOrder {
					if !recordInOrder(steps, reader, name, inferred) {
						unanswerable(from, r.field, producer, fmt.Sprintf("reading %s closes a cycle through dependsOn", name))
					}
					continue
				}
				switch dependOn(steps, reader, name) {
				case added:
					inferred[reader.Name] = append(inferred[reader.Name], name)
				case cycle:
					unanswerable(from, r.field, producer, fmt.Sprintf("reading %s closes a cycle through dependsOn", name))
				}
			}
		}
	}
	return steps, inferred
}

// recordInOrder records a read the order answers, as reader waiting on the step
// named name, unless that step itself waits for the reader through dependsOn,
// which no order can answer. It reports whether the read can be answered.
func recordInOrder(steps []v1alpha1.WorkflowStep, reader *v1alpha1.WorkflowStepBase, name string,
	inferred map[string][]string) bool {
	if waitsFor(steps, name, reader.Name) {
		return false
	}
	if !slices.Contains(inferred[reader.Name], name) && !slices.Contains(reader.DependsOn, name) {
		inferred[reader.Name] = append(inferred[reader.Name], name)
	}
	return true
}

// producerOf is the step a read of steps.<producer> from the step at from waits
// on: the producer, or the loop it is a body step of when the reader is outside
// that loop.
func (e *Evaluator) producerOf(producer string, from stepRef, steps []v1alpha1.WorkflowStep,
	named map[string]stepRef) (stepRef, bool) {
	to, ok := named[producer]
	if !ok {
		return stepRef{}, false
	}
	if to.Sub != topLevel && steps[to.Top].ForEach != nil && to.Top != from.Top {
		return stepRef{to.Top, topLevel}, true
	}
	return to, true
}

// stepsRead is the steps an expression in properties reads.
func (e *Evaluator) stepsRead(properties *runtime.RawExtension) []string {
	if properties == nil || len(properties.Raw) == 0 {
		return nil
	}
	plan, err := e.engine.PlanJSON(properties.Raw)
	if err != nil {
		// A step whose expressions do not compile fails when it runs.
		return nil
	}
	var out []string
	for _, r := range plan.Reads(RootSteps) {
		if len(r.Path) > 0 {
			out = append(out, r.Path[0])
		}
	}
	return out
}

type edge int

const (
	added edge = iota
	present
	cycle
)

// dependOn adds name to step's dependsOn unless it is there already or would
// close a cycle, and says which.
func dependOn(steps []v1alpha1.WorkflowStep, step *v1alpha1.WorkflowStepBase, name string) edge {
	for _, d := range step.DependsOn {
		if d == name {
			return present
		}
	}
	if waitsFor(steps, name, step.Name) {
		return cycle
	}
	step.DependsOn = append(step.DependsOn, name)
	return added
}

// waitsFor reports whether the step named from cannot finish before the step
// named to has: through dependsOn, a group through its sub-steps, and a sub-step
// through what its group waits on to start. Reaching a sub-step of to counts as
// reaching to, since that sub-step cannot start before to does.
func waitsFor(steps []v1alpha1.WorkflowStep, from, to string) bool {
	edges := map[string][]string{}
	groupOf := map[string]string{}
	for _, step := range steps {
		edges[step.Name] = append(edges[step.Name], step.DependsOn...)
		for _, sub := range step.SubSteps {
			groupOf[sub.Name] = step.Name
			edges[step.Name] = append(edges[step.Name], sub.Name)
			edges[sub.Name] = append(edges[sub.Name], sub.DependsOn...)
			edges[sub.Name] = append(edges[sub.Name], step.DependsOn...)
		}
	}
	seen := map[string]bool{}
	var visit func(string) bool
	visit = func(n string) bool {
		if n == to || groupOf[n] == to {
			return true
		}
		if seen[n] {
			return false
		}
		seen[n] = true
		for _, next := range edges[n] {
			if visit(next) {
				return true
			}
		}
		return false
	}
	return visit(from)
}
