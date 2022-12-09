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

package webhook

import (
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/webhook/conversion"

	"github.com/kubevela/workflow/controllers"
	"github.com/kubevela/workflow/pkg/webhook/v1alpha1/workflowrun"
)

// Register will be called in main and register all validation handlers
func Register(mgr manager.Manager, args controllers.Args) {
	workflowrun.RegisterValidatingHandler(mgr, args)
	workflowrun.RegisterMutatingHandler(mgr)

	server := mgr.GetWebhookServer()
	server.Register("/convert", &conversion.Webhook{})
}
