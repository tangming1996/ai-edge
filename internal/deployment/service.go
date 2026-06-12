package deployment

import (
	"context"
	"encoding/json"
	"strconv"

	pb "github.com/edgeai-platform/ai-edge/api/gen/go/edge/ai/api/v1"
	"github.com/edgeai-platform/ai-edge/internal/store"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

var phaseToText = map[pb.DeploymentPhase]string{
	pb.DeploymentPhase_DEPLOYMENT_PHASE_PENDING:     "Pending",
	pb.DeploymentPhase_DEPLOYMENT_PHASE_ROLLING_OUT: "RollingOut",
	pb.DeploymentPhase_DEPLOYMENT_PHASE_ACTIVE:      "Active",
	pb.DeploymentPhase_DEPLOYMENT_PHASE_FAILED:      "Failed",
	pb.DeploymentPhase_DEPLOYMENT_PHASE_PAUSED:      "Paused",
}

var textToPhase = map[string]pb.DeploymentPhase{
	"Pending":    pb.DeploymentPhase_DEPLOYMENT_PHASE_PENDING,
	"RollingOut": pb.DeploymentPhase_DEPLOYMENT_PHASE_ROLLING_OUT,
	"Active":     pb.DeploymentPhase_DEPLOYMENT_PHASE_ACTIVE,
	"Failed":     pb.DeploymentPhase_DEPLOYMENT_PHASE_FAILED,
	"Paused":     pb.DeploymentPhase_DEPLOYMENT_PHASE_PAUSED,
}

// Service implements pb.DeploymentServiceServer.
type Service struct {
	pb.UnimplementedDeploymentServiceServer

	store *Store
	db    *store.DB
}

// NewService creates a Deployment gRPC service.
func NewService(db *store.DB) *Service {
	return &Service{
		store: NewStore(db),
		db:    db,
	}
}

func (svc *Service) CreateDeployment(
	ctx context.Context,
	req *pb.CreateDeploymentRequest,
) (*pb.CreateDeploymentResponse, error) {
	if req.GetModelName() == "" {
		return nil, status.Error(codes.InvalidArgument, "model_name is required")
	}
	if req.GetModelVersion() == "" {
		return nil, status.Error(codes.InvalidArgument, "model_version is required")
	}

	runtime := req.GetRuntime()
	if runtime == "" {
		runtime = "auto"
	}

	target := DeploymentTargetJSON{}
	if t := req.GetTarget(); t != nil {
		target.GatewayIDs = t.GetGatewayIds()
		if ls := t.GetLabelSelector(); ls != nil {
			target.LabelSelector = ls.GetMatchLabels()
		}
	}
	targetJSON, _ := json.Marshal(target)

	rollout := RolloutJSON{MaxUnavailable: 1}
	if r := req.GetRollout(); r != nil && r.GetMaxUnavailable() > 0 {
		rollout.MaxUnavailable = int(r.GetMaxUnavailable())
	}
	rolloutJSON, _ := json.Marshal(rollout)

	statusJSON, _ := json.Marshal(DeploymentStatusJSON{Phase: "Pending"})

	row := &DeploymentRow{
		ModelName:    req.GetModelName(),
		ModelVersion: req.GetModelVersion(),
		Target:       targetJSON,
		Runtime:      runtime,
		Rollout:      rolloutJSON,
		Status:       statusJSON,
	}

	if err := svc.store.CreateDeployment(ctx, row); err != nil {
		return nil, status.Errorf(codes.Internal, "create deployment: %v", err)
	}

	return &pb.CreateDeploymentResponse{Deployment: rowToProto(row)}, nil
}

func (svc *Service) GetDeployment(
	ctx context.Context,
	req *pb.GetDeploymentRequest,
) (*pb.GetDeploymentResponse, error) {
	if req.GetId() == "" {
		return nil, status.Error(codes.InvalidArgument, "id is required")
	}

	row, err := svc.store.GetDeployment(ctx, req.GetId())
	if err != nil {
		return nil, errToStatus(err)
	}
	return &pb.GetDeploymentResponse{Deployment: rowToProto(row)}, nil
}

func (svc *Service) ListDeployments(
	ctx context.Context,
	req *pb.ListDeploymentsRequest,
) (*pb.ListDeploymentsResponse, error) {
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

	var phaseFilter string
	if req.GetPhase() != pb.DeploymentPhase_DEPLOYMENT_PHASE_UNSPECIFIED {
		phaseFilter = phaseToText[req.GetPhase()]
	}

	rows, total, err := svc.store.ListDeployments(ctx, DeploymentListFilter{
		ModelName: req.GetModelName(),
		Phase:     phaseFilter,
		Limit:     int(pageSize),
		Offset:    offset,
	})
	if err != nil {
		return nil, status.Errorf(codes.Internal, "list deployments: %v", err)
	}

	deployments := make([]*pb.ModelDeployment, 0, len(rows))
	for _, r := range rows {
		deployments = append(deployments, rowToProto(r))
	}

	nextToken := ""
	nextOffset := offset + int(pageSize)
	if nextOffset < total {
		nextToken = strconv.Itoa(nextOffset)
	}

	return &pb.ListDeploymentsResponse{
		Deployments: deployments,
		Page: &pb.PageResponse{
			NextPageToken: nextToken,
			TotalCount:    int32(total),
		},
	}, nil
}

func (svc *Service) UpdateDeployment(
	ctx context.Context,
	req *pb.UpdateDeploymentRequest,
) (*pb.UpdateDeploymentResponse, error) {
	if req.GetId() == "" {
		return nil, status.Error(codes.InvalidArgument, "id is required")
	}

	var updated *DeploymentRow
	err := svc.db.WithTx(ctx, func(tx *store.Tx) error {
		row, err := svc.store.GetDeploymentForUpdate(ctx, tx, req.GetId())
		if err != nil {
			return err
		}

		runtime := row.Runtime
		if req.GetRuntime() != "" {
			runtime = req.GetRuntime()
		}

		rollout := row.Rollout
		if req.GetRollout() != nil {
			r := RolloutJSON{MaxUnavailable: int(req.GetRollout().GetMaxUnavailable())}
			rollout, _ = json.Marshal(r)
		}

		st := row.Status
		if req.GetPhase() != pb.DeploymentPhase_DEPLOYMENT_PHASE_UNSPECIFIED {
			var current DeploymentStatusJSON
			_ = json.Unmarshal(row.Status, &current)
			current.Phase = phaseToText[req.GetPhase()]
			st, _ = json.Marshal(current)
		}

		if err := svc.store.UpdateDeployment(ctx, tx, row.ID, runtime, rollout, st); err != nil {
			return err
		}

		row.Runtime = runtime
		row.Rollout = rollout
		row.Status = st
		updated = row
		return nil
	})
	if err != nil {
		return nil, errToStatus(err)
	}

	return &pb.UpdateDeploymentResponse{Deployment: rowToProto(updated)}, nil
}

func rowToProto(r *DeploymentRow) *pb.ModelDeployment {
	d := &pb.ModelDeployment{
		Id:           r.ID,
		ModelName:    r.ModelName,
		ModelVersion: r.ModelVersion,
		Runtime:      r.Runtime,
		CreatedAt:    timestamppb.New(r.CreatedAt),
		UpdatedAt:    timestamppb.New(r.UpdatedAt),
	}

	var target DeploymentTargetJSON
	if json.Unmarshal(r.Target, &target) == nil {
		d.Target = &pb.DeploymentTarget{
			GatewayIds: target.GatewayIDs,
		}
		if len(target.LabelSelector) > 0 {
			d.Target.LabelSelector = &pb.LabelSelector{
				MatchLabels: target.LabelSelector,
			}
		}
	}

	var rollout RolloutJSON
	if json.Unmarshal(r.Rollout, &rollout) == nil {
		d.Rollout = &pb.RolloutConfig{
			MaxUnavailable: int32(rollout.MaxUnavailable),
		}
	}

	var st DeploymentStatusJSON
	if json.Unmarshal(r.Status, &st) == nil {
		d.Status = &pb.DeploymentStatus{
			DesiredNodes: int32(st.DesiredNodes),
			ReadyNodes:   int32(st.ReadyNodes),
			FailedNodes:  int32(st.FailedNodes),
			Phase:        textToPhase[st.Phase],
		}
	}

	return d
}

func errToStatus(err error) error {
	switch err {
	case store.ErrNotFound:
		return status.Error(codes.NotFound, "deployment not found")
	case store.ErrPrecondition:
		return status.Error(codes.FailedPrecondition, err.Error())
	case store.ErrAlreadyExists, store.ErrConflict:
		return status.Error(codes.AlreadyExists, err.Error())
	}
	return status.Errorf(codes.Internal, "%v", err)
}
