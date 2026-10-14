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

package http

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	providertypes "github.com/kubevela/workflow/pkg/providers/types"
)

// Each method's definition, called through its CUE as a template calls it,
// sends that method.
func TestMethodDefinitionsSendTheirMethod(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(r.Method))
	}))
	defer srv.Close()
	// a client in the context keeps the provider off the kubeconfig singleton
	ctx := context.WithValue(context.Background(), providertypes.KubeClientKey, fake.NewClientBuilder().Build())

	for def, method := range map[string]string{
		"#HTTPGet":    http.MethodGet,
		"#HTTPPost":   http.MethodPost,
		"#HTTPPut":    http.MethodPut,
		"#HTTPDelete": http.MethodDelete,
	} {
		t.Run(def, func(t *testing.T) {
			v := cuecontext.New().CompileString(GetTemplate() + fmt.Sprintf("\ncall: %s & {$params: {url: %q, request: body: \"hi\"}}", def, srv.URL))
			require.NoError(t, v.Err())
			out, err := GetProviders()["do"].Call(ctx, v.LookupPath(cue.ParsePath("call")))
			require.NoError(t, err)
			body, err := out.LookupPath(cue.ParsePath("$returns.body")).String()
			require.NoError(t, err)
			require.Equal(t, method, body)
		})
	}
}
