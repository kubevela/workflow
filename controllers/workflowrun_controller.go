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
	k8stypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/apiserver/pkg/util/feature"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	ctrlEvent "sigs.k8s.io/controller-runtime/pkg/event"
	ctrlHandler "sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	triggerv1alpha1 "github.com/kubevela/kube-trigger/api/v1alpha1"
	monitorContext "github.com/kubevela/pkg/monitor/context"

	"github.com/kubevela/workflow/api/condition"
	"github.com/kubevela/workflow/api/v1alpha1"
	wfContext "github.com/kubevela/workflow/pkg/context"
	"github.com/kubevela/workflow/pkg/cue/packages"
	"github.com/kubevela/workflow/pkg/executor"
	"github.com/kubevela/workflow/pkg/features"
	"github.com/kubevela/workflow/pkg/generator"
	"github.com/kubevela/workflow/pkg/monitor/metrics"
	"github.com/kubevela/workflow/pkg/types"
)

// Args args used by controller
type Args struct {
	// ConcurrentReconciles is the concurrent reconcile number of the controller
	ConcurrentReconciles int
	// IgnoreWorkflowWithoutControllerRequirement indicates that workflow controller will not process the workflowrun without 'workflowrun.oam.dev/controller-version-require' annotation.
	IgnoreWorkflowWithoutControllerRequirement bool
	// PackageDiscover discover the packages
	PackageDiscover *packages.PackageDiscover
}

// WorkflowRunReconciler reconciles a WorkflowRun object
type WorkflowRunReconciler struct {
	client.Client
	Scheme            *runtime.Scheme
	Recorder          event.Recorder
	ControllerVersion string
	Args
}

type workflowRunPatcher struct {
	client.Client
	run *v1alpha1.WorkflowRun
}

var (
	// ReconcileTimeout timeout for controller to reconcile
	ReconcileTimeout = time.Minute * 3
)

// Reconcile reconciles the WorkflowRun object
// +kubebuilder:rbac:groups=core.oam.dev,resources=workflowruns,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core.oam.dev,resources=workflowruns/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=core.oam.dev,resources=workflowruns/finalizers,verbs=update
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
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if !r.matchControllerRequirement(run) {
		logCtx.Info("skip workflowrun: not match the controller requirement of workflowrun")
		return ctrl.Result{}, nil
	}

	timeReporter := timeReconcile(run)
	defer timeReporter()

	if run.Status.Finished {
		logCtx.Info("WorkflowRun is finished, skip reconcile")
		return ctrl.Result{}, nil
	}

	instance, err := generator.GenerateWorkflowInstance(ctx, r.Client, run)
	if err != nil {
		logCtx.Error(err, "[generate workflow instance]")
		r.Recorder.Event(run, event.Warning(v1alpha1.ReasonGenerate, errors.WithMessage(err, v1alpha1.MessageFailedGenerate)))
		run.Status.Phase = v1alpha1.WorkflowStateInitializing
		return r.endWithNegativeCondition(logCtx, run, condition.ErrorCondition(v1alpha1.WorkflowRunConditionType, err))
	}
	isUpdate := instance.Status.Message != ""

	runners, err := generator.GenerateRunners(logCtx, instance, types.StepGeneratorOptions{
		PackageDiscover: r.PackageDiscover,
		Client:          r.Client,
	})
	if err != nil {
		logCtx.Error(err, "[generate runners]")
		r.Recorder.Event(run, event.Warning(v1alpha1.ReasonGenerate, errors.WithMessage(err, v1alpha1.MessageFailedGenerate)))
		run.Status.Phase = v1alpha1.WorkflowStateInitializing
		return r.endWithNegativeCondition(logCtx, run, condition.ErrorCondition(v1alpha1.WorkflowRunConditionType, err))
	}

	patcher := &workflowRunPatcher{
		Client: r.Client,
		run:    run,
	}
	executor := executor.New(instance, r.Client, patcher.patchStatus)
	state, err := executor.ExecuteRunners(logCtx, runners)
	if err != nil {
		logCtx.Error(err, "[execute runners]")
		r.Recorder.Event(run, event.Warning(v1alpha1.ReasonExecute, errors.WithMessage(err, v1alpha1.MessageFailedExecute)))
		run.Status.Phase = v1alpha1.WorkflowStateExecuting
		return r.endWithNegativeCondition(logCtx, run, condition.ErrorCondition(v1alpha1.WorkflowRunConditionType, err))
	}
	isUpdate = isUpdate && instance.Status.Message == ""
	run.Status = instance.Status
	run.Status.Phase = state
	switch state {
	case v1alpha1.WorkflowStateSuspending:
		logCtx.Info("Workflow return state=Suspend")
		if duration := executor.GetSuspendBackoffWaitTime(); duration > 0 {
			return ctrl.Result{RequeueAfter: duration}, patcher.patchStatus(logCtx, &run.Status, isUpdate)
		}
		return ctrl.Result{}, patcher.patchStatus(logCtx, &run.Status, isUpdate)
	case v1alpha1.WorkflowStateFailed:
		logCtx.Info("Workflow return state=Failed")
		r.doWorkflowFinish(run)
		r.Recorder.Event(run, event.Normal(v1alpha1.ReasonExecute, v1alpha1.MessageFailed))
		return ctrl.Result{}, patcher.patchStatus(logCtx, &run.Status, isUpdate)
	case v1alpha1.WorkflowStateTerminated:
		logCtx.Info("Workflow return state=Terminated")
		r.doWorkflowFinish(run)
		r.Recorder.Event(run, event.Normal(v1alpha1.ReasonExecute, v1alpha1.MessageTerminated))
		return ctrl.Result{}, patcher.patchStatus(logCtx, &run.Status, isUpdate)
	case v1alpha1.WorkflowStateExecuting:
		logCtx.Info("Workflow return state=Executing")
		return ctrl.Result{RequeueAfter: executor.GetBackoffWaitTime()}, patcher.patchStatus(logCtx, &run.Status, isUpdate)
	case v1alpha1.WorkflowStateSucceeded:
		logCtx.Info("Workflow return state=Succeeded")
		r.doWorkflowFinish(run)
		run.Status.SetConditions(condition.ReadyCondition(v1alpha1.WorkflowRunConditionType))
		r.Recorder.Event(run, event.Normal(v1alpha1.ReasonExecute, v1alpha1.MessageSuccessfully))
		return ctrl.Result{}, patcher.patchStatus(logCtx, &run.Status, isUpdate)
	case v1alpha1.WorkflowStateSkipped:
		logCtx.Info("Skip this reconcile")
		return ctrl.Result{RequeueAfter: executor.GetBackoffWaitTime()}, nil
	}

	return ctrl.Result{}, nil
}

