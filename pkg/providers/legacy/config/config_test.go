/*
Copyright 2024 The KubeVela Authors.

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

package config

import (
	"context"
	"strings"
	"testing"

	wfconfig "github.com/kubevela/workflow/pkg/config"
	providertypes "github.com/kubevela/workflow/pkg/providers/types"
)

type mockFactory struct {
	configs map[string]map[string]interface{}
}

func newMockFactory() *mockFactory {
	return &mockFactory{configs: make(map[string]map[string]interface{})}
}

func (m *mockFactory) ParseConfig(_ context.Context, _ wfconfig.NamespacedName, meta wfconfig.Metadata) (any, error) {
	return &meta, nil
}

func (m *mockFactory) CreateOrUpdateConfig(_ context.Context, configItem any, ns string) error {
	meta := configItem.(*wfconfig.Metadata)
	key := ns + "/" + meta.Name
	m.configs[key] = meta.Properties
	return nil
}

func (m *mockFactory) ReadConfig(_ context.Context, namespace, name string) (map[string]interface{}, error) {
	key := namespace + "/" + name
	if v, ok := m.configs[key]; ok {
		return v, nil
	}
	return nil, ErrRequestInvalid
}

func (m *mockFactory) ListConfigs(_ context.Context, _ string, _ string, _ string, _ bool) ([]*wfconfig.Item, error) {
	var items []*wfconfig.Item
	for key, props := range m.configs {
		parts := strings.SplitN(key, "/", 2)
		items = append(items, &wfconfig.Item{
			Name:       parts[1],
			Properties: props,
		})
	}
	return items, nil
}

func (m *mockFactory) DeleteConfig(_ context.Context, namespace, name string) error {
	key := namespace + "/" + name
	delete(m.configs, key)
	return nil
}

func TestLegacyGetTemplateNotEmpty(t *testing.T) {
	tmpl := GetTemplate()
	if tmpl == "" {
		t.Fatal("legacy config template is empty")
	}
	for _, name := range []string{"#CreateConfig", "#ReadConfig", "#DeleteConfig", "#ListConfig"} {
		if !strings.Contains(tmpl, name) {
			t.Errorf("template missing definition %s", name)
		}
	}
	// Legacy template must use "op" provider
	if !strings.Contains(tmpl, `#provider: "op"`) {
		t.Error("legacy template should use op provider")
	}
}

func TestLegacyGetProvidersRegistered(t *testing.T) {
	providers := GetProviders()
	for _, name := range []string{"create-config", "read-config", "list-config", "delete-config"} {
		if _, ok := providers[name]; !ok {
			t.Errorf("legacy provider %q not registered", name)
		}
	}
}

func TestLegacyCreateConfigNilFactory(t *testing.T) {
	params := &CreateParams{
		Params: CreateConfigProperties{
			Name:      "test",
			Namespace: "default",
			Config:    map[string]interface{}{"key": "val"},
		},
	}
	_, err := CreateConfig(context.Background(), params)
	if err != ErrConfigFactoryNotConfigured {
		t.Fatalf("expected ErrConfigFactoryNotConfigured, got %v", err)
	}
}

func TestLegacyCreateConfigWithFactory(t *testing.T) {
	factory := newMockFactory()
	params := &CreateParams{
		Params: CreateConfigProperties{
			Name:      "my-config",
			Namespace: "default",
			Template:  "ns1/tmpl1",
			Config:    map[string]interface{}{"foo": "bar"},
		},
		RuntimeParams: providertypes.RuntimeParams{
			ConfigFactory: factory,
		},
	}
	_, err := CreateConfig(context.Background(), params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := factory.configs["default/my-config"]; !ok {
		t.Fatal("config not created in factory")
	}
}

func TestLegacyReadConfigWithFactory(t *testing.T) {
	factory := newMockFactory()
	factory.configs["default/my-config"] = map[string]interface{}{"foo": "bar"}

	params := &providertypes.LegacyParams[wfconfig.NamespacedName]{
		Params: wfconfig.NamespacedName{Name: "my-config", Namespace: "default"},
		RuntimeParams: providertypes.RuntimeParams{
			ConfigFactory: factory,
		},
	}
	ret, err := ReadConfig(context.Background(), params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ret.Config["foo"] != "bar" {
		t.Fatalf("expected foo=bar, got %v", ret.Config)
	}
}

func TestLegacyDeleteConfigWithFactory(t *testing.T) {
	factory := newMockFactory()
	factory.configs["default/my-config"] = map[string]interface{}{"foo": "bar"}

	params := &providertypes.LegacyParams[wfconfig.NamespacedName]{
		Params: wfconfig.NamespacedName{Name: "my-config", Namespace: "default"},
		RuntimeParams: providertypes.RuntimeParams{
			ConfigFactory: factory,
		},
	}
	_, err := DeleteConfig(context.Background(), params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := factory.configs["default/my-config"]; ok {
		t.Fatal("config not deleted from factory")
	}
}
