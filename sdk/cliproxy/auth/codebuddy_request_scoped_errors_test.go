package auth

import (
	"context"
	"net/http"
	"testing"

	internalconfig "github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/registry"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
)

// codeBuddyChannelVerificationRules mirrors the codebuddy block in config.yaml.
// Keep both in sync: these are the patterns that stop a poisoned conversation
// from being replayed against the upstream on every turn.
func codeBuddyChannelVerificationRules() []internalconfig.RequestScopedErrorRule {
	return []internalconfig.RequestScopedErrorRule{
		{
			Status: http.StatusBadRequest,
			Match: []string{
				"11128",
				"Illegal API invocation from an unapproved channel",
				"unapproved channel",
				"conversation id missing",
				"not sent from an official client",
			},
			Action: "stop-and-cooldown",
		},
		{
			Status: http.StatusForbidden,
			Match: []string{
				"11140",
				"request illegal",
			},
			Action: "stop-and-cooldown",
		},
	}
}

// TestCodeBuddyChannelVerificationStopsAndCools covers the real upstream error
// bodies recorded in the field. Every one of them must terminate the request
// without rotating to a second credential, and must cool the reporting one.
func TestCodeBuddyChannelVerificationStopsAndCools(t *testing.T) {
	bodies := []struct {
		name    string
		status  int
		body    string
		comment string
	}{
		{
			name:   "11128 unapproved channel",
			status: http.StatusBadRequest,
			body:   `{"code":11128,"msg":"Illegal API invocation from an unapproved channel","requestId":"01a162c6c8f574c38e06834cf0645aa9"}`,
		},
		{
			name:   "11128 conversation id missing",
			status: http.StatusBadRequest,
			body:   `{"code":11128,"msg":"conversation id missing"}`,
		},
		{
			name:   "11128 display tips official client",
			status: http.StatusBadRequest,
			body:   `{"code":11128,"msg":"request blocked","displayTips":{"en":"This call failed channel verification, possibly because it was not sent from an official client."}}`,
		},
		{
			name:   "11140 request illegal",
			status: http.StatusForbidden,
			body:   `{"code":-32603,"message":"Internal error","data":{"details":"403 request illegal (7e15e5f6b0cf47efbc33f39900f41224/1623e72e-8e21-40b2-a5ab-9fa081dc96db)","statusCode":403,"code":11140}}`,
		},
	}

	for _, tt := range bodies {
		t.Run(tt.name, func(t *testing.T) {
			previous := quotaCooldownDisabled.Load()
			quotaCooldownDisabled.Store(false)
			t.Cleanup(func() { quotaCooldownDisabled.Store(previous) })

			cfg := &internalconfig.Config{
				OAuthRequestScopedErrors: map[string][]internalconfig.RequestScopedErrorRule{
					"codebuddy": codeBuddyChannelVerificationRules(),
				},
			}
			m := NewManager(nil, nil, nil)
			m.SetConfig(cfg)

			first := &Auth{ID: "auth-codebuddy-1", Provider: "codebuddy", Status: StatusActive,
				Attributes: map[string]string{"auth_kind": "oauth", "priority": "10"}}
			second := &Auth{ID: "auth-codebuddy-2", Provider: "codebuddy", Status: StatusActive,
				Attributes: map[string]string{"auth_kind": "oauth", "priority": "5"}}

			reg := registry.GetGlobalRegistry()
			for _, a := range []*Auth{first, second} {
				reg.RegisterClient(a.ID, "codebuddy", []*registry.ModelInfo{{ID: "deepseek-v4.1-flash"}})
				if _, errRegister := m.Register(context.Background(), a); errRegister != nil {
					t.Fatalf("register %s: %v", a.ID, errRegister)
				}
			}
			t.Cleanup(func() {
				reg.UnregisterClient(first.ID)
				reg.UnregisterClient(second.ID)
			})

			execCount := 0
			m.RegisterExecutor(&mockCustomErrorExecutor{
				identifier: "codebuddy",
				executeFn: func(_ context.Context, _ *Auth, _ cliproxyexecutor.Request, _ cliproxyexecutor.Options) (cliproxyexecutor.Response, error) {
					execCount++
					return cliproxyexecutor.Response{}, customStatusError{code: tt.status, msg: tt.body}
				},
			})

			_, errExec := m.Execute(context.Background(), []string{"codebuddy"},
				cliproxyexecutor.Request{Model: "deepseek-v4.1-flash"}, cliproxyexecutor.Options{})
			if errExec == nil {
				t.Fatal("expected upstream error, got nil")
			}
			if execCount != 1 {
				t.Fatalf("execCount = %d, want 1: a channel-verification failure is request-scoped and must not rotate credentials", execCount)
			}

			reporting, ok := m.GetByID(first.ID)
			if !ok {
				t.Fatal("reporting auth not found")
			}
			if !reporting.Unavailable && reporting.NextRetryAfter.IsZero() {
				t.Fatalf("reporting auth was not cooled: status=%v unavailable=%v nextRetry=%v",
					reporting.Status, reporting.Unavailable, reporting.NextRetryAfter)
			}
		})
	}
}

// TestCodeBuddyNonChannelErrorsStillRotate makes sure the rules stay narrow:
// an unrelated 400 must keep the default request-fault behaviour.
func TestCodeBuddyNonChannelErrorsStillRotate(t *testing.T) {
	previous := quotaCooldownDisabled.Load()
	quotaCooldownDisabled.Store(false)
	t.Cleanup(func() { quotaCooldownDisabled.Store(previous) })

	cfg := &internalconfig.Config{
		OAuthRequestScopedErrors: map[string][]internalconfig.RequestScopedErrorRule{
			"codebuddy": codeBuddyChannelVerificationRules(),
		},
	}
	m := NewManager(nil, nil, nil)
	m.SetConfig(cfg)

	auth := &Auth{ID: "auth-codebuddy-plain", Provider: "codebuddy", Status: StatusActive,
		Attributes: map[string]string{"auth_kind": "oauth"}}
	reg := registry.GetGlobalRegistry()
	reg.RegisterClient(auth.ID, "codebuddy", []*registry.ModelInfo{{ID: "deepseek-v4.1-flash"}})
	t.Cleanup(func() { reg.UnregisterClient(auth.ID) })
	if _, errRegister := m.Register(context.Background(), auth); errRegister != nil {
		t.Fatalf("register: %v", errRegister)
	}

	m.RegisterExecutor(&mockCustomErrorExecutor{
		identifier: "codebuddy",
		executeFn: func(_ context.Context, _ *Auth, _ cliproxyexecutor.Request, _ cliproxyexecutor.Options) (cliproxyexecutor.Response, error) {
			return cliproxyexecutor.Response{}, customStatusError{code: 400, msg: `{"code":10001,"msg":"invalid parameter"}`}
		},
	})

	if _, errExec := m.Execute(context.Background(), []string{"codebuddy"},
		cliproxyexecutor.Request{Model: "deepseek-v4.1-flash"}, cliproxyexecutor.Options{}); errExec == nil {
		t.Fatal("expected error, got nil")
	}

	state, _ := m.GetByID(auth.ID)
	if !state.NextRetryAfter.IsZero() {
		t.Fatalf("an unmatched 400 must not cool the credential, got nextRetry=%v", state.NextRetryAfter)
	}
}
