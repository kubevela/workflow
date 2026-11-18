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
)

// Each method's definition, decoded from its CUE and passed to Do,
// sends that method.
func TestMethodDefinitionsSendTheirMethod(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Method", r.Method)
	}))
	defer srv.Close()

	for def, method := range map[string]string{
		"#HTTPGet":     http.MethodGet,
		"#HTTPPost":    http.MethodPost,
		"#HTTPPut":     http.MethodPut,
		"#HTTPDelete":  http.MethodDelete,
		"#HTTPPatch":   http.MethodPatch,
		"#HTTPHead":    http.MethodHead,
		"#HTTPOptions": http.MethodOptions,
	} {
		t.Run(def, func(t *testing.T) {
			v := cuecontext.New().CompileString(GetTemplate() + fmt.Sprintf("\ncall: %s & {url: %q, request: body: \"hi\"}", def, srv.URL))
			require.NoError(t, v.Err())
			var req RequestVars
			require.NoError(t, v.LookupPath(cue.ParsePath("call")).Decode(&req))
			res, err := Do(context.Background(), &DoParams{Params: req})
			require.NoError(t, err)
			require.Equal(t, method, res.Header.Get("X-Method"))
		})
	}
}
