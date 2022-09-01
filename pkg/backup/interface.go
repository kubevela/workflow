package backup

import (
	"context"

	"github.com/kubevela/workflow/api/v1alpha1"
)

// PersistType is the type of persister.
type PersistType string

const (
	// PersistTypeSLS is the SLS persister.
	PersistTypeSLS PersistType = "sls"
)

// NewPersister is a factory method for creating a persister.
func NewPersister(persistType PersistType) persistWorkflowRecord {
	switch persistType {
	case PersistTypeSLS:
		return &slsHandler{}
	default:
		return nil
	}
}

type persistWorkflowRecord interface {
	Store(ctx context.Context, run *v1alpha1.WorkflowRun) error
}

type slsHandler struct {
}

func (s *slsHandler) Store(ctx context.Context, run *v1alpha1.WorkflowRun) error {
	return nil
}
