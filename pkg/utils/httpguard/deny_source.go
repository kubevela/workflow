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

package httpguard

import (
	"context"
	"fmt"
	"sync/atomic"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

// PolicyEnhancer optionally mutates base policy (for example enabling
// BlockPrivate from a feature gate) before denylist merge.
type PolicyEnhancer func(Policy) Policy

var (
	denyFragment atomic.Value // stores Policy
	enhancer     atomic.Value // stores PolicyEnhancer
)

func init() {
	denyFragment.Store(Policy{ExactHosts: map[string]struct{}{}})
	enhancer.Store(PolicyEnhancer(func(p Policy) Policy { return p }))
}

// SetEnhancer registers a hook applied on every Current() call.
func SetEnhancer(fn PolicyEnhancer) {
	if fn == nil {
		fn = func(p Policy) Policy { return p }
	}
	enhancer.Store(fn)
}

// SetDenyFragment atomically replaces the denylist overlay.
func SetDenyFragment(fragment Policy) {
	if fragment.ExactHosts == nil {
		fragment.ExactHosts = map[string]struct{}{}
	}
	denyFragment.Store(fragment)
}

// Current returns DefaultPolicy, then enhancer, then denylist merge.
func Current() Policy {
	base := DefaultPolicy()
	if fn, ok := enhancer.Load().(PolicyEnhancer); ok && fn != nil {
		base = fn(base)
	}
	fragment, _ := denyFragment.Load().(Policy)
	return base.MergeDeny(fragment)
}

// LoadConfigMap reads name from namespace and installs the denylist fragment.
// Fail-closed: missing or invalid ConfigMaps return an error.
func LoadConfigMap(ctx context.Context, c client.Client, name, namespace string) error {
	if name == "" {
		SetDenyFragment(Policy{ExactHosts: map[string]struct{}{}})
		return nil
	}
	cm := &corev1.ConfigMap{}
	if err := c.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, cm); err != nil {
		return fmt.Errorf("get workflow HTTP deny ConfigMap %s/%s: %w", namespace, name, err)
	}
	fragment, err := ParseConfigMap(cm)
	if err != nil {
		return fmt.Errorf("parse workflow HTTP deny ConfigMap %s/%s: %w", namespace, name, err)
	}
	SetDenyFragment(fragment)
	return nil
}

// SetupWatcher registers a cache-backed ConfigMap watch in the controller
// namespace. After startup, parse failures keep the last good policy and log.
func SetupWatcher(mgr manager.Manager, name, namespace string) error {
	if name == "" {
		return nil
	}
	return mgr.Add(manager.RunnableFunc(func(ctx context.Context) error {
		return watchAndReload(ctx, mgr.GetClient(), mgr, name, namespace)
	}))
}

func watchAndReload(ctx context.Context, cli client.Client, mgr manager.Manager, name, namespace string) error {
	informer, err := mgr.GetCache().GetInformer(ctx, &corev1.ConfigMap{})
	if err != nil {
		return fmt.Errorf("get ConfigMap informer for HTTP deny watch: %w", err)
	}
	reload := func() {
		cm := &corev1.ConfigMap{}
		if err := cli.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, cm); err != nil {
			if apierrors.IsNotFound(err) {
				klog.ErrorS(err, "workflow HTTP deny ConfigMap deleted; keeping last good policy", "name", name, "namespace", namespace)
				return
			}
			klog.ErrorS(err, "failed to reload workflow HTTP deny ConfigMap", "name", name, "namespace", namespace)
			return
		}
		fragment, err := ParseConfigMap(cm)
		if err != nil {
			klog.ErrorS(err, "invalid workflow HTTP deny ConfigMap update; keeping last good policy", "name", name, "namespace", namespace)
			return
		}
		SetDenyFragment(fragment)
		klog.InfoS("reloaded workflow HTTP deny ConfigMap", "name", name, "namespace", namespace)
	}

	handler := &denyEventHandler{reload: reload, name: name, namespace: namespace}
	_, err = informer.AddEventHandler(handler)
	if err != nil {
		return err
	}
	<-ctx.Done()
	return nil
}

type denyEventHandler struct {
	reload          func()
	name, namespace string
}

var _ cache.ResourceEventHandler = &denyEventHandler{}

func (h *denyEventHandler) OnAdd(obj interface{})           { h.maybe(obj) }
func (h *denyEventHandler) OnUpdate(_, newObj interface{}) { h.maybe(newObj) }
func (h *denyEventHandler) OnDelete(obj interface{})       { h.maybe(obj) }

func (h *denyEventHandler) maybe(obj interface{}) {
	cm, ok := obj.(*corev1.ConfigMap)
	if !ok {
		if tombstone, ok := obj.(cache.DeletedFinalStateUnknown); ok {
			cm, ok = tombstone.Obj.(*corev1.ConfigMap)
			if !ok {
				return
			}
		} else {
			return
		}
	}
	if cm.Name != h.name || cm.Namespace != h.namespace {
		return
	}
	h.reload()
}