func (r *WorkflowRunReconciler) matchControllerRequirement(wr *v1alpha1.WorkflowRun) bool {
	if wr.Annotations != nil {
		if requireVersion, ok := wr.Annotations[types.AnnotationControllerRequirement]; ok {
			return requireVersion == r.ControllerVersion
		}
	}
	if r.IgnoreWorkflowWithoutControllerRequirement {
		return false
	}
	return true
}

// SetupWithManager sets up the controller with the Manager.
func (r *WorkflowRunReconciler) SetupWithManager(mgr ctrl.Manager) error {
	builder := ctrl.NewControllerManagedBy(mgr)
	if feature.DefaultMutableFeatureGate.Enabled(features.EnableWatchEventListener) {
		builder = builder.Watches(&source.Kind{
			Type: &triggerv1alpha1.EventListener{},
		}, ctrlHandler.EnqueueRequestsFromMapFunc(findObjectForEventListener))
	}
	return builder.
		WithOptions(controller.Options{
			MaxConcurrentReconciles: r.ConcurrentReconciles,
		}).
		WithEventFilter(predicate.Funcs{
			// filter the changes in workflow status
			// let workflow handle its reconcile
			UpdateFunc: func(e ctrlEvent.UpdateEvent) bool {
				new, isNewWR := e.ObjectNew.DeepCopyObject().(*v1alpha1.WorkflowRun)
				old, isOldWR := e.ObjectOld.DeepCopyObject().(*v1alpha1.WorkflowRun)

				// if the object is a event listener, reconcile the controller
				if !isNewWR || !isOldWR {
					return true
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
	if err := r.Status().Patch(ctx, wr, client.Merge); err != nil {
		executor.StepStatusCache.Store(fmt.Sprintf("%s-%s", wr.Name, wr.Namespace), -1)
		return ctrl.Result{}, errors.WithMessage(err, "failed to patch workflowrun status")
	}
	return ctrl.Result{}, fmt.Errorf("reconcile WorkflowRun error, msg: %s", condition.Message)
}

func (r *workflowRunPatcher) patchStatus(ctx context.Context, status *v1alpha1.WorkflowRunStatus, isUpdate bool) error {
	r.run.Status = *status
	wr := r.run
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

func findObjectForEventListener(object client.Object) []reconcile.Request {
	return []reconcile.Request{{
		NamespacedName: k8stypes.NamespacedName{Name: object.GetName(), Namespace: object.GetNamespace()},
	}}
}
