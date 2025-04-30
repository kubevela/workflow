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
	_ "embed"
	"fmt"
	"strconv"
	"time"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
	"github.com/prometheus/client_golang/api"
	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	prommodel "github.com/prometheus/common/model"

	cuexruntime "github.com/kubevela/pkg/cue/cuex/runtime"

	wfContext "github.com/kubevela/workflow/pkg/context"
	"github.com/kubevela/workflow/pkg/cue/model"
	providertypes "github.com/kubevela/workflow/pkg/providers/types"
)

const (
	// ProviderName is provider name for install.
	ProviderName = "metrics"
)

// PromVars .
type PromVars struct {
	Query          string `json:"query"`
	MetricEndpoint string `json:"metricEndpoint"`
	Condition      string `json:"condition"`
	Duration       string `json:"duration"`
	FailDuration   string `json:"failDuration"`
}

// PromReturns .
type PromReturns struct {
	Message string `json:"message,omitempty"`
	Failed  bool   `json:"failed"`
	Result  bool   `json:"result"`
}

// PromParams .
type PromParams = providertypes.LegacyParams[PromVars]

// PromCheck do health check from metrics from prometheus
func PromCheck(ctx context.Context, params *PromParams) (*PromReturns, error) {
	pCtx := params.ProcessContext
	wfCtx := params.WorkflowContext
	stepID := fmt.Sprint(pCtx.GetData(model.ContextStepSessionID))

	valueStr, err := getQueryResult(ctx, params.Params)
	if err != nil {
		return nil, err
	}

	res, err := compareValueWithCondition(ctx, valueStr, params.Params)
	if err != nil {
		return nil, err
	}

	if res {
		// meet the condition
		return handleSuccessCompare(wfCtx, stepID, valueStr, params.Params)
	}
	return handleFailCompare(wfCtx, stepID, valueStr, params.Params)
}

func handleSuccessCompare(wfCtx wfContext.Context, stepID, valueStr string, vars PromVars) (*PromReturns, error) {
	// clean up fail timeStamp
	setMetricsStatusTime(wfCtx, stepID, "fail", 0)

	st := getMetricsStatusTime(wfCtx, stepID, "success")
	if st == 0 {
		// first success
		setMetricsStatusTime(wfCtx, stepID, "success", time.Now().Unix())
		return &PromReturns{
			Result:  false,
			Failed:  false,
			Message: fmt.Sprintf("The healthy condition should be %s, and the query result is %s, indicating success.", vars.Condition, valueStr),
		}, nil
	}
	successTime := time.Unix(st, 0)
	duration, err := time.ParseDuration(vars.Duration)
	if err != nil {
		return nil, fmt.Errorf("failed to parse duration %s: %w", vars.Duration, err)
	}
	if successTime.Add(duration).Before(time.Now()) {
		return &PromReturns{
			Result:  true,
			Failed:  false,
			Message: "The metric check has passed successfully.",
		}, nil
	}
	return &PromReturns{
		Result:  false,
		Failed:  false,
		Message: fmt.Sprintf("The healthy condition should be %s, and the query result is %s, indicating success. The success has persisted for %s, with success duration being %s.", vars.Condition, valueStr, time.Since(successTime).String(), vars.Duration),
	}, nil
}

func handleFailCompare(wfCtx wfContext.Context, stepID, valueStr string, vars PromVars) (*PromReturns, error) {
	// clean up success timeStamp
	setMetricsStatusTime(wfCtx, stepID, "success", 0)
	ft := getMetricsStatusTime(wfCtx, stepID, "")

	if ft == 0 {
		// first failed
		return &PromReturns{
			Result:  false,
			Failed:  false,
			Message: fmt.Sprintf("The healthy condition should be %s, but the query result is %s, indicating failure, with the failure duration being %s. This is first failed checking.", vars.Condition, valueStr, vars.FailDuration),
		}, nil
	}

	failTime := time.Unix(ft, 0)
	duration, err := time.ParseDuration(vars.FailDuration)
	if err != nil {
		return nil, fmt.Errorf("failed to parse duration %s: %w", vars.FailDuration, err)
	}
	if failTime.Add(duration).Before(time.Now()) {
		return &PromReturns{
			Result:  false,
			Failed:  true,
			Message: fmt.Sprintf("The healthy condition should be %s, but the query result is %s, indicating failure. The failure has persisted for %s, with the failure duration being %s. The check has terminated.", vars.Condition, valueStr, time.Since(failTime).String(), vars.FailDuration),
		}, nil
	}
	return &PromReturns{
		Result:  false,
		Failed:  false,
		Message: fmt.Sprintf("The healthy condition should be %s, but the query result is %s, indicating failure. The failure has persisted for %s, with the failure duration being %s.", vars.Condition, valueStr, time.Since(failTime).String(), vars.FailDuration),
	}, nil
}

func getQueryResult(ctx context.Context, vars PromVars) (string, error) {
	c, err := api.NewClient(api.Config{
		Address: vars.MetricEndpoint,
	})
	if err != nil {
		return "", err
	}
	promCli := v1.NewAPI(c)
	resp, _, err := promCli.Query(ctx, vars.Query, time.Now())
	if err != nil {
		return "", err
	}

	var valueStr string
	switch v := resp.(type) {
	case *prommodel.Scalar:
		valueStr = v.Value.String()
	case prommodel.Vector:
		if len(v) != 1 {
			return "", fmt.Errorf("the query is returning %d results when it should only return one. Please review the query to identify and fix the issue", len(v))
		}
		valueStr = v[0].Value.String()
	default:
		return "", fmt.Errorf("cannot handle the not query value")
	}
	return valueStr, nil
}

func compareValueWithCondition(_ context.Context, valueStr string, vars PromVars) (bool, error) {
	template := fmt.Sprintf("if: %s %s", valueStr, vars.Condition)
	res, err := cuecontext.New().CompileString(template).LookupPath(cue.ParsePath("if")).Bool()
	if err != nil {
		return false, err
	}
	return res, nil
}

func setMetricsStatusTime(wfCtx wfContext.Context, stepID string, status string, time int64) {
	wfCtx.SetMutableValue(strconv.FormatInt(time, 10), stepID, "metrics", status, "time")
}

func getMetricsStatusTime(wfCtx wfContext.Context, stepID string, status string) int64 {
	str := wfCtx.GetMutableValue(stepID, "metrics", status, "time")
	if len(str) == 0 {
		return 0
	}
	t, _ := strconv.ParseInt(str, 10, 64)
	return t
}

//go:embed metrics.cue
var template string

// GetTemplate returns the metrics template
func GetTemplate() string {
	return template
}

// GetProviders returns the metrics provider
func GetProviders() map[string]cuexruntime.ProviderFn {
	return map[string]cuexruntime.ProviderFn{
		"promCheck": providertypes.LegacyGenericProviderFn[PromVars, PromReturns](PromCheck),
	}
}
