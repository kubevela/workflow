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
	"os"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"

	"github.com/kubevela/workflow/api/v1alpha1"
	"github.com/kubevela/workflow/pkg/utils"
)

var _ = Describe("Test the workflow run with the built-in definitions", func() {
	ctx := context.Background()

	var namespace string
	var ns corev1.Namespace

	BeforeEach(func() {
		namespace = "wr-e2e-test"
		ns = corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}

		Eventually(func() error {
			return k8sClient.Create(ctx, &ns)
		}, time.Second*3, time.Microsecond*300).Should(SatisfyAny(BeNil(), &utils.AlreadyExistMatcher{}))
	})

	It("Test the workflow with config definition", func() {
		wr := applyWorkflowRunFromYAML(ctx, "./test-data/config-workflow-run.yaml", namespace)

		// Wait for workflow to complete
		Eventually(
			func() v1alpha1.WorkflowRunPhase {
				var getWorkflow v1alpha1.WorkflowRun
				if err := k8sClient.Get(ctx, client.ObjectKey{Namespace: namespace, Name: wr.Name}, &getWorkflow); err != nil {
					klog.Errorf("fail to query the app %s", err.Error())
				}
				klog.Infof("the workflow run status is %s (%+v)", getWorkflow.Status.Phase, getWorkflow.Status.Steps)
				return getWorkflow.Status.Phase
			},
			time.Second*30, time.Second*2).Should(Equal(v1alpha1.WorkflowStateSucceeded))

		// Verify the workflow steps
		var getWorkflow v1alpha1.WorkflowRun
		Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: namespace, Name: wr.Name}, &getWorkflow)).Should(BeNil())

		// 1. Verify create-config step was successful
		Expect(len(getWorkflow.Status.Steps)).Should(BeNumerically(">=", 4))
		createStep := getWorkflow.Status.Steps[0]
		Expect(createStep.Name).Should(Equal("write-config"))
		Expect(createStep.Phase).Should(Equal(v1alpha1.WorkflowStepPhaseSucceeded))
		klog.Infof("✓ Step 1: create-config succeeded")

		// 2. Verify read-config step was successful and value matches
		readStep := getWorkflow.Status.Steps[1]
		Expect(readStep.Name).Should(Equal("read-config"))
		Expect(readStep.Phase).Should(Equal(v1alpha1.WorkflowStepPhaseSucceeded))
		klog.Infof("✓ Step 2: read-config succeeded")

		// 3. Verify message step shows the correct value
		messageStep := getWorkflow.Status.Steps[2]
		Expect(messageStep.Name).Should(Equal("message"))
		Expect(messageStep.Phase).Should(Equal(v1alpha1.WorkflowStepPhaseSucceeded))
		Expect(messageStep.Message).Should(Equal("value1"))
		klog.Infof("✓ Step 3: message step verified with value: %s", messageStep.Message)

		// 4. Verify delete-config step was successful
		deleteStep := getWorkflow.Status.Steps[3]
		Expect(deleteStep.Name).Should(Equal("delete-config"))
		Expect(deleteStep.Phase).Should(Equal(v1alpha1.WorkflowStepPhaseSucceeded))
		klog.Infof("✓ Step 4: delete-config succeeded")

		// 5. Verify the secret no longer exists after deletion
		var secret corev1.Secret
		err := k8sClient.Get(ctx, client.ObjectKey{
			Namespace: namespace,
			Name:      "sample-config",
		}, &secret)
		Expect(err).ShouldNot(BeNil())
		klog.Infof("✓ Verified: secret 'sample-config' does not exist after deletion")
	})

	It("Test the workflow with config definition - verify secret creation", func() {
		// Apply workflow that only creates and reads config (without deletion)
		content, err := os.ReadFile("./test-data/config-workflow-run.yaml")
		Expect(err).Should(BeNil())
		var workflowRun v1alpha1.WorkflowRun
		Expect(yaml.Unmarshal(content, &workflowRun)).Should(BeNil())
		workflowRun.Namespace = namespace
		workflowRun.Name = "test-config-verify"
		// Remove the delete step temporarily
		workflowRun.Spec.WorkflowSpec.Steps = workflowRun.Spec.WorkflowSpec.Steps[:3]
		Expect(k8sClient.Create(ctx, &workflowRun)).Should(BeNil())

		// Wait for create and read steps to complete
		Eventually(
			func() bool {
				var getWorkflow v1alpha1.WorkflowRun
				if err := k8sClient.Get(ctx, client.ObjectKey{Namespace: namespace, Name: workflowRun.Name}, &getWorkflow); err != nil {
					return false
				}
				return len(getWorkflow.Status.Steps) >= 3 &&
					getWorkflow.Status.Steps[0].Phase == v1alpha1.WorkflowStepPhaseSucceeded &&
					getWorkflow.Status.Steps[1].Phase == v1alpha1.WorkflowStepPhaseSucceeded
			},
			time.Second*30, time.Second*2).Should(BeTrue())

		// 1. Verify the secret was created successfully
		var secret corev1.Secret
		Eventually(func() error {
			return k8sClient.Get(ctx, client.ObjectKey{
				Namespace: namespace,
				Name:      "sample-config",
			}, &secret)
		}, time.Second*5, time.Millisecond*500).Should(BeNil())
		klog.Infof("✓ Verified: secret 'sample-config' exists after creation")

		// 2. Verify the secret contains the correct data
		Expect(secret.Data).ShouldNot(BeNil())
		Expect(secret.Data["input-properties"]).ShouldNot(BeNil())

		// Parse the config data from the secret
		var configData map[string]interface{}
		err = yaml.Unmarshal(secret.Data["input-properties"], &configData)
		Expect(err).Should(BeNil())

		// Verify the config values
		Expect(configData["key1"]).Should(Equal("value1"))
		Expect(configData["key2"]).Should(Equal(float64(2))) // JSON numbers are float64
		Expect(configData["key3"]).Should(Equal(true))
		klog.Infof("✓ Verified: secret data matches expected values (key1=value1, key2=2, key3=true)")

		// Clean up - delete the secret manually
		Expect(k8sClient.Delete(ctx, &secret)).Should(BeNil())
		klog.Infof("✓ Cleanup: secret deleted")
	})

	It("Test the workflow with message definition", func() {
		wr := applyWorkflowRunFromYAML(ctx, "./test-data/message-workflow-run.yaml", namespace)
		Eventually(
			func() v1alpha1.WorkflowRunPhase {
				var getWorkflow v1alpha1.WorkflowRun
				if err := k8sClient.Get(ctx, client.ObjectKey{Namespace: namespace, Name: wr.Name}, &getWorkflow); err != nil {
					klog.Errorf("fail to query the app %s", err.Error())
				}
				klog.Infof("the workflow run status is %s (%+v)", getWorkflow.Status.Phase, getWorkflow.Status.Steps)
				return getWorkflow.Status.Phase
			},
			time.Second*30, time.Second*2).Should(Equal(v1alpha1.WorkflowStateSucceeded))
	})

	AfterEach(func() {
		By("Clean up resources after a test")
		k8sClient.DeleteAllOf(ctx, &v1alpha1.WorkflowRun{}, client.InNamespace(namespace))
	})
})

func applyWorkflowRunFromYAML(ctx context.Context, path, ns string) v1alpha1.WorkflowRun {
	content, err := os.ReadFile(path)
	Expect(err).Should(BeNil())
	var workflowRun v1alpha1.WorkflowRun
	Expect(yaml.Unmarshal(content, &workflowRun)).Should(BeNil())
	workflowRun.Namespace = ns
	Expect(k8sClient.Create(ctx, &workflowRun)).Should(BeNil())
	return workflowRun
}
