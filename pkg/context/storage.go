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
	"sync"

	v1 "k8s.io/api/core/v1"

	"github.com/kubevela/pkg/cache"
)

var (
	// EnableInMemoryContext optimize workflow context storage by storing it in memory instead of etcd
	EnableInMemoryContext = false
)

// inMemoryContextStorage holds live workflow contexts, not a disposable cache:
// entries are stored without expiration and are only removed explicitly.
// It keeps its own copies, so callers can keep changing the ConfigMaps they
// pass in or get back without racing with other readers.
type inMemoryContextStorage struct {
	// mu serializes writes so that GetOrCreateInMemoryContext is atomic.
	mu       sync.Mutex
	contexts cache.Cache[string]
}

// MemStore store in-memory context
var MemStore = &inMemoryContextStorage{
	contexts: cache.NewMemoryCacheStore[string](context.Background()),
}

func contextKey(name, ns string) string {
	if ns == "" {
		ns = "default"
	}
	return ns + "/" + name
}

func (o *inMemoryContextStorage) getKey(cm *v1.ConfigMap) string {
	return contextKey(cm.GetName(), cm.GetNamespace())
}

func (o *inMemoryContextStorage) load(key string) *v1.ConfigMap {
	if obj, ok := o.contexts.Get(key); ok {
		return obj.(*v1.ConfigMap)
	}
	return nil
}

func (o *inMemoryContextStorage) GetOrCreateInMemoryContext(cm *v1.ConfigMap) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if obj := o.load(o.getKey(cm)); obj != nil {
		obj.DeepCopyInto(cm)
		return
	}
	cm.Data = map[string]string{}
	o.contexts.Put(o.getKey(cm), cm.DeepCopy(), 0)
}

func (o *inMemoryContextStorage) GetInMemoryContext(name, ns string) *v1.ConfigMap {
	return o.load(contextKey(name, ns)).DeepCopy()
}

func (o *inMemoryContextStorage) CreateInMemoryContext(cm *v1.ConfigMap) {
	o.mu.Lock()
	defer o.mu.Unlock()
	cm.Data = map[string]string{}
	o.contexts.Put(o.getKey(cm), cm.DeepCopy(), 0)
}

func (o *inMemoryContextStorage) UpdateInMemoryContext(cm *v1.ConfigMap) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.contexts.Put(o.getKey(cm), cm.DeepCopy(), 0)
}

// DeleteInMemoryContext drops the stored context of an application. The key
// has to be built the same way getKey builds it, with the namespace included,
// or the delete silently matches nothing.
func (o *inMemoryContextStorage) DeleteInMemoryContext(appName, namespace string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.contexts.Delete(contextKey(generateStoreName(appName), namespace))
}
