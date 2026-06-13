//go:build !integration

package gateway

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	apiv1 "github.com/edgeai-platform/ai-edge/api/gen/go/edge/ai/api/v1"
	"github.com/edgeai-platform/ai-edge/internal/store"
)

func newTestIdentityCache(t *testing.T) (*IdentityCache, *store.DB) {
	t.Helper()
	resetMemDB()
	db := &store.DB{DB: newMemDB()}
	cache := NewIdentityCache(IdentityCacheConfig{
		TTL: 50 * time.Millisecond,
		DB:  db,
	})
	return cache, db
}

func TestIdentityCache_CacheHit(t *testing.T) {
	cache, _ := newTestIdentityCache(t)
	ctx := context.Background()

	sharedMemDB.setRow("fp-1", "node-1", "gw-1", "Active")

	// First call populates the cache.
	got, err := cache.Lookup(ctx, "fp-1")
	if err != nil {
		t.Fatalf("first lookup: %v", err)
	}
	if got == nil {
		t.Fatal("expected non-nil identity")
	}
	if got.NodeID != "node-1" {
		t.Fatalf("NodeID=%q", got.NodeID)
	}
	if got.GatewayID != "gw-1" {
		t.Fatalf("GatewayID=%q", got.GatewayID)
	}
	if got.Status != apiv1.IdentityStatus_IDENTITY_STATUS_ACTIVE {
		t.Fatalf("Status=%v", got.Status)
	}

	// Mutate the in-memory store and call again; the cache must serve the
	// original value, proving the hit path bypassed the DB.
	sharedMemDB.setRow("fp-1", "node-mutated", "gw-1", "Active")
	got2, err := cache.Lookup(ctx, "fp-1")
	if err != nil {
		t.Fatalf("second lookup: %v", err)
	}
	if got2.NodeID != "node-1" {
		t.Fatalf("cache hit returned mutated value: %q", got2.NodeID)
	}
}

func TestIdentityCache_CacheMissHitsDB(t *testing.T) {
	cache, _ := newTestIdentityCache(t)
	ctx := context.Background()
	sharedMemDB.setRow("fp-2", "node-2", "gw-2", "Active")

	got, err := cache.Lookup(ctx, "fp-2")
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	if got == nil {
		t.Fatal("expected non-nil identity")
	}
	if got.NodeID != "node-2" {
		t.Fatalf("NodeID=%q", got.NodeID)
	}
}

func TestIdentityCache_CacheMissNoRecord(t *testing.T) {
	cache, _ := newTestIdentityCache(t)
	ctx := context.Background()

	got, err := cache.Lookup(ctx, "fp-missing")
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil identity, got %+v", got)
	}
}

func TestIdentityCache_TTLExpiry(t *testing.T) {
	cache, _ := newTestIdentityCache(t)
	ctx := context.Background()
	sharedMemDB.setRow("fp-ttl", "node-ttl", "gw-1", "Active")

	// Populate cache.
	if _, err := cache.Lookup(ctx, "fp-ttl"); err != nil {
		t.Fatalf("first lookup: %v", err)
	}

	// Wait for TTL to expire.
	time.Sleep(80 * time.Millisecond)

	// Mutate the backing store to reflect a refresh.
	sharedMemDB.setRow("fp-ttl", "node-ttl-v2", "gw-1", "Active")
	got, err := cache.Lookup(ctx, "fp-ttl")
	if err != nil {
		t.Fatalf("post-expiry lookup: %v", err)
	}
	if got == nil {
		t.Fatal("expected non-nil identity after expiry")
	}
	if got.NodeID != "node-ttl-v2" {
		t.Fatalf("expected refresh from DB after expiry, got %q", got.NodeID)
	}
}

func TestIdentityCache_HandleEvent_Revoked(t *testing.T) {
	cache, _ := newTestIdentityCache(t)
	ctx := context.Background()
	sharedMemDB.setRow("fp-evt", "node-evt", "gw-1", "Active")

	// Populate.
	if _, err := cache.Lookup(ctx, "fp-evt"); err != nil {
		t.Fatalf("populate: %v", err)
	}

	cache.HandleIdentityEvent(&apiv1.NotifyIdentityEventRequest{
		Fingerprint: "fp-evt",
		EventType:   apiv1.IdentityEventType_IDENTITY_EVENT_TYPE_REVOKED,
	})

	// Entry must be gone from the cache; subsequent lookup should refetch.
	sharedMemDB.setRow("fp-evt", "node-evt-after", "gw-1", "Active")
	got, err := cache.Lookup(ctx, "fp-evt")
	if err != nil {
		t.Fatalf("lookup after event: %v", err)
	}
	if got == nil || got.NodeID != "node-evt-after" {
		t.Fatalf("expected refresh from DB, got %+v", got)
	}
}

