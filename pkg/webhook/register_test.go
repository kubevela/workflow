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

package webhook

import (
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	"github.com/kubevela/workflow/controllers"
)

func TestRegister(t *testing.T) {
	r := require.New(t)
	// The manager never talks to this address: building it and registering
	// webhooks does not reach the API server.
	mgr, err := manager.New(&rest.Config{Host: "https://127.0.0.1:1"}, manager.Options{
		Scheme:  runtime.NewScheme(),
		Metrics: metricsserver.Options{BindAddress: "0"},
	})
	r.NoError(err)

	Register(mgr, controllers.Args{})

	mux := mgr.GetWebhookServer().WebhookMux()
	for _, path := range []string{
		"/validating-core-oam-dev-v1alpha1-workflowruns",
		"/mutating-core-oam-dev-v1alpha1-workflowruns",
		"/convert",
	} {
		_, pattern := mux.Handler(httptest.NewRequest("POST", path, nil))
		r.Equal(path, pattern, "webhook %s is not registered", path)
	}
}
