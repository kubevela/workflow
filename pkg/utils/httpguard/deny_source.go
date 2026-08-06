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
	"sort"
	"sync/atomic"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

const (
	// ConfigTemplateLabel identifies the ConfigTemplate that produced a config.
	ConfigTemplateLabel = "config.oam.dev/type"
	// WorkflowHTTPDenyConfigTemplate is the workflow HTTP deny ConfigTemplate name.
	WorkflowHTTPDenyConfigTemplate = "workflow-http-deny"
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

// Current returns DefaultPolicy, then enhancer, then the immutable builtin
// denylist floor, then any discovered ConfigMap deny overlay.
func Current() Policy {
	base := DefaultPolicy()
	if fn, ok := enhancer.Load().(PolicyEnhancer); ok && fn != nil {
		base = fn(base)
	}
	base = base.MergeDeny(BuiltinDeny())
	fragment, _ := denyFragment.Load().(Policy)
	return base.MergeDeny(fragment)
}

// LoadConfigMap reads one legacy, explicitly named ConfigMap.
func LoadConfigMap(ctx context.Context, c client.Reader, name, namespace string) error {
	return LoadConfigMaps(ctx, c, "", name, namespace)
}

// LoadConfigMaps discovers ConfigMaps produced by templateName, optionally
// includes one legacy explicitly named ConfigMap, and atomically installs the
// union of all deny entries on top of BuiltinDeny. A missing legacy ConfigMap
// fails closed. Zero discovered labeled ConfigMaps is allowed and leaves only
// the builtin floor in effect.
func LoadConfigMaps(ctx context.Context, c client.Reader, templateName, legacyName, namespace string) error {
	if err := builtinDenyLoadError(); err != nil {
		return err
	}
	configMaps, err := listDenyConfigMaps(ctx, c, templateName, legacyName, namespace)
	if err != nil {
		return err
	}
	fragment, err := mergeDenyConfigMaps(configMaps)
	if err != nil {
		return err
	}
	SetDenyFragment(fragment)
	return nil
}

func listDenyConfigMaps(ctx context.Context, c client.Reader, templateName, legacyName, namespace string) ([]corev1.ConfigMap, error) {
	configMaps := make([]corev1.ConfigMap, 0)
	seen := map[types.NamespacedName]struct{}{}
	if templateName != "" {
		var list corev1.ConfigMapList
		if err := c.List(ctx, &list, client.InNamespace(namespace), client.MatchingLabels{
			ConfigTemplateLabel: templateName,
		}); err != nil {
			return nil, fmt.Errorf("list workflow HTTP deny ConfigMaps for template %q in namespace %s: %w", templateName, namespace, err)
		}
		for _, cm := range list.Items {
			key := types.NamespacedName{Name: cm.Name, Namespace: cm.Namespace}
			seen[key] = struct{}{}
			configMaps = append(configMaps, cm)
		}
	}
	if legacyName != "" {
		key := types.NamespacedName{Name: legacyName, Namespace: namespace}
		if _, ok := seen[key]; !ok {
			var cm corev1.ConfigMap
			if err := c.Get(ctx, key, &cm); err != nil {
				return nil, fmt.Errorf("get workflow HTTP deny ConfigMap %s: %w", key, err)
			}
			configMaps = append(configMaps, cm)
		}
	}
	sort.Slice(configMaps, func(i, j int) bool {
		return configMaps[i].Name < configMaps[j].Name
	})
	return configMaps, nil
}

func mergeDenyConfigMaps(configMaps []corev1.ConfigMap) (Policy, error) {
	merged := Policy{ExactHosts: map[string]struct{}{}}
	for i := range configMaps {
		fragment, err := ParseConfigMap(&configMaps[i])
		if err != nil {
			return Policy{}, fmt.Errorf("parse workflow HTTP deny ConfigMap %s/%s: %w",
				configMaps[i].Namespace, configMaps[i].Name, err)
		}
		merged = merged.MergeDeny(fragment)
	}
	return merged, nil
}

// SetupWatcher registers a cache-backed ConfigMap watch in the controller
// namespace. After startup, reload failures keep the last good aggregate.
func SetupWatcher(mgr manager.Manager, templateName, legacyName, namespace string) error {
	if templateName == "" && legacyName == "" {
		return nil
	}
	if mgr == nil {
		return nil
	}
	return mgr.Add(manager.RunnableFunc(func(ctx context.Context) error {
		return watchAndReload(ctx, mgr.GetClient(), mgr, templateName, legacyName, namespace)
	}))
}

func watchAndReload(ctx context.Context, cli client.Client, mgr manager.Manager, templateName, legacyName, namespace string) error {
	informer, err := mgr.GetCache().GetInformer(ctx, &corev1.ConfigMap{})
	if err != nil {
		return fmt.Errorf("get ConfigMap informer for HTTP deny watch: %w", err)
	}
	return watchAndReloadInformer(ctx, cli, informer, templateName, legacyName, namespace)
}

type configMapInformer interface {
	AddEventHandler(handler cache.ResourceEventHandler) (cache.ResourceEventHandlerRegistration, error)
}

func watchAndReloadInformer(ctx context.Context, cli client.Client, informer configMapInformer, templateName, legacyName, namespace string) error {
	reload := func() {
		tryReloadDenyConfigMaps(ctx, cli, templateName, legacyName, namespace)
	}

	handler := &denyEventHandler{
		reload:       reload,
		templateName: templateName,
		legacyName:   legacyName,
		namespace:    namespace,
	}
	_, err := informer.AddEventHandler(handler)
	if err != nil {
		return err
	}
	<-ctx.Done()
	return nil
}

func tryReloadDenyConfigMaps(ctx context.Context, cli client.Client, templateName, legacyName, namespace string) {
	if err := LoadConfigMaps(ctx, cli, templateName, legacyName, namespace); err != nil {
		klog.ErrorS(err, "failed to reload workflow HTTP deny ConfigMaps; keeping last good policy",
			"template", templateName, "legacyName", legacyName, "namespace", namespace)
		return
	}
	klog.InfoS("reloaded workflow HTTP deny ConfigMaps",
		"template", templateName, "legacyName", legacyName, "namespace", namespace)
}

type denyEventHandler struct {
	reload                   func()
	templateName, legacyName string
	namespace                string
}

var _ cache.ResourceEventHandler = &denyEventHandler{}

func (h *denyEventHandler) OnAdd(obj interface{}, _ bool) { h.maybe(obj) }
func (h *denyEventHandler) OnUpdate(oldObj, newObj interface{}) {
	if h.matches(oldObj) || h.matches(newObj) {
		h.reload()
	}
}
func (h *denyEventHandler) OnDelete(obj interface{}) { h.maybe(obj) }

func (h *denyEventHandler) maybe(obj interface{}) {
	if h.matches(obj) {
		h.reload()
	}
}

func (h *denyEventHandler) matches(obj interface{}) bool {
	cm, ok := obj.(*corev1.ConfigMap)
	if !ok {
		if tombstone, ok := obj.(cache.DeletedFinalStateUnknown); ok {
			cm, ok = tombstone.Obj.(*corev1.ConfigMap)
			if !ok {
				return false
			}
		} else {
			return false
		}
	}
	if cm.Namespace != h.namespace {
		return false
	}
	if h.legacyName != "" && cm.Name == h.legacyName {
		return true
	}
	return h.templateName != "" && cm.Labels[ConfigTemplateLabel] == h.templateName
}
