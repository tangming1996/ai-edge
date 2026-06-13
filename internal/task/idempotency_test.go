package task_test

import (
	"strings"
	"testing"

	"github.com/edgeai-platform/ai-edge/internal/task"
)

func TestTaskResultKey(t *testing.T) {
	cases := []struct {
		name   string
		taskID string
		nodeID string
		want   string
	}{
		{"normal ids", "task-1", "node-1", "task-1:node-1"},
		{"empty task id", "", "node-1", ":node-1"},
		{"empty node id", "task-1", "", "task-1:"},
		{"both empty", "", "", ":"},
		{"uuid-like", "11111111-2222-3333-4444-555555555555", "n", "11111111-2222-3333-4444-555555555555:n"},
		{"ids with colons", "t:a", "n:b", "t:a:n:b"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := task.TaskResultKey(tc.taskID, tc.nodeID)
			if got != tc.want {
				t.Fatalf("TaskResultKey(%q, %q) = %q, want %q",
					tc.taskID, tc.nodeID, got, tc.want)
			}
		})
	}
}

func TestTaskResultKey_UniquePerPair(t *testing.T) {
	// Different (task,node) pairs must produce different keys.
	a := task.TaskResultKey("t1", "n1")
	b := task.TaskResultKey("t1", "n2")
	c := task.TaskResultKey("t2", "n1")
	if a == b || a == c || b == c {
		t.Fatalf("collision: %q %q %q", a, b, c)
	}
}

func TestTaskResultKey_StableForSameInput(t *testing.T) {
	// The function must be pure: repeated calls with the same args return
	// the same value.
	a := task.TaskResultKey("t1", "n1")
	b := task.TaskResultKey("t1", "n1")
	if a != b {
		t.Fatalf("non-deterministic: %q vs %q", a, b)
	}
	if !strings.Contains(a, "t1") || !strings.Contains(a, "n1") {
		t.Fatalf("missing components in key: %q", a)
	}
}

func TestIdempotencyChecker_NilSafeKeyConstruction(t *testing.T) {
	// We deliberately do not open a DB connection here. TaskResultKey is a
	// pure function and must work even with edge-case inputs.
	for _, in := range []struct{ t, n string }{
		{"t", "n"},
		{"", ""},
		{"multi:colons:in:key", "node-1"},
	} {
		got := task.TaskResultKey(in.t, in.n)
		if got == "" {
			t.Fatalf("TaskResultKey(%q,%q) is empty", in.t, in.n)
		}
	}
}
