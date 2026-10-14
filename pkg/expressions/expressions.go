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

// Package expressions answers the roots a $( ) expression in a workflow step may
// read: context, steps and each. A host such as KubeVela adds roots of its own, with
// their resolvers, through NewEngine.
package expressions

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"cuelang.org/go/cue"
	"github.com/kubevela/pkg/apis/oam/v1alpha1"
	"github.com/kubevela/pkg/cel"
	"k8s.io/apiserver/pkg/util/feature"

	wfv1alpha1 "github.com/kubevela/workflow/api/v1alpha1"
	"github.com/kubevela/workflow/pkg/cue/model"
	"github.com/kubevela/workflow/pkg/cue/process"
	"github.com/kubevela/workflow/pkg/features"
	"github.com/kubevela/workflow/pkg/types"
)

const (
	// RootContext is the step's context: the run's name and namespace, the step's
	// name, and whatever the run adds. It is the context a CUE template reads.
	RootContext = "context"
	// RootSteps is the outputs earlier steps declare, as steps.<name>.outputs.<output>.
	RootSteps = "steps"
	// RootEach is the current forEach iteration, as each.item and each.index: the
	// names inputs[].from reads it by.
	RootEach = model.ContextEach
)

// NewEngine builds an engine over the workflow's roots and the host's. A host
// root named like a workflow one replaces it.
func NewEngine(host ...cel.Root) (*cel.Engine, error) {
	roots := append([]cel.Root(nil), host...)
	for _, name := range []string{RootContext, RootSteps, RootEach} {
		if !declares(host, name) {
			roots = append(roots, cel.Root{Name: name})
		}
	}
	return cel.NewEngine(roots...)
}

func declares(roots []cel.Root, name string) bool {
	for _, r := range roots {
		if r.Name == name {
			return true
		}
	}
	return false
}

// EnabledFor reports whether a WorkflowRun with these annotations resolves $( )
// expressions: the feature gate is on and the run opts in.
func EnabledFor(annotations map[string]string) bool {
	return feature.DefaultMutableFeatureGate.Enabled(features.EnableCelExpressions) &&
		annotations[types.AnnotationCelExpressions] == "true"
}

// notReady is a read that will be answered once the run gets further.
type notReady struct{ msg string }

func (e notReady) Error() string        { return e.msg }
func (e notReady) Is(target error) bool { return target == cel.ErrNotReady }

func waiting(format string, args ...interface{}) error {
	return notReady{msg: fmt.Sprintf(format, args...)}
}

// ContextResolver answers context from the step's process context.
func ContextResolver(pCtx process.Context) cel.Resolver {
	return cel.ResolverFunc(func(_ context.Context, reads []cel.Read) (interface{}, error) {
		// A run's own context data joins the process context only when the context
		// file is built.
		if _, err := pCtx.BaseContextFile(); err != nil {
			return nil, err
		}
		out := map[string]interface{}{}
		for _, r := range reads {
			if len(r.Path) == 0 {
				continue
			}
			if v := pCtx.GetData(r.Path[0]); v != nil {
				out[r.Path[0]] = v
			}
		}
		return out, nil
	})
}

// EachResolver answers each from the step's process context, where a forEach
// iteration sets it.
func EachResolver(pCtx process.Context) cel.Resolver {
	return cel.ResolverFunc(func(_ context.Context, reads []cel.Read) (interface{}, error) {
		v := pCtx.GetData(model.ContextEach)
		if v == nil {
			return nil, fmt.Errorf("%s: each is only set inside a forEach", reads[0].String())
		}
		return v, nil
	})
}

// Vars is where a run keeps what its steps output.
type Vars interface {
	GetVar(paths ...string) (cue.Value, error)
}

// StepsResolver answers steps.<name>.outputs.<output> for an output the named
// step declares. A read waits while that step has still to finish, and fails once
// it has failed or been skipped, since then it never will.
//
// A step in a forEach body reads by the names inputs[].from does: inside the loop,
// a body step's outputs are this iteration's; outside it, they are the lists the
// loop collected, ready once the loop is.
func StepsResolver(steps []v1alpha1.WorkflowStep, status map[string]wfv1alpha1.StepStatus, vars Vars) cel.Resolver {
	return stepsResolver(newWorkflowSteps(steps), status, vars, reader{}, nil)
}

