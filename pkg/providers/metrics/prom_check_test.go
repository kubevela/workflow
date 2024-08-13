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
package metrics

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/crossplane/crossplane-runtime/pkg/test"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kubevela/pkg/util/singleton"

	context2 "github.com/kubevela/workflow/pkg/context"
	"github.com/kubevela/workflow/pkg/cue/model"
	"github.com/kubevela/workflow/pkg/cue/process"
	providertypes "github.com/kubevela/workflow/pkg/providers/types"
)

func TestMetricCheck(t *testing.T) {
	srv := runMockPrometheusServer() // no lint
	r := require.New(t)
	ctx := context.Background()
	cli := &test.MockClient{
		MockCreate: func(ctx context.Context, obj client.Object, opts ...client.CreateOption) error {
			return nil
		},
		MockPatch: func(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
			return nil
		},
		MockGet: func(ctx context.Context, key client.ObjectKey, obj client.Object) error {
			return nil
		},
	}
	singleton.KubeClient.Set(cli)
	wfCtx, err := context2.NewContext(context.Background(), "default", "v1", nil)
	r.NoError(err)
	pCtx := process.NewContext(process.ContextData{})
	pCtx.PushData(model.ContextStepSessionID, "test-id")
	res, err := PromCheck(ctx, &PromParams{
		Params: PromVars{
			MetricEndpoint: "http://127.0.0.1:18089",
			Query:          "sum(nginx_ingress_controller_requests{host=\"canary-demo.com\",status=\"200\"})",
			Duration:       "4s",
			FailDuration:   "2s",
			Condition:      ">=3",
		},
		RuntimeParams: providertypes.RuntimeParams{
			WorkflowContext: wfCtx,
			ProcessContext:  pCtx,
		},
	})
	r.NoError(err)
	r.Equal(res.Returns.Result, false)
	r.Equal(res.Returns.Message, "The healthy condition should be >=3, and the query result is 10, indicating success.")
	if err := srv.Close(); err != nil {
		fmt.Printf("Server shutdown error: %v\n", err)
	}
}

func runMockPrometheusServer() *http.Server {
	srv := http.Server{Addr: ":18089", Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
    "status": "success",
    "data": {
        "resultType": "vector",
        "result": [
            {
                "metric": {},
                "value": [
                    1678701380.73,
                    "10"
                ]
            }
        ]
    }
}`))
	})}
	go srv.ListenAndServe() // no lint
	time.Sleep(3 * time.Second)
	return &srv
}
