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
	"fmt"
	"sync"

	"gopkg.in/gomail.v2"

	monitorContext "github.com/kubevela/pkg/monitor/context"

	wfContext "github.com/kubevela/workflow/pkg/context"
	"github.com/kubevela/workflow/pkg/cue/model/value"
	"github.com/kubevela/workflow/pkg/types"
)

const (
	// ProviderName is provider name for install.
	ProviderName = "email"
)

type provider struct {
}

type sender struct {
	Address  string `json:"address"`
	Alias    string `json:"alias,omitempty"`
	Password string `json:"password"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
}

type content struct {
	Subject string `json:"subject"`
	Body    string `json:"body"`
}

var emailRoutine sync.Map

// Send sends email
func (h *provider) Send(ctx monitorContext.Context, wfCtx wfContext.Context, v *value.Value, act types.Action) error {
	stepID, err := v.LookupValue("stepID")
	if err != nil {
		return err
	}
	id, err := stepID.String()
	if err != nil {
		return err
	}
	routine, ok := emailRoutine.Load(id)
	if ok {
		switch routine {
		case "success":
			emailRoutine.Delete(id)
			return nil
		case "initializing", "sending":
			act.Wait("wait for the email")
			return nil
		default:
			emailRoutine.Delete(id)
			return fmt.Errorf("failed to send email: %v", routine)
		}
	} else {
		emailRoutine.Store(id, "initializing")
	}

	s, err := v.LookupValue("from")
	if err != nil {
		return err
	}

	senderValue := &sender{}
	if err := s.UnmarshalTo(senderValue); err != nil {
		return err
	}

	r, err := v.LookupValue("to")
	if err != nil {
		return err
	}
	receiverValue := &[]string{}
	if err := r.UnmarshalTo(receiverValue); err != nil {
		return err
	}

	c, err := v.LookupValue("content")
	if err != nil {
		return err
	}
	contentValue := &content{}
	if err := c.UnmarshalTo(contentValue); err != nil {
		return err
	}

	m := gomail.NewMessage()
	m.SetAddressHeader("From", senderValue.Address, senderValue.Alias)
	m.SetHeader("To", *receiverValue...)
	m.SetHeader("Subject", contentValue.Subject)
	m.SetBody("text/html", contentValue.Body)

	dial := gomail.NewDialer(senderValue.Host, senderValue.Port, senderValue.Address, senderValue.Password)
	go func() {
		if routine, ok := emailRoutine.Load(id); ok && routine == "initializing" {
			emailRoutine.Store(id, "sending")
			if err := dial.DialAndSend(m); err != nil {
				emailRoutine.Store(id, err.Error())
				return
			}
			emailRoutine.Store(id, "success")
		}
	}()
	act.Wait("wait for the email")
	return nil
}

// Install register handlers to provider discover.
func Install(p types.Providers) {
	prd := &provider{}
	p.Register(ProviderName, map[string]types.Handler{
		"send": prd.Send,
	})
}
