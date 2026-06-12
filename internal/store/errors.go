package store

import (
	"errors"
	"strings"
)

// Sentinel errors for business-layer mapping.
var (
	ErrNotFound         = errors.New("not found")
	ErrConflict         = errors.New("conflict")
	ErrAlreadyExists    = errors.New("already exists")
	ErrPermissionDenied = errors.New("permission denied")
	ErrPrecondition     = errors.New("precondition failed")
)

// IsUniqueViolation checks whether a Postgres error is a unique_violation
// (SQLSTATE 23505).
func IsUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "duplicate key value violates unique constraint") ||
		strings.Contains(err.Error(), "23505")
}

// IsForeignKeyViolation checks whether a Postgres error is a
// foreign_key_violation (SQLSTATE 23503).
func IsForeignKeyViolation(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "violates foreign key constraint") ||
		strings.Contains(err.Error(), "23503")
}
