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
	"context"
	"fmt"
	"math"
	"sync"
	"time"

	"cuelang.org/go/cue"
	"github.com/pkg/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apiserver/pkg/endpoints/request"
	"k8s.io/apiserver/pkg/util/feature"

	monitorContext "github.com/kubevela/pkg/monitor/context"

	"github.com/kubevela/workflow/api/v1alpha1"
	wfContext "github.com/kubevela/workflow/pkg/context"
	"github.com/kubevela/workflow/pkg/debug"
	"github.com/kubevela/workflow/pkg/features"
	"github.com/kubevela/workflow/pkg/hooks"
	"github.com/kubevela/workflow/pkg/monitor/metrics"
	"github.com/kubevela/workflow/pkg/providers/legacy/workspace"
	"github.com/kubevela/workflow/pkg/tasks/custom"
	"github.com/kubevela/workflow/pkg/types"
)

var (
	// DisableRecorder optimize workflow by disable recorder
	DisableRecorder = false
	// StepStatusCache cache the step status
	StepStatusCache sync.Map
)

const (
	// minWorkflowBackoffWaitTime is the min time to wait before reconcile workflow again
	minWorkflowBackoffWaitTime = 1
	// backoffTimeCoefficient is the coefficient of time to wait before reconcile workflow again
	backoffTimeCoefficient = 0.05
)

type workflowExecutor struct {
	instance *types.WorkflowInstance
	wfCtx    wfContext.Context
	patcher  types.StatusPatcher
}

// New returns a Workflow Executor implementation.
func New(instance *types.WorkflowInstance, options ...Option) WorkflowExecutor {
	executor := &workflowExecutor{instance: instance}
	for _, opt := range options {
		opt.ApplyTo(executor)
	}
	return executor
}

// InitializeWorkflowInstance init workflow instance
func InitializeWorkflowInstance(instance *types.WorkflowInstance) {
	if instance.Status.StartTime.IsZero() && len(instance.Status.Steps) == 0 {
		metrics.WorkflowRunInitializedCounter.WithLabelValues().Inc()
		mode := v1alpha1.WorkflowExecuteMode{
			Steps:    v1alpha1.WorkflowModeStep,
			SubSteps: v1alpha1.WorkflowModeDAG,
		}
		if instance.Mode != nil {
			if instance.Mode.Steps != "" {
				mode.Steps = instance.Mode.Steps
			}
			if instance.Mode.SubSteps != "" {
				mode.SubSteps = instance.Mode.SubSteps
			}
		}
		instance.Status = v1alpha1.WorkflowRunStatus{
			Mode:      mode,
			StartTime: metav1.Now(),
		}
		StepStatusCache.Delete(fmt.Sprintf("%s-%s", instance.Name, instance.Namespace))
		wfContext.CleanupMemoryStore(instance.Name, instance.Namespace)
	}
}

// ExecuteRunners execute workflow task runners in order.
func (w *workflowExecutor) ExecuteRunners(ctx monitorContext.Context, taskRunners []types.TaskRunner) (v1alpha1.WorkflowRunPhase, error) {
	InitializeWorkflowInstance(w.instance)
	status := &w.instance.Status
	dagMode := status.Mode.Steps == v1alpha1.WorkflowModeDAG
	cacheKey := fmt.Sprintf("%s-%s", w.instance.Name, w.instance.Namespace)

	allRunnersDone, allRunnersSucceeded := checkRunners(taskRunners, w.instance.Status)
	if status.Finished {
		StepStatusCache.Delete(cacheKey)
	}
	if checkWorkflowTerminated(status, allRunnersDone) {
		if isTerminatedManually(status) {
			return v1alpha1.WorkflowStateTerminated, nil
		}
		return v1alpha1.WorkflowStateFailed, nil
	}

	wfCtx, err := w.makeContext(ctx, w.instance.Name)
	if err != nil {
		ctx.Error(err, "make context")
		return v1alpha1.WorkflowStateExecuting, err
	}
	w.wfCtx = wfCtx

	if checkWorkflowSuspended(status) {
		return v1alpha1.WorkflowStateSuspending, nil
	}
	if allRunnersSucceeded {
		return v1alpha1.WorkflowStateSucceeded, nil
	}

	if cacheValue, ok := StepStatusCache.Load(cacheKey); ok {
		// handle cache resource
		if len(status.Steps) < cacheValue.(int) {
			return v1alpha1.WorkflowStateSkipped, nil
		}
	}

	e := newEngine(ctx, wfCtx, w, status, taskRunners)

	err = e.Run(ctx, taskRunners, dagMode)
	if err != nil {
		ctx.Error(err, "run steps")
		StepStatusCache.Store(cacheKey, len(status.Steps))
		return v1alpha1.WorkflowStateExecuting, err
	}

	StepStatusCache.Store(cacheKey, len(status.Steps))
	if feature.DefaultMutableFeatureGate.Enabled(features.EnablePatchStatusAtOnce) {
		return e.status.Phase, nil
	}
	return e.checkWorkflowPhase(), nil
}

