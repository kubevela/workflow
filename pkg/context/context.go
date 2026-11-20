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
	// SecretKeyVars is the key in the companion Secret Data field that holds
	// sensitive workflow variables (step outputs derived from Secrets).
	SecretKeyVars = "vars"
	// SensitiveStoreSuffix is appended to the context ConfigMap name to build
	// the name of the companion Secret that stores sensitive variables.
	SensitiveStoreSuffix = "-sensitive"
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

	// sensitiveVars holds variables that were marked sensitive (e.g. step
	// outputs whose values come from Kubernetes Secrets). They are persisted
	// to a companion Secret instead of the plaintext context ConfigMap so that
	// ConfigMap readers can never see them.
	sensitiveVars cue.Value
	// hasSensitive records that sensitive vars were ever set or loaded, so an
	// existing companion Secret is kept in sync (including being cleared)
	// while workflows without sensitive data never create one.
	hasSensitive bool
}

// GetVar get variable from workflow context. Sensitive variables are read
// transparently, so step inputs keep working regardless of where a variable
// is stored.
func (wf *WorkflowContext) GetVar(paths ...string) (cue.Value, error) {
	v := wf.vars.LookupPath(value.FieldPath(paths...))
	if !v.Exists() {
		if sv := wf.sensitiveVars.LookupPath(value.FieldPath(paths...)); sv.Exists() {
			return sv, nil
		}
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

// SetSensitiveVar sets a variable whose value is sensitive (e.g. derived from
// a Kubernetes Secret). It behaves exactly like SetVar for readers, but the
// value is persisted to a companion Secret instead of the plaintext context
// ConfigMap. See kubevela/kubevela#6840 for the class of leak this prevents.
func (wf *WorkflowContext) SetSensitiveVar(v cue.Value, paths ...string) error {
	str, err := sets.ToString(v)
	if err != nil {
		return err
	}

	wf.sensitiveVars, err = value.FillRaw(wf.sensitiveVars, str, paths...)
	if err != nil {
		return err
	}
	if err := wf.sensitiveVars.Err(); err != nil {
		return err
	}
	wf.hasSensitive = true
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

	// Sensitive variables are intentionally NOT written into the ConfigMap
	// data; they are persisted by syncSensitive to a companion Secret.
	wf.store.Data[ConfigMapKeyVars] = varStr
	return nil
}

func (wf *WorkflowContext) sync(ctx context.Context) error {
	cli := singleton.KubeClient.Get()
	store := &corev1.ConfigMap{}
	if EnableInMemoryContext {
		// The in-memory store never reaches the API server, so sensitive vars
		// can safely ride in the same in-memory ConfigMap object.
		if err := wf.stashSensitiveInMemory(); err != nil {
			return err
		}
		MemStore.UpdateInMemoryContext(wf.store)
		return nil
	}
	if err := wf.syncSensitive(ctx, cli); err != nil {
		return errors.WithMessagef(err, "save sensitive context to secret(%s/%s)", wf.store.Namespace, wf.sensitiveStoreName())
	}
	if err := cli.Get(ctx, types.NamespacedName{
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

// sensitiveStoreName returns the name of the companion Secret that stores
// sensitive variables for this context.
func (wf *WorkflowContext) sensitiveStoreName() string {
	return wf.store.Name + SensitiveStoreSuffix
}

// stashSensitiveInMemory keeps sensitive vars inside the in-memory ConfigMap
// object (memory-only mode never persists to the API server, so this is safe).
func (wf *WorkflowContext) stashSensitiveInMemory() error {
	if !wf.hasSensitive {
		return nil
	}
	sensStr, err := sets.ToString(wf.sensitiveVars)
	if err != nil {
		return err
	}
	if wf.store.Data == nil {
		wf.store.Data = make(map[string]string)
	}
	wf.store.Data[inMemorySensitiveKey] = sensStr
	return nil
}

// inMemorySensitiveKey is only ever used in EnableInMemoryContext mode, where
// the ConfigMap object never leaves process memory.
const inMemorySensitiveKey = "sensitiveVars"

// syncSensitive persists sensitive variables to the companion Secret. A Secret
// is only created once sensitive data exists; afterwards it is kept in sync on
// every commit — including being emptied when the sensitive vars are gone — so
// stale credentials are never left behind. Errors fail the Commit (workflow
// reconciliation retries), and sensitive data is NEVER written to the
// ConfigMap as a fallback.
func (wf *WorkflowContext) syncSensitive(ctx context.Context, cli client.Client) error {
	if !wf.hasSensitive {
		return nil
	}
	sensStr, err := sets.ToString(wf.sensitiveVars)
	if err != nil {
		return err
	}
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:            wf.sensitiveStoreName(),
			Namespace:       wf.store.Namespace,
			OwnerReferences: wf.store.OwnerReferences,
			Labels:          wf.store.Labels,
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			SecretKeyVars: []byte(sensStr),
		},
	}
	existing := &corev1.Secret{}
	if err := cli.Get(ctx, types.NamespacedName{
		Name:      secret.Name,
		Namespace: secret.Namespace,
	}, existing); err != nil {
		if kerrors.IsNotFound(err) {
			return cli.Create(ctx, secret)
		}
		return err
	}
	return cli.Patch(ctx, secret, client.MergeFrom(existing.DeepCopy()))
}

// LoadFromConfigMap recover workflow context from configMap.
func (wf *WorkflowContext) LoadFromConfigMap(_ context.Context, cm corev1.ConfigMap) error {
	if wf.store == nil {
		wf.store = &cm
	}
	data := cm.Data

	wf.vars = cuecontext.New().CompileString(data[ConfigMapKeyVars])
	wf.sensitiveVars = cuecontext.New().CompileString("")
	// In-memory mode stashes sensitive vars inside the (never persisted)
	// ConfigMap object; recover them on reload.
	if sens, ok := data[inMemorySensitiveKey]; ok {
		wf.sensitiveVars = cuecontext.New().CompileString(sens)
		wf.hasSensitive = true
	}
	return nil
}

// loadSensitiveFromSecret recovers sensitive variables from the companion
// Secret, if one exists. Absence is not an error: workflows without sensitive
// outputs never create the Secret.
func (wf *WorkflowContext) loadSensitiveFromSecret(ctx context.Context) error {
	if EnableInMemoryContext {
		return nil
	}
	cli := singleton.KubeClient.Get()
	secret := &corev1.Secret{}
	if err := cli.Get(ctx, types.NamespacedName{
		Name:      wf.sensitiveStoreName(),
		Namespace: wf.store.Namespace,
	}, secret); err != nil {
		if kerrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	wf.sensitiveVars = cuecontext.New().CompileString(string(secret.Data[SecretKeyVars]))
	wf.hasSensitive = true
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
	wfCtx.sensitiveVars = cuecontext.New().CompileString("")

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
	if err := wfCtx.loadSensitiveFromSecret(ctx); err != nil {
		return nil, err
	}
	return wfCtx, nil
}

// generateStoreName generates the config map name of workflow context.
func generateStoreName(name string) string {
	return fmt.Sprintf("workflow-%s-context", name)
}
