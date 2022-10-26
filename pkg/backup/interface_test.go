package backup

import (
	"reflect"
	"testing"

	"github.com/kubevela/workflow/pkg/backup/sls"
)

func TestNewPersister(t *testing.T) {
	type args struct {
		persistType string
		config      map[string][]byte
	}
	tests := []struct {
		name string
		args args
		want persistWorkflowRecord
	}{
		{
			name: "Empty config",
			args: args{
				persistType: "sls",
				config:      nil,
			},
			want: nil,
		},
		{
			name: "Success",
			args: args{
				persistType: "sls",
				config:      make(map[string][]byte),
			},
			want: &sls.Handler{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NewPersister(tt.args.persistType, tt.args.config); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("NewPersister() = %v, want %v", got, tt.want)
			}
		})
	}
}
