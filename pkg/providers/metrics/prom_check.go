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
	"fmt"
	"strconv"
	"time"

	monitorContext "github.com/kubevela/pkg/monitor/context"
	wfContext "github.com/kubevela/workflow/pkg/context"
	"github.com/kubevela/workflow/pkg/cue/model/value"
	"github.com/kubevela/workflow/pkg/types"
	"github.com/prometheus/client_golang/api"
	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
)

const (
	// ProviderName is provider name for install.
	ProviderName = "metrics"
)

type provider struct{}

// PromCheck do health check from metrics from prometheus
func (h *provider) PromCheck(ctx monitorContext.Context, wfCtx wfContext.Context, v *value.Value, act types.Action) error { //nolint:golint,unused
	stepID, err := v.GetString("stepID")
	if err != nil {
		return err
	}

	valueStr, err := getQueryResult(ctx, v)
	if err != nil {
		return err
	}

	conditionStr, err := v.GetString("condition")
	if err != nil {
		return err
	}

	res, err := compareValueWithCondition(valueStr, conditionStr, v)

	if err != nil {
		return err
	}

	if res {
		// meet the condition
		return handleSuccessCompare(wfCtx, stepID, v, conditionStr, valueStr)
	}
	return handleFailCompare(wfCtx, stepID, v, conditionStr, valueStr)
}

func handleSuccessCompare(wfCtx wfContext.Context, stepID string, v *value.Value, conditionStr, valueStr string) error {
	// clean up fail timeStamp
	setMetricsStatusTime(wfCtx, stepID, "fail", 0)
	d, err := v.GetString("duration")
	if err != nil {
		return err
	}
	duration, err := time.ParseDuration(d)
	if err != nil {
		return err
	}

	st := getMetricsStatusTime(wfCtx, stepID, "success")
	if st == 0 {
		// first success
		if err := v.FillObject(fmt.Sprintf("The healthy condition should be %s, and the query result is %s, indicating success.", conditionStr, valueStr), "message"); err != nil {
			return err
		}
		setMetricsStatusTime(wfCtx, stepID, "success", time.Now().Unix())
		return v.FillObject(false, "result")
	}
	successTime := time.Unix(st, 0)
	if successTime.Add(duration).Before(time.Now()) {
		if err = v.FillObject("The metric check has passed successfully.", "message"); err != nil {
			return err
		}
		return v.FillObject(true, "result")
	}
	if err := v.FillObject(fmt.Sprintf("The healthy condition should be %s, and the query result is %s, indicating success. The success has persisted for %s, with success duration being %s.", conditionStr, valueStr, time.Since(successTime).String(), duration), "message"); err != nil {
		return err
	}
	return v.FillObject(false, "result")
}

func handleFailCompare(wfCtx wfContext.Context, stepID string, v *value.Value, conditionStr, valueStr string) error {
	// clean up success timeStamp
	setMetricsStatusTime(wfCtx, stepID, "success", 0)
	ft := getMetricsStatusTime(wfCtx, stepID, "")
	d, err := v.GetString("failDuration")
	if err != nil {
		return err
	}
	failDuration, err := time.ParseDuration(d)
	if err != nil {
		return err
	}

	if ft == 0 {
		// first failed
		setMetricsStatusTime(wfCtx, stepID, "fail", time.Now().Unix())
		if err := v.FillObject(fmt.Sprintf("The healthy condition should be %s, but the query result is %s, indicating failure, with the failure duration being %s. This is first failed checking.", conditionStr, valueStr, failDuration), "message"); err != nil {
			return err
		}
		return v.FillObject(false, "result")
	}

	failTime := time.Unix(ft, 0)
	if failTime.Add(failDuration).Before(time.Now()) {
		if err = v.FillObject(true, "failed"); err != nil {
			return err
		}
		if err := v.FillObject(fmt.Sprintf("The healthy condition should be %s, but the query result is %s, indicating failure. The failure has persisted for %s, with the failure duration being %s. The check has terminated.", conditionStr, valueStr, time.Since(failTime).String(), failDuration), "message"); err != nil {
			return err
		}
		return v.FillObject(false, "result")
	}
	if err := v.FillObject(fmt.Sprintf("The healthy condition should be %s, but the query result is %s, indicating failure. The failure has persisted for %s, with the failure duration being %s.", conditionStr, valueStr, time.Since(failTime).String(), failDuration), "message"); err != nil {
		return err
	}
	return v.FillObject(false, "result")
}

func getQueryResult(ctx monitorContext.Context, v *value.Value) (string, error) {
	addr, err := v.GetString("metricEndpoint")
	if err != nil {
		return "", err
	}
	c, err := api.NewClient(api.Config{
		Address: addr,
	})
	if err != nil {
		return "", err
	}
	promCli := v1.NewAPI(c)
	query, err := v.GetString("query")
	if err != nil {
		return "", err
	}
	resp, _, err := promCli.Query(ctx, query, time.Now())
	if err != nil {
		return "", err
	}

	var valueStr string
	switch v := resp.(type) {
	case *model.Scalar:
		valueStr = v.Value.String()
	case model.Vector:
		if len(v) != 1 {
			return "", fmt.Errorf(fmt.Sprintf("ehe query is returning %d results when it should only return one. Please review the query to identify and fix the issue", len(v)))
		}
		valueStr = v[0].Value.String()
	default:
		return "", fmt.Errorf("cannot handle the not query value")
	}
	return valueStr, nil
}

func compareValueWithCondition(valueStr string, conditionStr string, v *value.Value) (bool, error) { //nolint:golint,unused
	template := fmt.Sprintf("if: %s %s", valueStr, conditionStr)
	cueValue, err := value.NewValue(template, nil, "")
	if err != nil {
		return false, err
	}
	res, err := cueValue.GetBool("if")
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

// Install register handlers to provider discover.
func Install(p types.Providers) {
	prd := &provider{}
	p.Register(ProviderName, map[string]types.Handler{
		"promCheck": prd.PromCheck,
	})
}
