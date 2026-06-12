package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	_ "github.com/lib/pq"
)

// Config holds database connection parameters.
type Config struct {
	Host     string
	Port     int
	User     string
	Password string
	DBName   string
	SSLMode  string

	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
}

func (c Config) DSN() string {
	sslMode := c.SSLMode
	if sslMode == "" {
		sslMode = "disable"
	}
	return fmt.Sprintf(
		"host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
		c.Host, c.Port, c.User, c.Password, c.DBName, sslMode,
	)
}

// DB wraps *sql.DB with helper methods for transactions and row locking.
type DB struct {
	*sql.DB
}

// New opens a connection pool with the given config.
func New(cfg Config) (*DB, error) {
	db, err := sql.Open("postgres", cfg.DSN())
	if err != nil {
		return nil, fmt.Errorf("store: open db: %w", err)
	}

	if cfg.MaxOpenConns > 0 {
		db.SetMaxOpenConns(cfg.MaxOpenConns)
	}
	if cfg.MaxIdleConns > 0 {
		db.SetMaxIdleConns(cfg.MaxIdleConns)
	}
	if cfg.ConnMaxLifetime > 0 {
		db.SetConnMaxLifetime(cfg.ConnMaxLifetime)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("store: ping db: %w", err)
	}

	return &DB{DB: db}, nil
}

// Tx represents a database transaction.
type Tx struct {
	*sql.Tx
}

// WithTx runs fn inside a serializable transaction. If fn returns an error the
// transaction is rolled back; otherwise it is committed.
func (db *DB) WithTx(ctx context.Context, fn func(tx *Tx) error) error {
	sqlTx, err := db.BeginTx(ctx, &sql.TxOptions{
		Isolation: sql.LevelSerializable,
	})
	if err != nil {
		return fmt.Errorf("store: begin tx: %w", err)
	}

	tx := &Tx{Tx: sqlTx}
	if err := fn(tx); err != nil {
		_ = sqlTx.Rollback()
		return err
	}
	return sqlTx.Commit()
}

// ForUpdate appends "FOR UPDATE" to a SELECT query string.
func ForUpdate(query string) string {
	return query + " FOR UPDATE"
}

// ForUpdateNoWait appends "FOR UPDATE NOWAIT" to a SELECT query string.
func ForUpdateNoWait(query string) string {
	return query + " FOR UPDATE NOWAIT"
}
