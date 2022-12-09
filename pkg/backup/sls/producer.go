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
	LogStoreName string
	ProjectName  string
	Producer     *producer.Producer
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
		Producer:     producer.InitProducer(producerConfig),
		LogStoreName: logStoreName,
		ProjectName:  projectName,
	}, nil
}

// Store is store workflowRun to sls
func (s *Handler) Store(ctx monitorContext.Context, run *v1alpha1.WorkflowRun) error {
	ctx.Info("Start Send workflow record to SLS")
	s.Producer.Start()
	defer func(producerInstance *producer.Producer, timeoutMs int64) {
		err := producerInstance.Close(timeoutMs)
		if err != nil {
			ctx.Error(err, "Close SLS fail")
		}
	}(s.Producer, 60000)

	data, err := json.Marshal(run)
	if err != nil {
		ctx.Error(err, "Marshal WorkflowRun Content fail")
		return err
	}

	log := producer.GenerateLog(uint32(time.Now().Unix()), map[string]string{"content": string(data)})
	err = s.Producer.SendLog(s.ProjectName, s.LogStoreName, "topic", "", log)
	if err != nil {
		ctx.Error(err, "Send WorkflowRun Content to SLS fail")
		return err
	}

	return nil
}
