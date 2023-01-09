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
	"errors"
	"reflect"
	"sort"

	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	ctrlEvent "sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	monitorContext "github.com/kubevela/pkg/monitor/context"

	"github.com/kubevela/workflow/api/v1alpha1"
	"github.com/kubevela/workflow/pkg/backup"
	"github.com/kubevela/workflow/pkg/types"
)

// BackupReconciler reconciles a WorkflowRun object
type BackupReconciler struct {
	client.Client
	Scheme            *runtime.Scheme
	ControllerVersion string
	BackupArgs
	Args
}

// BackupArgs is the args for backup
type BackupArgs struct {
	Persister      backup.PersistWorkflowRecord
	BackupStrategy string
	IgnoreStrategy string
	GroupByLabel   string
	CleanOnBackup  bool
}

const (
	// StrategyIgnoreLatestFailed is the backup strategy to ignore the latest failed workflowrun
	StrategyIgnoreLatestFailed string = "IgnoreLatestFailedRecord"
	// StrategyBackupFinishedRecord is the backup strategy to backup all finished workflowrun
	StrategyBackupFinishedRecord string = "BackupFinishedRecord"
)

// Reconcile reconciles the WorkflowRun object
// +kubebuilder:rbac:groups=core.oam.dev,resources=workflowruns,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core.oam.dev,resources=workflowruns/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=core.oam.dev,resources=workflowruns/finalizers,verbs=update
func (r *BackupReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	ctx, cancel := context.WithTimeout(ctx, ReconcileTimeout)
	defer cancel()

	ctx = types.SetNamespaceInCtx(ctx, req.Namespace)

	logCtx := monitorContext.NewTraceContext(ctx, "").AddTag("workflowrun", req.String())
	logCtx.Info("Start backup workflow record")
	defer logCtx.Commit("End backup workflow record")
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

	if !run.Status.Finished {
		logCtx.Info("WorkflowRun is not finished, skip reconcile")
		return ctrl.Result{}, nil
	}

	switch r.BackupStrategy {
	case StrategyBackupFinishedRecord:
		if r.IgnoreStrategy == StrategyIgnoreLatestFailed {
			latest, failedList, err := isLatestFailedRecord(ctx, r.Client, run, r.GroupByLabel)
			if err != nil {
				logCtx.Error(err, "failed to list workflowrun record")
				return ctrl.Result{}, err
			}
			if !latest {
				if err := r.backup(logCtx, r.Client, run); err != nil {
					logCtx.Error(err, "failed to backup workflowrun", "workflowrun", run.Name)
					return ctrl.Result{}, err
				}
			}
			if len(failedList) > 1 {
				for _, item := range failedList[1:] {
					if err := r.backup(logCtx, r.Client, &item); err != nil {
						logCtx.Error(err, "failed to backup workflowrun", "workflowrun", run.Name)
						return ctrl.Result{}, err
					}
				}
			}
			break
		}
		if err := r.backup(logCtx, r.Client, run); err != nil {
			logCtx.Error(err, "failed to backup workflowrun", "workflowrun", run.Name)
			return ctrl.Result{}, err
		}
	default:
		err := errors.New("unknown backup strategy")
		logCtx.Error(err, "invalid strategy", "strategy", r.BackupStrategy)
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *BackupReconciler) backup(ctx monitorContext.Context, cli client.Client, run *v1alpha1.WorkflowRun) error {
	if r.Persister != nil {
		if err := r.Persister.Store(ctx, run); err != nil {
			return err
		}
	}
	if r.CleanOnBackup {
		if err := cli.Delete(ctx, run); err != nil && !kerrors.IsNotFound(err) {
			return err
		}
	}
	ctx.Info("Successfully backup workflowrun", "workflowrun", run.Name)
	return nil
}

func (r *BackupReconciler) matchControllerRequirement(wr *v1alpha1.WorkflowRun) bool {
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
func (r *BackupReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		WithOptions(controller.Options{
			MaxConcurrentReconciles: r.ConcurrentReconciles,
		}).
		WithEventFilter(predicate.Funcs{
			// filter the changes in workflow status
			// let workflow handle its reconcile
			UpdateFunc: func(e ctrlEvent.UpdateEvent) bool {
				new := e.ObjectNew.DeepCopyObject().(*v1alpha1.WorkflowRun)
				old := e.ObjectOld.DeepCopyObject().(*v1alpha1.WorkflowRun)
				// if the workflow is not finished, skip the reconcile
				if !new.Status.Finished {
					return false
				}

				return !reflect.DeepEqual(old, new)
			},
			CreateFunc: func(e ctrlEvent.CreateEvent) bool {
				run := e.Object.DeepCopyObject().(*v1alpha1.WorkflowRun)
				return run.Status.Finished
			},
		}).
		For(&v1alpha1.WorkflowRun{}).
		Complete(r)
}

func isLatestFailedRecord(ctx context.Context, cli client.Client, run *v1alpha1.WorkflowRun, groupByLabel string) (bool, []v1alpha1.WorkflowRun, error) {
	if run.Status.Phase != v1alpha1.WorkflowStateFailed {
		return false, nil, nil
	}
	runs := &v1alpha1.WorkflowRunList{}
	listOpt := &client.ListOptions{}
	if groupByLabel != "" && run.Labels != nil && run.Labels[groupByLabel] != "" {
		labels := &metav1.LabelSelector{
			MatchLabels: map[string]string{
				groupByLabel: run.Labels[groupByLabel],
			},
		}
		selector, err := metav1.LabelSelectorAsSelector(labels)
		if err != nil {
			return false, nil, err
		}
		listOpt = &client.ListOptions{LabelSelector: selector}
	}
	if err := cli.List(ctx, runs, listOpt); err != nil {
		return false, nil, err
	}
	sort.Sort(runs)
	failedRecord := make([]v1alpha1.WorkflowRun, 0)
	for _, item := range runs.Items {
		if item.Status.Phase == v1alpha1.WorkflowStateFailed {
			failedRecord = append(failedRecord, item)
		}
	}
	if len(failedRecord) > 0 {
		latest := failedRecord[0]
		return latest.Name == run.Name && latest.Namespace == run.Namespace, failedRecord, nil
	}
	return false, nil, nil
}
