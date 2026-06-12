package gateway

import (
	"context"
	"database/sql"
	"sync"
	"time"

	apiv1 "github.com/edgeai-platform/ai-edge/api/gen/go/edge/ai/api/v1"
	"github.com/edgeai-platform/ai-edge/internal/store"
)

// CachedIdentity holds a cached identity entry.
type CachedIdentity struct {
	NodeID    string
	GatewayID string
	Status    apiv1.IdentityStatus
	ExpireAt  time.Time
}

// IdentityCache provides a TTL-based in-memory cache for edge identity lookup
// by certificate fingerprint. It supports event-driven invalidation via
// NotifyIdentityEvent and falls back to DB on cache miss.
type IdentityCache struct {
	mu      sync.RWMutex
	entries map[string]*CachedIdentity

	ttl time.Duration
	db  *store.DB
}

// IdentityCacheConfig configures the cache.
type IdentityCacheConfig struct {
	TTL time.Duration
	DB  *store.DB
}

// NewIdentityCache creates a new IdentityCache.
func NewIdentityCache(cfg IdentityCacheConfig) *IdentityCache {
	ttl := cfg.TTL
	if ttl == 0 {
		ttl = 30 * time.Second
	}
	return &IdentityCache{
		entries: make(map[string]*CachedIdentity),
		ttl:     ttl,
		db:      cfg.DB,
	}
}

// Lookup retrieves identity info for the given fingerprint.
// Returns nil if identity not found or status is not Active.
func (c *IdentityCache) Lookup(ctx context.Context, fingerprint string) (*CachedIdentity, error) {
	c.mu.RLock()
	entry, ok := c.entries[fingerprint]
	c.mu.RUnlock()

	if ok && time.Now().Before(entry.ExpireAt) {
		return entry, nil
	}

	return c.loadFromDB(ctx, fingerprint)
}

func (c *IdentityCache) loadFromDB(ctx context.Context, fingerprint string) (*CachedIdentity, error) {
	const q = `SELECT node_id, gateway_id, status FROM edge_identities WHERE fingerprint = $1 AND status = 'Active' LIMIT 1`

	var nodeID, gatewayID, status string
	err := c.db.QueryRowContext(ctx, q, fingerprint).Scan(&nodeID, &gatewayID, &status)
	if err == sql.ErrNoRows {
		c.mu.Lock()
		delete(c.entries, fingerprint)
		c.mu.Unlock()
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	entry := &CachedIdentity{
		NodeID:    nodeID,
		GatewayID: gatewayID,
		Status:    apiv1.IdentityStatus_IDENTITY_STATUS_ACTIVE,
		ExpireAt:  time.Now().Add(c.ttl),
	}

	c.mu.Lock()
	c.entries[fingerprint] = entry
	c.mu.Unlock()

	return entry, nil
}

// HandleIdentityEvent processes a NotifyIdentityEvent push from Control Plane.
// Revoke/suspend events immediately remove the cache entry.
func (c *IdentityCache) HandleIdentityEvent(req *apiv1.NotifyIdentityEventRequest) {
	switch req.GetEventType() {
	case apiv1.IdentityEventType_IDENTITY_EVENT_TYPE_REVOKED,
		apiv1.IdentityEventType_IDENTITY_EVENT_TYPE_SUSPENDED:
		c.mu.Lock()
		delete(c.entries, req.GetFingerprint())
		c.mu.Unlock()
	case apiv1.IdentityEventType_IDENTITY_EVENT_TYPE_RENEWED:
		c.mu.Lock()
		delete(c.entries, req.GetFingerprint())
		c.mu.Unlock()
	}
}

// Invalidate removes a specific fingerprint from the cache.
func (c *IdentityCache) Invalidate(fingerprint string) {
	c.mu.Lock()
	delete(c.entries, fingerprint)
	c.mu.Unlock()
}
