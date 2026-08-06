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
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/cache"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestLoadConfigMap(t *testing.T) {
	t.Cleanup(func() {
		SetDenyFragment(Policy{ExactHosts: map[string]struct{}{}})
	})
	scheme := testScheme(t)
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "workflow-http-deny", Namespace: "vela-system"},
		Data: map[string]string{
			ConfigMapKeyDenyCIDRs: "10.0.0.0/8",
			ConfigMapKeyDenyHosts: "metadata.google.internal",
		},
	}
	cli := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cm).Build()
	require.NoError(t, LoadConfigMap(context.Background(), cli, "workflow-http-deny", "vela-system"))
	p := Current()
	require.True(t, p.Blocked(net.ParseIP("10.1.1.1")))
	require.Error(t, p.BlockedHost("metadata.google.internal"))
}

func TestLoadConfigMaps_mergesTemplateConfigs(t *testing.T) {
	t.Cleanup(func() {
		SetDenyFragment(Policy{ExactHosts: map[string]struct{}{}})
	})
	scheme := testScheme(t)
	defaultCM := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "workflow-http-deny-default",
			Namespace: "vela-system",
			Labels:    map[string]string{ConfigTemplateLabel: WorkflowHTTPDenyConfigTemplate},
		},
		Data: map[string]string{
			ConfigMapKeyDenyCIDRs: "169.254.0.0/16",
			ConfigMapKeyDenyHosts: "metadata.google.internal",
		},
	}
	extraCM := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "team-extra",
			Namespace: "vela-system",
			Labels:    map[string]string{ConfigTemplateLabel: WorkflowHTTPDenyConfigTemplate},
		},
		Data: map[string]string{
			ConfigMapKeyDenyHosts: "blocked.example",
		},
	}
	cli := fake.NewClientBuilder().WithScheme(scheme).WithObjects(defaultCM, extraCM).Build()
	require.NoError(t, LoadConfigMaps(context.Background(), cli, WorkflowHTTPDenyConfigTemplate, "", "vela-system"))
	p := Current()
	require.True(t, p.Blocked(net.ParseIP("169.254.169.254")))
	require.Error(t, p.BlockedHost("metadata.google.internal"))
	require.Error(t, p.BlockedHost("blocked.example"))
}

func TestLoadConfigMaps_deleteRemovesOnlyDeletedContribution(t *testing.T) {
	t.Cleanup(func() {
		SetDenyFragment(Policy{ExactHosts: map[string]struct{}{}})
	})
	scheme := testScheme(t)
	defaultCM := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "workflow-http-deny-default",
			Namespace: "vela-system",
			Labels:    map[string]string{ConfigTemplateLabel: WorkflowHTTPDenyConfigTemplate},
		},
		Data: map[string]string{ConfigMapKeyDenyHosts: "metadata.google.internal"},
	}
	extraCM := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "team-extra",
			Namespace: "vela-system",
			Labels:    map[string]string{ConfigTemplateLabel: WorkflowHTTPDenyConfigTemplate},
		},
		Data: map[string]string{ConfigMapKeyDenyHosts: "blocked.example"},
	}
	cli := fake.NewClientBuilder().WithScheme(scheme).WithObjects(defaultCM, extraCM).Build()
	require.NoError(t, LoadConfigMaps(context.Background(), cli, WorkflowHTTPDenyConfigTemplate, "", "vela-system"))

	require.NoError(t, cli.Delete(context.Background(), extraCM))
	tryReloadDenyConfigMaps(context.Background(), cli, WorkflowHTTPDenyConfigTemplate, "", "vela-system")

	p := Current()
	require.Error(t, p.BlockedHost("metadata.google.internal"))
	require.NoError(t, p.BlockedHost("blocked.example"))
}

func TestLoadConfigMap_emptyNameClearsFragment(t *testing.T) {
	fragment, err := ParseDenyList("", "blocked.example")
	require.NoError(t, err)
	SetDenyFragment(fragment)
	t.Cleanup(func() {
		SetDenyFragment(Policy{ExactHosts: map[string]struct{}{}})
	})
	require.NoError(t, LoadConfigMap(context.Background(), nil, "", "vela-system"))
	require.NoError(t, Current().BlockedHost("blocked.example"))
}

