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
	"testing"

	"cuelang.org/go/cue/cuecontext"
	"github.com/crossplane/crossplane-runtime/pkg/test"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	oamv1alpha1 "github.com/kubevela/pkg/apis/oam/v1alpha1"
	"github.com/kubevela/pkg/util/singleton"
	"github.com/kubevela/workflow/api/v1alpha1"
	wfContext "github.com/kubevela/workflow/pkg/context"
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

// TestOutputSensitiveRouting verifies the end-to-end fix for the workflow-side
// variant of kubevela/kubevela#6840: a step template that declares
// $sensitivePaths keeps matching outputs out of the plaintext context
// ConfigMap while non-sensitive outputs keep flowing as before.
func TestOutputSensitiveRouting(t *testing.T) {
	r := require.New(t)
	cms := map[string]*corev1.ConfigMap{}
	secrets := map[string]*corev1.Secret{}
	wfCtx := mockCapturingContext(t, cms, secrets)

	taskValue := cuecontext.New().CompileString(`
$sensitivePaths: ["password"]
password: "` + leakedTestSecret + `"
output: score: 99
`)
	stepStatus := make(map[string]v1alpha1.StepStatus)
	err := Output(wfCtx, taskValue, oamv1alpha1.WorkflowStep{
		WorkflowStepBase: oamv1alpha1.WorkflowStepBase{
			Properties: &runtime.RawExtension{Raw: []byte(`{"name":"mystep"}`)},
			Outputs: oamv1alpha1.StepOutputs{
				{ValueFrom: "password", Name: "dbpass"},
				{ValueFrom: "output.score", Name: "myscore"},
			},
		},
	}, v1alpha1.StepStatus{Phase: v1alpha1.WorkflowStepPhaseSucceeded}, stepStatus)
	r.NoError(err)

	// Both outputs must be readable downstream.
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

	// Persist and check where each landed.
	r.NoError(wfCtx.Commit(context.Background()))
	for name, cm := range cms {
		for k, v := range cm.Data {
			r.NotContains(v, leakedTestSecret, "ConfigMap %s key %s leaked the secret", name, k)
		}
	}
	found := false
	for _, s := range secrets {
		if string(s.Data[wfContext.SecretKeyVars]) != "" {
			found = true
			r.Contains(string(s.Data[wfContext.SecretKeyVars]), leakedTestSecret)
		}
	}
	r.True(found, "sensitive output should be persisted to the companion Secret")
}

func TestIsSensitiveOutput(t *testing.T) {
	r := require.New(t)
	paths := []string{"password", "output.value.data"}

	r.True(isSensitiveOutput("password", paths), "exact match")
	r.True(isSensitiveOutput("output.value.data.pw", paths), "child of sensitive path")
	r.True(isSensitiveOutput("output.value", paths), "parent extraction includes sensitive value")
	r.True(isSensitiveOutput("strings.ToUpper(password)", paths), "expressions fail closed")
	r.False(isSensitiveOutput("output.score", paths), "unrelated plain path")
	r.False(isSensitiveOutput("passwordPolicy", paths), "prefix must respect dot boundaries")
	r.False(isSensitiveOutput("anything", nil), "no declared paths means nothing is sensitive")
}
