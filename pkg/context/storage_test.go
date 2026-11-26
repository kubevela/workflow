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
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func newStoreForTest() *inMemoryContextStorage {
	return &inMemoryContextStorage{contexts: map[string]*corev1.ConfigMap{}}
}

func newConfigMap(name, ns string) *corev1.ConfigMap {
	return &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns}}
}

func TestDeleteInMemoryContext(t *testing.T) {
	r := require.New(t)
	store := newStoreForTest()

	store.CreateInMemoryContext(newConfigMap(generateStoreName("app"), "prod"))
	store.CreateInMemoryContext(newConfigMap(generateStoreName("app"), "staging"))
	r.NotNil(store.GetInMemoryContext(generateStoreName("app"), "prod"))

	store.DeleteInMemoryContext("app", "prod")
	r.Nil(store.GetInMemoryContext(generateStoreName("app"), "prod"), "delete should remove the stored context")
	r.NotNil(store.GetInMemoryContext(generateStoreName("app"), "staging"), "delete should leave another namespace's context alone")

	// The key is defaulted the same way reads and writes default it, in both
	// directions: stored under "", deleted as "default", and the reverse.
	store.CreateInMemoryContext(newConfigMap(generateStoreName("app"), ""))
	store.DeleteInMemoryContext("app", "default")
	r.Nil(store.GetInMemoryContext(generateStoreName("app"), "default"), "an empty namespace is the default namespace")

	store.CreateInMemoryContext(newConfigMap(generateStoreName("app"), "default"))
	store.DeleteInMemoryContext("app", "")
	r.Nil(store.GetInMemoryContext(generateStoreName("app"), ""), "delete should default an empty namespace the same way")

	// Deleting something that was never stored is a no-op.
	store.DeleteInMemoryContext("missing", "prod")
	r.Nil(store.GetInMemoryContext(generateStoreName("missing"), "prod"))
}
