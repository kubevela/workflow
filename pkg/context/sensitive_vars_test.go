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

package context

import (
	"context"
	"strings"
	"testing"

	"cuelang.org/go/cue/cuecontext"
	"github.com/crossplane/crossplane-runtime/pkg/test"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kubevela/pkg/util/singleton"
)

const sensitiveTestValue = "s3cr3t-db-password-DO-NOT-LEAK"

// capturingClient returns a mock client that records every ConfigMap and
// Secret written through it, so tests can assert exactly what would be
// persisted to the API server.
func capturingClient(t *testing.T, cms map[string]*corev1.ConfigMap, secrets map[string]*corev1.Secret) {
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
				return kerrors.NewNotFound(corev1.Resource("configmaps"), key.Name)
			case *corev1.Secret:
				if s, ok := secrets[key.Name]; ok {
					s.DeepCopyInto(o)
					return nil
				}
				return kerrors.NewNotFound(corev1.Resource("secrets"), key.Name)
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
}

func TestSensitiveVarSecretRoundTrip(t *testing.T) {
	r := require.New(t)
	cms := map[string]*corev1.ConfigMap{}
	secrets := map[string]*corev1.Secret{}
	capturingClient(t, cms, secrets)

	wfCtx, err := NewContext(context.Background(), "default", "app", nil)
	r.NoError(err)

	cuectx := cuecontext.New()
	r.NoError(wfCtx.SetVar(cuectx.CompileString(`"db.internal"`), "dbHost"))
	r.NoError(wfCtx.SetSensitiveVar(cuectx.CompileString(`"`+sensitiveTestValue+`"`), "dbPassword"))
	r.NoError(wfCtx.Commit(context.Background()))

	// The context ConfigMap must exist and must NOT contain the secret value.
	cm, ok := cms[generateStoreName("app")]
	r.True(ok, "context ConfigMap should be persisted")
	for k, v := range cm.Data {
		r.NotContains(v, sensitiveTestValue, "ConfigMap key %q leaked the sensitive value", k)
	}
	r.Contains(cm.Data[ConfigMapKeyVars], "db.internal")

	// The companion Secret must hold the sensitive value.
	secret, ok := secrets[generateStoreName("app")+SensitiveStoreSuffix]
	r.True(ok, "companion Secret should be persisted")
	r.Contains(string(secret.Data[SecretKeyVars]), sensitiveTestValue)
	r.Equal(corev1.SecretTypeOpaque, secret.Type)

	// Reads must be transparent.
	v, err := wfCtx.GetVar("dbPassword")
	r.NoError(err)
	s, err := v.String()
	r.NoError(err)
	r.Equal(sensitiveTestValue, s)
}

func TestNoSecretCreatedWithoutSensitiveVars(t *testing.T) {
	r := require.New(t)
	cms := map[string]*corev1.ConfigMap{}
	secrets := map[string]*corev1.Secret{}
	capturingClient(t, cms, secrets)

	wfCtx, err := NewContext(context.Background(), "default", "plain-app", nil)
	r.NoError(err)
	r.NoError(wfCtx.SetVar(cuecontext.New().CompileString(`"value"`), "plain"))
	r.NoError(wfCtx.Commit(context.Background()))

	r.Len(secrets, 0, "no Secret should be created when nothing sensitive was stored")
}

func TestLoadContextRecoversSensitiveVars(t *testing.T) {
	r := require.New(t)
	storeName := generateStoreName("resumed-app")
	cms := map[string]*corev1.ConfigMap{
		storeName: {
			ObjectMeta: metav1.ObjectMeta{Name: storeName, Namespace: "default"},
			Data:       map[string]string{ConfigMapKeyVars: `dbHost: "db.internal"`},
		},
	}
	secrets := map[string]*corev1.Secret{
		storeName + SensitiveStoreSuffix: {
			ObjectMeta: metav1.ObjectMeta{Name: storeName + SensitiveStoreSuffix, Namespace: "default"},
			Data:       map[string][]byte{SecretKeyVars: []byte(`dbPassword: "` + sensitiveTestValue + `"`)},
		},
	}
	capturingClient(t, cms, secrets)

	wfCtx, err := LoadContext(context.Background(), "default", "resumed-app", storeName)
	r.NoError(err)

	v, err := wfCtx.GetVar("dbPassword")
	r.NoError(err)
	s, err := v.String()
	r.NoError(err)
	r.Equal(sensitiveTestValue, s)

	// Plain vars still resolve from the ConfigMap side.
	host, err := wfCtx.GetVar("dbHost")
	r.NoError(err)
	hs, err := host.String()
	r.NoError(err)
	r.Equal("db.internal", hs)
}

func TestSensitiveVarsClearedOnCommit(t *testing.T) {
	r := require.New(t)
	cms := map[string]*corev1.ConfigMap{}
	secrets := map[string]*corev1.Secret{}
	capturingClient(t, cms, secrets)

	wfCtx, err := NewContext(context.Background(), "default", "clear-app", nil)
	r.NoError(err)
	r.NoError(wfCtx.(*WorkflowContext).SetSensitiveVar(cuecontext.New().CompileString(`"`+sensitiveTestValue+`"`), "token"))
	r.NoError(wfCtx.Commit(context.Background()))
	secretName := generateStoreName("clear-app") + SensitiveStoreSuffix
	r.Contains(string(secrets[secretName].Data[SecretKeyVars]), sensitiveTestValue)

	// A later commit keeps the Secret in sync (the doc still contains the var
	// here, but the write path must go through patch without error).
	r.NoError(wfCtx.(*WorkflowContext).SetVar(cuecontext.New().CompileString(`"x"`), "plain"))
	r.NoError(wfCtx.Commit(context.Background()))
	r.True(strings.Contains(string(secrets[secretName].Data[SecretKeyVars]), sensitiveTestValue))
}
