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
	"sync"
	"time"

	"github.com/crossplane/crossplane-runtime/v2/pkg/event"
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

	triggerv1alpha1 "github.com/kubevela/kube-trigger/api/v1alpha1"
	monitorContext "github.com/kubevela/pkg/monitor/context"

	"github.com/kubevela/workflow/api/condition"
	"github.com/kubevela/workflow/api/v1alpha1"
	wfconfig "github.com/kubevela/workflow/pkg/config"
	wfContext "github.com/kubevela/workflow/pkg/context"
	"github.com/kubevela/workflow/pkg/executor"
	"github.com/kubevela/workflow/pkg/expressions"
	"github.com/kubevela/workflow/pkg/features"
	"github.com/kubevela/workflow/pkg/generator"
	"github.com/kubevela/workflow/pkg/monitor/metrics"
	providertypes "github.com/kubevela/workflow/pkg/providers/types"
	"github.com/kubevela/workflow/pkg/types"
	"github.com/kubevela/workflow/pkg/utils"
)

// Args args used by controller
type Args struct {
	// ConcurrentReconciles is the concurrent reconcile number of the controller
	ConcurrentReconciles int
	// IgnoreWorkflowWithoutControllerRequirement indicates that workflow controller will not process the workflowrun without 'workflowrun.oam.dev/controller-version-require' annotation.
	IgnoreWorkflowWithoutControllerRequirement bool
}

// WorkflowRunReconciler reconciles a WorkflowRun object
type WorkflowRunReconciler struct {
	client.Client
	Scheme            *runtime.Scheme
	Recorder          event.Recorder
	ControllerVersion string
	// APIReader reads WorkflowRuns from the API server, past the cache. Without it a reconcile
	// trusts the cache, and a status write that conflicts is not retried.
	APIReader client.Reader
	Args
	// ownWrites holds, per existing WorkflowRun, the resourceVersion of the last copy a reconcile finished
	// with, its own status write included. SetupWithManager creates it; nil turns the check off.
	ownWrites *sync.Map
}

type workflowRunPatcher struct {
	client.Client
	// APIReader reads the live WorkflowRun when a status write conflicts; nil means no retry.
	APIReader client.Reader
	run       *v1alpha1.WorkflowRun
	// written is the status last read or written by this reconcile.
	written *v1alpha1.WorkflowRunStatus
	// userBase and userLive are the statuses either side of another writer's change that a write
	// was merged onto; every later write from this reconcile keeps that change too. nil until then.
	userBase, userLive *v1alpha1.WorkflowRunStatus
	// liveVersion is the resourceVersion of a restarted run a write was refused for. It is kept
	// apart from run, which must go on conflicting, and only feeds the own-write record.
	liveVersion string
}

// ownWrite is the copy of the run to record as this reconcile's last: run, carrying the live
// resourceVersion when a conflict showed another writer's change.
func (r *workflowRunPatcher) ownWrite() *v1alpha1.WorkflowRun {
	if r.liveVersion == "" {
		return r.run
	}
	run := r.run.DeepCopy()
	run.ResourceVersion = r.liveVersion
	return run
}

// newWorkflowRunPatcher writes run's status, starting from the status run holds now.
func newWorkflowRunPatcher(cli client.Client, reader client.Reader, run *v1alpha1.WorkflowRun) *workflowRunPatcher {
	return &workflowRunPatcher{Client: cli, APIReader: reader, run: run, written: run.Status.DeepCopy()}
}

// maxStatusWriteAttempts bounds how often a reconcile writes its status against concurrent writers.
const maxStatusWriteAttempts = 3

