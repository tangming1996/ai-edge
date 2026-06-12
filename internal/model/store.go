package model

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/edgeai-platform/ai-edge/internal/store"
)

// ModelRow is the row model for the models table.
type ModelRow struct {
	ID           string
	Name         string
	Version      string
	Format       string
	Checksum     string
	SizeBytes    int64
	ArtifactURI  string
	SignatureURI string
	Labels       json.RawMessage
	CreatedAt    time.Time
}

// Store provides data access for models.
type Store struct {
	db *store.DB
}

// NewStore creates a model Store.
func NewStore(db *store.DB) *Store {
	return &Store{db: db}
}

// Create inserts a new model row. The generated id and created_at are
// written back into row.
func (s *Store) Create(ctx context.Context, row *ModelRow) error {
	const q = `
		INSERT INTO models (name, version, format, checksum, size_bytes,
		                     artifact_uri, signature_uri, labels)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
		RETURNING id, created_at`

	labels := row.Labels
	if labels == nil {
		labels = json.RawMessage(`{}`)
	}

	return s.db.QueryRowContext(ctx, q,
		row.Name,
		row.Version,
		row.Format,
		row.Checksum,
		row.SizeBytes,
		row.ArtifactURI,
		row.SignatureURI,
		labels,
	).Scan(&row.ID, &row.CreatedAt)
}

// GetByID loads a single model by primary key.
func (s *Store) GetByID(ctx context.Context, id string) (*ModelRow, error) {
	const q = `
		SELECT id, name, version, format, checksum, size_bytes,
		       artifact_uri, signature_uri, labels, created_at
		FROM models WHERE id = $1`

	return s.scanOne(ctx, q, id)
}

// GetByNameVersion loads a model by its unique (name, version) pair.
func (s *Store) GetByNameVersion(ctx context.Context, name, version string) (*ModelRow, error) {
	const q = `
		SELECT id, name, version, format, checksum, size_bytes,
		       artifact_uri, signature_uri, labels, created_at
		FROM models WHERE name = $1 AND version = $2`

	return s.scanOne(ctx, q, name, version)
}

func (s *Store) scanOne(ctx context.Context, query string, args ...any) (*ModelRow, error) {
	row := &ModelRow{}
	err := s.db.QueryRowContext(ctx, query, args...).Scan(
		&row.ID, &row.Name, &row.Version, &row.Format,
		&row.Checksum, &row.SizeBytes,
		&row.ArtifactURI, &row.SignatureURI,
		&row.Labels, &row.CreatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, store.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("model: get: %w", err)
	}
	return row, nil
}

// ListFilter holds optional query filters for List.
type ListFilter struct {
	Name   string
	Format string
	Limit  int
	Offset int
}

// List returns a paginated slice of models matching the filter along with
// the total count.
func (s *Store) List(ctx context.Context, f ListFilter) ([]*ModelRow, int, error) {
	where := "WHERE 1=1"
	args := []any{}
	idx := 1

	if f.Name != "" {
		where += fmt.Sprintf(" AND name = $%d", idx)
		args = append(args, f.Name)
		idx++
	}
	if f.Format != "" {
		where += fmt.Sprintf(" AND format = $%d", idx)
		args = append(args, f.Format)
		idx++
	}

	var total int
	countQ := "SELECT COUNT(*) FROM models " + where
	if err := s.db.QueryRowContext(ctx, countQ, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("model: list count: %w", err)
	}

	limit := f.Limit
	if limit <= 0 || limit > 100 {
		limit = 20
	}

	dataQ := fmt.Sprintf(`
		SELECT id, name, version, format, checksum, size_bytes,
		       artifact_uri, signature_uri, labels, created_at
		FROM models %s
		ORDER BY created_at DESC
		LIMIT $%d OFFSET $%d`, where, idx, idx+1)
	args = append(args, limit, f.Offset)

	rows, err := s.db.QueryContext(ctx, dataQ, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("model: list: %w", err)
	}
	defer rows.Close()

	var result []*ModelRow
	for rows.Next() {
		r := &ModelRow{}
		if err := rows.Scan(
			&r.ID, &r.Name, &r.Version, &r.Format,
			&r.Checksum, &r.SizeBytes,
			&r.ArtifactURI, &r.SignatureURI,
			&r.Labels, &r.CreatedAt,
		); err != nil {
			return nil, 0, fmt.Errorf("model: scan row: %w", err)
		}
		result = append(result, r)
	}
	return result, total, rows.Err()
}
