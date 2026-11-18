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
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
)

func TestShippedDefaultDeny_blocksMetadataAndLinkLocal(t *testing.T) {
	policy := shippedDefaultDeny(t)

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
	require.Error(t, policy.BlockedHost("metadata.google.internal"))

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

func shippedDefaultDeny(t *testing.T) Policy {
	t.Helper()
	policy := BuiltinDeny()
	require.NoError(t, builtinDenyLoadError())
	require.NotEmpty(t, policy.ExactHosts)
	require.NotEmpty(t, policy.DenyCIDRs)
	return policy
}

func TestBuiltinDeny_returnsIndependentCopy(t *testing.T) {
	first := BuiltinDeny()
	require.Error(t, first.BlockedHost("metadata.google.internal"))
	delete(first.ExactHosts, "metadata.google.internal")
	second := BuiltinDeny()
	require.Error(t, second.BlockedHost("metadata.google.internal"))
	require.NoError(t, first.BlockedHost("metadata.google.internal"))
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
	_, err = ParseDenyList("", "evil.example/path")
	require.Error(t, err)
	_, err = ParseDenyList("", "*.corp.internal/evil")
	require.Error(t, err)
	_, err = ParseDenyList("", "http://evil.example")
	require.Error(t, err)
}

func TestMergeDeny_doesNotMutateSourceMaps(t *testing.T) {
	base := Policy{ExactHosts: map[string]struct{}{"keep.example": {}}}
	other := Policy{ExactHosts: map[string]struct{}{"deny.example": {}}}
	merged := base.MergeDeny(other)
	require.Error(t, merged.BlockedHost("deny.example"))
	require.NoError(t, base.BlockedHost("deny.example"))
	require.NoError(t, other.BlockedHost("keep.example"))
	require.Len(t, base.ExactHosts, 1)
	require.Len(t, other.ExactHosts, 1)
}

func TestSecureTransport_disablesInheritedProxy(t *testing.T) {
	base := http.DefaultTransport.(*http.Transport).Clone()
	require.NotNil(t, base.Proxy)
	transport := SecureTransport(base, shippedDefaultDeny(t))
	require.Nil(t, transport.Proxy)
}

func TestBlockedHost_trailingDot(t *testing.T) {
	fragment, err := ParseDenyList("", "blocked.example.com")
	require.NoError(t, err)
	policy := DefaultPolicy().MergeDeny(fragment)
	require.Error(t, policy.BlockedHost("blocked.example.com."))
}

func TestBlockedHost_ipv6Zone(t *testing.T) {
	policy := shippedDefaultDeny(t)
	require.Error(t, policy.BlockedHost("fe80::1%eth0"))
	require.Error(t, policy.BlockedAddress("[fe80::1%eth0]:80"))
}

func TestSecureTransport_ignoresPresetDialContext(t *testing.T) {
	base := http.DefaultTransport.(*http.Transport).Clone()
	base.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		return net.Dial(network, address)
	}
	client := &http.Client{
		Transport: SecureTransport(base, shippedDefaultDeny(t)),
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
		Transport: SecureTransport(base, shippedDefaultDeny(t)),
	}
	_, err := client.Get("https://169.254.169.254/latest/meta-data/")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "blocked SSRF target")
}

func TestParseConfigMap_nil(t *testing.T) {
	policy, err := ParseConfigMap(nil)
	require.NoError(t, err)
	require.NotNil(t, policy.ExactHosts)
}

func TestParseDenyList_emptyWildcard(t *testing.T) {
	_, err := ParseDenyList("", "*.")
	require.Error(t, err)
}

func TestParseDenyList_commentOnlyLine(t *testing.T) {
	policy, err := ParseDenyList("10.0.0.0/8 # private", "")
	require.NoError(t, err)
	assert.True(t, policy.Blocked(net.ParseIP("10.0.0.1")))
}

func TestParseDenyList_emptyHostAfterDot(t *testing.T) {
	_, err := ParseDenyList("", ".")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid empty deny host")
}

func TestParseDenyList_hostWithColon(t *testing.T) {
	_, err := ParseDenyList("", "evil:host")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "port qualifiers are not supported")

	_, err = ParseDenyList("", "a:b:c")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "port or scheme qualifiers are not supported")
}

func TestParseDenyList_hostWithWhitespace(t *testing.T) {
	_, err := ParseDenyList("", "evil example")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "path or whitespace")
}

func TestPolicy_blockLoopbackAndPrivate(t *testing.T) {
	policy := Policy{
		BlockLoopback: true,
		BlockPrivate:  true,
		ExactHosts:    map[string]struct{}{},
	}
	assert.True(t, policy.Blocked(net.ParseIP("127.0.0.1")))
	assert.True(t, policy.Blocked(net.ParseIP("10.0.0.1")))
	assert.True(t, policy.Blocked(net.ParseIP("fd00::1")))
}

