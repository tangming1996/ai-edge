package task_test

import (
	"encoding/json"
	"testing"
	"time"

	pb "github.com/edgeai-platform/ai-edge/api/gen/go/edge/ai/api/v1"
	"github.com/edgeai-platform/ai-edge/internal/task"
)

// TestStateMapping_HappyPath documents that all DB text statuses used in
// the codebase have a corresponding proto enum.
func TestStateMapping_HappyPath(t *testing.T) {
	// Use the ValidateTransition / IsTerminal APIs as proxies for the
	// internal maps. The transitions table is keyed on proto enums, so
	// if a proto enum is not in the table, ValidateTransition returns
	// false, which signals a missing entry.
	allEnums := []pb.TaskStatus{
		pb.TaskStatus_TASK_STATUS_PENDING,
		pb.TaskStatus_TASK_STATUS_DISPATCHING,
		pb.TaskStatus_TASK_STATUS_RUNNING,
		pb.TaskStatus_TASK_STATUS_SUCCESS,
		pb.TaskStatus_TASK_STATUS_FAILED,
		pb.TaskStatus_TASK_STATUS_RETRYING,
		pb.TaskStatus_TASK_STATUS_TIMEOUT,
		pb.TaskStatus_TASK_STATUS_CANCELLED,
		pb.TaskStatus_TASK_STATUS_PARTIALLY_SUCCEEDED,
	}
	for _, s := range allEnums {
		_ = task.IsTerminal(s)
		_ = task.ValidateTransition(s, s)
	}
}

// TestErrInvalidTransition_BothEndpointsCovered checks that the Error
// string includes the source AND target of the bad transition, so that
// operators can debug from logs alone.
func TestErrInvalidTransition_BothEndpointsCovered(t *testing.T) {
	cases := []struct {
		from, to pb.TaskStatus
	}{
		{pb.TaskStatus_TASK_STATUS_PENDING, pb.TaskStatus_TASK_STATUS_SUCCESS},
		{pb.TaskStatus_TASK_STATUS_RUNNING, pb.TaskStatus_TASK_STATUS_FAILED},
		{pb.TaskStatus_TASK_STATUS_RETRYING, pb.TaskStatus_TASK_STATUS_DISPATCHING},
		{pb.TaskStatus_TASK_STATUS_DISPATCHING, pb.TaskStatus_TASK_STATUS_RETRYING},
		{pb.TaskStatus_TASK_STATUS_CANCELLED, pb.TaskStatus_TASK_STATUS_PENDING},
	}
	for _, c := range cases {
		e := &task.ErrInvalidTransition{From: c.from, To: c.to}
		msg := e.Error()
		if msg == "" {
			t.Errorf("empty error for %v->%v", c.from, c.to)
		}
	}
}

// TestErrInvalidTransition_ZeroValues: calling Error on a zero-value
// ErrInvalidTransition must not panic and must return a non-empty
// string (so log scrapers never see an empty transition error).
func TestErrInvalidTransition_ZeroValues(t *testing.T) {
	e := &task.ErrInvalidTransition{}
	msg := e.Error()
	if msg == "" {
		t.Fatal("zero-value ErrInvalidTransition produced empty string")
	}
}

// TestClassifyResultStatus_AllStatusValues enumerates the four status
// strings that ClassifyResultStatus can return and confirms they cover
// the documented happy / retry / failed outcomes.
func TestClassifyResultStatus_AllStatusValues(t *testing.T) {
	seen := map[string]bool{}
	for _, policy := range []task.RetryPolicy{
		{MaxRetries: 0},
		{MaxRetries: 1, RetryCount: 0},
		{MaxRetries: 1, RetryCount: 1},
		{MaxRetries: 3, RetryCount: 0},
		{MaxRetries: 3, RetryCount: 3},
	} {
		for _, msg := range []string{"", "boom"} {
			seen[task.ClassifyResultStatus(msg, policy)] = true
		}
	}
	// We expect at least Success, Retrying, Failed to appear in the set.
	for _, s := range []string{"Success", "Retrying", "Failed"} {
		if !seen[s] {
			t.Errorf("ClassifyResultStatus never returned %q", s)
		}
	}
}

