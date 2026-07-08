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
)

// Policy controls which destination IPs outbound HTTP clients may connect to.
// Validation runs at dial time on the resolved address, not on the URL string.
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
}

// DefaultPolicy is the secure-by-default posture for controller outbound HTTP:
// block link-local and known cloud metadata, allow private and loopback.
func DefaultPolicy() Policy {
	return Policy{
		BlockLinkLocal: true,
		BlockMetadata:  true,
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
	return false
}

// BlockedAddress parses host:port from a dial address and reports whether it
// is blocked. Non-IP hosts are allowed through; resolution happens before dial.
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
