package onboarding

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	pb "github.com/edgeai-platform/ai-edge/api/gen/go/edge/ai/api/v1"
	"github.com/edgeai-platform/ai-edge/internal/store"
)

// NodeGRPC implements pb.NodeServiceServer.
type NodeGRPC struct {
	pb.UnimplementedNodeServiceServer
	db *store.DB
}

// NewNodeGRPC creates a gRPC service for node management.
func NewNodeGRPC(db *store.DB) *NodeGRPC {
	return &NodeGRPC{db: db}
}

type nodeRow struct {
	ID           string
	GatewayID    string
	Labels       json.RawMessage
	HardwareInfo json.RawMessage
	Status       string
	AgentVersion sql.NullString
	Online       bool
	LastSeenAt   sql.NullTime
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

func (s *NodeGRPC) GetNode(ctx context.Context, req *pb.GetNodeRequest) (*pb.GetNodeResponse, error) {
	if req.GetId() == "" {
		return nil, status.Error(codes.InvalidArgument, "id is required")
	}

	var r nodeRow
	err := s.db.QueryRowContext(ctx, `
		SELECT id, gateway_id, labels, hardware_info, status, agent_version, online, last_seen_at, created_at, updated_at
		FROM edge_nodes WHERE id = $1`, req.GetId(),
	).Scan(&r.ID, &r.GatewayID, &r.Labels, &r.HardwareInfo,
		&r.Status, &r.AgentVersion, &r.Online, &r.LastSeenAt, &r.CreatedAt, &r.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, status.Error(codes.NotFound, "node not found")
	}
	if err != nil {
		return nil, status.Errorf(codes.Internal, "get node: %v", err)
	}

	return &pb.GetNodeResponse{Node: nodeRowToProto(&r)}, nil
}

func (s *NodeGRPC) ListNodes(ctx context.Context, req *pb.ListNodesRequest) (*pb.ListNodesResponse, error) {
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

	where := "WHERE 1=1"
	args := []any{}
	idx := 1

	if req.GetGatewayId() != "" {
		where += fmt.Sprintf(" AND gateway_id = $%d", idx)
		args = append(args, req.GetGatewayId())
		idx++
	}

	var total int
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM edge_nodes "+where, args...).Scan(&total); err != nil {
		return nil, status.Errorf(codes.Internal, "count nodes: %v", err)
	}

	q := fmt.Sprintf(`
		SELECT id, gateway_id, labels, hardware_info, status, agent_version, online, last_seen_at, created_at, updated_at
		FROM edge_nodes %s
		ORDER BY created_at DESC
		LIMIT $%d OFFSET $%d`, where, idx, idx+1)
	args = append(args, pageSize, offset)

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "list nodes: %v", err)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			log.Printf("node_service: close rows: %v", err)
		}
	}()

	var nodes []*pb.EdgeNode
	for rows.Next() {
		var r nodeRow
		if err := rows.Scan(&r.ID, &r.GatewayID, &r.Labels, &r.HardwareInfo,
			&r.Status, &r.AgentVersion, &r.Online, &r.LastSeenAt, &r.CreatedAt, &r.UpdatedAt); err != nil {
			return nil, status.Errorf(codes.Internal, "scan node: %v", err)
		}
		nodes = append(nodes, nodeRowToProto(&r))
	}

	nextToken := ""
	nextOffset := offset + int(pageSize)
	if nextOffset < total {
		nextToken = strconv.Itoa(nextOffset)
	}

	return &pb.ListNodesResponse{
		Nodes: nodes,
		Page: &pb.PageResponse{
			NextPageToken: nextToken,
			TotalCount:    int32(total),
		},
	}, nil
}

func (s *NodeGRPC) UpdateNode(ctx context.Context, req *pb.UpdateNodeRequest) (*pb.UpdateNodeResponse, error) {
	if req.GetId() == "" {
		return nil, status.Error(codes.InvalidArgument, "id is required")
	}

	var labelsJSON json.RawMessage
	if req.GetLabels() != nil && len(req.GetLabels().GetItems()) > 0 {
		labelsJSON, _ = json.Marshal(req.GetLabels().GetItems())
	}

	var r nodeRow
	err := s.db.QueryRowContext(ctx, `
		UPDATE edge_nodes SET labels = COALESCE($1, labels), updated_at = now()
		WHERE id = $2
		RETURNING id, gateway_id, labels, hardware_info, status, agent_version, online, last_seen_at, created_at, updated_at`,
		labelsJSON, req.GetId(),
	).Scan(&r.ID, &r.GatewayID, &r.Labels, &r.HardwareInfo,
		&r.Status, &r.AgentVersion, &r.Online, &r.LastSeenAt, &r.CreatedAt, &r.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, status.Error(codes.NotFound, "node not found")
	}
	if err != nil {
		return nil, status.Errorf(codes.Internal, "update node: %v", err)
	}

	return &pb.UpdateNodeResponse{Node: nodeRowToProto(&r)}, nil
}

func nodeRowToProto(r *nodeRow) *pb.EdgeNode {
	n := &pb.EdgeNode{
		Id:        r.ID,
		GatewayId: r.GatewayID,
		Status:    r.Status,
		Online:    r.Online,
		CreatedAt: timestamppb.New(r.CreatedAt),
		UpdatedAt: timestamppb.New(r.UpdatedAt),
	}
	if r.AgentVersion.Valid {
		n.AgentVersion = r.AgentVersion.String
	}
	if r.LastSeenAt.Valid {
		n.LastSeenAt = timestamppb.New(r.LastSeenAt.Time)
	}
	if len(r.Labels) > 0 {
		var items map[string]string
		if json.Unmarshal(r.Labels, &items) == nil && len(items) > 0 {
			n.Labels = &pb.Labels{Items: items}
		}
	}
	return n
}