func TestLoadConfigMap_notFound(t *testing.T) {
	scheme := testScheme(t)
	cli := fake.NewClientBuilder().WithScheme(scheme).Build()
	err := LoadConfigMap(context.Background(), cli, "missing", "vela-system")
	require.Error(t, err)
	require.Contains(t, err.Error(), "get workflow HTTP deny ConfigMap")
}

func TestLoadConfigMaps_noneFoundUsesBuiltinFloor(t *testing.T) {
	t.Cleanup(func() {
		SetDenyFragment(Policy{ExactHosts: map[string]struct{}{}})
	})
	scheme := testScheme(t)
	cli := fake.NewClientBuilder().WithScheme(scheme).Build()
	require.NoError(t, LoadConfigMaps(context.Background(), cli, WorkflowHTTPDenyConfigTemplate, "", "vela-system"))
	p := Current()
	require.True(t, p.Blocked(net.ParseIP("169.254.169.254")))
	require.Error(t, p.BlockedHost("metadata.google.internal"))
}

func TestBuiltinDeny_alwaysPresent(t *testing.T) {
	t.Cleanup(func() {
		SetDenyFragment(Policy{ExactHosts: map[string]struct{}{}})
	})
	SetDenyFragment(Policy{ExactHosts: map[string]struct{}{}})
	p := Current()
	require.True(t, p.Blocked(net.ParseIP("169.254.169.254")))
	require.True(t, p.Blocked(net.ParseIP("fd00:ec2::254")))
	require.True(t, p.Blocked(net.ParseIP("100.100.100.200")))
	require.Error(t, p.BlockedHost("metadata.google.internal"))
	require.NoError(t, builtinDenyLoadError())
}

func TestSetEnhancer_nilUsesIdentity(t *testing.T) {
	t.Cleanup(func() {
		SetEnhancer(nil)
	})
	SetEnhancer(nil)
	cur := Current()
	require.False(t, cur.BlockPrivate)
}

func TestSetDenyFragment_nilExactHosts(t *testing.T) {
	t.Cleanup(func() {
		SetDenyFragment(Policy{ExactHosts: map[string]struct{}{}})
	})
	SetDenyFragment(Policy{})
	cur := Current()
	require.NotNil(t, cur.ExactHosts)
}

func TestSetupWatcher_emptyName(t *testing.T) {
	require.NoError(t, SetupWatcher(nil, "", "", "vela-system"))
}

func TestDenyEventHandler_triggersReload(t *testing.T) {
	var reloads int
	h := &denyEventHandler{
		reload:       func() { reloads++ },
		templateName: WorkflowHTTPDenyConfigTemplate,
		legacyName:   "workflow-http-deny",
		namespace:    "vela-system",
	}
	h.OnAdd(&corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "team-extra",
			Namespace: "vela-system",
			Labels:    map[string]string{ConfigTemplateLabel: WorkflowHTTPDenyConfigTemplate},
		},
	}, false)
	require.Equal(t, 1, reloads)

	h.OnUpdate(nil, &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "workflow-http-deny", Namespace: "vela-system"},
	})
	require.Equal(t, 2, reloads)

	h.OnDelete(&corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "team-extra",
			Namespace: "vela-system",
			Labels:    map[string]string{ConfigTemplateLabel: WorkflowHTTPDenyConfigTemplate},
		},
	})
	require.Equal(t, 3, reloads)
}

func TestDenyEventHandler_ignoresOtherConfigMaps(t *testing.T) {
	var reloads int
	h := &denyEventHandler{
		reload:       func() { reloads++ },
		templateName: WorkflowHTTPDenyConfigTemplate,
		legacyName:   "workflow-http-deny",
		namespace:    "vela-system",
	}
	h.OnAdd(&corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "other", Namespace: "vela-system"},
	}, false)
	require.Equal(t, 0, reloads)
}

func TestDenyEventHandler_deletedFinalStateUnknown(t *testing.T) {
	var reloads int
	h := &denyEventHandler{
		reload:       func() { reloads++ },
		templateName: WorkflowHTTPDenyConfigTemplate,
		namespace:    "vela-system",
	}
	h.OnDelete(cache.DeletedFinalStateUnknown{
		Obj: &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "team-extra",
				Namespace: "vela-system",
				Labels:    map[string]string{ConfigTemplateLabel: WorkflowHTTPDenyConfigTemplate},
			},
		},
	})
	require.Equal(t, 1, reloads)
}