func isTerminatedManually(status *v1alpha1.WorkflowRunStatus) bool {
	manually := false
	for _, step := range status.Steps {
		if step.Phase == v1alpha1.WorkflowStepPhaseFailed {
			if step.Reason == types.StatusReasonTerminate {
				manually = true
			} else {
				return false
			}
		}
	}
	return manually
}

func checkWorkflowTerminated(status *v1alpha1.WorkflowRunStatus, allTasksDone bool) bool {
	// if all tasks are done, and the terminated is true, then the workflow is terminated
	return status.Terminated && allTasksDone
}

func checkWorkflowSuspended(status *v1alpha1.WorkflowRunStatus) bool {
	// if workflow is suspended and the suspended step is still running, return false to run the suspended step
	if status.Suspend {
		for _, step := range status.Steps {
			if step.Reason == types.StatusReasonSuspend && step.Phase == v1alpha1.WorkflowStepPhaseSuspending {
				return false
			}
			for _, sub := range step.SubStepsStatus {
				if sub.Reason == types.StatusReasonSuspend && sub.Phase == v1alpha1.WorkflowStepPhaseSuspending {
					return false
				}
			}
		}
	}
	return status.Suspend
}

func newEngine(ctx monitorContext.Context, wfCtx wfContext.Context, w *workflowExecutor, wfStatus *v1alpha1.WorkflowRunStatus, taskRunners []types.TaskRunner) *engine { //nolint:revive,unused
	stepStatus := make(map[string]v1alpha1.StepStatus)
	setStepStatus(stepStatus, wfStatus.Steps)
	stepDependsOn := make(map[string][]string)
	for _, step := range w.instance.Steps {
		hooks.SetAdditionalNameInStatus(stepStatus, step.Name, step.Properties, stepStatus[step.Name])
		stepDependsOn[step.Name] = append(stepDependsOn[step.Name], step.DependsOn...)
		for _, sub := range step.SubSteps {
			hooks.SetAdditionalNameInStatus(stepStatus, step.Name, step.Properties, stepStatus[step.Name])
			stepDependsOn[sub.Name] = append(stepDependsOn[sub.Name], sub.DependsOn...)
		}
	}
	return &engine{
		status:        wfStatus,
		instance:      w.instance,
		wfCtx:         wfCtx,
		debug:         w.instance.Debug,
		stepStatus:    stepStatus,
		stepDependsOn: stepDependsOn,
		stepTimeout:   make(map[string]time.Time),
		taskRunners:   taskRunners,
		statusPatcher: w.patcher,
	}
}

func setStepStatus(statusMap map[string]v1alpha1.StepStatus, status []v1alpha1.WorkflowStepStatus) {
	for _, ss := range status {
		statusMap[ss.Name] = ss.StepStatus
		for _, sss := range ss.SubStepsStatus {
			statusMap[sss.Name] = sss
		}
	}
}

