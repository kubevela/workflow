package sls

import (
	"context"
	"testing"

	monitorContext "github.com/kubevela/pkg/monitor/context"
	"github.com/stretchr/testify/require"

	"github.com/kubevela/workflow/api/v1alpha1"
)

func TestHandler_Store(t *testing.T) {
	tests := []struct {
		name    string
		config  map[string][]byte
		run     *v1alpha1.WorkflowRun
		wantErr bool
	}{
		{
			name: "Success",
			config: map[string][]byte{
				"AccessKeyID":     []byte("accessKeyID"),
				"AccessKeySecret": []byte("accessKeySecret"),
				"Endpoint":        []byte("endpoint"),
				"ProjectName":     []byte("project"),
				"LogStoreName":    []byte("logstore"),
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			ctx := monitorContext.NewTraceContext(context.Background(), "test")
			s, err := NewSLSHandler(tt.config)
			r.NoError(err)
			if err := s.Store(ctx, tt.run); (err != nil) != tt.wantErr {
				t.Errorf("Store() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
