package backup

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	monitorContext "github.com/kubevela/pkg/monitor/context"
	"github.com/kubevela/workflow/api/v1alpha1"
	"github.com/kubevela/workflow/pkg/backup/sls"
)

const (
	// PersistTypeSLS is the SLS persister.
	PersistTypeSLS string = "sls"
)

// NewPersister is a factory method for creating a persister.
func NewPersister(ctx context.Context, cli client.Client, persistType, configName, configNamespace string) (PersistWorkflowRecord, error) {
	secret := &corev1.Secret{}
	if err := cli.Get(ctx, client.ObjectKey{Name: configName, Namespace: configNamespace}, secret); err != nil {
		return nil, err
	}
	config := secret.Data
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
