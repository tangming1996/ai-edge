//go:build !integration

package store

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"sync"
)

// Querier is the minimal interface that the gRPC service layer depends on.
// It is satisfied by *DB and *Tx, and by the test memDriver in this
// package, which lets unit tests exercise the service code paths without
// a real Postgres connection.
type Querier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// memDriver is an in-memory database driver used only by unit tests. It
// dispatches each query by a small regular expression matched against
// the SQL text and returns whatever canned response the test registered.
// This lets a single test run multiple distinct SELECTs/INSERTs/UPDATEs
// with confidence, even when they happen to share the same first
// placeholder.
//
// Refactor TODO: when a real test DB is available, prefer the
// integration driver (see db_integration_test.go). Until then,
// memDriver keeps the service layer testable from the `go test ./...`
// command line.
type memDriver struct{}

var memDriverName = "edgeai_mem"

func init() {
	sql.Register(memDriverName, &memDriver{})
}

// memDB is the in-memory state shared between tests in a single
// package. Each call to Open returns a new *sql.DB whose statements
// all see the same memDB.
type memDB struct {
	mu sync.Mutex

	// rowByQuery[querySubstring] = single row to return
	rowByQuery map[string][]driver.Value
	// rowsByQuery[querySubstring] = list of rows
	rowsByQuery map[string][][]driver.Value
	// noRowsByQuery[querySubstring] = true → return ErrNoRows
	noRowsByQuery map[string]bool
	// rowsAffectedByQuery[querySubstring] = RowsAffected for an Exec
	rowsAffectedByQuery map[string]int64
	// errorsByQuery[querySubstring] = error to return instead of result
	errorsByQuery map[string]error
	// defaultEmptyNoRows applies to SELECTs that have no placeholders
	// (e.g. COUNT(*) on an empty table). When true, returns a single
	// [int64(0)] row.
	defaultEmptyNoRows bool

	// nextID is the auto-incremented id source.
	nextID int64
}

var sharedMemDB = &memDB{
	rowByQuery:          map[string][]driver.Value{},
	rowsByQuery:         map[string][][]driver.Value{},
	noRowsByQuery:       map[string]bool{},
	rowsAffectedByQuery: map[string]int64{},
	errorsByQuery:       map[string]error{},
	defaultEmptyNoRows:  true,
}

// ResetMemDB clears all in-memory state. Tests should call this in
// setup to avoid bleed-through.
func ResetMemDB() {
	sharedMemDB.mu.Lock()
	defer sharedMemDB.mu.Unlock()
	sharedMemDB.rowByQuery = map[string][]driver.Value{}
	sharedMemDB.rowsByQuery = map[string][][]driver.Value{}
	sharedMemDB.noRowsByQuery = map[string]bool{}
	sharedMemDB.rowsAffectedByQuery = map[string]int64{}
	sharedMemDB.errorsByQuery = map[string]error{}
	sharedMemDB.defaultEmptyNoRows = true
	sharedMemDB.nextID = 0
}

// SetDefaultEmptyNoRows toggles whether an unconfigured SELECT with no
// placeholders (e.g. COUNT(*)) defaults to a single [int64(0)] row.
// Defaults to true; tests can disable it if they want a literal
// sql.ErrNoRows.
func SetDefaultEmptyNoRows(enabled bool) {
	sharedMemDB.mu.Lock()
	defer sharedMemDB.mu.Unlock()
	sharedMemDB.defaultEmptyNoRows = enabled
}

// SetRowForQuery configures a single-row response for any query whose
// (lowercased) text contains substring. The first matching substring
// wins, so register more specific substrings first.
func SetRowForQuery(substring string, row []driver.Value) {
	sharedMemDB.mu.Lock()
	defer sharedMemDB.mu.Unlock()
	lc := strings.ToLower(substring)
	sharedMemDB.rowByQuery[lc] = row
	delete(sharedMemDB.noRowsByQuery, lc)
}

// SetRowsForQuery configures a list-of-rows response.
func SetRowsForQuery(substring string, rows [][]driver.Value) {
	sharedMemDB.mu.Lock()
	defer sharedMemDB.mu.Unlock()
	lc := strings.ToLower(substring)
	sharedMemDB.rowsByQuery[lc] = rows
}

