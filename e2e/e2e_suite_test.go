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
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"

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

	k8sClient, err = client.New(conf, client.Options{Scheme: scheme})
	Expect(err).Should(BeNil())

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