func (w *workflowExecutor) GetSuspendBackoffWaitTime() time.Duration {
	if len(w.instance.Steps) == 0 {
		return 0
	}
	stepStatus := make(map[string]v1alpha1.StepStatus)
	setStepStatus(stepStatus, w.instance.Status.Steps)
	maxTime := time.Duration(1<<63 - 1)
	minTime := maxTime
	for _, step := range w.instance.Steps {
		minTime = handleSuspendBackoffTime(w.wfCtx, step, stepStatus[step.Name], minTime)
		for _, sub := range step.SubSteps {
			minTime = handleSuspendBackoffTime(w.wfCtx, v1alpha1.WorkflowStep{
				WorkflowStepBase: v1alpha1.WorkflowStepBase{
					Name:       sub.Name,
					Type:       sub.Type,
					Timeout:    sub.Timeout,
					Properties: sub.Properties,
				},
			}, stepStatus[sub.Name], minTime)
		}
	}
	if minTime == maxTime {
		return 0
	}
	return minTime
}

func handleSuspendBackoffTime(wfCtx wfContext.Context, step v1alpha1.WorkflowStep, status v1alpha1.StepStatus, minTime time.Duration) time.Duration {
	if status.Phase != v1alpha1.WorkflowStepPhaseSuspending {
		return minTime
	}
	if step.Timeout != "" {
		duration, err := time.ParseDuration(step.Timeout)
		if err != nil {
			return minTime
		}
		timeout := status.FirstExecuteTime.Add(duration)
		if time.Now().Before(timeout) {
			d := time.Until(timeout)
			if duration < minTime {
				minTime = d
			}
		}
	}

	if ts := wfCtx.GetMutableValue(status.ID, workspace.ResumeTimeStamp); ts != "" {
		t, err := time.Parse(time.RFC3339, ts)
		if err != nil {
			return minTime
		}
		d := time.Until(t)
		if d < minTime {
			minTime = d
		}
	}
	return minTime
}

func (w *workflowExecutor) GetBackoffWaitTime() time.Duration {
	nextTime, ok := w.wfCtx.GetValueInMemory(types.ContextKeyNextExecuteTime)
	if !ok {
		if w.instance.Status.Suspend {
			return 0
		}
		return time.Second
	}
	unix, ok := nextTime.(int64)
	if !ok {
		return time.Second
	}
	next := time.Unix(unix, 0)
	if next.After(time.Now()) {
		return time.Until(next)
	}

	return time.Second
}

func checkRunners(taskRunners []types.TaskRunner, status v1alpha1.WorkflowRunStatus) (bool, bool) {
	success := true
	for _, t := range taskRunners {
		done := false
		for _, ss := range status.Steps {
			if ss.Name == t.Name() {
				done = types.IsStepFinish(ss.Phase, ss.Reason)
				success = success && done && (ss.Phase == v1alpha1.WorkflowStepPhaseSucceeded || ss.Phase == v1alpha1.WorkflowStepPhaseSkipped)
				break
			}
		}
		if !done {
			return false, false
		}
	}
	return true, success
}

func (w *workflowExecutor) makeContext(ctx context.Context, name string) (wfContext.Context, error) {
	// clear the user info in context
	ctx = request.WithUser(ctx, nil)
	status := &w.instance.Status
	if status.ContextBackend != nil {
		wfCtx, err := wfContext.LoadContext(ctx, w.instance.Namespace, w.instance.Name, w.instance.Status.ContextBackend.Name)
		if err != nil {
			return nil, errors.WithMessage(err, "load context")
		}
		return wfCtx, nil
	}

	wfCtx, err := wfContext.NewContext(ctx, w.instance.Namespace, name, w.instance.ChildOwnerReferences)
	if err != nil {
		return nil, errors.WithMessage(err, "new context")
	}

	status.ContextBackend = wfCtx.StoreRef()
	return wfCtx, nil
}