// SetNoRowsForQuery makes any query whose text contains substring
// return sql.ErrNoRows.
func SetNoRowsForQuery(substring string) {
	sharedMemDB.mu.Lock()
	defer sharedMemDB.mu.Unlock()
	lc := strings.ToLower(substring)
	sharedMemDB.noRowsByQuery[lc] = true
	delete(sharedMemDB.rowByQuery, lc)
}

// SetRowsAffectedForQuery makes an Exec-style query report n rows
// affected. Useful for UPDATE/DELETE.
func SetRowsAffectedForQuery(substring string, n int64) {
	sharedMemDB.mu.Lock()
	defer sharedMemDB.mu.Unlock()
	lc := strings.ToLower(substring)
	sharedMemDB.rowsAffectedByQuery[lc] = n
}

// SetErrorForQuery makes the matching query return err.
func SetErrorForQuery(substring string, err error) {
	sharedMemDB.mu.Lock()
	defer sharedMemDB.mu.Unlock()
	lc := strings.ToLower(substring)
	sharedMemDB.errorsByQuery[lc] = err
}

// NextID returns a stable auto-incremented id.
func NextID() string {
	sharedMemDB.mu.Lock()
	defer sharedMemDB.mu.Unlock()
	sharedMemDB.nextID++
	return fmt.Sprintf("%d", sharedMemDB.nextID)
}

// fingerprint returns the first placeholder value if it is a string,
// int, int32, or int64. This lets LIMIT/OFFSET and similar integer
// arguments participate in the lookup so the same query with different
// page sizes does not collide.
func fingerprint(args []driver.Value) string {
	if len(args) == 0 {
		return ""
	}
	switch v := args[0].(type) {
	case string:
		return v
	case int:
		return fmt.Sprintf("%d", v)
	case int32:
		return fmt.Sprintf("%d", v)
	case int64:
		return fmt.Sprintf("%d", v)
	}
	return ""
}

// memConn implements driver.Conn.
type memConn struct{}

func (memDriver) Open(_ string) (driver.Conn, error) { return &memConn{}, nil }

func (c *memConn) Prepare(query string) (driver.Stmt, error) {
	return &memStmt{query: query}, nil
}
func (c *memConn) Close() error              { return nil }
func (c *memConn) Begin() (driver.Tx, error) { return memTx{}, nil }
func (c *memConn) BeginTx(_ context.Context, _ driver.TxOptions) (driver.Tx, error) {
	return memTx{}, nil
}
func (c *memConn) ResetSession(_ context.Context) error { return nil }
func (c *memConn) IsValid() bool                        { return true }

// memTx is a no-op transaction.
type memTx struct{}

func (memTx) Commit() error   { return nil }
func (memTx) Rollback() error { return nil }

// memStmt is a no-value-holder statement; values flow through args.
type memStmt struct{ query string }

func (s *memStmt) Close() error  { return nil }
func (s *memStmt) NumInput() int { return -1 }

func (s *memStmt) CheckNamedValue(_ *driver.NamedValue) error { return nil }

// findFirstMatch returns the first registered substring that is
// contained in the (lowercased) query. The lookup order is: errors,
// single rows, multi rows, noRows, rowsAffected. Errors win because
// the test most often uses them to simulate a failure path.
func (s *memStmt) findFirstMatch(lc string) (string, bool) {
	for substr := range sharedMemDB.errorsByQuery {
		if strings.Contains(lc, substr) {
			return substr, true
		}
	}
	for substr := range sharedMemDB.rowByQuery {
		if strings.Contains(lc, substr) {
			return substr, true
		}
	}
	for substr := range sharedMemDB.rowsByQuery {
		if strings.Contains(lc, substr) {
			return substr, true
		}
	}
	for substr := range sharedMemDB.noRowsByQuery {
		if strings.Contains(lc, substr) {
			return substr, true
		}
	}
	for substr := range sharedMemDB.rowsAffectedByQuery {
		if strings.Contains(lc, substr) {
			return substr, true
		}
	}
	return "", false
}

