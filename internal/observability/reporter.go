package observability

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"

	pb "github.com/edgeai-platform/ai-edge/api/gen/go/edge/ai/api/v1"
	"github.com/edgeai-platform/ai-edge/internal/deployment"
	"github.com/edgeai-platform/ai-edge/internal/store"
)

// MetricGauge represents a gauge metric that can be set.
type MetricGauge struct {
	mu    sync.RWMutex
	value float64
}

// Set sets the gauge value.
func (g *MetricGauge) Set(v float64) {
	g.mu.Lock()
	g.value = v
	g.mu.Unlock()
}

// Get returns the current gauge value.
func (g *MetricGauge) Get() float64 {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.value
}

// MetricRegistry holds named gauge metrics for edge node observability.
// In a production setup this would integrate with prometheus/client_golang,
// but we keep it self-contained for now.
type MetricRegistry struct {
	mu     sync.RWMutex
	gauges map[string]*MetricGauge
}

// NewMetricRegistry creates a new metric registry.
func NewMetricRegistry() *MetricRegistry {
	return &MetricRegistry{
		gauges: make(map[string]*MetricGauge),
	}
}

// Gauge returns (or creates) a gauge by name.
func (r *MetricRegistry) Gauge(name string) *MetricGauge {
	r.mu.Lock()
	defer r.mu.Unlock()
	g, ok := r.gauges[name]
	if !ok {
		g = &MetricGauge{}
		r.gauges[name] = g
	}
	return g
}

// Snapshot returns a copy of all gauge values.
func (r *MetricRegistry) Snapshot() map[string]float64 {
	r.mu.RLock()
	defer r.mu.RUnlock()
	snap := make(map[string]float64, len(r.gauges))
	for k, g := range r.gauges {
		snap[k] = g.Get()
	}
	return snap
}

// Reporter handles incoming metrics and runtime state reports from edge agents.
type Reporter struct {
	registry    *MetricRegistry
	deployStore *deployment.Store
	db          *store.DB
}

// NewReporter creates a Reporter.
func NewReporter(db *store.DB) *Reporter {
	return &Reporter{
		registry:    NewMetricRegistry(),
		deployStore: deployment.NewStore(db),
		db:          db,
	}
}

// Registry returns the underlying metric registry for reading.
func (r *Reporter) Registry() *MetricRegistry {
	return r.registry
}

// HandleMetrics processes a ReportMetricsRequest. Metrics are recorded into
// the in-process gauge registry keyed by "node:<nodeID>:<metricName>".
func (r *Reporter) HandleMetrics(_ context.Context, req *pb.ReportMetricsRequest) error {
	nodeID := req.GetNodeId()
	if nodeID == "" {
		return fmt.Errorf("observability: node_id is required")
	}

	for name, value := range req.GetMetrics() {
		key := fmt.Sprintf("node:%s:%s", nodeID, name)
		r.registry.Gauge(key).Set(value)
	}

	log.Printf("observability: recorded %d metrics for node %s", len(req.GetMetrics()), nodeID)
	return nil
}

// HandleRuntimeState processes a ReportRuntimeStateRequest. Each entry is
// upserted into the edge_runtime_states table as a current snapshot.
func (r *Reporter) HandleRuntimeState(ctx context.Context, req *pb.ReportRuntimeStateRequest) error {
	nodeID := req.GetNodeId()
	if nodeID == "" {
		return fmt.Errorf("observability: node_id is required")
	}

	for _, entry := range req.GetEntries() {
		metrics, _ := json.Marshal(map[string]float64{
			"latency_p95_ms": entry.GetLatencyP95Ms(),
			"tokens_per_sec": entry.GetTokensPerSec(),
			"qps":            entry.GetQps(),
		})

		if err := r.deployStore.UpsertRuntimeState(ctx,
			nodeID,
			entry.GetModelName(),
			entry.GetModelVersion(),
			entry.GetRuntime(),
			entry.GetStatus(),
			metrics,
		); err != nil {
			log.Printf("observability: upsert runtime state for node %s model %s:%s: %v",
				nodeID, entry.GetModelName(), entry.GetModelVersion(), err)
			continue
		}
	}

	return nil
}
