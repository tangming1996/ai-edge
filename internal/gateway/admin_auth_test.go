package gateway

import (
	"context"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func TestAdminSubjectFromContext_Missing(t *testing.T) {
	if got := AdminSubjectFromContext(context.Background()); got != "" {
		t.Fatalf("AdminSubjectFromContext(no ctx) = %q, want empty", got)
	}
}

func TestAdminSubjectFromContext_RoundTrip(t *testing.T) {
	ctx := context.WithValue(context.Background(), ctxKeyAdminSubject, "alice")
	if got := AdminSubjectFromContext(ctx); got != "alice" {
		t.Fatalf("got %q, want alice", got)
	}
}

func TestValidateStaticToken_EmptyMap(t *testing.T) {
	subject, ok := validateStaticToken(nil, "any")
	if ok {
		t.Fatal("nil token map must reject")
	}
	if subject != "" {
		t.Fatalf("expected empty subject, got %q", subject)
	}
}

func TestValidateStaticToken_EmptyToken(t *testing.T) {
	tokens := map[string]string{"abc": "alice"}
	if _, ok := validateStaticToken(tokens, ""); ok {
		t.Fatal("empty token must not match a non-empty stored token")
	}
}

func TestValidateStaticToken_Match(t *testing.T) {
	tokens := map[string]string{"abc": "alice", "def": "bob"}
	for tok, want := range tokens {
		got, ok := validateStaticToken(tokens, tok)
		if !ok {
			t.Errorf("token %q should match", tok)
			continue
		}
		if got != want {
			t.Errorf("token %q -> subject %q, want %q", tok, got, want)
		}
	}
}

func TestValidateStaticToken_NoMatch(t *testing.T) {
	tokens := map[string]string{"abc": "alice"}
	if _, ok := validateStaticToken(tokens, "xyz"); ok {
		t.Fatal("non-matching token must be rejected")
	}
}

func TestAdminAuthInterceptor_SkipMethod(t *testing.T) {
	called := false
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		called = true
		return "ok", nil
	}
	interceptor := NewAdminAuthInterceptor(AdminAuthConfig{
		StaticTokens: map[string]string{"abc": "alice"},
		SkipMethods:  []string{"/admin/health"},
	})
	out, err := interceptor(context.Background(), "req", fakeInfo("/admin/health"), handler)
	if err != nil {
		t.Fatalf("skip method errored: %v", err)
	}
	if out != "ok" {
		t.Fatalf("skip method output = %v, want ok", out)
	}
	if !called {
		t.Fatal("handler not invoked for skip method")
	}
}

func TestAdminAuthInterceptor_MissingMetadata(t *testing.T) {
	called := false
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		called = true
		return nil, nil
	}
	interceptor := NewAdminAuthInterceptor(AdminAuthConfig{
		StaticTokens: map[string]string{"abc": "alice"},
	})
	_, err := interceptor(context.Background(), "req", fakeInfo("/Some/Method"), handler)
	if err == nil {
		t.Fatal("expected error when metadata missing")
	}
	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("code = %v, want Unauthenticated", status.Code(err))
	}
	if called {
		t.Fatal("handler should not have been called")
	}
}

func TestAdminAuthInterceptor_MissingAuthHeader(t *testing.T) {
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return nil, nil
	}
	ctx := metadata.NewIncomingContext(context.Background(), metadata.MD{})
	interceptor := NewAdminAuthInterceptor(AdminAuthConfig{
		StaticTokens: map[string]string{"abc": "alice"},
	})
	_, err := interceptor(ctx, "req", fakeInfo("/x"), handler)
	if err == nil {
		t.Fatal("expected error when no authorization header")
	}
	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("code = %v, want Unauthenticated", status.Code(err))
	}
}

func TestAdminAuthInterceptor_NonBearerScheme(t *testing.T) {
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return nil, nil
	}
	md := metadata.NewIncomingContext(context.Background(), metadata.Pairs(
		"authorization", "Basic dXNlcjpwYXNz",
	))
	interceptor := NewAdminAuthInterceptor(AdminAuthConfig{
		StaticTokens: map[string]string{"abc": "alice"},
	})
	_, err := interceptor(md, "req", fakeInfo("/x"), handler)
	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("code = %v, want Unauthenticated", status.Code(err))
	}
}

func TestAdminAuthInterceptor_EmptyBearerToken(t *testing.T) {
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return nil, nil
	}
	md := metadata.NewIncomingContext(context.Background(), metadata.Pairs(
		"authorization", "Bearer ",
	))
	interceptor := NewAdminAuthInterceptor(AdminAuthConfig{
		StaticTokens: map[string]string{"abc": "alice"},
	})
	_, err := interceptor(md, "req", fakeInfo("/x"), handler)
	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("code = %v, want Unauthenticated", status.Code(err))
	}
}

func TestAdminAuthInterceptor_InvalidToken(t *testing.T) {
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return nil, nil
	}
	md := metadata.NewIncomingContext(context.Background(), metadata.Pairs(
		"authorization", "Bearer wrong",
	))
	interceptor := NewAdminAuthInterceptor(AdminAuthConfig{
		StaticTokens: map[string]string{"abc": "alice"},
	})
	_, err := interceptor(md, "req", fakeInfo("/x"), handler)
	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("code = %v, want Unauthenticated", status.Code(err))
	}
}

func TestAdminAuthInterceptor_ValidToken(t *testing.T) {
	var seenSubject string
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		seenSubject = AdminSubjectFromContext(ctx)
		return "ok", nil
	}
	md := metadata.NewIncomingContext(context.Background(), metadata.Pairs(
		"authorization", "Bearer abc",
	))
	interceptor := NewAdminAuthInterceptor(AdminAuthConfig{
		StaticTokens: map[string]string{"abc": "alice"},
	})
	out, err := interceptor(md, "req", fakeInfo("/x"), handler)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "ok" {
		t.Fatalf("output = %v, want ok", out)
	}
	if seenSubject != "alice" {
		t.Fatalf("subject = %q, want alice", seenSubject)
	}
}

func TestExtractBearerToken_Valid(t *testing.T) {
	md := metadata.NewIncomingContext(context.Background(), metadata.Pairs(
		"authorization", "Bearer xyz",
	))
	got, err := extractBearerToken(md)
	if err != nil {
		t.Fatalf("extractBearerToken: %v", err)
	}
	if got != "xyz" {
		t.Fatalf("token = %q, want xyz", got)
	}
}

func TestExtractBearerToken_PrefixOnly(t *testing.T) {
	md := metadata.NewIncomingContext(context.Background(), metadata.Pairs(
		"authorization", "Bearer",
	))
	_, err := extractBearerToken(md)
	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("code = %v, want Unauthenticated", status.Code(err))
	}
}