// workflowSteps is what a steps read needs to know of the workflow's steps.
type workflowSteps struct {
	declared map[string]v1alpha1.StepOutputs
	// loops is every forEach step.
	loops map[string]bool
	// loopOf is the forEach step whose body each step-group sub-step is.
	loopOf map[string]string
}

func newWorkflowSteps(steps []v1alpha1.WorkflowStep) workflowSteps {
	ws := workflowSteps{declared: map[string]v1alpha1.StepOutputs{}, loops: map[string]bool{}, loopOf: map[string]string{}}
	for _, s := range steps {
		ws.declared[s.Name] = s.Outputs
		if s.ForEach != nil {
			ws.loops[s.Name] = true
		}
		for _, sub := range s.SubSteps {
			ws.declared[sub.Name] = sub.Outputs
			if s.ForEach != nil {
				ws.loopOf[sub.Name] = s.Name
			}
		}
	}
	return ws
}

// reader is the step whose properties are being resolved: its name as it runs
// and as written, and the loop and iteration it is in, if any.
type reader struct {
	name    string
	written string
	loop    string
	index   int
}

func (ws workflowSteps) readerOf(pCtx process.Context) reader {
	r := reader{}
	r.name, _ = pCtx.GetData(model.ContextStepName).(string)
	r.written = r.name
	each, _ := pCtx.GetData(model.ContextEach).(map[string]interface{})
	index, ok := each["index"].(int)
	if !ok {
		return r
	}
	// The longest match wins, so a loop named like another's passes cannot take
	// its readers; admission refuses such names anyway.
	for loop := range ws.loops {
		prefix := types.IterationName(loop, strconv.Itoa(index), "")
		switch {
		case len(loop) <= len(r.loop):
		case r.name == fmt.Sprintf("%s-%d", loop, index):
			r.loop, r.index, r.written = loop, index, loop
		case strings.HasPrefix(r.name, prefix):
			r.loop, r.index, r.written = loop, index, strings.TrimPrefix(r.name, prefix)
		}
	}
	return r
}

func stepsResolver(ws workflowSteps, status map[string]wfv1alpha1.StepStatus, vars Vars, r reader,
	refused map[string]map[string]string) cel.Resolver {
	return cel.ResolverFunc(func(_ context.Context, reads []cel.Read) (interface{}, error) {
		out := map[string]interface{}{}
		for _, read := range reads {
			if len(read.Path) > 0 {
				if reason, ok := refused[r.written][read.Path[0]]; ok {
					return nil, fmt.Errorf("%s: %s", read.String(), reason)
				}
			}
			if len(read.Path) < 2 {
				return nil, fmt.Errorf("%s: %w", read.String(), readHint(read))
			}
			step := read.Path[0]
			// A guarded read of a missing output still finds its step, so has() can
			// answer false rather than fail one level up.
			if _, ok := out[step]; !ok {
				out[step] = map[string]interface{}{"outputs": map[string]interface{}{}}
			}
			if isStatusField(read.Path[1]) {
				st, err := stepStatus(read, ws, status, r)
				if err != nil {
					return nil, fmt.Errorf("%s: %w", read.String(), err)
				}
				for k, v := range st {
					out[step].(map[string]interface{})[k] = v
				}
				continue
			}
			v, found, err := stepOutput(read, ws, status, vars, r)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", read.String(), err)
			}
			output := read.Path[2]
			if found {
				out[step].(map[string]interface{})["outputs"].(map[string]interface{})[output] = v
			}
		}
		return out, nil
	})
}

