//go:build integration

package onboarding

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/edgeai-platform/ai-edge/internal/store"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// dsnToConfig parses a libpq-style DSN into a store.Config without
// pulling in any new dependencies. It supports the URL form
// (postgres://user:pass@host:port/dbname?sslmode=...).
func dsnToConfig(dsn string) (store.Config, error) {
	u, err := url.Parse(dsn)
	if err != nil {
		return store.Config{}, fmt.Errorf("parse dsn: %w", err)
	}
	port := 5432
	if p := u.Port(); p != "" {
		n, err := strconv.Atoi(p)
		if err != nil {
			return store.Config{}, fmt.Errorf("parse port: %w", err)
		}
		port = n
	}
	pw, _ := u.User.Password()
	cfg := store.Config{
		Host:     u.Hostname(),
		Port:     port,
		User:     u.User.Username(),
		Password: pw,
		DBName:   u.Path[1:], // strip leading "/"
		SSLMode:  u.Query().Get("sslmode"),
	}
	return cfg, nil
}

// requireIntegrationDB opens a *store.DB against the configured
// integration database or skips the test. It is the entry point for
// every integration test in this package.
func requireIntegrationDB(t *testing.T) *store.DB {
	t.Helper()
	dsn := os.Getenv("INTEGRATION_DATABASE_URL")
	if dsn == "" {
		t.Skip("INTEGRATION_DATABASE_URL not set; skipping integration test")
	}
	cfg, err := dsnToConfig(dsn)
	if err != nil {
		t.Fatalf("parse dsn: %v", err)
	}
	db, err := store.New(cfg)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// TestIntegration_TokenCreateConsumeExhaust verifies the bootstrap_token
// happy path against a real Postgres: a fresh token can be created,
// consumed once successfully, and then rejected as exhausted.
func TestIntegration_TokenCreateConsumeExhaust(t *testing.T) {
	db := requireIntegrationDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Need a gateway row to satisfy the FK on bootstrap_tokens.gateway_id.
	var gatewayID string
	err := db.QueryRowContext(ctx,
		`INSERT INTO gateways (name, region, status)
		 VALUES ('integration-gw', 'cn-test', 'Active')
		 RETURNING id`,
	).Scan(&gatewayID)
	if err != nil {
		t.Fatalf("insert gateway: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(),
			`DELETE FROM gateways WHERE id = $1`, gatewayID)
	})

	tok := NewTokenStore(db)
	rec, tokenPlain, err := tok.Create(ctx, gatewayID, "integration test", nil, 1, 1*time.Hour)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(),
			`DELETE FROM bootstrap_tokens WHERE id = $1`, rec.ID)
	})

	// First consumption must succeed.
	err = db.WithTx(ctx, func(tx *store.Tx) error {
		_, err := tok.ValidateAndConsume(ctx, tx, tokenPlain, gatewayID)
		return err
	})
	if err != nil {
		t.Fatalf("first ValidateAndConsume: %v", err)
	}

	// Second consumption must fail with the exhausted error.
	err = db.WithTx(ctx, func(tx *store.Tx) error {
		_, err := tok.ValidateAndConsume(ctx, tx, tokenPlain, gatewayID)
		return err
	})
	if err == nil {
		t.Fatal("expected exhausted error on second consumption")
	}
	st, ok := status.FromError(err)
	if !ok || st.Code() != codes.ResourceExhausted {
		t.Fatalf("expected ResourceExhausted, got %v", err)
	}
}
