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

package steps

import (
	"context"

	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kubevela/workflow/api/v1alpha1"
	"github.com/kubevela/workflow/pkg/cue/process"
	monitorContext "github.com/kubevela/workflow/pkg/monitor/context"
	"github.com/kubevela/workflow/pkg/monitor/metrics"
	"github.com/kubevela/workflow/pkg/providers"
	"github.com/kubevela/workflow/pkg/providers/email"
	"github.com/kubevela/workflow/pkg/providers/http"
	"github.com/kubevela/workflow/pkg/providers/kube"
	"github.com/kubevela/workflow/pkg/providers/util"
	"github.com/kubevela/workflow/pkg/providers/workspace"
	"github.com/kubevela/workflow/pkg/tasks"
	"github.com/kubevela/workflow/pkg/tasks/template"
	"github.com/kubevela/workflow/pkg/types"
	"github.com/kubevela/workflow/pkg/utils"
)

// Generate generates task runners
func Generate(ctx monitorContext.Context, wr *v1alpha1.WorkflowRun, options types.StepGeneratorOptions) ([]types.TaskRunner, error) {
	ctx.V(options.LogLevel)
	subCtx := ctx.Fork("generate-task-runners", monitorContext.DurationMetric(func(v float64) {
		metrics.GenerateTaskRunnersDurationHistogram.WithLabelValues("workflowrun").Observe(v)
	}))
	defer subCtx.Commit("finish generate task runners")
	options = initStepGeneratorOptions(ctx, wr, options)
	taskDiscover := tasks.NewTaskDiscover(ctx, options)
	var tasks []types.TaskRunner
	for _, step := range wr.Spec.WorkflowSpec.Steps {
		opt := &types.TaskGeneratorOptions{
			ID:              generateStepID(wr.Status, step.Name),
			PackageDiscover: options.PackageDiscover,
			ProcessContext:  options.ProcessCtx,
		}
		for typ, convertor := range options.StepConvertor {
			if step.Type == typ {
				opt.StepConvertor = convertor
			}
		}
		task, err := generateTaskRunner(ctx, wr, step, taskDiscover, opt, options)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, task)
	}
	return tasks, nil
}

func initStepGeneratorOptions(ctx monitorContext.Context, wr *v1alpha1.WorkflowRun, options types.StepGeneratorOptions) types.StepGeneratorOptions {
	if options.Providers == nil {
		options.Providers = providers.NewProviders()
	}
	installBuiltinProviders(wr, options.Client, options.Providers)
	if options.ProcessCtx == nil {
		options.ProcessCtx = process.NewContext(generateContextDataFromWorkflowRun(wr))
	}
	if options.TemplateLoader == nil {
		options.TemplateLoader = template.NewWorkflowStepTemplateLoader(options.Client)
	}
	return options
}

func installBuiltinProviders(wr *v1alpha1.WorkflowRun, client client.Client, providerHandlers types.Providers) {
	workspace.Install(providerHandlers)
	email.Install(providerHandlers)
	util.Install(providerHandlers)
	http.Install(providerHandlers, client, wr.Namespace)
	kube.Install(providerHandlers, client, nil, map[string]string{
		types.LabelWorkflowRunName:      wr.Name,
		types.LabelWorkflowRunNamespace: wr.Namespace,
	}, nil)
}

func generateTaskRunner(ctx context.Context,
	wr *v1alpha1.WorkflowRun,
	step v1alpha1.WorkflowStep,
	taskDiscover types.TaskDiscover,
	options *types.TaskGeneratorOptions,
	stepOptions types.StepGeneratorOptions) (types.TaskRunner, error) {
	if step.Type == types.WorkflowStepTypeStepGroup {
		var subTaskRunners []types.TaskRunner
		for _, subStep := range step.SubSteps {
			workflowStep := v1alpha1.WorkflowStep{
				WorkflowStepBase: subStep,
			}
			o := &types.TaskGeneratorOptions{
				ID:              generateSubStepID(wr.Status, subStep.Name, step.Name),
				PackageDiscover: options.PackageDiscover,
				ProcessContext:  options.ProcessContext,
			}
			for typ, convertor := range stepOptions.StepConvertor {
				if subStep.Type == typ {
					o.StepConvertor = convertor
				}
			}
			subTask, err := generateTaskRunner(ctx, wr, workflowStep, taskDiscover, o, stepOptions)
			if err != nil {
				return nil, err
			}
			subTaskRunners = append(subTaskRunners, subTask)
		}
		options.SubTaskRunners = subTaskRunners
		options.SubStepExecuteMode = v1alpha1.WorkflowModeDAG
		if wr.Spec.Mode != nil {
			options.SubStepExecuteMode = wr.Spec.Mode.SubSteps
		}
	}

	genTask, err := taskDiscover.GetTaskGenerator(ctx, step.Type)
	if err != nil {
		return nil, err
	}

	task, err := genTask(step, options)
	if err != nil {
		return nil, err
	}
	return task, nil
}

func generateStepID(status v1alpha1.WorkflowRunStatus, name string) string {
	for _, ss := range status.Steps {
		if ss.Name == name {
			return ss.ID
		}
	}

	return utils.RandomString(10)
}

func generateSubStepID(status v1alpha1.WorkflowRunStatus, name, parentStepName string) string {
	for _, ss := range status.Steps {
		if ss.Name == parentStepName {
			for _, sub := range ss.SubStepsStatus {
				if sub.Name == name {
					return sub.ID
				}
			}
		}
	}

	return utils.RandomString(10)
}

func generateContextDataFromWorkflowRun(wr *v1alpha1.WorkflowRun) process.ContextData {
	data := process.ContextData{
		Name:      wr.Name,
		Namespace: wr.Namespace,
	}
	if wr.Spec.WorkflowRef != "" {
		data.WorkflowName = wr.Spec.WorkflowRef
	}
	return data
}