// Exec dispatches to the first matching registered substring and
// returns the configured RowsAffected (default 1).
func (s *memStmt) Exec(args []driver.Value) (driver.Result, error) {
	sharedMemDB.mu.Lock()
	defer sharedMemDB.mu.Unlock()
	lc := strings.ToLower(s.query)

	if substr, ok := s.findFirstMatch(lc); ok {
		if err, ok := sharedMemDB.errorsByQuery[substr]; ok {
			return nil, err
		}
		if n, ok := sharedMemDB.rowsAffectedByQuery[substr]; ok {
			return driver.RowsAffected(n), nil
		}
	}
	_ = args
	return driver.RowsAffected(1), nil
}

// Query is the legacy path; convert and delegate to QueryContext.
func (s *memStmt) Query(args []driver.Value) (driver.Rows, error) {
	nv := make([]driver.NamedValue, len(args))
	for i, v := range args {
		nv[i] = driver.NamedValue{Ordinal: i + 1, Value: v}
	}
	return s.QueryContext(context.Background(), nv)
}

func (s *memStmt) QueryContext(_ context.Context, args []driver.NamedValue) (driver.Rows, error) {
	sharedMemDB.mu.Lock()
	defer sharedMemDB.mu.Unlock()
	lc := strings.ToLower(s.query)

	if substr, ok := s.findFirstMatch(lc); ok {
		if err, ok := sharedMemDB.errorsByQuery[substr]; ok {
			return nil, err
		}
		if noRows, ok := sharedMemDB.noRowsByQuery[substr]; ok && noRows {
			return &memRows{err: io.EOF}, nil
		}
		if row, ok := sharedMemDB.rowByQuery[substr]; ok {
			return &memRows{row: row, idx: 0}, nil
		}
		if rows, ok := sharedMemDB.rowsByQuery[substr]; ok {
			return &memRows{rows: rows}, nil
		}
	}
	// No match: queries with no placeholders (e.g. COUNT(*)) default
	// to a single [int64(0)] row so the Scan succeeds. All others
	// return io.EOF which becomes sql.ErrNoRows.
	if len(args) == 0 && sharedMemDB.defaultEmptyNoRows {
		return &memRows{row: []driver.Value{int64(0)}, idx: 0}, nil
	}
	return &memRows{err: io.EOF}, nil
}

// memRows is the in-memory result set used for all queries.
type memRows struct {
	row  []driver.Value
	rows [][]driver.Value

	idx int
	err error
}

func (r *memRows) Columns() []string {
	if r.row != nil {
		cols := make([]string, len(r.row))
		for i := range cols {
			cols[i] = fmt.Sprintf("c%d", i)
		}
		return cols
	}
	if len(r.rows) > 0 {
		cols := make([]string, len(r.rows[0]))
		for i := range cols {
			cols[i] = fmt.Sprintf("c%d", i)
		}
		return cols
	}
	return nil
}

func (r *memRows) Close() error { return nil }

func (r *memRows) Next(dest []driver.Value) error {
	if r.err != nil {
		return r.err
	}
	if r.row != nil {
		if r.idx > 0 {
			return io.EOF
		}
		r.idx++
		for i, v := range r.row {
			if i < len(dest) {
				dest[i] = v
			}
		}
		return nil
	}
	if r.idx >= len(r.rows) {
		return io.EOF
	}
	row := r.rows[r.idx]
	r.idx++
	for i, v := range row {
		if i < len(dest) {
			dest[i] = v
		}
	}
	return nil
}

// NewMemDB returns a *sql.DB backed by the in-memory driver. The
// returned *sql.DB shares state via the package-level memDB; tests
// should call ResetMemDB to clear it.
func NewMemDB() *sql.DB {
	db, err := sql.Open(memDriverName, "")
	if err != nil {
		panic("store: open mem driver: " + err.Error())
	}
	return db
}

// NewMemStore returns a *DB wrapping the in-memory driver. Useful for
// tests that need a *store.DB.
func NewMemStore() *DB {
	return &DB{DB: NewMemDB()}
}

// Ensure errors package is referenced (used by callers wrapping these).
var _ = errors.New

// regexp is reserved for future use as the lookup mechanism (see
// findFirstMatch). Keeping the import makes that future refactor
// trivial.
var _ = regexp.MustCompile
