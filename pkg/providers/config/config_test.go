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

// mockFactory is a minimal config.Factory for testing.
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

func TestGetTemplateNotEmpty(t *testing.T) {
	tmpl := GetTemplate()
	if tmpl == "" {
		t.Fatal("config template is empty")
	}
	for _, name := range []string{"#CreateConfig", "#ReadConfig", "#DeleteConfig", "#ListConfig"} {
		if !strings.Contains(tmpl, name) {
			t.Errorf("template missing definition %s", name)
		}
	}
}

func TestGetProvidersRegistered(t *testing.T) {
	providers := GetProviders()
	for _, name := range []string{"create", "read", "list", "delete"} {
		if _, ok := providers[name]; !ok {
			t.Errorf("provider %q not registered", name)
		}
	}
}

func TestCreateConfigNilFactory(t *testing.T) {
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

func TestCreateConfigWithFactory(t *testing.T) {
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

func TestReadConfigWithFactory(t *testing.T) {
	factory := newMockFactory()
	factory.configs["default/my-config"] = map[string]interface{}{"foo": "bar"}

	params := &providertypes.Params[wfconfig.NamespacedName]{
		Params: wfconfig.NamespacedName{Name: "my-config", Namespace: "default"},
		RuntimeParams: providertypes.RuntimeParams{
			ConfigFactory: factory,
		},
	}
	ret, err := ReadConfig(context.Background(), params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ret.Returns.Config["foo"] != "bar" {
		t.Fatalf("expected foo=bar, got %v", ret.Returns.Config)
	}
}

func TestDeleteConfigWithFactory(t *testing.T) {
	factory := newMockFactory()
	factory.configs["default/my-config"] = map[string]interface{}{"foo": "bar"}

	params := &providertypes.Params[wfconfig.NamespacedName]{
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

func TestListConfigWithFactory(t *testing.T) {
	factory := newMockFactory()
	factory.configs["default/cfg1"] = map[string]interface{}{"a": "1"}
	factory.configs["default/cfg2"] = map[string]interface{}{"b": "2"}

	params := &providertypes.Params[ListVars]{
		Params: ListVars{Namespace: "default", Template: "my-template"},
		RuntimeParams: providertypes.RuntimeParams{
			ConfigFactory: factory,
		},
	}
	ret, err := ListConfig(context.Background(), params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ret.Returns.Configs) != 2 {
		t.Fatalf("expected 2 configs, got %d", len(ret.Returns.Configs))
	}
}

func TestListConfigValidation(t *testing.T) {
	factory := newMockFactory()
	params := &providertypes.Params[ListVars]{
		Params: ListVars{Namespace: "", Template: "tmpl"},
		RuntimeParams: providertypes.RuntimeParams{
			ConfigFactory: factory,
		},
	}
	_, err := ListConfig(context.Background(), params)
	if err != ErrRequestInvalid {
		t.Fatalf("expected ErrRequestInvalid for empty namespace, got %v", err)
	}
}

func TestCreateConfigTemplateParsingWithSlash(t *testing.T) {
	factory := newMockFactory()
	params := &CreateParams{
		Params: CreateConfigProperties{
			Name:      "my-config",
			Namespace: "default",
			Template:  "custom-ns/my-template",
			Config:    map[string]interface{}{"key": "val"},
		},
		RuntimeParams: providertypes.RuntimeParams{
			ConfigFactory: factory,
		},
	}
	_, err := CreateConfig(context.Background(), params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestListConfigTemplateStripsNamespace(t *testing.T) {
	factory := newMockFactory()
	factory.configs["default/cfg1"] = map[string]interface{}{"a": "1"}

	params := &providertypes.Params[ListVars]{
		Params: ListVars{Namespace: "default", Template: "ns/my-template"},
		RuntimeParams: providertypes.RuntimeParams{
			ConfigFactory: factory,
		},
	}
	_, err := ListConfig(context.Background(), params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
