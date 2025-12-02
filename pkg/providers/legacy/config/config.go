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
	"encoding/json"
	"errors"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	cuexruntime "github.com/kubevela/pkg/cue/cuex/runtime"

	providertypes "github.com/kubevela/workflow/pkg/providers/types"
)

const (
	// SaveInputPropertiesKey define the key name for saving the input properties in the secret.
	SaveInputPropertiesKey = "input-properties"
	// ConfigTemplateLabel is the label key for config template
	ConfigTemplateLabel = "config.oam.dev/template"
	// ConfigTypeLabel is the label key for config type
	ConfigTypeLabel = "config.oam.dev/type"
	// ConfigTypeConfig is the label value for config type
	ConfigTypeConfig = "config"
)

// ErrRequestInvalid means the request is invalid
var ErrRequestInvalid = errors.New("the request is invalid")

// ErrConfigNotFound means the config does not exist
var ErrConfigNotFound = errors.New("the config does not exist")

// CreateConfigVars is the input vars for creating a config
type CreateConfigVars struct {
	Name      string                 `json:"name"`
	Namespace string                 `json:"namespace"`
	Template  string                 `json:"template,omitempty"`
	Config    map[string]interface{} `json:"config"`
}

// CreateConfigParams is the create params
type CreateConfigParams = providertypes.LegacyParams[CreateConfigVars]

// CreateConfig creates a config by creating a secret
func CreateConfig(ctx context.Context, params *CreateConfigParams) (*any, error) {
	vars := params.Params
	if vars.Name == "" || vars.Namespace == "" {
		return nil, ErrRequestInvalid
	}

	// Marshal config to JSON
	configData, err := json.Marshal(vars.Config)
	if err != nil {
		return nil, err
	}

	// Create or update the secret
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      vars.Name,
			Namespace: vars.Namespace,
			Labels: map[string]string{
				ConfigTypeLabel: ConfigTypeConfig,
			},
		},
		Data: map[string][]byte{
			SaveInputPropertiesKey: configData,
		},
	}

	// Add template label if provided
	if vars.Template != "" {
		secret.Labels[ConfigTemplateLabel] = vars.Template
	}

	cli := params.KubeClient
	existing := &corev1.Secret{}
	err = cli.Get(ctx, client.ObjectKey{Name: vars.Name, Namespace: vars.Namespace}, existing)
	if err != nil {
		if apierrors.IsNotFound(err) {
			// Create new secret
			if err := cli.Create(ctx, secret); err != nil {
				return nil, err
			}
			return nil, nil
		}
		return nil, err
	}

	// Update existing secret
	existing.Data = secret.Data
	existing.Labels = secret.Labels
	if err := cli.Update(ctx, existing); err != nil {
		return nil, err
	}

	return nil, nil
}

// NamespacedName is the namespaced name
type NamespacedName struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
}

// ReadConfigParams is the read params
type ReadConfigParams = providertypes.LegacyParams[NamespacedName]

// ReadConfigResult is the read result
type ReadConfigResult struct {
	Config map[string]interface{} `json:"config"`
}

// ReadConfig reads the config from a secret
func ReadConfig(ctx context.Context, params *ReadConfigParams) (*ReadConfigResult, error) {
	vars := params.Params
	if vars.Name == "" || vars.Namespace == "" {
		return nil, ErrRequestInvalid
	}

	cli := params.KubeClient
	secret := &corev1.Secret{}
	err := cli.Get(ctx, client.ObjectKey{Name: vars.Name, Namespace: vars.Namespace}, secret)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil, ErrConfigNotFound
		}
		return nil, err
	}

	// Parse the config data
	configData, ok := secret.Data[SaveInputPropertiesKey]
	if !ok {
		return &ReadConfigResult{Config: map[string]interface{}{}}, nil
	}

	var config map[string]interface{}
	if err := json.Unmarshal(configData, &config); err != nil {
		return nil, err
	}

	return &ReadConfigResult{Config: config}, nil
}

// ListConfigVars is the input vars for listing configs
type ListConfigVars struct {
	Namespace string `json:"namespace"`
	Template  string `json:"template"`
}

// ListConfigParams is the list params
type ListConfigParams = providertypes.LegacyParams[ListConfigVars]

// ListConfigResult is the list result
type ListConfigResult struct {
	Configs []map[string]interface{} `json:"configs"`
}

// ListConfig lists the configs from secrets
func ListConfig(ctx context.Context, params *ListConfigParams) (*ListConfigResult, error) {
	vars := params.Params
	if vars.Namespace == "" || vars.Template == "" {
		return nil, ErrRequestInvalid
	}

	cli := params.KubeClient
	secretList := &corev1.SecretList{}
	listOpts := []client.ListOption{
		client.InNamespace(vars.Namespace),
		client.MatchingLabels{
			ConfigTypeLabel:     ConfigTypeConfig,
			ConfigTemplateLabel: vars.Template,
		},
	}

	if err := cli.List(ctx, secretList, listOpts...); err != nil {
		return nil, err
	}

	var configs []map[string]interface{}
	for _, secret := range secretList.Items {
		configData, ok := secret.Data[SaveInputPropertiesKey]
		if !ok {
			continue
		}

		var config map[string]interface{}
		if err := json.Unmarshal(configData, &config); err != nil {
			continue
		}

		configs = append(configs, map[string]interface{}{
			"name":   secret.Name,
			"config": config,
		})
	}

	if configs == nil {
		configs = []map[string]interface{}{}
	}

	return &ListConfigResult{Configs: configs}, nil
}

// DeleteConfigParams is the delete params
type DeleteConfigParams = providertypes.LegacyParams[NamespacedName]

// DeleteConfig deletes a config by deleting the secret
func DeleteConfig(ctx context.Context, params *DeleteConfigParams) (*any, error) {
	vars := params.Params
	if vars.Name == "" || vars.Namespace == "" {
		return nil, ErrRequestInvalid
	}

	cli := params.KubeClient
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      vars.Name,
			Namespace: vars.Namespace,
		},
	}

	if err := cli.Delete(ctx, secret); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}

	return nil, nil
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
		"create": providertypes.LegacyGenericProviderFn[CreateConfigVars, any](CreateConfig),
		"read":   providertypes.LegacyGenericProviderFn[NamespacedName, ReadConfigResult](ReadConfig),
		"list":   providertypes.LegacyGenericProviderFn[ListConfigVars, ListConfigResult](ListConfig),
		"delete": providertypes.LegacyGenericProviderFn[NamespacedName, any](DeleteConfig),
	}
}
