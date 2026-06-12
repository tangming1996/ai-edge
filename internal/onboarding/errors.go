package onboarding

import (
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Business error reasons surfaced via gRPC status + ErrorInfo.
const (
	ReasonTokenNotFound     = "TOKEN_NOT_FOUND"
	ReasonTokenExpired      = "TOKEN_EXPIRED"
	ReasonTokenExhausted    = "TOKEN_EXHAUSTED"
	ReasonTokenFrozen       = "TOKEN_FROZEN"
	ReasonTokenRevoked      = "TOKEN_REVOKED"
	ReasonGatewayMismatch   = "GATEWAY_MISMATCH"
	ReasonIdentityConflict  = "IDENTITY_CONFLICT"
	ReasonCSRInvalid        = "CSR_INVALID"
	ReasonIdentityRevoked   = "IDENTITY_REVOKED"
	ReasonIdentityNotFound  = "IDENTITY_NOT_FOUND"
	ReasonNodeNotFound      = "NODE_NOT_FOUND"
	ReasonRenewNotAllowed   = "RENEW_NOT_ALLOWED"
)

func errTokenNotFound() error {
	return status.Errorf(codes.NotFound, ReasonTokenNotFound)
}

func errTokenExpired() error {
	return status.Errorf(codes.FailedPrecondition, ReasonTokenExpired)
}

func errTokenExhausted() error {
	return status.Errorf(codes.ResourceExhausted, ReasonTokenExhausted)
}

func errTokenFrozen() error {
	return status.Errorf(codes.FailedPrecondition, ReasonTokenFrozen)
}

func errGatewayMismatch() error {
	return status.Errorf(codes.InvalidArgument, ReasonGatewayMismatch)
}

func errIdentityConflict() error {
	return status.Errorf(codes.AlreadyExists, ReasonIdentityConflict)
}

func errCSRInvalid(msg string) error {
	return status.Errorf(codes.InvalidArgument, "%s: %s", ReasonCSRInvalid, msg)
}

func errIdentityRevoked() error {
	return status.Errorf(codes.PermissionDenied, ReasonIdentityRevoked)
}
