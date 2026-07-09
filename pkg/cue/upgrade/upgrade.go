/*
Copyright 2026 The KubeVela Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Package upgrade provides CUE version-compatibility helpers for KubeVela Workflow.
// The core engine lives in github.com/kubevela/pkg/cue/upgrade; this package
// wires workflow-specific concerns (version provider, Prometheus metrics) and
// re-exports the public API so call sites need no import change.
//
// # Dual-mode support
//
// The workflow engine supports two operational modes:
//
//  1. Standalone workflow controller: the workflow binary sets GetCurrentVersion
//     in its own init() and owns the version provider.
//
//  2. KubeVela consuming workflow: kubevela's init() runs after this package's
//     init() and overwrites pkgupgrade.GetCurrentVersion with velaversion.VelaVersion,
//     so the kubevela version governs all upgrade decisions. The workflow-specific
//     metrics callbacks remain active in both modes.
package upgrade

import (
	"context"
	"sync"
	"time"

	pkgupgrade "github.com/kubevela/pkg/cue/upgrade"
	workflowversion "github.com/kubevela/workflow/version"
)

// Version is a release version for upgrade ordering.
type Version = pkgupgrade.Version

// DefinitionKind identifies the type of definition for metrics and compatibility reports.
type DefinitionKind = pkgupgrade.DefinitionKind

// TemplateArea identifies which part of a definition's CUE template a rewrite was applied to.
type TemplateArea = pkgupgrade.TemplateArea

// KubeVelaUpgradeFunc is a CUE compatibility fix triggered by a KubeVela version.
type KubeVelaUpgradeFunc = pkgupgrade.KubeVelaUpgradeFunc

// CUEUpgradeFunc is a CUE compatibility fix triggered by the CUE language version.
type CUEUpgradeFunc = pkgupgrade.CUEUpgradeFunc

// UpgradeFunc is a backward-compatible alias for KubeVelaUpgradeFunc.
type UpgradeFunc = pkgupgrade.UpgradeFunc //nolint:revive

// Workflow-specific DefinitionKind constants.
const (
	WorkflowStepKind DefinitionKind = "WorkflowStep"
)

// Workflow-specific TemplateArea constants.
const (
	TemplateAreaMain TemplateArea = "template"
)

// EnableCUEVersionCompatibility is an alias for pkgupgrade.EnableCUEVersionCompatibility.
var EnableCUEVersionCompatibility = &pkgupgrade.EnableCUEVersionCompatibility

// CompatibilityCacheSize mirrors pkgupgrade.CompatibilityCacheSize.
var CompatibilityCacheSize = pkgupgrade.CompatibilityCacheSize

var (
	// EnableListConcatUpgrade controls the list-arithmetic compatibility rewrite pass.
	EnableListConcatUpgrade = true
	// EnableErrorFieldLabelUpgrade controls quoting of legacy unquoted error labels.
	EnableErrorFieldLabelUpgrade = true
	// EnableBoolDefaultGuardUpgrade controls the bool default-guard hazard rewrite pass.
	EnableBoolDefaultGuardUpgrade = false
	// EnableGenericDefaultGuardUpgrade controls generic (non-bool) default-guard hazard rewrites.
	EnableGenericDefaultGuardUpgrade = false
	// EnableKeepValidatorsSingletonUpgrade controls singleton keepvalidators concretization rewrites.
	EnableKeepValidatorsSingletonUpgrade = false
	// EnableEvalv3SelfRefGuardUpgrade controls evalv3 self-reference default-guard rewrites.
	EnableEvalv3SelfRefGuardUpgrade = false
)

var syncLocalFlagsMu sync.Mutex

// Re-export functions.
var (
	ParseVersion         = pkgupgrade.ParseVersion
	RegisterUpgrade      = pkgupgrade.RegisterUpgrade
	GetSupportedVersions = pkgupgrade.GetSupportedVersions
)

// SetCacheEntryTTL sets how long an unaccessed cache entry lives before eviction.
// Must be called before InitCompatibilityCache to take effect.
func SetCacheEntryTTL(d time.Duration) {
	pkgupgrade.CacheEntryTTL = d
}

// Upgrade applies all registered upgrades to cueStr.
func Upgrade(cueStr string, targetVersion ...Version) (string, error) {
	syncLocalFlagsMu.Lock()
	defer syncLocalFlagsMu.Unlock()
	syncLocalFlagsLocked()
	return pkgupgrade.Upgrade(cueStr, targetVersion...)
}

// RequiresUpgrade checks whether cueStr needs upgrading.
func RequiresUpgrade(cueStr string, targetVersion ...Version) (bool, []string, error) {
	syncLocalFlagsMu.Lock()
	defer syncLocalFlagsMu.Unlock()
	syncLocalFlagsLocked()
	return pkgupgrade.RequiresUpgrade(cueStr, targetVersion...)
}

// EnsureCueVersionCompatibility applies all upgrades for the running workflow version.
func EnsureCueVersionCompatibility(cueStr, defName string, defKind DefinitionKind, area TemplateArea) (string, bool) {
	syncLocalFlagsMu.Lock()
	defer syncLocalFlagsMu.Unlock()
	syncLocalFlagsLocked()
	return pkgupgrade.EnsureCueVersionCompatibility(cueStr, defName, defKind, area)
}

// InitCompatibilityCache reinitialises the LRU cache.
func InitCompatibilityCache(ctx context.Context, size int) {
	pkgupgrade.InitCompatibilityCache(ctx, size)
}

func init() {
	syncLocalFlags()
	// Wire the workflow version provider (standalone mode).
	// When KubeVela consumes workflow, kubevela's init() will overwrite this
	// with velaversion.VelaVersion, which is the correct behaviour — upgrade
	// decisions should be governed by the KubeVela release version.
	pkgupgrade.GetCurrentVersion = func() string {
		return workflowversion.VelaVersion
	}

	// Wire Prometheus metrics callbacks into the engine hooks.
	pkgupgrade.OnRewrite = func(fixID, fixVersion string, defKind pkgupgrade.DefinitionKind, area pkgupgrade.TemplateArea) {
		CUECompatRewriteTotal.WithLabelValues(fixID, fixVersion, string(defKind), string(area)).Inc()
	}
	pkgupgrade.OnUpgradeDuration = func(defKind pkgupgrade.DefinitionKind, elapsed time.Duration) {
		CUECompatUpgradeDuration.WithLabelValues(string(defKind)).Observe(elapsed.Seconds())
	}
	pkgupgrade.OnCacheEviction = func(reason string) {
		CUECompatCacheEvictionsTotal.WithLabelValues(reason).Inc()
	}
}

func syncLocalFlags() {
	syncLocalFlagsMu.Lock()
	defer syncLocalFlagsMu.Unlock()
	syncLocalFlagsLocked()
}

func syncLocalFlagsLocked() {
	pkgupgrade.EnableListArithmeticUpgrade = EnableListConcatUpgrade
	pkgupgrade.EnableErrorFieldLabelUpgrade = EnableErrorFieldLabelUpgrade
	pkgupgrade.EnableBoolDefaultNegationUpgrade = EnableBoolDefaultGuardUpgrade
	pkgupgrade.EnableGenericDefaultGuardUpgrade = EnableGenericDefaultGuardUpgrade
	pkgupgrade.EnableKeepValidatorsSingletonUpgrade = EnableKeepValidatorsSingletonUpgrade
	pkgupgrade.EnableEvalv3SelfRefGuardUpgrade = EnableEvalv3SelfRefGuardUpgrade
}
