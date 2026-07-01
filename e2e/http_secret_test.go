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

package e2e

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kubevela/workflow/api/v1alpha1"
	"github.com/kubevela/workflow/pkg/utils"
)

const httpSecretTestNamespace = "http-secret-e2e-test"

var _ = Describe("Test the request step resolving headers from a Kubernetes Secret", func() {
	ctx := context.Background()

	BeforeEach(func() {
		ns := corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: httpSecretTestNamespace}}
		Eventually(func() error {
			return k8sClient.Create(ctx, &ns)
		}, time.Second*3, time.Microsecond*300).Should(SatisfyAny(BeNil(), &utils.AlreadyExistMatcher{}))

		secret := corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "api-creds", Namespace: httpSecretTestNamespace},
			StringData: map[string]string{"token": "Bearer sk-supersecret-12345"},
		}
		Expect(k8sClient.Create(ctx, &secret)).Should(SatisfyAny(BeNil(), &utils.AlreadyExistMatcher{}))

		deployHTTPBin(ctx)
	})

	It("should resolve the Authorization header from the Secret and reach the target", func() {
		wr := applyWorkflowRunFromYAML(ctx, "./test-data/http-secret-workflow-run.yaml", httpSecretTestNamespace)
		Eventually(
			func() v1alpha1.WorkflowRunPhase {
				var getWorkflow v1alpha1.WorkflowRun
				if err := k8sClient.Get(ctx, client.ObjectKey{Namespace: httpSecretTestNamespace, Name: wr.Name}, &getWorkflow); err != nil {
					klog.Errorf("fail to query the workflow run %s", err.Error())
				}
				klog.Infof("the workflow run status is %s (%+v)", getWorkflow.Status.Phase, getWorkflow.Status.Steps)
				return getWorkflow.Status.Phase
			},
			time.Second*60, time.Second*2).Should(Equal(v1alpha1.WorkflowStateSucceeded))
	})

	AfterEach(func() {
		By("Clean up resources after a test")
		k8sClient.DeleteAllOf(ctx, &v1alpha1.WorkflowRun{}, client.InNamespace(httpSecretTestNamespace))
	})
})

func deployHTTPBin(ctx context.Context) {
	replicas := int32(1)
	deploy := appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "httpbin", Namespace: httpSecretTestNamespace},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "httpbin"}},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "httpbin"}},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name:  "httpbin",
						Image: "kennethreitz/httpbin",
						Ports: []corev1.ContainerPort{{ContainerPort: 80}},
					}},
				},
			},
		},
	}
	Expect(k8sClient.Create(ctx, &deploy)).Should(SatisfyAny(BeNil(), &utils.AlreadyExistMatcher{}))

	svc := corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "httpbin", Namespace: httpSecretTestNamespace},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{"app": "httpbin"},
			Ports:    []corev1.ServicePort{{Port: 80, TargetPort: intstr.FromInt(80)}},
		},
	}
	Expect(k8sClient.Create(ctx, &svc)).Should(SatisfyAny(BeNil(), &utils.AlreadyExistMatcher{}))

	Eventually(func() int32 {
		var d appsv1.Deployment
		if err := k8sClient.Get(ctx, client.ObjectKey{Namespace: httpSecretTestNamespace, Name: "httpbin"}, &d); err != nil {
			return 0
		}
		return d.Status.ReadyReplicas
	}, time.Second*90, time.Second*2).Should(BeNumerically(">=", 1))
}
