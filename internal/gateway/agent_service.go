package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	pb "github.com/edgeai-platform/ai-edge/api/gen/go/edge/ai/api/v1"
	"github.com/edgeai-platform/ai-edge/internal/observability"
	"github.com/edgeai-platform/ai-edge/internal/store"
	"github.com/edgeai-platform/ai-edge/internal/task"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// AgentService implements pb.AgentServiceServer for edge-agent traffic.
type AgentService struct {
	pb.UnimplementedAgentServiceServer

	gatewayID string
	db        *store.DB
	tasks     *task.Store
	reporter  *observability.Reporter
}

// NewAgentService creates a gateway-side AgentService.
func NewAgentService(db *store.DB, gatewayID string, reporter *observability.Reporter) *AgentService {
	return &AgentService{
		gatewayID: gatewayID,
		db:        db,
		tasks:     task.NewStore(db),
		reporter:  reporter,
	}
}

func (s *AgentService) ReportHeartbeat(ctx context.Context, req *pb.ReportHeartbeatRequest) (*pb.ReportHeartbeatResponse, error) {
	if req.GetNodeId() == "" {
		return nil, status.Error(codes.InvalidArgument, "node_id is required")
	}
	if err := validateAuthenticatedNode(ctx, req.GetNodeId()); err != nil {
		return nil, err
	}

	ts := time.Now()
	if req.GetTimestamp() != nil {
		ts = req.GetTimestamp().AsTime()
	}

	res, err := s.db.ExecContext(ctx, `
		UPDATE edge_nodes
		SET online = true,
		    agent_version = $1,
		    last_seen_at = $2,
		    updated_at = now()
		WHERE id = $3`,
		req.GetAgentVersion(), ts, req.GetNodeId(),
	)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "update heartbeat: %v", err)
	}
	rows, _ := res.RowsAffected()
	if rows == 0 {
		return nil, status.Error(codes.NotFound, "node not found")
	}

	return &pb.ReportHeartbeatResponse{ServerTime: timestamppb.Now()}, nil
}

func (s *AgentService) PullTasks(ctx context.Context, req *pb.PullTasksRequest) (*pb.PullTasksResponse, error) {
	if req.GetNodeId() == "" {
		return nil, status.Error(codes.InvalidArgument, "node_id is required")
	}
	if err := validateAuthenticatedNode(ctx, req.GetNodeId()); err != nil {
		return nil, err
	}

	maxTasks := req.GetMaxTasks()
	if maxTasks <= 0 {
		maxTasks = 10
	}
	if maxTasks > 100 {
		maxTasks = 100
	}

	var tasksOut []*pb.NodeTask
	err := s.db.WithTx(ctx, func(tx *store.Tx) error {
		rows, err := tx.QueryContext(ctx, `
			SELECT id, type, payload, timeout_seconds
			FROM tasks
			WHERE scope = 'Node'
			  AND target_node_id = $1
			  AND status IN ('Pending', 'Retrying')
			  AND (target_gateway_id IS NULL OR target_gateway_id::text = $2)
			ORDER BY created_at ASC
			LIMIT $3
			FOR UPDATE SKIP LOCKED`,
			req.GetNodeId(), s.gatewayID, maxTasks,
		)
		if err != nil {
			return fmt.Errorf("query node tasks: %w", err)
		}
		defer rows.Close()

		for rows.Next() {
			var (
				taskID         string
				taskType       string
				payload        []byte
				timeoutSeconds int32
			)
			if err := rows.Scan(&taskID, &taskType, &payload, &timeoutSeconds); err != nil {
				return fmt.Errorf("scan node task: %w", err)
			}

			if _, err := tx.ExecContext(ctx, `
				UPDATE tasks
				SET status = 'Running',
				    dispatch_status = 'Delivered',
				    owner_instance = COALESCE(owner_instance, $1),
				    updated_at = now()
				WHERE id = $2`,
				"gateway:"+s.gatewayID, taskID,
			); err != nil {
				return fmt.Errorf("mark task running: %w", err)
			}

			detail, _ := json.Marshal(map[string]string{
				"node_id":    req.GetNodeId(),
				"gateway_id": s.gatewayID,
			})
			if err := s.tasks.CreateTaskEvent(ctx, tx, taskID, "delivered", "Pending", "Running", "gateway:"+s.gatewayID, detail); err != nil {
				return fmt.Errorf("create delivery event: %w", err)
			}

			tasksOut = append(tasksOut, &pb.NodeTask{
				TaskId:         taskID,
				Type:           taskType,
				Payload:        payload,
				TimeoutSeconds: timeoutSeconds,
			})
		}

		return rows.Err()
	})
	if err != nil {
		return nil, status.Errorf(codes.Internal, "pull tasks: %v", err)
	}

	return &pb.PullTasksResponse{Tasks: tasksOut}, nil
}

