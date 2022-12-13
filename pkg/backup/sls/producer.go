package sls

import (
	"encoding/json"
	"fmt"
	"time"

	monitorContext "github.com/kubevela/pkg/monitor/context"

	"github.com/aliyun/aliyun-log-go-sdk/producer"
	"github.com/kubevela/workflow/api/v1alpha1"
)

// Handler is sls config.
type Handler struct {
	LogStoreName   string
	ProjectName    string
	ProducerConfig *producer.ProducerConfig
}

// Callback is for sls callback
type Callback struct {
	ctx monitorContext.Context
}

// NewSLSHandler create a new sls handler
func NewSLSHandler(config map[string][]byte) (*Handler, error) {
	endpoint := string(config["Endpoint"])
	accessKeyID := string(config["AccessKeyID"])
	accessKeySecret := string(config["AccessKeySecret"])
	projectName := string(config["ProjectName"])
	logStoreName := string(config["LogStoreName"])
	if endpoint == "" || accessKeyID == "" || accessKeySecret == "" || projectName == "" || logStoreName == "" {
		return nil, fmt.Errorf("invalid SLS config, please make sure endpoint/ak/sk/project/logstore are both provided correctly")
	}
	producerConfig := producer.GetDefaultProducerConfig()
	producerConfig.Endpoint = endpoint
	producerConfig.AccessKeyID = accessKeyID
	producerConfig.AccessKeySecret = accessKeySecret

	return &Handler{
		ProducerConfig: producerConfig,
		LogStoreName:   logStoreName,
		ProjectName:    projectName,
	}, nil
}

// Fail is fail callback
func (callback *Callback) Fail(result *producer.Result) {
	callback.ctx.Error(fmt.Errorf("failed to send log to sls"), result.GetErrorMessage(), "errorCode", result.GetErrorCode(), "requestId", result.GetRequestId())
}

// Success is success callback
func (callback *Callback) Success(result *producer.Result) {
}

// Store is store workflowRun to sls
func (s *Handler) Store(ctx monitorContext.Context, run *v1alpha1.WorkflowRun) error {
	ctx.Info("Start Send workflow record to SLS")
	p := producer.InitProducer(s.ProducerConfig)
	p.Start()
	defer p.SafeClose()

	data, err := json.Marshal(run)
	if err != nil {
		ctx.Error(err, "Marshal WorkflowRun Content fail")
		return err
	}

	callback := &Callback{ctx: ctx}
	log := producer.GenerateLog(uint32(time.Now().Unix()), map[string]string{"content": string(data)})
	err = p.SendLogWithCallBack(s.ProjectName, s.LogStoreName, "topic", "", log, callback)
	if err != nil {
		ctx.Error(err, "Send WorkflowRun Content to SLS fail")
		return err
	}

	return nil
}
