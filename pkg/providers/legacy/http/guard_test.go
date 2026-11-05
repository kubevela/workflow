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

package http

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	utilfeature "k8s.io/apiserver/pkg/util/feature"

	"github.com/kubevela/workflow/pkg/features"
	"github.com/kubevela/workflow/pkg/utils/httpguard"
)

func TestLegacyHTTPProviderSpecs(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "legacy op.#HTTPDo provider")
}

var _ = Describe("legacy op.#HTTPDo guard", func() {
	do := func(url string) (*ResponseVars, error) {
		return Do(context.Background(), &DoParams{Params: RequestVars{Method: "GET", URL: url}})
	}
	setGate := func(gate string, on bool) {
		previous := utilfeature.DefaultMutableFeatureGate.Enabled(features.DisableWorkflowHTTP)
		if gate == string(features.BlockPrivateHTTPAddresses) {
			previous = utilfeature.DefaultMutableFeatureGate.Enabled(features.BlockPrivateHTTPAddresses)
		}
		DeferCleanup(func() {
			Expect(utilfeature.DefaultMutableFeatureGate.SetFromMap(map[string]bool{gate: previous})).To(Succeed())
		})
		Expect(utilfeature.DefaultMutableFeatureGate.SetFromMap(map[string]bool{gate: on})).To(Succeed())
	}

	BeforeEach(func() {
		httpguard.SetDenyFragment(httpguard.Policy{})
	})

	It("blocks the cloud metadata address", func() {
		_, err := do("http://169.254.169.254/latest/meta-data/")
		Expect(err).To(MatchError(ContainSubstring("blocked SSRF target")))
	})

	It("blocks a redirect into the cloud metadata address", func() {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "http://169.254.169.254/latest/meta-data/", http.StatusFound)
		}))
		DeferCleanup(server.Close)

		_, err := do(server.URL)
		Expect(err).To(MatchError(ContainSubstring("blocked SSRF target")))
	})

	It("stops a redirect loop at the net/http limit", func() {
		hops := 0
		var server *httptest.Server
		server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			hops++
			http.Redirect(w, r, server.URL+"/loop", http.StatusFound)
		}))
		DeferCleanup(server.Close)

		_, err := do(server.URL)
		Expect(err).To(MatchError(ContainSubstring("stopped after 10 redirects")))
		Expect(hops).To(Equal(maxRedirects))
	})

	It("blocks private addresses when BlockPrivateHTTPAddresses is on", func() {
		setGate(string(features.BlockPrivateHTTPAddresses), true)

		_, err := do("http://10.0.0.1/")
		Expect(err).To(MatchError(ContainSubstring("blocked SSRF target")))
	})

	It("refuses every request when DisableWorkflowHTTP is on", func() {
		setGate(string(features.DisableWorkflowHTTP), true)

		_, err := do("http://127.0.0.1:1/")
		Expect(err).To(MatchError(ContainSubstring("DisableWorkflowHTTP")))
	})

	It("still serves an allowed address", func() {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("ok"))
		}))
		DeferCleanup(server.Close)

		res, err := do(server.URL)
		Expect(err).NotTo(HaveOccurred())
		Expect(res.StatusCode).To(Equal(http.StatusOK))
		Expect(res.Body).To(Equal("ok"))
	})
})