func TestMergeDeny_copiesAllFields(t *testing.T) {
	_, cidr, err := net.ParseCIDR("192.168.0.0/16")
	require.NoError(t, err)
	other := Policy{
		DenyCIDRs:        []*net.IPNet{cidr},
		ExactIPs:         []net.IP{net.ParseIP("8.8.4.4")},
		ExactHosts:       map[string]struct{}{"evil.example": {}},
		WildcardSuffixes: []string{"corp.internal"},
	}
	merged := DefaultPolicy().MergeDeny(other)
	assert.True(t, merged.Blocked(net.ParseIP("192.168.1.1")))
	assert.True(t, merged.Blocked(net.ParseIP("8.8.4.4")))
	require.Error(t, merged.BlockedHost("evil.example"))
	require.Error(t, merged.BlockedHost("a.corp.internal"))
}

func TestBlockedAddress_invalidAddress(t *testing.T) {
	err := DefaultPolicy().BlockedAddress("not-an-address")
	require.Error(t, err)
}

func TestBlockedHost_ipLiteral(t *testing.T) {
	policy := shippedDefaultDeny(t)
	require.Error(t, policy.BlockedHost("169.254.169.254"))
	require.NoError(t, policy.BlockedHost("8.8.8.8"))
}

func TestSecureTransport_nilBase(t *testing.T) {
	client := &http.Client{
		Transport: SecureTransport(nil, shippedDefaultDeny(t)),
	}
	_, err := client.Get("http://169.254.169.254/latest/meta-data/")
	require.Error(t, err)
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
	require.Error(t, cur.BlockedHost("blocked.example"))
	assert.True(t, cur.Blocked(net.ParseIP("10.0.0.1")))
}