func TestIdentityCache_HandleEvent_Suspended(t *testing.T) {
	cache, _ := newTestIdentityCache(t)
	ctx := context.Background()
	sharedMemDB.setRow("fp-susp", "n", "gw", "Active")

	if _, err := cache.Lookup(ctx, "fp-susp"); err != nil {
		t.Fatalf("populate: %v", err)
	}
	cache.HandleIdentityEvent(&apiv1.NotifyIdentityEventRequest{
		Fingerprint: "fp-susp",
		EventType:   apiv1.IdentityEventType_IDENTITY_EVENT_TYPE_SUSPENDED,
	})
	sharedMemDB.setRow("fp-susp", "n-after", "gw", "Active")
	got, _ := cache.Lookup(ctx, "fp-susp")
	if got == nil || got.NodeID != "n-after" {
		t.Fatalf("suspended event did not invalidate: %+v", got)
	}
}

func TestIdentityCache_HandleEvent_Renewed(t *testing.T) {
	cache, _ := newTestIdentityCache(t)
	ctx := context.Background()
	sharedMemDB.setRow("fp-renew", "n-old", "gw", "Active")

	if _, err := cache.Lookup(ctx, "fp-renew"); err != nil {
		t.Fatalf("populate: %v", err)
	}
	cache.HandleIdentityEvent(&apiv1.NotifyIdentityEventRequest{
		Fingerprint: "fp-renew",
		EventType:   apiv1.IdentityEventType_IDENTITY_EVENT_TYPE_RENEWED,
	})
	sharedMemDB.setRow("fp-renew", "n-new", "gw", "Active")
	got, _ := cache.Lookup(ctx, "fp-renew")
	if got == nil || got.NodeID != "n-new" {
		t.Fatalf("renewed event did not invalidate: %+v", got)
	}
}

func TestIdentityCache_HandleEvent_UnknownType(t *testing.T) {
	// Unknown event types must not corrupt the cache; the existing entry
	// should be served as before.
	cache, _ := newTestIdentityCache(t)
	ctx := context.Background()
	sharedMemDB.setRow("fp-unk", "n", "gw", "Active")

	if _, err := cache.Lookup(ctx, "fp-unk"); err != nil {
		t.Fatalf("populate: %v", err)
	}
	cache.HandleIdentityEvent(&apiv1.NotifyIdentityEventRequest{
		Fingerprint: "fp-unk",
		EventType:   apiv1.IdentityEventType_IDENTITY_EVENT_TYPE_UNSPECIFIED,
	})

	sharedMemDB.setRow("fp-unk", "n-mutated", "gw", "Active")
	got, _ := cache.Lookup(ctx, "fp-unk")
	if got == nil || got.NodeID != "n" {
		t.Fatalf("unknown event must not invalidate; got %+v", got)
	}
}

func TestIdentityCache_Invalidate(t *testing.T) {
	cache, _ := newTestIdentityCache(t)
	ctx := context.Background()
	sharedMemDB.setRow("fp-inv", "n", "gw", "Active")

	if _, err := cache.Lookup(ctx, "fp-inv"); err != nil {
		t.Fatalf("populate: %v", err)
	}
	cache.Invalidate("fp-inv")

	sharedMemDB.setRow("fp-inv", "n-new", "gw", "Active")
	got, _ := cache.Lookup(ctx, "fp-inv")
	if got == nil || got.NodeID != "n-new" {
		t.Fatalf("Invalidate did not refresh: %+v", got)
	}
}

func TestIdentityCache_DBError(t *testing.T) {
	cache, _ := newTestIdentityCache(t)
	sharedMemDB.setFailOnce()

	_, err := cache.Lookup(context.Background(), "fp-fail")
	if err == nil {
		t.Fatal("expected error from forced DB failure")
	}
	// The error should be the raw driver error (IdentityCache.loadFromDB
	// only translates sql.ErrNoRows; everything else bubbles up).
	if !contains(err.Error(), "forced failure") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestIdentityCache_ConcurrentLookupAndInvalidate(t *testing.T) {
	cache, _ := newTestIdentityCache(t)
	ctx := context.Background()

	// Pre-populate.
	sharedMemDB.setRow("fp-conc", "n", "gw", "Active")
	if _, err := cache.Lookup(ctx, "fp-conc"); err != nil {
		t.Fatalf("populate: %v", err)
	}

	const goroutines = 32
	const iterations = 200

	var wg sync.WaitGroup
	wg.Add(goroutines * 2)

	// Readers.
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				_, _ = cache.Lookup(ctx, "fp-conc")
			}
		}()
	}

	// Writers.
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				cache.Invalidate("fp-conc")
			}
		}()
	}

	wg.Wait()
}

func TestIdentityCache_HandleEvent_NilRequest(t *testing.T) {
	// Nil request must be handled defensively (no panic).
	cache, _ := newTestIdentityCache(t)
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("nil request panicked: %v", r)
		}
	}()
	cache.HandleIdentityEvent(nil)
}

func TestIdentityCache_EmptyFingerprint(t *testing.T) {
	cache, _ := newTestIdentityCache(t)
	// A blank fingerprint should be a normal DB miss, not a crash.
	got, err := cache.Lookup(context.Background(), "")
	if err != nil && !errors.Is(err, context.Canceled) {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil for empty fingerprint, got %+v", got)
	}
}

// contains is a tiny local helper so this test file does not depend on
// the strings package for a single substring check.
func contains(s, sub string) bool {
	if sub == "" {
		return true
	}
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
