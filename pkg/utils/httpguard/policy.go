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
	"fmt"
	"net"
	"net/netip"
	"strings"
)

// Policy controls which destination hosts and IPs outbound HTTP clients may
// connect to. Host checks run on the URL before dial; IP checks run at dial
// time on the resolved address.
type Policy struct {
	// BlockPrivate denies RFC-1918 and RFC-4193 ULA ranges. Off by default
	// because workflow HTTP steps legitimately call in-cluster ClusterIP services.
	BlockPrivate bool
	// BlockLoopback denies 127.0.0.0/8 and ::1. Off by default.
	BlockLoopback bool
	// DenyCIDRs blocks any destination IP contained in these networks.
	DenyCIDRs []*net.IPNet
	ExactIPs  []net.IP
	// ExactHosts are lower-case hostnames that must be rejected.
	ExactHosts map[string]struct{}
	// WildcardSuffixes are lower-case DNS suffixes without the leading "*."
	// (for example "corp.internal" from "*.corp.internal").
	WildcardSuffixes []string
}

// clone returns a deep copy, so a stored policy cannot change when the caller
// later mutates its maps, slices or IP values.
func (p Policy) clone() Policy {
	out := p
	out.DenyCIDRs = make([]*net.IPNet, 0, len(p.DenyCIDRs))
	for _, n := range p.DenyCIDRs {
		if n == nil {
			continue
		}
		out.DenyCIDRs = append(out.DenyCIDRs, &net.IPNet{
			IP:   append(net.IP(nil), n.IP...),
			Mask: append(net.IPMask(nil), n.Mask...),
		})
	}
	out.ExactIPs = make([]net.IP, 0, len(p.ExactIPs))
	for _, ip := range p.ExactIPs {
		out.ExactIPs = append(out.ExactIPs, append(net.IP(nil), ip...))
	}
	out.ExactHosts = make(map[string]struct{}, len(p.ExactHosts))
	for host := range p.ExactHosts {
		out.ExactHosts[host] = struct{}{}
	}
	out.WildcardSuffixes = append([]string(nil), p.WildcardSuffixes...)
	return out
}

// DefaultPolicy returns the base outbound HTTP policy. Destination deny rules
// are supplied by discovered workflow HTTP deny ConfigMaps.
func DefaultPolicy() Policy {
	return Policy{
		ExactHosts: map[string]struct{}{},
	}
}

// MergeDeny overlays denylist CIDRs/hosts from other onto p.
func (p Policy) MergeDeny(other Policy) Policy {
	if len(other.DenyCIDRs) > 0 {
		p.DenyCIDRs = append(append([]*net.IPNet{}, p.DenyCIDRs...), other.DenyCIDRs...)
	}
	if len(other.ExactIPs) > 0 {
		p.ExactIPs = append(append([]net.IP{}, p.ExactIPs...), other.ExactIPs...)
	}
	if len(other.ExactHosts) > 0 {
		copied := make(map[string]struct{}, len(p.ExactHosts)+len(other.ExactHosts))
		for host := range p.ExactHosts {
			copied[host] = struct{}{}
		}
		for host := range other.ExactHosts {
			copied[host] = struct{}{}
		}
		p.ExactHosts = copied
	} else if p.ExactHosts == nil {
		p.ExactHosts = map[string]struct{}{}
	}
	if len(other.WildcardSuffixes) > 0 {
		p.WildcardSuffixes = append(append([]string{}, p.WildcardSuffixes...), other.WildcardSuffixes...)
	}
	return p
}

// nat64Octets are the octets of an IPv6 address that carry the IPv4 address under a NAT64 prefix
// of each length RFC 6052 allows (section 2.2); octet 8 is never one of them.
var nat64Octets = map[int][4]int{
	32: {4, 5, 6, 7},
	40: {5, 6, 7, 9},
	48: {6, 7, 9, 10},
	56: {7, 9, 10, 11},
	64: {9, 10, 11, 12},
	96: {12, 13, 14, 15},
}

// NAT64Prefixes are the NAT64 prefixes whose addresses are judged by the IPv4 address they
// carry: the well-known prefix (RFC 6052) and the local-use prefix (RFC 8215) by default, and any
// network-specific prefix added with AddNAT64Prefix.
var NAT64Prefixes = []*net.IPNet{
	{IP: net.ParseIP("64:ff9b::"), Mask: net.CIDRMask(96, 128)},
	{IP: net.ParseIP("64:ff9b:1::"), Mask: net.CIDRMask(48, 128)},
}

