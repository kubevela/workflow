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

package executor

import (
	"github.com/kubevela/pkg/cue/cuex"
	"github.com/kubevela/workflow/pkg/types"
)

// Option is the option of executor
type Option interface {
	ApplyTo(*workflowExecutor)
}

type withCompiler struct {
	compiler *cuex.Compiler
}

func (w *withCompiler) ApplyTo(e *workflowExecutor) {
	e.compiler = w.compiler
}

// WithCompiler set the cue compiler
func WithCompiler(compiler *cuex.Compiler) Option {
	return &withCompiler{compiler: compiler}
}

type withStatusPatcher struct {
	patcher types.StatusPatcher
}

func (w *withStatusPatcher) ApplyTo(e *workflowExecutor) {
	e.patcher = w.patcher
}

// WithStatusPatcher set the status patcher
func WithStatusPatcher(patcher types.StatusPatcher) Option {
	return &withStatusPatcher{patcher: patcher}
}
