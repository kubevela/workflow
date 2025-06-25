package legacy

import (
	"strings"

	cuexruntime "github.com/kubevela/pkg/cue/cuex/runtime"

	"github.com/kubevela/workflow/pkg/providers/legacy/email"
	"github.com/kubevela/workflow/pkg/providers/legacy/http"
	"github.com/kubevela/workflow/pkg/providers/legacy/kube"
	"github.com/kubevela/workflow/pkg/providers/legacy/metrics"
	"github.com/kubevela/workflow/pkg/providers/legacy/time"
	"github.com/kubevela/workflow/pkg/providers/legacy/util"
	"github.com/kubevela/workflow/pkg/providers/legacy/workspace"
)

func registerProviders(providers map[string]cuexruntime.ProviderFn, providerFuncs map[string]cuexruntime.ProviderFn) map[string]cuexruntime.ProviderFn {
	for k, v := range providerFuncs {
		providers[k] = v
	}
	return providers
}

// GetLegacyProviders get legacy providers
func GetLegacyProviders() map[string]cuexruntime.ProviderFn {
	providers := make(map[string]cuexruntime.ProviderFn, 0)
	registerProviders(providers, email.GetProviders())
	registerProviders(providers, http.GetProviders())
	registerProviders(providers, kube.GetProviders())
	registerProviders(providers, metrics.GetProviders())
	registerProviders(providers, time.GetProviders())
	registerProviders(providers, util.GetProviders())
	registerProviders(providers, workspace.GetProviders())
	return providers
}

// GetLegacyTemplate get legacy template
func GetLegacyTemplate() string {
	return strings.Join([]string{
		email.GetTemplate(),
		http.GetTemplate(),
		kube.GetTemplate(),
		metrics.GetTemplate(),
		time.GetTemplate(),
		util.GetTemplate(),
		workspace.GetTemplate(),
	},
		"\n")
}