func (s *AgentService) ReportTaskResult(ctx context.Context, req *pb.ReportTaskResultRequest) (*pb.ReportTaskResultResponse, error) {
	if req.GetTaskId() == "" {
		return nil, status.Error(codes.InvalidArgument, "task_id is required")
	}
	if req.GetNodeId() == "" {
		return nil, status.Error(codes.InvalidArgument, "node_id is required")
	}
	if err := validateAuthenticatedNode(ctx, req.GetNodeId()); err != nil {
		return nil, err
	}

	newStatus, err := normalizeReportedStatus(req.GetStatus())
	if err != nil {
		return nil, err
	}

	resultJSON := json.RawMessage(`{}`)
	if len(req.GetResult()) > 0 {
		resultJSON = req.GetResult()
		if !json.Valid(resultJSON) {
			resultJSON, _ = json.Marshal(map[string]string{"raw_result": string(req.GetResult())})
		}
	}

	err = s.db.WithTx(ctx, func(tx *store.Tx) error {
		row, getErr := s.tasks.GetTaskForUpdate(ctx, tx, req.GetTaskId())
		if getErr != nil {
			return getErr
		}
		if !row.TargetNodeID.Valid || row.TargetNodeID.String != req.GetNodeId() {
			return status.Error(codes.PermissionDenied, "task does not belong to node")
		}

		attempt, attemptErr := s.tasks.LatestAttempt(ctx, tx, row.ID)
		if attemptErr != nil {
			return attemptErr
		}
		run, runErr := s.tasks.CreateTaskRun(ctx, tx, row.ID, req.GetNodeId(), attempt+1)
		if runErr != nil {
			return runErr
		}
		if updateRunErr := s.tasks.UpdateTaskRun(ctx, tx, run.ID, newStatus, req.GetErrorMessage(), resultJSON); updateRunErr != nil {
			return updateRunErr
		}

		if updateResultErr := s.tasks.UpdateResult(ctx, tx, row.ID, newStatus, resultJSON, row.RetryCount); updateResultErr != nil {
			return updateResultErr
		}

		detail, _ := json.Marshal(map[string]string{
			"node_id":       req.GetNodeId(),
			"error_message": req.GetErrorMessage(),
		})
		if eventErr := s.tasks.CreateTaskEvent(ctx, tx, row.ID, "result_reported", row.Status, newStatus, "agent:"+req.GetNodeId(), detail); eventErr != nil {
			return eventErr
		}

		return nil
	})
	if err != nil {
		if st, ok := status.FromError(err); ok {
			return nil, st.Err()
		}
		return nil, status.Errorf(codes.Internal, "report task result: %v", err)
	}

	return &pb.ReportTaskResultResponse{}, nil
}

func (s *AgentService) ReportMetrics(ctx context.Context, req *pb.ReportMetricsRequest) (*pb.ReportMetricsResponse, error) {
	if req.GetNodeId() == "" {
		return nil, status.Error(codes.InvalidArgument, "node_id is required")
	}
	if err := validateAuthenticatedNode(ctx, req.GetNodeId()); err != nil {
		return nil, err
	}
	if err := s.reporter.HandleMetrics(ctx, req); err != nil {
		return nil, status.Errorf(codes.Internal, "report metrics: %v", err)
	}
	return &pb.ReportMetricsResponse{}, nil
}

func (s *AgentService) ReportRuntimeState(ctx context.Context, req *pb.ReportRuntimeStateRequest) (*pb.ReportRuntimeStateResponse, error) {
	if req.GetNodeId() == "" {
		return nil, status.Error(codes.InvalidArgument, "node_id is required")
	}
	if err := validateAuthenticatedNode(ctx, req.GetNodeId()); err != nil {
		return nil, err
	}
	if err := s.reporter.HandleRuntimeState(ctx, req); err != nil {
		return nil, status.Errorf(codes.Internal, "report runtime state: %v", err)
	}
	return &pb.ReportRuntimeStateResponse{}, nil
}

func validateAuthenticatedNode(ctx context.Context, nodeID string) error {
	authenticatedNodeID := NodeIDFromContext(ctx)
	if authenticatedNodeID != "" && authenticatedNodeID != nodeID {
		return status.Error(codes.PermissionDenied, "node_id mismatch")
	}
	return nil
}

func normalizeReportedStatus(raw string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "success":
		return "Success", nil
	case "failed", "failure":
		return "Failed", nil
	case "cancelled", "canceled":
		return "Cancelled", nil
	case "timeout":
		return "Timeout", nil
	case "partiallysucceeded", "partially_succeeded", "partially-succeeded":
		return "PartiallySucceeded", nil
	default:
		return "", status.Errorf(codes.InvalidArgument, "unsupported task status %q", raw)
	}
}
