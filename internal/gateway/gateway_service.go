package gateway

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	pb "github.com/edgeai-platform/ai-edge/api/gen/go/edge/ai/api/v1"
	"github.com/edgeai-platform/ai-edge/internal/store"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// GatewayManagementService implements pb.GatewayServiceServer.
type GatewayManagementService struct {
	pb.UnimplementedGatewayServiceServer
	db *store.DB
}

// NewGatewayManagementService creates a GatewayManagementService.
func NewGatewayManagementService(db *store.DB) *GatewayManagementService {
	return &GatewayManagementService{db: db}
}

type gatewayRow struct {
	ID        string
	Name      string
	Region    string
	Labels    json.RawMessage
	Status    string
	Endpoint  string
	CreatedAt time.Time
	UpdatedAt time.Time
}

func (svc *GatewayManagementService) CreateGateway(
	ctx context.Context,
	req *pb.CreateGatewayRequest,
) (*pb.CreateGatewayResponse, error) {
	if req.GetName() == "" {
		return nil, status.Error(codes.InvalidArgument, "name is required")
	}

	labels := marshalLabels(req.GetLabels())

	var row gatewayRow
	err := svc.db.QueryRowContext(ctx, `
		INSERT INTO gateways (name, region, labels, endpoint, status)
		VALUES ($1, $2, $3, $4, 'Active')
		RETURNING id, name, region, labels, status, endpoint, created_at, updated_at`,
		req.GetName(), req.GetRegion(), labels, req.GetEndpoint(),
	).Scan(&row.ID, &row.Name, &row.Region, &row.Labels,
		&row.Status, &row.Endpoint, &row.CreatedAt, &row.UpdatedAt)
	if err != nil {
		if store.IsUniqueViolation(err) {
			return nil, status.Errorf(codes.AlreadyExists, "gateway %q already exists", req.GetName())
		}
		return nil, status.Errorf(codes.Internal, "create gateway: %v", err)
	}

	return &pb.CreateGatewayResponse{Gateway: rowToGatewayProto(&row)}, nil
}

func (svc *GatewayManagementService) GetGateway(
	ctx context.Context,
	req *pb.GetGatewayRequest,
) (*pb.GetGatewayResponse, error) {
	if req.GetId() == "" {
		return nil, status.Error(codes.InvalidArgument, "id is required")
	}

	row, err := svc.getGatewayRow(ctx, req.GetId())
	if err != nil {
		return nil, err
	}

	return &pb.GetGatewayResponse{Gateway: rowToGatewayProto(row)}, nil
}

func (svc *GatewayManagementService) ListGateways(
	ctx context.Context,
	req *pb.ListGatewaysRequest,
) (*pb.ListGatewaysResponse, error) {
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

	if req.GetRegion() != "" {
		where += fmt.Sprintf(" AND region = $%d", idx)
		args = append(args, req.GetRegion())
		idx++
	}

	var total int
	if err := svc.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM gateways "+where, args...).Scan(&total); err != nil {
		return nil, status.Errorf(codes.Internal, "count gateways: %v", err)
	}

	dataQ := fmt.Sprintf(`
		SELECT id, name, region, labels, status, endpoint, created_at, updated_at
		FROM gateways %s
		ORDER BY created_at DESC
		LIMIT $%d OFFSET $%d`, where, idx, idx+1)
	args = append(args, pageSize, offset)

	rows, err := svc.db.QueryContext(ctx, dataQ, args...)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "list gateways: %v", err)
	}
	defer rows.Close()

	var gateways []*pb.Gateway
	for rows.Next() {
		var r gatewayRow
		if err := rows.Scan(&r.ID, &r.Name, &r.Region, &r.Labels,
			&r.Status, &r.Endpoint, &r.CreatedAt, &r.UpdatedAt); err != nil {
			return nil, status.Errorf(codes.Internal, "scan gateway: %v", err)
		}
		gateways = append(gateways, rowToGatewayProto(&r))
	}
	if err := rows.Err(); err != nil {
		return nil, status.Errorf(codes.Internal, "iterate gateways: %v", err)
	}

	nextToken := ""
	nextOffset := offset + int(pageSize)
	if nextOffset < total {
		nextToken = strconv.Itoa(nextOffset)
	}

	return &pb.ListGatewaysResponse{
		Gateways: gateways,
		Page: &pb.PageResponse{
			NextPageToken: nextToken,
			TotalCount:    int32(total),
		},
	}, nil
}