// finishedRecordGrace is how long the record of a run's finishing write is kept, for reconciles
// already queued against an older cached copy. The finished run raises no further reconcile.
var finishedRecordGrace = time.Minute

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
	ctx = context.WithValue(ctx, providertypes.ConfigFactoryKey, wfconfig.NewK8sFactory(r.Client))
	ctx = providertypes.WithLabelParams(ctx, map[string]string{
		types.LabelWorkflowRunName:      req.Name,
		types.LabelWorkflowRunNamespace: req.Namespace,
	})

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
		if r.ownWrites != nil {
			r.ownWrites.Delete(req.NamespacedName)
		}
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	fresh, err := r.readOwnWrites(ctx, run)
	if err != nil {
		if !kerrors.IsNotFound(err) {
			logCtx.Error(err, "get workflowrun")
		}
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	cachedVersion := ""
	if fresh == run {
		cachedVersion = run.ResourceVersion
	}
	run = fresh
	patcher := newWorkflowRunPatcher(r.Client, r.APIReader, run)
	defer func() { r.recordOwnWrite(patcher.ownWrite(), cachedVersion) }()

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
		return r.endWithNegativeCondition(logCtx, patcher, condition.ErrorCondition(v1alpha1.WorkflowRunConditionType, err))
	}
	isUpdate := instance.Status.Message != ""

	var stepOptions types.StepGeneratorOptions
	if expressions.EnabledFor(run.Annotations) {
		stepOptions.Expressions = &types.ExpressionOptions{}
	}
	runners, err := generator.GenerateRunners(logCtx, instance, stepOptions)
	if err != nil {
		logCtx.Error(err, "[generate runners]")
		r.Recorder.Event(run, event.Warning(v1alpha1.ReasonGenerate, errors.WithMessage(err, v1alpha1.MessageFailedGenerate)))
		run.Status.Phase = v1alpha1.WorkflowStateInitializing
		return r.endWithNegativeCondition(logCtx, patcher, condition.ErrorCondition(v1alpha1.WorkflowRunConditionType, err))
	}

	executor := executor.New(instance, executor.WithStatusPatcher(patcher.patchStatus))
	state, err := executor.ExecuteRunners(logCtx, runners)
	if err != nil {
		logCtx.Error(err, "[execute runners]")
		r.Recorder.Event(run, event.Warning(v1alpha1.ReasonExecute, errors.WithMessage(err, v1alpha1.MessageFailedExecute)))
		run.Status.Phase = v1alpha1.WorkflowStateExecuting
		return r.endWithNegativeCondition(logCtx, patcher, condition.ErrorCondition(v1alpha1.WorkflowRunConditionType, err))
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
	executor.InitStepStatusCache(context.Background())
	if r.ownWrites == nil {
		r.ownWrites = &sync.Map{}
	}
	builder := ctrl.NewControllerManagedBy(mgr)
	if feature.DefaultMutableFeatureGate.Enabled(features.EnableWatchEventListener) {
		builder = builder.Watches(&triggerv1alpha1.EventListener{}, ctrlHandler.EnqueueRequestsFromMapFunc(findObjectForEventListener))
	}
	return builder.
		WithOptions(controller.Options{
			MaxConcurrentReconciles: r.ConcurrentReconciles,
		}).
		WithEventFilter(predicate.Funcs{
			// filter the changes in workflow status
			// let workflow handle its reconcile
			UpdateFunc: func(e ctrlEvent.UpdateEvent) bool {
				newObj, isNewWR := e.ObjectNew.DeepCopyObject().(*v1alpha1.WorkflowRun)
				oldObj, isOldWR := e.ObjectOld.DeepCopyObject().(*v1alpha1.WorkflowRun)

				// if the object is a event listener, reconcile the controller
				if !isNewWR || !isOldWR {
					return true
				}

				// if the workflow is finished, skip the reconcile
				if newObj.Status.Finished {
					return false
				}

				// filter managedFields changes
				oldObj.ManagedFields = nil
				newObj.ManagedFields = nil

				// filter resourceVersion changes
				oldObj.ResourceVersion = newObj.ResourceVersion

				// if the generation is changed, return true to let the controller handle it
				if oldObj.Generation != newObj.Generation {
					return true
				}

				// ignore the changes in step status
				oldObj.Status.Steps = newObj.Status.Steps

				return !reflect.DeepEqual(oldObj, newObj)
			},
			CreateFunc: func(e ctrlEvent.CreateEvent) bool { //nolint:revive,unused
				return true
			},
		}).
		For(&v1alpha1.WorkflowRun{}).
		Complete(r)
}

func (r *WorkflowRunReconciler) endWithNegativeCondition(ctx context.Context, p *workflowRunPatcher, condition condition.Condition) (ctrl.Result, error) {
	p.run.SetConditions(condition)
	if err := p.writeStatus(ctx, false); err != nil {
		return ctrl.Result{}, errors.WithMessage(err, "failed to patch workflowrun status")
	}
	return ctrl.Result{}, fmt.Errorf("reconcile WorkflowRun error, msg: %s", condition.Message)
}

