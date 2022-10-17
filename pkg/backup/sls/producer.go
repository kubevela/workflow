package sls

import (
	"context"
	"encoding/json"
	sls "github.com/aliyun/aliyun-log-go-sdk"
	"time"

	"github.com/aliyun/aliyun-log-go-sdk/producer"
	"github.com/gogo/protobuf/proto"
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
func (s *Handler) Store(ctx context.Context, run *v1alpha1.WorkflowRun) error {
	producerConfig := producer.GetDefaultProducerConfig()
	producerConfig.Endpoint = s.Endpoint
	producerConfig.AccessKeyID = s.AccessKeyID
	producerConfig.AccessKeySecret = s.AccessKeySecret

	producerInstance := producer.InitProducer(producerConfig)
	producerInstance.Start()
	data, err := json.Marshal(run)
	if err != nil {
		return err
	}

	var content []*sls.LogContent
	content = append(content, &sls.LogContent{
		Key:   proto.String("content"),
		Value: proto.String(string(data)),
	})
	log := &sls.Log{
		Time:     proto.Uint32(uint32(time.Now().Unix())),
		Contents: content,
	}

	err = producerInstance.SendLog(s.ProjectName, s.LogStoreName, "topic", "", log)
	if err != nil {
		return err
	}
	err = producerInstance.Close(60000)
	if err != nil {
		return err
	}

	return nil
}
