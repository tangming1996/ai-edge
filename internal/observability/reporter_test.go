package observability_test

import (
	"sync"
	"testing"

	pb "github.com/edgeai-platform/ai-edge/api/gen/go/edge/ai/api/v1"
	"github.com/edgeai-platform/ai-edge/internal/observability"
)

// metricRequest is a tiny helper that builds a ReportMetricsRequest.
func metricRequest(nodeID string, metrics map[string]float64) *pb.ReportMetricsRequest {
	return &pb.ReportMetricsRequest{
		NodeId:  nodeID,
		Metrics: metrics,
	}
}

// TestMetricGauge_SetGet covers the basic gauge set/get.
func TestMetricGauge_SetGet(t *testing.T) {
	var g observability.MetricGauge
	g.Set(42.5)
	if got := g.Get(); got != 42.5 {
		t.Errorf("Get = %v, want 42.5", got)
	}
}

// TestMetricGauge_ConcurrentSetGet exercises the gauge under
// concurrent access (race detector will flag any unsynchronised access).
func TestMetricGauge_ConcurrentSetGet(t *testing.T) {
	var g observability.MetricGauge
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			g.Set(float64(i))
			_ = g.Get()
		}(i)
	}
	wg.Wait()
}

// TestMetricRegistry_GaugeCachesByName covers the
// "second call returns the same gauge instance" contract.
func TestMetricRegistry_GaugeCachesByName(t *testing.T) {
	r := observability.NewMetricRegistry()
	g1 := r.Gauge("cpu")
	g2 := r.Gauge("cpu")
	if g1 != g2 {
		t.Errorf("Gauge() returned different instances for the same name")
	}
	g3 := r.Gauge("mem")
	if g1 == g3 {
		t.Errorf("Gauge() returned the same instance for different names")
	}
}

// TestMetricRegistry_Snapshot covers the Snapshot helper.
func TestMetricRegistry_Snapshot(t *testing.T) {
	r := observability.NewMetricRegistry()
	r.Gauge("a").Set(1)
	r.Gauge("b").Set(2)
	snap := r.Snapshot()
	if len(snap) != 2 {
		t.Errorf("snapshot size = %d, want 2", len(snap))
	}
	if snap["a"] != 1 || snap["b"] != 2 {
		t.Errorf("snapshot content = %+v", snap)
	}
}

// TestMetricRegistry_Snapshot_Empty covers the empty-registry case.
func TestMetricRegistry_Snapshot_Empty(t *testing.T) {
	r := observability.NewMetricRegistry()
	snap := r.Snapshot()
	if len(snap) != 0 {
		t.Errorf("empty snapshot size = %d, want 0", len(snap))
	}
}

// TestMetricRegistry_Snapshot_IsCopy covers the "snapshot is a copy"
// contract — mutating the original gauges after a snapshot should not
// affect the previously taken snapshot values.
func TestMetricRegistry_Snapshot_IsCopy(t *testing.T) {
	r := observability.NewMetricRegistry()
	r.Gauge("a").Set(1)
	snap := r.Snapshot()
	r.Gauge("a").Set(99)
	if snap["a"] != 1 {
		t.Errorf("snapshot mutated: %v", snap["a"])
	}
}

// TestReporter_Registry covers the Registry accessor.
func TestReporter_Registry(t *testing.T) {
	r := observability.NewReporter(nil)
	if r == nil {
		t.Fatal("nil reporter")
	}
	if r.Registry() == nil {
		t.Fatal("nil registry")
	}
}

// TestReporter_HandleMetrics_MissingNodeID covers the validation
// branch: a request with no NodeId must return an error.
func TestReporter_HandleMetrics_MissingNodeID(t *testing.T) {
	r := observability.NewReporter(nil)
	if err := r.HandleMetrics(t.Context(), nil); err == nil {
		t.Fatal("expected error for missing node_id")
	}
}

// TestReporter_HandleMetrics_HappyPath covers the happy path:
// metrics are recorded into the in-process registry, keyed by
// "node:<nodeID>:<metricName>".
func TestReporter_HandleMetrics_HappyPath(t *testing.T) {
	r := observability.NewReporter(nil)
	req := metricRequest("node-1", map[string]float64{
		"cpu": 50.0,
		"mem": 80.0,
	})
	if err := r.HandleMetrics(t.Context(), req); err != nil {
		t.Fatalf("HandleMetrics: %v", err)
	}
	snap := r.Registry().Snapshot()
	if snap["node:node-1:cpu"] != 50.0 {
		t.Errorf("cpu = %v", snap["node:node-1:cpu"])
	}
	if snap["node:node-1:mem"] != 80.0 {
		t.Errorf("mem = %v", snap["node:node-1:mem"])
	}
}

// TestReporter_HandleMetrics_EmptyMetrics covers the "no metrics to
// record" branch — should still succeed and not pollute the registry.
func TestReporter_HandleMetrics_EmptyMetrics(t *testing.T) {
	r := observability.NewReporter(nil)
	req := metricRequest("n-1", nil)
	if err := r.HandleMetrics(t.Context(), req); err != nil {
		t.Fatalf("HandleMetrics: %v", err)
	}
	if len(r.Registry().Snapshot()) != 0 {
		t.Errorf("empty metrics must not record anything")
	}
}

// TestReporter_HandleRuntimeState_MissingNodeID covers the validation
// branch.
func TestReporter_HandleRuntimeState_MissingNodeID(t *testing.T) {
	r := observability.NewReporter(nil)
	if err := r.HandleRuntimeState(t.Context(), nil); err == nil {
		t.Fatal("expected error for missing node_id")
	}
}

// TestReporter_HandleRuntimeState_Empty covers the no-entries branch.
func TestReporter_HandleRuntimeState_Empty(t *testing.T) {
	r := observability.NewReporter(nil)
	req := &pb.ReportRuntimeStateRequest{NodeId: "n-1"}
	if err := r.HandleRuntimeState(t.Context(), req); err != nil {
		t.Fatalf("HandleRuntimeState: %v", err)
	}
}
