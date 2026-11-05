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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kubevela/workflow/pkg/utils/httpguard"
)

var _ = Describe("guarded redirects", func() {
	BeforeEach(func() {
		httpguard.SetDenyFragment(httpguard.Policy{ExactHosts: map[string]struct{}{}})
	})

	It("stops after the net/http redirect limit", func() {
		hops := 0
		var server *httptest.Server
		server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			hops++
			http.Redirect(w, r, server.URL+"/loop", http.StatusFound)
		}))
		DeferCleanup(server.Close)

		_, err := Do(context.Background(), &DoParams{
			Params: RequestVars{Method: "GET", URL: server.URL},
		})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("stopped after 10 redirects"))
		Expect(hops).To(Equal(maxRedirects))
	})
})