func (e *engine) getBackoffTimes(stepID string) int {
	if v, ok := e.wfCtx.GetValueInMemory(types.ContextPrefixBackoffTimes, stepID); ok {
		times, ok := v.(int)
		if ok {
			return times
		}
	}
	return -1
}

func (e *engine) getBackoffWaitTime() int {
	// the default value of min times reaches the max workflow backoff wait time
	minTimes := 15
	found := false
	for _, step := range e.status.Steps {
		if backoffTimes := e.getBackoffTimes(step.ID); backoffTimes > 0 {
			found = true
			if backoffTimes < minTimes {
				minTimes = backoffTimes
			}
		}
		if step.SubStepsStatus != nil {
			for _, subStep := range step.SubStepsStatus {
				if backoffTimes := e.getBackoffTimes(subStep.ID); backoffTimes > 0 {
					found = true
					if backoffTimes < minTimes {
						minTimes = backoffTimes
					}
				}
			}
		}
	}

	if !found {
		return minWorkflowBackoffWaitTime
	}

	interval := int(math.Pow(2, float64(minTimes)) * backoffTimeCoefficient)
	if interval < minWorkflowBackoffWaitTime {
		return minWorkflowBackoffWaitTime
	}
	maxWorkflowBackoffWaitTime := e.getMaxBackoffWaitTime()
	if interval > maxWorkflowBackoffWaitTime {
		return maxWorkflowBackoffWaitTime
	}
	return interval
}

func (e *engine) getMaxBackoffWaitTime() int {
	for _, step := range e.status.Steps {
		if step.Phase == v1alpha1.WorkflowStepPhaseFailed {
			return types.MaxWorkflowFailedBackoffTime
		}
	}
	return types.MaxWorkflowWaitBackoffTime
}

func (e *engine) getNextTimeout() int64 {
	maxTime := time.Duration(1<<63 - 1)
	minTime := time.Duration(1<<63 - 1)
	now := time.Now()
	for _, step := range e.status.Steps {
		if step.Phase == v1alpha1.WorkflowStepPhaseRunning {
			if timeout, ok := e.stepTimeout[step.Name]; ok {
				duration := timeout.Sub(now)
				if duration < minTime {
					minTime = duration
				}
			}
		}
	}
	if minTime == maxTime {
		return -1
	}
	if minTime.Seconds() < 1 {
		return minWorkflowBackoffWaitTime
	}
	return int64(math.Ceil(minTime.Seconds()))
}

func (e *engine) setNextExecuteTime(ctx monitorContext.Context) {
	backoff := e.getBackoffWaitTime()
	lastExecuteTime, ok := e.wfCtx.GetValueInMemory(types.ContextKeyLastExecuteTime)
	if !ok {
		ctx.Error(fmt.Errorf("failed to get last execute time"), "workflow run", e.instance.Name)
	}

	last, ok := lastExecuteTime.(int64)
	if !ok {
		ctx.Error(fmt.Errorf("failed to parse last execute time to int64"), "lastExecuteTime", lastExecuteTime)
	}
	interval := int64(backoff)
	if timeout := e.getNextTimeout(); timeout > 0 && timeout < interval {
		interval = timeout
	}

	next := last + interval
	e.wfCtx.SetValueInMemory(next, types.ContextKeyNextExecuteTime)
}

