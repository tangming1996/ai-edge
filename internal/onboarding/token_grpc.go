package onboarding

import (
	"context"
	"database/sql"
	"strconv"
	"time"

	pb "github.com/edgeai-platform/ai-edge/api/gen/go/edge/ai/api/v1"
	"github.com/edgeai-platform/ai-edge/internal/store"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// TokenGRPC implements pb.BootstrapTokenServiceServer.
type TokenGRPC struct {
	pb.UnimplementedBootstrapTokenServiceServer
	tokens *TokenStore
	db     *store.DB
}

// NewTokenGRPC creates a gRPC service for bootstrap token management.
func NewTokenGRPC(db *store.DB) *TokenGRPC {
	return &TokenGRPC{
		tokens: NewTokenStore(db),
		db:     db,
	}
}

func (s *TokenGRPC) CreateBootstrapToken(
	ctx context.Context,
	req *pb.CreateBootstrapTokenRequest,
) (*pb.CreateBootstrapTokenResponse, error) {
	if req.GetGatewayId() == "" {
		return nil, status.Error(codes.InvalidArgument, "gateway_id is required")
	}

	maxUses := int(req.GetMaxUses())
	if maxUses <= 0 {
		maxUses = 10
	}
	expiresIn := time.Duration(req.GetExpiresInSeconds()) * time.Second
	if expiresIn <= 0 {
		expiresIn = 24 * time.Hour
	}

	var labels map[string]string
	if req.GetLabels() != nil {
		labels = req.GetLabels().GetItems()
	}

	rec, plain, err := s.tokens.Create(ctx,
		req.GetGatewayId(), req.GetDescription(), labels, maxUses, expiresIn)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "create token: %v", err)
	}

	return &pb.CreateBootstrapTokenResponse{
		TokenMetadata: recToProto(rec),
		TokenPlain:    plain,
	}, nil
}

func (s *TokenGRPC) GetBootstrapToken(
	ctx context.Context,
	req *pb.GetBootstrapTokenRequest,
) (*pb.GetBootstrapTokenResponse, error) {
	if req.GetId() == "" {
		return nil, status.Error(codes.InvalidArgument, "id is required")
	}

	rec, err := s.tokens.GetByID(ctx, req.GetId())
	if err != nil {
		return nil, tokenErrToStatus(err)
	}

	return &pb.GetBootstrapTokenResponse{Token: recToProto(rec)}, nil
}

func (s *TokenGRPC) ListBootstrapTokens(
	ctx context.Context,
	req *pb.ListBootstrapTokensRequest,
) (*pb.ListBootstrapTokensResponse, error) {
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

	query := `SELECT id, gateway_id, token_hash, description, labels, max_uses, used_count, status, expires_at, created_at, updated_at
		FROM bootstrap_tokens WHERE 1=1`
	args := []any{}
	idx := 1

	if req.GetGatewayId() != "" {
		query += " AND gateway_id = $" + strconv.Itoa(idx)
		args = append(args, req.GetGatewayId())
		idx++
	}

	query += " ORDER BY created_at DESC"
	query += " LIMIT $" + strconv.Itoa(idx) + " OFFSET $" + strconv.Itoa(idx+1)
	args = append(args, pageSize, offset)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "list tokens: %v", err)
	}
	defer rows.Close()

	var tokens []*pb.BootstrapToken
	for rows.Next() {
		var rec TokenRecord
		var labelsJSON []byte
		if err := rows.Scan(
			&rec.ID, &rec.GatewayID, &rec.TokenHash, &rec.Description, &labelsJSON,
			&rec.MaxUses, &rec.UsedCount, &rec.Status, &rec.ExpiresAt, &rec.CreatedAt, &rec.UpdatedAt,
		); err != nil {
			return nil, status.Errorf(codes.Internal, "scan token: %v", err)
		}
		tokens = append(tokens, recToProto(&rec))
	}

	return &pb.ListBootstrapTokensResponse{Tokens: tokens}, nil
}

func (s *TokenGRPC) FreezeBootstrapToken(
	ctx context.Context,
	req *pb.FreezeBootstrapTokenRequest,
) (*pb.FreezeBootstrapTokenResponse, error) {
	if req.GetId() == "" {
		return nil, status.Error(codes.InvalidArgument, "id is required")
	}

	rec, err := s.tokens.UpdateStatus(ctx, req.GetId(), "Frozen")
	if err != nil {
		return nil, tokenErrToStatus(err)
	}

	return &pb.FreezeBootstrapTokenResponse{Token: recToProto(rec)}, nil
}

func (s *TokenGRPC) RevokeBootstrapToken(
	ctx context.Context,
	req *pb.RevokeBootstrapTokenRequest,
) (*pb.RevokeBootstrapTokenResponse, error) {
	if req.GetId() == "" {
		return nil, status.Error(codes.InvalidArgument, "id is required")
	}

	rec, err := s.tokens.UpdateStatus(ctx, req.GetId(), "Revoked")
	if err != nil {
		return nil, tokenErrToStatus(err)
	}

	return &pb.RevokeBootstrapTokenResponse{Token: recToProto(rec)}, nil
}

func recToProto(rec *TokenRecord) *pb.BootstrapToken {
	return &pb.BootstrapToken{
		Id:        rec.ID,
		GatewayId: rec.GatewayID,
		Description: rec.Description,
		MaxUses:   int32(rec.MaxUses),
		UsedCount: int32(rec.UsedCount),
		Status:    rec.Status,
		ExpiresAt: timestamppb.New(rec.ExpiresAt),
		CreatedAt: timestamppb.New(rec.CreatedAt),
	}
}

func tokenErrToStatus(err error) error {
	if err == sql.ErrNoRows {
		return status.Error(codes.NotFound, "token not found")
	}
	if st, ok := status.FromError(err); ok {
		return st.Err()
	}
	return status.Errorf(codes.Internal, "%v", err)
}
