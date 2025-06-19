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

package workflowrun

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/kubevela/workflow/api/v1alpha1"
)

// MutatingHandler adding user info to application annotations
type MutatingHandler struct {
	Decoder admission.Decoder
}

var _ admission.Handler = &MutatingHandler{}

// Handle mutate application
func (h *MutatingHandler) Handle(ctx context.Context, req admission.Request) admission.Response { //nolint:revive,unused
	wr := &v1alpha1.WorkflowRun{}
	if err := h.Decoder.Decode(req, wr); err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}
	if wr.Spec.WorkflowSpec != nil {
		for i, step := range wr.Spec.WorkflowSpec.Steps {
			if step.Name == "" {
				wr.Spec.WorkflowSpec.Steps[i].Name = fmt.Sprintf("step-%d", i)
			}
			for j, sub := range step.SubSteps {
				if sub.Name == "" {
					wr.Spec.WorkflowSpec.Steps[i].SubSteps[j].Name = fmt.Sprintf("step-%d-%d", i, j)
				}
			}
		}
	}
	bs, err := json.Marshal(wr)
	if err != nil {
		return admission.Errored(http.StatusInternalServerError, err)
	}
	return admission.PatchResponseFromRaw(req.AdmissionRequest.Object.Raw, bs)
}

// RegisterMutatingHandler will register workflow mutation handler to the webhook
func RegisterMutatingHandler(mgr manager.Manager) {
	server := mgr.GetWebhookServer()
	server.Register("/mutating-core-oam-dev-v1alpha1-workflowruns", &webhook.Admission{Handler: &MutatingHandler{
		Decoder: admission.NewDecoder(mgr.GetScheme()),
	}})
}
