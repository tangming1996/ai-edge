package task_test

import (
	"errors"
	"testing"
	"time"

	"github.com/edgeai-platform/ai-edge/internal/task"
)

func TestRetryPolicy_CanRetry(t *testing.T) {
	cases := []struct {
		name   string
		policy task.RetryPolicy
		want   bool
	}{
		{"max=3 retry=0 can", task.RetryPolicy{MaxRetries: 3, RetryCount: 0}, true},
		{"max=3 retry=1 can", task.RetryPolicy{MaxRetries: 3, RetryCount: 1}, true},
		{"max=3 retry=2 can", task.RetryPolicy{MaxRetries: 3, RetryCount: 2}, true},
		{"max=3 retry=3 cannot", task.RetryPolicy{MaxRetries: 3, RetryCount: 3}, false},
		{"max=3 retry=99 cannot", task.RetryPolicy{MaxRetries: 3, RetryCount: 99}, false},
		{"max=0 retry=0 cannot", task.RetryPolicy{MaxRetries: 0, RetryCount: 0}, false},
		{"max=1 retry=0 can", task.RetryPolicy{MaxRetries: 1, RetryCount: 0}, true},
		{"max=1 retry=1 cannot", task.RetryPolicy{MaxRetries: 1, RetryCount: 1}, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.policy.CanRetry()
			if got != tc.want {
				t.Fatalf("CanRetry() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestRetryPolicy_NextBackoff(t *testing.T) {
	// base = 2s, max = 60s. The formula is min(2s * 2^retry, 60s).
	cases := []struct {
		name       string
		retryCount int
		wantFloor  time.Duration
		wantCeil   time.Duration
	}{
		{"retry=0 -> 2s", 0, 2 * time.Second, 2 * time.Second},
		{"retry=1 -> 4s", 1, 4 * time.Second, 4 * time.Second},
		{"retry=2 -> 8s", 2, 8 * time.Second, 8 * time.Second},
		{"retry=3 -> 16s", 3, 16 * time.Second, 16 * time.Second},
		{"retry=4 -> 32s", 4, 32 * time.Second, 32 * time.Second},
		{"retry=5 -> 60s (capped)", 5, 60 * time.Second, 60 * time.Second},
		{"retry=6 -> 60s (capped)", 6, 60 * time.Second, 60 * time.Second},
		{"retry=20 -> 60s (capped)", 20, 60 * time.Second, 60 * time.Second},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := task.RetryPolicy{RetryCount: tc.retryCount}.NextBackoff()
			if got < tc.wantFloor || got > tc.wantCeil {
				t.Fatalf("NextBackoff(retry=%d) = %s, want in [%s, %s]",
					tc.retryCount, got, tc.wantFloor, tc.wantCeil)
			}
		})
	}
}

func TestRetryPolicy_NextBackoff_NeverExceedsCap(t *testing.T) {
	// Sweep a wide range of retry counts; the result must never exceed
	// the documented 60s cap.
	for rc := 0; rc < 50; rc++ {
		got := task.RetryPolicy{RetryCount: rc}.NextBackoff()
		if got > 60*time.Second {
			t.Fatalf("retry=%d -> %s exceeds 60s cap", rc, got)
		}
	}
}

func TestRecoverableError(t *testing.T) {
	inner := errors.New("kaboom")
	rec := &task.RecoverableError{Err: inner}
	if rec.Error() == "" {
		t.Fatal("empty error message")
	}
	if !errors.Is(rec, inner) {
		t.Fatal("Unwrap should expose inner error for errors.Is")
	}
	if !task.IsRecoverable(rec) {
		t.Fatal("IsRecoverable should return true for RecoverableError")
	}
}

func TestUnrecoverableError(t *testing.T) {
	inner := errors.New("nope")
	unrec := &task.UnrecoverableError{Err: inner}
	if unrec.Error() == "" {
		t.Fatal("empty error message")
	}
	// Unwrap exists on UnrecoverableError, so errors.Is should match.
	if !errors.Is(unrec, inner) {
		t.Fatal("errors.Is should match wrapped inner")
	}
	if task.IsRecoverable(unrec) {
		t.Fatal("IsRecoverable must return false for UnrecoverableError")
	}
	if task.IsRecoverable(inner) {
		t.Fatal("IsRecoverable must return false for a plain error")
	}
	if task.IsRecoverable(nil) {
		t.Fatal("IsRecoverable(nil) must be false")
	}
}

func TestIsRecoverable(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"plain error", errors.New("plain"), false},
		{"RecoverableError", &task.RecoverableError{Err: errors.New("x")}, true},
		{"UnrecoverableError", &task.UnrecoverableError{Err: errors.New("x")}, false},
		{"string-backed fake error", errString("fake"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := task.IsRecoverable(tc.err)
			if got != tc.want {
				t.Fatalf("IsRecoverable(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

func TestClassifyResultStatus(t *testing.T) {
	cases := []struct {
		name   string
		errMsg string
		policy task.RetryPolicy
		want   string
	}{
		{"no error -> Success", "", task.RetryPolicy{MaxRetries: 3}, "Success"},
		{"error with budget -> Retrying", "boom", task.RetryPolicy{MaxRetries: 3, RetryCount: 0}, "Retrying"},
		{"error no budget -> Failed", "boom", task.RetryPolicy{MaxRetries: 3, RetryCount: 3}, "Failed"},
		{"error no retries configured -> Failed", "boom", task.RetryPolicy{MaxRetries: 0}, "Failed"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := task.ClassifyResultStatus(tc.errMsg, tc.policy)
			if got != tc.want {
				t.Fatalf("ClassifyResultStatus(%q, %+v) = %q, want %q",
					tc.errMsg, tc.policy, got, tc.want)
			}
		})
	}
}

func TestTimeoutExceeded(t *testing.T) {
	cases := []struct {
		name           string
		started        time.Time
		timeoutSeconds int
		want           bool
	}{
		{
			name:           "timeout<=0 disables check",
			started:        time.Now().Add(-1 * time.Hour),
			timeoutSeconds: 0,
			want:           false,
		},
		{
			name:           "negative timeout disables check",
			started:        time.Now().Add(-1 * time.Hour),
			timeoutSeconds: -1,
			want:           false,
		},
		{
			name:           "started just now with 10s timeout",
			started:        time.Now(),
			timeoutSeconds: 10,
			want:           false,
		},
		{
			name:           "started 1h ago with 10s timeout",
			started:        time.Now().Add(-1 * time.Hour),
			timeoutSeconds: 10,
			want:           true,
		},
		{
			name:           "started exactly at timeout (within slop)",
			started:        time.Now().Add(-2 * time.Second),
			timeoutSeconds: 1,
			want:           true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := task.TimeoutExceeded(tc.started, tc.timeoutSeconds)
			if got != tc.want {
				t.Fatalf("TimeoutExceeded(%v, %d) = %v, want %v",
					tc.started, tc.timeoutSeconds, got, tc.want)
			}
		})
	}
}

// errString is a small error type so the table above can mix error
// types and pre-built sentinels without import cycles.
type errString string

func (e errString) Error() string { return string(e) }