func (r *workflowRunPatcher) patchStatus(ctx context.Context, status *v1alpha1.WorkflowRunStatus, isUpdate bool) error {
	r.run.Status = *status
	err := r.writeStatus(ctx, isUpdate)
	// The engine goes on from status, so it learns of a terminate or suspend its write was merged onto.
	status.Terminated, status.Suspend = r.run.Status.Terminated, r.run.Status.Suspend
	if err != nil {
		if isUpdate {
			return errors.WithMessage(err, "failed to update workflowrun status")
		}
		return errors.WithMessage(err, "failed to patch workflowrun status")
	}
	return nil
}

// writeStatus writes the run's status. The status records the workflow steps that ran, some with
// side effects, so a write that conflicts with another writer is not dropped: it is merged onto
// the live WorkflowRun, keeping what the user operations set there (terminate, suspend, resume)
// and recording this reconcile's steps, up to maxStatusWriteAttempts writes. A run that was
// restarted, or replaced, since this reconcile last read or wrote it is left for the next
// reconcile, and the conflict returned. A write that fails sets the step guard back to the steps
// last written, so later reconciles are not held back by steps that were never recorded.
func (r *workflowRunPatcher) writeStatus(ctx context.Context, isUpdate bool) error {
	wr := r.run
	if r.userLive != nil {
		wr.Status = mergeUserChanges(wr.Status, *r.userBase, *r.userLive)
	}
	for attempt := 1; ; attempt++ {
		var err error
		if isUpdate {
			err = r.Status().Update(ctx, wr)
		} else {
			err = r.Status().Patch(ctx, wr, client.Merge)
		}
		if err == nil {
			r.written = wr.Status.DeepCopy()
			r.liveVersion = ""
			return nil
		}
		if !kerrors.IsConflict(err) || r.APIReader == nil || r.written == nil || attempt == maxStatusWriteAttempts {
			r.resetStepGuard()
			return err
		}
		live := &v1alpha1.WorkflowRun{}
		if getErr := r.APIReader.Get(ctx, client.ObjectKeyFromObject(wr), live); getErr != nil {
			r.resetStepGuard()
			return err
		}
		if live.UID != wr.UID || restarted(*r.written, live.Status) {
			// The own-write record takes the live resourceVersion, so the next reconcile does not
			// trust a cache that has not seen the restart. run keeps its own, so a later write
			// from this reconcile conflicts as well.
			r.liveVersion = live.ResourceVersion
			r.setStepGuard(len(live.Status.Steps))
			return err
		}
		if !reflect.DeepEqual(live.Status, *r.written) {
			r.userBase, r.userLive = r.written.DeepCopy(), live.Status.DeepCopy()
		}
		status := wr.Status
		if r.userLive != nil {
			status = mergeUserChanges(status, *r.userBase, *r.userLive)
		}
		live.Status = status
		*wr = *live
	}
}

// restarted reports whether live is no longer the run base was read or written from: a restart
// resets the start time, or clears the records of steps and sub-steps from the one it restarts at.
func restarted(base, live v1alpha1.WorkflowRunStatus) bool {
	if !live.StartTime.Equal(&base.StartTime) {
		return true
	}
	kept := stepKeys(live)
	for key := range stepKeys(base) {
		if !kept[key] {
			return true
		}
	}
	return false
}

// stepKeys is the set of step and sub-step records in status.
func stepKeys(status v1alpha1.WorkflowRunStatus) map[string]bool {
	keys := map[string]bool{}
	for _, step := range status.Steps {
		keys[step.ID+"/"+step.Name] = true
		for _, sub := range step.SubStepsStatus {
			keys[step.ID+"/"+step.Name+"/"+sub.ID+"/"+sub.Name] = true
		}
	}
	return keys
}

