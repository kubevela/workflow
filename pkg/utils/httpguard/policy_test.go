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
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
)

func TestDefaultPolicy_blocksMetadataAndLinkLocal(t *testing.T) {
	policy := DefaultPolicy()

	blocked := []string{
		"169.254.169.254",
		"169.254.0.1",
		"fd00:ec2::254",
		"100.100.100.200",
	}
	for _, raw := range blocked {
		ip := net.ParseIP(raw)
		require.NotNil(t, ip, raw)
		assert.True(t, policy.Blocked(ip), "expected %s blocked", raw)
	}

	allowed := []string{
		"127.0.0.1",
		"10.0.0.1",
		"192.168.1.1",
		"8.8.8.8",
	}
	for _, raw := range allowed {
		ip := net.ParseIP(raw)
		require.NotNil(t, ip, raw)
		assert.False(t, policy.Blocked(ip), "expected %s allowed", raw)
	}
}

func TestParseDenyList(t *testing.T) {
	policy, err := ParseDenyList(`
# cidrs
10.0.0.0/8
169.254.169.254
`, `
metadata.google.internal
*.corp.internal
8.8.4.4
`)
	require.NoError(t, err)
	assert.True(t, policy.Blocked(net.ParseIP("10.1.2.3")))
	assert.True(t, policy.Blocked(net.ParseIP("169.254.169.254")))
	assert.True(t, policy.Blocked(net.ParseIP("8.8.4.4")))
	assert.False(t, policy.Blocked(net.ParseIP("8.8.8.8")))

	require.NoError(t, policy.BlockedHost("public.example.com"))
	require.Error(t, policy.BlockedHost("metadata.google.internal"))
	require.Error(t, policy.BlockedHost("a.corp.internal"))
	require.Error(t, policy.BlockedHost("a.b.corp.internal"))
	require.NoError(t, policy.BlockedHost("corp.internal"))
}

func TestParseDenyList_invalid(t *testing.T) {
	_, err := ParseDenyList("not-a-cidr", "")
	require.Error(t, err)
	_, err = ParseDenyList("", "foo.*.bar")
	require.Error(t, err)
	_, err = ParseDenyList("", "evil.example:443")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "port qualifiers are not supported")
}

func TestBlockedHost_trailingDot(t *testing.T) {
	fragment, err := ParseDenyList("", "blocked.example.com")
	require.NoError(t, err)
	policy := DefaultPolicy().MergeDeny(fragment)
	require.Error(t, policy.BlockedHost("blocked.example.com."))
}

func TestBlockedHost_ipv6Zone(t *testing.T) {
	policy := DefaultPolicy()
	require.Error(t, policy.BlockedHost("fe80::1%eth0"))
	require.Error(t, policy.BlockedAddress("[fe80::1%eth0]:80"))
}

func TestSecureTransport_ignoresPresetDialContext(t *testing.T) {
	base := http.DefaultTransport.(*http.Transport).Clone()
	base.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		return net.Dial(network, address)
	}
	client := &http.Client{
		Transport: SecureTransport(base, DefaultPolicy()),
	}
	_, err := client.Get("http://169.254.169.254/latest/meta-data/")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "blocked SSRF target")
}

func TestSecureTransport_wrapsDialTLSContext(t *testing.T) {
	base := http.DefaultTransport.(*http.Transport).Clone()
	base.DialTLSContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		return net.Dial(network, addr)
	}
	client := &http.Client{
		Transport: SecureTransport(base, DefaultPolicy()),
	}
	_, err := client.Get("https://169.254.169.254/latest/meta-data/")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "blocked SSRF target")
}

func TestParseConfigMap(t *testing.T) {
	cm := &corev1.ConfigMap{Data: map[string]string{
		ConfigMapKeyDenyCIDRs: "192.168.0.0/16",
		ConfigMapKeyDenyHosts: "evil.example",
	}}
	policy, err := ParseConfigMap(cm)
	require.NoError(t, err)
	assert.True(t, policy.Blocked(net.ParseIP("192.168.1.1")))
	require.Error(t, policy.BlockedHost("evil.example"))
}

func TestCurrent_mergesDenyAndEnhancer(t *testing.T) {
	t.Cleanup(func() {
		SetDenyFragment(Policy{ExactHosts: map[string]struct{}{}})
		SetEnhancer(nil)
	})
	SetEnhancer(func(p Policy) Policy {
		p.BlockPrivate = true
		return p
	})
	fragment, err := ParseDenyList("", "blocked.example")
	require.NoError(t, err)
	SetDenyFragment(fragment)

	cur := Current()
	assert.True(t, cur.BlockPrivate)
	assert.True(t, cur.BlockLinkLocal)
	require.Error(t, cur.BlockedHost("blocked.example"))
	assert.True(t, cur.Blocked(net.ParseIP("10.0.0.1")))
}

func TestSecureTransport_blocksLinkLocalDial(t *testing.T) {
	client := &http.Client{
		Transport: SecureTransport(http.DefaultTransport.(*http.Transport).Clone(), DefaultPolicy()),
	}
	_, err := client.Get("http://169.254.169.254/latest/meta-data/")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "blocked SSRF target")
}

func TestSecureTransport_allowsLoopback(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer ts.Close()

	client := &http.Client{
		Transport: SecureTransport(http.DefaultTransport.(*http.Transport).Clone(), DefaultPolicy()),
	}
	resp, err := client.Get(ts.URL)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestSecureTransport_redirectRevalidated(t *testing.T) {
	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://169.254.169.254/latest/meta-data/", http.StatusFound)
	}))
	defer redirector.Close()

	client := &http.Client{
		Transport: SecureTransport(http.DefaultTransport.(*http.Transport).Clone(), DefaultPolicy()),
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return nil
		},
	}
	_, err := client.Get(redirector.URL)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "blocked SSRF target")
}

func TestSecureTransport_blocksDenyCIDR(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	fragment, err := ParseDenyList("127.0.0.0/8", "")
	require.NoError(t, err)
	policy := DefaultPolicy().MergeDeny(fragment)
	client := &http.Client{
		Transport: SecureTransport(http.DefaultTransport.(*http.Transport).Clone(), policy),
	}
	_, err = client.Get(ts.URL)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "blocked SSRF target")
}
