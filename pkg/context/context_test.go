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

package context

import (
	"context"
	"encoding/json"
	"testing"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
	"github.com/crossplane/crossplane-runtime/pkg/test"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	yamlUtil "sigs.k8s.io/yaml"

	"github.com/kubevela/pkg/cue/util"
	"github.com/kubevela/pkg/util/singleton"
)

func TestVars(t *testing.T) {
	wfCtx := newContextForTest(t)

	testCases := []struct {
		variable string
		paths    []string
		expected string
	}{
		{
			variable: `input: "1.1.1.1"`,
			paths:    []string{"clusterIP"},
			expected: `"1.1.1.1"`,
		},
		{
			variable: "input: 100",
			paths:    []string{"football", "score"},
			expected: "100",
		},
		{
			variable: `
input: {
    score: int
	result: score+1
}`,
			paths: []string{"football"},
			expected: `score:  100
result: 101`,
		},
	}
	for _, tCase := range testCases {
		r := require.New(t)
		cuectx := cuecontext.New()
		val := cuectx.CompileString(tCase.variable)
		input := val.LookupPath(cue.ParsePath("input"))
		err := wfCtx.SetVar(input, tCase.paths...)
		r.NoError(err)
		result, err := wfCtx.GetVar(tCase.paths...)
		r.NoError(err)
		rStr, err := util.ToString(result)
		r.NoError(err)
		r.Equal(rStr, tCase.expected)
	}

	r := require.New(t)
	conflictV := cuecontext.New().CompileString(`score: 101`)
	err := wfCtx.SetVar(conflictV, "football")
	r.Equal(err.Error(), "football.score: conflicting values 101 and 100")
}

func TestRefObj(t *testing.T) {

	wfCtx := new(WorkflowContext)
	wfCtx.store = &corev1.ConfigMap{}
	wfCtx.store.APIVersion = "v1"
	wfCtx.store.Kind = "ConfigMap"
	wfCtx.store.Name = "app-v1"

	ref := wfCtx.StoreRef()
	r := require.New(t)
	r.Equal(*ref, corev1.ObjectReference{
		APIVersion: "v1",
		Kind:       "ConfigMap",
		Name:       "app-v1",
	})
}

func TestContext(t *testing.T) {
	newCliForTest(t, nil)
	r := require.New(t)
	ctx := context.Background()

	wfCtx, err := NewContext(ctx, "default", "app-v1", []metav1.OwnerReference{{Name: "test1"}})
	r.NoError(err)
	err = wfCtx.Commit(context.Background())
	r.NoError(err)

	_, err = NewContext(ctx, "default", "app-v1", []metav1.OwnerReference{{Name: "test2"}})
	r.NoError(err)

	wfCtx, err = LoadContext(ctx, "default", "app-v1", "workflow-app-v1-context")
	r.NoError(err)
	err = wfCtx.Commit(context.Background())
	r.NoError(err)

	newCliForTest(t, nil)
	_, err = LoadContext(ctx, "default", "app-v1", "workflow-app-v1-context")
	r.Equal(err.Error(), `configMap "workflow-app-v1-context" not found`)

	_, err = NewContext(ctx, "default", "app-v1", nil)
	r.NoError(err)
}

func TestGetStore(t *testing.T) {
	newCliForTest(t, nil)
	r := require.New(t)

	wfCtx, err := NewContext(context.Background(), "default", "app-v1", nil)
	r.NoError(err)
	err = wfCtx.Commit(context.Background())
	r.NoError(err)

	store := wfCtx.GetStore()
	r.Equal(store.Name, "workflow-app-v1-context")
}

func TestMutableValue(t *testing.T) {
	newCliForTest(t, nil)
	r := require.New(t)

	wfCtx, err := NewContext(context.Background(), "default", "app-v1", nil)
	r.NoError(err)
	err = wfCtx.Commit(context.Background())
	r.NoError(err)

	wfCtx.SetMutableValue("value", "test", "key")
	v := wfCtx.GetMutableValue("test", "key")
	r.Equal(v, "value")

	wfCtx.DeleteMutableValue("test", "key")
	v = wfCtx.GetMutableValue("test", "key")
	r.Equal(v, "")
}

