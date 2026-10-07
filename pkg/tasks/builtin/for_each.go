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
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"cuelang.org/go/cue"
	"github.com/kubevela/pkg/util/slices"
	"github.com/pkg/errors"

	monitorContext "github.com/kubevela/pkg/monitor/context"

	"github.com/kubevela/workflow/api/v1alpha1"
	wfContext "github.com/kubevela/workflow/pkg/context"
	"github.com/kubevela/workflow/pkg/cue/process"
	"github.com/kubevela/workflow/pkg/providers"
	"github.com/kubevela/workflow/pkg/tasks/custom"
	"github.com/kubevela/workflow/pkg/types"

	oamv1alpha1 "github.com/kubevela/pkg/apis/oam/v1alpha1"
)

// forEachItemsKey is where the resolved items are pinned in the workflow context, so
// every reconcile of a running loop iterates the same list.
const forEachItemsKey = "for-each-items"

// loopRoot is the name inputs[].from uses to read the current iteration.
const loopRoot = "loop"

// ForEach is the runner for a step with forEach: it runs the step, or a step-group's
// sub-steps, once per item.
func ForEach(step oamv1alpha1.WorkflowStep, opt *types.TaskGeneratorOptions) (types.TaskRunner, error) {
	mode := step.ForEach.Mode
	if mode == "" {
		mode = v1alpha1.WorkflowModeStep
	}
	return &forEachTaskRunner{
		id:       opt.ID,
		name:     step.Name,
		step:     step,
		mode:     mode,
		bodyMode: opt.SubStepExecuteMode,
		pCtx:     opt.ProcessContext,
		generate: opt.SubTaskGenerator,
	}, nil
}

type forEachTaskRunner struct {
	id       string
	name     string
	step     oamv1alpha1.WorkflowStep
	mode     oamv1alpha1.WorkflowMode
	bodyMode oamv1alpha1.WorkflowMode
	pCtx     process.Context
	generate func(step oamv1alpha1.WorkflowStepBase, id string) (types.TaskRunner, error)
}

// Name return the step name.
func (tr *forEachTaskRunner) Name() string {
	return tr.name
}

// Pending waits for the loop's dependsOn and for the variable forEach.from reads. A
// single step's inputs belong to its iterations, so only a group's are waited on here.
func (tr *forEachTaskRunner) Pending(ctx monitorContext.Context, wfCtx wfContext.Context, stepStatus map[string]v1alpha1.StepStatus) (bool, v1alpha1.StepStatus) {
	resetter := tr.FillContextData(ctx, tr.pCtx)
	defer resetter(tr.pCtx)
	basicVal, _ := custom.MakeBasicValue(ctx, providers.DefaultCompiler.Get(), nil, tr.pCtx)
	loop := oamv1alpha1.WorkflowStep{WorkflowStepBase: oamv1alpha1.WorkflowStepBase{
		Name:      tr.step.Name,
		Type:      tr.step.Type,
		DependsOn: tr.step.DependsOn,
	}}
	if _, group := body(tr.step); group {
		loop.Inputs = tr.step.Inputs
	}
	if pending, status := custom.CheckPending(wfCtx, loop, tr.id, stepStatus, basicVal); pending {
		return pending, status
	}
	if from := tr.step.ForEach.From; from != "" && wfCtx.GetMutableValue(PinnedItemsKey(tr.id)) == "" {
		if _, err := wfCtx.GetVar(strings.Split(from, ".")...); err != nil {
			return true, v1alpha1.StepStatus{
				ID:      tr.id,
				Name:    tr.name,
				Type:    tr.step.Type,
				Phase:   v1alpha1.WorkflowStepPhasePending,
				Message: fmt.Sprintf("Pending on forEach.from: %s", from),
			}
		}
	}
	return false, v1alpha1.StepStatus{}
}

// FillContextData fills the step's runtime metadata into the process context.
func (tr *forEachTaskRunner) FillContextData(ctx monitorContext.Context, processCtx process.Context) types.ContextDataResetter {
	metas := []process.StepMetaKV{
		process.WithName(tr.name),
		process.WithSessionID(tr.id),
		process.WithSpanID(ctx.GetID()),
		process.WithGroupName(tr.name),
	}
	manager := process.NewStepRunTimeMeta()
	manager.Fill(processCtx, metas)
	return func(processCtx process.Context) {
		manager.Remove(processCtx, slices.Map(metas,
			func(t process.StepMetaKV) string {
				return t.Key
			}),
		)
	}
}

