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

package e2e

import (
	"context"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kubevela/workflow/api/v1alpha1"
	"github.com/kubevela/workflow/pkg/utils"
)

// These need the controller run with --feature-gates=EnableCelExpressions=true.
var _ = Describe("Test a workflow run with $( ) expressions", func() {
	ctx := context.Background()
	namespace := "wr-e2e-expressions"

	BeforeEach(func() {
		ns := corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}
		Eventually(func() error {
			return k8sClient.Create(ctx, &ns)
		}, time.Second*3, time.Microsecond*300).Should(SatisfyAny(BeNil(), &utils.AlreadyExistMatcher{}))
	})

	It("reads earlier steps' outputs, loops over them, and orders by the reads", func() {
		wr := applyWorkflowRunFromYAML(ctx, "./test-data/expressions-workflow-run.yaml", namespace)
		// The run and the configs it writes have fixed names: remove them, so the
		// suite can run again against the same cluster.
		DeferCleanup(func() {
			Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, &wr))).Should(Succeed())
			for _, name := range []string{"tenants", "greeting-a", "greeting-b", "summary"} {
				secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: name}}
				Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, secret))).Should(Succeed())
			}
		})
		var run v1alpha1.WorkflowRun
		Eventually(func(g Gomega) {
			g.Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: namespace, Name: wr.Name}, &run)).Should(Succeed())
			g.Expect(run.Status.Phase).Should(Equal(v1alpha1.WorkflowStateSucceeded), "%+v", run.Status.Steps)
		}, time.Second*60, time.Second*2).Should(Succeed())

		By("each config holds the values its expressions read")
		configText := func(name string) string {
			var secret corev1.Secret
			Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, &secret)).Should(Succeed())
			var all []string
			for _, v := range secret.Data {
				all = append(all, string(v))
			}
			return strings.Join(all, "\n")
		}
		Expect(configText("greeting-a")).Should(ContainSubstring("hi a #0 in expressions"))
		Expect(configText("greeting-b")).Should(ContainSubstring("hi b #1 in expressions"))
		Expect(configText("summary")).Should(ContainSubstring("2 tenants, from expressions"))

		By("the status records which dependencies the reads added")
		Expect(run.Status.Dependencies).Should(Equal([]v1alpha1.StepDependency{
			{Step: "list", DependsOn: "create", Source: v1alpha1.StepDependencySourceDependsOn},
			{Step: "per-tenant", DependsOn: "list", Source: v1alpha1.StepDependencySourceExpression},
			{Step: "summary", DependsOn: "list", Source: v1alpha1.StepDependencySourceExpression},
		}))
	})
})
