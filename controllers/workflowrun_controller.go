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
	"reflect"
	"time"

	"github.com/crossplane/crossplane-runtime/pkg/event"
	"github.com/pkg/errors"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	ctrlEvent "sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	"github.com/kubevela/workflow/api/condition"
	"github.com/kubevela/workflow/api/v1alpha1"
	wfContext "github.com/kubevela/workflow/pkg/context"
	"github.com/kubevela/workflow/pkg/cue/packages"
	"github.com/kubevela/workflow/pkg/executor"
	monitorContext "github.com/kubevela/workflow/pkg/monitor/context"
	"github.com/kubevela/workflow/pkg/monitor/metrics"
	"github.com/kubevela/workflow/pkg/steps"
	"github.com/kubevela/workflow/pkg/types"
)

// Args args used by controller
type Args struct {
	// ConcurrentReconciles is the concurrent reconcile number of the controller
	ConcurrentReconciles int
}

// WorkflowRunReconciler reconciles a WorkflowRun object
type WorkflowRunReconciler struct {
	client.Client
	Scheme          *runtime.Scheme
	PackageDiscover *packages.PackageDiscover
	Recorder        event.Recorder
	Args
}

var (
	// ReconcileTimeout timeout for controller to reconcile
	ReconcileTimeout = time.Minute * 3
)

