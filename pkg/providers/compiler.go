/*
Copyright 2022 The KubeVela Authors.

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

package providers

import (
	"context"

	"github.com/kubevela/pkg/cue/cuex"
	cuexruntime "github.com/kubevela/pkg/cue/cuex/runtime"
	"github.com/kubevela/pkg/util/runtime"
	"github.com/kubevela/pkg/util/singleton"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/klog/v2"

	"github.com/kubevela/workflow/pkg/providers/email"
	"github.com/kubevela/workflow/pkg/providers/http"
	"github.com/kubevela/workflow/pkg/providers/kube"
	"github.com/kubevela/workflow/pkg/providers/legacy"
	"github.com/kubevela/workflow/pkg/providers/metrics"
	"github.com/kubevela/workflow/pkg/providers/time"
	"github.com/kubevela/workflow/pkg/providers/util"
)

const (
	// LegacyProviderName is the name of legacy provider
	LegacyProviderName = "op"
)

var (
	// EnableExternalPackageForDefaultCompiler .
	EnableExternalPackageForDefaultCompiler = true
	// EnableExternalPackageWatchForDefaultCompiler .
	EnableExternalPackageWatchForDefaultCompiler = false
)

var compiler = singleton.NewSingletonE[*cuex.Compiler](func() (*cuex.Compiler, error) {
	return cuex.NewCompilerWithInternalPackages(
		// legacy packages
		runtime.Must(cuexruntime.NewInternalPackage(LegacyProviderName, legacy.GetLegacyTemplate(), legacy.GetLegacyProviders())),

		// internal packages
		runtime.Must(cuexruntime.NewInternalPackage("email", email.GetTemplate(), email.GetProviders())),
		runtime.Must(cuexruntime.NewInternalPackage("http", http.GetTemplate(), http.GetProviders())),
		runtime.Must(cuexruntime.NewInternalPackage("kube", kube.GetTemplate(), kube.GetProviders())),
		runtime.Must(cuexruntime.NewInternalPackage("metrics", metrics.GetTemplate(), metrics.GetProviders())),
		runtime.Must(cuexruntime.NewInternalPackage("time", time.GetTemplate(), time.GetProviders())),
		runtime.Must(cuexruntime.NewInternalPackage("util", util.GetTemplate(), util.GetProviders())),
	), nil
})

// DefaultCompiler compiler for cuex to compile
var DefaultCompiler = singleton.NewSingleton[*cuex.Compiler](func() *cuex.Compiler {
	c := compiler.Get()
	if EnableExternalPackageForDefaultCompiler {
		if err := c.LoadExternalPackages(context.Background()); err != nil && !kerrors.IsNotFound(err) {
			klog.Errorf("failed to load external packages for cuex default compiler: %s", err.Error())
		}
	}
	if EnableExternalPackageWatchForDefaultCompiler {
		go c.ListenExternalPackages(nil)
	}
	return c
})
