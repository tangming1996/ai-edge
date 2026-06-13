//go:build !integration

package gateway

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"sync"
	"time"
)

// memDB is a minimal in-memory *sql.DB replacement used only by the
// IdentityCache unit tests. It registers a private driver that always
// returns a hand-crafted row keyed by the SQL placeholder argument.
//
// Refactor TODO: the production IdentityCache should accept an interface
// (e.g. identityLookup) so tests don't need to register a sql driver.
// Until then this stub provides enough surface to exercise the cache
// without a real Postgres connection.
type memDB struct {
	mu       sync.Mutex
	rows     map[string]memIdentityRow
	failNext bool
}

type memIdentityRow struct {
	nodeID    string
	gatewayID string
	status    string
}

const memDriverName = "gateway_mem"

var sharedMemDB = &memDB{rows: map[string]memIdentityRow{}}

func init() {
	sql.Register(memDriverName, &memDriver{})
}

func (m *memDB) setRow(fingerprint, nodeID, gatewayID, status string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.rows[fingerprint] = memIdentityRow{nodeID, gatewayID, status}
}

func (m *memDB) setFailOnce() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.failNext = true
}

type memDriver struct{}

func (memDriver) Open(_ string) (driver.Conn, error) { return &memConn{}, nil }

type memConn struct{}

func (c *memConn) Prepare(query string) (driver.Stmt, error) { return &memStmt{query: query}, nil }
func (c *memConn) Close() error                              { return nil }
func (c *memConn) Begin() (driver.Tx, error)                 { return memTx{}, nil }
func (c *memConn) ResetSession(_ context.Context) error      { return nil }
func (c *memConn) IsValid() bool                             { return true }

type memTx struct{}

func (memTx) Commit() error   { return nil }
func (memTx) Rollback() error { return nil }

type memStmt struct{ query string }

func (s *memStmt) Close() error                               { return nil }
func (s *memStmt) NumInput() int                              { return -1 }
func (s *memStmt) CheckNamedValue(_ *driver.NamedValue) error { return nil }
func (s *memStmt) Exec(_ []driver.Value) (driver.Result, error) {
	return nil, errors.New("exec not supported in mem driver")
}
func (s *memStmt) Query(args []driver.Value) (driver.Rows, error) {
	// Legacy Query path: convert Value -> NamedValue and delegate.
	nv := make([]driver.NamedValue, len(args))
	for i, v := range args {
		nv[i] = driver.NamedValue{Ordinal: i + 1, Value: v}
	}
	return s.QueryContext(context.Background(), nv)
}

// QueryContext is the entry point used by database/sql since Go 1.10.
func (s *memStmt) QueryContext(_ context.Context, args []driver.NamedValue) (driver.Rows, error) {
	sharedMemDB.mu.Lock()
	defer sharedMemDB.mu.Unlock()

	if sharedMemDB.failNext {
		sharedMemDB.failNext = false
		return nil, errors.New("forced failure")
	}

	if len(args) == 0 {
		return nil, errors.New("expected at least one arg")
	}
	fp, ok := args[0].Value.(string)
	if !ok {
		return nil, errors.New("first arg must be a string")
	}
	row, ok := sharedMemDB.rows[fp]
	if !ok {
		return &memRows{empty: true}, nil
	}
	return &memRows{row: row}, nil
}

// unused import guards
var (
	_ = time.Second
	_ = io.EOF
	_ context.Context
)

type memRows struct {
	row   memIdentityRow
	empty bool
	idx   int
}

func (r *memRows) Columns() []string {
	return []string{"node_id", "gateway_id", "status"}
}

func (r *memRows) Close() error { return nil }

func (r *memRows) Next(dest []driver.Value) error {
	if r.empty {
		return io.EOF
	}
	if r.idx > 0 {
		return io.EOF
	}
	r.idx++
	dest[0] = r.row.nodeID
	dest[1] = r.row.gatewayID
	dest[2] = r.row.status
	return nil
}

// newMemDB opens a *sql.DB connected to the in-memory driver.
func newMemDB() *sql.DB {
	db, err := sql.Open(memDriverName, "")
	if err != nil {
		panic("open mem driver: " + err.Error())
	}
	// Don't actually open a real connection: QueryRowContext will use the
	// driver. The ping in store.New would fail, but we don't use that here.
	return db
}

// resetMemDB clears all rows and any pending fail-once flag.
func resetMemDB() {
	sharedMemDB.mu.Lock()
	defer sharedMemDB.mu.Unlock()
	sharedMemDB.rows = map[string]memIdentityRow{}
	sharedMemDB.failNext = false
}
