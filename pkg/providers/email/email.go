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
	_ "embed"
	"fmt"
	"sync"

	"gopkg.in/gomail.v2"

	cuexruntime "github.com/kubevela/pkg/cue/cuex/runtime"

	"github.com/kubevela/workflow/pkg/cue/model"
	"github.com/kubevela/workflow/pkg/errors"
	providertypes "github.com/kubevela/workflow/pkg/providers/types"
)

const (
	// ProviderName is provider name for install.
	ProviderName = "email"
)

// Sender is the sender of email
type Sender struct {
	Address  string `json:"address"`
	Alias    string `json:"alias,omitempty"`
	Password string `json:"password"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
}

// Content is the content of email
type Content struct {
	Subject string `json:"subject"`
	Body    string `json:"body"`
}

// MailVars .
type MailVars struct {
	From    Sender   `json:"from"`
	To      []string `json:"to"`
	Content Content  `json:"content"`
}

// MailParams .
type MailParams = providertypes.Params[MailVars]

var emailRoutine sync.Map

// Send sends email
func Send(_ context.Context, params *MailParams) (res *any, err error) {
	pCtx := params.ProcessContext
	act := params.Action
	id := fmt.Sprint(pCtx.GetData(model.ContextStepSessionID))
	routine, ok := emailRoutine.Load(id)
	if ok {
		switch routine {
		case "success":
			emailRoutine.Delete(id)
			return nil, nil
		case "initializing", "sending":
			act.Wait("wait for the email")
			return nil, errors.GenericActionError(errors.ActionWait)
		default:
			emailRoutine.Delete(id)
			return nil, fmt.Errorf("failed to send email: %v", routine)
		}
	} else {
		emailRoutine.Store(id, "initializing")
	}

	sender := params.Params.From
	content := params.Params.Content
	m := gomail.NewMessage()
	m.SetAddressHeader("From", sender.Address, sender.Alias)
	m.SetHeader("To", params.Params.To...)
	m.SetHeader("Subject", content.Subject)
	m.SetBody("text/html", content.Body)

	dial := gomail.NewDialer(sender.Host, sender.Port, sender.Address, sender.Password)
	go func() {
		if routine, ok := emailRoutine.Load(id); ok && routine == "initializing" {
			emailRoutine.Store(id, "sending")
			if err = dial.DialAndSend(m); err != nil {
				emailRoutine.Store(id, err.Error())
				return
			}
			emailRoutine.Store(id, "success")
		}
	}()
	act.Wait("wait for the email")
	return nil, errors.GenericActionError(errors.ActionWait)
}

//go:embed email.cue
var template string

// GetTemplate returns the template
func GetTemplate() string {
	return template
}

// GetProviders returns the provider
func GetProviders() map[string]cuexruntime.ProviderFn {
	return map[string]cuexruntime.ProviderFn{
		"send": providertypes.GenericProviderFn[MailVars, any](Send),
	}
}
