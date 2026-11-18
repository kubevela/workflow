/*
Copyright 2026 The KubeVela Authors.

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
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	"github.com/kubevela/workflow/api/condition"
	"github.com/kubevela/workflow/api/v1alpha1"
	"github.com/kubevela/workflow/pkg/executor"
	wfTypes "github.com/kubevela/workflow/pkg/types"
	"github.com/kubevela/workflow/pkg/utils"
)

// countingReader counts the live reads a reconcile makes.
type countingReader struct {
	client.Reader
	gets int
}

func (c *countingReader) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	c.gets++
	return c.Reader.Get(ctx, key, obj, opts...)
}

func runsOnceScheme(t *testing.T) *runtime.Scheme {
	scheme := runtime.NewScheme()
	require.NoError(t, v1alpha1.AddToScheme(scheme))
	return scheme
}

// A reconcile must not start from a cached WorkflowRun older than the controller's own last
// status write: it would not see the steps that write recorded, and would run them again.
func TestReadOwnWritesUsesTheLiveRunWhenTheCacheIsBehind(t *testing.T) {
	live := &v1alpha1.WorkflowRun{ObjectMeta: metav1.ObjectMeta{Name: "deploy", Namespace: "default"}}
	live.Status.Steps = []v1alpha1.WorkflowStepStatus{{StepStatus: v1alpha1.StepStatus{Name: "notify", Phase: v1alpha1.WorkflowStepPhaseSucceeded}}}
	cli := fake.NewClientBuilder().WithScheme(runsOnceScheme(t)).WithObjects(live).WithStatusSubresource(live).Build()
	stored := &v1alpha1.WorkflowRun{}
	require.NoError(t, cli.Get(context.Background(), client.ObjectKeyFromObject(live), stored))

	reader := &countingReader{Reader: cli}
	r := &WorkflowRunReconciler{Client: cli, APIReader: reader, ownWrites: &sync.Map{}}
	r.recordOwnWrite(stored, "")

	cached := stored.DeepCopy()
	cached.ResourceVersion = "1"
	cached.Status.Steps = nil
	got, err := r.readOwnWrites(context.Background(), cached)
	require.NoError(t, err)
	require.Equal(t, stored.ResourceVersion, got.ResourceVersion)
	require.Len(t, got.Status.Steps, 1, "the live WorkflowRun carries the recorded step")
	require.Equal(t, 1, reader.gets)

	got, err = r.readOwnWrites(context.Background(), stored.DeepCopy())
	require.NoError(t, err)
	require.Equal(t, stored.ResourceVersion, got.ResourceVersion)
	require.Equal(t, 1, reader.gets, "a cache that shows the last write needs no live read")
}

// A deleted WorkflowRun leaves nothing behind in the record of own writes.
func TestADeletedRunIsForgotten(t *testing.T) {
	cli := fake.NewClientBuilder().WithScheme(runsOnceScheme(t)).Build()
	r := &WorkflowRunReconciler{Client: cli, APIReader: cli, ownWrites: &sync.Map{}}
	key := client.ObjectKey{Namespace: "default", Name: "deploy"}
	r.ownWrites.Store(key, "7")

	_, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: key})
	require.NoError(t, err)
	_, ok := r.ownWrites.Load(key)
	require.False(t, ok)
}

// Another writer changing the WorkflowRun while a reconcile runs must not lose the reconcile's
// status write, which records the steps that ran.
func TestStatusWriteIsRetriedOnConflict(t *testing.T) {
	run := &v1alpha1.WorkflowRun{ObjectMeta: metav1.ObjectMeta{Name: "deploy", Namespace: "default"}}
	conflicted := false
	cli := fake.NewClientBuilder().WithScheme(runsOnceScheme(t)).WithObjects(run).WithStatusSubresource(run).
		WithInterceptorFuncs(interceptor.Funcs{
			SubResourcePatch: func(ctx context.Context, c client.Client, sub string, obj client.Object, patch client.Patch, opts ...client.SubResourcePatchOption) error {
				if !conflicted {
					conflicted = true
					// A kubectl annotate lands between the reconcile's read and its write.
					other := &v1alpha1.WorkflowRun{}
					if err := c.Get(ctx, client.ObjectKeyFromObject(obj), other); err != nil {
						return err
					}
					other.Annotations = map[string]string{"touched-by": "kubectl"}
					if err := c.Update(ctx, other); err != nil {
						return err
					}
					return kerrors.NewConflict(schema.GroupResource{Resource: "workflowruns"}, obj.GetName(), nil)
				}
				return c.SubResource(sub).Patch(ctx, obj, patch, opts...)
			},
		}).Build()

	stale := &v1alpha1.WorkflowRun{}
	require.NoError(t, cli.Get(context.Background(), client.ObjectKeyFromObject(run), stale))
	p := newWorkflowRunPatcher(cli, cli, stale)
	status := stale.Status
	status.Message = "notify sent"
	require.NoError(t, p.patchStatus(context.Background(), &status, false))

	got := &v1alpha1.WorkflowRun{}
	require.NoError(t, cli.Get(context.Background(), client.ObjectKeyFromObject(run), got))
	require.Equal(t, "notify sent", got.Status.Message)
	require.Equal(t, "kubectl", got.Annotations["touched-by"], "the other writer's change is kept")
}

// A failed status write sets the step guard back to the steps the stored status records: the
// steps this reconcile ran were not recorded, so a guard at its in-memory count would only hold
// later reconciles back until it expired.
func TestAFailedStatusWriteSetsTheStepGuardBack(t *testing.T) {
	executor.StepStatusCache = sync.Map{}
	run := &v1alpha1.WorkflowRun{ObjectMeta: metav1.ObjectMeta{Name: "deploy", Namespace: "default"}}
	run.Status.Steps = []v1alpha1.WorkflowStepStatus{{StepStatus: v1alpha1.StepStatus{Name: "notify", Phase: v1alpha1.WorkflowStepPhaseSucceeded}}}
	cli := fake.NewClientBuilder().WithScheme(runsOnceScheme(t)).WithObjects(run).WithStatusSubresource(run).
		WithInterceptorFuncs(interceptor.Funcs{
			SubResourcePatch: func(context.Context, client.Client, string, client.Object, client.Patch, ...client.SubResourcePatchOption) error {
				return errors.New("etcd unavailable")
			},
		}).Build()
	key := "deploy-default"
	executor.StepStatusCache.Store(key, 3)

	p := newWorkflowRunPatcher(cli, cli, run)
	status := run.Status.DeepCopy()
	status.Steps = append(status.Steps, v1alpha1.WorkflowStepStatus{StepStatus: v1alpha1.StepStatus{Name: "webhook"}}, v1alpha1.WorkflowStepStatus{StepStatus: v1alpha1.StepStatus{Name: "apply"}})
	require.Error(t, p.patchStatus(context.Background(), status, false))
	guard, ok := executor.StepStatusCache.Load(key)
	require.True(t, ok)
	require.Equal(t, 1, guard, "the one step the stored status records")
}

// A user's terminate or suspend landing while a reconcile is inside a step conflicts with the
// reconcile's status write. The write is merged onto the live run: the step this reconcile ran is
// recorded, and the user's change stands. A restart is not merged over: the conflict is returned and
// the reset status kept.
func TestAUserOperationSurvivesAConflictingStatusWrite(t *testing.T) {
	ops := map[string]struct {
		op     func(context.Context, client.Client, *v1alpha1.WorkflowRun) error
		merged bool
		check  func(*testing.T, *v1alpha1.WorkflowRun)
	}{
		"terminate": {utils.TerminateWorkflow, true, func(t *testing.T, got *v1alpha1.WorkflowRun) {
			require.True(t, got.Status.Terminated)
		}},
		"suspend": {func(ctx context.Context, c client.Client, r *v1alpha1.WorkflowRun) error {
			return utils.SuspendWorkflow(ctx, c, r, "")
		}, true, func(t *testing.T, got *v1alpha1.WorkflowRun) { require.True(t, got.Status.Suspend) }},
		"restart": {func(ctx context.Context, c client.Client, r *v1alpha1.WorkflowRun) error {
			return utils.RestartWorkflow(ctx, c, r, "")
		}, false, func(t *testing.T, got *v1alpha1.WorkflowRun) { require.Empty(t, got.Status.Steps) }},
	}
	for name, tc := range ops {
		for _, isUpdate := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/update=%v", name, isUpdate), func(t *testing.T) {
				ctx := context.Background()
				executor.StepStatusCache = sync.Map{}
				run := &v1alpha1.WorkflowRun{ObjectMeta: metav1.ObjectMeta{Name: "deploy", Namespace: "default"}}
				run.Status.StartTime = metav1.Now()
				run.Status.Steps = []v1alpha1.WorkflowStepStatus{{StepStatus: v1alpha1.StepStatus{Name: "slow", Phase: v1alpha1.WorkflowStepPhaseRunning}}}
				cli := fake.NewClientBuilder().WithScheme(runsOnceScheme(t)).WithObjects(run).WithStatusSubresource(run).Build()

				reconciling := &v1alpha1.WorkflowRun{}
				require.NoError(t, cli.Get(ctx, client.ObjectKeyFromObject(run), reconciling))
				p := newWorkflowRunPatcher(cli, cli, reconciling)

				user := &v1alpha1.WorkflowRun{}
				require.NoError(t, cli.Get(ctx, client.ObjectKeyFromObject(run), user))
				require.NoError(t, tc.op(ctx, cli, user))

				reconciling.Status.Steps[0].Phase = v1alpha1.WorkflowStepPhaseSucceeded
				reconciling.Status.Steps = append(reconciling.Status.Steps, v1alpha1.WorkflowStepStatus{StepStatus: v1alpha1.StepStatus{Name: "next", Phase: v1alpha1.WorkflowStepPhaseRunning}})
				err := p.writeStatus(ctx, isUpdate)

				got := &v1alpha1.WorkflowRun{}
				require.NoError(t, cli.Get(ctx, client.ObjectKeyFromObject(run), got))
				tc.check(t, got)
				if tc.merged {
					require.NoError(t, err, "the write is merged onto the live run")
					require.Len(t, got.Status.Steps, 2)
					require.Equal(t, v1alpha1.WorkflowStepPhaseSucceeded, got.Status.Steps[0].Phase, "the step this reconcile ran is recorded")
				} else {
					require.True(t, kerrors.IsConflict(err), "the conflict is returned")
				}

				r := &WorkflowRunReconciler{Client: cli, APIReader: cli, ownWrites: &sync.Map{}}
				r.recordOwnWrite(p.ownWrite(), "")
				marker, _ := r.ownWrites.Load(client.ObjectKeyFromObject(run))
				require.Equal(t, got.ResourceVersion, marker, "the own-write record is the live resourceVersion")
			})
		}
	}
}

// A terminate lands on a step this reconcile has not finished: the merged write keeps the user's
// terminate and the step's terminated record, and records the step this reconcile did finish.
func TestAMergedWriteKeepsTheUsersTerminateAndRecordsOurStep(t *testing.T) {
	ctx := context.Background()
	executor.StepStatusCache = sync.Map{}
	run := &v1alpha1.WorkflowRun{ObjectMeta: metav1.ObjectMeta{Name: "deploy", Namespace: "default"}}
	run.Status.StartTime = metav1.Now()
	run.Status.Steps = []v1alpha1.WorkflowStepStatus{
		{StepStatus: v1alpha1.StepStatus{Name: "notify", Phase: v1alpha1.WorkflowStepPhaseRunning}},
		{StepStatus: v1alpha1.StepStatus{Name: "wait", Phase: v1alpha1.WorkflowStepPhaseRunning}},
	}
	cli := fake.NewClientBuilder().WithScheme(runsOnceScheme(t)).WithObjects(run).WithStatusSubresource(run).Build()
	reconciling := &v1alpha1.WorkflowRun{}
	require.NoError(t, cli.Get(ctx, client.ObjectKeyFromObject(run), reconciling))
	p := newWorkflowRunPatcher(cli, cli, reconciling)

	user := &v1alpha1.WorkflowRun{}
	require.NoError(t, cli.Get(ctx, client.ObjectKeyFromObject(run), user))
	require.NoError(t, utils.TerminateWorkflow(ctx, cli, user))

	status := reconciling.Status.DeepCopy()
	status.Steps[0].Phase = v1alpha1.WorkflowStepPhaseSucceeded
	status.Message = "notify sent"
	require.NoError(t, p.patchStatus(ctx, status, false))
	require.True(t, status.Terminated, "the engine's status learns of the terminate")

	got := &v1alpha1.WorkflowRun{}
	require.NoError(t, cli.Get(ctx, client.ObjectKeyFromObject(run), got))
	require.True(t, got.Status.Terminated, "the user's terminate stands")
	require.Equal(t, v1alpha1.WorkflowStepPhaseSucceeded, got.Status.Steps[0].Phase, "the step this reconcile finished is recorded")
	require.Equal(t, v1alpha1.WorkflowStepPhaseFailed, got.Status.Steps[1].Phase, "the step it had not finished keeps the terminate")
	require.Equal(t, "notify sent", got.Status.Message)

	status.Message = "next write"
	status.Terminated = false
	require.NoError(t, p.patchStatus(ctx, status, false))
	require.NoError(t, cli.Get(ctx, client.ObjectKeyFromObject(run), got))
	require.True(t, got.Status.Terminated, "a later write in the same reconcile keeps it too")
	require.Equal(t, v1alpha1.WorkflowStepPhaseFailed, got.Status.Steps[1].Phase)
}

// The record of a run's finishing write is kept until the cache shows the run finished, so a
// reconcile queued behind the write still reads the live, finished run and runs nothing.
func TestAFinishedRunIsForgottenOnceTheCacheShowsIt(t *testing.T) {
	run := &v1alpha1.WorkflowRun{ObjectMeta: metav1.ObjectMeta{Name: "deploy", Namespace: "default"}}
	cli := fake.NewClientBuilder().WithScheme(runsOnceScheme(t)).WithObjects(run).WithStatusSubresource(run).Build()
	cached := &v1alpha1.WorkflowRun{}
	require.NoError(t, cli.Get(context.Background(), client.ObjectKeyFromObject(run), cached))
	finished := cached.DeepCopy()
	finished.Status.Finished = true
	require.NoError(t, cli.Status().Update(context.Background(), finished))

	reader := &countingReader{Reader: cli}
	r := &WorkflowRunReconciler{Client: cli, APIReader: reader, ownWrites: &sync.Map{}}
	key := client.ObjectKeyFromObject(run)
	r.recordOwnWrite(finished, cached.ResourceVersion)
	_, ok := r.ownWrites.Load(key)
	require.True(t, ok, "the finishing write is recorded")

	got, err := r.readOwnWrites(context.Background(), cached.DeepCopy())
	require.NoError(t, err)
	require.True(t, got.Status.Finished, "a reconcile from the stale cache reads the finished run")
	r.recordOwnWrite(got, "")
	_, ok = r.ownWrites.Load(key)
	require.True(t, ok, "a live read says nothing about the cache, so the record stays")

	got, err = r.readOwnWrites(context.Background(), finished.DeepCopy())
	require.NoError(t, err)
	r.recordOwnWrite(got, finished.ResourceVersion)
	_, ok = r.ownWrites.Load(key)
	require.False(t, ok, "once the cache shows the run finished, it is forgotten")
}

// With EnablePatchStatusAtOnce the engine writes status per step, and Reconcile may write again
// through endWithNegativeCondition with the same patcher and run. After a write merged onto a
// user's terminate, that second write keeps the terminate too.
func TestAWriteAfterAConflictStillKeepsTheUsersTerminate(t *testing.T) {
	for _, isUpdate := range []bool{false, true} {
		t.Run(fmt.Sprintf("update=%v", isUpdate), func(t *testing.T) {
			ctx := context.Background()
			executor.StepStatusCache = sync.Map{}
			run := &v1alpha1.WorkflowRun{ObjectMeta: metav1.ObjectMeta{Name: "deploy", Namespace: "default"}}
			run.Status.Steps = []v1alpha1.WorkflowStepStatus{{StepStatus: v1alpha1.StepStatus{Name: "slow", Phase: v1alpha1.WorkflowStepPhaseRunning}}}
			cli := fake.NewClientBuilder().WithScheme(runsOnceScheme(t)).WithObjects(run).WithStatusSubresource(run).Build()

			reconciling := &v1alpha1.WorkflowRun{}
			require.NoError(t, cli.Get(ctx, client.ObjectKeyFromObject(run), reconciling))
			p := newWorkflowRunPatcher(cli, cli, reconciling)

			user := &v1alpha1.WorkflowRun{}
			require.NoError(t, cli.Get(ctx, client.ObjectKeyFromObject(run), user))
			require.NoError(t, utils.TerminateWorkflow(ctx, cli, user))

			reconciling.Status.Steps[0].Phase = v1alpha1.WorkflowStepPhaseSucceeded
			require.NoError(t, p.patchStatus(ctx, &reconciling.Status, isUpdate), "the per-step write is merged onto the live run")
			reconciling.Status.Terminated = false

			r := &WorkflowRunReconciler{Client: cli, APIReader: cli}
			_, err := r.endWithNegativeCondition(ctx, p, condition.ErrorCondition(v1alpha1.WorkflowRunConditionType, errors.New("run steps")))
			require.Error(t, err)

			got := &v1alpha1.WorkflowRun{}
			require.NoError(t, cli.Get(ctx, client.ObjectKeyFromObject(run), got))
			require.True(t, got.Status.Terminated, "the second write does not overwrite the user's terminate")
		})
	}
}

// A suspend this reconcile set itself (a suspend step) survives a conflict with another writer that
// changed something else in the status: only a flag the other writer changed is taken from it.
func TestAMergedWriteKeepsASuspendThisReconcileSet(t *testing.T) {
	ctx := context.Background()
	executor.StepStatusCache = sync.Map{}
	run := &v1alpha1.WorkflowRun{ObjectMeta: metav1.ObjectMeta{Name: "deploy", Namespace: "default"}}
	run.Status.StartTime = metav1.Now()
	run.Status.Steps = []v1alpha1.WorkflowStepStatus{{StepStatus: v1alpha1.StepStatus{Name: "approve", Phase: v1alpha1.WorkflowStepPhaseRunning}}}
	cli := fake.NewClientBuilder().WithScheme(runsOnceScheme(t)).WithObjects(run).WithStatusSubresource(run).Build()
	reconciling := &v1alpha1.WorkflowRun{}
	require.NoError(t, cli.Get(ctx, client.ObjectKeyFromObject(run), reconciling))
	p := newWorkflowRunPatcher(cli, cli, reconciling)

	other := &v1alpha1.WorkflowRun{}
	require.NoError(t, cli.Get(ctx, client.ObjectKeyFromObject(run), other))
	other.Status.Message = "noted by another writer"
	require.NoError(t, cli.Status().Update(ctx, other))

	status := reconciling.Status.DeepCopy()
	status.Suspend = true
	status.Steps[0].Phase = v1alpha1.WorkflowStepPhaseSuspending
	require.NoError(t, p.patchStatus(ctx, status, false))

	got := &v1alpha1.WorkflowRun{}
	require.NoError(t, cli.Get(ctx, client.ObjectKeyFromObject(run), got))
	require.True(t, got.Status.Suspend, "the run stays suspended")
	require.Equal(t, v1alpha1.WorkflowStepPhaseSuspending, got.Status.Steps[0].Phase)
}

// A restart from a sub-step clears that sub-step's record; a write that conflicts with it is not
// merged over, so the restart stands.
func TestARestartFromASubStepIsNotMergedOver(t *testing.T) {
	ctx := context.Background()
	executor.StepStatusCache = sync.Map{}
	run := &v1alpha1.WorkflowRun{ObjectMeta: metav1.ObjectMeta{Name: "deploy", Namespace: "default"}}
	run.Status.StartTime = metav1.Now()
	run.Status.Steps = []v1alpha1.WorkflowStepStatus{{
		StepStatus: v1alpha1.StepStatus{Name: "group", Phase: v1alpha1.WorkflowStepPhaseRunning},
		SubStepsStatus: []v1alpha1.StepStatus{
			{Name: "first", Phase: v1alpha1.WorkflowStepPhaseSucceeded},
			{Name: "second", Phase: v1alpha1.WorkflowStepPhaseSucceeded},
		},
	}}
	cli := fake.NewClientBuilder().WithScheme(runsOnceScheme(t)).WithObjects(run).WithStatusSubresource(run).Build()
	reconciling := &v1alpha1.WorkflowRun{}
	require.NoError(t, cli.Get(ctx, client.ObjectKeyFromObject(run), reconciling))
	p := newWorkflowRunPatcher(cli, cli, reconciling)

	restarted := &v1alpha1.WorkflowRun{}
	require.NoError(t, cli.Get(ctx, client.ObjectKeyFromObject(run), restarted))
	restarted.Status.Steps[0].SubStepsStatus = restarted.Status.Steps[0].SubStepsStatus[:1]
	require.NoError(t, cli.Status().Update(ctx, restarted))

	reconciling.Status.Steps[0].Phase = v1alpha1.WorkflowStepPhaseSucceeded
	require.True(t, kerrors.IsConflict(p.writeStatus(ctx, false)), "the conflict is returned")

	got := &v1alpha1.WorkflowRun{}
	require.NoError(t, cli.Get(ctx, client.ObjectKeyFromObject(run), got))
	require.Len(t, got.Status.Steps[0].SubStepsStatus, 1, "the restart stands")
}

// A restart that conflicts with this reconcile's write sets the step guard to the steps the
// restarted run records, so later reconciles are not held back by records the restart cleared.
func TestARestartConflictSetsTheStepGuardFromTheRestartedRun(t *testing.T) {
	ctx := context.Background()
	executor.StepStatusCache = sync.Map{}
	run := &v1alpha1.WorkflowRun{ObjectMeta: metav1.ObjectMeta{Name: "deploy", Namespace: "default"}}
	run.Status.StartTime = metav1.Now()
	run.Status.Steps = []v1alpha1.WorkflowStepStatus{
		{StepStatus: v1alpha1.StepStatus{Name: "first", Phase: v1alpha1.WorkflowStepPhaseSucceeded}},
		{StepStatus: v1alpha1.StepStatus{Name: "second", Phase: v1alpha1.WorkflowStepPhaseRunning}},
	}
	cli := fake.NewClientBuilder().WithScheme(runsOnceScheme(t)).WithObjects(run).WithStatusSubresource(run).Build()
	reconciling := &v1alpha1.WorkflowRun{}
	require.NoError(t, cli.Get(ctx, client.ObjectKeyFromObject(run), reconciling))
	p := newWorkflowRunPatcher(cli, cli, reconciling)
	key := "deploy-default"
	executor.StepStatusCache.Store(key, 2)

	user := &v1alpha1.WorkflowRun{}
	require.NoError(t, cli.Get(ctx, client.ObjectKeyFromObject(run), user))
	require.NoError(t, utils.RestartWorkflow(ctx, cli, user, ""))

	reconciling.Status.Steps[1].Phase = v1alpha1.WorkflowStepPhaseSucceeded
	require.True(t, kerrors.IsConflict(p.writeStatus(ctx, false)))
	guard, ok := executor.StepStatusCache.Load(key)
	require.True(t, ok)
	require.Equal(t, 0, guard, "the restarted run records no steps")
}

// The record of a reconcile's write is dropped once a reconcile starts from a cached copy that
// shows it: the cache does not go back, so the record is no longer needed.
func TestAnOwnWriteRecordIsForgottenOnceTheCacheShowsIt(t *testing.T) {
	r := &WorkflowRunReconciler{ownWrites: &sync.Map{}}
	run := &v1alpha1.WorkflowRun{ObjectMeta: metav1.ObjectMeta{Name: "deploy", Namespace: "default", ResourceVersion: "7"}}
	r.recordOwnWrite(run, "")
	_, ok := r.ownWrites.Load(client.ObjectKeyFromObject(run))
	require.True(t, ok)
	r.recordOwnWrite(run, "7")
	_, ok = r.ownWrites.Load(client.ObjectKeyFromObject(run))
	require.False(t, ok, "a reconcile from a cache that shows the write forgets it")
}

// A finished run raises no further reconcile, so the record of its finishing write is dropped
// after finishedRecordGrace without one.
func TestAFinishedRunsRecordIsForgottenWithoutAnotherReconcile(t *testing.T) {
	grace := finishedRecordGrace
	finishedRecordGrace = 500 * time.Millisecond
	t.Cleanup(func() { finishedRecordGrace = grace })
	r := &WorkflowRunReconciler{ownWrites: &sync.Map{}}
	run := &v1alpha1.WorkflowRun{ObjectMeta: metav1.ObjectMeta{Name: "deploy", Namespace: "default", ResourceVersion: "9"}}
	run.Status.Finished = true
	r.recordOwnWrite(run, "8")
	_, ok := r.ownWrites.Load(client.ObjectKeyFromObject(run))
	require.True(t, ok, "kept for reconciles already queued")
	require.Eventually(t, func() bool {
		_, ok := r.ownWrites.Load(client.ObjectKeyFromObject(run))
		return !ok
	}, 2*time.Second, 20*time.Millisecond, "dropped after the grace period")
}

// A terminate lands while a reconcile is between steps: the reconcile read warm-up suspending, the
// user's terminate failed it with reason Terminate, and the reconcile meanwhile finished warm-up and
// slow-call and has hold suspending. The merged status stops the run as the terminate does: the
// steps that ran stay recorded, and every unfinished step is failed with reason Terminate.
func TestAMergedTerminateStillStopsTheRun(t *testing.T) {
	step := func(name string, phase v1alpha1.WorkflowStepPhase, reason string) v1alpha1.WorkflowStepStatus {
		return v1alpha1.WorkflowStepStatus{StepStatus: v1alpha1.StepStatus{ID: name + "-id", Name: name, Phase: phase, Reason: reason}}
	}
	base := v1alpha1.WorkflowRunStatus{Suspend: true, Steps: []v1alpha1.WorkflowStepStatus{
		step("warm-up", v1alpha1.WorkflowStepPhaseSuspending, wfTypes.StatusReasonSuspend)}}
	live := v1alpha1.WorkflowRunStatus{Terminated: true, Steps: []v1alpha1.WorkflowStepStatus{
		step("warm-up", v1alpha1.WorkflowStepPhaseFailed, wfTypes.StatusReasonTerminate)}}
	ours := v1alpha1.WorkflowRunStatus{Suspend: true, Steps: []v1alpha1.WorkflowStepStatus{
		step("warm-up", v1alpha1.WorkflowStepPhaseSucceeded, ""),
		step("slow-call", v1alpha1.WorkflowStepPhaseSucceeded, ""),
		step("hold", v1alpha1.WorkflowStepPhaseSuspending, wfTypes.StatusReasonSuspend)}}

	merged := mergeUserChanges(ours, base, live)
	require.True(t, merged.Terminated)
	require.False(t, merged.Suspend)
	require.Equal(t, v1alpha1.WorkflowStepPhaseSucceeded, merged.Steps[0].Phase, "warm-up ran")
	require.Equal(t, v1alpha1.WorkflowStepPhaseSucceeded, merged.Steps[1].Phase, "slow-call ran, and stays recorded once")
	require.Equal(t, v1alpha1.WorkflowStepPhaseFailed, merged.Steps[2].Phase, "hold had not finished")
	require.Equal(t, wfTypes.StatusReasonTerminate, merged.Steps[2].Reason)
}

// withRunsOnce is the suite's reconciler with the live reader and own-write record that
// SetupWithManager gives the controller.
func withRunsOnce(r *WorkflowRunReconciler) *WorkflowRunReconciler {
	c := *r
	c.APIReader = k8sClient
	c.ownWrites = &sync.Map{}
	return &c
}