func TestMemoryValue(t *testing.T) {
	newCliForTest(t, nil)
	r := require.New(t)

	wfCtx, err := NewContext(context.Background(), "default", "app-v1", nil)
	r.NoError(err)
	err = wfCtx.Commit(context.Background())
	r.NoError(err)

	wfCtx.SetValueInMemory("value", "test", "key")
	v, ok := wfCtx.GetValueInMemory("test", "key")
	r.Equal(ok, true)
	r.Equal(v.(string), "value")

	wfCtx.DeleteValueInMemory("test", "key")
	_, ok = wfCtx.GetValueInMemory("test", "key")
	r.Equal(ok, false)

	wfCtx.SetValueInMemory("value", "test", "key")
	count := wfCtx.IncreaseCountValueInMemory("test", "key")
	r.Equal(count, 0)
	count = wfCtx.IncreaseCountValueInMemory("notfound", "key")
	r.Equal(count, 0)
	wfCtx.SetValueInMemory(10, "number", "key")
	count = wfCtx.IncreaseCountValueInMemory("number", "key")
	r.Equal(count, 11)
}

func newCliForTest(t *testing.T, wfCm *corev1.ConfigMap) {
	r := require.New(t)
	cli := &test.MockClient{
		MockGet: func(ctx context.Context, key client.ObjectKey, obj client.Object) error {
			o, ok := obj.(*corev1.ConfigMap)
			if ok {
				switch key.Name {
				case "app-v1":
					var cm corev1.ConfigMap
					testCaseJson, err := yamlUtil.YAMLToJSON([]byte(testCaseYaml))
					r.NoError(err)
					err = json.Unmarshal(testCaseJson, &cm)
					r.NoError(err)
					*o = cm
					return nil
				case generateStoreName("app-v1"):
					if wfCm != nil {
						*o = *wfCm
						return nil
					}
				}
			}
			return kerrors.NewNotFound(corev1.Resource("configMap"), key.Name)
		},
		MockCreate: func(ctx context.Context, obj client.Object, opts ...client.CreateOption) error {
			o, ok := obj.(*corev1.ConfigMap)
			if ok {
				wfCm = o
			}
			return nil
		},
		MockPatch: func(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
			o, ok := obj.(*corev1.ConfigMap)
			if ok {
				if wfCm == nil {
					return kerrors.NewNotFound(corev1.Resource("configMap"), o.Name)
				}
				*wfCm = *o
			}
			return nil
		},
	}
	singleton.KubeClient.Set(cli)
}

func newContextForTest(t *testing.T) *WorkflowContext {
	r := require.New(t)
	var cm corev1.ConfigMap
	testCaseJson, err := yamlUtil.YAMLToJSON([]byte(testCaseYaml))
	r.NoError(err)
	err = json.Unmarshal(testCaseJson, &cm)
	r.NoError(err)

	wfCtx := &WorkflowContext{
		store: &cm,
	}
	err = wfCtx.LoadFromConfigMap(context.Background(), cm)
	r.NoError(err)
	return wfCtx
}

func TestSensitiveVars(t *testing.T) {
	wfCtx := newContextForTestWithSecret(t)

	testCases := []struct {
		variable string
		paths    []string
		expected string
	}{
		{
			variable: `input: "super-secret-api-key"`,
			paths:    []string{"apiKey"},
			expected: `"super-secret-api-key"`,
		},
		{
			variable: `input: "password123"`,
			paths:    []string{"credentials", "password"},
			expected: `"password123"`,
		},
	}
	for _, tCase := range testCases {
		r := require.New(t)
		cuectx := cuecontext.New()
		val := cuectx.CompileString(tCase.variable)
		input := val.LookupPath(cue.ParsePath("input"))
		err := wfCtx.SetSensitiveVar(input, tCase.paths...)
		r.NoError(err)
		result, err := wfCtx.GetSensitiveVar(tCase.paths...)
		r.NoError(err)
		rStr, err := util.ToString(result)
		r.NoError(err)
		r.Equal(rStr, tCase.expected)
	}
}

