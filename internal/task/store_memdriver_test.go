//go:build !integration

package task_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"database/sql/driver"

	"github.com/edgeai-platform/ai-edge/internal/store"
	"github.com/edgeai-platform/ai-edge/internal/task"
)

// rowOnly is a convenience constructor for the common case of a single
// driver.Value row used as a mem-driver fixture.
func rowOnly(v ...driver.Value) []driver.Value { return v }

// nowFixed returns a stable timestamp so that mem-driver fixtures
// compare cleanly.
func nowFixed() time.Time {
	return time.Date(2026, 6, 13, 12, 0, 0, 0, time.UTC)
}

func newMemStore(t *testing.T) (*store.DB, *task.Store) {
	t.Helper()
	store.ResetMemDB()
	db := store.NewMemStore()
	return db, task.NewStore(db)
}

// newMemTx returns a real (mem-driver backed) *store.Tx. The
// *store.Tx{Tx: nil} zero value is unsafe because *sql.Tx methods
// panic on a nil receiver; tests must always go through this helper.
func newMemTx(t *testing.T) *store.Tx {
	t.Helper()
	sqlTx, err := store.NewMemDB().BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	return &store.Tx{Tx: sqlTx}
}

func TestStore_NewStore(t *testing.T) {
	_, s := newMemStore(t)
	if s == nil {
		t.Fatal("nil store")
	}
}

func TestStore_CreateTask_HappyPath(t *testing.T) {
	_, s := newMemStore(t)
	store.SetRowForQuery("INSERT INTO tasks", rowOnly("id-1", nowFixed(), nowFixed()))
	row := &task.TaskRow{
		Scope: "Region", Type: "deploy", Status: "Pending",
		DispatchStatus: "Unclaimed", Payload: []byte(`{}`),
		MaxRetries: 3, TimeoutSeconds: 60, CreatedBy: "alice",
	}
	err := s.CreateTask(context.Background(), newMemTx(t), row)
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if row.ID != "id-1" {
		t.Errorf("ID = %q", row.ID)
	}
}