// mergeUserChanges returns ours with what another writer changed between base and live: the
// terminate and suspend flags where the other writer changed them, and the phase and reason of any
// step or sub-step the user operations moved (terminated, suspended, resumed) that ours has not
// finished. A flag the other writer left alone keeps ours, and a step ours finished keeps ours,
// since it ran. A terminate the other writer set is applied to the merged status as
// utils.TerminateStatus applies it, so the steps ours has not finished stop as the user's do.
func mergeUserChanges(ours, base, live v1alpha1.WorkflowRunStatus) v1alpha1.WorkflowRunStatus {
	merged := *ours.DeepCopy()
	if live.Terminated != base.Terminated {
		merged.Terminated = live.Terminated
	}
	if live.Suspend != base.Suspend {
		merged.Suspend = live.Suspend
	}
	key := func(s v1alpha1.StepStatus) string { return s.ID + "/" + s.Name }
	baseSteps, liveSteps := map[string]v1alpha1.StepStatus{}, map[string]v1alpha1.StepStatus{}
	for _, step := range base.Steps {
		baseSteps[key(step.StepStatus)] = step.StepStatus
		for _, sub := range step.SubStepsStatus {
			baseSteps[key(sub)] = sub
		}
	}
	for _, step := range live.Steps {
		liveSteps[key(step.StepStatus)] = step.StepStatus
		for _, sub := range step.SubStepsStatus {
			liveSteps[key(sub)] = sub
		}
	}
	keep := func(s *v1alpha1.StepStatus) {
		if types.IsStepFinish(s.Phase, s.Reason) {
			return
		}
		l, inLive := liveSteps[key(*s)]
		b := baseSteps[key(*s)]
		if inLive && (l.Phase != b.Phase || l.Reason != b.Reason) {
			s.Phase, s.Reason = l.Phase, l.Reason
		}
	}
	for i := range merged.Steps {
		keep(&merged.Steps[i].StepStatus)
		for j := range merged.Steps[i].SubStepsStatus {
			keep(&merged.Steps[i].SubStepsStatus[j])
		}
	}
	if live.Terminated && !base.Terminated {
		utils.TerminateStatus(&merged)
	}
	return merged
}

// resetStepGuard sets the step guard to the steps the stored status records.
func (r *workflowRunPatcher) resetStepGuard() {
	if r.written != nil {
		r.setStepGuard(len(r.written.Steps))
	}
}

// setStepGuard sets the step guard to steps.
func (r *workflowRunPatcher) setStepGuard(steps int) {
	if executor.StepStatusCache == nil {
		return
	}
	executor.StepStatusCache.Put(fmt.Sprintf("%s-%s", r.run.Name, r.run.Namespace), steps, ReconcileTimeout)
}

// recordOwnWrite remembers the copy of run a reconcile finished with. Patch and Update refresh
// run's resourceVersion in place, so after a status write it is the write's. cachedVersion is
// the resourceVersion of the cached copy the reconcile started from, or empty when it read the
// live run. A reconcile that started from a cached copy and wrote nothing forgets the run: the
// cache has caught up, and it does not go back. A finished run raises no further reconcile, so its
// record is dropped after finishedRecordGrace.
func (r *WorkflowRunReconciler) recordOwnWrite(run *v1alpha1.WorkflowRun, cachedVersion string) {
	if r.ownWrites == nil || run.ResourceVersion == "" {
		return
	}
	key := client.ObjectKeyFromObject(run)
	if run.ResourceVersion == cachedVersion {
		r.ownWrites.Delete(key)
		return
	}
	version := run.ResourceVersion
	r.ownWrites.Store(key, version)
	if run.Status.Finished {
		ownWrites := r.ownWrites
		time.AfterFunc(finishedRecordGrace, func() { ownWrites.CompareAndDelete(key, version) })
	}
}

// readOwnWrites returns run, or the live WorkflowRun when the cache has not yet caught up with the
// copy the last reconcile finished with. A reconcile from an older copy would miss the steps that
// reconcile recorded, and run them again.
func (r *WorkflowRunReconciler) readOwnWrites(ctx context.Context, run *v1alpha1.WorkflowRun) (*v1alpha1.WorkflowRun, error) {
	if r.APIReader == nil || r.ownWrites == nil {
		return run, nil
	}
	key := client.ObjectKeyFromObject(run)
	last, ok := r.ownWrites.Load(key)
	if !ok || last == run.ResourceVersion {
		return run, nil
	}
	live := &v1alpha1.WorkflowRun{}
	if err := r.APIReader.Get(ctx, key, live); err != nil {
		if kerrors.IsNotFound(err) {
			r.ownWrites.Delete(key)
		}
		return nil, err
	}
	return live, nil
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

func findObjectForEventListener(_ context.Context, object client.Object) []reconcile.Request {
	return []reconcile.Request{{
		NamespacedName: k8stypes.NamespacedName{Name: object.GetName(), Namespace: object.GetNamespace()},
	}}
}
