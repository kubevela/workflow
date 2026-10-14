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

import "context"

// RunStore is where a run keeps values across its steps and reconciles: its
// workflow context.
type RunStore interface {
	GetMutableValue(paths ...string) string
	SetMutableValue(data string, paths ...string)
}

// RunScope is the run and step a host's resolver answers for, so that it can
// hold a value for the whole run and say which step read it.
type RunScope struct {
	// Run identifies this run of the workflow. A workflow context can outlive a
	// run, as an Application's does across revisions, so a value kept in Store
	// is the run's only if it was kept under this.
	Run string
	// Step and StepType are the step whose expressions are resolved, Step as
	// it runs: a forEach iteration's step has its own name.
	Step     string
	StepType string
	Store    RunStore
}

type runScopeKey struct{}

func withRun(ctx context.Context, scope RunScope) context.Context {
	return context.WithValue(ctx, runScopeKey{}, scope)
}

// RunFrom is the run and step a resolver is called for, if it is called by a
// workflow.
func RunFrom(ctx context.Context) (RunScope, bool) {
	scope, ok := ctx.Value(runScopeKey{}).(RunScope)
	return scope, ok
}
