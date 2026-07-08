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
	"bufio"
	"fmt"
	"net"
	"strings"

	corev1 "k8s.io/api/core/v1"
)

const (
	// ConfigMapKeyDenyCIDRs is the ConfigMap data key for CIDR/IP denylist lines.
	ConfigMapKeyDenyCIDRs = "denyCIDRs"
	// ConfigMapKeyDenyHosts is the ConfigMap data key for hostname denylist lines.
	ConfigMapKeyDenyHosts = "denyHosts"
)

// ParseDenyList parses denyCIDRs and denyHosts text blobs into a Policy fragment
// that only carries denylist fields. Blank lines and # comments are ignored.
func ParseDenyList(cidrsText, hostsText string) (Policy, error) {
	out := Policy{ExactHosts: map[string]struct{}{}}
	if err := parseCIDRLines(cidrsText, &out); err != nil {
		return Policy{}, err
	}
	if err := parseHostLines(hostsText, &out); err != nil {
		return Policy{}, err
	}
	return out, nil
}

// ParseConfigMap builds a denylist Policy fragment from a ConfigMap.
func ParseConfigMap(cm *corev1.ConfigMap) (Policy, error) {
	if cm == nil {
		return Policy{ExactHosts: map[string]struct{}{}}, nil
	}
	return ParseDenyList(cm.Data[ConfigMapKeyDenyCIDRs], cm.Data[ConfigMapKeyDenyHosts])
}

func parseCIDRLines(text string, out *Policy) error {
	return forEachEntry(text, func(line string) error {
		if ip := net.ParseIP(line); ip != nil {
			out.ExactIPs = append(out.ExactIPs, ip.To16())
			return nil
		}
		_, network, err := net.ParseCIDR(line)
		if err != nil {
			return fmt.Errorf("invalid deny CIDR %q: %w", line, err)
		}
		out.DenyCIDRs = append(out.DenyCIDRs, network)
		return nil
	})
}

func parseHostLines(text string, out *Policy) error {
	return forEachEntry(text, func(line string) error {
		line = strings.ToLower(line)
		if ip := net.ParseIP(line); ip != nil {
			out.ExactIPs = append(out.ExactIPs, ip.To16())
			return nil
		}
		if strings.HasPrefix(line, "*.") {
			suffix := strings.TrimPrefix(line, "*.")
			suffix = strings.TrimSuffix(suffix, ".")
			if suffix == "" || strings.Contains(suffix, "*") {
				return fmt.Errorf("invalid deny host wildcard %q", line)
			}
			out.WildcardSuffixes = append(out.WildcardSuffixes, suffix)
			return nil
		}
		if strings.Contains(line, "*") {
			return fmt.Errorf("invalid deny host %q: only *.suffix wildcards are supported", line)
		}
		if _, _, err := net.SplitHostPort(line); err == nil {
			return fmt.Errorf("invalid deny host %q: port qualifiers are not supported", line)
		}
		host := strings.TrimSuffix(line, ".")
		if host == "" {
			return fmt.Errorf("invalid empty deny host")
		}
		out.ExactHosts[host] = struct{}{}
		return nil
	})
}

func forEachEntry(text string, fn func(line string) error) error {
	scanner := bufio.NewScanner(strings.NewReader(text))
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if i := strings.Index(line, "#"); i >= 0 {
			line = strings.TrimSpace(line[:i])
			if line == "" {
				continue
			}
		}
		if err := fn(line); err != nil {
			return fmt.Errorf("line %d: %w", lineNo, err)
		}
	}
	return scanner.Err()
}
