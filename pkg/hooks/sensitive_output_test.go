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

package hooks

import (
	"context"
	"strings"
	"testing"

	"cuelang.org/go/cue/cuecontext"
	"github.com/crossplane/crossplane-runtime/v2/pkg/test"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	oamv1alpha1 "github.com/kubevela/pkg/apis/oam/v1alpha1"
	"github.com/kubevela/pkg/util/singleton"
	"github.com/kubevela/workflow/api/v1alpha1"
	wfContext "github.com/kubevela/workflow/pkg/context"
	"github.com/kubevela/workflow/pkg/cue/sensitive"
)

const leakedTestSecret = "s3cr3t-token-DO-NOT-LEAK"

// mockCapturingContext builds a real workflow context backed by a mock client
// that records persisted ConfigMaps and Secrets.
func mockCapturingContext(t *testing.T, cms map[string]*corev1.ConfigMap, secrets map[string]*corev1.Secret) wfContext.Context {
	t.Helper()
	record := func(obj client.Object) {
		switch o := obj.(type) {
		case *corev1.ConfigMap:
			cms[o.Name] = o.DeepCopy()
		case *corev1.Secret:
			secrets[o.Name] = o.DeepCopy()
		}
	}
	cli := &test.MockClient{
		MockGet: func(ctx context.Context, key client.ObjectKey, obj client.Object) error {
			switch o := obj.(type) {
			case *corev1.ConfigMap:
				if cm, ok := cms[key.Name]; ok {
					cm.DeepCopyInto(o)
					return nil
				}
			case *corev1.Secret:
				if s, ok := secrets[key.Name]; ok {
					s.DeepCopyInto(o)
					return nil
				}
			}
			return kerrors.NewNotFound(corev1.Resource("objects"), key.Name)
		},
		MockCreate: func(ctx context.Context, obj client.Object, opts ...client.CreateOption) error {
			record(obj)
			return nil
		},
		MockPatch: func(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
			record(obj)
			return nil
		},
	}
	singleton.KubeClient.Set(cli)
	wfCtx, err := wfContext.NewContext(context.Background(), "default", "sens-app", nil)
	require.NoError(t, err)
	return wfCtx
}

// stepWithOutputs is the step shape the routing tests share: one output taken
// from a marked path, one from an unmarked one.
func stepWithOutputs() oamv1alpha1.WorkflowStep {
	return oamv1alpha1.WorkflowStep{
		WorkflowStepBase: oamv1alpha1.WorkflowStepBase{
			Properties: &runtime.RawExtension{Raw: []byte(`{"name":"mystep"}`)},
			Outputs: oamv1alpha1.StepOutputs{
				{ValueFrom: "output.password", Name: "dbpass"},
				{ValueFrom: "output.score", Name: "myscore"},
			},
		},
	}
}

// assertNoConfigMapLeak fails if any persisted ConfigMap carries the secret.
func assertNoConfigMapLeak(t *testing.T, cms map[string]*corev1.ConfigMap) {
	t.Helper()
	for name, cm := range cms {
		for k, v := range cm.Data {
			require.NotContains(t, v, leakedTestSecret, "ConfigMap %s key %s leaked the secret", name, k)
		}
	}
}

// contextVars returns the serialized vars of the one persisted context
// ConfigMap. The store name is derived inside pkg/context, so the test asks the
// recording client what was written rather than recomputing it.
func contextVars(t *testing.T, cms map[string]*corev1.ConfigMap) string {
	t.Helper()
	require.Len(t, cms, 1, "exactly one context ConfigMap should be persisted")
	for _, cm := range cms {
		return cm.Data[wfContext.ConfigMapKeyVars]
	}
	return ""
}

// assertSecretHolds fails unless some companion Secret carries the secret.
func assertSecretHolds(t *testing.T, secrets map[string]*corev1.Secret) {
	t.Helper()
	for _, s := range secrets {
		if strings.Contains(string(s.Data[wfContext.SecretKeyVars]), leakedTestSecret) {
			return
		}
	}
	t.Fatalf("sensitive output should be persisted to the companion Secret, got %d secrets", len(secrets))
}

// TestOutputSensitiveRouting verifies the end-to-end fix for the workflow-side
// variant of kubevela/kubevela#6840: a step template that marks a field
// `// +sensitive` keeps the matching output out of the plaintext context
// ConfigMap while non-sensitive outputs keep flowing as before.
func TestOutputSensitiveRouting(t *testing.T) {
	r := require.New(t)
	cms := map[string]*corev1.ConfigMap{}
	secrets := map[string]*corev1.Secret{}
	wfCtx := mockCapturingContext(t, cms, secrets)

	// The template the definition author writes. The marker is a comment, so it
	// only exists in this text -- the value it evaluates to below has no trace
	// of it, which is why the paths are read here and handed to the hook.
	template := `
output: {
	// +sensitive
	password: string
	score:    int
}`
	paths, err := sensitive.ExtractPaths(template)
	r.NoError(err)
	r.Equal([]string{"output.password"}, paths)

	taskValue := cuecontext.New().CompileString(`
output: {
	password: "` + leakedTestSecret + `"
	score:    99
}`)
	stepStatus := make(map[string]v1alpha1.StepStatus)
	hook := OutputWithSensitive(paths, nil)
	r.NoError(hook(wfCtx, taskValue, stepWithOutputs(),
		v1alpha1.StepStatus{Phase: v1alpha1.WorkflowStepPhaseSucceeded}, stepStatus))

	// Both outputs must be readable downstream, wherever they were stored.
	pv, err := wfCtx.GetVar("dbpass")
	r.NoError(err)
	ps, err := pv.String()
	r.NoError(err)
	r.Equal(leakedTestSecret, ps)
	sv, err := wfCtx.GetVar("myscore")
	r.NoError(err)
	si, err := sv.Int64()
	r.NoError(err)
	r.Equal(int64(99), si)

	r.NoError(wfCtx.Commit(context.Background()))
	assertNoConfigMapLeak(t, cms)
	assertSecretHolds(t, secrets)
}