func (e *engine) runAsDAG(ctx monitorContext.Context, taskRunners []types.TaskRunner, pendingRunners bool) error {
	var (
		todoTasks    []types.TaskRunner
		pendingTasks []types.TaskRunner
	)
	wfCtx := e.wfCtx
	done := true
	for _, tRunner := range taskRunners {
		finish := false
		var stepID string
		if status, ok := e.stepStatus[tRunner.Name()]; ok {
			stepID = status.ID
			finish = types.IsStepFinish(status.Phase, status.Reason)
		}
		if !finish {
			done = false
			if pending, status := tRunner.Pending(ctx, wfCtx, e.stepStatus); pending {
				if pendingRunners {
					wfCtx.IncreaseCountValueInMemory(types.ContextPrefixBackoffTimes, status.ID)
					if err := e.updateStepStatus(ctx, status); err != nil {
						return err
					}
				}
				pendingTasks = append(pendingTasks, tRunner)
				continue
			} else if status.Phase == v1alpha1.WorkflowStepPhasePending {
				wfCtx.DeleteValueInMemory(types.ContextPrefixBackoffTimes, stepID)
			}
			todoTasks = append(todoTasks, tRunner)
		} else {
			wfCtx.DeleteValueInMemory(types.ContextPrefixBackoffTimes, stepID)
		}
	}
	if done {
		return nil
	}

	if len(todoTasks) > 0 {
		err := e.steps(ctx, todoTasks, true)
		if err != nil {
			return err
		}

		if e.needStop() {
			return nil
		}

		if len(pendingTasks) > 0 {
			return e.runAsDAG(ctx, pendingTasks, true)
		}
	}
	return nil

}

func (e *engine) Run(ctx monitorContext.Context, taskRunners []types.TaskRunner, dag bool) error {
	var err error
	if dag {
		err = e.runAsDAG(ctx, taskRunners, false)
	} else {
		err = e.steps(ctx, taskRunners, dag)
	}

	e.checkFailedAfterRetries()
	e.setNextExecuteTime(ctx)
	return err
}

func (e *engine) checkWorkflowStatusMessage() {
	switch {
	case !e.waiting && e.failedAfterRetries && feature.DefaultMutableFeatureGate.Enabled(features.EnableSuspendOnFailure):
		e.status.Message = types.MessageSuspendFailedAfterRetries
	default:
		e.status.Message = ""
	}
}

func (e *engine) steps(ctx monitorContext.Context, taskRunners []types.TaskRunner, dag bool) error {
	wfCtx := e.wfCtx
	for index, runner := range taskRunners {
		if status, ok := e.stepStatus[runner.Name()]; ok {
			if types.IsStepFinish(status.Phase, status.Reason) {
				continue
			}
		}
		if pending, status := runner.Pending(ctx, wfCtx, e.stepStatus); pending {
			wfCtx.IncreaseCountValueInMemory(types.ContextPrefixBackoffTimes, status.ID)
			if err := e.updateStepStatus(ctx, status); err != nil {
				return err
			}
			if dag {
				continue
			}
			return nil
		}
		options := e.generateRunOptions(ctx, e.findDependPhase(taskRunners, index, dag))

		status, operation, err := runner.Run(wfCtx, options)
		if err != nil {
			return err
		}
		e.finishStep(operation)

		// for the suspend step with duration, there's no need to increase the backoff time in reconcile when it's still running
		if !types.IsStepFinish(status.Phase, status.Reason) && status.Phase != v1alpha1.WorkflowStepPhaseSuspending {
			if err := e.updateStepStatus(ctx, status); err != nil {
				return err
			}
			if err := handleBackoffTimes(ctx, wfCtx, status, false); err != nil {
				return err
			}
			if dag {
				continue
			}
			return nil
		}
		// clear the backoff time when the step is finished
		if err := handleBackoffTimes(ctx, wfCtx, status, true); err != nil {
			return err
		}
		if err := e.updateStepStatus(ctx, status); err != nil {
			return err
		}

		if dag {
			continue
		}
		if e.needStop() {
			return nil
		}
	}
	return nil
}

