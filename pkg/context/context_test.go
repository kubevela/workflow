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

var (
	testCaseYaml = `apiVersion: v1
data:
  test: ""
kind: ConfigMap
metadata:
  name: app-v1
`
)
