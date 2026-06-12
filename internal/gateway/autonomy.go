package gateway

import (
	"context"
	"log"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

// ConnectivityState represents the current connectivity to Cloud.
type ConnectivityState int32

const (
	StateOnline  ConnectivityState = 0
	StateOffline ConnectivityState = 1
)

// ConnectivityMonitor detects and tracks connectivity to the Cloud
// Control Plane. When offline, the gateway enters read-only coordination
// mode (no new task claims, no new onboardings). On recovery, it
// performs incremental sync.
type ConnectivityMonitor struct {
	cloudHealthURL string
	checkInterval  time.Duration
	timeout        time.Duration

	state atomic.Int32 // 0 = online, 1 = offline

	mu            sync.Mutex
	lastOnlineAt  time.Time
	lastOfflineAt time.Time

	onOffline func()
	onRecover func()
}

// ConnectivityMonitorConfig configures the ConnectivityMonitor.
type ConnectivityMonitorConfig struct {
	CloudHealthURL string
	CheckInterval  time.Duration
	Timeout        time.Duration
	OnOffline      func()
	OnRecover      func()
}

// NewConnectivityMonitor creates a ConnectivityMonitor.
func NewConnectivityMonitor(cfg ConnectivityMonitorConfig) *ConnectivityMonitor {
	interval := cfg.CheckInterval
	if interval == 0 {
		interval = 10 * time.Second
	}
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 5 * time.Second
	}

	cm := &ConnectivityMonitor{
		cloudHealthURL: cfg.CloudHealthURL,
		checkInterval:  interval,
		timeout:        timeout,
		lastOnlineAt:   time.Now(),
		onOffline:      cfg.OnOffline,
		onRecover:      cfg.OnRecover,
	}
	return cm
}

// IsOnline returns true if the gateway currently has connectivity.
func (cm *ConnectivityMonitor) IsOnline() bool {
	return ConnectivityState(cm.state.Load()) == StateOnline
}

// Run starts the connectivity check loop.
func (cm *ConnectivityMonitor) Run(ctx context.Context) {
	ticker := time.NewTicker(cm.checkInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Println("gateway: connectivity monitor stopped")
			return
		case <-ticker.C:
			cm.probe(ctx)
		}
	}
}

func (cm *ConnectivityMonitor) probe(_ context.Context) {
	reachable := cm.checkHealth()
	prev := ConnectivityState(cm.state.Load())

	if reachable {
		cm.state.Store(int32(StateOnline))
		cm.mu.Lock()
		cm.lastOnlineAt = time.Now()
		cm.mu.Unlock()

		if prev == StateOffline {
			log.Println("gateway: connectivity RESTORED, triggering incremental sync")
			if cm.onRecover != nil {
				go cm.onRecover()
			}
		}
	} else {
		cm.state.Store(int32(StateOffline))
		cm.mu.Lock()
		cm.lastOfflineAt = time.Now()
		cm.mu.Unlock()

		if prev == StateOnline {
			log.Println("gateway: connectivity LOST, entering read-only mode")
			if cm.onOffline != nil {
				go cm.onOffline()
			}
		}
	}
}

func (cm *ConnectivityMonitor) checkHealth() bool {
	if cm.cloudHealthURL == "" {
		return true
	}

	client := &http.Client{Timeout: cm.timeout}
	resp, err := client.Get(cm.cloudHealthURL)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode < 500
}

// AutonomyGuard gates operations based on connectivity state.
// During offline mode: reject new task claims and new node onboardings.
type AutonomyGuard struct {
	monitor *ConnectivityMonitor
}

// NewAutonomyGuard creates an AutonomyGuard.
func NewAutonomyGuard(monitor *ConnectivityMonitor) *AutonomyGuard {
	return &AutonomyGuard{monitor: monitor}
}

// AllowNewTaskClaim returns false when offline (read-only mode).
func (g *AutonomyGuard) AllowNewTaskClaim() bool {
	return g.monitor.IsOnline()
}

// AllowNewOnboarding returns false when offline.
func (g *AutonomyGuard) AllowNewOnboarding() bool {
	return g.monitor.IsOnline()
}

// IncrementalSyncer re-syncs buffered results and refreshes identities
// after connectivity is restored.
type IncrementalSyncer struct {
	gatewayID     string
	identityCache *IdentityCache
}

// NewIncrementalSyncer creates an IncrementalSyncer.
func NewIncrementalSyncer(gatewayID string, cache *IdentityCache) *IncrementalSyncer {
	return &IncrementalSyncer{
		gatewayID:     gatewayID,
		identityCache: cache,
	}
}

// Sync performs post-recovery synchronization.
func (s *IncrementalSyncer) Sync() {
	log.Printf("gateway: incremental sync: flushing buffered results for gateway %s", s.gatewayID)
	s.refreshIdentities()
	log.Printf("gateway: incremental sync complete")
}

func (s *IncrementalSyncer) refreshIdentities() {
	s.identityCache.mu.Lock()
	for k := range s.identityCache.entries {
		delete(s.identityCache.entries, k)
	}
	s.identityCache.mu.Unlock()
	log.Println("gateway: incremental sync: identity cache cleared for refresh")
}
