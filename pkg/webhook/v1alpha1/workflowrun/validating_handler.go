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
	"fmt"
	"net/http"

	admissionv1 "k8s.io/api/admission/v1"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/kubevela/workflow/api/v1alpha1"
	"github.com/kubevela/workflow/pkg/types"

	"github.com/kubevela/workflow/controllers"
)

var _ admission.Handler = &ValidatingHandler{}

// ValidatingHandler handles application
type ValidatingHandler struct {
	Client client.Client
	// Decoder decodes objects
	Decoder admission.Decoder
}

func mergeErrors(errs field.ErrorList) error {
	s := ""
	for _, err := range errs {
		s += fmt.Sprintf("field \"%s\": %s error encountered, %s. ", err.Field, err.Type, err.Detail)
	}
	return fmt.Errorf("%s", s)
}

// Handle validate Application Spec here
func (h *ValidatingHandler) Handle(ctx context.Context, req admission.Request) admission.Response {
	wr := &v1alpha1.WorkflowRun{}
	if err := h.Decoder.Decode(req, wr); err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}
	ctx = types.SetNamespaceInCtx(ctx, wr.Namespace)
	switch req.Operation {
	case admissionv1.Create:
		if allErrs := h.ValidateWorkflow(ctx, wr); len(allErrs) > 0 {
			// http.StatusUnprocessableEntity will NOT report any error descriptions
			// to the client, use generic http.StatusBadRequest instead.
			return admission.Errored(http.StatusBadRequest, mergeErrors(allErrs))
		}
	case admissionv1.Update:
		if wr.ObjectMeta.DeletionTimestamp.IsZero() {
			if allErrs := h.ValidateWorkflow(ctx, wr); len(allErrs) > 0 {
				return admission.Errored(http.StatusBadRequest, mergeErrors(allErrs))
			}
		}
	default:
		// Do nothing for DELETE and CONNECT
	}
	return admission.ValidationResponse(true, "")
}

// RegisterValidatingHandler will register application validate handler to the webhook
func RegisterValidatingHandler(mgr manager.Manager, _ controllers.Args) {
	server := mgr.GetWebhookServer()
	server.Register("/validating-core-oam-dev-v1alpha1-workflowruns", &webhook.Admission{Handler: &ValidatingHandler{
		Client:  mgr.GetClient(),
		Decoder: admission.NewDecoder(mgr.GetScheme()),
	}})
}
