/*
Copyright 2026 The KubeVela Authors.

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

package main

import (
	"context"
	"fmt"

	"github.com/kubevela/workflow/pkg/utils/httpguard"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

func configureWorkflowHTTPDeny(ctx context.Context, reader client.Reader, mgr manager.Manager, configMapName, namespace string, blockPrivate bool) error {
	httpguard.SetEnhancer(func(p httpguard.Policy) httpguard.Policy {
		if blockPrivate {
			p.BlockPrivate = true
		}
		return p
	})
	if err := httpguard.LoadConfigMap(ctx, reader, configMapName, namespace); err != nil {
		return fmt.Errorf("initialize workflow HTTP deny ConfigMap: %w", err)
	}
	if err := httpguard.SetupWatcher(mgr, configMapName, namespace); err != nil {
		return fmt.Errorf("watch workflow HTTP deny ConfigMap: %w", err)
	}
	return nil
}
