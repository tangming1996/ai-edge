//go:build !integration

package deployment_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"database/sql/driver"

	pb "github.com/edgeai-platform/ai-edge/api/gen/go/edge/ai/api/v1"
	"github.com/edgeai-platform/ai-edge/internal/deployment"
	"github.com/edgeai-platform/ai-edge/internal/store"
)

func rowD(v ...driver.Value) []driver.Value { return v }

func newMemDeployment(t *testing.T) *deployment.Service {
	t.Helper()
	store.ResetMemDB()
	db := store.NewMemStore()
	return deployment.NewService(db)
}

func deploymentRow(id string) []driver.Value {
	return rowD(
		id, "model-a", "v1",
		[]byte(`{"gateway_ids":["gw-1"]}`),
		"auto",
		[]byte(`{"max_unavailable":1}`),
		[]byte(`{"phase":"Pending","desired_nodes":3,"ready_nodes":0,"failed_nodes":0}`),
		time.Date(2026, 6, 13, 12, 0, 0, 0, time.UTC),
		time.Date(2026, 6, 13, 12, 0, 0, 0, time.UTC),
	)
}

func TestService_CreateDeployment_HappyPath(t *testing.T) {
	svc := newMemDeployment(t)
	store.SetRowForQuery("INSERT INTO model_deployments", rowD(
		"d-1",
		time.Date(2026, 6, 13, 12, 0, 0, 0, time.UTC),
		time.Date(2026, 6, 13, 12, 0, 0, 0, time.UTC),
	))
	resp, err := svc.CreateDeployment(context.Background(), &pb.CreateDeploymentRequest{
		ModelName: "model-a", ModelVersion: "v1",
	})
	if err != nil {
		t.Fatalf("CreateDeployment: %v", err)
	}
	if resp.GetDeployment().GetId() != "d-1" {
		t.Errorf("id = %q", resp.GetDeployment().GetId())
	}
}

