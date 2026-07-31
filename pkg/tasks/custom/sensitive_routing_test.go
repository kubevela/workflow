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

package custom

import (
	"context"
	"testing"

	"github.com/crossplane/crossplane-runtime/v2/pkg/test"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic/fake"
	"sigs.k8s.io/controller-runtime/pkg/client"

	cuexv1alpha1 "github.com/kubevela/pkg/apis/cue/v1alpha1"
	oamv1alpha1 "github.com/kubevela/pkg/apis/oam/v1alpha1"
	"github.com/kubevela/pkg/util/singleton"

	wfContext "github.com/kubevela/workflow/pkg/context"
	"github.com/kubevela/workflow/pkg/cue/process"
	"github.com/kubevela/workflow/pkg/providers"
	"github.com/kubevela/workflow/pkg/types"
)

const routedSecret = "s3cr3t-token-DO-NOT-LEAK"

// recordingCluster stands in for the API server, keeping whatever a commit
// writes so a test can ask where a value actually landed.
func recordingCluster(t *testing.T) (map[string]*corev1.ConfigMap, map[string]*corev1.Secret) {
	t.Helper()
	cms := map[string]*corev1.ConfigMap{}
	secrets := map[string]*corev1.Secret{}
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
	scheme := runtime.NewScheme()
	require.NoError(t, cuexv1alpha1.AddToScheme(scheme))
	singleton.DynamicClient.Set(fake.NewSimpleDynamicClient(scheme))
	return cms, secrets
}

// TestTaskRunnerRoutesMarkedOutputsToTheSecret drives a real task runner, which
// is the only way to see the plumbing work: the `// +sensitive` marker is a
// comment, so it exists in the template text that the loader holds and nowhere
// in the value the step evaluates to. The task runner reads it once per
// template and builds the output hook around it, which is also why the executor
// no longer installs a single shared hook for every step.
func TestTaskRunnerRoutesMarkedOutputsToTheSecret(t *testing.T) {
	cms, secrets := recordingCluster(t)

	loadTemplate := func(_ context.Context, name string) (string, error) {
		switch name {
		case "marked":
			return `
parameter: {}
creds: {
	// +sensitive
	token: "` + routedSecret + `"
	user:  "admin"
}
`, nil
		default:
			return `
parameter: {}
creds: {
	token: "` + routedSecret + `"
	user:  "admin"
}
`, nil
		}
	}

	for _, tc := range []struct {
		name         string
		stepType     string
		wantInSecret bool
	}{
		{name: "marked template routes to the companion Secret", stepType: "marked", wantInSecret: true},
		{name: "unmarked template keeps today's behaviour", stepType: "plain", wantInSecret: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			wfCtx, err := wfContext.NewContext(context.Background(), "default", tc.stepType+"-app", nil)
			r.NoError(err)

			pCtx := process.NewContext(process.ContextData{Name: "app", Namespace: "default"})
			loader := NewTaskLoader(loadTemplate, 0, pCtx, providers.DefaultCompiler.Get())
			gen, err := loader.GetTaskGenerator(context.Background(), tc.stepType)
			r.NoError(err)

			runner, err := gen(oamv1alpha1.WorkflowStep{
				WorkflowStepBase: oamv1alpha1.WorkflowStepBase{
					Name: "read-creds",
					Type: tc.stepType,
					Outputs: oamv1alpha1.StepOutputs{
						{ValueFrom: "creds.token", Name: "token"},
						{ValueFrom: "creds.user", Name: "user"},
					},
				},
			}, &types.TaskGeneratorOptions{ID: "id"})
			r.NoError(err)

			// No PostStopHooks: the runner supplies the one carrying this
			// template's marked paths. That is the path the executor takes.
			_, _, err = runner.Run(wfCtx, &types.TaskRunOptions{})
			r.NoError(err)
			r.NoError(wfCtx.Commit(context.Background()))

			// Either way the value is readable downstream.
			v, err := wfCtx.GetVar("token")
			r.NoError(err)
			got, err := v.String()
			r.NoError(err)
			r.Equal(routedSecret, got)

			storeName := cms[storeNameOf(t, cms)].Name
			vars := cms[storeName].Data[wfContext.ConfigMapKeyVars]
			r.Contains(vars, "admin", "the unmarked output always goes to the ConfigMap")
			if tc.wantInSecret {
				r.NotContains(vars, routedSecret, "the marked output must not reach the ConfigMap")
				r.Contains(string(secrets[storeName+wfContext.SensitiveStoreSuffix].Data[wfContext.SecretKeyVars]), routedSecret)
			} else {
				r.Contains(vars, routedSecret, "without a marker nothing changes")
				r.Empty(secrets, "and no Secret is created")
			}
			// Reset between cases so each sees only its own writes.
			for k := range cms {
				delete(cms, k)
			}
			for k := range secrets {
				delete(secrets, k)
			}
		})
	}
}

// storeNameOf returns the name of the single persisted context ConfigMap. The
// name is derived inside pkg/context, so the test reads it back rather than
// recomputing it.
func storeNameOf(t *testing.T, cms map[string]*corev1.ConfigMap) string {
	t.Helper()
	require.Len(t, cms, 1, "exactly one context ConfigMap should be persisted")
	for name := range cms {
		return name
	}
	return ""
}