// Run resolves the items, then runs one iteration per item.
func (tr *forEachTaskRunner) Run(ctx wfContext.Context, options *types.TaskRunOptions) (v1alpha1.StepStatus, *types.Operation, error) {
	status := v1alpha1.StepStatus{
		ID:   tr.id,
		Name: tr.name,
		Type: tr.step.Type,
	}
	if options.GetTracer == nil {
		options.GetTracer = func(string, oamv1alpha1.WorkflowStep) monitorContext.Context {
			return monitorContext.NewTraceContext(context.Background(), "")
		}
	}
	tracer := options.GetTracer(tr.id, tr.step).AddTag("step_name", tr.name, "step_type", tr.step.Type)
	resetter := tr.FillContextData(tracer, tr.pCtx)
	defer resetter(tr.pCtx)
	basicVal, err := custom.MakeBasicValue(tracer, providers.DefaultCompiler.Get(), nil, tr.pCtx)
	if err != nil {
		return status, nil, err
	}

	runPreChecks(tr.step, options, basicVal, &status)
	switch {
	case status.Phase == v1alpha1.WorkflowStepPhaseSkipped:
		return status, &types.Operation{Skip: true}, nil
	case status.Phase == v1alpha1.WorkflowStepPhaseFailed && status.Reason == types.StatusReasonTimeout:
		// Past its timeout, a loop starts no further pass.
		return status, &types.Operation{Terminated: true}, nil
	}

	items, err := tr.resolveItems(ctx)
	if err != nil {
		return failed(status, types.StatusReasonParameter, err), &types.Operation{Terminated: true}, nil
	}
	iterations, err := expandIterations(tr.step, items)
	if err != nil {
		return failed(status, types.StatusReasonParameter, err), &types.Operation{Terminated: true}, nil
	}
	if tr.generate == nil {
		return status, nil, errors.Errorf("forEach step %s has no sub-task generator", tr.name)
	}

	e := options.Engine
	ids := map[string]string{}
	for _, sub := range e.GetStepStatus(tr.name).SubStepsStatus {
		ids[sub.Name] = sub.ID
	}
	e.SetParentRunner(tr.name)
	defer e.SetParentRunner("")
	failedBefore := false
	for _, it := range iterations {
		// A finished pass is left alone: rebuilding its runners would load every
		// template again only for the engine to skip them.
		if finished, anyFailed := iterationState(e.GetStepStatus(tr.name), it); finished {
			failedBefore = failedBefore || anyFailed
			continue
		}
		runners, err := tr.iterationRunners(ctx, basicVal.Context(), e, it, ids, failedBefore)
		if err != nil {
			return status, nil, err
		}
		// An unset mode is DAG, as for a plain step-group.
		if err := e.Run(tracer, runners, tr.bodyMode != v1alpha1.WorkflowModeStep); err != nil {
			status.Phase = v1alpha1.WorkflowStepPhaseRunning
			return status, e.GetOperation(), err
		}
		if tr.mode == v1alpha1.WorkflowModeDAG {
			continue
		}
		finished, anyFailed := iterationState(e.GetStepStatus(tr.name), it)
		if !finished {
			break
		}
		failedBefore = failedBefore || anyFailed
	}

	// Every pass counts, run or not, so a loop that has not reached its last pass reads
	// as running unless it has timed out or a pass is suspended.
	status, operation := getStepGroupStatus(status, e.GetStepStatus(tr.name), e.GetOperation(), countSteps(iterations))
	if status.Phase != v1alpha1.WorkflowStepPhaseSucceeded {
		return status, operation, nil
	}
	if err := collectOutputs(ctx, basicVal.Context(), tr.step, len(items)); err != nil {
		return failed(status, types.StatusReasonOutput, err), &types.Operation{Terminated: true}, nil
	}
	return status, operation, nil
}