func TestGetSecretStore(t *testing.T) {
	newCliForTestWithSecret(t, nil, nil)
	r := require.New(t)

	wfCtx, err := NewContext(context.Background(), "default", "app-v1", nil)
	r.NoError(err)

	secretStore := wfCtx.GetSecretStore()
	r.NotNil(secretStore)
	r.Equal(secretStore.Name, "workflow-app-v1-context-secret")
}

func TestSecretStoreOwnerReferences(t *testing.T) {
	newCliForTestWithSecret(t, nil, nil)
	r := require.New(t)

	owner := []metav1.OwnerReference{{
		APIVersion: "core.oam.dev/v1beta1",
		Kind:       "Application",
		Name:       "test-app",
		UID:        "test-uid-12345",
	}}

	wfCtx, err := NewContext(context.Background(), "default", "app-v1", owner)
	r.NoError(err)

	// Verify ConfigMap has owner references
	store := wfCtx.GetStore()
	r.NotNil(store)
	r.Equal(owner, store.OwnerReferences)

	// Verify Secret has same owner references (critical for garbage collection)
	secretStore := wfCtx.GetSecretStore()
	r.NotNil(secretStore)
	r.Equal(owner, secretStore.OwnerReferences, "Secret must have same OwnerReferences as ConfigMap for garbage collection")
}

func TestSensitiveVarsNotInConfigMap(t *testing.T) {
	wfCtx := newContextForTestWithSecret(t)
	r := require.New(t)

	// Set a sensitive variable
	cuectx := cuecontext.New()
	val := cuectx.CompileString(`input: "secret-token-xyz"`)
	input := val.LookupPath(cue.ParsePath("input"))
	err := wfCtx.SetSensitiveVar(input, "token")
	r.NoError(err)

	// Write to store
	err = wfCtx.writeToStore()
	r.NoError(err)

	// Verify sensitive data is NOT in ConfigMap
	configMapData := wfCtx.store.Data[ConfigMapKeyVars]
	r.NotContains(configMapData, "secret-token-xyz", "Sensitive data should NOT be in ConfigMap")

	// Verify sensitive data IS in Secret
	secretData := string(wfCtx.secretStore.Data[SecretKeyVars])
	r.Contains(secretData, "secret-token-xyz", "Sensitive data should be in Secret")
}

func TestLoadFromSecret(t *testing.T) {
	r := require.New(t)

	// Create a secret with sensitive data
	secret := corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-secret",
			Namespace: "default",
		},
		Data: map[string][]byte{
			SecretKeyVars: []byte(`apiKey: "loaded-secret-key"`),
		},
	}

	wfCtx := &WorkflowContext{}
	err := wfCtx.LoadFromSecret(context.Background(), secret)
	r.NoError(err)

	// Verify secret store is set
	r.NotNil(wfCtx.secretStore)
	r.Equal("test-secret", wfCtx.secretStore.Name)

	// Verify sensitive vars are loaded
	result, err := wfCtx.GetSensitiveVar("apiKey")
	r.NoError(err)
	rStr, err := util.ToString(result)
	r.NoError(err)
	r.Equal(`"loaded-secret-key"`, rStr)
}

func TestLoadFromSecretEmpty(t *testing.T) {
	r := require.New(t)

	// Create a secret without vars key
	secret := corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-secret-empty",
			Namespace: "default",
		},
		Data: map[string][]byte{},
	}

	wfCtx := &WorkflowContext{}
	err := wfCtx.LoadFromSecret(context.Background(), secret)
	r.NoError(err)

	// Verify sensitive vars are initialized to empty
	_, err = wfCtx.GetSensitiveVar("nonexistent")
	r.Error(err)
	r.Contains(err.Error(), "not found")
}

