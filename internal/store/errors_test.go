package store

import (
	"errors"
	"fmt"
	"testing"
)

func TestIsUniqueViolation(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "nil error",
			err:  nil,
			want: false,
		},
		{
			name: "plain error",
			err:  errors.New("some other failure"),
			want: false,
		},
		{
			name: "SQLSTATE 23505",
			err:  errors.New("pq: duplicate key value violates unique constraint \"uq_foo\": SQLSTATE 23505"),
			want: true,
		},
		{
			name: "english duplicate key description",
			err:  errors.New("ERROR: duplicate key value violates unique constraint \"uq_foo\""),
			want: true,
		},
		{
			name: "foreign key violation must not match",
			err:  errors.New("violates foreign key constraint"),
			want: false,
		},
		{
			name: "wrapped unique violation still detected",
			err:  fmt.Errorf("insert failed: %w", errors.New("duplicate key value violates unique constraint")),
			want: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := IsUniqueViolation(tc.err)
			if got != tc.want {
				t.Fatalf("IsUniqueViolation(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

func TestIsForeignKeyViolation(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "nil error",
			err:  nil,
			want: false,
		},
		{
			name: "plain error",
			err:  errors.New("kaboom"),
			want: false,
		},
		{
			name: "SQLSTATE 23503",
			err:  errors.New("pq: insert or update on table \"foo\" violates foreign key constraint \"fk_foo_bar\": SQLSTATE 23503"),
			want: true,
		},
		{
			name: "english foreign key description",
			err:  errors.New("ERROR: violates foreign key constraint \"fk_foo_bar\""),
			want: true,
		},
		{
			name: "unique violation must not match",
			err:  errors.New("duplicate key value violates unique constraint"),
			want: false,
		},
		{
			name: "wrapped foreign key violation still detected",
			err:  fmt.Errorf("tx commit: %w", errors.New("violates foreign key constraint \"fk_a\"")),
			want: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := IsForeignKeyViolation(tc.err)
			if got != tc.want {
				t.Fatalf("IsForeignKeyViolation(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

func TestSentinelErrors_NotNil(t *testing.T) {
	// Smoke test: the exported sentinels must not be nil so that callers can
	// rely on errors.Is comparisons.
	sentinels := map[string]error{
		"ErrNotFound":         ErrNotFound,
		"ErrConflict":         ErrConflict,
		"ErrAlreadyExists":    ErrAlreadyExists,
		"ErrPermissionDenied": ErrPermissionDenied,
		"ErrPrecondition":     ErrPrecondition,
	}
	for name, e := range sentinels {
		if e == nil {
			t.Errorf("%s is nil", name)
		}
	}
}

func TestSentinelErrors_AreDistinguishable(t *testing.T) {
	// IsUniqueViolation and IsForeignKeyViolation must not overlap, and the
	// generic sentinel errors must not accidentally match each other.
	if IsUniqueViolation(ErrNotFound) {
		t.Error("ErrNotFound should not match IsUniqueViolation")
	}
	if IsForeignKeyViolation(ErrNotFound) {
		t.Error("ErrNotFound should not match IsForeignKeyViolation")
	}
	if errors.Is(ErrNotFound, ErrConflict) {
		t.Error("ErrNotFound should not match ErrConflict")
	}
}
