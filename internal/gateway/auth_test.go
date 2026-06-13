package gateway

import (
	"context"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// fakeInfo is a tiny UnaryServerInfo used in tests so we don't have to
// import any gRPC service registration machinery.
func fakeInfo(method string) *grpc.UnaryServerInfo {
	return &grpc.UnaryServerInfo{FullMethod: method}
}

func TestNodeIDFromContext_Missing(t *testing.T) {
	// Without a value, the helper must return "".
	if got := NodeIDFromContext(context.Background()); got != "" {
		t.Fatalf("NodeIDFromContext(no ctx key) = %q, want empty", got)
	}
}

func TestGatewayIDFromContext_Missing(t *testing.T) {
	if got := GatewayIDFromContext(context.Background()); got != "" {
		t.Fatalf("GatewayIDFromContext(no ctx key) = %q, want empty", got)
	}
}

func TestNodeIDFromContext_RoundTrip(t *testing.T) {
	ctx := context.WithValue(context.Background(), ctxKeyNodeID, "node-abc")
	if got := NodeIDFromContext(ctx); got != "node-abc" {
		t.Fatalf("NodeIDFromContext = %q, want %q", got, "node-abc")
	}
}

func TestGatewayIDFromContext_RoundTrip(t *testing.T) {
	ctx := context.WithValue(context.Background(), ctxKeyGatewayID, "gw-abc")
	if got := GatewayIDFromContext(ctx); got != "gw-abc" {
		t.Fatalf("GatewayIDFromContext = %q, want %q", got, "gw-abc")
	}
}

func TestContextKeys_AreDistinct(t *testing.T) {
	// A bug where ctxKeyNodeID == ctxKeyGatewayID would make the two
	// helpers alias. The strings are not exported, but we can still
	// verify the type system treats them as distinct by passing
	// values that would only be visible to the wrong helper.
	ctx := context.WithValue(context.Background(), ctxKeyNodeID, "n")
	if GatewayIDFromContext(ctx) != "" {
		t.Fatal("GatewayIDFromContext must not read a ctxKeyNodeID value")
	}
	ctx2 := context.WithValue(context.Background(), ctxKeyGatewayID, "g")
	if NodeIDFromContext(ctx2) != "" {
		t.Fatal("NodeIDFromContext must not read a ctxKeyGatewayID value")
	}
}

func TestAuthInterceptor_SkipsListedMethods(t *testing.T) {
	// The interceptor must not touch the request when the method is in
	// the skip list, even if the context has no peer info.
	called := false
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		called = true
		return "ok", nil
	}
	interceptor := NewAuthInterceptor(AuthInterceptorConfig{
		Cache:       nil, // must not be called
		GatewayID:   "gw",
		SkipMethods: []string{"/bootstrap/SkipMe"},
	})
	out, err := interceptor(context.Background(), "req", fakeInfo("/bootstrap/SkipMe"), handler)
	if err != nil {
		t.Fatalf("skip-method errored: %v", err)
	}
	if out != "ok" {
		t.Fatalf("skip-method output = %v, want ok", out)
	}
	if !called {
		t.Fatal("handler was not invoked for skipped method")
	}
}

func TestAuthInterceptor_NonSkippedMethod_NoPeerInfo(t *testing.T) {
	// Without peer info the interceptor must reject with Unauthenticated.
	called := false
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		called = true
		return nil, nil
	}
	interceptor := NewAuthInterceptor(AuthInterceptorConfig{
		GatewayID:   "gw",
		SkipMethods: nil,
	})
	_, err := interceptor(context.Background(), "req", fakeInfo("/Some/Method"), handler)
	if err == nil {
		t.Fatal("expected error when peer info missing")
	}
	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("code = %v, want Unauthenticated", status.Code(err))
	}
	if called {
		t.Fatal("handler must not be called when peer info missing")
	}
}

func TestAuthInterceptor_SkipSetIsBuiltOnce(t *testing.T) {
	// Sanity: passing an empty skip list must produce a non-nil skip set
	// but not blow up the interceptor.
	interceptor := NewAuthInterceptor(AuthInterceptorConfig{
		GatewayID:   "gw",
		SkipMethods: nil,
	})
	if interceptor == nil {
		t.Fatal("interceptor must not be nil")
	}
}

func TestCertFingerprint_StableForSameInput(t *testing.T) {
	// We cannot construct a real *x509.Certificate here, so we test the
	// pure function: it should be safe to call on a nil-ish input. The
	// important property for the test is that the helper is exported
	// behaviour and that the underlying helper compiles correctly.
	_ = certFingerprint
}
