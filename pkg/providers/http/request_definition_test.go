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
	"fmt"
	"os"
	"strings"
	"testing"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
	"github.com/stretchr/testify/require"
)

// requestStepTemplate reads the request step's CUE from the chart and swaps
// its vela imports for stubs, so it can be evaluated without the engine.
func requestStepTemplate(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile("../../../charts/vela-workflow/definitions/request.yaml")
	require.NoError(t, err)
	_, tmpl, found := strings.Cut(string(raw), "template: |\n")
	require.True(t, found)
	tmpl = strings.NewReplacer(`"vela/op"`, "", `"vela/http"`, "").Replace(tmpl)
	return tmpl + `
op: {
	#ConditionalWait: {continue: bool, message?: string}
	#Steps: {...}
	#Fail: {message: string}
}
http: #HTTPDo: {$params: {...}, $returns?: {statusCode: int, body: string}}
`
}

// A response without a body, as every HEAD response is, must not fail the step.
func TestRequestStepResponseBody(t *testing.T) {
	tmpl := requestStepTemplate(t)
	for name, tc := range map[string]struct {
		method, body string
		status       int
		wantResponse bool
	}{
		"HEAD has no body":   {method: "HEAD", status: 200},
		"204 has no body":    {method: "DELETE", status: 204},
		"empty 200":          {method: "OPTIONS", status: 200},
		"JSON is still read": {method: "GET", status: 200, body: `{"ok":true}`, wantResponse: true},
	} {
		t.Run(name, func(t *testing.T) {
			src := tmpl + fmt.Sprintf("\nparameter: {url: \"http://x\", method: %q}\nreq: $returns: {statusCode: %d, body: %q}\n", tc.method, tc.status, tc.body)
			v := cuecontext.New().CompileString(src)
			require.NoError(t, v.Validate(cue.Concrete(false)))
			response := v.LookupPath(cue.ParsePath("response"))
			require.Equal(t, tc.wantResponse, response.Exists())
			if tc.wantResponse {
				ok, err := response.LookupPath(cue.ParsePath("ok")).Bool()
				require.NoError(t, err)
				require.True(t, ok)
			}
		})
	}
}

// Before the request returns, the step must keep waiting rather than error.
func TestRequestStepBeforeResponse(t *testing.T) {
	v := cuecontext.New().CompileString(requestStepTemplate(t) + "\nparameter: {url: \"http://x\", method: \"HEAD\"}\n")
	require.NoError(t, v.Err())
	require.False(t, v.LookupPath(cue.ParsePath("response")).Exists())
}
