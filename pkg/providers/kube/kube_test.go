/*
Copyright 2021 The KubeVela Authors.

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

package kube

import (
	"context"
	"fmt"
	"testing"
	"time"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	crdv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	k8stypes "k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"

	providertypes "github.com/kubevela/workflow/pkg/providers/types"
)

// These tests use Ginkgo (BDD-style Go testing framework). Refer to
// http://onsi.github.io/ginkgo/ to learn more about Ginkgo.

var cfg *rest.Config
var k8sClient client.Client
var testEnv *envtest.Environment
var scheme = runtime.NewScheme()

func TestProvider(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecs(t, "Test Definition Suite")
}

var _ = BeforeSuite(func(ctx SpecContext) {
	By("Bootstrapping test environment")
	testEnv = &envtest.Environment{
		ControlPlaneStartTimeout: time.Minute,
		ControlPlaneStopTimeout:  time.Minute,
		UseExistingCluster:       ptr.To(false),
	}
	var err error
	cfg, err = testEnv.Start()
	Expect(err).ToNot(HaveOccurred())
	Expect(cfg).ToNot(BeNil())
	Expect(clientgoscheme.AddToScheme(scheme)).Should(BeNil())
	Expect(crdv1.AddToScheme(scheme)).Should(BeNil())
	// +kubebuilder:scaffold:scheme
	By("Create the k8s client")
	k8sClient, err = client.New(cfg, client.Options{Scheme: scheme})
	Expect(err).ToNot(HaveOccurred())
	Expect(k8sClient).ToNot(BeNil())
}, NodeTimeout(2*time.Minute))

var _ = AfterSuite(func() {
	By("Tearing down the test environment")
	err := testEnv.Stop()
	Expect(err).ToNot(HaveOccurred())
})

var _ = Describe("Test Workflow Provider Kube", func() {
	It("apply and read", func() {
		ctx := context.Background()
		un := testUnstructured.DeepCopy()
		un.SetName("app")
		un.SetLabels(map[string]string{
			"test": "test",
		})
		res, err := Apply(ctx, &ResourceParams{
			Params: ResourceVars{
				Resource: un,
			},
			RuntimeParams: providertypes.RuntimeParams{
				Labels: map[string]string{
					"hello": "world",
				},
				KubeClient: k8sClient,
			},
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(res.Returns.Resource.GetLabels()).Should(Equal(un.GetLabels()))
		workload := &corev1.Pod{}
		Eventually(func() error {
			return k8sClient.Get(context.Background(), client.ObjectKey{
				Namespace: "default",
				Name:      "app",
			}, workload)
		}, time.Second*2, time.Millisecond*300).Should(BeNil())
		Expect(workload.GetLabels()).Should(Equal(map[string]string{
			"test":  "test",
			"hello": "world",
		}))

		res, err = Read(ctx, &ResourceParams{
			Params: ResourceVars{
				Resource: &unstructured.Unstructured{
					Object: map[string]interface{}{
						"apiVersion": "v1",
						"kind":       "Pod",
						"metadata": map[string]interface{}{
							"name": "app",
						},
					},
				},
			},
			RuntimeParams: providertypes.RuntimeParams{
				Labels: map[string]string{
					"hello": "world",
				},
				KubeClient: k8sClient,
			},
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(res.Returns.Error).Should(Equal(""))
		Expect(res.Returns.Resource.GetLabels()).Should(Equal(map[string]string{
			"test":  "test",
			"hello": "world",
		}))
	})

	It("patch & apply", func() {
		ctx := context.Background()

		un := testUnstructured
		un.SetName("test-app-1")
		un.SetLabels(map[string]string{
			"test": "test",
		})
		_, err := Apply(ctx, &ResourceParams{
			Params: ResourceVars{
				Resource: &un,
			},
			RuntimeParams: providertypes.RuntimeParams{
				KubeClient: k8sClient,
			},
		})
		Expect(err).ToNot(HaveOccurred())

		v := cuecontext.New().CompileString(`
$params: {
	value: {
		apiVersion: "v1"
		kind:       "Pod"
		metadata: name: "test-app-1"
	}
	cluster: ""
	patch: {
		metadata: name: "test-app-1"
		spec: {
			containers: [{
				// +patchStrategy=retainKeys
				image: "nginx:notfound"
			}]
		}
	}
}
`)
		_, err = Patch(ctx, &providertypes.Params[cue.Value]{
			Params: v,
			RuntimeParams: providertypes.RuntimeParams{
				KubeClient: k8sClient,
			},
		})
		Expect(err).ToNot(HaveOccurred())

		pod := &corev1.Pod{}
		Expect(err).ToNot(HaveOccurred())
		Eventually(func() error {
			return k8sClient.Get(context.Background(), client.ObjectKey{
				Namespace: "default",
				Name:      "test-app-1",
			}, pod)
		}, time.Second*2, time.Millisecond*300).Should(BeNil())
		Expect(pod.Name).To(Equal("test-app-1"))
		Expect(pod.Spec.Containers[0].Image).To(Equal("nginx:notfound"))
	})

	It("list", func() {
		ctx := context.Background()
		for i := 2; i >= 0; i-- {
			err := k8sClient.Create(ctx, &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      fmt.Sprintf("test-%v", i),
					Namespace: "default",
					Labels: map[string]string{
						"test":  "test",
						"index": fmt.Sprintf("test-%v", i),
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  fmt.Sprintf("test-%v", i),
							Image: "busybox",
						},
					},
				},
			})
			Expect(err).ToNot(HaveOccurred())
		}

		By("List pods with labels test=test")
		res, err := List(ctx, &ResourceParams{
			Params: ResourceVars{
				Resource: &unstructured.Unstructured{
					Object: map[string]interface{}{
						"apiVersion": "v1",
						"kind":       "Pod",
					},
				},
				Filter: &ListFilter{
					Namespace: "default",
					MatchingLabels: map[string]string{
						"test": "test",
					},
				},
			},
			RuntimeParams: providertypes.RuntimeParams{
				KubeClient: k8sClient,
			},
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(len(res.Returns.Resources.Items)).Should(Equal(5))

		By("List pods with labels index=test-1")
		res, err = List(ctx, &ResourceParams{
			Params: ResourceVars{
				Resource: &unstructured.Unstructured{
					Object: map[string]interface{}{
						"apiVersion": "v1",
						"kind":       "Pod",
					},
				},
				Filter: &ListFilter{
					MatchingLabels: map[string]string{
						"index": "test-1",
					},
				},
			},
			RuntimeParams: providertypes.RuntimeParams{
				KubeClient: k8sClient,
			},
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(len(res.Returns.Resources.Items)).Should(Equal(1))
	})

	It("delete", func() {
		ctx := context.Background()
		err := k8sClient.Create(ctx, &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test",
				Namespace: "default",
			},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name:  "test",
						Image: "busybox",
					},
				},
			},
		})
		Expect(err).ToNot(HaveOccurred())
		err = k8sClient.Get(ctx, k8stypes.NamespacedName{
			Name:      "test",
			Namespace: "default",
		}, &corev1.Pod{})
		Expect(err).ToNot(HaveOccurred())

		_, err = Delete(ctx, &ResourceParams{
			Params: ResourceVars{
				Resource: &unstructured.Unstructured{
					Object: map[string]interface{}{
						"apiVersion": "v1",
						"kind":       "Pod",
						"metadata": map[string]interface{}{
							"name":      "test",
							"namespace": "default",
						},
					},
				},
			},
			RuntimeParams: providertypes.RuntimeParams{
				KubeClient: k8sClient,
			},
		})
		Expect(err).ToNot(HaveOccurred())
		err = k8sClient.Get(ctx, k8stypes.NamespacedName{
			Name:      "test",
			Namespace: "default",
		}, &corev1.Pod{})
		Expect(err).To(HaveOccurred())
		Expect(errors.IsNotFound(err)).Should(Equal(true))
	})

	It("delete with labels", func() {
		ctx := context.Background()
		err := k8sClient.Create(ctx, &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test",
				Namespace: "default",
				Labels: map[string]string{
					"test.oam.dev": "true",
				},
			},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name:  "test",
						Image: "busybox",
					},
				},
			},
		})
		Expect(err).ToNot(HaveOccurred())
		err = k8sClient.Get(ctx, k8stypes.NamespacedName{
			Name:      "test",
			Namespace: "default",
		}, &corev1.Pod{})
		Expect(err).ToNot(HaveOccurred())

		_, err = Delete(ctx, &ResourceParams{
			Params: ResourceVars{
				Resource: &unstructured.Unstructured{
					Object: map[string]interface{}{
						"apiVersion": "v1",
						"kind":       "Pod",
						"metadata": map[string]interface{}{
							"namespace": "default",
						},
					},
				},
				Filter: &ListFilter{
					Namespace: "default",
					MatchingLabels: map[string]string{
						"test.oam.dev": "true",
					},
				},
			},
			RuntimeParams: providertypes.RuntimeParams{
				KubeClient: k8sClient,
			},
		})
		Expect(err).ToNot(HaveOccurred())
		err = k8sClient.Get(ctx, k8stypes.NamespacedName{
			Name:      "test",
			Namespace: "default",
		}, &corev1.Pod{})
		Expect(err).To(HaveOccurred())
		Expect(errors.IsNotFound(err)).Should(Equal(true))
	})

	It("apply parallel", func() {
		un1 := testUnstructured.DeepCopy()
		un1.SetName("app1")
		un2 := testUnstructured.DeepCopy()
		un2.SetName("app2")
		ctx := context.Background()
		_, err := ApplyInParallel(ctx, &ApplyInParallelParams{
			Params: ApplyInParallelVars{
				Resources: []*unstructured.Unstructured{un1, un2},
			},
			RuntimeParams: providertypes.RuntimeParams{
				KubeClient: k8sClient,
			},
		})
		Expect(err).ToNot(HaveOccurred())
		pod := &corev1.Pod{}
		Eventually(func() error {
			return k8sClient.Get(context.Background(), client.ObjectKey{
				Namespace: "default",
				Name:      "app1",
			}, pod)
		}, time.Second*2, time.Millisecond*300).Should(BeNil())
		Eventually(func() error {
			return k8sClient.Get(context.Background(), client.ObjectKey{
				Namespace: "default",
				Name:      "app2",
			}, pod)
		}, time.Second*2, time.Millisecond*300).Should(BeNil())
	})

	It("test error case", func() {
		ctx := context.Background()
		res, err := Read(ctx, &ResourceParams{
			Params: ResourceVars{
				Resource: &unstructured.Unstructured{
					Object: map[string]interface{}{
						"apiVersion": "v1",
						"kind":       "Pod",
						"metadata": map[string]interface{}{
							"name": "not-exist",
						},
					},
				},
			},
			RuntimeParams: providertypes.RuntimeParams{
				KubeClient: k8sClient,
			},
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(res.Returns.Error).ShouldNot(BeNil())
	})
})

var (
	testUnstructured = unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "Pod",
			"metadata":   map[string]interface{}{},
			"spec": map[string]interface{}{
				"containers": []interface{}{
					map[string]interface{}{
						"name":  "main",
						"image": "nginx:1.14.2",
						"env": []interface{}{
							map[string]interface{}{
								"name":  "APP",
								"value": "nginx",
							},
						},
					},
				},
			},
		},
	}
)
