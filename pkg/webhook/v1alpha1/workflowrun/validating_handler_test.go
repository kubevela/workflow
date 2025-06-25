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
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	admissionv1 "k8s.io/api/admission/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
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

})
