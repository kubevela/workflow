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
	"errors"
	"fmt"
	"sync"
	"testing"

	"cuelang.org/go/cue/cuecontext"
	"github.com/crossplane/crossplane-runtime/pkg/test"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kubevela/pkg/cache"
	"github.com/kubevela/pkg/util/singleton"
)

func newStoreForTest() *inMemoryContextStorage {
	return &inMemoryContextStorage{
		contexts: cache.NewMemoryCacheStore[string](context.Background()),
	}
}

func newConfigMap(name, ns string) *corev1.ConfigMap {
	return &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns}}
}

func TestInMemoryContextStorage(t *testing.T) {
	r := require.New(t)
	store := newStoreForTest()

	r.Nil(store.GetInMemoryContext("workflow-app-context", "prod"))

	cm := newConfigMap("workflow-app-context", "prod")
	cm.Data = map[string]string{"stale": "value"}
	store.CreateInMemoryContext(cm)
	r.Empty(cm.Data, "create should reset the context data")
	r.Same(cm, store.GetInMemoryContext("workflow-app-context", "prod"))
	r.Nil(store.GetInMemoryContext("workflow-app-context", "other"))

	updated := newConfigMap("workflow-app-context", "prod")
	updated.Data = map[string]string{ConfigMapKeyVars: "vars"}
	store.UpdateInMemoryContext(updated)
	r.Equal("vars", store.GetInMemoryContext("workflow-app-context", "prod").Data[ConfigMapKeyVars])

	// An empty namespace resolves to "default" for both reads and writes.
	store.CreateInMemoryContext(newConfigMap("workflow-app-context", ""))
	r.NotNil(store.GetInMemoryContext("workflow-app-context", ""))
	r.NotNil(store.GetInMemoryContext("workflow-app-context", "default"))
}

func TestGetOrCreateInMemoryContext(t *testing.T) {
	r := require.New(t)
	store := newStoreForTest()

	created := newConfigMap("workflow-app-context", "prod")
	store.GetOrCreateInMemoryContext(created)
	r.NotNil(created.Data)
	created.Data[ConfigMapKeyVars] = "vars"

	loaded := newConfigMap("workflow-app-context", "prod")
	store.GetOrCreateInMemoryContext(loaded)
	r.Equal("vars", loaded.Data[ConfigMapKeyVars])
	r.NotSame(created, loaded, "existing context should be copied, not shared")
}

func TestInMemoryContextStorageConcurrentGetOrCreate(t *testing.T) {
	r := require.New(t)
	store := newStoreForTest()

	const workers = 16
	start := make(chan struct{})
	results := make([]*corev1.ConfigMap, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			cm := newConfigMap("workflow-app-context", "prod")
			cm.Annotations = map[string]string{"worker": fmt.Sprint(i)}
			<-start
			store.GetOrCreateInMemoryContext(cm)
			results[i] = cm
		}(i)
	}
	close(start)
	wg.Wait()

	stored := store.GetInMemoryContext("workflow-app-context", "prod")
	r.NotNil(stored)
	winner := stored.Annotations["worker"]
	for i, cm := range results {
		r.Equal(winner, cm.Annotations["worker"], "worker %d did not get the stored context", i)
	}
}

func TestWorkflowContextInMemory(t *testing.T) {
	r := require.New(t)
	ctx := context.Background()

	origEnabled, origStore := EnableInMemoryContext, MemStore
	t.Cleanup(func() { EnableInMemoryContext, MemStore = origEnabled, origStore })
	EnableInMemoryContext = true
	MemStore = newStoreForTest()

	// With in-memory context enabled, the context must not be read from or created in the API server.
	errUnexpected := errors.New("unexpected API server call")
	singleton.KubeClient.Set(&test.MockClient{
		MockGet:    test.NewMockGetFn(errUnexpected),
		MockCreate: test.NewMockCreateFn(errUnexpected),
		MockPatch:  test.NewMockPatchFn(nil),
	})

	wfCtx, err := NewContext(ctx, "prod", "app", nil)
	r.NoError(err)
	r.NoError(wfCtx.SetVar(cuecontext.New().CompileString(`"hello"`), "greeting"))
	r.NoError(wfCtx.Commit(ctx))
	r.NotNil(MemStore.GetInMemoryContext(generateStoreName("app"), "prod"))

	loaded, err := LoadContext(ctx, "prod", "app", generateStoreName("app"))
	r.NoError(err)
	v, err := loaded.GetVar("greeting")
	r.NoError(err)
	s, err := v.String()
	r.NoError(err)
	r.Equal("hello", s)
}
