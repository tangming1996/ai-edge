package onboarding

import (
	"errors"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestSentinelErrors_AreGRPCStatusErrors(t *testing.T) {
	cases := []struct {
		name string
		err  error
		code codes.Code
		want string
	}{
		{"token not found", errTokenNotFound(), codes.NotFound, ReasonTokenNotFound},
		{"token expired", errTokenExpired(), codes.FailedPrecondition, ReasonTokenExpired},
		{"token exhausted", errTokenExhausted(), codes.ResourceExhausted, ReasonTokenExhausted},
		{"token frozen", errTokenFrozen(), codes.FailedPrecondition, ReasonTokenFrozen},
		{"gateway mismatch", errGatewayMismatch(), codes.InvalidArgument, ReasonGatewayMismatch},
		{"identity conflict", errIdentityConflict(), codes.AlreadyExists, ReasonIdentityConflict},
		{"identity revoked", errIdentityRevoked(), codes.PermissionDenied, ReasonIdentityRevoked},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			st, ok := status.FromError(tc.err)
			if !ok {
				t.Fatalf("expected grpc status error, got %T", tc.err)
			}
			if st.Code() != tc.code {
				t.Errorf("code=%v, want %v", st.Code(), tc.code)
			}
			if st.Message() != tc.want {
				t.Errorf("message=%q, want %q", st.Message(), tc.want)
			}
		})
	}
}

func TestSentinelErrors_AreDistinct(t *testing.T) {
	// None of the sentinels should be errors.Is equal to each other. They
	// only need to be distinguishable via status.FromError.
	all := []error{
		errTokenNotFound(),
		errTokenExpired(),
		errTokenExhausted(),
		errTokenFrozen(),
		errGatewayMismatch(),
		errIdentityConflict(),
		errIdentityRevoked(),
	}
	for i, a := range all {
		for j, b := range all {
			if i == j {
				continue
			}
			// Compare the underlying codes/messages rather than errors.Is,
			// because the helpers are factory functions that return a new
			// status error each time.
			sa, _ := status.FromError(a)
			sb, _ := status.FromError(b)
			if sa.Code() == sb.Code() && sa.Message() == sb.Message() {
				t.Errorf("sentinel #%d collides with #%d: %+v", i, j, a)
			}
		}
	}
}

func TestSentinelErrors_NotNil(t *testing.T) {
	for name, e := range map[string]error{
		"errTokenNotFound":    errTokenNotFound(),
		"errTokenExpired":     errTokenExpired(),
		"errTokenExhausted":   errTokenExhausted(),
		"errTokenFrozen":      errTokenFrozen(),
		"errGatewayMismatch":  errGatewayMismatch(),
		"errIdentityConflict": errIdentityConflict(),
		"errIdentityRevoked":  errIdentityRevoked(),
	} {
		if e == nil {
			t.Errorf("%s returned nil", name)
		}
	}
}

func TestErrCSRInvalid_IncludesReasonAndMessage(t *testing.T) {
	e := errCSRInvalid("signature missing")
	if e == nil {
		t.Fatal("nil error")
	}
	st, ok := status.FromError(e)
	if !ok {
		t.Fatal("not a grpc status")
	}
	if st.Code() != codes.InvalidArgument {
		t.Errorf("code=%v, want %v", st.Code(), codes.InvalidArgument)
	}
	if st.Message() != ReasonCSRInvalid+": signature missing" {
		t.Errorf("unexpected message: %q", st.Message())
	}
}

func TestSentinels_RecognisableViaStatusCode(t *testing.T) {
	// Callers rely on status.Code() to map these to HTTP/grpc codes.
	if status.Code(errTokenNotFound()) != codes.NotFound {
		t.Error("token not found -> NotFound")
	}
	if status.Code(errTokenExhausted()) != codes.ResourceExhausted {
		t.Error("token exhausted -> ResourceExhausted")
	}
	if status.Code(errIdentityConflict()) != codes.AlreadyExists {
		t.Error("identity conflict -> AlreadyExists")
	}
}

func TestSentinels_DistinctFromGenericErrors(t *testing.T) {
	plain := errors.New("plain")
	if status.Code(plain) == codes.NotFound {
		// A non-status error is not a NotFound.
		t.Fatal("plain error should not be NotFound")
	}
}
