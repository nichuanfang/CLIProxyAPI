package helps

import (
	"testing"
	"time"
)

func TestCodeBuddyRetryAfter(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
		want   time.Duration
		ok     bool
	}{
		{name: "rate limit", status: 429, body: `{"retry_policy":{"retry":true,"retry_after":30}}`, want: 30 * time.Second, ok: true},
		{name: "service error", status: 500, body: `{"retry_policy":{"retry":true,"retry_after":10}}`, want: 10 * time.Second, ok: true},
		{name: "zero", status: 503, body: `{"retry_policy":{"retry":true,"retry_after":0}}`, ok: true},
		{name: "disabled", status: 429, body: `{"retry_policy":{"retry":false,"retry_after":30}}`},
		{name: "missing retry flag", status: 429, body: `{"retry_policy":{"retry_after":30}}`},
		{name: "missing delay", status: 429, body: `{"retry_policy":{"retry":true}}`},
		{name: "negative", status: 429, body: `{"retry_policy":{"retry":true,"retry_after":-1}}`},
		{name: "fractional", status: 429, body: `{"retry_policy":{"retry":true,"retry_after":0.5}}`},
		{name: "string", status: 429, body: `{"retry_policy":{"retry":true,"retry_after":"30"}}`},
		{name: "overflow", status: 429, body: `{"retry_policy":{"retry":true,"retry_after":9223372037}}`},
		{name: "request fault", status: 400, body: `{"retry_policy":{"retry":true,"retry_after":30}}`},
		{name: "unauthorized", status: 401, body: `{"retry_policy":{"retry":true,"retry_after":30}}`},
		{name: "invalid JSON", status: 429, body: `{"retry_policy":{"retry":true,"retry_after":30}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := CodeBuddyRetryAfter(tc.status, []byte(tc.body))
			if (got != nil) != tc.ok || (got != nil && *got != tc.want) {
				t.Fatalf("retry delay = %v, want duration %v, present %v", got, tc.want, tc.ok)
			}
		})
	}
}
