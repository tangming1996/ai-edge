//go:build !integration

package store

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"testing"
)

func openMemDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open(memDriverName, "")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	return db
}

func TestMemDriver_Registers(t *testing.T) {
	ResetMemDB()
	db := openMemDB(t)
	defer func() { _ = db.Close() }()
}

func TestMemDriver_PingContext_NoOp(t *testing.T) {
	ResetMemDB()
	db := openMemDB(t)
	defer func() { _ = db.Close() }()
	if err := db.PingContext(context.Background()); err != nil {
		t.Errorf("ping: %v", err)
	}
}

func TestNewMemDB_Opens(t *testing.T) {
	db := NewMemDB()
	if db == nil {
		t.Fatal("nil db")
	}
	defer func() { _ = db.Close() }()
}

func TestNewMemStore_Opens(t *testing.T) {
	s := NewMemStore()
	if s == nil {
		t.Fatal("nil store")
	}
}

func TestSetDefaultEmptyNoRows_True(t *testing.T) {
	ResetMemDB()
	SetDefaultEmptyNoRows(true)
	db := openMemDB(t)
	defer func() { _ = db.Close() }()
	var v int64
	if err := db.QueryRowContext(context.Background(), "SELECT 1").Scan(&v); err != nil {
		t.Fatalf("scan: %v", err)
	}
	// The mem driver returns int64(0) for unmatched empty queries.
	if v != 0 {
		t.Errorf("v = %d, want 0", v)
	}
}

func TestSetDefaultEmptyNoRows_False(t *testing.T) {
	ResetMemDB()
	SetDefaultEmptyNoRows(false)
	db := openMemDB(t)
	defer func() { _ = db.Close() }()
	var v int64
	err := db.QueryRowContext(context.Background(), "SELECT 1").Scan(&v)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestSetRowsForQuery(t *testing.T) {
	ResetMemDB()
	SetRowsForQuery("FROM multi", [][]driver.Value{
		{"a", "b"},
		{"c", "d"},
	})
	db := openMemDB(t)
	defer func() { _ = db.Close() }()
	rows, err := db.QueryContext(context.Background(), "SELECT * FROM multi")
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer func() { _ = rows.Close() }()
	count := 0
	for rows.Next() {
		count++
	}
	if count != 2 {
		t.Errorf("count = %d, want 2", count)
	}
}

func TestNextID_AutoIncrements(t *testing.T) {
	ResetMemDB()
	a := NextID()
	b := NextID()
	if a == b {
		t.Errorf("ids not unique: %q == %q", a, b)
	}
}

func TestWithTx_Commit_Success(t *testing.T) {
	ResetMemDB()
	db := NewMemStore()
	SetRowsAffectedForQuery("UPDATE tx_test", 1)
	err := db.WithTx(context.Background(), func(tx *Tx) error {
		_, err := tx.ExecContext(context.Background(), "UPDATE tx_test SET x=1")
		return err
	})
	if err != nil {
		t.Errorf("WithTx: %v", err)
	}
}

func TestWithTx_Rollback_OnError(t *testing.T) {
	ResetMemDB()
	db := NewMemStore()
	myErr := errors.New("nope")
	err := db.WithTx(context.Background(), func(tx *Tx) error {
		return myErr
	})
	if !errors.Is(err, myErr) {
		t.Errorf("err = %v, want %v", err, myErr)
	}
}

func TestWithTx_NoOp(t *testing.T) {
	ResetMemDB()
	db := NewMemStore()
	if err := db.WithTx(context.Background(), func(tx *Tx) error { return nil }); err != nil {
		t.Errorf("WithTx: %v", err)
	}
}

func TestDB_QueryRowContext_NoMatch(t *testing.T) {
	ResetMemDB()
	SetDefaultEmptyNoRows(false)
	db := openMemDB(t)
	defer func() { _ = db.Close() }()
	var v string
	err := db.QueryRowContext(context.Background(), "SELECT * FROM unknown_table").Scan(&v)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestDB_ExecContext(t *testing.T) {
	ResetMemDB()
	db := openMemDB(t)
	defer func() { _ = db.Close() }()
	res, err := db.ExecContext(context.Background(), "INSERT INTO foo VALUES (1)")
	if err != nil {
		t.Fatalf("exec: %v", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		t.Fatalf("rows affected: %v", err)
	}
	// Default RowsAffected is 1 when not configured.
	if n != 1 {
		t.Errorf("rows affected = %d", n)
	}
}

func TestSetErrorForQuery_Exec(t *testing.T) {
	ResetMemDB()
	SetErrorForQuery("UPDATE bad", errors.New("exec err"))
	db := openMemDB(t)
	defer func() { _ = db.Close() }()
	_, err := db.ExecContext(context.Background(), "UPDATE bad SET x=1")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestResetMemDB_Clears(t *testing.T) {
	ResetMemDB()
	SetDefaultEmptyNoRows(false)
	SetRowForQuery("SELECT x", []driver.Value{1})
	ResetMemDB()
	SetDefaultEmptyNoRows(false)
	db := openMemDB(t)
	defer func() { _ = db.Close() }()
	var v int
	err := db.QueryRowContext(context.Background(), "SELECT x").Scan(&v)
	if err == nil {
		t.Fatal("expected error after reset")
	}
}

func TestMemConn_ResetSession_IsValid(t *testing.T) {
	ResetMemDB()
	c, err := (&memDriver{}).Open("")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	mc := c.(*memConn)
	if !mc.IsValid() {
		t.Error("expected valid")
	}
	if err := mc.ResetSession(context.Background()); err != nil {
		t.Errorf("reset: %v", err)
	}
	if err := mc.Close(); err != nil {
		t.Errorf("close: %v", err)
	}
}

func TestMemTx_CommitRollback(t *testing.T) {
	tx := memTx{}
	if err := tx.Commit(); err != nil {
		t.Errorf("commit: %v", err)
	}
	if err := tx.Rollback(); err != nil {
		t.Errorf("rollback: %v", err)
	}
}

func TestMemStmt_CloseAndNumInput(t *testing.T) {
	s := &memStmt{query: "SELECT 1"}
	if err := s.Close(); err != nil {
		t.Errorf("close: %v", err)
	}
	if s.NumInput() != -1 {
		t.Errorf("NumInput = %d", s.NumInput())
	}
	if err := s.CheckNamedValue(nil); err != nil {
		t.Errorf("CheckNamedValue: %v", err)
	}
}

func TestMemStmt_Query_NoMatch_NoArgs(t *testing.T) {
	ResetMemDB()
	SetDefaultEmptyNoRows(false)
	s := &memStmt{query: "SELECT * FROM unknown"}
	rows, err := s.Query(nil)
	if err != nil {
		t.Fatalf("Query returned error: %v", err)
	}
	// The error is embedded in the rows, surfaced on Next().
	if rows.Next(nil) == nil {
		t.Fatal("expected error from Next")
	}
}

func TestFingerprint_AllTypes(t *testing.T) {
	cases := []driver.Value{
		"foo",
		42,
		int32(42),
		int64(42),
		nil,
		1.5,
	}
	for _, v := range cases {
		_ = fingerprint([]driver.Value{v})
	}
}
