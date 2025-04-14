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
	"fmt"
	"reflect"
	"strings"
	"sync"
	"time"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kubevela/pkg/util/rand"
	"github.com/kubevela/pkg/util/singleton"
	"github.com/kubevela/workflow/pkg/cue/model/sets"
	"github.com/kubevela/workflow/pkg/cue/model/value"
)

const (
	// ConfigMapKeyVars is the key in ConfigMap Data field for containing data of variable
	ConfigMapKeyVars = "vars"
	// AnnotationStartTimestamp is the annotation key of the workflow start  timestamp
	AnnotationStartTimestamp = "vela.io/startTime"
)

var (
	workflowMemoryCache sync.Map
)

// WorkflowContext is workflow context.
type WorkflowContext struct {
	store       *corev1.ConfigMap
	memoryStore *sync.Map
	vars        cue.Value
	modified    bool
}

// GetVar get variable from workflow context.
func (wf *WorkflowContext) GetVar(paths ...string) (cue.Value, error) {
	v := wf.vars.LookupPath(value.FieldPath(paths...))
	if !v.Exists() {
		return v, fmt.Errorf("var %s not found", strings.Join(paths, "."))
	}
	return v, nil
}

// SetVar set variable to workflow context.
func (wf *WorkflowContext) SetVar(v cue.Value, paths ...string) error {
	// convert value to string to set
	str, err := sets.ToString(v)
	if err != nil {
		return err
	}

	wf.vars, err = value.FillRaw(wf.vars, str, paths...)
	if err != nil {
		return err
	}
	if err := wf.vars.Err(); err != nil {
		return err
	}
	wf.modified = true
	return nil
}

// GetStore get store of workflow context.
func (wf *WorkflowContext) GetStore() *corev1.ConfigMap {
	return wf.store
}

// GetMutableValue get mutable data from workflow context.
func (wf *WorkflowContext) GetMutableValue(paths ...string) string {
	return wf.store.Data[strings.Join(paths, ".")]
}

// SetMutableValue set mutable data in workflow context config map.
func (wf *WorkflowContext) SetMutableValue(data string, paths ...string) {
	wf.store.Data[strings.Join(paths, ".")] = data
	wf.modified = true
}

// DeleteMutableValue delete mutable data in workflow context.
func (wf *WorkflowContext) DeleteMutableValue(paths ...string) {
	key := strings.Join(paths, ".")
	if _, ok := wf.store.Data[key]; ok {
		delete(wf.store.Data, strings.Join(paths, "."))
		wf.modified = true
	}
}

// IncreaseCountValueInMemory increase count in workflow context memory store.
func (wf *WorkflowContext) IncreaseCountValueInMemory(paths ...string) int {
	key := strings.Join(paths, ".")
	c, ok := wf.memoryStore.Load(key)
	if !ok {
		wf.memoryStore.Store(key, 0)
		return 0
	}
	count, ok := c.(int)
	if !ok {
		wf.memoryStore.Store(key, 0)
		return 0
	}
	count++
	wf.memoryStore.Store(key, count)
	return count
}

// SetValueInMemory set data in workflow context memory store.
func (wf *WorkflowContext) SetValueInMemory(data interface{}, paths ...string) {
	wf.memoryStore.Store(strings.Join(paths, "."), data)
}

// GetValueInMemory get data in workflow context memory store.
func (wf *WorkflowContext) GetValueInMemory(paths ...string) (interface{}, bool) {
	return wf.memoryStore.Load(strings.Join(paths, "."))
}

// DeleteValueInMemory delete data in workflow context memory store.
func (wf *WorkflowContext) DeleteValueInMemory(paths ...string) {
	wf.memoryStore.Delete(strings.Join(paths, "."))
}

// Commit the workflow context and persist it's content.
func (wf *WorkflowContext) Commit(ctx context.Context) error {
	if !wf.modified {
		return nil
	}
	if err := wf.writeToStore(); err != nil {
		return err
	}
	if err := wf.sync(ctx); err != nil {
		return errors.WithMessagef(err, "save context to configMap(%s/%s)", wf.store.Namespace, wf.store.Name)
	}
	return nil
}

func (wf *WorkflowContext) writeToStore() error {
	varStr, err := sets.ToString(wf.vars)
	if err != nil {
		return err
	}

	if wf.store.Data == nil {
		wf.store.Data = make(map[string]string)
	}

	wf.store.Data[ConfigMapKeyVars] = varStr
	return nil
}

func (wf *WorkflowContext) sync(ctx context.Context) error {
	cli := singleton.KubeClient.Get()
	store := &corev1.ConfigMap{}
	if EnableInMemoryContext {
		MemStore.UpdateInMemoryContext(wf.store)
	} else if err := cli.Get(ctx, types.NamespacedName{
		Name:      wf.store.Name,
		Namespace: wf.store.Namespace,
	}, store); err != nil {
		if kerrors.IsNotFound(err) {
			return cli.Create(ctx, wf.store)
		}
		return err
	}
	return cli.Patch(ctx, wf.store, client.MergeFrom(store.DeepCopy()))
}

