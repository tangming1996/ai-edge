package task_test

import (
	"errors"
	"testing"

	pb "github.com/edgeai-platform/ai-edge/api/gen/go/edge/ai/api/v1"
	"github.com/edgeai-platform/ai-edge/internal/task"
)

func TestValidateTransition(t *testing.T) {
	cases := []struct {
		name string
		from pb.TaskStatus
		to   pb.TaskStatus
		want bool
	}{
		// Allowed transitions.
		{"pending -> dispatching", pb.TaskStatus_TASK_STATUS_PENDING, pb.TaskStatus_TASK_STATUS_DISPATCHING, true},
		{"pending -> cancelled", pb.TaskStatus_TASK_STATUS_PENDING, pb.TaskStatus_TASK_STATUS_CANCELLED, true},
		{"dispatching -> running", pb.TaskStatus_TASK_STATUS_DISPATCHING, pb.TaskStatus_TASK_STATUS_RUNNING, true},
		{"dispatching -> timeout", pb.TaskStatus_TASK_STATUS_DISPATCHING, pb.TaskStatus_TASK_STATUS_TIMEOUT, true},
		{"running -> success", pb.TaskStatus_TASK_STATUS_RUNNING, pb.TaskStatus_TASK_STATUS_SUCCESS, true},
		{"running -> failed", pb.TaskStatus_TASK_STATUS_RUNNING, pb.TaskStatus_TASK_STATUS_FAILED, true},
		{"running -> partially_succeeded", pb.TaskStatus_TASK_STATUS_RUNNING, pb.TaskStatus_TASK_STATUS_PARTIALLY_SUCCEEDED, true},
		{"failed -> retrying", pb.TaskStatus_TASK_STATUS_FAILED, pb.TaskStatus_TASK_STATUS_RETRYING, true},
		{"retrying -> running", pb.TaskStatus_TASK_STATUS_RETRYING, pb.TaskStatus_TASK_STATUS_RUNNING, true},

		// Same-state transitions must be rejected (transitions are moves).
		{"pending -> pending", pb.TaskStatus_TASK_STATUS_PENDING, pb.TaskStatus_TASK_STATUS_PENDING, false},
		{"running -> running", pb.TaskStatus_TASK_STATUS_RUNNING, pb.TaskStatus_TASK_STATUS_RUNNING, false},
		{"success -> success", pb.TaskStatus_TASK_STATUS_SUCCESS, pb.TaskStatus_TASK_STATUS_SUCCESS, false},

		// Invalid transitions.
		{"pending -> running", pb.TaskStatus_TASK_STATUS_PENDING, pb.TaskStatus_TASK_STATUS_RUNNING, false},
		{"pending -> success", pb.TaskStatus_TASK_STATUS_PENDING, pb.TaskStatus_TASK_STATUS_SUCCESS, false},
		{"running -> pending", pb.TaskStatus_TASK_STATUS_RUNNING, pb.TaskStatus_TASK_STATUS_PENDING, false},
		{"running -> dispatching", pb.TaskStatus_TASK_STATUS_RUNNING, pb.TaskStatus_TASK_STATUS_DISPATCHING, false},
		{"failed -> success", pb.TaskStatus_TASK_STATUS_FAILED, pb.TaskStatus_TASK_STATUS_SUCCESS, false},
		{"failed -> running", pb.TaskStatus_TASK_STATUS_FAILED, pb.TaskStatus_TASK_STATUS_RUNNING, false},
		{"retrying -> failed", pb.TaskStatus_TASK_STATUS_RETRYING, pb.TaskStatus_TASK_STATUS_FAILED, false},
		{"dispatching -> failed", pb.TaskStatus_TASK_STATUS_DISPATCHING, pb.TaskStatus_TASK_STATUS_FAILED, false},

		// Terminal-state transitions: must not allow further movement.
		{"success -> running", pb.TaskStatus_TASK_STATUS_SUCCESS, pb.TaskStatus_TASK_STATUS_RUNNING, false},
		{"success -> cancelled", pb.TaskStatus_TASK_STATUS_SUCCESS, pb.TaskStatus_TASK_STATUS_CANCELLED, false},
		{"cancelled -> running", pb.TaskStatus_TASK_STATUS_CANCELLED, pb.TaskStatus_TASK_STATUS_RUNNING, false},
		{"timeout -> running", pb.TaskStatus_TASK_STATUS_TIMEOUT, pb.TaskStatus_TASK_STATUS_RUNNING, false},
		{"timeout -> failed", pb.TaskStatus_TASK_STATUS_TIMEOUT, pb.TaskStatus_TASK_STATUS_FAILED, false},
		{"partially_succeeded -> running", pb.TaskStatus_TASK_STATUS_PARTIALLY_SUCCEEDED, pb.TaskStatus_TASK_STATUS_RUNNING, false},
		{"partially_succeeded -> cancelled", pb.TaskStatus_TASK_STATUS_PARTIALLY_SUCCEEDED, pb.TaskStatus_TASK_STATUS_CANCELLED, false},

		// Undefined source state.
		{"undefined -> pending", pb.TaskStatus_TASK_STATUS_UNSPECIFIED, pb.TaskStatus_TASK_STATUS_PENDING, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := task.ValidateTransition(tc.from, tc.to)
			if got != tc.want {
				t.Fatalf("ValidateTransition(%v, %v) = %v, want %v", tc.from, tc.to, got, tc.want)
			}
		})
	}
}

