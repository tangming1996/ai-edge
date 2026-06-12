package gateway

import (
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
)

type contextKey string

const (
	ctxKeyNodeID    contextKey = "gateway.node_id"
	ctxKeyGatewayID contextKey = "gateway.gateway_id"
)

// NodeIDFromContext extracts the authenticated node ID from context.
func NodeIDFromContext(ctx context.Context) string {
	v, _ := ctx.Value(ctxKeyNodeID).(string)
	return v
}

// GatewayIDFromContext extracts the gateway ID bound to the caller.
func GatewayIDFromContext(ctx context.Context) string {
	v, _ := ctx.Value(ctxKeyGatewayID).(string)
	return v
}

// AuthInterceptorConfig configures the mTLS auth interceptor.
type AuthInterceptorConfig struct {
	Cache     *IdentityCache
	GatewayID string

	// SkipMethods lists full gRPC method names that bypass mTLS verification
	// (e.g., Bootstrap which uses token auth instead).
	SkipMethods []string
}

// NewAuthInterceptor returns a gRPC UnaryServerInterceptor that performs
// per-request mTLS identity verification.
func NewAuthInterceptor(cfg AuthInterceptorConfig) grpc.UnaryServerInterceptor {
	skipSet := make(map[string]struct{}, len(cfg.SkipMethods))
	for _, m := range cfg.SkipMethods {
		skipSet[m] = struct{}{}
	}

	return func(
		ctx context.Context,
		req interface{},
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (interface{}, error) {
		if _, skip := skipSet[info.FullMethod]; skip {
			return handler(ctx, req)
		}

		fingerprint, err := extractFingerprint(ctx)
		if err != nil {
			return nil, err
		}

		identity, err := cfg.Cache.Lookup(ctx, fingerprint)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "identity lookup failed: %v", err)
		}
		if identity == nil {
			return nil, status.Error(codes.Unauthenticated, "unknown client certificate")
		}

		if !strings.EqualFold(identity.GatewayID, cfg.GatewayID) {
			return nil, status.Error(codes.PermissionDenied, "GATEWAY_MISMATCH")
		}

		ctx = context.WithValue(ctx, ctxKeyNodeID, identity.NodeID)
		ctx = context.WithValue(ctx, ctxKeyGatewayID, identity.GatewayID)

		return handler(ctx, req)
	}
}

func extractFingerprint(ctx context.Context) (string, error) {
	p, ok := peer.FromContext(ctx)
	if !ok {
		return "", status.Error(codes.Unauthenticated, "no peer info")
	}

	tlsInfo, ok := p.AuthInfo.(credentials.TLSInfo)
	if !ok {
		return "", status.Error(codes.Unauthenticated, "no TLS info")
	}

	certs := tlsInfo.State.PeerCertificates
	if len(certs) == 0 {
		return "", status.Error(codes.Unauthenticated, "no client certificate")
	}

	return certFingerprint(certs[0]), nil
}

func certFingerprint(cert *x509.Certificate) string {
	hash := sha256.Sum256(cert.Raw)
	return hex.EncodeToString(hash[:])
}
