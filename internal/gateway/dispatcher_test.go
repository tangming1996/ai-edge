package gateway

import (
	"context"
	"testing"

	apiv1 "github.com/edgeai-platform/ai-edge/api/gen/go/edge/ai/api/v1"
)

func TestDispatcherConfig_DefaultClaimDuration(t *testing.T) {
	// A zero ClaimDuration must default to defaultClaimDuration.
	d := NewDispatcher(DispatcherConfig{GatewayID: "gw"})
	if d.claimDuration != defaultClaimDuration {
		t.Fatalf("claimDuration = %s, want %s", d.claimDuration, defaultClaimDuration)
	}
}

func TestDispatcherConfig_CustomClaimDuration(t *testing.T) {
	d := NewDispatcher(DispatcherConfig{
		GatewayID:     "gw",
		ClaimDuration: 123,
	})
	if d.claimDuration != 123 {
		t.Fatalf("claimDuration = %s, want 123ns", d.claimDuration)
	}
}

func TestDispatcher_PushRegionalTask_RequiresTaskID(t *testing.T) {
	d := NewDispatcher(DispatcherConfig{GatewayID: "gw"})
	_, err := d.PushRegionalTask(context.Background(), &apiv1.PushRegionalTaskRequest{})
	if err == nil {
		t.Fatal("expected error for missing task_id")
	}
}

func TestDispatcher_PushRegionalTask_RequiresGatewayID(t *testing.T) {
	d := NewDispatcher(DispatcherConfig{GatewayID: "gw"})
	_, err := d.PushRegionalTask(context.Background(), &apiv1.PushRegionalTaskRequest{
		TaskId: "t1",
	})
	if err == nil {
		t.Fatal("expected error for missing gateway_id")
	}
}

func TestDispatcher_PushRegionalTask_GatewayMismatch(t *testing.T) {
	d := NewDispatcher(DispatcherConfig{GatewayID: "gw-a"})
	_, err := d.PushRegionalTask(context.Background(), &apiv1.PushRegionalTaskRequest{
		TaskId:    "t1",
		GatewayId: "gw-b",
	})
	if err == nil {
		t.Fatal("expected error for gateway mismatch")
	}
}

func TestDispatcher_SyncGatewayStatus_RequiresGatewayID(t *testing.T) {
	d := NewDispatcher(DispatcherConfig{GatewayID: "gw"})
	_, err := d.SyncGatewayStatus(context.Background(), &apiv1.SyncGatewayStatusRequest{})
	if err == nil {
		t.Fatal("expected error for missing gateway_id")
	}
}

func TestDispatcher_SyncGatewayStatus_GatewayMismatch(t *testing.T) {
	d := NewDispatcher(DispatcherConfig{GatewayID: "gw-a"})
	_, err := d.SyncGatewayStatus(context.Background(), &apiv1.SyncGatewayStatusRequest{
		GatewayId: "gw-b",
	})
	if err == nil {
		t.Fatal("expected error for gateway mismatch")
	}
}

func TestDispatcher_SyncGatewayStatus_Valid(t *testing.T) {
	d := NewDispatcher(DispatcherConfig{GatewayID: "gw-a"})
	_, err := d.SyncGatewayStatus(context.Background(), &apiv1.SyncGatewayStatusRequest{
		GatewayId: "gw-a",
		NodeStatuses: []*apiv1.NodeStatusSummary{
			{NodeId: "n1", Online: true},
			{NodeId: "n2", Online: false},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDispatcher_NotifyIdentityEvent_RequiresIdentityID(t *testing.T) {
	d := NewDispatcher(DispatcherConfig{GatewayID: "gw"})
	_, err := d.NotifyIdentityEvent(context.Background(), &apiv1.NotifyIdentityEventRequest{})
	if err == nil {
		t.Fatal("expected error for missing identity_id")
	}
}

func TestDispatcherConfig_StoresAllFields(t *testing.T) {
	// Ensure NewDispatcher does not lose any field of DispatcherConfig.
	d := NewDispatcher(DispatcherConfig{
		GatewayID:     "gw-x",
		ClaimDuration: 7,
	})
	if d.gatewayID != "gw-x" {
		t.Errorf("gatewayID = %q", d.gatewayID)
	}
	if d.claimDuration != 7 {
		t.Errorf("claimDuration = %d", d.claimDuration)
	}
	if d.taskStore == nil {
		t.Error("taskStore must be initialised")
	}
}