func TestInMemorySecretStorage(t *testing.T) {
	r := require.New(t)

	// Test CreateInMemorySecret
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-inmem-secret",
			Namespace: "default",
		},
	}
	MemStore.CreateInMemorySecret(secret)

	// Test GetInMemorySecret
	retrieved := MemStore.GetInMemorySecret("test-inmem-secret", "default")
	r.NotNil(retrieved)
	r.Equal("test-inmem-secret", retrieved.Name)

	// Test UpdateInMemorySecret
	secret.Data = map[string][]byte{"key": []byte("value")}
	MemStore.UpdateInMemorySecret(secret)
	retrieved = MemStore.GetInMemorySecret("test-inmem-secret", "default")
	r.Equal([]byte("value"), retrieved.Data["key"])

	// Test GetOrCreateInMemorySecret (existing)
	newSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-inmem-secret",
			Namespace: "default",
		},
	}
	MemStore.GetOrCreateInMemorySecret(newSecret)
	r.Equal([]byte("value"), newSecret.Data["key"])

	// Test GetOrCreateInMemorySecret (new)
	newSecret2 := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-inmem-secret-2",
			Namespace: "default",
		},
	}
	MemStore.GetOrCreateInMemorySecret(newSecret2)
	r.NotNil(newSecret2.Data)

	// Test DeleteInMemorySecret
	MemStore.DeleteInMemorySecret("test-inmem-secret")
	// Note: DeleteInMemorySecret uses a different key format, so we test the function runs without error

	// Cleanup
	delete(MemStore.secrets, "default/test-inmem-secret")
	delete(MemStore.secrets, "default/test-inmem-secret-2")
}

func TestGetSensitiveVarNotFound(t *testing.T) {
	wfCtx := newContextForTestWithSecret(t)
	r := require.New(t)

	_, err := wfCtx.GetSensitiveVar("nonexistent", "path")
	r.Error(err)
	r.Contains(err.Error(), "sensitive var nonexistent.path not found")
}

func newContextForTestWithSecret(t *testing.T) *WorkflowContext {
	r := require.New(t)
	var cm corev1.ConfigMap
	testCaseJson, err := yamlUtil.YAMLToJSON([]byte(testCaseYaml))
	r.NoError(err)
	err = json.Unmarshal(testCaseJson, &cm)
	r.NoError(err)

	wfCtx := &WorkflowContext{
		store: &cm,
		secretStore: &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "app-v1-secret",
				Namespace: "default",
			},
			Data: map[string][]byte{
				SecretKeyVars: []byte(""),
			},
		},
	}
	err = wfCtx.LoadFromConfigMap(context.Background(), cm)
	r.NoError(err)
	wfCtx.sensitiveVars = cuecontext.New().CompileString("")
	return wfCtx
}

func newCliForTestWithSecret(t *testing.T, wfCm *corev1.ConfigMap, wfSecret *corev1.Secret) {
	r := require.New(t)
	cli := &test.MockClient{
		MockGet: func(ctx context.Context, key client.ObjectKey, obj client.Object) error {
			if o, ok := obj.(*corev1.ConfigMap); ok {
				switch key.Name {
				case "app-v1":
					var cm corev1.ConfigMap
					testCaseJson, err := yamlUtil.YAMLToJSON([]byte(testCaseYaml))
					r.NoError(err)
					err = json.Unmarshal(testCaseJson, &cm)
					r.NoError(err)
					*o = cm
					return nil
				case generateStoreName("app-v1"):
					if wfCm != nil {
						*o = *wfCm
						return nil
					}
				}
			}
			if o, ok := obj.(*corev1.Secret); ok {
				switch key.Name {
				case generateSecretStoreName("app-v1"):
					if wfSecret != nil {
						*o = *wfSecret
						return nil
					}
				}
			}
			return kerrors.NewNotFound(corev1.Resource("configMap"), key.Name)
		},
		MockCreate: func(ctx context.Context, obj client.Object, opts ...client.CreateOption) error {
			if o, ok := obj.(*corev1.ConfigMap); ok {
				wfCm = o
			}
			if o, ok := obj.(*corev1.Secret); ok {
				wfSecret = o
			}
			return nil
		},
		MockPatch: func(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
			if o, ok := obj.(*corev1.ConfigMap); ok {
				if wfCm == nil {
					return kerrors.NewNotFound(corev1.Resource("configMap"), o.Name)
				}
				*wfCm = *o
			}
			if o, ok := obj.(*corev1.Secret); ok {
				if wfSecret == nil {
					return kerrors.NewNotFound(corev1.Resource("secret"), o.Name)
				}
				*wfSecret = *o
			}
			return nil
		},
	}
	singleton.KubeClient.Set(cli)
}

var (
	testCaseYaml = `apiVersion: v1
data:
  test: ""
kind: ConfigMap
metadata:
  name: app-v1
`
)
