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

	monitorContext "github.com/kubevela/pkg/monitor/context"
	context2 "github.com/kubevela/workflow/pkg/context"
	"github.com/kubevela/workflow/pkg/cue/model/value"
	"github.com/stretchr/testify/assert"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	queryString = `sum(nginx_ingress_controller_requests{host="canary-demo.com",status="200"})`
)

func TestMetricCheck(t *testing.T) {
	srv := runMockPrometheusServer() // no lint

	v, err := value.NewValue(`
	  metricEndpoint: "http://127.0.0.1:18089"
      query: "sum(nginx_ingress_controller_requests{host=\"canary-demo.com\",status=\"200\"})"
      duration: "4s"
      failDuration: "2s"
      condition: ">=3"
      stepID: "123456"`, nil, "")
	assert.NoError(t, err)
	prd := &provider{}
	ctx := monitorContext.NewTraceContext(context.Background(), "")
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
	wfCtx, err := context2.NewContext(context.Background(), cli, "default", "v1", nil)
	assert.NoError(t, err)
	err = prd.PromCheck(ctx, wfCtx, v, nil)
	assert.NoError(t, err)
	res, err := v.GetBool("result")
	assert.NoError(t, err)
	assert.Equal(t, res, false)
	message, err := v.GetString("message")
	assert.NoError(t, err)
	assert.Equal(t, message, "The healthy condition should be >=3, and the query result is 10, indicating success.")
	if err := srv.Close(); err != nil {
		fmt.Printf("Server shutdown error: %v\n", err)
	}
}

func runMockPrometheusServer() *http.Server {
	srv := http.Server{Addr: ":18089", Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(fmt.Sprintf(`{
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
}`)))
	})}
	time.Sleep(3 * time.Second)
	go srv.ListenAndServe() // no lint
	return &srv
}
