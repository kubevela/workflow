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

	kerrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	oamv1alpha1 "github.com/kubevela/pkg/apis/oam/v1alpha1"
)

// SystemNamespace is the shared namespace where reusable Workflows are looked up
// as a fallback, mirroring how X-Definitions resolve from vela-system.
const SystemNamespace = "vela-system"

// GetWorkflow gets the Workflow referenced by a WorkflowRun. It first looks in the
// given namespace, then falls back to SystemNamespace so a Workflow created in
// vela-system can be referenced by a WorkflowRun in any namespace.
func GetWorkflow(ctx context.Context, cli client.Reader, namespace, name string) (*oamv1alpha1.Workflow, error) {
	workflow := &oamv1alpha1.Workflow{}
	err := cli.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, workflow)
	if err == nil {
		return workflow, nil
	}
	if !kerrors.IsNotFound(err) || namespace == SystemNamespace {
		return nil, err
	}
	if sysErr := cli.Get(ctx, client.ObjectKey{Namespace: SystemNamespace, Name: name}, workflow); sysErr != nil {
		if kerrors.IsNotFound(sysErr) {
			return nil, err
		}
		return nil, sysErr
	}
	return workflow, nil
}
