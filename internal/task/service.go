package task

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	pb "github.com/edgeai-platform/ai-edge/api/gen/go/edge/ai/api/v1"
	"github.com/edgeai-platform/ai-edge/internal/store"
)

// Service implements pb.TaskServiceServer.
type Service struct {
	pb.UnimplementedTaskServiceServer

	store       *Store
	db          *store.DB
	idempotency *IdempotencyChecker
}

// NewService creates a Task gRPC service.
func NewService(db *store.DB) *Service {
	return &Service{
		store:       NewStore(db),
		db:          db,
		idempotency: NewIdempotencyChecker(db),
	}
}

// CreateTask creates a new task.
func (svc *Service) CreateTask(
	ctx context.Context,
	req *pb.CreateTaskRequest,
) (*pb.CreateTaskResponse, error) {
	if req.GetType() == "" {
		return nil, status.Error(codes.InvalidArgument, "type is required")
	}

	scope, ok := protoToScope[req.GetScope()]
	if !ok || req.GetScope() == pb.TaskScope_TASK_SCOPE_UNSPECIFIED {
		return nil, status.Error(codes.InvalidArgument, "valid scope is required")
	}

	maxRetries := int(req.GetMaxRetries())
	if maxRetries <= 0 {
		maxRetries = DefaultMaxRetries
	}
	timeoutSec := int(req.GetTimeoutSeconds())
	if timeoutSec <= 0 {
		timeoutSec = 600
	}

	payload := req.GetPayload()
	if payload == nil {
		payload = []byte(`{}`)
	}

	row := &TaskRow{
		Scope:          scope,
		Type:           req.GetType(),
		Status:         "Pending",
		DispatchStatus: "Unclaimed",
		Payload:        payload,
		MaxRetries:     maxRetries,
		TimeoutSeconds: timeoutSec,
		CreatedBy:      req.GetCreatedBy(),
	}
	if req.GetTargetGatewayId() != "" {
		row.TargetGatewayID = sql.NullString{String: req.GetTargetGatewayId(), Valid: true}
	}
	if req.GetTargetNodeId() != "" {
		row.TargetNodeID = sql.NullString{String: req.GetTargetNodeId(), Valid: true}
	}

	var created *TaskRow
	err := svc.db.WithTx(ctx, func(tx *store.Tx) error {
		if err := svc.store.CreateTask(ctx, tx, row); err != nil {
			return err
		}

		detail, _ := json.Marshal(map[string]string{"type": row.Type, "scope": scope})
		if err := svc.store.CreateTaskEvent(ctx, tx,
			row.ID, "created", "", "Pending", row.CreatedBy, detail,
		); err != nil {
			return err
		}

		created = row
		return nil
	})
	if err != nil {
		return nil, status.Errorf(codes.Internal, "create task: %v", err)
	}

	return &pb.CreateTaskResponse{Task: rowToProto(created)}, nil
}

// GetTask returns a task by ID.
func (svc *Service) GetTask(
	ctx context.Context,
	req *pb.GetTaskRequest,
) (*pb.GetTaskResponse, error) {
	if req.GetId() == "" {
		return nil, status.Error(codes.InvalidArgument, "id is required")
	}

	row, err := svc.store.GetTask(ctx, req.GetId())
	if err != nil {
		return nil, errToStatus(err)
	}
	return &pb.GetTaskResponse{Task: rowToProto(row)}, nil
}

