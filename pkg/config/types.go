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

import "context"

// NamespacedName is a namespace/name pair for config references.
type NamespacedName struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
}

// Metadata is the user-provided metadata for a config item.
type Metadata struct {
	NamespacedName
	Alias       string                 `json:"alias,omitempty"`
	Description string                 `json:"description,omitempty"`
	Properties  map[string]interface{} `json:"properties"`
}

// Item is the minimal view of a config returned by ListConfigs.
type Item struct {
	Name        string                 `json:"name"`
	Alias       string                 `json:"alias,omitempty"`
	Description string                 `json:"description,omitempty"`
	Properties  map[string]interface{} `json:"properties"`
}

// Factory defines the config CRUD operations.
// Implementations are provided by the consuming application
// (e.g., KubeVela) and injected via RuntimeParams.
type Factory interface {
	ParseConfig(ctx context.Context, template NamespacedName, meta Metadata) (any, error)
	CreateOrUpdateConfig(ctx context.Context, configItem any, ns string) error
	ReadConfig(ctx context.Context, namespace, name string) (map[string]interface{}, error)
	ListConfigs(ctx context.Context, namespace, template, scope string, withStatus bool) ([]*Item, error)
	DeleteConfig(ctx context.Context, namespace, name string) error
}