func TestService_CreateDeployment_MissingName(t *testing.T) {
	svc := newMemDeployment(t)
	_, err := svc.CreateDeployment(context.Background(), &pb.CreateDeploymentRequest{ModelVersion: "v1"})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestService_CreateDeployment_MissingVersion(t *testing.T) {
	svc := newMemDeployment(t)
	_, err := svc.CreateDeployment(context.Background(), &pb.CreateDeploymentRequest{ModelName: "m"})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestService_CreateDeployment_WithTarget(t *testing.T) {
	svc := newMemDeployment(t)
	store.SetRowForQuery("INSERT INTO model_deployments", rowD(
		"d-1", time.Now(), time.Now(),
	))
	resp, err := svc.CreateDeployment(context.Background(), &pb.CreateDeploymentRequest{
		ModelName: "m", ModelVersion: "v",
		Target: &pb.DeploymentTarget{
			GatewayIds: []string{"gw-1"},
			LabelSelector: &pb.LabelSelector{
				MatchLabels: map[string]string{"region": "us"},
			},
		},
		Rollout: &pb.RolloutConfig{MaxUnavailable: 5},
	})
	if err != nil {
		t.Fatalf("CreateDeployment: %v", err)
	}
	if len(resp.GetDeployment().GetTarget().GetGatewayIds()) != 1 {
		t.Errorf("gateway_ids = %v", resp.GetDeployment().GetTarget().GetGatewayIds())
	}
	if resp.GetDeployment().GetRollout().GetMaxUnavailable() != 5 {
		t.Errorf("max_unavailable = %d", resp.GetDeployment().GetRollout().GetMaxUnavailable())
	}
}

func TestService_CreateDeployment_DefaultRuntime(t *testing.T) {
	svc := newMemDeployment(t)
	store.SetRowForQuery("INSERT INTO model_deployments", rowD("d-1", time.Now(), time.Now()))
	resp, err := svc.CreateDeployment(context.Background(), &pb.CreateDeploymentRequest{
		ModelName: "m", ModelVersion: "v",
	})
	if err != nil {
		t.Fatalf("CreateDeployment: %v", err)
	}
	if resp.GetDeployment().GetRuntime() != "auto" {
		t.Errorf("runtime = %q", resp.GetDeployment().GetRuntime())
	}
}

func TestService_CreateDeployment_StoreError(t *testing.T) {
	svc := newMemDeployment(t)
	store.SetErrorForQuery("INSERT INTO model_deployments", errSentinel("db down"))
	_, err := svc.CreateDeployment(context.Background(), &pb.CreateDeploymentRequest{
		ModelName: "m", ModelVersion: "v",
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestService_GetDeployment_HappyPath(t *testing.T) {
	svc := newMemDeployment(t)
	store.SetRowForQuery("FROM model_deployments WHERE id = $1", deploymentRow("d-1"))
	resp, err := svc.GetDeployment(context.Background(), &pb.GetDeploymentRequest{Id: "d-1"})
	if err != nil {
		t.Fatalf("GetDeployment: %v", err)
	}
	if resp.GetDeployment().GetId() != "d-1" {
		t.Errorf("id = %q", resp.GetDeployment().GetId())
	}
}

func TestService_GetDeployment_MissingID(t *testing.T) {
	svc := newMemDeployment(t)
	_, err := svc.GetDeployment(context.Background(), &pb.GetDeploymentRequest{})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestService_GetDeployment_NotFound(t *testing.T) {
	svc := newMemDeployment(t)
	store.SetNoRowsForQuery("FROM model_deployments WHERE id = $1")
	_, err := svc.GetDeployment(context.Background(), &pb.GetDeploymentRequest{Id: "missing"})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestService_ListDeployments_Empty(t *testing.T) {
	svc := newMemDeployment(t)
	resp, err := svc.ListDeployments(context.Background(), &pb.ListDeploymentsRequest{})
	if err != nil {
		t.Fatalf("ListDeployments: %v", err)
	}
	if len(resp.GetDeployments()) != 0 {
		t.Errorf("expected 0, got %d", len(resp.GetDeployments()))
	}
}

func TestService_ListDeployments_WithRows(t *testing.T) {
	svc := newMemDeployment(t)
	store.SetRowForQuery("SELECT COUNT(*) FROM model_deployments", rowD(int64(1)))
	store.SetRowForQuery("ORDER BY created_at DESC", deploymentRow("d-1"))
	resp, err := svc.ListDeployments(context.Background(), &pb.ListDeploymentsRequest{})
	if err != nil {
		t.Fatalf("ListDeployments: %v", err)
	}
	if len(resp.GetDeployments()) != 1 {
		t.Errorf("expected 1, got %d", len(resp.GetDeployments()))
	}
}

func TestService_ListDeployments_PhaseFilter(t *testing.T) {
	svc := newMemDeployment(t)
	store.SetRowForQuery("SELECT COUNT(*) FROM model_deployments", rowD(int64(0)))
	resp, err := svc.ListDeployments(context.Background(), &pb.ListDeploymentsRequest{
		Phase: pb.DeploymentPhase_DEPLOYMENT_PHASE_ACTIVE,
	})
	if err != nil {
		t.Fatalf("ListDeployments: %v", err)
	}
	if resp.GetPage().GetTotalCount() != 0 {
		t.Errorf("total = %d", resp.GetPage().GetTotalCount())
	}
}

func TestService_ListDeployments_PageConfig(t *testing.T) {
	svc := newMemDeployment(t)
	resp, err := svc.ListDeployments(context.Background(), &pb.ListDeploymentsRequest{
		Page: &pb.PageRequest{PageSize: 50, PageToken: "10"},
	})
	if err != nil {
		t.Fatalf("ListDeployments: %v", err)
	}
	if resp.GetPage() == nil {
		t.Fatal("nil page")
	}
}

func TestService_ListDeployments_CountError(t *testing.T) {
	svc := newMemDeployment(t)
	store.SetErrorForQuery("SELECT COUNT(*) FROM model_deployments", errSentinel("count failed"))
	_, err := svc.ListDeployments(context.Background(), &pb.ListDeploymentsRequest{})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestService_UpdateDeployment_MissingID(t *testing.T) {
	svc := newMemDeployment(t)
	_, err := svc.UpdateDeployment(context.Background(), &pb.UpdateDeploymentRequest{})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestService_UpdateDeployment_NotFound(t *testing.T) {
	svc := newMemDeployment(t)
	store.SetNoRowsForQuery("FROM model_deployments WHERE id = $1 FOR UPDATE")
	_, err := svc.UpdateDeployment(context.Background(), &pb.UpdateDeploymentRequest{Id: "missing"})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestService_UpdateDeployment_HappyPath(t *testing.T) {
	svc := newMemDeployment(t)
	// Get for update
	store.SetRowForQuery("FROM model_deployments WHERE id = $1 FOR UPDATE", deploymentRow("d-1"))
	store.SetRowsAffectedForQuery("UPDATE model_deployments\n\t\tSET runtime = $1", 1)
	resp, err := svc.UpdateDeployment(context.Background(), &pb.UpdateDeploymentRequest{
		Id: "d-1", Runtime: "tflite",
	})
	if err != nil {
		t.Fatalf("UpdateDeployment: %v", err)
	}
	if resp.GetDeployment().GetRuntime() != "tflite" {
		t.Errorf("runtime = %q", resp.GetDeployment().GetRuntime())
	}
}

func TestService_UpdateDeployment_PhaseAndRollout(t *testing.T) {
	svc := newMemDeployment(t)
	store.SetRowForQuery("FROM model_deployments WHERE id = $1 FOR UPDATE", deploymentRow("d-1"))
	store.SetRowsAffectedForQuery("UPDATE model_deployments\n\t\tSET runtime = $1", 1)
	resp, err := svc.UpdateDeployment(context.Background(), &pb.UpdateDeploymentRequest{
		Id:      "d-1",
		Phase:   pb.DeploymentPhase_DEPLOYMENT_PHASE_PAUSED,
		Rollout: &pb.RolloutConfig{MaxUnavailable: 3},
	})
	if err != nil {
		t.Fatalf("UpdateDeployment: %v", err)
	}
	if resp.GetDeployment().GetStatus().GetPhase() != pb.DeploymentPhase_DEPLOYMENT_PHASE_PAUSED {
		t.Errorf("phase = %v", resp.GetDeployment().GetStatus().GetPhase())
	}
	if resp.GetDeployment().GetRollout().GetMaxUnavailable() != 3 {
		t.Errorf("max_unavailable = %d", resp.GetDeployment().GetRollout().GetMaxUnavailable())
	}
}

func TestService_UpdateDeployment_NotFound_AfterUpdate(t *testing.T) {
	svc := newMemDeployment(t)
	store.SetRowForQuery("FROM model_deployments WHERE id = $1 FOR UPDATE", deploymentRow("d-1"))
	store.SetRowsAffectedForQuery("UPDATE model_deployments\n\t\tSET runtime = $1", 0)
	_, err := svc.UpdateDeployment(context.Background(), &pb.UpdateDeploymentRequest{Id: "d-1"})
	if err == nil {
		t.Fatal("expected error")
	}
}

// --- RolloutChecker ---

func TestRolloutChecker_CanProceed_Allows(t *testing.T) {
	rc := deployment.NewRolloutChecker()
	row := &deployment.DeploymentRow{
		Rollout: []byte(`{"max_unavailable":3}`),
		Status:  []byte(`{"desired_nodes":10,"ready_nodes":8,"failed_nodes":0}`),
	}
	ok, batch := rc.CanProceed(row)
	if !ok {
		t.Errorf("expected ok=true")
	}
	// max_unavailable=3, unavailable=2 -> allowedBatch=1; remaining=2 keeps it 1.
	if batch != 1 {
		t.Errorf("batch = %d, want 1", batch)
	}
}

func TestRolloutChecker_CanProceed_Blocked(t *testing.T) {
	rc := deployment.NewRolloutChecker()
	row := &deployment.DeploymentRow{
		Rollout: []byte(`{"max_unavailable":2}`),
		Status:  []byte(`{"desired_nodes":10,"ready_nodes":7,"failed_nodes":0}`),
	}
	ok, batch := rc.CanProceed(row)
	if ok {
		t.Errorf("expected ok=false")
	}
	if batch != 0 {
		t.Errorf("batch = %d, want 0", batch)
	}
}

func TestRolloutChecker_CanProceed_BadStatus(t *testing.T) {
	rc := deployment.NewRolloutChecker()
	row := &deployment.DeploymentRow{
		Rollout: []byte(`{"max_unavailable":1}`),
		Status:  []byte(`not-json`),
	}
	ok, _ := rc.CanProceed(row)
	if ok {
		t.Error("expected ok=false on bad status")
	}
}

func TestRolloutChecker_CanProceed_BadRollout(t *testing.T) {
	rc := deployment.NewRolloutChecker()
	row := &deployment.DeploymentRow{
		Rollout: []byte(`not-json`),
		Status:  []byte(`{"desired_nodes":1,"ready_nodes":0,"failed_nodes":0}`),
	}
	ok, _ := rc.CanProceed(row)
	if ok {
		t.Error("expected ok=false on bad rollout")
	}
}

func TestRolloutChecker_IsComplete_True(t *testing.T) {
	rc := deployment.NewRolloutChecker()
	row := &deployment.DeploymentRow{
		Status: []byte(`{"desired_nodes":3,"ready_nodes":2,"failed_nodes":1}`),
	}
	if !rc.IsComplete(row) {
		t.Error("expected complete")
	}
}

func TestRolloutChecker_IsComplete_False(t *testing.T) {
	rc := deployment.NewRolloutChecker()
	row := &deployment.DeploymentRow{
		Status: []byte(`{"desired_nodes":3,"ready_nodes":1,"failed_nodes":0}`),
	}
	if rc.IsComplete(row) {
		t.Error("expected not complete")
	}
}

func TestRolloutChecker_IsComplete_BadStatus(t *testing.T) {
	rc := deployment.NewRolloutChecker()
	row := &deployment.DeploymentRow{Status: []byte(`not-json`)}
	if rc.IsComplete(row) {
		t.Error("expected not complete on bad status")
	}
}

func TestRolloutChecker_SuggestPhase_Active(t *testing.T) {
	rc := deployment.NewRolloutChecker()
	row := &deployment.DeploymentRow{
		Status: []byte(`{"desired_nodes":2,"ready_nodes":2,"failed_nodes":0}`),
	}
	if got := rc.SuggestPhase(row); got != "Active" {
		t.Errorf("phase = %q, want Active", got)
	}
}

func TestRolloutChecker_SuggestPhase_Failed(t *testing.T) {
	rc := deployment.NewRolloutChecker()
	row := &deployment.DeploymentRow{
		Status: []byte(`{"desired_nodes":2,"ready_nodes":0,"failed_nodes":2}`),
	}
	if got := rc.SuggestPhase(row); got != "Failed" {
		t.Errorf("phase = %q, want Failed", got)
	}
}

func TestRolloutChecker_SuggestPhase_RollingOut(t *testing.T) {
	rc := deployment.NewRolloutChecker()
	row := &deployment.DeploymentRow{
		Status: []byte(`{"desired_nodes":5,"ready_nodes":2,"failed_nodes":0}`),
	}
	if got := rc.SuggestPhase(row); got != "RollingOut" {
		t.Errorf("phase = %q, want RollingOut", got)
	}
}

func TestRolloutChecker_SuggestPhase_BadStatus(t *testing.T) {
	rc := deployment.NewRolloutChecker()
	row := &deployment.DeploymentRow{Status: []byte(`not-json`)}
	if got := rc.SuggestPhase(row); got != "Failed" {
		t.Errorf("phase = %q, want Failed", got)
	}
}

// --- JSON helpers ---

func TestDeploymentTargetJSON_Omits(t *testing.T) {
	tj := deployment.DeploymentTargetJSON{}
	b, err := json.Marshal(tj)
	if err != nil {
		t.Fatal(err)
	}
	// Both fields are omitempty, so empty struct should marshal to "{}".
	if string(b) != "{}" {
		t.Errorf("empty target = %s", string(b))
	}
}

func TestRolloutJSON_Marshals(t *testing.T) {
	r := deployment.RolloutJSON{MaxUnavailable: 5}
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	var back deployment.RolloutJSON
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatal(err)
	}
	if back.MaxUnavailable != 5 {
		t.Errorf("roundtrip = %d", back.MaxUnavailable)
	}
}

// errSentinel is a typed error used for store error injection.
type errSentinel string

func (e errSentinel) Error() string { return string(e) }
