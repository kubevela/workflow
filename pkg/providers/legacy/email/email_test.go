/*
Copyright 2022 The KubeVela Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package email

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	. "github.com/agiledragon/gomonkey/v2"
	"github.com/stretchr/testify/require"
	"gopkg.in/gomail.v2"

	"github.com/kubevela/workflow/pkg/cue/model"
	"github.com/kubevela/workflow/pkg/cue/process"
	"github.com/kubevela/workflow/pkg/mock"
	providertypes "github.com/kubevela/workflow/pkg/providers/types"
)

func TestSendEmail(t *testing.T) {
	ctx := context.Background()
	var dial *gomail.Dialer
	pCtx := process.NewContext(process.ContextData{})
	pCtx.PushData(model.ContextStepSessionID, "test-id")
	act := &mock.Action{}

	testCases := map[string]struct {
		vars        MailVars
		from        string
		expectedErr error
		errMsg      string
	}{
		"success": {
			vars: MailVars{
				From: Sender{
					Address:  "kubevela@gmail.com",
					Alias:    "kubevela-bot",
					Password: "pwd",
					Host:     "smtp.test.com",
					Port:     465,
				},
				To: []string{"user1@gmail.com", "user2@gmail.com"},
				Content: Content{
					Subject: "Subject",
					Body:    "Test body.",
				},
			},
		},
		"send-fail": {
			vars: MailVars{
				From: Sender{
					Address:  "kubevela@gmail.com",
					Alias:    "kubevela-bot",
					Password: "pwd",
					Host:     "smtp.test.com",
					Port:     465,
				},
				To: []string{"user1@gmail.com", "user2@gmail.com"},
				Content: Content{
					Subject: "fail",
					Body:    "Test body.",
				},
			},
			errMsg: "fail to send",
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			r := require.New(t)

			patch := ApplyMethod(reflect.TypeOf(dial), "DialAndSend", func(_ *gomail.Dialer, _ ...*gomail.Message) error {
				return nil
			})
			defer patch.Reset()

			if tc.errMsg != "" {
				patch.Reset()
				patch = ApplyMethod(reflect.TypeOf(dial), "DialAndSend", func(_ *gomail.Dialer, _ ...*gomail.Message) error {
					return errors.New(tc.errMsg)
				})
				defer patch.Reset()
			}
			_, err := Send(ctx, &MailParams{
				Params: tc.vars,
				RuntimeParams: providertypes.RuntimeParams{
					ProcessContext: pCtx,
					Action:         act,
				},
			})
			if tc.expectedErr != nil {
				r.Equal(tc.expectedErr.Error(), err.Error())
				return
			}
			r.NoError(err)
			r.Equal(act.Phase, "Wait")

			// mock reconcile
			time.Sleep(time.Second)
			_, err = Send(ctx, &MailParams{
				Params: tc.vars,
				RuntimeParams: providertypes.RuntimeParams{
					ProcessContext: pCtx,
					Action:         act,
				},
			})
			if tc.errMsg != "" {
				r.Equal(fmt.Errorf("failed to send email: %s", tc.errMsg), err)
				return
			}
			r.NoError(err)
		})
	}
}