// iterationRunners builds the runners for one iteration and registers their dependsOn
// with the engine. Once an earlier iteration has failed in step mode, the runners
// record a skip instead of running, as the engine does for later steps in step mode.
func (tr *forEachTaskRunner) iterationRunners(ctx wfContext.Context, cueCtx *cue.Context, e types.Engine, it iteration, ids map[string]string, skip bool) ([]types.TaskRunner, error) {
	if _, err := ctx.GetVar(it.loopVar); err != nil {
		b, err := json.Marshal(map[string]any{"item": it.item, "index": it.index})
		if err != nil {
			return nil, err
		}
		if err := ctx.SetVar(cueCtx.CompileBytes(b), it.loopVar); err != nil {
			return nil, errors.WithMessagef(err, "set %s", it.loopVar)
		}
	}
	runners := make([]types.TaskRunner, 0, len(it.steps))
	for _, sub := range it.steps {
		e.SetDependsOn(sub.Name, sub.DependsOn)
		if skip {
			runners = append(runners, &skippedRunner{step: sub, id: ids[sub.Name]})
			continue
		}
		runner, err := tr.generate(sub, ids[sub.Name])
		if err != nil {
			return nil, errors.WithMessagef(err, "generate iteration %d of forEach step %s", it.index, tr.name)
		}
		runners = append(runners, &iterationRunner{TaskRunner: runner, pCtx: tr.pCtx, item: it.item, index: it.index})
	}
	return runners, nil
}

// iterationState reports whether every step of an iteration has finished, and whether
// any of them failed.
func iterationState(status v1alpha1.WorkflowStepStatus, it iteration) (finished, anyFailed bool) {
	phases := map[string]v1alpha1.StepStatus{}
	for _, sub := range status.SubStepsStatus {
		phases[sub.Name] = sub
	}
	for _, s := range it.steps {
		sub, ok := phases[s.Name]
		if !ok || !types.IsStepFinish(sub.Phase, sub.Reason) {
			return false, anyFailed
		}
		anyFailed = anyFailed || sub.Phase == v1alpha1.WorkflowStepPhaseFailed
	}
	return true, anyFailed
}

func countSteps(iterations []iteration) int {
	n := 0
	for _, it := range iterations {
		n += len(it.steps)
	}
	return n
}

// resolveItems returns the pinned items, or reads forEach.items or forEach.from and
// pins the result.
func (tr *forEachTaskRunner) resolveItems(ctx wfContext.Context) ([]any, error) {
	var items []any
	if pinned := ctx.GetMutableValue(PinnedItemsKey(tr.id)); pinned != "" {
		if err := json.Unmarshal([]byte(pinned), &items); err != nil {
			return nil, errors.WithMessage(err, "decode pinned items")
		}
		return items, nil
	}

	forEach := tr.step.ForEach
	var raw []byte
	switch {
	case (forEach.Items != nil) == (forEach.From != ""):
		return nil, errors.New("forEach needs exactly one of items and from")
	case forEach.Items != nil:
		raw = forEach.Items.Raw
		var expr string
		if json.Unmarshal(raw, &expr) == nil && strings.Contains(expr, "$(") {
			return nil, errors.Errorf("forEach.items %q was not resolved; $( ) expressions need KubeVela with EnableCelExpressions", expr)
		}
	default:
		v, err := ctx.GetVar(strings.Split(forEach.From, ".")...)
		if err != nil {
			return nil, errors.WithMessagef(err, "read forEach.from %s", forEach.From)
		}
		if raw, err = v.MarshalJSON(); err != nil {
			return nil, errors.WithMessagef(err, "evaluate forEach.from %s", forEach.From)
		}
	}
	if err := json.Unmarshal(raw, &items); err != nil || items == nil {
		return nil, errors.Errorf("forEach items must be a list, got %s", raw)
	}
	if len(items) > types.MaxForEachItems {
		return nil, errors.Errorf("forEach has %d items, over the limit of %d", len(items), types.MaxForEachItems)
	}
	b, err := json.Marshal(items)
	if err != nil {
		return nil, err
	}
	ctx.SetMutableValue(string(b), PinnedItemsKey(tr.id))
	return items, nil
}

func failed(status v1alpha1.StepStatus, reason string, err error) v1alpha1.StepStatus {
	status.Phase = v1alpha1.WorkflowStepPhaseFailed
	status.Reason = reason
	status.Message = err.Error()
	return status
}

// iteration is one pass of a forEach body, with its steps renamed for that pass.
type iteration struct {
	index int
	item  any
	// loopVar is the workflow variable holding {item, index} for this pass.
	loopVar string
	steps   []oamv1alpha1.WorkflowStepBase
}

