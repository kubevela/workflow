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

	"cuelang.org/go/cue"
	"github.com/kubevela/pkg/cue/cuex"
	"github.com/kubevela/pkg/util/k8s"
	"github.com/kubevela/pkg/util/k8s/patch"
	corev1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// LabelConfigCatalog identifies a Secret as a workflow config.
	LabelConfigCatalog = "config.oam.dev/catalog"
	// LabelConfigType stores the template name that generated this config.
	LabelConfigType = "config.oam.dev/type"
	// LabelConfigScope stores the scope of the config.
	LabelConfigScope = "config.oam.dev/scope"
	// CatalogValue is the value for the catalog label.
	CatalogValue = "velacore-config"
	// DataKeyProperties is the Secret data key for config properties.
	DataKeyProperties = "input-properties"
	// DataKeyObjectReference is the Secret data key for output object references.
	DataKeyObjectReference = "objects-reference"
	// DataKeyTemplate is the ConfigMap data key for the CUE template script.
	DataKeyTemplate = "template"
	// AnnotationConfigSensitive marks a config as sensitive (not readable via API).
	AnnotationConfigSensitive = "config.oam.dev/sensitive"
	// AnnotationConfigAlias stores the alias of a config.
	AnnotationConfigAlias = "config.oam.dev/alias"
	// AnnotationConfigDescription stores the description of a config.
	AnnotationConfigDescription = "config.oam.dev/description"
	// AnnotationConfigTemplateNamespace stores the namespace of the template that generated this config.
	AnnotationConfigTemplateNamespace = "config.oam.dev/template-namespace"
	// AnnoLastAppliedConfig is the annotation for 3-way merge last applied config.
	AnnoLastAppliedConfig = "config.oam.dev/last-applied-configuration"
	// AnnoLastAppliedTime is the annotation for 3-way merge last applied time.
	AnnoLastAppliedTime = "config.oam.dev/last-applied-time"
	// TemplateConfigMapNamePrefix is the prefix of config template ConfigMap names.
	TemplateConfigMapNamePrefix = "config-template-"
	// TemplateOutput is the CUE path for the output secret in a config template.
	TemplateOutput = "template.output"
	// TemplateOutputs is the CUE path for additional output objects in a config template.
	TemplateOutputs = "template.outputs"
	// TemplateValidationReturns is the CUE path for validation returns in a config template.
	TemplateValidationReturns = "template.validation.$returns"
)

// DefaultContext is the default CUE context template appended to config templates.
var DefaultContext = []byte(`
	context: {
		name: string
		namespace: string
	}
`)

// ErrSensitiveConfig means this config cannot be read directly.
var ErrSensitiveConfig = fmt.Errorf("the config is sensitive")

// ErrChangeTemplate means the template of the config cannot be changed.
var ErrChangeTemplate = fmt.Errorf("the template of the config can not be changed")

// ErrChangeSecretType means the secret type of the config cannot be changed.
var ErrChangeSecretType = fmt.Errorf("the secret type of the config can not be changed")

// ErrTemplateNotFound means the config template does not exist.
var ErrTemplateNotFound = fmt.Errorf("the template does not exist")

// configRenderContext provides context variables for CUE template rendering.
type configRenderContext struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
}

// validation is the response of template validation.
type validation struct {
	Result  bool   `json:"result"`
	Message string `json:"message"`
}

// renderedConfig is the intermediate representation returned by ParseConfig.
type renderedConfig struct {
	// Secret is the fully rendered Secret to apply.
	Secret *corev1.Secret
	// OutputObjects are additional objects rendered from template.outputs.
	OutputObjects map[string]*unstructured.Unstructured
}

// K8sFactory is a Kubernetes-backed Factory that stores configs as Secrets
// and supports CUE template rendering from ConfigMap-stored templates.
type K8sFactory struct {
	cli      client.Client
	compiler *cuex.Compiler
}

// NewK8sFactory creates a new K8sFactory.
func NewK8sFactory(cli client.Client) Factory {
	return &K8sFactory{
		cli:      cli,
		compiler: cuex.NewCompilerWithInternalPackages(),
	}
}

