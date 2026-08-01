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

package utils

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	oamv1alpha1 "github.com/kubevela/pkg/apis/oam/v1alpha1"
)

func TestGetWorkflow(t *testing.T) {
	ctx := context.Background()

	t.Run("found in the given namespace", func(t *testing.T) {
		r := require.New(t)
		workflow := &oamv1alpha1.Workflow{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "local-workflow",
				Namespace: "default",
			},
		}
		r.NoError(cli.Create(ctx, workflow))
		defer func() { r.NoError(cli.Delete(ctx, workflow)) }()

		got, err := GetWorkflow(ctx, cli, "default", workflow.Name)
		r.NoError(err)
		r.Equal(workflow.Name, got.Name)
		r.Equal("default", got.Namespace)
	})

	t.Run("falls back to vela-system when not found in the given namespace", func(t *testing.T) {
		r := require.New(t)
		workflow := &oamv1alpha1.Workflow{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "system-workflow",
				Namespace: SystemNamespace,
			},
		}
		r.NoError(cli.Create(ctx, workflow))
		defer func() { r.NoError(cli.Delete(ctx, workflow)) }()

		got, err := GetWorkflow(ctx, cli, "other-namespace", workflow.Name)
		r.NoError(err)
		r.Equal(workflow.Name, got.Name)
		r.Equal(SystemNamespace, got.Namespace)
	})

	t.Run("prefers the given namespace over vela-system", func(t *testing.T) {
		r := require.New(t)
		local := &oamv1alpha1.Workflow{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "shadowed-workflow",
				Namespace: "shadow-ns",
			},
			WorkflowSpec: oamv1alpha1.WorkflowSpec{
				Steps: []oamv1alpha1.WorkflowStep{{WorkflowStepBase: oamv1alpha1.WorkflowStepBase{Name: "local"}}},
			},
		}
		system := &oamv1alpha1.Workflow{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "shadowed-workflow",
				Namespace: SystemNamespace,
			},
			WorkflowSpec: oamv1alpha1.WorkflowSpec{
				Steps: []oamv1alpha1.WorkflowStep{{WorkflowStepBase: oamv1alpha1.WorkflowStepBase{Name: "system"}}},
			},
		}
		r.NoError(cli.Create(ctx, local))
		defer func() { r.NoError(cli.Delete(ctx, local)) }()
		r.NoError(cli.Create(ctx, system))
		defer func() { r.NoError(cli.Delete(ctx, system)) }()

		got, err := GetWorkflow(ctx, cli, "shadow-ns", local.Name)
		r.NoError(err)
		r.Equal("shadow-ns", got.Namespace)
		r.Equal("local", got.Steps[0].Name)
	})

	t.Run("not found anywhere returns a not-found error", func(t *testing.T) {
		r := require.New(t)
		_, err := GetWorkflow(ctx, cli, "default", "does-not-exist")
		r.Error(err)
		r.True(kerrors.IsNotFound(err))
	})
}