func TestStore_CreateTask_QueryError(t *testing.T) {
	_, s := newMemStore(t)
	store.SetErrorForQuery("INSERT INTO tasks", errBoom)
	err := s.CreateTask(context.Background(), newMemTx(t), &task.TaskRow{Scope: "Region"})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestStore_GetTask_HappyPath(t *testing.T) {
	_, s := newMemStore(t)
	now := nowFixed()
	store.SetRowForQuery("FROM tasks WHERE id = $1", rowOnly(
		"id-1", nil, "Region", "deploy", "Pending",
		nil, nil, []byte(`{}`), []byte(`{}`),
		"Unclaimed", nil, now,
		3, 0, 60, nil, "alice", now, now,
	))
	row, err := s.GetTask(context.Background(), "id-1")
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if row.ID != "id-1" {
		t.Errorf("ID = %q", row.ID)
	}
	if row.Type != "deploy" {
		t.Errorf("Type = %q", row.Type)
	}
}

func TestStore_GetTask_NotFound(t *testing.T) {
	_, s := newMemStore(t)
	store.SetNoRowsForQuery("FROM tasks WHERE id = $1")
	_, err := s.GetTask(context.Background(), "missing")
	if err != store.ErrNotFound {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestStore_GetTask_QueryError(t *testing.T) {
	_, s := newMemStore(t)
	store.SetErrorForQuery("FROM tasks WHERE id = $1", errBoom)
	_, err := s.GetTask(context.Background(), "x")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestStore_ListTasks_Empty(t *testing.T) {
	_, s := newMemStore(t)
	rows, total, err := s.ListTasks(context.Background(), task.ListFilter{})
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	if total != 0 {
		t.Errorf("total = %d, want 0", total)
	}
	if len(rows) != 0 {
		t.Errorf("rows = %d, want 0", len(rows))
	}
}

func TestStore_ListTasks_CountError(t *testing.T) {
	_, s := newMemStore(t)
	store.SetErrorForQuery("SELECT COUNT(*) FROM tasks", errBoom)
	_, _, err := s.ListTasks(context.Background(), task.ListFilter{})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestStore_ListTasks_QueryError(t *testing.T) {
	_, s := newMemStore(t)
	store.SetErrorForQuery("FROM tasks WHERE 1=1", errBoom)
	_, _, err := s.ListTasks(context.Background(), task.ListFilter{})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestStore_ListTasks_WithFilters(t *testing.T) {
	_, s := newMemStore(t)
	now := nowFixed()
	store.SetRowForQuery("SELECT COUNT(*) FROM tasks", rowOnly(int64(1)))
	store.SetRowForQuery("ORDER BY created_at DESC", rowOnly(
		"id-1", nil, "Region", "deploy", "Pending",
		nil, nil, []byte(`{}`), []byte(`{}`),
		"Unclaimed", nil, now,
		3, 0, 60, nil, "alice", now, now,
	))
	rows, total, err := s.ListTasks(context.Background(), task.ListFilter{
		TargetGatewayID: "gw-1", TargetNodeID: "n-1",
		Status: "Pending", Type: "deploy",
		Limit: 10, Offset: 0,
	})
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	if total != 1 {
		t.Errorf("total = %d", total)
	}
	if len(rows) != 1 {
		t.Errorf("rows = %d", len(rows))
	}
}

func TestStore_UpdateStatus_HappyPath(t *testing.T) {
	_, s := newMemStore(t)
	store.SetRowsAffectedForQuery("UPDATE tasks\n\t\tSET status = $1", 1)
	err := s.UpdateStatus(context.Background(), newMemTx(t),
		"id-1", "Pending", "Running", "alice", []byte(`{"x":1}`))
	if err != nil {
		t.Fatalf("UpdateStatus: %v", err)
	}
}

func TestStore_UpdateStatus_Precondition(t *testing.T) {
	_, s := newMemStore(t)
	store.SetRowsAffectedForQuery("UPDATE tasks\n\t\tSET status = $1", 0)
	err := s.UpdateStatus(context.Background(), newMemTx(t),
		"id-1", "Pending", "Running", "alice", nil)
	if err != store.ErrPrecondition {
		t.Fatalf("err = %v, want ErrPrecondition", err)
	}
}

func TestStore_UpdateStatus_QueryError(t *testing.T) {
	_, s := newMemStore(t)
	store.SetErrorForQuery("UPDATE tasks\n\t\tSET status = $1", errBoom)
	err := s.UpdateStatus(context.Background(), newMemTx(t),
		"id-1", "Pending", "Running", "alice", nil)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestStore_AtomicClaim_Won(t *testing.T) {
	_, s := newMemStore(t)
	store.SetRowsAffectedForQuery("UPDATE tasks\n\t\tSET dispatch_status = 'Claimed'", 1)
	won, err := s.AtomicClaim(context.Background(), newMemTx(t), "id-1", "instance-1", time.Minute)
	if err != nil {
		t.Fatalf("AtomicClaim: %v", err)
	}
	if !won {
		t.Error("expected won=true")
	}
}

func TestStore_AtomicClaim_Lost(t *testing.T) {
	_, s := newMemStore(t)
	store.SetRowsAffectedForQuery("UPDATE tasks\n\t\tSET dispatch_status = 'Claimed'", 0)
	won, err := s.AtomicClaim(context.Background(), newMemTx(t), "id-1", "instance-1", time.Minute)
	if err != nil {
		t.Fatalf("AtomicClaim: %v", err)
	}
	if won {
		t.Error("expected won=false")
	}
}

func TestStore_AtomicClaim_Error(t *testing.T) {
	_, s := newMemStore(t)
	store.SetErrorForQuery("UPDATE tasks\n\t\tSET dispatch_status = 'Claimed'", errBoom)
	_, err := s.AtomicClaim(context.Background(), newMemTx(t), "id-1", "instance-1", time.Minute)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestStore_MarkDelivered_HappyPath(t *testing.T) {
	_, s := newMemStore(t)
	store.SetRowsAffectedForQuery("UPDATE tasks\n\t\tSET dispatch_status = 'Delivered'", 1)
	if err := s.MarkDelivered(context.Background(), newMemTx(t), "id-1"); err != nil {
		t.Fatalf("MarkDelivered: %v", err)
	}
}

func TestStore_MarkDelivered_Precondition(t *testing.T) {
	_, s := newMemStore(t)
	store.SetRowsAffectedForQuery("UPDATE tasks\n\t\tSET dispatch_status = 'Delivered'", 0)
	if err := s.MarkDelivered(context.Background(), newMemTx(t), "id-1"); err != store.ErrPrecondition {
		t.Fatalf("err = %v, want ErrPrecondition", err)
	}
}

func TestStore_MarkDelivered_Error(t *testing.T) {
	_, s := newMemStore(t)
	store.SetErrorForQuery("UPDATE tasks\n\t\tSET dispatch_status = 'Delivered'", errBoom)
	if err := s.MarkDelivered(context.Background(), newMemTx(t), "id-1"); err == nil {
		t.Fatal("expected error")
	}
}

func TestStore_UpdateResult(t *testing.T) {
	_, s := newMemStore(t)
	store.SetRowsAffectedForQuery("UPDATE tasks\n\t\tSET status = $1", 1)
	if err := s.UpdateResult(context.Background(), newMemTx(t), "id-1", "Success", []byte(`{"ok":true}`), 0); err != nil {
		t.Fatalf("UpdateResult: %v", err)
	}
}

func TestStore_UpdateResult_Error(t *testing.T) {
	_, s := newMemStore(t)
	store.SetErrorForQuery("UPDATE tasks\n\t\tSET status = $1", errBoom)
	if err := s.UpdateResult(context.Background(), newMemTx(t), "id-1", "Success", nil, 0); err == nil {
		t.Fatal("expected error")
	}
}

func TestStore_CreateTaskRun_HappyPath(t *testing.T) {
	_, s := newMemStore(t)
	now := nowFixed()
	store.SetRowForQuery("INSERT INTO task_runs", rowOnly("run-1", now, now))
	r, err := s.CreateTaskRun(context.Background(), newMemTx(t), "id-1", "n-1", 1)
	if err != nil {
		t.Fatalf("CreateTaskRun: %v", err)
	}
	if r.ID != "run-1" {
		t.Errorf("ID = %q", r.ID)
	}
	if r.Status != "Running" {
		t.Errorf("Status = %q", r.Status)
	}
}

func TestStore_CreateTaskRun_Error(t *testing.T) {
	_, s := newMemStore(t)
	store.SetErrorForQuery("INSERT INTO task_runs", errBoom)
	_, err := s.CreateTaskRun(context.Background(), newMemTx(t), "id-1", "n-1", 1)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestStore_UpdateTaskRun(t *testing.T) {
	_, s := newMemStore(t)
	store.SetRowsAffectedForQuery("UPDATE task_runs", 1)
	if err := s.UpdateTaskRun(context.Background(), newMemTx(t), "run-1", "Success", "", []byte(`{}`)); err != nil {
		t.Fatalf("UpdateTaskRun: %v", err)
	}
}

func TestStore_UpdateTaskRun_Error(t *testing.T) {
	_, s := newMemStore(t)
	store.SetErrorForQuery("UPDATE task_runs", errBoom)
	if err := s.UpdateTaskRun(context.Background(), newMemTx(t), "run-1", "Success", "", nil); err == nil {
		t.Fatal("expected error")
	}
}

func TestStore_CreateTaskEvent(t *testing.T) {
	_, s := newMemStore(t)
	store.SetRowsAffectedForQuery("INSERT INTO task_events", 1)
	if err := s.CreateTaskEvent(context.Background(), newMemTx(t), "id-1", "created", "", "Pending", "alice", nil); err != nil {
		t.Fatalf("CreateTaskEvent: %v", err)
	}
}

func TestStore_CreateTaskEvent_Error(t *testing.T) {
	_, s := newMemStore(t)
	store.SetErrorForQuery("INSERT INTO task_events", errBoom)
	if err := s.CreateTaskEvent(context.Background(), newMemTx(t), "id-1", "created", "", "Pending", "alice", nil); err == nil {
		t.Fatal("expected error")
	}
}

func TestStore_GetTaskForUpdate_HappyPath(t *testing.T) {
	_, s := newMemStore(t)
	now := nowFixed()
	store.SetRowForQuery("FROM tasks WHERE id = $1 FOR UPDATE", rowOnly(
		"id-1", nil, "Region", "deploy", "Pending",
		nil, nil, []byte(`{}`), []byte(`{}`),
		"Unclaimed", nil, now,
		3, 0, 60, nil, "alice", now, now,
	))
	row, err := s.GetTaskForUpdate(context.Background(), newMemTx(t), "id-1")
	if err != nil {
		t.Fatalf("GetTaskForUpdate: %v", err)
	}
	if row.ID != "id-1" {
		t.Errorf("ID = %q", row.ID)
	}
}

func TestStore_GetTaskForUpdate_NotFound(t *testing.T) {
	_, s := newMemStore(t)
	store.SetNoRowsForQuery("FROM tasks WHERE id = $1 FOR UPDATE")
	_, err := s.GetTaskForUpdate(context.Background(), newMemTx(t), "missing")
	if err != store.ErrNotFound {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestStore_GetTaskForUpdate_QueryError(t *testing.T) {
	_, s := newMemStore(t)
	store.SetErrorForQuery("FROM tasks WHERE id = $1 FOR UPDATE", errBoom)
	_, err := s.GetTaskForUpdate(context.Background(), newMemTx(t), "x")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestStore_LatestAttempt_HappyPath(t *testing.T) {
	_, s := newMemStore(t)
	store.SetRowForQuery("SELECT COALESCE(MAX(attempt), 0)", rowOnly(int64(3)))
	n, err := s.LatestAttempt(context.Background(), newMemTx(t), "id-1")
	if err != nil {
		t.Fatalf("LatestAttempt: %v", err)
	}
	if n != 3 {
		t.Errorf("n = %d, want 3", n)
	}
}

func TestStore_LatestAttempt_Error(t *testing.T) {
	_, s := newMemStore(t)
	store.SetErrorForQuery("SELECT COALESCE(MAX(attempt), 0)", errBoom)
	_, err := s.LatestAttempt(context.Background(), newMemTx(t), "id-1")
	if err == nil {
		t.Fatal("expected error")
	}
}

// errBoom is a generic error used across store tests.
var errBoom = errSentinel("kaboom")

type errSentinel string

func (e errSentinel) Error() string { return string(e) }

// ensure json.RawMessage round-trips through TaskRow
func TestStore_TaskRow_PayloadRoundTrip(t *testing.T) {
	row := &task.TaskRow{
		Payload: json.RawMessage(`{"x":1}`),
		Result:  json.RawMessage(`{"y":2}`),
	}
	if string(row.Payload) != `{"x":1}` {
		t.Errorf("payload = %q", row.Payload)
	}
	if string(row.Result) != `{"y":2}` {
		t.Errorf("result = %q", row.Result)
	}
}
