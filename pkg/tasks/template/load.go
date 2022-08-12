/*
Copyright 2022 The KubeVela Authors.

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

package template

import (
	"context"
	"embed"
	"fmt"

	"github.com/pkg/errors"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var (
	//go:embed static
	templateFS embed.FS
)

const (
	templateDir = "static"
)

type namespaceContextKey int

const (
	// DefinitionNamespace is context key to define workflow run namespace
	DefinitionNamespace namespaceContextKey = iota
	// SystemDefinitionNamespace is the system definition namespace
	systemDefinitionNamespace string = "vela-system"
)

// Loader load task definition template.
type Loader interface {
	LoadTemplate(ctx context.Context, name string) (string, error)
}

// WorkflowStepLoader load workflowStep task definition template.
type WorkflowStepLoader struct {
	loadDefinition func(ctx context.Context, capName string) (string, error)
}

// LoadTemplate gets the workflow step definition.
func (loader *WorkflowStepLoader) LoadTemplate(ctx context.Context, name string) (string, error) {
	files, err := templateFS.ReadDir(templateDir)
	if err != nil {
		return "", err
	}

	staticFilename := name + ".cue"
	for _, file := range files {
		if staticFilename == file.Name() {
			fileName := fmt.Sprintf("%s/%s", templateDir, file.Name())
			content, err := templateFS.ReadFile(fileName)
			return string(content), err
		}
	}

	return loader.loadDefinition(ctx, name)
}

// NewWorkflowStepTemplateLoader create a task template loader.
func NewWorkflowStepTemplateLoader(client client.Client) Loader {
	return &WorkflowStepLoader{
		loadDefinition: func(ctx context.Context, capName string) (string, error) {
			return getDefinitionTemplate(ctx, client, capName)
		},
	}
}

type def struct {
	Spec struct {
		Schematic struct {
			CUE struct {
				Template string `json:"template"`
			} `json:"cue"`
		} `json:"schematic"`
	} `json:"spec,omitempty"`
}

func getDefinitionTemplate(ctx context.Context, cli client.Client, definitionName string) (string, error) {
	const (
		definitionAPIVersion       = "core.oam.dev/v1beta1"
		kindWorkflowStepDefinition = "WorkflowStepDefinition"
	)
	definition := &unstructured.Unstructured{}
	definition.SetAPIVersion(definitionAPIVersion)
	definition.SetKind(kindWorkflowStepDefinition)
	ns := getDefinitionNamespaceWithCtx(ctx)
	if err := cli.Get(ctx, types.NamespacedName{Name: definitionName, Namespace: ns}, definition); err != nil {
		if apierrors.IsNotFound(err) {
			if err := cli.Get(ctx, types.NamespacedName{Name: definitionName, Namespace: systemDefinitionNamespace}, definition); err != nil {
				return "", err
			}
		} else {
			return "", err
		}
	}
	d := new(def)
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(definition.Object, d); err != nil {
		return "", errors.Wrap(err, "invalid workflow step definition")
	}
	return d.Spec.Schematic.CUE.Template, nil
}

func getDefinitionNamespaceWithCtx(ctx context.Context) string {
	var ns string
	if run := ctx.Value(DefinitionNamespace); run == nil {
		ns = systemDefinitionNamespace
	} else {
		ns = run.(string)
	}
	return ns
}
