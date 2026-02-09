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
	"encoding/json"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// LabelConfigCatalog identifies a Secret as a workflow config.
	LabelConfigCatalog = "config.oam.dev/catalog"
	// LabelConfigType stores the template name that generated this config.
	LabelConfigType = "config.oam.dev/type"
	// CatalogValue is the value for the catalog label.
	CatalogValue = "velacore-config"
	// DataKeyProperties is the Secret data key for config properties.
	DataKeyProperties = "input-properties"
)

// parsedConfig is the intermediate representation returned by ParseConfig.
type parsedConfig struct {
	Name       string
	Namespace  string
	Template   string
	Properties map[string]interface{}
}

// K8sFactory is a simple Kubernetes-backed Factory that stores configs as Secrets.
type K8sFactory struct {
	cli client.Client
}

// NewK8sFactory creates a new K8sFactory.
func NewK8sFactory(cli client.Client) Factory {
	return &K8sFactory{cli: cli}
}

// ParseConfig converts template + metadata into an intermediate config item.
func (f *K8sFactory) ParseConfig(_ context.Context, template NamespacedName, meta Metadata) (any, error) {
	return &parsedConfig{
		Name:       meta.Name,
		Namespace:  meta.Namespace,
		Template:   template.Name,
		Properties: meta.Properties,
	}, nil
}

// CreateOrUpdateConfig stores the config as a Kubernetes Secret.
func (f *K8sFactory) CreateOrUpdateConfig(ctx context.Context, configItem any, ns string) error {
	cfg, ok := configItem.(*parsedConfig)
	if !ok {
		return fmt.Errorf("invalid config item type: %T", configItem)
	}
	if ns == "" {
		ns = cfg.Namespace
	}

	propBytes, err := json.Marshal(cfg.Properties)
	if err != nil {
		return fmt.Errorf("failed to marshal config properties: %w", err)
	}

	secret := &corev1.Secret{}
	key := client.ObjectKey{Namespace: ns, Name: cfg.Name}
	if err := f.cli.Get(ctx, key, secret); err != nil {
		if !kerrors.IsNotFound(err) {
			return err
		}
		// Create new
		secret = &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      cfg.Name,
				Namespace: ns,
				Labels: map[string]string{
					LabelConfigCatalog: CatalogValue,
					LabelConfigType:    cfg.Template,
				},
			},
			Data: map[string][]byte{
				DataKeyProperties: propBytes,
			},
		}
		return f.cli.Create(ctx, secret)
	}

	// Update existing
	if secret.Labels == nil {
		secret.Labels = make(map[string]string)
	}
	secret.Labels[LabelConfigCatalog] = CatalogValue
	secret.Labels[LabelConfigType] = cfg.Template
	if secret.Data == nil {
		secret.Data = make(map[string][]byte)
	}
	secret.Data[DataKeyProperties] = propBytes
	return f.cli.Update(ctx, secret)
}

// ReadConfig reads a config Secret and returns its properties.
func (f *K8sFactory) ReadConfig(ctx context.Context, namespace, name string) (map[string]interface{}, error) {
	secret := &corev1.Secret{}
	if err := f.cli.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, secret); err != nil {
		return nil, err
	}
	raw, ok := secret.Data[DataKeyProperties]
	if !ok {
		return nil, fmt.Errorf("config %s/%s has no properties data", namespace, name)
	}
	var props map[string]interface{}
	if err := json.Unmarshal(raw, &props); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config properties: %w", err)
	}
	return props, nil
}

// ListConfigs lists config Secrets filtered by template name.
func (f *K8sFactory) ListConfigs(ctx context.Context, namespace, template, _ string, _ bool) ([]*ConfigItem, error) {
	selector := labels.SelectorFromSet(labels.Set{
		LabelConfigCatalog: CatalogValue,
	})
	if template != "" {
		selector = labels.SelectorFromSet(labels.Set{
			LabelConfigCatalog: CatalogValue,
			LabelConfigType:    template,
		})
	}

	secretList := &corev1.SecretList{}
	if err := f.cli.List(ctx, secretList,
		client.InNamespace(namespace),
		client.MatchingLabelsSelector{Selector: selector},
	); err != nil {
		return nil, err
	}

	var items []*ConfigItem
	for i := range secretList.Items {
		s := &secretList.Items[i]
		raw, ok := s.Data[DataKeyProperties]
		if !ok {
			continue
		}
		var props map[string]interface{}
		if err := json.Unmarshal(raw, &props); err != nil {
			continue
		}
		items = append(items, &ConfigItem{
			Name:       s.Name,
			Properties: props,
		})
	}
	return items, nil
}

// DeleteConfig deletes a config Secret.
func (f *K8sFactory) DeleteConfig(ctx context.Context, namespace, name string) error {
	secret := &corev1.Secret{}
	if err := f.cli.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, secret); err != nil {
		return err
	}
	if secret.Labels[LabelConfigCatalog] != CatalogValue {
		return fmt.Errorf("secret %s/%s is not a workflow config", namespace, name)
	}
	return f.cli.Delete(ctx, secret)
}