func TestSecureTransport_blocksLinkLocalDial(t *testing.T) {
	client := &http.Client{
		Transport: SecureTransport(http.DefaultTransport.(*http.Transport).Clone(), shippedDefaultDeny(t)),
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
		Transport: SecureTransport(http.DefaultTransport.(*http.Transport).Clone(), shippedDefaultDeny(t)),
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
		Transport: SecureTransport(http.DefaultTransport.(*http.Transport).Clone(), shippedDefaultDeny(t)),
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

func TestMergeDeny_nilExactHostsInitialized(t *testing.T) {
	merged := Policy{}.MergeDeny(Policy{})
	require.NotNil(t, merged.ExactHosts)
}

func TestBlocked_nilAndInvalidIP(t *testing.T) {
	policy := shippedDefaultDeny(t)
	assert.False(t, policy.Blocked(nil))
	assert.False(t, policy.Blocked(net.IP{1, 2, 3}))
}

func TestBlockedHost_empty(t *testing.T) {
	require.NoError(t, DefaultPolicy().BlockedHost(""))
	require.NoError(t, DefaultPolicy().BlockedHost("   "))
}

func TestBlockedHost_stripsPort(t *testing.T) {
	fragment, err := ParseDenyList("", "blocked.example")
	require.NoError(t, err)
	require.Error(t, DefaultPolicy().MergeDeny(fragment).BlockedHost("blocked.example:8443"))
}

func TestBlockedAddress_hostnameAllowed(t *testing.T) {
	require.NoError(t, shippedDefaultDeny(t).BlockedAddress("example.com:443"))
}

func TestBlockedAddress_emptyHost(t *testing.T) {
	require.NoError(t, DefaultPolicy().BlockedAddress(":80"))
}

func TestClonePolicy_copiesWildcards(t *testing.T) {
	out := (Policy{
		ExactHosts:       map[string]struct{}{"a.example": {}},
		WildcardSuffixes: []string{"corp.internal"},
	}).clone()
	require.Equal(t, []string{"corp.internal"}, out.WildcardSuffixes)
	require.Error(t, out.BlockedHost("a.example"))
}

func TestSecureTransport_dropsCustomTLSDialers(t *testing.T) {
	base := &http.Transport{}
	base.DialTLSContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		return nil, fmt.Errorf("custom TLS dialer must not run")
	}
	base.DialTLS = func(network, addr string) (net.Conn, error) { //nolint:staticcheck // the hook under test
		return nil, fmt.Errorf("custom TLS dialer must not run")
	}
	transport := SecureTransport(base, shippedDefaultDeny(t))
	assert.Nil(t, transport.DialTLSContext)
	assert.Nil(t, transport.DialTLS) //nolint:staticcheck // the hook under test
	assert.NotNil(t, transport.DialContext)
}

// A NAT64 address under the well-known prefix carries an IPv4 address, which the gateway reaches
// for it: that address is checked as if it were dialled.
func TestBlockedChecksTheIPv4ANAT64AddressCarries(t *testing.T) {
	_, metadata, _ := net.ParseCIDR("169.254.0.0/16")
	p := Policy{BlockLoopback: true, DenyCIDRs: []*net.IPNet{metadata}}
	for addr, blocked := range map[string]bool{
		"64:ff9b::a9fe:a9fe": true,  // 169.254.169.254
		"64:ff9b::7f00:1":    true,  // 127.0.0.1
		"64:ff9b::808:808":   false, // 8.8.8.8
	} {
		require.Equal(t, blocked, p.Blocked(net.ParseIP(addr)), addr)
	}
	require.True(t, Policy{BlockPrivate: true}.Blocked(net.ParseIP("64:ff9b::a00:1")), "10.0.0.1, when private addresses are blocked")
	require.False(t, Policy{}.Blocked(net.ParseIP("64:ff9b::a00:1")))
}

// rfc6052Octets are the octets RFC 6052 section 2.2 puts an IPv4 address in, per prefix length.
var rfc6052Octets = map[int][4]int{
	32: {4, 5, 6, 7},
	40: {5, 6, 7, 9},
	48: {6, 7, 9, 10},
	56: {7, 9, 10, 11},
	64: {9, 10, 11, 12},
	96: {12, 13, 14, 15},
}

// embed places v4 in prefix as RFC 6052 section 2.2 lays it out, skipping bits 64 to 71.
func embed(t *testing.T, prefix string, v4 string) net.IP {
	t.Helper()
	_, n, err := net.ParseCIDR(prefix)
	require.NoError(t, err)
	ones, _ := n.Mask.Size()
	out := make(net.IP, net.IPv6len)
	copy(out, n.IP.To16())
	at := rfc6052Octets[ones]
	for i, b := range net.ParseIP(v4).To4() {
		out[at[i]] = b
	}
	return out
}

// withNAT64Prefixes adds prefixes for the rest of the test.
func withNAT64Prefixes(t *testing.T, prefixes ...string) {
	t.Helper()
	restore := NAT64Prefixes
	NAT64Prefixes = append([]*net.IPNet(nil), restore...)
	t.Cleanup(func() { NAT64Prefixes = restore })
	for _, prefix := range prefixes {
		require.NoError(t, AddNAT64Prefix(prefix))
	}
}

// A NAT64 address is refused when the IPv4 address it carries is, under the well-known prefix,
// the local-use prefix, and a network-specific prefix the operator configures.
func TestBlockedUnwrapsNAT64Prefixes(t *testing.T) {
	p := Policy{BlockLoopback: true, BlockPrivate: true}
	require.True(t, p.Blocked(embed(t, "64:ff9b::/96", "127.0.0.1")), "well-known prefix")
	require.True(t, p.Blocked(embed(t, "64:ff9b:1::/48", "127.0.0.1")), "local-use prefix")
	require.False(t, p.Blocked(embed(t, "64:ff9b:1::/48", "93.184.216.34")), "a public address through the local-use prefix")

	require.False(t, p.Blocked(embed(t, "2001:db8:122:344::/64", "10.0.0.1")), "an unconfigured network-specific prefix is plain IPv6")
	withNAT64Prefixes(t, "2001:db8:122:344::/64")
	require.True(t, p.Blocked(embed(t, "2001:db8:122:344::/64", "10.0.0.1")), "a configured network-specific prefix")
	require.Error(t, AddNAT64Prefix("2001:db8::/80"), "RFC 6052 allows /32, /40, /48, /56, /64 and /96 only")
}

// The IPv4 address carried by each example in RFC 6052 section 2.4 is 192.0.2.33.
func TestNAT64IPv4ReadsTheRFC6052Examples(t *testing.T) {
	for prefix, addr := range map[string]string{
		"2001:db8::/32":         "2001:db8:c000:221::",
		"2001:db8:100::/40":     "2001:db8:1c0:2:21::",
		"2001:db8:122::/48":     "2001:db8:122:c000:2:2100::",
		"2001:db8:122:300::/56": "2001:db8:122:3c0:0:221::",
		"2001:db8:122:344::/64": "2001:db8:122:344:c0:2:2100:0",
		"2001:db8:122:344::/96": "2001:db8:122:344::192.0.2.33",
	} {
		t.Run(prefix, func(t *testing.T) {
			withNAT64Prefixes(t, prefix)
			require.Equal(t, "192.0.2.33", nat64IPv4(net.ParseIP(addr)).String())
		})
	}
}

// Under overlapping prefixes an address is read by the longest one containing it.
func TestNAT64IPv4ReadsTheLongestPrefix(t *testing.T) {
	withNAT64Prefixes(t, "2001:db8::/32", "2001:db8:122:344::/96")
	addr := embed(t, "2001:db8:122:344::/96", "127.0.0.1")
	require.Equal(t, "127.0.0.1", nat64IPv4(addr).String())
	require.True(t, Policy{BlockLoopback: true}.Blocked(addr))
}

// A prefix overlapping the IPv4-mapped range is refused, and an IPv4 address is never unwrapped.
func TestNAT64PrefixesNeverCoverIPv4(t *testing.T) {
	require.Error(t, AddNAT64Prefix("::ffff:0:0/96"))
	require.Error(t, AddNAT64Prefix("::/32"))

	restore := NAT64Prefixes
	t.Cleanup(func() { NAT64Prefixes = restore })
	NAT64Prefixes = append([]*net.IPNet{{IP: net.ParseIP("::ffff:0:0"), Mask: net.CIDRMask(96, 128)}}, restore...)
	require.Nil(t, nat64IPv4(net.ParseIP("127.0.0.1")))
	require.True(t, Policy{BlockLoopback: true}.Blocked(net.ParseIP("127.0.0.1")))
}