// ParseConfig loads a config template (if specified), compiles it with the
// provided properties, and returns a renderedConfig containing the output Secret
// and any additional output objects.
func (f *K8sFactory) ParseConfig(ctx context.Context, template NamespacedName, meta Metadata) (any, error) {
	var secret corev1.Secret
	rc := &renderedConfig{
		Secret: &secret,
	}

	if template.Name != "" {
		// Load template from ConfigMap
		tmplScript, sensitive, scope, err := f.loadTemplate(ctx, template.Name, template.Namespace)
		if err != nil {
			return nil, err
		}

		contextValue := configRenderContext{
			Name:      meta.Name,
			Namespace: meta.Namespace,
		}

		// Compile the CUE template with context and properties
		val, err := f.compileTemplate(ctx, tmplScript, contextValue, meta.Properties)
		if err != nil {
			return nil, err
		}

		// Check validation result
		validPath := val.LookupPath(cue.ParsePath(TemplateValidationReturns))
		if validPath.Exists() {
			var v validation
			if err := validPath.Decode(&v); err != nil {
				return nil, fmt.Errorf("the validation.$returns format must be validation")
			}
			if len(v.Message) > 0 {
				return nil, fmt.Errorf("failed to validate config: %s", v.Message)
			}
		}

		// Render the output secret
		output := val.LookupPath(cue.ParsePath(TemplateOutput))
		if output.Exists() {
			if err := output.Decode(&secret); err != nil {
				return nil, fmt.Errorf("the output format must be secret")
			}
		}

		// Set secret type if not specified
		if secret.Type == "" {
			secret.Type = corev1.SecretType(fmt.Sprintf("/%s", template.Name))
		}
		if secret.Labels == nil {
			secret.Labels = map[string]string{}
		}
		secret.Labels[LabelConfigCatalog] = CatalogValue
		secret.Labels[LabelConfigType] = template.Name
		secret.Labels[LabelConfigScope] = scope

		if secret.Annotations == nil {
			secret.Annotations = map[string]string{}
		}
		secret.Annotations[AnnotationConfigSensitive] = fmt.Sprintf("%t", sensitive)
		secret.Annotations[AnnotationConfigTemplateNamespace] = template.Namespace

		// Render additional output objects
		outputs := val.LookupPath(cue.ParsePath(TemplateOutputs))
		if outputs.Exists() {
			var objects = map[string]interface{}{}
			if err := outputs.Decode(&objects); err != nil {
				return nil, fmt.Errorf("the outputs is invalid %w", err)
			}
			var objectReferences []corev1.ObjectReference
			rc.OutputObjects = make(map[string]*unstructured.Unstructured)
			for k := range objects {
				if ob, ok := objects[k].(map[string]interface{}); ok {
					obj := &unstructured.Unstructured{Object: ob}
					rc.OutputObjects[k] = obj
					objectReferences = append(objectReferences, corev1.ObjectReference{
						Kind:       obj.GetKind(),
						Namespace:  obj.GetNamespace(),
						Name:       obj.GetName(),
						APIVersion: obj.GetAPIVersion(),
					})
				}
			}
			objectReferenceJSON, err := json.Marshal(objectReferences)
			if err != nil {
				return nil, err
			}
			if secret.Data == nil {
				secret.Data = map[string][]byte{}
			}
			secret.Data[DataKeyObjectReference] = objectReferenceJSON
		}
	} else {
		// No template — create a basic config secret
		secret.Labels = map[string]string{
			LabelConfigCatalog: CatalogValue,
			LabelConfigType:    "",
		}
		secret.Annotations = map[string]string{}
	}

	secret.Namespace = meta.Namespace
	if secret.Name == "" {
		secret.Name = meta.Name
	}
	secret.Annotations[AnnotationConfigAlias] = meta.Alias
	secret.Annotations[AnnotationConfigDescription] = meta.Description

	propBytes, err := json.Marshal(meta.Properties)
	if err != nil {
		return nil, err
	}
	if secret.Data == nil {
		secret.Data = map[string][]byte{}
	}
	secret.Data[DataKeyProperties] = propBytes

	return rc, nil
}

