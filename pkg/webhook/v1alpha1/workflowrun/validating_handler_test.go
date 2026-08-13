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
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	admissionv1 "k8s.io/api/admission/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	oamv1alpha1 "github.com/kubevela/pkg/apis/oam/v1alpha1"
)

var _ = Describe("Test WorkflowRun Validator", func() {
	BeforeEach(func() {
		handler.Client = k8sClient
		handler.Decoder = decoder
	})

	It("Test WorkflowRun Validator [bad request]", func() {
		req := admission.Request{
			AdmissionRequest: admissionv1.AdmissionRequest{
				Operation: admissionv1.Create,
				Resource:  metav1.GroupVersionResource{Group: "core.oam.dev", Version: "v1alpha1", Resource: "workflowruns"},
				Object:    runtime.RawExtension{Raw: []byte("bad request")},
			},
		}
		resp := handler.Handle(ctx, req)
		Expect(resp.Allowed).Should(BeFalse())
	})

	It("Test WorkflowRun Validator [Allow]", func() {
		req := admission.Request{
			AdmissionRequest: admissionv1.AdmissionRequest{
				Operation: admissionv1.Create,
				Resource:  metav1.GroupVersionResource{Group: "core.oam.dev", Version: "v1alpha1", Resource: "workflowruns"},
				Object: runtime.RawExtension{
					Raw: []byte(`{"apiVersion":"core.oam.dev/v1alpha1","kind":"WorkflowRun","metadata":{"name":"wr-sample"},"spec":{"workflowSpec":{"steps":[{"name":"step1","properties":{"duration":"3s"},"type":"suspend"}]}}}`),
				},
			},
		}
		resp := handler.Handle(ctx, req)
		Expect(resp.Allowed).Should(BeTrue())
	})

	It("Test WorkflowRun Validator [Error]", func() {
		req := admission.Request{
			AdmissionRequest: admissionv1.AdmissionRequest{
				Operation: admissionv1.Create,
				Resource:  metav1.GroupVersionResource{Group: "core.oam.dev", Version: "v1alpha1", Resource: "workflowruns"},
				Object: runtime.RawExtension{
					Raw: []byte(
						`{"apiVersion":"core.oam.dev/v1alpha1","kind":"WorkflowRun","metadata":{"name":"wr-sample"},"spec":{"workflowSpec":{"steps":[{"properties":{"duration":"3s"},"type":"suspend"}]}}}`),
				},
			},
		}
		resp := handler.Handle(ctx, req)
		Expect(resp.Allowed).Should(BeFalse())
	})

	It("Test WorkflowRun Validator workflow step name duplicate [error]", func() {
		By("test duplicated step name in workflow")
		req := admission.Request{
			AdmissionRequest: admissionv1.AdmissionRequest{
				Operation: admissionv1.Create,
				Resource:  metav1.GroupVersionResource{Group: "core.oam.dev", Version: "v1alpha1", Resource: "workflowruns"},
				Object: runtime.RawExtension{
					Raw: []byte(`{"apiVersion":"core.oam.dev/v1alpha1","kind":"WorkflowRun","metadata":{"name":"wr-sample"},"spec":{"workflowSpec":{"steps":[{"name":"step1","properties":{"duration":"3s"},"type":"suspend"},{"name":"step1","properties":{"duration":"3s"},"type":"suspend"}]}}}`),
				},
			},
		}
		resp := handler.Handle(ctx, req)
		Expect(resp.Allowed).Should(BeFalse())

		By("test duplicated sub step name in workflow")
		req = admission.Request{
			AdmissionRequest: admissionv1.AdmissionRequest{
				Operation: admissionv1.Create,
				Resource:  metav1.GroupVersionResource{Group: "core.oam.dev", Version: "v1alpha1", Resource: "workflowruns"},
				Object: runtime.RawExtension{
					Raw: []byte(`{"apiVersion":"core.oam.dev/v1alpha1","kind":"WorkflowRun","metadata":{"name":"wr-sample"},"spec":{"workflowSpec":{"steps":[{"name":"step1","subSteps":[{"name":"sub1","type":"suspend"},{"name":"sub1","type":"suspend"}],"type":"step-group"}]}}}`),
				},
			},
		}
		resp = handler.Handle(ctx, req)
		Expect(resp.Allowed).Should(BeFalse())

		By("test duplicated sub and parent step name in workflow")
		req = admission.Request{
			AdmissionRequest: admissionv1.AdmissionRequest{
				Operation: admissionv1.Create,
				Resource:  metav1.GroupVersionResource{Group: "core.oam.dev", Version: "v1alpha1", Resource: "workflowruns"},
				Object: runtime.RawExtension{
					Raw: []byte(`{"apiVersion":"core.oam.dev/v1alpha1","kind":"WorkflowRun","metadata":{"name":"wr-sample"},"spec":{"workflowSpec":{"steps":[{"name":"step1","subSteps":[{"name":"step1","type":"suspend"},{"name":"sub1","type":"suspend"}],"type":"step-group"}]}}}`),
				},
			},
		}
		resp = handler.Handle(ctx, req)
		Expect(resp.Allowed).Should(BeFalse())
	})

	It("Test WorkflowRun Validator workflow step invalid timeout [error]", func() {
		req := admission.Request{
			AdmissionRequest: admissionv1.AdmissionRequest{
				Operation: admissionv1.Create,
				Resource:  metav1.GroupVersionResource{Group: "core.oam.dev", Version: "v1alpha1", Resource: "workflowruns"},
				Object: runtime.RawExtension{
					Raw: []byte(`{"apiVersion":"core.oam.dev/v1alpha1","kind":"WorkflowRun","metadata":{"name":"wr-sample"},"spec":{"workflowSpec":{"steps":[{"name":"step1","properties":{"duration":"3s"},"timeout":"test","type":"suspend"}]}}}`),
				},
			},
		}
		resp := handler.Handle(ctx, req)
		Expect(resp.Allowed).Should(BeFalse())
	})

	It("Test WorkflowRun Validator workflow step valid timeout [allow]", func() {
		req := admission.Request{
			AdmissionRequest: admissionv1.AdmissionRequest{
				Operation: admissionv1.Create,
				Resource:  metav1.GroupVersionResource{Group: "core.oam.dev", Version: "v1alpha1", Resource: "workflowruns"},
				Object: runtime.RawExtension{
					Raw: []byte(`{"apiVersion":"core.oam.dev/v1alpha1","kind":"WorkflowRun","metadata":{"name":"wr-sample"},"spec":{"workflowSpec":{"steps":[{"name":"step1","properties":{"duration":"3s"},"timeout":"5s","type":"suspend"}]}}}`),
				},
			},
		}
		resp := handler.Handle(ctx, req)
		Expect(resp.Allowed).Should(BeTrue())
	})

	It("Test WorkflowRun Validator [workflowRef from vela-system namespace, run in another namespace]", func() {
		workflow := &oamv1alpha1.Workflow{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "wf-shared-across-namespaces",
				Namespace: "vela-system",
			},
			WorkflowSpec: oamv1alpha1.WorkflowSpec{
				Steps: []oamv1alpha1.WorkflowStep{
					{WorkflowStepBase: oamv1alpha1.WorkflowStepBase{Name: "step1", Type: "suspend"}},
				},
			},
		}
		Expect(k8sClient.Create(ctx, workflow)).Should(Succeed())

		req := admission.Request{
			AdmissionRequest: admissionv1.AdmissionRequest{
				Operation: admissionv1.Create,
				Resource:  metav1.GroupVersionResource{Group: "core.oam.dev", Version: "v1alpha1", Resource: "workflowruns"},
				Object: runtime.RawExtension{
					Raw: []byte(fmt.Sprintf(`{"apiVersion":"core.oam.dev/v1alpha1","kind":"WorkflowRun","metadata":{"name":"wr-ref-sample","namespace":"default"},"spec":{"workflowRef":%q}}`, workflow.Name)),
				},
			},
		}
		resp := handler.Handle(ctx, req)
		Expect(resp.Allowed).Should(BeTrue())
	})

	It("Test WorkflowRun Validator [workflowRef not found anywhere, error]", func() {
		req := admission.Request{
			AdmissionRequest: admissionv1.AdmissionRequest{
				Operation: admissionv1.Create,
				Resource:  metav1.GroupVersionResource{Group: "core.oam.dev", Version: "v1alpha1", Resource: "workflowruns"},
				Object: runtime.RawExtension{
					Raw: []byte(`{"apiVersion":"core.oam.dev/v1alpha1","kind":"WorkflowRun","metadata":{"name":"wr-ref-missing","namespace":"default"},"spec":{"workflowRef":"does-not-exist"}}`),
				},
			},
		}
		resp := handler.Handle(ctx, req)
		Expect(resp.Allowed).Should(BeFalse())
	})

})
