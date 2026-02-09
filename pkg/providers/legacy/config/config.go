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
	_ "embed"
	"errors"
	"strings"

	cuexruntime "github.com/kubevela/pkg/cue/cuex/runtime"

	wfconfig "github.com/kubevela/workflow/pkg/config"
	providertypes "github.com/kubevela/workflow/pkg/providers/types"
)

const (
	// ProviderName is provider name
	ProviderName = "config"
)

// ErrRequestInvalid means the request is invalid
var ErrRequestInvalid = errors.New("the request is invalid")

// ErrConfigFactoryNotConfigured means the config factory is not configured
var ErrConfigFactoryNotConfigured = errors.New("config factory not configured")

// CreateConfigProperties the request body for creating a config
type CreateConfigProperties struct {
	Name      string                 `json:"name"`
	Namespace string                 `json:"namespace"`
	Template  string                 `json:"template,omitempty"`
	Config    map[string]interface{} `json:"config"`
}

// CreateParams is the create params
type CreateParams = providertypes.LegacyParams[CreateConfigProperties]

// CreateConfig creates a config
func CreateConfig(ctx context.Context, params *CreateParams) (*any, error) {
	ccp := params.Params
	name := ccp.Template
	namespace := "vela-system"
	if strings.Contains(ccp.Template, "/") {
		namespacedName := strings.SplitN(ccp.Template, "/", 2)
		namespace = namespacedName[0]
		name = namespacedName[1]
	}
	factory := params.ConfigFactory
	if factory == nil {
		return nil, ErrConfigFactoryNotConfigured
	}
	configItem, err := factory.ParseConfig(ctx, wfconfig.NamespacedName{
		Name:      name,
		Namespace: namespace,
	}, wfconfig.Metadata{
		NamespacedName: wfconfig.NamespacedName{
			Name:      ccp.Name,
			Namespace: ccp.Namespace,
		},
		Properties: ccp.Config,
	})
	if err != nil {
		return nil, err
	}
	return nil, factory.CreateOrUpdateConfig(ctx, configItem, ccp.Namespace)
}

// ReadResult is the read result
type ReadResult struct {
	Config map[string]any `json:"config"`
}

// ReadConfig reads the config
func ReadConfig(ctx context.Context, params *providertypes.LegacyParams[wfconfig.NamespacedName]) (*ReadResult, error) {
	nn := params.Params
	factory := params.ConfigFactory
	if factory == nil {
		return nil, ErrConfigFactoryNotConfigured
	}
	content, err := factory.ReadConfig(ctx, nn.Namespace, nn.Name)
	if err != nil {
		return nil, err
	}
	return &ReadResult{Config: content}, nil
}

// ListVars is the list vars
type ListVars struct {
	Namespace string `json:"namespace"`
	Template  string `json:"template"`
}

// ListResult is the list result
type ListResult struct {
	Configs []map[string]any `json:"configs"`
}

// ListConfig lists the config
func ListConfig(ctx context.Context, params *providertypes.LegacyParams[ListVars]) (*ListResult, error) {
	template := params.Params.Template
	namespace := params.Params.Namespace
	if template == "" || namespace == "" {
		return nil, ErrRequestInvalid
	}

	if strings.Contains(template, "/") {
		namespacedName := strings.SplitN(template, "/", 2)
		template = namespacedName[1]
	}
	factory := params.ConfigFactory
	if factory == nil {
		return nil, ErrConfigFactoryNotConfigured
	}
	configs, err := factory.ListConfigs(ctx, namespace, template, "", false)
	if err != nil {
		return nil, err
	}
	var contents = []map[string]interface{}{}
	for _, c := range configs {
		contents = append(contents, map[string]interface{}{
			"name":        c.Name,
			"alias":       c.Alias,
			"description": c.Description,
			"config":      c.Properties,
		})
	}
	return &ListResult{Configs: contents}, nil
}

// DeleteConfig deletes a config
func DeleteConfig(ctx context.Context, params *providertypes.LegacyParams[wfconfig.NamespacedName]) (*any, error) {
	nn := params.Params
	factory := params.ConfigFactory
	if factory == nil {
		return nil, ErrConfigFactoryNotConfigured
	}
	return nil, factory.DeleteConfig(ctx, nn.Namespace, nn.Name)
}

//go:embed config.cue
var template string

// GetTemplate returns the cue template.
func GetTemplate() string {
	return template
}

// GetProviders returns the cue providers.
func GetProviders() map[string]cuexruntime.ProviderFn {
	return map[string]cuexruntime.ProviderFn{
		"create-config": providertypes.LegacyGenericProviderFn[CreateConfigProperties, any](CreateConfig),
		"read-config":   providertypes.LegacyGenericProviderFn[wfconfig.NamespacedName, ReadResult](ReadConfig),
		"list-config":   providertypes.LegacyGenericProviderFn[ListVars, ListResult](ListConfig),
		"delete-config": providertypes.LegacyGenericProviderFn[wfconfig.NamespacedName, any](DeleteConfig),
	}
}