// loadTemplate loads a config template CUE script from a ConfigMap.
func (f *K8sFactory) loadTemplate(ctx context.Context, name, ns string) (script string, sensitive bool, scope string, err error) {
	var cm corev1.ConfigMap
	if err = f.cli.Get(ctx, client.ObjectKey{Namespace: ns, Name: TemplateConfigMapNamePrefix + name}, &cm); err != nil {
		if kerrors.IsNotFound(err) {
			return "", false, "", ErrTemplateNotFound
		}
		return "", false, "", err
	}
	script = cm.Data[DataKeyTemplate]
	if script == "" {
		return "", false, "", fmt.Errorf("config template %s/%s has no template data", ns, name)
	}
	if cm.Annotations != nil {
		sensitive = cm.Annotations[AnnotationConfigSensitive] == "true"
	}
	if cm.Labels != nil {
		scope = cm.Labels[LabelConfigScope]
	}
	return script, sensitive, scope, nil
}

// compileTemplate compiles a CUE config template with context and properties.
func (f *K8sFactory) compileTemplate(ctx context.Context, script string, context configRenderContext, properties map[string]interface{}) (cue.Value, error) {
	// Append default context definition
	fullScript := script + "\n" + string(DefaultContext)

	contextOption := cuex.WithExtraData("context", context)
	parameterOption := cuex.WithExtraData("template.parameter", properties)
	val, err := f.compiler.CompileStringWithOptions(ctx, fullScript, contextOption, parameterOption, cuex.DisableResolveProviderFunctions{})
	if err != nil {
		return cue.Value{}, fmt.Errorf("failed to compile config template: %w", err)
	}
	if !val.Exists() {
		return cue.Value{}, fmt.Errorf("failed to compile config template")
	}
	return val, nil
}

// CreateOrUpdateConfig applies the rendered config Secret using 3-way merge
// and applies any additional output objects.
func (f *K8sFactory) CreateOrUpdateConfig(ctx context.Context, configItem any, ns string) error {
	rc, ok := configItem.(*renderedConfig)
	if !ok {
		return fmt.Errorf("invalid config item type: %T", configItem)
	}
	secret := rc.Secret
	if ns != "" {
		secret.Namespace = ns
	}

	existing := &corev1.Secret{}
	key := client.ObjectKey{Namespace: secret.Namespace, Name: secret.Name}
	if err := f.cli.Get(ctx, key, existing); err != nil {
		if !kerrors.IsNotFound(err) {
			return err
		}
		// Create new — record last-applied-config for future 3-way merges
		workload, err := toUnstructured(secret)
		if err != nil {
			return err
		}
		b, err := workload.MarshalJSON()
		if err != nil {
			return err
		}
		if err := k8s.AddAnnotation(workload, AnnoLastAppliedConfig, string(b)); err != nil {
			return err
		}
		return f.cli.Create(ctx, workload)
	}

	// Update existing — prevent template change
	if existing.Labels != nil && existing.Labels[LabelConfigType] != "" &&
		secret.Labels != nil && existing.Labels[LabelConfigType] != secret.Labels[LabelConfigType] {
		return ErrChangeTemplate
	}
	// Prevent secret type change
	if secret.Type != "" && existing.Type != secret.Type {
		return ErrChangeSecretType
	}

	// 3-way merge patch
	workload, err := toUnstructured(secret)
	if err != nil {
		return err
	}
	existingU, err := toUnstructured(existing)
	if err != nil {
		return err
	}
	patcher, err := patch.ThreeWayMergePatch(existingU, workload, &patch.PatchAction{
		UpdateAnno:            true,
		AnnoLastAppliedConfig: AnnoLastAppliedConfig,
		AnnoLastAppliedTime:   AnnoLastAppliedTime,
	})
	if err != nil {
		return fmt.Errorf("fail to compute merge patch: %w", err)
	}
	if err := f.cli.Patch(ctx, workload, patcher); err != nil {
		return fmt.Errorf("fail to apply the secret: %w", err)
	}

	// Apply output objects
	for key, obj := range rc.OutputObjects {
		existing := new(unstructured.Unstructured)
		existing.SetGroupVersionKind(obj.GroupVersionKind())
		if err := f.cli.Get(ctx, client.ObjectKeyFromObject(obj), existing); err != nil {
			if !kerrors.IsNotFound(err) {
				return fmt.Errorf("fail to get the object %s: %w", key, err)
			}
			b, err := obj.MarshalJSON()
			if err != nil {
				return err
			}
			if err := k8s.AddAnnotation(obj, AnnoLastAppliedConfig, string(b)); err != nil {
				return err
			}
			if err := f.cli.Create(ctx, obj); err != nil {
				return fmt.Errorf("fail to create the object %s: %w", key, err)
			}
			continue
		}
		patcher, err := patch.ThreeWayMergePatch(existing, obj, &patch.PatchAction{
			UpdateAnno:            true,
			AnnoLastAppliedConfig: AnnoLastAppliedConfig,
			AnnoLastAppliedTime:   AnnoLastAppliedTime,
		})
		if err != nil {
			return fmt.Errorf("fail to compute merge patch for object %s: %w", key, err)
		}
		if err := f.cli.Patch(ctx, obj, patcher); err != nil {
			return fmt.Errorf("fail to apply the object %s: %w", key, err)
		}
	}

	return nil
}