// ListTasks returns a paginated list of tasks.
func (svc *Service) ListTasks(
	ctx context.Context,
	req *pb.ListTasksRequest,
) (*pb.ListTasksResponse, error) {
	pageSize := int32(20)
	offset := 0
	if p := req.GetPage(); p != nil {
		if p.PageSize > 0 {
			pageSize = p.PageSize
		}
		if p.PageToken != "" {
			if v, err := strconv.Atoi(p.PageToken); err == nil {
				offset = v
			}
		}
	}

	var statusFilter string
	if req.GetStatus() != pb.TaskStatus_TASK_STATUS_UNSPECIFIED {
		statusFilter = protoToStatus[req.GetStatus()]
	}

	rows, total, err := svc.store.ListTasks(ctx, ListFilter{
		TargetGatewayID: req.GetTargetGatewayId(),
		TargetNodeID:    req.GetTargetNodeId(),
		Status:          statusFilter,
		Type:            req.GetType(),
		Limit:           int(pageSize),
		Offset:          offset,
	})
	if err != nil {
		return nil, status.Errorf(codes.Internal, "list tasks: %v", err)
	}

	tasks := make([]*pb.Task, 0, len(rows))
	for _, r := range rows {
		tasks = append(tasks, rowToProto(r))
	}

	nextToken := ""
	nextOffset := offset + int(pageSize)
	if nextOffset < total {
		nextToken = strconv.Itoa(nextOffset)
	}

	return &pb.ListTasksResponse{
		Tasks: tasks,
		Page: &pb.PageResponse{
			NextPageToken: nextToken,
			TotalCount:    int32(total),
		},
	}, nil
}

// CancelTask cancels a task if it is not in a terminal state.
func (svc *Service) CancelTask(
	ctx context.Context,
	req *pb.CancelTaskRequest,
) (*pb.CancelTaskResponse, error) {
	if req.GetId() == "" {
		return nil, status.Error(codes.InvalidArgument, "id is required")
	}

	var result *TaskRow
	err := svc.db.WithTx(ctx, func(tx *store.Tx) error {
		row, err := svc.store.GetTaskForUpdate(ctx, tx, req.GetId())
		if err != nil {
			return err
		}

		currentProto := statusToProto[row.Status]
		if IsTerminal(currentProto) {
			return fmt.Errorf("%w: task is already in terminal state: %s", store.ErrPrecondition, row.Status)
		}
		if !ValidateTransition(currentProto, pb.TaskStatus_TASK_STATUS_CANCELLED) {
			return &ErrInvalidTransition{From: currentProto, To: pb.TaskStatus_TASK_STATUS_CANCELLED}
		}

		detail, _ := json.Marshal(map[string]string{"reason": req.GetReason()})
		if err := svc.store.UpdateStatus(ctx, tx,
			row.ID, row.Status, "Cancelled", "user", detail,
		); err != nil {
			return err
		}

		row.Status = "Cancelled"
		result = row
		return nil
	})
	if err != nil {
		return nil, errToStatus(err)
	}
	return &pb.CancelTaskResponse{Task: rowToProto(result)}, nil
}

// rowToProto converts a TaskRow to a proto Task message.
func rowToProto(r *TaskRow) *pb.Task {
	t := &pb.Task{
		Id:             r.ID,
		Scope:          scopeToProto[r.Scope],
		Type:           r.Type,
		Status:         statusToProto[r.Status],
		Payload:        r.Payload,
		Result:         r.Result,
		DispatchStatus: dispatchToProto[r.DispatchStatus],
		MaxRetries:     int32(r.MaxRetries),
		RetryCount:     int32(r.RetryCount),
		TimeoutSeconds: int32(r.TimeoutSeconds),
		CreatedBy:      r.CreatedBy,
		CreatedAt:      timestamppb.New(r.CreatedAt),
		UpdatedAt:      timestamppb.New(r.UpdatedAt),
	}
	if r.ParentTaskID.Valid {
		t.ParentTaskId = r.ParentTaskID.String
	}
	if r.TargetGatewayID.Valid {
		t.TargetGatewayId = r.TargetGatewayID.String
	}
	if r.TargetNodeID.Valid {
		t.TargetNodeId = r.TargetNodeID.String
	}
	return t
}

func errToStatus(err error) error {
	switch {
	case errors.Is(err, store.ErrNotFound):
		return status.Error(codes.NotFound, "task not found")
	case errors.Is(err, store.ErrPrecondition):
		return status.Error(codes.FailedPrecondition, err.Error())
	case errors.Is(err, store.ErrConflict), errors.Is(err, store.ErrAlreadyExists):
		return status.Error(codes.AlreadyExists, err.Error())
	}
	var inv *ErrInvalidTransition
	if errors.As(err, &inv) {
		return status.Error(codes.FailedPrecondition, err.Error())
	}
	return status.Errorf(codes.Internal, "%v", err)
}
