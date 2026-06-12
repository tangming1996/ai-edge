package onboarding

import (
	"context"
	"crypto/tls"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	pb "github.com/edgeai-platform/ai-edge/api/gen/go/edge/ai/api/v1"
	"github.com/edgeai-platform/ai-edge/internal/pki"
	"github.com/edgeai-platform/ai-edge/internal/store"
)

// OnboardingGRPC implements pb.NodeOnboardingServiceServer.
type OnboardingGRPC struct {
	pb.UnimplementedNodeOnboardingServiceServer
	svc *BootstrapService
}

// NewOnboardingGRPC wraps BootstrapService into a gRPC server.
func NewOnboardingGRPC(db *store.DB, signer *pki.Signer) *OnboardingGRPC {
	tokens := NewTokenStore(db)
	svc := NewBootstrapService(db, tokens, signer)
	return &OnboardingGRPC{svc: svc}
}

func (s *OnboardingGRPC) Bootstrap(ctx context.Context, req *pb.BootstrapRequest) (*pb.BootstrapResponse, error) {
	if req.GetToken() == "" {
		return nil, status.Error(codes.InvalidArgument, "token is required")
	}
	if req.GetGatewayId() == "" {
		return nil, status.Error(codes.InvalidArgument, "gateway_id is required")
	}

	var labels map[string]string
	if req.GetLabels() != nil {
		labels = req.GetLabels().GetItems()
	}

	out, err := s.svc.Bootstrap(ctx, BootstrapInput{
		Token:        req.GetToken(),
		GatewayID:    req.GetGatewayId(),
		CSRPEM:       req.GetCsrPem(),
		Serial:       req.GetSerial(),
		HardwareInfo: req.GetHardwareInfo(),
		Labels:       labels,
	})
	if err != nil {
		return nil, err
	}

	return &pb.BootstrapResponse{
		NodeId:         out.NodeID,
		CertificatePem: out.CertPEM,
		CaPem:          out.CAPEM,
		ExpiresAt:      timestamppb.New(out.ExpiresAt),
	}, nil
}

func (s *OnboardingGRPC) Renew(ctx context.Context, req *pb.RenewRequest) (*pb.RenewResponse, error) {
	nodeID := nodeIDFromContext(ctx)
	if nodeID == "" {
		return nil, status.Error(codes.Unauthenticated, "node_id not found in mTLS context")
	}

	out, err := s.svc.Renew(ctx, RenewInput{
		NodeID: nodeID,
		CSRPEM: req.GetCsrPem(),
	})
	if err != nil {
		return nil, err
	}

	return &pb.RenewResponse{
		CertificatePem: out.CertPEM,
		CaPem:          out.CAPEM,
		ExpiresAt:      timestamppb.New(out.ExpiresAt),
	}, nil
}

// nodeIDFromContext extracts the CommonName (= node ID) from the mTLS peer certificate.
func nodeIDFromContext(ctx context.Context) string {
	p, ok := peer.FromContext(ctx)
	if !ok {
		return ""
	}
	tlsInfo, ok := p.AuthInfo.(credentials.TLSInfo)
	if !ok {
		return ""
	}
	state := tlsInfo.State
	return nodeIDFromTLSState(state)
}

func nodeIDFromTLSState(state tls.ConnectionState) string {
	if len(state.PeerCertificates) == 0 {
		return ""
	}
	return state.PeerCertificates[0].Subject.CommonName
}
