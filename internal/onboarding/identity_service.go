package onboarding

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"time"

	pb "github.com/edgeai-platform/ai-edge/api/gen/go/edge/ai/api/v1"
	"github.com/edgeai-platform/ai-edge/internal/store"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// IdentityGRPC implements pb.IdentityServiceServer.
type IdentityGRPC struct {
	pb.UnimplementedIdentityServiceServer
	db  *store.DB
	svc *BootstrapService
}

// NewIdentityGRPC creates a gRPC service for identity management.
func NewIdentityGRPC(db *store.DB, svc *BootstrapService) *IdentityGRPC {
	return &IdentityGRPC{db: db, svc: svc}
}

var textToIdentityStatus = map[string]pb.IdentityStatus{
	"Active":    pb.IdentityStatus_IDENTITY_STATUS_ACTIVE,
	"Suspended": pb.IdentityStatus_IDENTITY_STATUS_SUSPENDED,
	"Revoked":   pb.IdentityStatus_IDENTITY_STATUS_REVOKED,
}

type identityRow struct {
	ID             string
	NodeID         string
	GatewayID      string
	Serial         string
	Fingerprint    string
	Status         string
	CertificatePEM string
	ExpiresAt      time.Time
	IssuedAt       time.Time
	RevokedAt      sql.NullTime
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

func (s *IdentityGRPC) GetIdentity(ctx context.Context, req *pb.GetIdentityRequest) (*pb.GetIdentityResponse, error) {
	if req.GetId() == "" {
		return nil, status.Error(codes.InvalidArgument, "id is required")
	}

	var r identityRow
	err := s.db.QueryRowContext(ctx, `
		SELECT id, node_id, gateway_id, serial, fingerprint, status, certificate_pem, expires_at, issued_at, revoked_at, created_at, updated_at
		FROM edge_identities WHERE id = $1`, req.GetId(),
	).Scan(&r.ID, &r.NodeID, &r.GatewayID, &r.Serial, &r.Fingerprint,
		&r.Status, &r.CertificatePEM, &r.ExpiresAt, &r.IssuedAt, &r.RevokedAt, &r.CreatedAt, &r.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, status.Error(codes.NotFound, "identity not found")
	}
	if err != nil {
		return nil, status.Errorf(codes.Internal, "get identity: %v", err)
	}

	return &pb.GetIdentityResponse{Identity: identityRowToProto(&r)}, nil
}

func (s *IdentityGRPC) ListIdentities(ctx context.Context, req *pb.ListIdentitiesRequest) (*pb.ListIdentitiesResponse, error) {
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

	if req.GetNodeId() != "" {
		where += fmt.Sprintf(" AND node_id = $%d", idx)
		args = append(args, req.GetNodeId())
		idx++
	}

	q := fmt.Sprintf(`
		SELECT id, node_id, gateway_id, serial, fingerprint, status, certificate_pem, expires_at, issued_at, revoked_at, created_at, updated_at
		FROM edge_identities %s
		ORDER BY created_at DESC
		LIMIT $%d OFFSET $%d`, where, idx, idx+1)
	args = append(args, pageSize, offset)

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "list identities: %v", err)
	}
	defer rows.Close()

	var identities []*pb.EdgeIdentity
	for rows.Next() {
		var r identityRow
		if err := rows.Scan(&r.ID, &r.NodeID, &r.GatewayID, &r.Serial, &r.Fingerprint,
			&r.Status, &r.CertificatePEM, &r.ExpiresAt, &r.IssuedAt, &r.RevokedAt, &r.CreatedAt, &r.UpdatedAt); err != nil {
			return nil, status.Errorf(codes.Internal, "scan identity: %v", err)
		}
		identities = append(identities, identityRowToProto(&r))
	}

	return &pb.ListIdentitiesResponse{Identities: identities}, nil
}

func (s *IdentityGRPC) RevokeIdentity(ctx context.Context, req *pb.RevokeIdentityRequest) (*pb.RevokeIdentityResponse, error) {
	if req.GetId() == "" {
		return nil, status.Error(codes.InvalidArgument, "id is required")
	}

	reason := req.GetReason()
	if reason == "" {
		reason = "admin revoked"
	}

	if err := s.svc.RevokeNode(ctx, req.GetId(), reason); err != nil {
		return nil, status.Errorf(codes.Internal, "revoke identity: %v", err)
	}

	return &pb.RevokeIdentityResponse{}, nil
}

func identityRowToProto(r *identityRow) *pb.EdgeIdentity {
	ei := &pb.EdgeIdentity{
		Id:          r.ID,
		NodeId:      r.NodeID,
		GatewayId:   r.GatewayID,
		Serial:      r.Serial,
		Fingerprint: r.Fingerprint,
		Status:      textToIdentityStatus[r.Status],
		ExpiresAt:   timestamppb.New(r.ExpiresAt),
		IssuedAt:    timestamppb.New(r.IssuedAt),
	}
	if r.RevokedAt.Valid {
		ei.RevokedAt = timestamppb.New(r.RevokedAt.Time)
	}
	return ei
}