func (e *engine) generateRunOptions(ctx monitorContext.Context, dependsOnPhase v1alpha1.WorkflowStepPhase) *types.TaskRunOptions {
	options := &types.TaskRunOptions{
		GetTracer: func(id string, stepStatus v1alpha1.WorkflowStep) monitorContext.Context {
			return ctx.Fork(id, monitorContext.DurationMetric(func(v float64) {
				metrics.WorkflowRunStepDurationHistogram.WithLabelValues("workflowrun", stepStatus.Type).Observe(v)
			}))
		},
		StepStatus: e.stepStatus,
		Engine:     e,
		PreCheckHooks: []types.TaskPreCheckHook{
			func(step v1alpha1.WorkflowStep, options *types.PreCheckOptions) (*types.PreCheckResult, error) {
				if feature.DefaultMutableFeatureGate.Enabled(features.EnableSuspendOnFailure) {
					return &types.PreCheckResult{Skip: false}, nil
				}
				if e.parentRunner != "" {
					if status, ok := e.stepStatus[e.parentRunner]; ok && status.Phase == v1alpha1.WorkflowStepPhaseSkipped {
						return &types.PreCheckResult{Skip: true}, nil
					}
				}
				switch step.If {
				case "always":
					return &types.PreCheckResult{Skip: false}, nil
				case "":
					return &types.PreCheckResult{Skip: skipExecutionOfNextStep(dependsOnPhase, len(step.DependsOn) > 0)}, nil
				default:
					basicVal := cue.Value{}
					if options != nil {
						basicVal = options.BasicValue
					}
					ifValue, err := custom.ValidateIfValue(e.wfCtx, step, e.stepStatus, basicVal)
					if err != nil {
						return &types.PreCheckResult{Skip: true}, err
					}
					return &types.PreCheckResult{Skip: !ifValue}, nil
				}
			},
			func(step v1alpha1.WorkflowStep, options *types.PreCheckOptions) (*types.PreCheckResult, error) { //nolint:revive,unused
				status := e.stepStatus[step.Name]
				if e.parentRunner != "" {
					if status, ok := e.stepStatus[e.parentRunner]; ok && status.Phase == v1alpha1.WorkflowStepPhaseFailed && status.Reason == types.StatusReasonTimeout {
						return &types.PreCheckResult{Timeout: true}, nil
					}
				}
				if !status.FirstExecuteTime.Time.IsZero() && step.Timeout != "" {
					duration, err := time.ParseDuration(step.Timeout)
					if err != nil {
						// if the timeout is a invalid duration, return {timeout: false}
						return &types.PreCheckResult{Timeout: false}, err
					}
					timeout := status.FirstExecuteTime.Add(duration)
					e.stepTimeout[step.Name] = timeout
					if time.Now().After(timeout) {
						return &types.PreCheckResult{Timeout: true}, nil
					}
				}
				return &types.PreCheckResult{Timeout: false}, nil
			},
		},
		PreStartHooks: []types.TaskPreStartHook{hooks.Input},
		PostStopHooks: []types.TaskPostStopHook{hooks.Output},
	}
	if e.debug {
		options.Debug = func(id string, v cue.Value) error {
			debugContext := debug.NewContext(e.instance, id)
			return debugContext.Set(v)
		}
	}
	return options
}

type engine struct {
	failedAfterRetries bool
	waiting            bool
	suspending         bool
	debug              bool
	status             *v1alpha1.WorkflowRunStatus
	wfCtx              wfContext.Context
	instance           *types.WorkflowInstance
	parentRunner       string
	stepStatus         map[string]v1alpha1.StepStatus
	stepTimeout        map[string]time.Time
	stepDependsOn      map[string][]string
	taskRunners        []types.TaskRunner
	statusPatcher      types.StatusPatcher
}

func (e *engine) finishStep(operation *types.Operation) {
	if operation != nil {
		e.status.Terminated = e.status.Terminated || operation.Terminated
		e.failedAfterRetries = e.failedAfterRetries || operation.FailedAfterRetries
		e.waiting = e.waiting || operation.Waiting
		e.suspending = e.suspending || operation.Suspend
	}
	e.status.Suspend = e.suspending
	if !e.waiting && e.failedAfterRetries && feature.DefaultMutableFeatureGate.Enabled(features.EnableSuspendOnFailure) {
		e.status.Suspend = true
	}
	if e.failedAfterRetries && !feature.DefaultMutableFeatureGate.Enabled(features.EnableSuspendOnFailure) {
		e.status.Terminated = true
	}
}

