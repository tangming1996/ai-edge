package gateway

import (
	"context"
	"crypto/subtle"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

type adminContextKey string

const ctxKeyAdminSubject adminContextKey = "gateway.admin_subject"

// AdminSubjectFromContext extracts the authenticated admin subject from context.
func AdminSubjectFromContext(ctx context.Context) string {
	v, _ := ctx.Value(ctxKeyAdminSubject).(string)
	return v
}

// AdminAuthConfig configures the management-plane auth interceptor.
type AdminAuthConfig struct {
	// StaticTokens maps token -> subject for V1 static token auth.
	StaticTokens map[string]string

	// SkipMethods lists full gRPC method names that bypass admin auth.
	SkipMethods []string
}

// NewAdminAuthInterceptor returns a gRPC UnaryServerInterceptor for
// management-plane authentication. V1 supports static Bearer token
// validation extracted from gRPC metadata.
func NewAdminAuthInterceptor(cfg AdminAuthConfig) grpc.UnaryServerInterceptor {
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

		token, err := extractBearerToken(ctx)
		if err != nil {
			return nil, err
		}

		subject, ok := validateStaticToken(cfg.StaticTokens, token)
		if !ok {
			return nil, status.Error(codes.Unauthenticated, "invalid or expired token")
		}

		ctx = context.WithValue(ctx, ctxKeyAdminSubject, subject)
		return handler(ctx, req)
	}
}

func extractBearerToken(ctx context.Context) (string, error) {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return "", status.Error(codes.Unauthenticated, "missing metadata")
	}

	values := md.Get("authorization")
	if len(values) == 0 {
		return "", status.Error(codes.Unauthenticated, "missing authorization header")
	}

	auth := values[0]
	const prefix = "Bearer "
	if !strings.HasPrefix(auth, prefix) {
		return "", status.Error(codes.Unauthenticated, "authorization must use Bearer scheme")
	}

	token := strings.TrimPrefix(auth, prefix)
	if token == "" {
		return "", status.Error(codes.Unauthenticated, "empty bearer token")
	}
	return token, nil
}

func validateStaticToken(tokens map[string]string, token string) (string, bool) {
	if len(tokens) == 0 {
		return "", false
	}

	for t, subject := range tokens {
		if subtle.ConstantTimeCompare([]byte(t), []byte(token)) == 1 {
			return subject, true
		}
	}
	return "", false
}