// AddNAT64Prefix adds a network-specific NAT64 prefix, of a length RFC 6052 allows.
func AddNAT64Prefix(cidr string) error {
	_, n, err := net.ParseCIDR(cidr)
	if err != nil {
		return fmt.Errorf("NAT64 prefix %q: %w", cidr, err)
	}
	ones, bits := n.Mask.Size()
	if _, ok := nat64Octets[ones]; !ok || bits != 128 {
		return fmt.Errorf("NAT64 prefix %q must be an IPv6 prefix of length 32, 40, 48, 56, 64 or 96", cidr)
	}
	prefix, err := netip.ParsePrefix(cidr)
	if err != nil {
		return fmt.Errorf("NAT64 prefix %q: %w", cidr, err)
	}
	if prefix.Overlaps(ipv4Mapped) {
		return fmt.Errorf("NAT64 prefix %q overlaps the IPv4-mapped range %s", cidr, ipv4Mapped)
	}
	NAT64Prefixes = append(NAT64Prefixes, n)
	return nil
}

// ipv4Mapped is the range net.IP uses for IPv4 addresses, so no NAT64 prefix may overlap it.
var ipv4Mapped = netip.MustParsePrefix("::ffff:0:0/96")

// nat64IPv4 is the IPv4 address ip carries under the longest NAT64 prefix containing it, or nil
// for an IPv4 address.
func nat64IPv4(ip net.IP) net.IP {
	if ip.To4() != nil {
		return nil
	}
	longest := -1
	for _, prefix := range NAT64Prefixes {
		if ones, _ := prefix.Mask.Size(); ones > longest && prefix.Contains(ip) {
			longest = ones
		}
	}
	if longest < 0 {
		return nil
	}
	at := nat64Octets[longest]
	return net.IPv4(ip[at[0]], ip[at[1]], ip[at[2]], ip[at[3]])
}

// Blocked reports whether ip must be rejected under policy. A NAT64 address is also rejected when
// the IPv4 address it carries is, because the gateway connects to that address.
func (p Policy) Blocked(ip net.IP) bool {
	if ip == nil {
		return false
	}
	ip = ip.To16()
	if ip == nil {
		return false
	}
	if v4 := nat64IPv4(ip); v4 != nil && p.Blocked(v4) {
		return true
	}
	if p.BlockLoopback && ip.IsLoopback() {
		return true
	}
	if p.BlockPrivate && ip.IsPrivate() {
		return true
	}
	for _, exact := range p.ExactIPs {
		if ip.Equal(exact) {
			return true
		}
	}
	for _, cidr := range p.DenyCIDRs {
		if cidr != nil && cidr.Contains(ip) {
			return true
		}
	}
	return false
}

// BlockedHost reports whether hostname must be rejected under the denylist.
// Host may include a port; IP literals are checked against ExactIPs/DenyCIDRs.
func (p Policy) BlockedHost(host string) error {
	host = normalizeHost(host)
	if host == "" {
		return nil
	}
	if ip := parseIPLiteral(host); ip != nil {
		if p.Blocked(ip) {
			return fmt.Errorf("blocked SSRF target: %s", ip)
		}
		return nil
	}
	if _, ok := p.ExactHosts[host]; ok {
		return fmt.Errorf("blocked SSRF host: %s", host)
	}
	for _, suffix := range p.WildcardSuffixes {
		if host == suffix {
			continue
		}
		if strings.HasSuffix(host, "."+suffix) {
			return fmt.Errorf("blocked SSRF host: %s", host)
		}
	}
	return nil
}

// BlockedAddress parses host:port from a dial address and reports whether it
// is blocked. Non-IP hosts are allowed through; host denylist must be checked
// separately via BlockedHost before dial.
func (p Policy) BlockedAddress(address string) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return err
	}
	ip := parseIPLiteral(host)
	if ip == nil {
		return nil
	}
	if p.Blocked(ip) {
		return fmt.Errorf("blocked SSRF target: %s", ip)
	}
	return nil
}

func normalizeHost(host string) string {
	host = strings.TrimSpace(host)
	if host == "" {
		return ""
	}
	// Strip brackets from IPv6 literals like [::1]:443 after SplitHostPort, or
	// raw hostname:port when callers pass URL.Host.
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	host = strings.TrimPrefix(host, "[")
	host = strings.TrimSuffix(host, "]")
	return strings.TrimSuffix(strings.ToLower(host), ".")
}

func parseIPLiteral(host string) net.IP {
	if host == "" {
		return nil
	}
	if i := strings.Index(host, "%"); i >= 0 {
		host = host[:i]
	}
	return net.ParseIP(host)
}