// body is what a forEach repeats: a step-group's sub-steps, or the step itself. The
// step's if, timeout and dependsOn apply to the loop as a whole, not to each pass.
func body(step oamv1alpha1.WorkflowStep) (steps []oamv1alpha1.WorkflowStepBase, group bool) {
	if step.Type == types.WorkflowStepTypeStepGroup {
		return step.SubSteps, true
	}
	return []oamv1alpha1.WorkflowStepBase{{
		Name:       step.Name,
		Type:       step.Type,
		Meta:       step.Meta,
		Properties: step.Properties,
		Inputs:     step.Inputs,
		Outputs:    step.Outputs,
	}}, false
}

// expandIterations repeats the body once per item. Step names, dependsOn entries naming
// body steps, body outputs, and inputs reading a body output or the loop are all scoped
// to the iteration, so iterations never share names or vars. A group's sub-step is
// named <loop>-<index>-<sub>; a single step is named <loop>-<index>.
func expandIterations(step oamv1alpha1.WorkflowStep, items []any) ([]iteration, error) {
	template, group := body(step)
	bodySteps := map[string]bool{}
	bodyOutputs := map[string]bool{}
	for _, sub := range template {
		bodySteps[sub.Name] = true
		for _, o := range sub.Outputs {
			if o.Name == loopRoot {
				return nil, errors.Errorf("step %s: an output inside a forEach cannot be named %q", sub.Name, loopRoot)
			}
			bodyOutputs[o.Name] = true
		}
	}

	iterations := make([]iteration, 0, len(items))
	for i, item := range items {
		scope := func(name string) string { return iterationName(step.Name, i, name) }
		stepName := scope
		if !group {
			stepName = func(string) string { return fmt.Sprintf("%s-%d", step.Name, i) }
		}
		steps := make([]oamv1alpha1.WorkflowStepBase, 0, len(template))
		for _, sub := range template {
			s := *sub.DeepCopy()
			s.Name = stepName(sub.Name)
			for j, dep := range s.DependsOn {
				if bodySteps[dep] {
					s.DependsOn[j] = stepName(dep)
				}
			}
			for j := range s.Outputs {
				s.Outputs[j].Name = scope(s.Outputs[j].Name)
			}
			for j, in := range s.Inputs {
				head, rest, dotted := strings.Cut(in.From, ".")
				if head == loopRoot || bodyOutputs[head] {
					s.Inputs[j].From = scope(head)
					if dotted {
						s.Inputs[j].From += "." + rest
					}
				}
			}
			steps = append(steps, s)
		}
		iterations = append(iterations, iteration{index: i, item: item, loopVar: scope(loopRoot), steps: steps})
	}
	return iterations, nil
}

func iterationName(loop string, index int, name string) string {
	return fmt.Sprintf("%s-%d-%s", loop, index, name)
}

// IterationTemplate returns the body step that the iteration step called name was
// expanded from, for a caller that holds only the status of a forEach step's passes.
func IterationTemplate(step oamv1alpha1.WorkflowStep, name string) (oamv1alpha1.WorkflowStepBase, bool) {
	_, template, ok := parseIteration(step, name)
	return template, ok
}

// IterationIndex returns the index of the pass the iteration step called name belongs to.
func IterationIndex(step oamv1alpha1.WorkflowStep, name string) (int, bool) {
	index, _, ok := parseIteration(step, name)
	return index, ok
}

func parseIteration(step oamv1alpha1.WorkflowStep, name string) (int, oamv1alpha1.WorkflowStepBase, bool) {
	template, group := body(step)
	index, sub, ok := cutIndex(step.Name, name)
	if !ok {
		return 0, oamv1alpha1.WorkflowStepBase{}, false
	}
	if !group {
		if sub != "" {
			return 0, oamv1alpha1.WorkflowStepBase{}, false
		}
		return index, template[0], true
	}
	for _, t := range template {
		if t.Name == sub {
			return index, t, true
		}
	}
	return 0, oamv1alpha1.WorkflowStepBase{}, false
}

// cutIndex splits <loop>-<index>[-<rest>] into the index and rest.
func cutIndex(loop, name string) (int, string, bool) {
	rest, ok := strings.CutPrefix(name, loop+"-")
	if !ok {
		return 0, "", false
	}
	digits, tail, _ := strings.Cut(rest, "-")
	index, err := strconv.Atoi(digits)
	if err != nil || strconv.Itoa(index) != digits {
		return 0, "", false
	}
	return index, tail, true
}

