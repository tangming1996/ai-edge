package gateway

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestConnectivityState_DefaultOnline(t *testing.T) {
	cm := NewConnectivityMonitor(ConnectivityMonitorConfig{})
	if !cm.IsOnline() {
		t.Fatal("default state must be online")
	}
}

func TestConnectivityMonitor_DefaultsApplied(t *testing.T) {
	// A monitor with zero config must still produce a usable object
	// and have sensible default intervals.
	cm := NewConnectivityMonitor(ConnectivityMonitorConfig{})
	if cm.checkInterval != 10*time.Second {
		t.Errorf("default check interval = %s, want 10s", cm.checkInterval)
	}
	if cm.timeout != 5*time.Second {
		t.Errorf("default timeout = %s, want 5s", cm.timeout)
	}
	if cm.lastOnlineAt.IsZero() {
		t.Error("lastOnlineAt should be set on construction")
	}
}

func TestConnectivityMonitor_NoURL_Healthy(t *testing.T) {
	// An empty health URL means the gateway is always considered online.
	cm := NewConnectivityMonitor(ConnectivityMonitorConfig{})
	cm.probe(context.Background())
	if !cm.IsOnline() {
		t.Fatal("empty URL should keep state online")
	}
}

func TestConnectivityMonitor_HealthyURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cm := NewConnectivityMonitor(ConnectivityMonitorConfig{
		CloudHealthURL: srv.URL,
		Timeout:        1 * time.Second,
	})
	cm.probe(context.Background())
	if !cm.IsOnline() {
		t.Fatal("2xx should be online")
	}
}

func TestConnectivityMonitor_UnhealthyURL_5xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	cm := NewConnectivityMonitor(ConnectivityMonitorConfig{
		CloudHealthURL: srv.URL,
		Timeout:        1 * time.Second,
	})
	cm.probe(context.Background())
	if cm.IsOnline() {
		t.Fatal("5xx should mark state offline")
	}
}

func TestConnectivityMonitor_UnhealthyURL_ConnectionError(t *testing.T) {
	// A URL that we immediately close so the Get call fails.
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	srv.Close() // closed immediately

	cm := NewConnectivityMonitor(ConnectivityMonitorConfig{
		CloudHealthURL: srv.URL,
		Timeout:        200 * time.Millisecond,
	})
	cm.probe(context.Background())
	if cm.IsOnline() {
		t.Fatal("connection failure should mark state offline")
	}
}

func TestConnectivityMonitor_RecoversFromOffline(t *testing.T) {
	var healthy atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if healthy.Load() {
			w.WriteHeader(http.StatusOK)
		} else {
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	defer srv.Close()

	cm := NewConnectivityMonitor(ConnectivityMonitorConfig{
		CloudHealthURL: srv.URL,
		Timeout:        1 * time.Second,
	})
	// First probe while unhealthy.
	cm.probe(context.Background())
	if cm.IsOnline() {
		t.Fatal("5xx must mark offline")
	}
	// Now flip the server to healthy and probe again.
	healthy.Store(true)
	cm.probe(context.Background())
	if !cm.IsOnline() {
		t.Fatal("after recovery the state must be online")
	}
}

func TestConnectivityMonitor_RunStopsOnContextCancel(t *testing.T) {
	cm := NewConnectivityMonitor(ConnectivityMonitorConfig{
		CheckInterval: 1 * time.Millisecond,
	})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		cm.Run(ctx)
		close(done)
	}()
	cancel()
	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Run did not stop after context cancel")
	}
}

func TestAutonomyGuard_Online(t *testing.T) {
	cm := NewConnectivityMonitor(ConnectivityMonitorConfig{}) // online by default
	g := NewAutonomyGuard(cm)
	if !g.AllowNewTaskClaim() {
		t.Fatal("online should allow new task claim")
	}
	if !g.AllowNewOnboarding() {
		t.Fatal("online should allow new onboarding")
	}
}

func TestAutonomyGuard_Offline(t *testing.T) {
	cm := NewConnectivityMonitor(ConnectivityMonitorConfig{})
	cm.state.Store(int32(StateOffline)) // force offline
	g := NewAutonomyGuard(cm)
	if g.AllowNewTaskClaim() {
		t.Fatal("offline must reject new task claim")
	}
	if g.AllowNewOnboarding() {
		t.Fatal("offline must reject new onboarding")
	}
}

func TestIncrementalSyncer_NewClearsCache(t *testing.T) {
	cache := NewIdentityCache(IdentityCacheConfig{TTL: 1 * time.Minute})
	cache.entries["fp-1"] = &CachedIdentity{NodeID: "n", GatewayID: "gw"}
	cache.entries["fp-2"] = &CachedIdentity{NodeID: "n", GatewayID: "gw"}

	syncer := NewIncrementalSyncer("gw-1", cache)
	syncer.Sync()

	if len(cache.entries) != 0 {
		t.Fatalf("expected cache cleared, got %d entries", len(cache.entries))
	}
}

func TestIncrementalSyncer_EmptyCache(t *testing.T) {
	cache := NewIdentityCache(IdentityCacheConfig{TTL: 1 * time.Minute})
	syncer := NewIncrementalSyncer("gw-1", cache)
	// Must not panic on an empty cache.
	syncer.Sync()
}