func stepOutput(read cel.Read, ws workflowSteps, status map[string]wfv1alpha1.StepStatus, vars Vars,
	r reader) (interface{}, bool, error) {
	if len(read.Path) < 3 || read.Path[1] != "outputs" {
		return nil, false, readHint(read)
	}
	step, output := read.Path[0], read.Path[2]
	statusOf, varOf, err := ws.located(step, output, r, "outputs")
	if err != nil {
		return nil, false, err
	}

	if !declaresOutput(ws.declared[step], output) {
		return nil, false, fmt.Errorf("step %s declares no output %q", step, output)
	}

	// A skipped step never runs, so its outputs are absent: a guarded read answers
	// that, and an unguarded one fails. Whether a failed step is finished depends on
	// EnableSuspendOnFailure, under which it suspends the run and may be resumed.
	s, ok := status[statusOf]
	switch {
	case ok && s.Phase == wfv1alpha1.WorkflowStepPhaseSkipped && read.Defaulted:
		return nil, false, nil
	case ok && s.Phase == wfv1alpha1.WorkflowStepPhaseSkipped:
		return nil, false, fmt.Errorf("step %s was skipped", statusOf)
	case !ok || !types.IsStepFinish(s.Phase, s.Reason):
		return nil, false, waiting("waiting for step %s", statusOf)
	case s.Phase != wfv1alpha1.WorkflowStepPhaseSucceeded:
		return nil, false, fmt.Errorf("step %s failed", statusOf)
	}

	v, err := vars.GetVar(strings.Split(varOf, ".")...)
	if err != nil {
		if read.Defaulted {
			return nil, false, nil
		}
		return nil, false, waiting("step %s has no output %s yet", statusOf, output)
	}
	raw, err := v.MarshalJSON()
	if err != nil {
		return nil, false, fmt.Errorf("output %s of step %s: %w", output, statusOf, err)
	}
	var decoded interface{}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return nil, false, err
	}
	return decoded, true, nil
}

// located is where a read of step's what (its outputs or its status) is answered
// from: the step's own status and the variable named name, or, for a body step of
// a loop, the loop's as a whole from outside it and this pass's from inside.
func (ws workflowSteps) located(step, name string, r reader, what string) (statusOf, varOf string, err error) {
	if _, ok := ws.declared[step]; !ok {
		return "", "", fmt.Errorf("no step %q", step)
	}
	statusOf, varOf = step, name
	if loop, ok := ws.loopOf[step]; ok {
		statusOf = loop
		if loop == r.loop {
			statusOf, varOf = types.IterationName(loop, strconv.Itoa(r.index), step), types.IterationName(loop, strconv.Itoa(r.index), name)
		}
	}
	if (r.name != "" && statusOf == r.name) || (r.loop != "" && step == r.loop) {
		return "", "", fmt.Errorf("step %s cannot read its own %s", step, what)
	}
	return statusOf, varOf, nil
}

// statusFields are what steps.<name> offers beside outputs: the step's status once
// it has finished, in any phase.
var statusFields = []string{"phase", "message", "reason", "succeeded", "failed", "skipped", "timeout",
	"failedAfterRetries", "terminate"}

func isStatusField(name string) bool {
	for _, f := range statusFields {
		if f == name {
			return true
		}
	}
	return false
}

// readHint is the error for a read of steps.<name> that is neither an output nor
// a status field.
func readHint(read cel.Read) error {
	name := "<step>"
	if len(read.Path) > 0 {
		name = read.Path[0]
	}
	return fmt.Errorf("read steps.%s.outputs.<name>, or steps.%s.<%s>", name, name, strings.Join(statusFields, "|"))
}

// stepStatus is a finished step's status as steps.<name> reads it. A read waits
// while the step has still to finish.
func stepStatus(read cel.Read, ws workflowSteps, status map[string]wfv1alpha1.StepStatus, r reader) (map[string]interface{}, error) {
	statusOf, _, err := ws.located(read.Path[0], "", r, "status")
	if err != nil {
		return nil, err
	}
	// A skipped step never runs, whatever EnableSuspendOnFailure makes of a failed one.
	s, ok := status[statusOf]
	if !ok || (s.Phase != wfv1alpha1.WorkflowStepPhaseSkipped && !types.IsStepFinish(s.Phase, s.Reason)) {
		return nil, waiting("waiting for step %s", statusOf)
	}
	return map[string]interface{}{
		"phase":              string(s.Phase),
		"message":            s.Message,
		"reason":             s.Reason,
		"succeeded":          s.Phase == wfv1alpha1.WorkflowStepPhaseSucceeded,
		"failed":             s.Phase == wfv1alpha1.WorkflowStepPhaseFailed,
		"skipped":            s.Phase == wfv1alpha1.WorkflowStepPhaseSkipped,
		"timeout":            s.Reason == types.StatusReasonTimeout,
		"failedAfterRetries": s.Reason == types.StatusReasonFailedAfterRetries,
		"terminate":          s.Reason == types.StatusReasonTerminate,
	}, nil
}

func declaresOutput(outputs v1alpha1.StepOutputs, name string) bool {
	for _, o := range outputs {
		if o.Name == name {
			return true
		}
	}
	return false
}
