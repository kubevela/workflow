package backup

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestNewPersister(t *testing.T) {
	cli := fake.NewFakeClientWithScheme(scheme.Scheme)
	ctx := context.Background()
	testCases := map[string]struct {
		persistType string
		configName  string
		expectedErr string
		secret      *corev1.Secret
	}{
		"empty config": {
			persistType: "sls",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "valid",
					Namespace: "default",
				},
			},
			expectedErr: "empty config",
		},
		"invalid type": {
			persistType: "invalid",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "valid",
					Namespace: "default",
				},
				Data: map[string][]byte{
					"accessKeyID": []byte("accessKeyID"),
				},
			},
			expectedErr: "unsupported persist type",
		},
		"sls-not-complete": {
			persistType: "sls",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "valid",
					Namespace: "default",
				},
				Data: map[string][]byte{
					"accessKeyID": []byte("accessKeyID"),
				},
			},
			expectedErr: "invalid SLS config",
		},
		"sls-success": {
			persistType: "sls",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "valid",
					Namespace: "default",
				},
				Data: map[string][]byte{
					"AccessKeyID":     []byte("accessKeyID"),
					"AccessKeySecret": []byte("accessKeySecret"),
					"Endpoint":        []byte("endpoint"),
					"ProjectName":     []byte("project"),
					"LogStoreName":    []byte("logstore"),
				},
			},
		},
	}
	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			r := require.New(t)
			if tc.secret != nil {
				r.NoError(cli.Create(ctx, tc.secret))
				defer cli.Delete(ctx, tc.secret)
			}
			_, err := NewPersister(tc.secret.Data, tc.persistType)
			if tc.expectedErr != "" {
				r.Contains(err.Error(), tc.expectedErr)
				return
			}
			r.NoError(err)
		})
	}
}
