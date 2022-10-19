package sls

import (
	"encoding/json"
	"time"

	monitorContext "github.com/kubevela/pkg/monitor/context"

	"github.com/aliyun/aliyun-log-go-sdk/producer"
	"github.com/kubevela/workflow/api/v1alpha1"
)

// Handler is sls config.
type Handler struct {
	LogStoreName    string
	ProjectName     string
	Endpoint        string
	AccessKeyID     string
	AccessKeySecret string
}

// Store is store workflowRun to sls
func (s *Handler) Store(ctx monitorContext.Context, run *v1alpha1.WorkflowRun) error {
	ctx.Info("Start Send workflow record to SLS")
	producerConfig := producer.GetDefaultProducerConfig()
	producerConfig.Endpoint = s.Endpoint
	producerConfig.AccessKeyID = s.AccessKeyID
	producerConfig.AccessKeySecret = s.AccessKeySecret

	producerInstance := producer.InitProducer(producerConfig)
	producerInstance.Start()
	defer func(producerInstance *producer.Producer, timeoutMs int64) {
		err := producerInstance.Close(timeoutMs)
		if err != nil {
			ctx.Error(err, "Close SLS fail")
		}
	}(producerInstance, 60000)

	data, err := json.Marshal(run)
	if err != nil {
		ctx.Error(err, "Marshal WorkflowRun Content fail")
		return err
	}

	log := producer.GenerateLog(uint32(time.Now().Unix()), map[string]string{"content": string(data)})
	err = producerInstance.SendLog(s.ProjectName, s.LogStoreName, "topic", "", log)
	if err != nil {
		ctx.Error(err, "Send WorkflowRun Content to SLS fail")
		return err
	}

	return nil
}
