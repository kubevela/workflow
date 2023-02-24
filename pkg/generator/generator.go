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

package generator

import (
	"context"
	"encoding/json"
	"errors"

	metrics2 "github.com/kubevela/workflow/pkg/providers/metrics"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/client"

	monitorContext "github.com/kubevela/pkg/monitor/context"
	"github.com/kubevela/pkg/util/rand"

	"github.com/oam-dev/kubevela/pkg/config/provider"

	"github.com/kubevela/workflow/api/v1alpha1"
	"github.com/kubevela/workflow/pkg/cue/process"
	"github.com/kubevela/workflow/pkg/executor"
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
)

// GenerateRunners generates task runners
func GenerateRunners(ctx monitorContext.Context, instance *types.WorkflowInstance, options types.StepGeneratorOptions) ([]types.TaskRunner, error) {
	ctx.V(options.LogLevel)
	subCtx := ctx.Fork("generate-task-runners", monitorContext.DurationMetric(func(v float64) {
		metrics.GenerateTaskRunnersDurationHistogram.WithLabelValues("workflowrun").Observe(v)
	}))
	defer subCtx.Commit("finish generate task runners")
	options = initStepGeneratorOptions(ctx, instance, options)
	taskDiscover := tasks.NewTaskDiscover(ctx, options)
	var tasks []types.TaskRunner
	for _, step := range instance.Steps {
		opt := &types.TaskGeneratorOptions{
			ID:              generateStepID(instance.Status, step.Name),
			PackageDiscover: options.PackageDiscover,
			ProcessContext:  options.ProcessCtx,
		}
		for typ, convertor := range options.StepConvertor {
			if step.Type == typ {
				opt.StepConvertor = convertor
			}
		}
		task, err := generateTaskRunner(ctx, instance, step, taskDiscover, opt, options)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, task)
	}
	return tasks, nil
}

// GenerateWorkflowInstance generates a workflow instance
func GenerateWorkflowInstance(ctx context.Context, cli client.Client, run *v1alpha1.WorkflowRun) (*types.WorkflowInstance, error) {
	var steps []v1alpha1.WorkflowStep
	mode := run.Spec.Mode
	switch {
	case run.Spec.WorkflowSpec != nil:
		steps = run.Spec.WorkflowSpec.Steps
	case run.Spec.WorkflowRef != "":
		template := new(v1alpha1.Workflow)
		if err := cli.Get(ctx, client.ObjectKey{
			Name:      run.Spec.WorkflowRef,
			Namespace: run.Namespace,
		}, template); err != nil {
			return nil, err
		}
		steps = template.WorkflowSpec.Steps
		if template.Mode != nil && mode == nil {
			mode = template.Mode
		}
	default:
		return nil, errors.New("failed to generate workflow instance")
	}

	debug := false
	if run.Annotations != nil && run.Annotations[types.AnnotationWorkflowRunDebug] == "true" {
		debug = true
	}

	contextData := make(map[string]interface{})
	if run.Spec.Context != nil {
		contextByte, err := run.Spec.Context.MarshalJSON()
		if err != nil {
			return nil, err
		}
		if err := json.Unmarshal(contextByte, &contextData); err != nil {
			return nil, err
		}
	}
	instance := &types.WorkflowInstance{
		WorkflowMeta: types.WorkflowMeta{
			Name:        run.Name,
			Namespace:   run.Namespace,
			Annotations: run.Annotations,
			Labels:      run.Labels,
			UID:         run.UID,
			ChildOwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: v1alpha1.SchemeGroupVersion.String(),
					Kind:       v1alpha1.WorkflowRunKind,
					Name:       run.Name,
					UID:        run.UID,
					Controller: pointer.BoolPtr(true),
				},
			},
		},
		Context: contextData,
		Debug:   debug,
		Mode:    mode,
		Steps:   steps,
		Status:  run.Status,
	}
	executor.InitializeWorkflowInstance(instance)
	return instance, nil
}

func initStepGeneratorOptions(ctx monitorContext.Context, instance *types.WorkflowInstance, options types.StepGeneratorOptions) types.StepGeneratorOptions {
	if options.Providers == nil {
		options.Providers = providers.NewProviders()
	}
	if options.ProcessCtx == nil {
		options.ProcessCtx = process.NewContext(generateContextDataFromWorkflowRun(instance))
	}
	installBuiltinProviders(instance, options.Client, options.Providers, options.ProcessCtx)
	if options.TemplateLoader == nil {
		options.TemplateLoader = template.NewWorkflowStepTemplateLoader(options.Client)
	}
	return options
}

func installBuiltinProviders(instance *types.WorkflowInstance, client client.Client, providerHandlers types.Providers, pCtx process.Context) {
	workspace.Install(providerHandlers)
	email.Install(providerHandlers)
	util.Install(providerHandlers, pCtx)
	http.Install(providerHandlers, client, instance.Namespace)
	provider.Install(providerHandlers, client, nil)
	metrics2.Install(providerHandlers)
	kube.Install(providerHandlers, client, map[string]string{
		types.LabelWorkflowRunName:      instance.Name,
		types.LabelWorkflowRunNamespace: instance.Namespace,
	}, nil)
}

func generateTaskRunner(ctx context.Context,
	instance *types.WorkflowInstance,
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
				ID:              generateSubStepID(instance.Status, subStep.Name, step.Name),
				PackageDiscover: options.PackageDiscover,
				ProcessContext:  options.ProcessContext,
			}
			for typ, convertor := range stepOptions.StepConvertor {
				if subStep.Type == typ {
					o.StepConvertor = convertor
				}
			}
			subTask, err := generateTaskRunner(ctx, instance, workflowStep, taskDiscover, o, stepOptions)
			if err != nil {
				return nil, err
			}
			subTaskRunners = append(subTaskRunners, subTask)
		}
		options.SubTaskRunners = subTaskRunners
		options.SubStepExecuteMode = v1alpha1.WorkflowModeDAG
		if instance.Mode != nil {
			options.SubStepExecuteMode = instance.Mode.SubSteps
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

	return rand.RandomString(10)
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

	return rand.RandomString(10)
}

func generateContextDataFromWorkflowRun(instance *types.WorkflowInstance) process.ContextData {
	data := process.ContextData{
		Name:       instance.Name,
		Namespace:  instance.Namespace,
		CustomData: instance.Context,
	}
	return data
}
