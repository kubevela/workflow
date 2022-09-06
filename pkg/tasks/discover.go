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

package tasks

import (
	"context"

	"github.com/pkg/errors"

	monitorContext "github.com/kubevela/workflow/pkg/monitor/context"
	"github.com/kubevela/workflow/pkg/tasks/builtin"
	"github.com/kubevela/workflow/pkg/tasks/custom"
	"github.com/kubevela/workflow/pkg/types"
)

type taskDiscover struct {
	builtin            map[string]types.TaskGenerator
	customTaskDiscover *custom.TaskLoader
}

func NewTaskDiscover(ctx monitorContext.Context, options types.StepGeneratorOptions) types.TaskDiscover {
	return &taskDiscover{
		builtin: map[string]types.TaskGenerator{
			types.WorkflowStepTypeSuspend:   builtin.Suspend,
			types.WorkflowStepTypeStepGroup: builtin.StepGroup,
		},
		customTaskDiscover: custom.NewTaskLoader(options.TemplateLoader.LoadTemplate, options.PackageDiscover, options.Providers, options.LogLevel, options.ProcessCtx),
	}
}

// GetTaskGenerator get task generator by name.
func (td *taskDiscover) GetTaskGenerator(ctx context.Context, name string) (types.TaskGenerator, error) {
	tg, ok := td.builtin[name]
	if ok {
		return tg, nil
	}
	if td.customTaskDiscover != nil {
		var err error
		tg, err = td.customTaskDiscover.GetTaskGenerator(ctx, name)
		if err != nil {
			return nil, err
		}
		return tg, nil

	}
	return nil, errors.Errorf("can't find task generator: %s", name)
}