// LoadFromConfigMap recover workflow context from configMap.
func (wf *WorkflowContext) LoadFromConfigMap(_ context.Context, cm corev1.ConfigMap) error {
	if wf.store == nil {
		wf.store = &cm
	}
	data := cm.Data

	wf.vars = cuecontext.New().CompileString(data[ConfigMapKeyVars])
	return nil
}

// StoreRef return the store reference of workflow context.
func (wf *WorkflowContext) StoreRef() *corev1.ObjectReference {
	return &corev1.ObjectReference{
		APIVersion: wf.store.APIVersion,
		Kind:       wf.store.Kind,
		Name:       wf.store.Name,
		Namespace:  wf.store.Namespace,
		UID:        wf.store.UID,
	}
}

// NewContext new workflow context without initialize data.
func NewContext(ctx context.Context, ns, name string, owner []metav1.OwnerReference) (Context, error) {
	wfCtx, err := newContext(ctx, ns, name, owner)
	if err != nil {
		return nil, err
	}

	return wfCtx, nil
}

// CleanupMemoryStore cleans up memory store.
func CleanupMemoryStore(name, ns string) {
	workflowMemoryCache.Delete(fmt.Sprintf("%s-%s", name, ns))
}

func newContext(ctx context.Context, ns, name string, owner []metav1.OwnerReference) (*WorkflowContext, error) {
	cli := singleton.KubeClient.Get()
	store := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:            generateStoreName(name),
			Namespace:       ns,
			OwnerReferences: owner,
		},
		Data: map[string]string{
			ConfigMapKeyVars: "",
		},
	}

	kindConfigMap := reflect.TypeOf(corev1.ConfigMap{}).Name()
	if EnableInMemoryContext {
		MemStore.GetOrCreateInMemoryContext(store)
	} else if err := cli.Get(ctx, client.ObjectKey{Name: store.Name, Namespace: store.Namespace}, store); err != nil {
		if kerrors.IsNotFound(err) {
			if err := cli.Create(ctx, store); err != nil {
				return nil, err
			}
			store.SetGroupVersionKind(corev1.SchemeGroupVersion.WithKind(kindConfigMap))
		} else {
			return nil, err
		}
	} else if !reflect.DeepEqual(store.OwnerReferences, owner) {
		store = &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:            fmt.Sprintf("%s-%s", generateStoreName(name), rand.RandomString(5)),
				Namespace:       ns,
				OwnerReferences: owner,
			},
			Data: map[string]string{
				ConfigMapKeyVars: "",
			},
		}
		if err := cli.Create(ctx, store); err != nil {
			return nil, err
		}
		store.SetGroupVersionKind(corev1.SchemeGroupVersion.WithKind(kindConfigMap))
	}
	store.Annotations = map[string]string{
		AnnotationStartTimestamp: time.Now().String(),
	}
	memCache := getMemoryStore(fmt.Sprintf("%s-%s", name, ns))
	wfCtx := &WorkflowContext{
		store:       store,
		memoryStore: memCache,
		modified:    true,
	}
	var err error
	wfCtx.vars = cuecontext.New().CompileString("")

	return wfCtx, err
}

func getMemoryStore(key string) *sync.Map {
	memCache := &sync.Map{}
	mc, ok := workflowMemoryCache.Load(key)
	if !ok {
		workflowMemoryCache.Store(key, memCache)
	} else {
		memCache, ok = mc.(*sync.Map)
		if !ok {
			workflowMemoryCache.Store(key, memCache)
		}
	}
	return memCache
}

// LoadContext load workflow context from store.
func LoadContext(ctx context.Context, ns, name, ctxName string) (Context, error) {
	var store corev1.ConfigMap
	store.Name = ctxName
	store.Namespace = ns
	cli := singleton.KubeClient.Get()
	if EnableInMemoryContext {
		MemStore.GetOrCreateInMemoryContext(&store)
	} else if err := cli.Get(context.Background(), client.ObjectKey{
		Namespace: ns,
		Name:      ctxName,
	}, &store); err != nil {
		return nil, err
	}
	memCache := getMemoryStore(fmt.Sprintf("%s-%s", name, ns))
	wfCtx := &WorkflowContext{
		store:       &store,
		memoryStore: memCache,
	}
	if err := wfCtx.LoadFromConfigMap(ctx, store); err != nil {
		return nil, err
	}
	return wfCtx, nil
}

// generateStoreName generates the config map name of workflow context.
func generateStoreName(name string) string {
	return fmt.Sprintf("workflow-%s-context", name)
}
