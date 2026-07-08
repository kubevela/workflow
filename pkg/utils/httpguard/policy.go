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
	"strings"
)

// Policy controls which destination hosts and IPs outbound HTTP clients may
// connect to. Host checks run on the URL before dial; IP checks run at dial
// time on the resolved address.
type Policy struct {
	// BlockLinkLocal denies link-local unicast (169.254.0.0/16, fe80::/10).
	BlockLinkLocal bool
	// BlockMetadata denies curated cloud metadata endpoints that fall outside
	// link-local (for example AWS IPv6 IMDS and Alibaba metadata).
	BlockMetadata bool
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

// DefaultPolicy is the secure-by-default posture for controller outbound HTTP:
// block link-local and known cloud metadata, allow private and loopback.
func DefaultPolicy() Policy {
	return Policy{
		BlockLinkLocal: true,
		BlockMetadata:  true,
		ExactHosts:     map[string]struct{}{},
	}
}

var metadataIPs = func() []net.IP {
	raw := []string{
		"fd00:ec2::254",   // AWS IPv6 IMDS
		"100.100.100.200", // Alibaba Cloud metadata
	}
	out := make([]net.IP, 0, len(raw))
	for _, s := range raw {
		if ip := net.ParseIP(s); ip != nil {
			out = append(out, ip)
		}
	}
	return out
}()

// MergeDeny overlays denylist CIDRs/hosts from other onto p.
func (p Policy) MergeDeny(other Policy) Policy {
	if len(other.DenyCIDRs) > 0 {
		p.DenyCIDRs = append(append([]*net.IPNet{}, p.DenyCIDRs...), other.DenyCIDRs...)
	}
	if len(other.ExactIPs) > 0 {
		p.ExactIPs = append(append([]net.IP{}, p.ExactIPs...), other.ExactIPs...)
	}
	if p.ExactHosts == nil {
		p.ExactHosts = map[string]struct{}{}
	}
	for host := range other.ExactHosts {
		p.ExactHosts[host] = struct{}{}
	}
	if len(other.WildcardSuffixes) > 0 {
		p.WildcardSuffixes = append(append([]string{}, p.WildcardSuffixes...), other.WildcardSuffixes...)
	}
	return p
}

// Blocked reports whether ip must be rejected under policy.
func (p Policy) Blocked(ip net.IP) bool {
	if ip == nil {
		return false
	}
	ip = ip.To16()
	if ip == nil {
		return false
	}
	if p.BlockLoopback && ip.IsLoopback() {
		return true
	}
	if p.BlockPrivate && ip.IsPrivate() {
		return true
	}
	if p.BlockLinkLocal && ip.IsLinkLocalUnicast() {
		return true
	}
	if p.BlockMetadata {
		for _, metadata := range metadataIPs {
			if ip.Equal(metadata) {
				return true
			}
		}
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
	if ip := net.ParseIP(host); ip != nil {
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
	ip := net.ParseIP(host)
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
	return strings.ToLower(host)
}