// A template whose markers could not be read tells us nothing about what is
// secret, so every output goes to the Secret. Routing them to the ConfigMap
// instead would leak precisely the values a broken marker was meant to protect.
func TestOutputFailsClosedWhenMarkersCannotBeRead(t *testing.T) {
	r := require.New(t)
	cms := map[string]*corev1.ConfigMap{}
	secrets := map[string]*corev1.Secret{}
	wfCtx := mockCapturingContext(t, cms, secrets)

	paths, extractErr := sensitive.ExtractPaths(`this is not cue {{{`)
	r.Error(extractErr)
	r.Nil(paths)

	taskValue := cuecontext.New().CompileString(`
output: {
	password: "` + leakedTestSecret + `"
	score:    99
}`)
	hook := OutputWithSensitive(paths, extractErr)
	r.NoError(hook(wfCtx, taskValue, stepWithOutputs(),
		v1alpha1.StepStatus{Phase: v1alpha1.WorkflowStepPhaseSucceeded},
		make(map[string]v1alpha1.StepStatus)))

	r.NoError(wfCtx.Commit(context.Background()))
	assertNoConfigMapLeak(t, cms)
	assertSecretHolds(t, secrets)
	for _, cm := range cms {
		r.NotContains(cm.Data[wfContext.ConfigMapKeyVars], "myscore",
			"with the markers unreadable, even the unmarked output stays out of the ConfigMap")
	}
}

// A var written in the clear and later classified sensitive must not keep its
// plaintext in the ConfigMap. This is the reused-output-name case: the template
// gained a marker between runs.
func TestOutputEvictsAnEarlierPlaintextCopy(t *testing.T) {
	r := require.New(t)
	cms := map[string]*corev1.ConfigMap{}
	secrets := map[string]*corev1.Secret{}
	wfCtx := mockCapturingContext(t, cms, secrets)

	// Run one: no marker, so the value goes to the ConfigMap.
	plain := cuecontext.New().CompileString(`output: {password: "harmless", score: 1}`)
	r.NoError(Output(wfCtx, plain, stepWithOutputs(),
		v1alpha1.StepStatus{Phase: v1alpha1.WorkflowStepPhaseSucceeded},
		make(map[string]v1alpha1.StepStatus)))
	r.NoError(wfCtx.Commit(context.Background()))
	r.Contains(contextVars(t, cms), "harmless")

	// Run two: the template now marks it, and the real credential arrives.
	// score keeps its value: SetVar unifies, so an unmarked output that changed
	// between runs conflicts, which is existing behaviour and not what this is
	// about.
	marked := cuecontext.New().CompileString(`output: {password: "` + leakedTestSecret + `", score: 1}`)
	hook := OutputWithSensitive([]string{"output.password"}, nil)
	r.NoError(hook(wfCtx, marked, stepWithOutputs(),
		v1alpha1.StepStatus{Phase: v1alpha1.WorkflowStepPhaseSucceeded},
		make(map[string]v1alpha1.StepStatus)))
	r.NoError(wfCtx.Commit(context.Background()))

	assertNoConfigMapLeak(t, cms)
	assertSecretHolds(t, secrets)
	vars := contextVars(t, cms)
	r.NotContains(vars, "dbpass", "the var moved to the Secret is gone from the ConfigMap")
	r.NotContains(vars, "harmless", "and so is the plaintext it used to hold")
	r.Contains(vars, "myscore", "the unmarked output is untouched")
}

func TestIsSensitiveOutput(t *testing.T) {
	r := require.New(t)
	paths := []string{"output.password", "output.value.data"}

	r.True(isSensitiveOutput("output.password", paths, nil), "exact match")
	r.True(isSensitiveOutput("output.value.data.pw", paths, nil), "child of a marked path")
	r.True(isSensitiveOutput("output.value", paths, nil), "parent extraction includes the marked value")
	r.True(isSensitiveOutput("strings.ToUpper(output.password)", paths, nil), "expressions fail closed")
	r.False(isSensitiveOutput("output.score", paths, nil), "unrelated plain path")
	r.False(isSensitiveOutput("output.passwordPolicy", paths, nil), "prefix must respect dot boundaries")
	r.False(isSensitiveOutput("anything", nil, nil), "no marked paths means nothing is sensitive")

	// An unreadable template is the one case that overrides everything else:
	// not even an expression-free path against an empty mark list is trusted.
	r.True(isSensitiveOutput("output.score", nil, errors.New("parse")), "unreadable markers fail closed")
}
