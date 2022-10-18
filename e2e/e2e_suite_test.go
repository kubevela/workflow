package e2e

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/yaml"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"

	"github.com/kubevela/workflow/api/v1alpha1"

	"github.com/oam-dev/kubevela/apis/core.oam.dev/v1beta1"
	"github.com/oam-dev/kubevela/pkg/oam/util"
)

var (
	scheme = runtime.NewScheme()
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	utilruntime.Must(v1alpha1.AddToScheme(scheme))
	utilruntime.Must(v1beta1.AddToScheme(scheme))
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

	k8sClient, err = client.New(conf, client.Options{Scheme: scheme})
	Expect(err).Should(BeNil())

	prepareWorkflowDefinitions()
})

func prepareWorkflowDefinitions() {
	var files []string
	err := filepath.Walk("./definitions/", func(path string, info os.FileInfo, err error) error {
		if strings.HasSuffix(path, "yaml") {
			files = append(files, path)
		}
		return nil
	})
	Expect(err).Should(BeNil())
	for _, f := range files {
		content, err := os.ReadFile(f)
		Expect(err).Should(BeNil())
		var def v1beta1.WorkflowStepDefinition
		Expect(yaml.Unmarshal(content, &def)).Should(BeNil())
		Expect(k8sClient.Create(context.Background(), &def)).Should(SatisfyAny(BeNil(), &util.AlreadyExistMatcher{}))
	}
}