func (svc *GatewayManagementService) UpdateGateway(
	ctx context.Context,
	req *pb.UpdateGatewayRequest,
) (*pb.UpdateGatewayResponse, error) {
	if req.GetId() == "" {
		return nil, status.Error(codes.InvalidArgument, "id is required")
	}

	labels := marshalLabels(req.GetLabels())

	var row gatewayRow
	err := svc.db.QueryRowContext(ctx, `
		UPDATE gateways SET labels = COALESCE($1, labels), endpoint = COALESCE(NULLIF($2,''), endpoint), updated_at = now()
		WHERE id = $3
		RETURNING id, name, region, labels, status, endpoint, created_at, updated_at`,
		labels, req.GetEndpoint(), req.GetId(),
	).Scan(&row.ID, &row.Name, &row.Region, &row.Labels,
		&row.Status, &row.Endpoint, &row.CreatedAt, &row.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, status.Error(codes.NotFound, "gateway not found")
	}
	if err != nil {
		return nil, status.Errorf(codes.Internal, "update gateway: %v", err)
	}

	return &pb.UpdateGatewayResponse{Gateway: rowToGatewayProto(&row)}, nil
}

func (svc *GatewayManagementService) DeleteGateway(
	ctx context.Context,
	req *pb.DeleteGatewayRequest,
) (*pb.DeleteGatewayResponse, error) {
	if req.GetId() == "" {
		return nil, status.Error(codes.InvalidArgument, "id is required")
	}

	res, err := svc.db.ExecContext(ctx,
		"UPDATE gateways SET status = 'Deleted', updated_at = now() WHERE id = $1 AND status != 'Deleted'",
		req.GetId())
	if err != nil {
		return nil, status.Errorf(codes.Internal, "delete gateway: %v", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return nil, status.Error(codes.NotFound, "gateway not found")
	}

	return &pb.DeleteGatewayResponse{}, nil
}

func (svc *GatewayManagementService) getGatewayRow(ctx context.Context, id string) (*gatewayRow, error) {
	var row gatewayRow
	err := svc.db.QueryRowContext(ctx, `
		SELECT id, name, region, labels, status, endpoint, created_at, updated_at
		FROM gateways WHERE id = $1`, id,
	).Scan(&row.ID, &row.Name, &row.Region, &row.Labels,
		&row.Status, &row.Endpoint, &row.CreatedAt, &row.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, status.Error(codes.NotFound, "gateway not found")
	}
	if err != nil {
		return nil, status.Errorf(codes.Internal, "get gateway: %v", err)
	}
	return &row, nil
}

func rowToGatewayProto(r *gatewayRow) *pb.Gateway {
	gw := &pb.Gateway{
		Id:        r.ID,
		Name:      r.Name,
		Region:    r.Region,
		Status:    r.Status,
		Endpoint:  r.Endpoint,
		CreatedAt: timestamppb.New(r.CreatedAt),
		UpdatedAt: timestamppb.New(r.UpdatedAt),
	}

	if len(r.Labels) > 0 {
		var items map[string]string
		if json.Unmarshal(r.Labels, &items) == nil && len(items) > 0 {
			gw.Labels = &pb.Labels{Items: items}
		}
	}
	return gw
}

func marshalLabels(labels *pb.Labels) json.RawMessage {
	if labels == nil || len(labels.GetItems()) == 0 {
		return json.RawMessage(`{}`)
	}
	data, _ := json.Marshal(labels.GetItems())
	return data
}
