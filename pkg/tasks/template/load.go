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
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/oam-dev/kubevela/apis/core.oam.dev/v1beta1"
	oamutil "github.com/oam-dev/kubevela/pkg/oam/util"
)

var (
	//go:embed static
	templateFS embed.FS
)

const (
	templateDir = "static"
)

// Loader load task definition template.
type Loader interface {
	LoadTemplate(ctx context.Context, name string) (string, error)
}

// WorkflowStepLoader load workflowStep task definition template.
type WorkflowStepLoader struct {
	loadDefinition func(ctx context.Context, capName string) (string, error)
}

// LoadTaskTemplate gets the workflowStep definition.
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
			d := new(v1beta1.WorkflowStepDefinition)
			err := oamutil.GetCapabilityDefinition(ctx, client, d, capName)
			if err != nil {
				return "", errors.WithMessagef(err, "LoadTemplate [%s] ", capName)
			}
			schematic := d.Spec.Schematic
			if schematic != nil && schematic.CUE != nil {
				return schematic.CUE.Template, nil
			}

			return "", errors.New("custom workflowStep only support cue")
		},
	}
}
