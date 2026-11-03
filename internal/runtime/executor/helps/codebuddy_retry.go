package helps

import (
	"net/http"
	"strconv"
	"time"

	"github.com/tidwall/gjson"
)

// CodeBuddyRetryAfter reads the enabled retry policy on a retryable rejection.
// Callers use it as a fallback when no standard retry hint is available.
func CodeBuddyRetryAfter(status int, body []byte) *time.Duration {
	if (status != http.StatusTooManyRequests && (status < 500 || status > 599)) || !gjson.ValidBytes(body) {
		return nil
	}
	policy := gjson.GetBytes(body, "retry_policy")
	if policy.Get("retry").Type != gjson.True {
		return nil
	}
	value := policy.Get("retry_after")
	if value.Type != gjson.Number {
		return nil
	}
	seconds, errParse := strconv.ParseInt(value.Raw, 10, 64)
	if errParse != nil || seconds < 0 || seconds > int64((1<<63-1)/time.Second) {
		return nil
	}
	delay := time.Duration(seconds) * time.Second
	return &delay
}
