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
	"encoding/json"
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

		// Verify all 6 steps completed successfully
		Expect(len(getWorkflow.Status.Steps)).Should(BeNumerically(">=", 6))

		// 1. Verify create-config step was successful
		createStep := getWorkflow.Status.Steps[0]
		Expect(createStep.Name).Should(Equal("write-config"))
		Expect(createStep.Phase).Should(Equal(v1alpha1.WorkflowStepPhaseSucceeded))
		klog.Infof("✓ Step 1: write-config (create-config) succeeded")

		// 2. Verify list-config step was successful
		listStep := getWorkflow.Status.Steps[1]
		Expect(listStep.Name).Should(Equal("list-config"))
		Expect(listStep.Phase).Should(Equal(v1alpha1.WorkflowStepPhaseSucceeded))
		klog.Infof("✓ Step 2: list-config succeeded")

		// 3. Verify save-list-to-configmap step was successful
		saveStep := getWorkflow.Status.Steps[2]
		Expect(saveStep.Name).Should(Equal("save-list-to-configmap"))
		Expect(saveStep.Phase).Should(Equal(v1alpha1.WorkflowStepPhaseSucceeded))
		klog.Infof("✓ Step 3: save-list-to-configmap (export2config) succeeded")

		// 4. Verify read-config step was successful
		readStep := getWorkflow.Status.Steps[3]
		Expect(readStep.Name).Should(Equal("read-config"))
		Expect(readStep.Phase).Should(Equal(v1alpha1.WorkflowStepPhaseSucceeded))
		klog.Infof("✓ Step 4: read-config succeeded")

		// 5. Verify message step shows the correct value
		messageStep := getWorkflow.Status.Steps[4]
		Expect(messageStep.Name).Should(Equal("message"))
		Expect(messageStep.Phase).Should(Equal(v1alpha1.WorkflowStepPhaseSucceeded))
		Expect(messageStep.Message).Should(Equal("value1"))
		klog.Infof("✓ Step 5: message step verified with value: %s", messageStep.Message)

		// 6. Verify delete-config step was successful
		deleteStep := getWorkflow.Status.Steps[5]
		Expect(deleteStep.Name).Should(Equal("delete-config"))
		Expect(deleteStep.Phase).Should(Equal(v1alpha1.WorkflowStepPhaseSucceeded))
		klog.Infof("✓ Step 6: delete-config succeeded")

		// 7. Verify the secret no longer exists after deletion
		var secret corev1.Secret
		err := k8sClient.Get(ctx, client.ObjectKey{
			Namespace: namespace,
			Name:      "sample-config",
		}, &secret)
		Expect(err).ShouldNot(BeNil())
		klog.Infof("✓ Verified: secret 'sample-config' does not exist after deletion")

		// 8. Verify the ConfigMap 'list-config-result' was created with correct data
		var configMap corev1.ConfigMap
		err = k8sClient.Get(ctx, client.ObjectKey{
			Namespace: namespace,
			Name:      "list-config-result",
		}, &configMap)
		Expect(err).Should(BeNil())
		klog.Infof("✓ Verified: ConfigMap 'list-config-result' exists")

		// Parse the configs from ConfigMap and verify content matches create-config data
		configsJSON := configMap.Data["configs"]
		Expect(configsJSON).ShouldNot(BeEmpty())

		var configs []map[string]interface{}
		err = json.Unmarshal([]byte(configsJSON), &configs)
		Expect(err).Should(BeNil())
		Expect(len(configs)).Should(BeNumerically(">=", 1))

		// Find the sample-config in the list
		var foundConfig map[string]interface{}
		for _, cfg := range configs {
			if cfg["name"] == "sample-config" {
				foundConfig = cfg
				break
			}
		}
		Expect(foundConfig).ShouldNot(BeNil())

		// Verify the config content matches what was created
		configData := foundConfig["config"].(map[string]interface{})
		Expect(configData["key1"]).Should(Equal("value1"))
		Expect(configData["key2"]).Should(Equal(float64(2))) // JSON numbers are float64
		Expect(configData["key3"]).Should(Equal(true))
		nestedData := configData["nested"].(map[string]interface{})
		Expect(nestedData["inner"]).Should(Equal("value"))
		klog.Infof("✓ Verified: ConfigMap data matches create-config data (key1=value1, key2=2, key3=true, nested.inner=value)")

		// Clean up the ConfigMap
		Expect(k8sClient.Delete(ctx, &configMap)).Should(BeNil())
		klog.Infof("✓ Cleanup: ConfigMap 'list-config-result' deleted")
	})

	It("Test the workflow with config definition - verify secret and configmap creation", func() {
		// Apply workflow that creates config, lists it, saves to configmap, reads it (without deletion)
		content, err := os.ReadFile("./test-data/config-workflow-run.yaml")
		Expect(err).Should(BeNil())
		var workflowRun v1alpha1.WorkflowRun
		Expect(yaml.Unmarshal(content, &workflowRun)).Should(BeNil())
		workflowRun.Namespace = namespace
		workflowRun.Name = "test-config-verify"
		// Remove the delete step (keep first 5 steps: write-config, list-config, save-list-to-configmap, read-config, message)
		workflowRun.Spec.WorkflowSpec.Steps = workflowRun.Spec.WorkflowSpec.Steps[:5]
		Expect(k8sClient.Create(ctx, &workflowRun)).Should(BeNil())

		// Wait for all 5 steps to complete
		Eventually(
			func() bool {
				var getWorkflow v1alpha1.WorkflowRun
				if err := k8sClient.Get(ctx, client.ObjectKey{Namespace: namespace, Name: workflowRun.Name}, &getWorkflow); err != nil {
					return false
				}
				return getWorkflow.Status.Phase == v1alpha1.WorkflowStateSucceeded
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
		var secretConfigData map[string]interface{}
		err = json.Unmarshal(secret.Data["input-properties"], &secretConfigData)
		Expect(err).Should(BeNil())

		// Verify the config values in secret
		Expect(secretConfigData["key1"]).Should(Equal("value1"))
		Expect(secretConfigData["key2"]).Should(Equal(float64(2))) // JSON numbers are float64
		Expect(secretConfigData["key3"]).Should(Equal(true))
		klog.Infof("✓ Verified: secret data matches expected values (key1=value1, key2=2, key3=true)")

		// 3. Verify the ConfigMap was created and contains the same data
		var configMap corev1.ConfigMap
		err = k8sClient.Get(ctx, client.ObjectKey{
			Namespace: namespace,
			Name:      "list-config-result",
		}, &configMap)
		Expect(err).Should(BeNil())
		klog.Infof("✓ Verified: ConfigMap 'list-config-result' exists")

		// Parse the configs from ConfigMap
		configsJSON := configMap.Data["configs"]
		Expect(configsJSON).ShouldNot(BeEmpty())

		var configs []map[string]interface{}
		err = json.Unmarshal([]byte(configsJSON), &configs)
		Expect(err).Should(BeNil())
		Expect(len(configs)).Should(BeNumerically(">=", 1))

		// Find the sample-config in the list and verify it matches the secret data
		var foundConfig map[string]interface{}
		for _, cfg := range configs {
			if cfg["name"] == "sample-config" {
				foundConfig = cfg
				break
			}
		}
		Expect(foundConfig).ShouldNot(BeNil())

		// Verify the ConfigMap config content matches the secret data (create-config data)
		configData := foundConfig["config"].(map[string]interface{})
		Expect(configData["key1"]).Should(Equal(secretConfigData["key1"]))
		Expect(configData["key2"]).Should(Equal(secretConfigData["key2"]))
		Expect(configData["key3"]).Should(Equal(secretConfigData["key3"]))
		klog.Infof("✓ Verified: ConfigMap config data matches secret (create-config) data")

		// Clean up - delete the secret and configmap manually
		Expect(k8sClient.Delete(ctx, &secret)).Should(BeNil())
		klog.Infof("✓ Cleanup: secret deleted")
		Expect(k8sClient.Delete(ctx, &configMap)).Should(BeNil())
		klog.Infof("✓ Cleanup: ConfigMap deleted")
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
