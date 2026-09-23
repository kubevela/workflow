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
	"fmt"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	. "github.com/agiledragon/gomonkey/v2"
	"github.com/kubevela/pkg/cache"
	"github.com/stretchr/testify/require"
	"gopkg.in/gomail.v2"

	"github.com/kubevela/workflow/pkg/cue/model"
	"github.com/kubevela/workflow/pkg/cue/process"
	"github.com/kubevela/workflow/pkg/errors"
	"github.com/kubevela/workflow/pkg/mock"
	providertypes "github.com/kubevela/workflow/pkg/providers/types"
)

// resetEmailCache gives every test a fresh cache so no state leaks across tests
// or repeated runs (-count=N).
func resetEmailCache(t *testing.T) {
	t.Helper()
	emailRoutine = cache.NewMemoryCacheStore[string](context.Background())
}

func sendEmail(ctx context.Context, id string, act *mock.Action, vars MailVars) (*any, error) {
	pCtx := process.NewContext(process.ContextData{})
	pCtx.PushData(model.ContextStepSessionID, id)
	return Send(ctx, &MailParams{
		Params: vars,
		RuntimeParams: providertypes.RuntimeParams{
			ProcessContext: pCtx,
			Action:         act,
		},
	})
}

// waitEmailTerminal blocks until the in-flight goroutine has recorded a terminal
// state ("success" or an error string) for the given session id.
func waitEmailTerminal(t *testing.T, id string) {
	t.Helper()
	require.Eventually(t, func() bool {
		v, ok := emailRoutine.Get(id)
		if !ok {
			return false
		}
		s, _ := v.(string)
		return s == "success" || (s != "initializing" && s != "sending")
	}, 5*time.Second, 10*time.Millisecond)
}

func TestSendEmail(t *testing.T) {
	resetEmailCache(t)
	ctx := context.Background()

	testCases := map[string]struct {
		vars   MailVars
		errMsg string
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
			id := "single-" + name
			act := &mock.Action{}
			var calls atomic.Int32

			dial := &gomail.Dialer{}
			patch := ApplyMethod(reflect.TypeOf(dial), "DialAndSend",
				func(_ *gomail.Dialer, _ ...*gomail.Message) error {
					calls.Add(1)
					if tc.errMsg != "" {
						return fmt.Errorf(tc.errMsg)
					}
					return nil
				})
			defer patch.Reset()

			// first invocation initializes the send
			_, err := sendEmail(ctx, id, act, tc.vars)
			_, isWait := err.(errors.GenericActionError)
			r.True(isWait)
			r.Equal("Wait", act.Phase)

			// wait until the goroutine records the outcome
			waitEmailTerminal(t, id)

			// the reconcile consumes the recorded state
			_, err = sendEmail(ctx, id, act, tc.vars)
			if tc.errMsg != "" {
				r.Error(err)
				r.Contains(err.Error(), tc.errMsg)
			} else {
				r.NoError(err)
			}

			// entry is explicitly deleted after completion
			_, ok := emailRoutine.Get(id)
			r.False(ok, "cache entry should be cleaned up after the reconcile")

			// the email must be sent exactly once, regardless of reconciles
			r.Equal(int32(1), calls.Load(), "email must not be sent twice")
		})
	}
}

// TestSendEmailNoDoubleSendAfterSuccess verifies that a fresh step invocation
// for the same session id is not wrongly deduped by stale state: after a
// completed send the entry is deleted, so a new send starts from scratch.
func TestSendEmailNoDoubleSendAfterSuccess(t *testing.T) {
	resetEmailCache(t)
	ctx := context.Background()
	id := "resend-id"
	act := &mock.Action{}
	mailVars := MailVars{
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
	}
	var calls atomic.Int32

	dial := &gomail.Dialer{}
	patch := ApplyMethod(reflect.TypeOf(dial), "DialAndSend",
		func(_ *gomail.Dialer, _ ...*gomail.Message) error {
			calls.Add(1)
			return nil
		})
	defer patch.Reset()

	r := require.New(t)

	// first cycle: send + resolve
	_, err := sendEmail(ctx, id, act, mailVars)
	_, isWait := err.(errors.GenericActionError)
	r.True(isWait)
	waitEmailTerminal(t, id)
	_, err = sendEmail(ctx, id, act, mailVars)
	r.NoError(err)
	_, ok := emailRoutine.Get(id)
	r.False(ok)

	// second cycle: a brand new send must be started for the same id
	_, err = sendEmail(ctx, id, act, mailVars)
	_, isWait = err.(errors.GenericActionError)
	r.True(isWait)
	waitEmailTerminal(t, id)
	_, err = sendEmail(ctx, id, act, mailVars)
	r.NoError(err)
	_, ok = emailRoutine.Get(id)
	r.False(ok)

	r.Equal(int32(2), calls.Load(), "each fresh cycle must send exactly once")
}

