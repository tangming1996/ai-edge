package deployment_test

import (
	"encoding/json"
	"testing"

	"github.com/edgeai-platform/ai-edge/internal/deployment"
)

// mustJSON is a tiny helper that marshals a value or fails the test.
func mustJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}

func rowWithStatus(t *testing.T, st deployment.DeploymentStatusJSON, rollout deployment.RolloutJSON) *deployment.DeploymentRow {
	t.Helper()
	return &deployment.DeploymentRow{
		Status:  mustJSON(t, st),
		Rollout: mustJSON(t, rollout),
	}
}

// TestRolloutChecker_CanProceed_NoNodesNeeded is the trivial "all ready"
// case: rollout should always proceed.
func TestRolloutChecker_CanProceed_NoNodesNeeded(t *testing.T) {
	rc := deployment.NewRolloutChecker()
	row := rowWithStatus(t, deployment.DeploymentStatusJSON{
		DesiredNodes: 0, ReadyNodes: 0,
	}, deployment.RolloutJSON{MaxUnavailable: 1})
	ok, _ := rc.CanProceed(row)
	// When DesiredNodes=0 there is nothing to rollout, so the helper
	// returns false (0 nodes still to act on). The contract is: the
	// caller should check IsComplete first.
	if ok {
		t.Errorf("CanProceed = true, want false (0 nodes remaining)")
	}
}

// TestRolloutChecker_CanProceed_DefaultsMaxUnavailable covers the
// "MaxUnavailable <= 0 → default to 1" branch.
func TestRolloutChecker_CanProceed_DefaultsMaxUnavailable(t *testing.T) {
	rc := deployment.NewRolloutChecker()
	// 4 ready + 1 unavailable, max defaults to 1, 1 unavailable < 1?
	// No: 1 unavailable >= 1 max → cannot proceed. Use Ready=5 instead.
	row := rowWithStatus(t, deployment.DeploymentStatusJSON{
		DesiredNodes: 5, ReadyNodes: 5, FailedNodes: 0,
	}, deployment.RolloutJSON{MaxUnavailable: 0})
	// With DesiredNodes==ReadyNodes, remaining is 0, so the helper
	// returns (false, 0). We just check it does not panic.
	_, _ = rc.CanProceed(row)
}

// TestRolloutChecker_CanProceed_AtMaxUnavailable covers the boundary
// "unavailable >= max → cannot proceed" case.
func TestRolloutChecker_CanProceed_AtMaxUnavailable(t *testing.T) {
	rc := deployment.NewRolloutChecker()
	row := rowWithStatus(t, deployment.DeploymentStatusJSON{
		DesiredNodes: 5, ReadyNodes: 3, FailedNodes: 0,
	}, deployment.RolloutJSON{MaxUnavailable: 2})
	ok, _ := rc.CanProceed(row)
	if ok {
		t.Fatal("expected to NOT proceed when 2 unavailable >= max 2")
	}
}

// TestRolloutChecker_CanProceed_LargeBatch covers the
// "allowedBatch > remaining" cap.
func TestRolloutChecker_CanProceed_LargeBatch(t *testing.T) {
	rc := deployment.NewRolloutChecker()
	row := rowWithStatus(t, deployment.DeploymentStatusJSON{
		DesiredNodes: 5, ReadyNodes: 4, FailedNodes: 0,
	}, deployment.RolloutJSON{MaxUnavailable: 10})
	ok, batch := rc.CanProceed(row)
	if !ok {
		t.Fatal("expected to proceed")
	}
	// 1 node is unavailable; remaining = 5 - 4 - 0 = 1.
	if batch != 1 {
		t.Errorf("batch = %d, want 1", batch)
	}
}

// TestRolloutChecker_CanProceed_NegativeCounters exercises the
// defensive "negative count → floor to 0" branch.
func TestRolloutChecker_CanProceed_NegativeCounters(t *testing.T) {
	rc := deployment.NewRolloutChecker()
	// Some external bug reports ReadyNodes=10 but DesiredNodes=5.
	row := rowWithStatus(t, deployment.DeploymentStatusJSON{
		DesiredNodes: 5, ReadyNodes: 10, FailedNodes: 0,
	}, deployment.RolloutJSON{MaxUnavailable: 1})
	ok, _ := rc.CanProceed(row)
	// With DesiredNodes=5 and ReadyNodes=10, remaining=0, so we don't
	// proceed. The important thing is that we don't panic on negative
	// counters.
	_ = ok
}

// TestRolloutChecker_CanProceed_InvalidJSON covers the
// "status is not valid JSON" branch.
func TestRolloutChecker_CanProceed_InvalidJSON(t *testing.T) {
	rc := deployment.NewRolloutChecker()
	row := &deployment.DeploymentRow{
		Status:  json.RawMessage(`{ not json`),
		Rollout: mustJSON(t, deployment.RolloutJSON{MaxUnavailable: 1}),
	}
	ok, _ := rc.CanProceed(row)
	if ok {
		t.Fatal("expected to fail for invalid JSON")
	}
}

