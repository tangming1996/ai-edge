package store

import (
	"strings"
	"testing"
)

func TestConfig_DSN(t *testing.T) {
	cases := []struct {
		name     string
		cfg      Config
		contains []string
		omits    []string
	}{
		{
			name: "default sslmode=disable",
			cfg: Config{
				Host:     "h",
				Port:     5432,
				User:     "u",
				Password: "p",
				DBName:   "d",
			},
			contains: []string{
				"host=h", "port=5432", "user=u", "password=p", "dbname=d", "sslmode=disable",
			},
		},
		{
			name: "custom sslmode=require",
			cfg: Config{
				Host:     "10.0.0.1",
				Port:     6543,
				User:     "alice",
				Password: "s3cret",
				DBName:   "edgeai",
				SSLMode:  "require",
			},
			contains: []string{
				"host=10.0.0.1", "port=6543", "user=alice", "password=s3cret",
				"dbname=edgeai", "sslmode=require",
			},
		},
		{
			name: "sslmode=verify-full",
			cfg: Config{
				Host:     "h",
				Port:     5432,
				User:     "u",
				Password: "p",
				DBName:   "d",
				SSLMode:  "verify-full",
			},
			contains: []string{"sslmode=verify-full"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.cfg.DSN()
			if got == "" {
				t.Fatal("DSN is empty")
			}
			for _, want := range tc.contains {
				if !strings.Contains(got, want) {
					t.Errorf("DSN %q missing %q", got, want)
				}
			}
			if tc.cfg.SSLMode == "" {
				// Default branch should never accidentally include a stale sslmode.
				if !strings.Contains(got, "sslmode=disable") {
					t.Errorf("DSN %q did not default to sslmode=disable", got)
				}
			}
		})
	}
}

func TestForUpdate(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "empty string",
			input: "",
			want:  " FOR UPDATE",
		},
		{
			name:  "single line with trailing space",
			input: "SELECT id FROM tasks",
			want:  "SELECT id FROM tasks FOR UPDATE",
		},
		{
			name:  "no trailing space",
			input: "SELECT id FROM tasks WHERE id=1",
			want:  "SELECT id FROM tasks WHERE id=1 FOR UPDATE",
		},
		{
			name:  "multi-line query",
			input: "SELECT id\nFROM tasks\nWHERE status = 'Pending'",
			want:  "SELECT id\nFROM tasks\nWHERE status = 'Pending' FOR UPDATE",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ForUpdate(tc.input)
			if got != tc.want {
				t.Fatalf("ForUpdate(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestForUpdateNoWait(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "empty string",
			input: "",
			want:  " FOR UPDATE NOWAIT",
		},
		{
			name:  "simple query",
			input: "SELECT id FROM tasks",
			want:  "SELECT id FROM tasks FOR UPDATE NOWAIT",
		},
		{
			name:  "multi-line query",
			input: "SELECT id\nFROM tasks\nWHERE id = 1",
			want:  "SELECT id\nFROM tasks\nWHERE id = 1 FOR UPDATE NOWAIT",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ForUpdateNoWait(tc.input)
			if got != tc.want {
				t.Fatalf("ForUpdateNoWait(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestForUpdate_DoesNotMutateInput(t *testing.T) {
	// Defensive: a query that is already terminated should not have its
	// internals rewritten.
	input := "SELECT 1"
	_ = ForUpdate(input)
	if input != "SELECT 1" {
		t.Fatalf("input mutated: %q", input)
	}
}

func TestForUpdate_AlwaysAppendsClause(t *testing.T) {
	// ForUpdate should *always* return a string that contains the
	// FOR UPDATE clause — the function is intentionally non-validating.
	got := ForUpdate("SELECT 1")
	if !strings.Contains(got, "FOR UPDATE") {
		t.Fatalf("ForUpdate missing clause: %q", got)
	}
	gotNoWait := ForUpdateNoWait("SELECT 1")
	if !strings.Contains(gotNoWait, "FOR UPDATE NOWAIT") {
		t.Fatalf("ForUpdateNoWait missing clause: %q", gotNoWait)
	}
}