// IterationVarIndex returns the index of the pass a workflow variable belongs to: the
// {item, index} one, or an output scoped to that pass.
func IterationVarIndex(step oamv1alpha1.WorkflowStep, label string) (int, bool) {
	index, rest, ok := cutIndex(step.Name, label)
	return index, ok && rest != ""
}

// CollectedOutputs names the variables a forEach step writes as lists once it succeeds.
func CollectedOutputs(step oamv1alpha1.WorkflowStep) []string {
	template, _ := body(step)
	var names []string
	for _, sub := range template {
		for _, o := range sub.Outputs {
			names = append(names, o.Name)
		}
	}
	return names
}

// PinnedItemsKey is where a forEach step's resolved items are kept in the workflow
// context, under the ID of the step's current run.
func PinnedItemsKey(stepID string) string {
	return stepID + "." + forEachItemsKey
}

// IsPinnedItemsKey reports whether a workflow context key holds a pinned forEach list.
func IsPinnedItemsKey(key string) bool {
	return strings.HasSuffix(key, "."+forEachItemsKey)
}

// collectOutputs writes each body output as a list, one entry per iteration and null
// where an iteration did not produce it.
func collectOutputs(ctx wfContext.Context, cueCtx *cue.Context, step oamv1alpha1.WorkflowStep, count int) error {
	template, _ := body(step)
	for _, sub := range template {
		for _, o := range sub.Outputs {
			values := make([]string, count)
			for i := range values {
				values[i] = "null"
				if v, err := ctx.GetVar(iterationName(step.Name, i, o.Name)); err == nil {
					b, err := v.MarshalJSON()
					if err != nil {
						return errors.WithMessagef(err, "read output %s of iteration %d", o.Name, i)
					}
					values[i] = string(b)
				}
			}
			list := cueCtx.CompileString("[" + strings.Join(values, ",") + "]")
			if err := ctx.SetVar(list, o.Name); err != nil {
				return errors.WithMessagef(err, "set collected output %s", o.Name)
			}
		}
	}
	return nil
}

// iterationRunner exposes context.loop to a body step while it is checked and run.
type iterationRunner struct {
	types.TaskRunner
	pCtx  process.Context
	item  any
	index int
}

func (r *iterationRunner) withLoop() func() {
	meta := process.WithLoop(r.item, r.index)
	manager := process.NewStepRunTimeMeta()
	manager.Fill(r.pCtx, []process.StepMetaKV{meta})
	return func() { manager.Remove(r.pCtx, []string{meta.Key}) }
}

func (r *iterationRunner) Pending(ctx monitorContext.Context, wfCtx wfContext.Context, stepStatus map[string]v1alpha1.StepStatus) (bool, v1alpha1.StepStatus) {
	defer r.withLoop()()
	return r.TaskRunner.Pending(ctx, wfCtx, stepStatus)
}

func (r *iterationRunner) Run(ctx wfContext.Context, options *types.TaskRunOptions) (v1alpha1.StepStatus, *types.Operation, error) {
	defer r.withLoop()()
	return r.TaskRunner.Run(ctx, options)
}

// skippedRunner records a skip for an iteration step that must not run.
type skippedRunner struct {
	step oamv1alpha1.WorkflowStepBase
	id   string
}

func (r *skippedRunner) Name() string { return r.step.Name }

func (r *skippedRunner) Pending(monitorContext.Context, wfContext.Context, map[string]v1alpha1.StepStatus) (bool, v1alpha1.StepStatus) {
	return false, v1alpha1.StepStatus{}
}

func (r *skippedRunner) Run(wfContext.Context, *types.TaskRunOptions) (v1alpha1.StepStatus, *types.Operation, error) {
	return v1alpha1.StepStatus{
		ID:     r.id,
		Name:   r.step.Name,
		Type:   r.step.Type,
		Phase:  v1alpha1.WorkflowStepPhaseSkipped,
		Reason: types.StatusReasonSkip,
	}, &types.Operation{Skip: true}, nil
}

func (r *skippedRunner) FillContextData(monitorContext.Context, process.Context) types.ContextDataResetter {
	return func(process.Context) {}
}
