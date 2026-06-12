package gateway

import (
	"context"
	"database/sql"
	"encoding/json"
	"log"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/edgeai-platform/ai-edge/api/gen/go/edge/ai/api/v1"
	"github.com/edgeai-platform/ai-edge/internal/store"
	"github.com/edgeai-platform/ai-edge/internal/task"
)

const (
	defaultClaimDuration = 5 * time.Minute
)

// Dispatcher implements GatewaySyncServiceServer. It receives regional tasks
// from the Control Plane, expands them into per-node tasks, atomically claims
// each against the central DB, and tracks delivery status.
type Dispatcher struct {
	pb.UnimplementedGatewaySyncServiceServer

	gatewayID     string
	db            *store.DB
	taskStore     *task.Store
	identityCache *IdentityCache
	claimDuration time.Duration
}

// DispatcherConfig holds dependencies for creating a Dispatcher.
type DispatcherConfig struct {
	GatewayID     string
	DB            *store.DB
	IdentityCache *IdentityCache
	ClaimDuration time.Duration
}

// NewDispatcher creates a Dispatcher with the given config.
func NewDispatcher(cfg DispatcherConfig) *Dispatcher {
	dur := cfg.ClaimDuration
	if dur == 0 {
		dur = defaultClaimDuration
	}
	return &Dispatcher{
		gatewayID:     cfg.GatewayID,
		db:            cfg.DB,
		taskStore:     task.NewStore(cfg.DB),
		identityCache: cfg.IdentityCache,
		claimDuration: dur,
	}
}

// PushRegionalTask receives a region-scoped parent task from Control Plane,
// expands it into NodeTask rows (one per target node), performs atomic claim
// for each, and returns the expansion/claim counts.
func (d *Dispatcher) PushRegionalTask(
	ctx context.Context,
	req *pb.PushRegionalTaskRequest,
) (*pb.PushRegionalTaskResponse, error) {
	if req.GetTaskId() == "" {
		return nil, status.Error(codes.InvalidArgument, "task_id is required")
	}
	if req.GetGatewayId() == "" {
		return nil, status.Error(codes.InvalidArgument, "gateway_id is required")
	}
	if req.GetGatewayId() != d.gatewayID {
		return nil, status.Error(codes.PermissionDenied, "gateway_id mismatch")
	}

	nodeIDs := req.GetTargetNodeIds()
	if len(nodeIDs) == 0 {
		return &pb.PushRegionalTaskResponse{
			ExpandedCount: 0,
			ClaimedCount:  0,
		}, nil
	}

	var expandedCount, claimedCount int32

	for _, nodeID := range nodeIDs {
		claimed, err := d.expandAndClaim(ctx, req, nodeID)
		if err != nil {
			log.Printf("dispatcher: expand+claim failed for node %s task %s: %v",
				nodeID, req.GetTaskId(), err)
			continue
		}
		expandedCount++
		if claimed {
			claimedCount++
		}
	}

	log.Printf("dispatcher: PushRegionalTask task=%s expanded=%d claimed=%d",
		req.GetTaskId(), expandedCount, claimedCount)

	return &pb.PushRegionalTaskResponse{
		ExpandedCount: expandedCount,
		ClaimedCount:  claimedCount,
	}, nil
}

// expandAndClaim creates a NodeTask row for one target node, then atomically
// claims it for this gateway instance. Returns true if claim was won.
func (d *Dispatcher) expandAndClaim(
	ctx context.Context,
	req *pb.PushRegionalTaskRequest,
	nodeID string,
) (bool, error) {
	var claimed bool

	err := d.db.WithTx(ctx, func(tx *store.Tx) error {
		row := &task.TaskRow{
			ParentTaskID:    sql.NullString{String: req.GetTaskId(), Valid: true},
			Scope:           "Node",
			Type:            req.GetType(),
			Status:          "Pending",
			TargetGatewayID: sql.NullString{String: d.gatewayID, Valid: true},
			TargetNodeID:    sql.NullString{String: nodeID, Valid: true},
			Payload:         req.GetPayload(),
			DispatchStatus:  "Unclaimed",
			MaxRetries:      task.DefaultMaxRetries,
			TimeoutSeconds:  600,
			CreatedBy:       "gateway:" + d.gatewayID,
		}

		if err := d.taskStore.CreateTask(ctx, tx, row); err != nil {
			return err
		}

		detail, _ := json.Marshal(map[string]string{
			"parent_task_id": req.GetTaskId(),
			"node_id":        nodeID,
		})
		if err := d.taskStore.CreateTaskEvent(ctx, tx,
			row.ID, "expanded", "", "Pending", "gateway:"+d.gatewayID, detail,
		); err != nil {
			return err
		}

		ok, err := d.taskStore.AtomicClaim(ctx, tx, row.ID, d.gatewayID, d.claimDuration)
		if err != nil {
			return err
		}
		if !ok {
			return nil
		}

		if err := d.taskStore.MarkDelivered(ctx, tx, row.ID); err != nil {
			return err
		}

		claimed = true
		return nil
	})

	return claimed, err
}

// SyncGatewayStatus receives aggregated node health from the gateway-runtime
// and logs the snapshot. A production version would persist this to the
// gateway_status / node_status tables.
func (d *Dispatcher) SyncGatewayStatus(
	ctx context.Context,
	req *pb.SyncGatewayStatusRequest,
) (*pb.SyncGatewayStatusResponse, error) {
	if req.GetGatewayId() == "" {
		return nil, status.Error(codes.InvalidArgument, "gateway_id is required")
	}
	if req.GetGatewayId() != d.gatewayID {
		return nil, status.Error(codes.PermissionDenied, "gateway_id mismatch")
	}

	for _, ns := range req.GetNodeStatuses() {
		log.Printf("dispatcher: SyncGatewayStatus node=%s online=%v version=%s lastSeen=%v",
			ns.GetNodeId(), ns.GetOnline(), ns.GetAgentVersion(), ns.GetLastSeenAt())
	}

	return &pb.SyncGatewayStatusResponse{}, nil
}

// NotifyIdentityEvent delegates identity cache invalidation to IdentityCache.
func (d *Dispatcher) NotifyIdentityEvent(
	ctx context.Context,
	req *pb.NotifyIdentityEventRequest,
) (*pb.NotifyIdentityEventResponse, error) {
	if req.GetIdentityId() == "" {
		return nil, status.Error(codes.InvalidArgument, "identity_id is required")
	}

	d.identityCache.HandleIdentityEvent(req)
	log.Printf("dispatcher: NotifyIdentityEvent identity=%s node=%s event=%s",
		req.GetIdentityId(), req.GetNodeId(), req.GetEventType())

	return &pb.NotifyIdentityEventResponse{}, nil
}
