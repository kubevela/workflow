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
	"path/filepath"
	"strings"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"

	"github.com/kubevela/pkg/util/singleton"
	"github.com/kubevela/pkg/util/test/definition"

	"github.com/kubevela/workflow/api/v1alpha1"
	"github.com/kubevela/workflow/pkg/utils"
)

var (
	scheme = runtime.NewScheme()
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	utilruntime.Must(v1alpha1.AddToScheme(scheme))
	//+kubebuilder:scaffold:scheme
}

func TestE2e(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "E2e Suite")
}

var k8sClient client.Client

var _ = BeforeSuite(func() {
	conf, err := config.GetConfig()
	Expect(err).Should(BeNil())
	singleton.KubeConfig.Set(conf)

	k8sClient, err = client.New(conf, client.Options{Scheme: scheme})
	Expect(err).Should(BeNil())
	singleton.KubeClient.Set(k8sClient)

	prepareWorkflowDefinitions()
})

func prepareWorkflowDefinitions() {
	var files []string
	err := filepath.Walk("./test-data/definitions/", func(path string, info os.FileInfo, err error) error {
		if strings.HasSuffix(path, "yaml") {
			files = append(files, path)
		}
		return nil
	})
	Expect(err).Should(BeNil())
	for _, f := range files {
		Expect(definition.InstallDefinitionFromYAML(context.TODO(), k8sClient, f, nil)).Should(SatisfyAny(BeNil(), &utils.AlreadyExistMatcher{}))
	}
}