// Reconcile reconciles the WorkflowRun object
//+kubebuilder:rbac:groups=core.oam.dev,resources=workflowruns,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=core.oam.dev,resources=workflowruns/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=core.oam.dev,resources=workflowruns/finalizers,verbs=update
func (r *WorkflowRunReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	ctx, cancel := context.WithTimeout(ctx, ReconcileTimeout)
	defer cancel()

	ctx = types.SetNamespaceInCtx(ctx, req.Namespace)

	logCtx := monitorContext.NewTraceContext(ctx, "").AddTag("workflowrun", req.String())
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

	timeReporter := timeReconcile(run)
	defer timeReporter()

	if run.Status.Finished {
		logCtx.Info("WorkflowRun is finished, skip reconcile")
		return ctrl.Result{}, nil
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

	executor.InitializeWorkflowRun(run)
	isUpdate := run.Status.Message != ""

	runners, err := steps.Generate(logCtx, run, types.StepGeneratorOptions{
		PackageDiscover: r.PackageDiscover,
		Client:          r.Client,
	})
	if err != nil {
		logCtx.Error(err, "[generate runners]")
		r.Recorder.Event(run, event.Warning(v1alpha1.ReasonGenerate, errors.WithMessage(err, v1alpha1.MessageFailedGenerate)))
		run.Status.Phase = v1alpha1.WorkflowStateInitializing
		return r.endWithNegativeCondition(logCtx, run, condition.ErrorCondition(v1alpha1.WorkflowRunConditionType, err))
	}

	executor := executor.New(run, r.Client)
	state, err := executor.ExecuteRunners(logCtx, runners)
	if err != nil {
		logCtx.Error(err, "[execute runners]")
		r.Recorder.Event(run, event.Warning(v1alpha1.ReasonExecute, errors.WithMessage(err, v1alpha1.MessageFailedExecute)))
		run.Status.Phase = v1alpha1.WorkflowStateExecuting
		return r.endWithNegativeCondition(logCtx, run, condition.ErrorCondition(v1alpha1.WorkflowRunConditionType, err))
	}
	isUpdate = isUpdate && run.Status.Message == ""
	run.Status.Phase = state
	switch state {
	case v1alpha1.WorkflowStateSuspending:
		logCtx.Info("Workflow return state=Suspend")
		if duration := executor.GetSuspendBackoffWaitTime(); duration > 0 {
			return ctrl.Result{RequeueAfter: duration}, r.patchStatus(logCtx, run, isUpdate)
		}
		return ctrl.Result{}, r.patchStatus(logCtx, run, isUpdate)
	case v1alpha1.WorkflowStateTerminated:
		logCtx.Info("Workflow return state=Terminated")
		r.doWorkflowFinish(run)
		r.Recorder.Event(run, event.Normal(v1alpha1.ReasonExecute, v1alpha1.MessageTerminated))
		return ctrl.Result{}, r.patchStatus(logCtx, run, isUpdate)
	case v1alpha1.WorkflowStateExecuting:
		logCtx.Info("Workflow return state=Executing")
		return ctrl.Result{RequeueAfter: executor.GetBackoffWaitTime()}, r.patchStatus(logCtx, run, isUpdate)
	case v1alpha1.WorkflowStateSucceeded:
		logCtx.Info("Workflow return state=Succeeded")
		r.doWorkflowFinish(run)
		run.Status.SetConditions(condition.ReadyCondition(v1alpha1.WorkflowRunConditionType))
		r.Recorder.Event(run, event.Normal(v1alpha1.ReasonExecute, v1alpha1.MessageSuccessfully))
		return ctrl.Result{}, r.patchStatus(logCtx, run, isUpdate)
	case v1alpha1.WorkflowStateSkipped:
		logCtx.Info("Skip this reconcile")
		return ctrl.Result{}, nil
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *WorkflowRunReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		WithOptions(controller.Options{
			MaxConcurrentReconciles: r.ConcurrentReconciles,
		}).
		WithEventFilter(predicate.Funcs{
			// filter the changes in workflow status
			// let workflow handle its reconcile
			UpdateFunc: func(e ctrlEvent.UpdateEvent) bool {
				new, isNewRun := e.ObjectNew.DeepCopyObject().(*v1alpha1.WorkflowRun)
				old, isOldRun := e.ObjectOld.DeepCopyObject().(*v1alpha1.WorkflowRun)
				if !isNewRun || !isOldRun {
					return false
				}

				// if the workflow is finished, skip the reconcile
				if new.Status.Finished {
					return false
				}

				// filter managedFields changes
				old.ManagedFields = nil
				new.ManagedFields = nil

				// filter resourceVersion changes
				old.ResourceVersion = new.ResourceVersion

				// if the generation is changed, return true to let the controller handle it
				if old.Generation != new.Generation {
					return true
				}

				// ignore the changes in step status
				old.Status.Steps = new.Status.Steps

				return !reflect.DeepEqual(old, new)
			},
			CreateFunc: func(e ctrlEvent.CreateEvent) bool {
				return true
			},
		}).
		For(&v1alpha1.WorkflowRun{}).
		Complete(r)
}

func (r *WorkflowRunReconciler) endWithNegativeCondition(ctx context.Context, wr *v1alpha1.WorkflowRun, condition condition.Condition) (ctrl.Result, error) {
	wr.SetConditions(condition)
	if err := r.patchStatus(ctx, wr, false); err != nil {
		return ctrl.Result{}, errors.WithMessage(err, "failed to patch workflowrun status")
	}
	return ctrl.Result{}, fmt.Errorf("reconcile WorkflowRun error, msg: %s", condition.Message)
}

func (r *WorkflowRunReconciler) patchStatus(ctx context.Context, wr *v1alpha1.WorkflowRun, isUpdate bool) error {
	if isUpdate {
		if err := r.Status().Update(ctx, wr); err != nil {
			executor.StepStatusCache.Store(fmt.Sprintf("%s-%s", wr.Name, wr.Namespace), -1)
			return errors.WithMessage(err, "failed to update workflowrun status")
		}
		return nil
	}
	if err := r.Status().Patch(ctx, wr, client.Merge); err != nil {
		executor.StepStatusCache.Store(fmt.Sprintf("%s-%s", wr.Name, wr.Namespace), -1)
		return errors.WithMessage(err, "failed to patch workflowrun status")
	}
	return nil
}

func (r *WorkflowRunReconciler) doWorkflowFinish(wr *v1alpha1.WorkflowRun) {
	wr.Status.Finished = true
	wr.Status.EndTime = metav1.Now()
	metrics.WorkflowRunFinishedTimeHistogram.WithLabelValues(string(wr.Status.Phase)).Observe(wr.Status.EndTime.Sub(wr.Status.StartTime.Time).Seconds())
	executor.StepStatusCache.Delete(fmt.Sprintf("%s-%s", wr.Name, wr.Namespace))
	wfContext.CleanupMemoryStore(wr.Name, wr.Namespace)
}

func timeReconcile(wr *v1alpha1.WorkflowRun) func() {
	t := time.Now()
	beginPhase := string(wr.Status.Phase)
	return func() {
		v := time.Since(t).Seconds()
		metrics.WorkflowRunReconcileTimeHistogram.WithLabelValues(beginPhase, string(wr.Status.Phase)).Observe(v)
	}
}
