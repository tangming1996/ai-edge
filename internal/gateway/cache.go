package gateway

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"time"

	"github.com/edgeai-platform/ai-edge/internal/store"
)

const defaultMaxCachedVersions = 10

// CacheEntry maps to a row in gateway_cache_entries.
type CacheEntry struct {
	ID           string
	GatewayID    string
	ModelID      string
	Version      string
	CachedAt     time.Time
	LastAccessAt time.Time
	SizeBytes    int64
}

// CacheStore manages gateway-level model cache metadata.
type CacheStore struct {
	db                *store.DB
	maxCachedVersions int
}

// NewCacheStore creates a CacheStore with the default eviction threshold.
func NewCacheStore(db *store.DB) *CacheStore {
	return &CacheStore{db: db, maxCachedVersions: defaultMaxCachedVersions}
}

// SetMaxCachedVersions overrides the LRU eviction threshold.
func (c *CacheStore) SetMaxCachedVersions(n int) {
	if n > 0 {
		c.maxCachedVersions = n
	}
}

// Touch records a cache hit. If the entry doesn't exist yet it is created;
// otherwise last_access_at is bumped.
func (c *CacheStore) Touch(ctx context.Context, gatewayID, modelID, version string, sizeBytes int64) error {
	const q = `
		INSERT INTO gateway_cache_entries (gateway_id, model_id, version, size_bytes)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (gateway_id, model_id, version)
		DO UPDATE SET last_access_at = now()`

	_, err := c.db.ExecContext(ctx, q, gatewayID, modelID, version, sizeBytes)
	if err != nil {
		return fmt.Errorf("cache: touch: %w", err)
	}
	return nil
}

// EvictLRU removes the least-recently-used entries for a gateway when the
// total count exceeds maxCachedVersions.
func (c *CacheStore) EvictLRU(ctx context.Context, gatewayID string) (int64, error) {
	const q = `
		DELETE FROM gateway_cache_entries
		WHERE id IN (
			SELECT id FROM gateway_cache_entries
			WHERE gateway_id = $1
			ORDER BY last_access_at DESC
			OFFSET $2
		)`

	res, err := c.db.ExecContext(ctx, q, gatewayID, c.maxCachedVersions)
	if err != nil {
		return 0, fmt.Errorf("cache: evict: %w", err)
	}
	return res.RowsAffected()
}

// Exists checks whether a cache entry already exists.
func (c *CacheStore) Exists(ctx context.Context, gatewayID, modelID, version string) (bool, error) {
	const q = `
		SELECT 1 FROM gateway_cache_entries
		WHERE gateway_id = $1 AND model_id = $2 AND version = $3`

	var dummy int
	err := c.db.QueryRowContext(ctx, q, gatewayID, modelID, version).Scan(&dummy)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("cache: exists: %w", err)
	}
	return true, nil
}

// ListByGateway returns all cached entries for a gateway ordered by last
// access (most recent first).
func (c *CacheStore) ListByGateway(ctx context.Context, gatewayID string) ([]*CacheEntry, error) {
	const q = `
		SELECT id, gateway_id, model_id, version, cached_at, last_access_at, size_bytes
		FROM gateway_cache_entries
		WHERE gateway_id = $1
		ORDER BY last_access_at DESC`

	rows, err := c.db.QueryContext(ctx, q, gatewayID)
	if err != nil {
		return nil, fmt.Errorf("cache: list: %w", err)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			log.Printf("cache: close rows: %v", err)
		}
	}()

	var result []*CacheEntry
	for rows.Next() {
		e := &CacheEntry{}
		if err := rows.Scan(
			&e.ID, &e.GatewayID, &e.ModelID, &e.Version,
			&e.CachedAt, &e.LastAccessAt, &e.SizeBytes,
		); err != nil {
			return nil, fmt.Errorf("cache: scan: %w", err)
		}
		result = append(result, e)
	}
	return result, rows.Err()
}