func (e *engine) updateStepStatus(ctx context.Context, status v1alpha1.StepStatus) error {
	var (
		conditionUpdated bool
		now              = metav1.NewTime(time.Now())
	)

	parentRunner := e.parentRunner
	stepName := status.Name
	if parentRunner != "" {
		stepName = parentRunner
	}
	e.wfCtx.SetValueInMemory(now.Unix(), types.ContextKeyLastExecuteTime)
	status.LastExecuteTime = now
	index := -1
	for i, ss := range e.status.Steps {
		if ss.Name == stepName {
			index = i
			if parentRunner != "" {
				// update the sub steps status
				for j, sub := range ss.SubStepsStatus {
					if sub.Name == status.Name {
						status.FirstExecuteTime = sub.FirstExecuteTime
						e.status.Steps[i].SubStepsStatus[j] = status
						conditionUpdated = true
						break
					}
				}
			} else {
				// update the parent steps status
				status.FirstExecuteTime = ss.FirstExecuteTime
				e.status.Steps[i].StepStatus = status
				conditionUpdated = true
				break
			}
		}
	}
	if !conditionUpdated {
		status.FirstExecuteTime = now
		if parentRunner != "" {
			if index < 0 {
				e.status.Steps = append(e.status.Steps, v1alpha1.WorkflowStepStatus{
					StepStatus: v1alpha1.StepStatus{
						Name:             parentRunner,
						FirstExecuteTime: now,
					}})
				index = len(e.status.Steps) - 1
			}
			e.status.Steps[index].SubStepsStatus = append(e.status.Steps[index].SubStepsStatus, status)
		} else {
			e.status.Steps = append(e.status.Steps, v1alpha1.WorkflowStepStatus{StepStatus: status})
		}
	}
	e.stepStatus[status.Name] = status
	if feature.DefaultMutableFeatureGate.Enabled(features.EnablePatchStatusAtOnce) {
		isUpdate := false
		orig := e.status.Message
		e.status.Phase = e.checkWorkflowPhase()
		if orig != "" && e.status.Message == "" {
			// patch can not set empty string
			isUpdate = true
		}
		return e.statusPatcher(ctx, e.status, isUpdate)
	}
	return nil
}

func (e *engine) checkWorkflowPhase() v1alpha1.WorkflowRunPhase {
	status := e.status
	e.checkWorkflowStatusMessage()
	allRunnersDone, allRunnersSucceeded := checkRunners(e.taskRunners, e.instance.Status)
	if status.Terminated {
		e.cleanBackoffTimesForTerminated()
		if checkWorkflowTerminated(status, allRunnersDone) {
			wfContext.CleanupMemoryStore(e.instance.Name, e.instance.Namespace)
			if isTerminatedManually(status) {
				return v1alpha1.WorkflowStateTerminated
			}
			return v1alpha1.WorkflowStateFailed
		}
	}
	if status.Suspend {
		wfContext.CleanupMemoryStore(e.instance.Name, e.instance.Namespace)
		return v1alpha1.WorkflowStateSuspending
	}
	if allRunnersSucceeded {
		return v1alpha1.WorkflowStateSucceeded
	}
	return v1alpha1.WorkflowStateExecuting
}

func (e *engine) checkFailedAfterRetries() {
	if !e.waiting && e.failedAfterRetries && feature.DefaultMutableFeatureGate.Enabled(features.EnableSuspendOnFailure) {
		e.status.Suspend = true
	}
	if e.failedAfterRetries && !feature.DefaultMutableFeatureGate.Enabled(features.EnableSuspendOnFailure) {
		e.status.Terminated = true
	}
}

func (e *engine) needStop() bool {
	// if the workflow is terminated, we still need to execute all the remaining steps
	return e.status.Suspend
}

