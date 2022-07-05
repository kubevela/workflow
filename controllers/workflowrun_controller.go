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

package controllers

import (
	"context"
	"fmt"
	"time"

	kerrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/crossplane/crossplane-runtime/pkg/event"
	"github.com/kubevela/workflow/api/condition"
	"github.com/kubevela/workflow/api/v1alpha1"
	"github.com/kubevela/workflow/pkg/cue/packages"
	"github.com/kubevela/workflow/pkg/cue/process"
	"github.com/kubevela/workflow/pkg/executor"
	monitorContext "github.com/kubevela/workflow/pkg/monitor/context"
	"github.com/kubevela/workflow/pkg/providers"
	"github.com/kubevela/workflow/pkg/steps"
	"github.com/kubevela/workflow/pkg/tasks/template"
	"github.com/kubevela/workflow/pkg/types"
	"github.com/pkg/errors"
)

// WorkflowRunReconciler reconciles a WorkflowRun object
type WorkflowRunReconciler struct {
	client.Client
	Scheme          *runtime.Scheme
	PackageDiscover *packages.PackageDiscover
	Recorder        event.Recorder
}

var (
	// ReconcileTimeout timeout for controller to reconcile
	ReconcileTimeout = time.Minute * 3
)

//+kubebuilder:rbac:groups=core.oam.dev,resources=workflowruns,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=core.oam.dev,resources=workflowruns/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=core.oam.dev,resources=workflowruns/finalizers,verbs=update
func (r *WorkflowRunReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	ctx, cancel := context.WithTimeout(ctx, ReconcileTimeout)
	defer cancel()

	logCtx := monitorContext.NewTraceContext(ctx, "").AddTag("workflowrun", req.String(), "controller")
	logCtx.Info("Start reconcile workflowrun")
	defer logCtx.Commit("End reconcile workflowrun")
	run := new(v1alpha1.WorkflowRun)
	if err := r.Get(ctx, client.ObjectKey{
		Name:      req.Name,
		Namespace: req.Namespace,
	}, run); err != nil {
		if !kerrors.IsNotFound(err) {
			logCtx.Error(err, "get workflowrun")
		} else {
			return ctrl.Result{}, client.IgnoreNotFound(err)
		}
	}

	switch {
	case run.Spec.WorkflowSpec != nil && len(run.Spec.WorkflowSpec.Steps) > 0:
	case run.Spec.WorkflowRef != "":
		template := new(v1alpha1.Workflow)
		if err := r.Get(ctx, client.ObjectKey{
			Name:      run.Spec.WorkflowRef,
			Namespace: run.Namespace,
		}, template); err != nil {
			logCtx.Error(err, "get workflow ref")
			return ctrl.Result{}, err
		}
		if len(template.WorkflowSpec.Steps) > 0 {
			run.Spec.WorkflowSpec = &template.WorkflowSpec
		}
	default:
		return ctrl.Result{}, nil
	}

	runners, err := steps.Generate(logCtx, run, types.StepGeneratorOptions{
		Providers:       providers.NewProviders(),
		PackageDiscover: r.PackageDiscover,
		ProcessCtx:      process.NewContext(steps.GenerateContextDataFromWorkflowRun(run)),
		TemplateLoader:  template.NewWorkflowStepTemplateLoader(r.Client),
	})
	if err != nil {
		logCtx.Error(err, "[generate runners]")
		r.Recorder.Event(run, event.Warning(v1alpha1.ReasonFailedWorkflow, err))
		return r.endWithNegativeCondition(logCtx, run, condition.ErrorCondition(v1alpha1.WorkflowRunConditionType, err), v1alpha1.WorkflowRunInitializing)
	}

	executor := executor.New(run, r.Client)
	state, err := executor.ExecuteRunners(logCtx, runners)
	if err != nil {
		logCtx.Error(err, "[execute runners]")
		r.Recorder.Event(run, event.Warning(v1alpha1.ReasonFailedWorkflow, err))
		return r.endWithNegativeCondition(logCtx, run, condition.ErrorCondition(v1alpha1.WorkflowRunConditionType, err), v1alpha1.WorkflowRunExecuting)
	}
	switch state {
	case types.WorkflowStateInitializing:
		logCtx.Info("Workflow return state=Initializing")
		return ctrl.Result{}, r.patchStatus(logCtx, run, v1alpha1.WorkflowRunInitializing)
	case types.WorkflowStateSuspended:
		logCtx.Info("Workflow return state=Suspend")
		if duration := executor.GetSuspendBackoffWaitTime(); duration > 0 {
			return ctrl.Result{RequeueAfter: duration}, r.patchStatus(logCtx, run, v1alpha1.WorkflowRunSuspending)
		}
		return ctrl.Result{}, r.patchStatus(logCtx, run, v1alpha1.WorkflowRunSuspending)
	case types.WorkflowStateTerminated:
		logCtx.Info("Workflow return state=Terminated")
		return ctrl.Result{}, r.patchStatus(logCtx, run, v1alpha1.WorkflowRunTerminated)
	case types.WorkflowStateExecuting:
		logCtx.Info("Workflow return state=Executing")
		return ctrl.Result{RequeueAfter: executor.GetBackoffWaitTime()}, r.patchStatus(logCtx, run, v1alpha1.WorkflowRunExecuting)
	case types.WorkflowStateSucceeded:
		logCtx.Info("Workflow return state=Succeeded")
		run.Status.SetConditions(condition.ReadyCondition(v1alpha1.WorkflowRunConditionType))
		r.Recorder.Event(run, event.Normal(v1alpha1.ReasonApplied, v1alpha1.MessageWorkflowFinished))
		logCtx.Info("Application manifests has applied by workflow successfully")
		return ctrl.Result{}, r.patchStatus(logCtx, run, v1alpha1.WorkflowRunSuspending)
	case types.WorkflowStateSkipping:
		logCtx.Info("Skip this reconcile")
		return ctrl.Result{}, nil
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *WorkflowRunReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.WorkflowRun{}).
		Complete(r)
}

func (r *WorkflowRunReconciler) endWithNegativeCondition(ctx context.Context, wr *v1alpha1.WorkflowRun, condition condition.Condition, phase v1alpha1.WorkflowRunPhase) (ctrl.Result, error) {
	wr.SetConditions(condition)
	if err := r.patchStatus(ctx, wr, phase); err != nil {
		return ctrl.Result{}, errors.WithMessage(err, "cannot update application status")
	}
	return ctrl.Result{}, fmt.Errorf("object level reconcile error, type: %q, msg: %q", string(condition.Type), condition.Message)
}

func (r *WorkflowRunReconciler) patchStatus(ctx context.Context, wr *v1alpha1.WorkflowRun, phase v1alpha1.WorkflowRunPhase) error {
	wr.Status.Phase = phase
	if err := r.Status().Patch(ctx, wr, client.Merge); err != nil {
		return err
	}
	return nil
}