func TestIsTerminal(t *testing.T) {
	cases := []struct {
		name   string
		status pb.TaskStatus
		want   bool
	}{
		// Terminal states.
		{"success is terminal", pb.TaskStatus_TASK_STATUS_SUCCESS, true},
		{"cancelled is terminal", pb.TaskStatus_TASK_STATUS_CANCELLED, true},
		{"timeout is terminal", pb.TaskStatus_TASK_STATUS_TIMEOUT, true},
		{"partially_succeeded is terminal", pb.TaskStatus_TASK_STATUS_PARTIALLY_SUCCEEDED, true},

		// Non-terminal states.
		{"pending is not terminal", pb.TaskStatus_TASK_STATUS_PENDING, false},
		{"dispatching is not terminal", pb.TaskStatus_TASK_STATUS_DISPATCHING, false},
		{"running is not terminal", pb.TaskStatus_TASK_STATUS_RUNNING, false},
		{"failed is not terminal", pb.TaskStatus_TASK_STATUS_FAILED, false},
		{"retrying is not terminal", pb.TaskStatus_TASK_STATUS_RETRYING, false},

		// Unspecified / unknown.
		{"unspecified is not terminal", pb.TaskStatus_TASK_STATUS_UNSPECIFIED, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := task.IsTerminal(tc.status)
			if got != tc.want {
				t.Fatalf("IsTerminal(%v) = %v, want %v", tc.status, got, tc.want)
			}
		})
	}
}

func TestErrInvalidTransition_ErrorString(t *testing.T) {
	e := &task.ErrInvalidTransition{
		From: pb.TaskStatus_TASK_STATUS_PENDING,
		To:   pb.TaskStatus_TASK_STATUS_SUCCESS,
	}
	got := e.Error()
	if got == "" {
		t.Fatal("empty error string")
	}
	// Both endpoints should appear in the message for debuggability.
	if !contains(got, "Pending") {
		t.Errorf("error message missing source: %s", got)
	}
	if !contains(got, "Success") {
		t.Errorf("error message missing target: %s", got)
	}
}

func TestStatusMappings_RoundTrip(t *testing.T) {
	// Sanity: every status in protoToStatus has a proto enum that maps back.
	// We exercise this by calling ValidateTransition with each pair.
	statuses := []pb.TaskStatus{
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
	for _, s := range statuses {
		_ = task.IsTerminal(s)
		_ = task.ValidateTransition(s, s)
	}
}

// contains is a small helper to keep table tests readable without pulling
// in the strings package for a single substring check.
func contains(s, sub string) bool {
	if sub == "" {
		return true
	}
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// Ensure errors can be used with errors.As.
func TestErrInvalidTransition_ErrorsAs(t *testing.T) {
	orig := &task.ErrInvalidTransition{
		From: pb.TaskStatus_TASK_STATUS_PENDING,
		To:   pb.TaskStatus_TASK_STATUS_SUCCESS,
	}
	wrapped := error(orig)
	var target *task.ErrInvalidTransition
	if !errors.As(wrapped, &target) {
		t.Fatal("errors.As should find ErrInvalidTransition")
	}
	if target.From != pb.TaskStatus_TASK_STATUS_PENDING {
		t.Fatalf("From lost: %v", target.From)
	}
}
