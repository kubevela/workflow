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

package httpguard

import (
	"net"
	"net/http"
	"os"
	"strings"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"sigs.k8s.io/yaml"
)

func TestHTTPGuardSpecs(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "workflow HTTP guard")
}

var _ = Describe("SecureTransport", func() {
	It("works when http.DefaultTransport is not an *http.Transport", func() {
		previous := http.DefaultTransport
		DeferCleanup(func() { http.DefaultTransport = previous })
		http.DefaultTransport = roundTripperFunc(func(*http.Request) (*http.Response, error) { return nil, nil })

		var transport *http.Transport
		Expect(func() { transport = SecureTransport(nil, DefaultPolicy()) }).NotTo(Panic())
		Expect(transport.DialContext).NotTo(BeNil())
		Expect(transport.TLSHandshakeTimeout).To(BeNumerically(">", 0))
	})
})

var _ = Describe("deny host list", func() {
	It("stores a trailing-dot IP literal as an IP, so BlockedHost denies it", func() {
		policy, err := ParseDenyList("", "10.20.30.40.")
		Expect(err).NotTo(HaveOccurred())
		Expect(policy.ExactHosts).To(BeEmpty())

		Expect(policy.BlockedHost("10.20.30.40.")).To(MatchError(ContainSubstring("blocked SSRF target")))
		Expect(policy.BlockedHost("10.20.30.40:80")).To(MatchError(ContainSubstring("blocked SSRF target")))
	})
})

var _ = Describe("SetDenyFragment", func() {
	It("keeps its own copy, so later caller changes do not reach the active policy", func() {
		DeferCleanup(SetDenyFragment, Policy{})
		fragment, err := ParseDenyList("10.0.0.0/8", "blocked.example.com\n10.20.30.40")
		Expect(err).NotTo(HaveOccurred())
		SetDenyFragment(fragment)

		delete(fragment.ExactHosts, "blocked.example.com")
		fragment.DenyCIDRs[0].IP[0] = 192
		fragment.ExactIPs[0][15] = 41

		current := Current()
		Expect(current.BlockedHost("blocked.example.com")).To(HaveOccurred())
		Expect(current.Blocked(net.ParseIP("10.1.2.3"))).To(BeTrue())
		Expect(current.Blocked(net.ParseIP("10.20.30.40"))).To(BeTrue())
	})
})

var _ = Describe("builtin deny floor", func() {
	It("matches the defaults shipped in the vela-workflow chart", func() {
		raw, err := os.ReadFile("../../../charts/vela-workflow/values.yaml")
		Expect(err).NotTo(HaveOccurred())
		var values struct {
			Workflow struct {
				HTTPDeny struct {
					DefaultConfig struct {
						DenyHosts []string `json:"denyHosts"`
						DenyCIDRs []string `json:"denyCIDRs"`
					} `json:"defaultConfig"`
				} `json:"httpDeny"`
			} `json:"workflow"`
		}
		Expect(yaml.Unmarshal(raw, &values)).To(Succeed())

		shipped := values.Workflow.HTTPDeny.DefaultConfig
		Expect(strings.Join(shipped.DenyHosts, "\n")).To(Equal(denyHostsDefaults))
		Expect(strings.Join(shipped.DenyCIDRs, "\n")).To(Equal(denyCIDRsDefaults))
	})
})

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
