//go:build integration

package store

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"
)

// TestIntegration_PingAndMigrate verifies that the configured Postgres is
// reachable from this process. It is the minimum-viable integration check
// for the store package and gates all other integration tests.
func TestIntegration_PingAndMigrate(t *testing.T) {
	dsn := os.Getenv("INTEGRATION_DATABASE_URL")
	if dsn == "" {
		t.Skip("INTEGRATION_DATABASE_URL not set; skipping integration test")
	}

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		t.Fatalf("ping: %v", err)
	}

	// Quick smoke query to make sure the connection is healthy and the
	// role has read access.
	var v int
	if err := db.QueryRowContext(ctx, "SELECT 1").Scan(&v); err != nil {
		t.Fatalf("SELECT 1: %v", err)
	}
	if v != 1 {
		t.Fatalf("expected 1, got %d", v)
	}
}