func (e *engine) findDependPhase(taskRunners []types.TaskRunner, index int, dag bool) v1alpha1.WorkflowStepPhase {
	dependsOn := len(e.stepDependsOn[taskRunners[index].Name()]) > 0
	if dag || dependsOn {
		return e.findDependsOnPhase(taskRunners[index].Name())
	}
	if index < 1 {
		return v1alpha1.WorkflowStepPhaseSucceeded
	}
	for i := index - 1; i >= 0; i-- {
		if skipExecutionOfNextStep(e.stepStatus[taskRunners[i].Name()].Phase, dependsOn) {
			return e.stepStatus[taskRunners[i].Name()].Phase
		}
	}
	return e.stepStatus[taskRunners[index-1].Name()].Phase
}

func (e *engine) findDependsOnPhase(name string) v1alpha1.WorkflowStepPhase {
	for _, dependsOn := range e.stepDependsOn[name] {
		if e.stepStatus[dependsOn].Phase != v1alpha1.WorkflowStepPhaseSucceeded {
			return e.stepStatus[dependsOn].Phase
		}
		if result := e.findDependsOnPhase(dependsOn); result != v1alpha1.WorkflowStepPhaseSucceeded {
			return result
		}
	}
	return v1alpha1.WorkflowStepPhaseSucceeded
}

// skipExecutionOfNextStep returns true if the next step should be skipped
func skipExecutionOfNextStep(phase v1alpha1.WorkflowStepPhase, dependsOn bool) bool {
	if dependsOn {
		return phase != v1alpha1.WorkflowStepPhaseSucceeded
	}
	return phase != v1alpha1.WorkflowStepPhaseSucceeded && phase != v1alpha1.WorkflowStepPhaseSkipped
}

func handleBackoffTimes(ctx context.Context, wfCtx wfContext.Context, status v1alpha1.StepStatus, ok bool) error {
	if ok {
		wfCtx.DeleteValueInMemory(types.ContextPrefixBackoffTimes, status.ID)
		wfCtx.DeleteValueInMemory(types.ContextPrefixBackoffReason, status.ID)
	} else {
		if val, exists := wfCtx.GetValueInMemory(types.ContextPrefixBackoffReason, status.ID); !exists || val != status.Message {
			wfCtx.SetValueInMemory(status.Message, types.ContextPrefixBackoffReason, status.ID)
			wfCtx.DeleteValueInMemory(types.ContextPrefixBackoffTimes, status.ID)
		}
		wfCtx.IncreaseCountValueInMemory(types.ContextPrefixBackoffTimes, status.ID)
	}
	if err := wfCtx.Commit(ctx); err != nil {
		return errors.WithMessage(err, "commit workflow context")
	}
	return nil
}

func (e *engine) cleanBackoffTimesForTerminated() {
	for _, ss := range e.status.Steps {
		for _, sub := range ss.SubStepsStatus {
			if sub.Reason == types.StatusReasonTerminate {
				e.wfCtx.DeleteValueInMemory(types.ContextPrefixBackoffTimes, sub.ID)
				e.wfCtx.DeleteValueInMemory(types.ContextPrefixBackoffReason, sub.ID)
			}
		}
		if ss.Reason == types.StatusReasonTerminate {
			e.wfCtx.DeleteValueInMemory(types.ContextPrefixBackoffTimes, ss.ID)
			e.wfCtx.DeleteValueInMemory(types.ContextPrefixBackoffReason, ss.ID)
		}
	}
}

func (e *engine) GetStepStatus(stepName string) v1alpha1.WorkflowStepStatus {
	// ss is step status
	for _, ss := range e.status.Steps {
		if ss.Name == stepName {
			return ss
		}
	}
	return v1alpha1.WorkflowStepStatus{}
}

func (e *engine) GetCommonStepStatus(stepName string) v1alpha1.StepStatus {
	if status, ok := e.stepStatus[stepName]; ok {
		return status
	}
	return v1alpha1.StepStatus{}
}

func (e *engine) SetParentRunner(name string) {
	e.parentRunner = name
}

func (e *engine) GetOperation() *types.Operation {
	return &types.Operation{
		Suspend:            e.status.Suspend,
		Terminated:         e.status.Terminated,
		Waiting:            e.waiting,
		FailedAfterRetries: e.failedAfterRetries,
	}
}