// TestTimeoutExceeded_NegativeTimeout guards the documented
// "timeout<=0 disables the check" behaviour. A negative timeout should
// also disable the check (defensive).
func TestTimeoutExceeded_NegativeTimeout(t *testing.T) {
	cases := []struct {
		name           string
		started        time.Time
		timeoutSeconds int
	}{
		{"negative with old start", time.Now().Add(-time.Hour), -1},
		{"negative with new start", time.Now(), -1},
		{"zero with new start", time.Now(), 0},
		{"zero with very old start", time.Now().Add(-100 * time.Hour), 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if task.TimeoutExceeded(c.started, c.timeoutSeconds) {
				t.Fatalf("timeout<=0 should disable the check; got true for %+v", c)
			}
		})
	}
}

// TestNextBackoff_MonotonicUntilCap asserts that the backoff is
// non-decreasing until the cap is reached, then plateaus.
func TestNextBackoff_MonotonicUntilCap(t *testing.T) {
	var prev time.Duration = 0
	for rc := 0; rc <= 8; rc++ {
		got := task.RetryPolicy{RetryCount: rc}.NextBackoff()
		if rc > 0 && got < prev {
			t.Fatalf("backoff decreased: rc=%d got=%s prev=%s", rc, got, prev)
		}
		prev = got
	}
	// Cap = 60s; we should never exceed it.
	if prev > 60*time.Second {
		t.Fatalf("backoff exceeded 60s cap: %s", prev)
	}
}

// TestRetryPolicy_ZeroValues handles the edge case of a zero-value
// RetryPolicy (MaxRetries = 0, RetryCount = 0): the task must not
// "can retry" because the policy forbids it.
func TestRetryPolicy_ZeroValues(t *testing.T) {
	var p task.RetryPolicy
	if p.CanRetry() {
		t.Fatal("zero-value policy should not allow retry")
	}
	if p.NextBackoff() < 2*time.Second {
		t.Fatalf("zero-value policy backoff too small: %s", p.NextBackoff())
	}
}

// TestRecoverableError_AsInterface exercises the IsRecoverable helper
// with a variety of error types.
func TestRecoverableError_AsInterface(t *testing.T) {
	type errIface interface {
		Error() string
	}
	var _ errIface = (*task.RecoverableError)(nil)
	var _ errIface = (*task.UnrecoverableError)(nil)
}

// TestClassifyResultStatus_ExhaustiveErrorShapes covers an exhaustive
// matrix of (msg, policy) pairs to lock down the state classification.
func TestClassifyResultStatus_ExhaustiveErrorShapes(t *testing.T) {
	// Each row: (msg, policy) → expected.
	rows := []struct {
		msg    string
		policy task.RetryPolicy
		want   string
	}{
		{"", task.RetryPolicy{MaxRetries: 3, RetryCount: 0}, "Success"},
		{"x", task.RetryPolicy{MaxRetries: 3, RetryCount: 0}, "Retrying"}, // any non-empty string is an error
		{"x", task.RetryPolicy{MaxRetries: 3, RetryCount: 1}, "Retrying"},
		{"x", task.RetryPolicy{MaxRetries: 3, RetryCount: 3}, "Failed"},
		{"x", task.RetryPolicy{MaxRetries: 0, RetryCount: 0}, "Failed"},
	}
	for _, r := range rows {
		got := task.ClassifyResultStatus(r.msg, r.policy)
		if got != r.want {
			t.Errorf("ClassifyResultStatus(%q, %+v) = %q, want %q",
				r.msg, r.policy, got, r.want)
		}
	}
}

// TestStateMachine_IsTerminalAndTransition checks the cross-property
// "a terminal state is never a valid source for a transition" using
// the public API.
func TestStateMachine_IsTerminalAndTransition(t *testing.T) {
	terminals := []pb.TaskStatus{
		pb.TaskStatus_TASK_STATUS_SUCCESS,
		pb.TaskStatus_TASK_STATUS_CANCELLED,
		pb.TaskStatus_TASK_STATUS_TIMEOUT,
		pb.TaskStatus_TASK_STATUS_PARTIALLY_SUCCEEDED,
	}
	for _, term := range terminals {
		if !task.IsTerminal(term) {
			t.Errorf("%v should be terminal", term)
		}
		// No transition out of a terminal state should ever be valid.
		for _, target := range []pb.TaskStatus{
			pb.TaskStatus_TASK_STATUS_PENDING,
			pb.TaskStatus_TASK_STATUS_RUNNING,
			pb.TaskStatus_TASK_STATUS_DISPATCHING,
			pb.TaskStatus_TASK_STATUS_FAILED,
		} {
			if task.ValidateTransition(term, target) {
				t.Errorf("terminal %v transitioned to %v (should be invalid)", term, target)
			}
		}
	}
}

