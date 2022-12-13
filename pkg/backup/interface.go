package backup

import (
	"fmt"

	monitorContext "github.com/kubevela/pkg/monitor/context"
	"github.com/kubevela/workflow/api/v1alpha1"
	"github.com/kubevela/workflow/pkg/backup/sls"
)

const (
	// PersistTypeSLS is the SLS persister.
	PersistTypeSLS string = "sls"
)

// NewPersister is a factory method for creating a persister.
func NewPersister(config map[string][]byte, persistType string) (PersistWorkflowRecord, error) {
	if config == nil {
		return nil, fmt.Errorf("empty config")
	}
	switch persistType {
	case PersistTypeSLS:
		return sls.NewSLSHandler(config)
	case "":
		return nil, nil
	default:
		return nil, fmt.Errorf("unsupported persist type %s", persistType)
	}
}

// PersistWorkflowRecord is the interface for record persist
type PersistWorkflowRecord interface {
	Store(ctx monitorContext.Context, run *v1alpha1.WorkflowRun) error
}