// TestSendEmailWaitsWhileInFlight verifies that a reconcile arriving while the
// send is still in flight returns a Wait action instead of re-sending.
func TestSendEmailWaitsWhileInFlight(t *testing.T) {
	resetEmailCache(t)
	ctx := context.Background()
	id := "in-flight-id"
	act := &mock.Action{}
	mailVars := MailVars{
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
	}

	entered := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	var calls atomic.Int32

	dial := &gomail.Dialer{}
	patch := ApplyMethod(reflect.TypeOf(dial), "DialAndSend",
		func(_ *gomail.Dialer, _ ...*gomail.Message) error {
			calls.Add(1)
			once.Do(func() { close(entered) })
			<-release
			return nil
		})
	defer patch.Reset()

	r := require.New(t)

	_, err := sendEmail(ctx, id, act, mailVars)
	_, isWait := err.(errors.GenericActionError)
	r.True(isWait)
	r.Equal("Wait", act.Phase)

	select {
	case <-entered: // dial in progress, cache is in "sending" state
	case <-time.After(5 * time.Second):
		t.Fatal("send did not start")
	}

	// reconcile while the send is in flight: wait, do not send again
	_, err = sendEmail(ctx, id, act, mailVars)
	_, isWait = err.(errors.GenericActionError)
	r.True(isWait)
	r.Equal("Wait", act.Phase)
	r.Equal(int32(1), calls.Load(), "an in-flight send must not be re-sent")

	close(release)
	waitEmailTerminal(t, id)

	// final reconcile resolves the step and cleans up
	_, err = sendEmail(ctx, id, act, mailVars)
	r.NoError(err)
	_, ok := emailRoutine.Get(id)
	r.False(ok)
	r.Equal(int32(1), calls.Load())
}

// TestSendEmailConcurrentNoDoubleSend releases a burst of concurrent first
// invocations against an empty cache with a barrier, guarding the cold
// initialization race: exactly one caller may claim the send and call
// DialAndSend, the rest must wait.
func TestSendEmailConcurrentNoDoubleSend(t *testing.T) {
	resetEmailCache(t)
	ctx := context.Background()
	id := "concurrent-id"
	mailVars := MailVars{
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
	}

	entered := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	var calls atomic.Int32

	dial := &gomail.Dialer{}
	patch := ApplyMethod(reflect.TypeOf(dial), "DialAndSend",
		func(_ *gomail.Dialer, _ ...*gomail.Message) error {
			calls.Add(1)
			once.Do(func() { close(entered) })
			<-release
			return nil
		})
	defer patch.Reset()

	const workers = 8
	start := make(chan struct{})
	var waits atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := sendEmail(ctx, id, &mock.Action{}, mailVars)
			if _, ok := err.(errors.GenericActionError); ok {
				waits.Add(1)
			}
		}()
	}
	close(start)
	wg.Wait()

	r := require.New(t)
	r.Equal(int32(workers), waits.Load(), "all concurrent cold callers should wait")

	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("send did not start")
	}
	r.Equal(int32(1), calls.Load(), "exactly one cold caller may dial")

	close(release)
	waitEmailTerminal(t, id)

	_, err := sendEmail(ctx, id, &mock.Action{}, mailVars)
	r.NoError(err)
	_, ok := emailRoutine.Get(id)
	r.False(ok)
	r.Equal(int32(1), calls.Load())
}