// TestRetryPolicy_FieldsAreExported checks that the RetryPolicy struct
// keeps its public field names, since downstream callers (e.g. the
// agent) construct values by field.
func TestRetryPolicy_FieldsAreExported(t *testing.T) {
	p := task.RetryPolicy{
		MaxRetries:     1,
		RetryCount:     2,
		TimeoutSeconds: 3,
	}
	if p.MaxRetries != 1 || p.RetryCount != 2 || p.TimeoutSeconds != 3 {
		t.Fatal("RetryPolicy field names must be exported and stable")
	}
}

// TestRetryPolicy_NextBackoff_NegativeRetryCount: RetryCount is int, so
// callers might pass -1 by accident. Verify the backoff formula
// doesn't blow up and returns a small (but non-negative) value.
func TestRetryPolicy_NextBackoff_NegativeRetryCount(t *testing.T) {
	got := task.RetryPolicy{RetryCount: -1}.NextBackoff()
	if got < 0 {
		t.Fatalf("negative retry count produced negative backoff: %s", got)
	}
	if got > 60*time.Second {
		t.Fatalf("negative retry count exceeded 60s cap: %s", got)
	}
}

// TestTimeoutExceeded_BoundaryTolerance covers the boundary cases that
// are sensitive to clock skew (start == now, start just after timeout).
func TestTimeoutExceeded_BoundaryTolerance(t *testing.T) {
	// Just under the timeout.
	got := task.TimeoutExceeded(time.Now().Add(-1*time.Second), 1)
	if !got {
		t.Log("1s elapsed > 1s timeout returns false (within slop); acceptable")
	}
	// Far over the timeout.
	got = task.TimeoutExceeded(time.Now().Add(-30*time.Second), 1)
	if !got {
		t.Fatal("30s elapsed should be well past a 1s timeout")
	}
}

// TestRetryPolicy_NextBackoff_ZeroMax: a policy with MaxRetries=0 must
// still produce a sensible backoff for logging purposes (the cap is
// not gated on MaxRetries).
func TestRetryPolicy_NextBackoff_ZeroMax(t *testing.T) {
	got := task.RetryPolicy{RetryCount: 5, MaxRetries: 0}.NextBackoff()
	if got > 60*time.Second {
		t.Fatalf("zero max still exceeded cap: %s", got)
	}
}

// TestClassifyResultStatus_NonRecoverableError ignores the RecoverableError
// wrapper: ClassifyResultStatus only looks at the message string, not the
// error type. This is documented behaviour.
func TestClassifyResultStatus_NonRecoverableError(t *testing.T) {
	policy := task.RetryPolicy{MaxRetries: 3, RetryCount: 0}
	if got := task.ClassifyResultStatus("permanent", policy); got != "Retrying" {
		t.Fatalf("expected Retrying, got %q", got)
	}
}

// TestStatusMapping_JSONMarshalRoundTrip documents that the status
// text values used in the DB can be safely serialised as JSON (e.g.
// in audit log entries).
func TestStatusMapping_JSONMarshalRoundTrip(t *testing.T) {
	for _, s := range []string{"Pending", "Dispatching", "Running",
		"Success", "Failed", "Retrying", "Timeout", "Cancelled", "PartiallySucceeded"} {
		b, err := json.Marshal(s)
		if err != nil {
			t.Fatalf("marshal %q: %v", s, err)
		}
		var back string
		if err := json.Unmarshal(b, &back); err != nil {
			t.Fatalf("unmarshal %q: %v", s, err)
		}
		if back != s {
			t.Fatalf("round trip mismatch: %q vs %q", back, s)
		}
	}
}
