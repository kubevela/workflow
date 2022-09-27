package backup

import (
	"context"

	"github.com/kubevela/workflow/api/v1alpha1"
	"github.com/kubevela/workflow/pkg/backup/sls"
)

const (
	// PersistTypeSLS is the SLS persister.
	PersistTypeSLS string = "sls"
)

// NewPersister is a factory method for creating a persister.
func NewPersister(persistType string, config map[string][]byte) persistWorkflowRecord {
	switch persistType {
	case PersistTypeSLS:
		return &sls.Handler{
			LogStoreName:    string(config["LogStoreName"]),
			ProjectName:     string(config["ProjectName"]),
			Endpoint:        string(config["Endpoint"]),
			AccessKeyID:     string(config["AccessKeyID"]),
			AccessKeySecret: string(config["AccessKeySecret"]),
		}
	default:
		return nil
	}
}

type persistWorkflowRecord interface {
	Store(ctx context.Context, run *v1alpha1.WorkflowRun) error
}
