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
	_ "embed"
	"fmt"
	"net"
	"sync"
)

// Keep these embeds in sync with charts/vela-workflow values
// workflow.httpDeny.defaultConfig.
//
//go:embed defaults/deny_hosts.txt
var denyHostsDefaults string

//go:embed defaults/deny_cidrs.txt
var denyCIDRsDefaults string

var (
	builtinDenyOnce sync.Once
	builtinDeny     Policy
	builtinDenyErr  error
)

// BuiltinDeny returns a copy of the immutable denylist floor shipped with the
// binary. Cluster ConfigMaps can only add denies; they cannot remove these
// entries. Callers must not rely on mutating the returned Policy.
func BuiltinDeny() Policy {
	builtinDenyOnce.Do(func() {
		policy, err := ParseDenyList(denyCIDRsDefaults, denyHostsDefaults)
		if err != nil {
			builtinDenyErr = fmt.Errorf("parse embedded workflow HTTP deny defaults: %w", err)
			builtinDeny = Policy{ExactHosts: map[string]struct{}{}}
			return
		}
		builtinDeny = policy
	})
	return clonePolicy(builtinDeny)
}

func clonePolicy(p Policy) Policy {
	out := Policy{
		BlockPrivate:  p.BlockPrivate,
		BlockLoopback: p.BlockLoopback,
		ExactHosts:    map[string]struct{}{},
	}
	for host := range p.ExactHosts {
		out.ExactHosts[host] = struct{}{}
	}
	if len(p.ExactIPs) > 0 {
		out.ExactIPs = append([]net.IP(nil), p.ExactIPs...)
	}
	if len(p.DenyCIDRs) > 0 {
		out.DenyCIDRs = append([]*net.IPNet(nil), p.DenyCIDRs...)
	}
	if len(p.WildcardSuffixes) > 0 {
		out.WildcardSuffixes = append([]string(nil), p.WildcardSuffixes...)
	}
	return out
}

func builtinDenyLoadError() error {
	_ = BuiltinDeny()
	return builtinDenyErr
}