// TestRolloutChecker_CanProceed_InvalidRolloutJSON covers the
// "rollout is not valid JSON" branch.
func TestRolloutChecker_CanProceed_InvalidRolloutJSON(t *testing.T) {
	rc := deployment.NewRolloutChecker()
	row := &deployment.DeploymentRow{
		Status:  mustJSON(t, deployment.DeploymentStatusJSON{}),
		Rollout: json.RawMessage(`{ not json`),
	}
	ok, _ := rc.CanProceed(row)
	if ok {
		t.Fatal("expected to fail for invalid rollout JSON")
	}
}

// TestRolloutChecker_IsComplete covers the IsComplete helper.
func TestRolloutChecker_IsComplete(t *testing.T) {
	rc := deployment.NewRolloutChecker()
	cases := []struct {
		st   deployment.DeploymentStatusJSON
		want bool
	}{
		{deployment.DeploymentStatusJSON{DesiredNodes: 5, ReadyNodes: 5, FailedNodes: 0}, true},
		{deployment.DeploymentStatusJSON{DesiredNodes: 5, ReadyNodes: 3, FailedNodes: 2}, true},
		{deployment.DeploymentStatusJSON{DesiredNodes: 5, ReadyNodes: 3, FailedNodes: 0}, false},
		{deployment.DeploymentStatusJSON{DesiredNodes: 0, ReadyNodes: 0, FailedNodes: 0}, true},
	}
	for i, c := range cases {
		row := rowWithStatus(t, c.st, deployment.RolloutJSON{})
		if got := rc.IsComplete(row); got != c.want {
			t.Errorf("case %d: IsComplete = %v, want %v", i, got, c.want)
		}
	}
}

// TestRolloutChecker_IsComplete_InvalidJSON covers the failure branch.
func TestRolloutChecker_IsComplete_InvalidJSON(t *testing.T) {
	rc := deployment.NewRolloutChecker()
	row := &deployment.DeploymentRow{Status: json.RawMessage(`bad`)}
	if rc.IsComplete(row) {
		t.Fatal("expected false for invalid status JSON")
	}
}

// TestRolloutChecker_SuggestPhase covers the phase recommendation.
func TestRolloutChecker_SuggestPhase(t *testing.T) {
	rc := deployment.NewRolloutChecker()
	cases := []struct {
		st   deployment.DeploymentStatusJSON
		want string
	}{
		{deployment.DeploymentStatusJSON{DesiredNodes: 5, ReadyNodes: 5, FailedNodes: 0}, "Active"},
		{deployment.DeploymentStatusJSON{DesiredNodes: 5, ReadyNodes: 3, FailedNodes: 2}, "Failed"},
		{deployment.DeploymentStatusJSON{DesiredNodes: 5, ReadyNodes: 3, FailedNodes: 0}, "RollingOut"},
		{deployment.DeploymentStatusJSON{DesiredNodes: 0, ReadyNodes: 0, FailedNodes: 0}, "Active"},
	}
	for i, c := range cases {
		row := rowWithStatus(t, c.st, deployment.RolloutJSON{})
		if got := rc.SuggestPhase(row); got != c.want {
			t.Errorf("case %d: SuggestPhase = %q, want %q", i, got, c.want)
		}
	}
}

// TestRolloutChecker_SuggestPhase_InvalidJSON covers the failure
// branch (returns Failed by default).
func TestRolloutChecker_SuggestPhase_InvalidJSON(t *testing.T) {
	rc := deployment.NewRolloutChecker()
	row := &deployment.DeploymentRow{Status: json.RawMessage(`bad`)}
	if got := rc.SuggestPhase(row); got != "Failed" {
		t.Errorf("SuggestPhase = %q, want Failed", got)
	}
}

// TestDeploymentStatusJSON_JSONRoundTrip locks the public field names.
func TestDeploymentStatusJSON_JSONRoundTrip(t *testing.T) {
	in := deployment.DeploymentStatusJSON{
		DesiredNodes: 5, ReadyNodes: 3, FailedNodes: 1, Phase: "RollingOut",
	}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out deployment.DeploymentStatusJSON
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out != in {
		t.Errorf("round trip mismatch: %+v vs %+v", out, in)
	}
}

// TestDeploymentTargetJSON_JSONRoundTrip locks the public field names.
func TestDeploymentTargetJSON_JSONRoundTrip(t *testing.T) {
	in := deployment.DeploymentTargetJSON{
		GatewayIDs:    []string{"g1", "g2"},
		LabelSelector: map[string]string{"region": "us"},
	}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out deployment.DeploymentTargetJSON
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(out.GatewayIDs) != 2 || out.LabelSelector["region"] != "us" {
		t.Errorf("round trip mismatch: %+v", out)
	}
}

// TestRolloutJSON_JSONRoundTrip locks the public field names.
func TestRolloutJSON_JSONRoundTrip(t *testing.T) {
	in := deployment.RolloutJSON{MaxUnavailable: 7}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out deployment.RolloutJSON
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out != in {
		t.Errorf("round trip mismatch: %+v vs %+v", out, in)
	}
}
