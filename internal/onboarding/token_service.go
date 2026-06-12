package onboarding

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/edgeai-platform/ai-edge/internal/store"
)

// TokenStore handles bootstrap_tokens table operations.
type TokenStore struct {
	db *store.DB
}

// NewTokenStore creates a new TokenStore.
func NewTokenStore(db *store.DB) *TokenStore {
	return &TokenStore{db: db}
}

// TokenRecord represents a row in bootstrap_tokens.
type TokenRecord struct {
	ID          string
	GatewayID   string
	TokenHash   string
	Description string
	Labels      map[string]string
	MaxUses     int
	UsedCount   int
	Status      string
	ExpiresAt   time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// Create generates a new bootstrap token, stores its SHA256 hash, and returns
// both the metadata record and the plaintext token.
func (s *TokenStore) Create(ctx context.Context, gatewayID, description string, labels map[string]string, maxUses int, expiresIn time.Duration) (*TokenRecord, string, error) {
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return nil, "", fmt.Errorf("generate token: %w", err)
	}
	tokenPlain := hex.EncodeToString(tokenBytes)

	hash := sha256.Sum256([]byte(tokenPlain))
	tokenHash := hex.EncodeToString(hash[:])

	labelsJSON, _ := json.Marshal(labels)
	if labels == nil {
		labelsJSON = []byte("{}")
	}

	expiresAt := time.Now().Add(expiresIn)

	var rec TokenRecord
	err := s.db.QueryRowContext(ctx, `
		INSERT INTO bootstrap_tokens (gateway_id, token_hash, description, labels, max_uses, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, gateway_id, token_hash, description, labels, max_uses, used_count, status, expires_at, created_at, updated_at`,
		gatewayID, tokenHash, description, labelsJSON, maxUses, expiresAt,
	).Scan(&rec.ID, &rec.GatewayID, &rec.TokenHash, &rec.Description, &labelsJSON,
		&rec.MaxUses, &rec.UsedCount, &rec.Status, &rec.ExpiresAt, &rec.CreatedAt, &rec.UpdatedAt)
	if err != nil {
		return nil, "", fmt.Errorf("insert token: %w", err)
	}
	_ = json.Unmarshal(labelsJSON, &rec.Labels)
	return &rec, tokenPlain, nil
}

// GetByID retrieves a token by its ID.
func (s *TokenStore) GetByID(ctx context.Context, id string) (*TokenRecord, error) {
	return s.scanOne(ctx, `SELECT id, gateway_id, token_hash, description, labels, max_uses, used_count, status, expires_at, created_at, updated_at
		FROM bootstrap_tokens WHERE id = $1`, id)
}

// ValidateAndConsume looks up a token by its hash, validates status/expiry/usage,
// and atomically increments used_count. Must be called within a transaction.
func (s *TokenStore) ValidateAndConsume(ctx context.Context, tx *store.Tx, tokenPlain, gatewayID string) (*TokenRecord, error) {
	hash := sha256.Sum256([]byte(tokenPlain))
	tokenHash := hex.EncodeToString(hash[:])

	var rec TokenRecord
	var labelsJSON []byte
	err := tx.QueryRowContext(ctx, store.ForUpdate(`
		SELECT id, gateway_id, token_hash, description, labels, max_uses, used_count, status, expires_at, created_at, updated_at
		FROM bootstrap_tokens WHERE token_hash = $1`), tokenHash,
	).Scan(&rec.ID, &rec.GatewayID, &rec.TokenHash, &rec.Description, &labelsJSON,
		&rec.MaxUses, &rec.UsedCount, &rec.Status, &rec.ExpiresAt, &rec.CreatedAt, &rec.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, errTokenNotFound()
	}
	if err != nil {
		return nil, fmt.Errorf("query token: %w", err)
	}
	_ = json.Unmarshal(labelsJSON, &rec.Labels)

	if rec.Status == "Revoked" {
		return nil, errTokenNotFound()
	}
	if rec.Status == "Frozen" {
		return nil, errTokenFrozen()
	}
	if time.Now().After(rec.ExpiresAt) {
		return nil, errTokenExpired()
	}
	if rec.UsedCount >= rec.MaxUses {
		return nil, errTokenExhausted()
	}
	if rec.GatewayID != gatewayID {
		return nil, errGatewayMismatch()
	}

	_, err = tx.ExecContext(ctx,
		`UPDATE bootstrap_tokens SET used_count = used_count + 1, updated_at = now() WHERE id = $1`,
		rec.ID)
	if err != nil {
		return nil, fmt.Errorf("increment used_count: %w", err)
	}
	rec.UsedCount++
	return &rec, nil
}

// UpdateStatus changes a token's status (Frozen/Revoked).
func (s *TokenStore) UpdateStatus(ctx context.Context, id, newStatus string) (*TokenRecord, error) {
	var rec TokenRecord
	var labelsJSON []byte
	err := s.db.QueryRowContext(ctx, `
		UPDATE bootstrap_tokens SET status = $1, updated_at = now()
		WHERE id = $2
		RETURNING id, gateway_id, token_hash, description, labels, max_uses, used_count, status, expires_at, created_at, updated_at`,
		newStatus, id,
	).Scan(&rec.ID, &rec.GatewayID, &rec.TokenHash, &rec.Description, &labelsJSON,
		&rec.MaxUses, &rec.UsedCount, &rec.Status, &rec.ExpiresAt, &rec.CreatedAt, &rec.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, errTokenNotFound()
	}
	if err != nil {
		return nil, fmt.Errorf("update token status: %w", err)
	}
	_ = json.Unmarshal(labelsJSON, &rec.Labels)
	return &rec, nil
}

func (s *TokenStore) scanOne(ctx context.Context, query string, args ...interface{}) (*TokenRecord, error) {
	var rec TokenRecord
	var labelsJSON []byte
	err := s.db.QueryRowContext(ctx, query, args...).Scan(
		&rec.ID, &rec.GatewayID, &rec.TokenHash, &rec.Description, &labelsJSON,
		&rec.MaxUses, &rec.UsedCount, &rec.Status, &rec.ExpiresAt, &rec.CreatedAt, &rec.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, errTokenNotFound()
	}
	if err != nil {
		return nil, fmt.Errorf("scan token: %w", err)
	}
	_ = json.Unmarshal(labelsJSON, &rec.Labels)
	return &rec, nil
}