// ReadConfig reads a config Secret and returns its properties.
func (f *K8sFactory) ReadConfig(ctx context.Context, namespace, name string) (map[string]interface{}, error) {
	secret := &corev1.Secret{}
	if err := f.cli.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, secret); err != nil {
		return nil, err
	}
	if secret.Annotations[AnnotationConfigSensitive] == "true" {
		return nil, ErrSensitiveConfig
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
func (f *K8sFactory) ListConfigs(ctx context.Context, namespace, template, _ string, _ bool) ([]*Item, error) {
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

	var items []*Item
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
		item := &Item{
			Name:       s.Name,
			Properties: props,
		}
		if s.Annotations != nil {
			item.Alias = s.Annotations[AnnotationConfigAlias]
			item.Description = s.Annotations[AnnotationConfigDescription]
		}
		items = append(items, item)
	}
	return items, nil
}

// DeleteConfig deletes a config Secret and cleans up any referenced output objects.
func (f *K8sFactory) DeleteConfig(ctx context.Context, namespace, name string) error {
	secret := &corev1.Secret{}
	if err := f.cli.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, secret); err != nil {
		if kerrors.IsNotFound(err) {
			return fmt.Errorf("the config %s not found", name)
		}
		return fmt.Errorf("fail to delete the config %s: %w", name, err)
	}
	if secret.Labels[LabelConfigCatalog] != CatalogValue {
		return fmt.Errorf("secret %s/%s is not a workflow config", namespace, name)
	}

	// Clean up output object references before deleting the secret
	if objects, exist := secret.Data[DataKeyObjectReference]; exist {
		var objectReferences []corev1.ObjectReference
		if err := json.Unmarshal(objects, &objectReferences); err != nil {
			return err
		}
		for _, ref := range objectReferences {
			obj := &unstructured.Unstructured{}
			obj.SetAPIVersion(ref.APIVersion)
			obj.SetKind(ref.Kind)
			obj.SetNamespace(ref.Namespace)
			obj.SetName(ref.Name)
			if err := f.cli.Delete(ctx, obj); err != nil && !kerrors.IsNotFound(err) {
				return fmt.Errorf("fail to clear the object %s: %w", ref.Name, err)
			}
		}
	}

	return f.cli.Delete(ctx, secret)
}

// toUnstructured converts a typed object to an unstructured.Unstructured.
func toUnstructured(obj client.Object) (*unstructured.Unstructured, error) {
	data, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return nil, fmt.Errorf("fail to convert to unstructured: %w", err)
	}
	u := &unstructured.Unstructured{Object: data}
	u.SetAPIVersion("v1")
	u.SetKind("Secret")
	return u, nil
}
