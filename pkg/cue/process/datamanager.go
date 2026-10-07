package process

import "github.com/kubevela/workflow/pkg/cue/model"

// DataManager is in charge of injecting and removing runtime context for ContextData
type DataManager interface {
	Fill(ctx Context, kvs []StepMetaKV)
	Remove(ctx Context, keys []string)
}

// StepRunTimeMeta manage step runtime metadata
type StepRunTimeMeta struct{}

// StepMetaKV store the key and value of step runtime metadata
type StepMetaKV struct {
	Key   string
	Value interface{}
}

// WithSessionID return stepSessionID of the step
func WithSessionID(id string) StepMetaKV {
	return StepMetaKV{
		Key:   model.ContextStepSessionID,
		Value: id,
	}
}

// WithName return stepName of the step
func WithName(name string) StepMetaKV {
	return StepMetaKV{
		Key:   model.ContextStepName,
		Value: name,
	}
}

// WithGroupName return stepGroupName of the step
func WithGroupName(name string) StepMetaKV {
	return StepMetaKV{
		Key:   model.ContextStepGroupName,
		Value: name,
	}
}

// WithLoop return the for-each item and index of the step
func WithLoop(item interface{}, index int) StepMetaKV {
	return StepMetaKV{
		Key:   model.ContextLoop,
		Value: map[string]interface{}{"item": item, "index": index},
	}
}

// WithSpanID return spanID of the step
func WithSpanID(id string) StepMetaKV {
	return StepMetaKV{
		Key:   model.ContextSpanID,
		Value: id,
	}
}

// NewStepRunTimeMeta create step runtime metadata manager
func NewStepRunTimeMeta() DataManager {
	return &StepRunTimeMeta{}
}

// Fill will fill step runtime metadata for ContextData
func (s *StepRunTimeMeta) Fill(ctx Context, kvs []StepMetaKV) {
	for _, kv := range kvs {
		ctx.PushData(kv.Key, kv.Value)
	}
}

// Remove remove step runtime metadata of ContextData
func (s *StepRunTimeMeta) Remove(ctx Context, keys []string) {
	for _, key := range keys {
		ctx.RemoveData(key)
	}
}