func TestDenyEventHandler_ignoresInvalidObjects(t *testing.T) {
	var reloads int
	h := &denyEventHandler{
		reload:       func() { reloads++ },
		templateName: WorkflowHTTPDenyConfigTemplate,
		namespace:    "vela-system",
	}
	h.OnAdd("not-a-configmap", false)
	h.OnDelete(cache.DeletedFinalStateUnknown{Obj: "still-not-a-configmap"})
	require.Equal(t, 0, reloads)
}

func TestTryReloadDenyConfigMaps_success(t *testing.T) {
	t.Cleanup(func() {
		SetDenyFragment(Policy{ExactHosts: map[string]struct{}{}})
	})
	scheme := testScheme(t)
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "reloaded",
			Namespace: "vela-system",
			Labels:    map[string]string{ConfigTemplateLabel: WorkflowHTTPDenyConfigTemplate},
		},
		Data: map[string]string{ConfigMapKeyDenyHosts: "reloaded.example"},
	}
	cli := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cm).Build()
	tryReloadDenyConfigMaps(context.Background(), cli, WorkflowHTTPDenyConfigTemplate, "", "vela-system")
	require.Error(t, Current().BlockedHost("reloaded.example"))
}

func TestTryReloadDenyConfigMaps_keepsLastGood(t *testing.T) {
	t.Cleanup(func() {
		SetDenyFragment(Policy{ExactHosts: map[string]struct{}{}})
	})
	scheme := testScheme(t)
	good := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "good",
			Namespace: "vela-system",
			Labels:    map[string]string{ConfigTemplateLabel: WorkflowHTTPDenyConfigTemplate},
		},
		Data: map[string]string{ConfigMapKeyDenyHosts: "good.example"},
	}
	cli := fake.NewClientBuilder().WithScheme(scheme).WithObjects(good).Build()
	require.NoError(t, LoadConfigMaps(context.Background(), cli, WorkflowHTTPDenyConfigTemplate, "", "vela-system"))

	bad := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "bad",
			Namespace: "vela-system",
			Labels:    map[string]string{ConfigTemplateLabel: WorkflowHTTPDenyConfigTemplate},
		},
		Data: map[string]string{ConfigMapKeyDenyCIDRs: "not-a-cidr"},
	}
	require.NoError(t, cli.Create(context.Background(), bad))
	tryReloadDenyConfigMaps(context.Background(), cli, WorkflowHTTPDenyConfigTemplate, "", "vela-system")
	require.Error(t, Current().BlockedHost("good.example"))
}

func TestLoadConfigMap_invalid(t *testing.T) {
	scheme := testScheme(t)
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "bad", Namespace: "vela-system"},
		Data: map[string]string{
			ConfigMapKeyDenyCIDRs: "invalid-cidr",
		},
	}
	cli := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cm).Build()
	err := LoadConfigMap(context.Background(), cli, "bad", "vela-system")
	require.Error(t, err)
}

func testScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(s))
	return s
}

type stubInformer struct {
	addErr error
}

type stubRegistration struct{}

func (stubRegistration) HasSynced() bool { return true }
func (stubRegistration) Remove() error   { return nil }

func (s *stubInformer) AddEventHandler(_ cache.ResourceEventHandler) (cache.ResourceEventHandlerRegistration, error) {
	return stubRegistration{}, s.addErr
}

func TestWatchAndReloadInformer_stopsOnCancel(t *testing.T) {
	scheme := testScheme(t)
	cli := fake.NewClientBuilder().WithScheme(scheme).Build()
	ctx, cancel := context.WithCancel(context.Background())

	errCh := make(chan error, 1)
	go func() {
		errCh <- watchAndReloadInformer(ctx, cli, &stubInformer{}, WorkflowHTTPDenyConfigTemplate, "", "vela-system")
	}()

	require.Eventually(t, func() bool {
		select {
		case <-errCh:
			return false
		default:
			return true
		}
	}, time.Second, 10*time.Millisecond)

	cancel()

	select {
	case err := <-errCh:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("watch did not stop after context cancellation")
	}
}

func TestWatchAndReloadInformer_addHandlerError(t *testing.T) {
	scheme := testScheme(t)
	cli := fake.NewClientBuilder().WithScheme(scheme).Build()
	ctx := context.Background()

	err := watchAndReloadInformer(ctx, cli, &stubInformer{addErr: fmt.Errorf("add failed")}, WorkflowHTTPDenyConfigTemplate, "", "vela-system")
	require.Error(t, err)
	require.Contains(t, err.Error(), "add failed")
}
