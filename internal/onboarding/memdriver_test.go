//go:build !integration

package onboarding_test

import (
	"context"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"database/sql/driver"

	pb "github.com/edgeai-platform/ai-edge/api/gen/go/edge/ai/api/v1"
	"github.com/edgeai-platform/ai-edge/internal/onboarding"
	"github.com/edgeai-platform/ai-edge/internal/pki"
	"github.com/edgeai-platform/ai-edge/internal/store"
)

// row is a convenience constructor for a mem-driver single-row fixture.
func row(v ...driver.Value) []driver.Value { return v }

// newMemOnboarding returns a mem-backed token store + identity gRPC
// service. Tests configure SQL responses via the mem driver and then
// invoke the public gRPC methods to assert behaviour.
func newMemOnboarding(t *testing.T) (*onboarding.TokenStore, *onboarding.IdentityGRPC) {
	t.Helper()
	store.ResetMemDB()
	db := store.NewMemStore()
	tokens := onboarding.NewTokenStore(db)
	// IdentityGRPC.RevokeIdentity delegates to BootstrapService.RevokeNode.
	// Build a real (but non-functional outside SQL fixture) signer so the
	// delegation path is reachable.
	caCert, caKey, err := pki.GenerateSelfSignedCA("Test Root CA", 10*365*24*time.Hour)
	if err != nil {
		t.Fatalf("generate CA: %v", err)
	}
	signer, err := pki.NewSigner(pki.SignerConfig{CACertPEM: caCert, CAKeyPEM: caKey})
	if err != nil {
		t.Fatalf("new signer: %v", err)
	}
	bs := onboarding.NewBootstrapService(db, tokens, signer)
	return tokens, onboarding.NewIdentityGRPC(db, bs)
}

func now() time.Time { return time.Now() }

// --- TokenStore tests ---

func TestTokenStore_Create_HappyPath(t *testing.T) {
	store.ResetMemDB()
	db := store.NewMemStore()
	ts := onboarding.NewTokenStore(db)

	store.SetRowForQuery("INSERT INTO bootstrap_tokens", row(
		"id-1", "gw-1", "hash", "test", []byte(`{}`),
		3, 0, "Active", now().Add(time.Hour), now(), now(),
	))
	rec, plain, err := ts.Create(context.Background(), "gw-1", "test", nil, 3, time.Hour)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if rec == nil {
		t.Fatal("nil record")
	}
	if plain == "" {
		t.Error("empty plaintext token")
	}
	if rec.ID != "id-1" {
		t.Errorf("ID = %q", rec.ID)
	}
}

func TestTokenStore_GetByID_HappyPath(t *testing.T) {
	store.ResetMemDB()
	db := store.NewMemStore()
	ts := onboarding.NewTokenStore(db)

	store.SetRowForQuery("FROM bootstrap_tokens WHERE id = $1", row(
		"id-1", "gw-1", "hash", "test", []byte(`{}`),
		3, 0, "Active", now().Add(time.Hour), now(), now(),
	))
	rec, err := ts.GetByID(context.Background(), "id-1")
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if rec.ID != "id-1" {
		t.Errorf("ID = %q", rec.ID)
	}
}

func TestTokenStore_GetByID_NotFound(t *testing.T) {
	store.ResetMemDB()
	db := store.NewMemStore()
	ts := onboarding.NewTokenStore(db)

	store.SetNoRowsForQuery("FROM bootstrap_tokens WHERE id = $1")
	_, err := ts.GetByID(context.Background(), "missing")
	if got := status.Code(err); got != codes.NotFound {
		t.Fatalf("code = %v, want NotFound", got)
	}
}

func TestTokenStore_UpdateStatus_HappyPath(t *testing.T) {
	store.ResetMemDB()
	db := store.NewMemStore()
	ts := onboarding.NewTokenStore(db)

	store.SetRowForQuery("UPDATE bootstrap_tokens", row(
		"id-1", "gw-1", "hash", "test", []byte(`{}`),
		3, 0, "Frozen", now().Add(time.Hour), now(), now(),
	))
	rec, err := ts.UpdateStatus(context.Background(), "id-1", "Frozen")
	if err != nil {
		t.Fatalf("UpdateStatus: %v", err)
	}
	if rec.Status != "Frozen" {
		t.Errorf("status = %q", rec.Status)
	}
}

func TestTokenStore_UpdateStatus_NotFound(t *testing.T) {
	store.ResetMemDB()
	db := store.NewMemStore()
	ts := onboarding.NewTokenStore(db)

	store.SetNoRowsForQuery("UPDATE bootstrap_tokens")
	_, err := ts.UpdateStatus(context.Background(), "missing", "Frozen")
	if got := status.Code(err); got != codes.NotFound {
		t.Fatalf("code = %v, want NotFound", got)
	}
}

func TestTokenStore_ValidateAndConsume_HappyPath(t *testing.T) {
	store.ResetMemDB()
	db := store.NewMemStore()
	ts := onboarding.NewTokenStore(db)
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	storeTx := &store.Tx{Tx: tx}

	store.SetRowForQuery("FROM bootstrap_tokens WHERE token_hash = $1", row(
		"id-1", "gw-1", "hash", "test", []byte(`{}`),
		3, 0, "Active", now().Add(time.Hour), now(), now(),
	))
	store.SetRowsAffectedForQuery("UPDATE bootstrap_tokens SET used_count", 1)

	rec, err := ts.ValidateAndConsume(context.Background(), storeTx, "tokenplain", "gw-1")
	if err != nil {
		t.Fatalf("ValidateAndConsume: %v", err)
	}
	if rec.UsedCount != 1 {
		t.Errorf("UsedCount = %d, want 1", rec.UsedCount)
	}
}

func TestTokenStore_ValidateAndConsume_NotFound(t *testing.T) {
	store.ResetMemDB()
	db := store.NewMemStore()
	ts := onboarding.NewTokenStore(db)
	tx, _ := db.BeginTx(context.Background(), nil)
	storeTx := &store.Tx{Tx: tx}

	store.SetNoRowsForQuery("FROM bootstrap_tokens WHERE token_hash = $1")
	_, err := ts.ValidateAndConsume(context.Background(), storeTx, "token", "gw-1")
	if got := status.Code(err); got != codes.NotFound {
		t.Fatalf("code = %v, want NotFound", got)
	}
}

func TestTokenStore_ValidateAndConsume_Revoked(t *testing.T) {
	store.ResetMemDB()
	db := store.NewMemStore()
	ts := onboarding.NewTokenStore(db)
	tx, _ := db.BeginTx(context.Background(), nil)
	storeTx := &store.Tx{Tx: tx}

	store.SetRowForQuery("FROM bootstrap_tokens WHERE token_hash = $1", row(
		"id-1", "gw-1", "hash", "test", []byte(`{}`),
		3, 0, "Revoked", now().Add(time.Hour), now(), now(),
	))
	_, err := ts.ValidateAndConsume(context.Background(), storeTx, "tokenplain", "gw-1")
	if got := status.Code(err); got != codes.NotFound {
		t.Fatalf("code = %v, want NotFound", got)
	}
}

func TestTokenStore_ValidateAndConsume_Frozen(t *testing.T) {
	store.ResetMemDB()
	db := store.NewMemStore()
	ts := onboarding.NewTokenStore(db)
	tx, _ := db.BeginTx(context.Background(), nil)
	storeTx := &store.Tx{Tx: tx}

	store.SetRowForQuery("FROM bootstrap_tokens WHERE token_hash = $1", row(
		"id-1", "gw-1", "hash", "test", []byte(`{}`),
		3, 0, "Frozen", now().Add(time.Hour), now(), now(),
	))
	_, err := ts.ValidateAndConsume(context.Background(), storeTx, "tokenplain", "gw-1")
	if got := status.Code(err); got != codes.FailedPrecondition {
		t.Fatalf("code = %v, want FailedPrecondition", got)
	}
}

func TestTokenStore_ValidateAndConsume_Expired(t *testing.T) {
	store.ResetMemDB()
	db := store.NewMemStore()
	ts := onboarding.NewTokenStore(db)
	tx, _ := db.BeginTx(context.Background(), nil)
	storeTx := &store.Tx{Tx: tx}

	store.SetRowForQuery("FROM bootstrap_tokens WHERE token_hash = $1", row(
		"id-1", "gw-1", "hash", "test", []byte(`{}`),
		3, 0, "Active", now().Add(-time.Hour), now(), now(),
	))
	_, err := ts.ValidateAndConsume(context.Background(), storeTx, "tokenplain", "gw-1")
	if got := status.Code(err); got != codes.FailedPrecondition {
		t.Fatalf("code = %v, want FailedPrecondition", got)
	}
}

func TestTokenStore_ValidateAndConsume_Exhausted(t *testing.T) {
	store.ResetMemDB()
	db := store.NewMemStore()
	ts := onboarding.NewTokenStore(db)
	tx, _ := db.BeginTx(context.Background(), nil)
	storeTx := &store.Tx{Tx: tx}

	store.SetRowForQuery("FROM bootstrap_tokens WHERE token_hash = $1", row(
		"id-1", "gw-1", "hash", "test", []byte(`{}`),
		1, 1, "Active", now().Add(time.Hour), now(), now(),
	))
	_, err := ts.ValidateAndConsume(context.Background(), storeTx, "tokenplain", "gw-1")
	if got := status.Code(err); got != codes.ResourceExhausted {
		t.Fatalf("code = %v, want ResourceExhausted", got)
	}
}

func TestTokenStore_ValidateAndConsume_GatewayMismatch(t *testing.T) {
	store.ResetMemDB()
	db := store.NewMemStore()
	ts := onboarding.NewTokenStore(db)
	tx, _ := db.BeginTx(context.Background(), nil)
	storeTx := &store.Tx{Tx: tx}

	store.SetRowForQuery("FROM bootstrap_tokens WHERE token_hash = $1", row(
		"id-1", "gw-other", "hash", "test", []byte(`{}`),
		3, 0, "Active", now().Add(time.Hour), now(), now(),
	))
	_, err := ts.ValidateAndConsume(context.Background(), storeTx, "tokenplain", "gw-1")
	if got := status.Code(err); got != codes.InvalidArgument {
		t.Fatalf("code = %v, want InvalidArgument", got)
	}
}

// --- IdentityGRPC tests ---

func TestIdentityGRPC_GetIdentity_MissingID(t *testing.T) {
	_, svc := newMemOnboarding(t)
	_, err := svc.GetIdentity(context.Background(), &pb.GetIdentityRequest{})
	if got := status.Code(err); got != codes.InvalidArgument {
		t.Fatalf("code = %v, want InvalidArgument", got)
	}
}

func TestIdentityGRPC_GetIdentity_NotFound(t *testing.T) {
	_, svc := newMemOnboarding(t)
	store.SetNoRowsForQuery("FROM edge_identities WHERE id = $1")
	_, err := svc.GetIdentity(context.Background(), &pb.GetIdentityRequest{Id: "x"})
	if got := status.Code(err); got != codes.NotFound {
		t.Fatalf("code = %v, want NotFound", got)
	}
}

func TestIdentityGRPC_GetIdentity_HappyPath(t *testing.T) {
	_, svc := newMemOnboarding(t)
	store.SetRowForQuery("FROM edge_identities WHERE id = $1", row(
		"id-1", "n-1", "gw-1", "serial-1", "fp", "Active", "PEM",
		now().Add(24*time.Hour), now(), nil, now(), now(),
	))
	resp, err := svc.GetIdentity(context.Background(), &pb.GetIdentityRequest{Id: "id-1"})
	if err != nil {
		t.Fatalf("GetIdentity: %v", err)
	}
	if resp.GetIdentity().GetId() != "id-1" {
		t.Errorf("Id = %q", resp.GetIdentity().GetId())
	}
	if resp.GetIdentity().GetStatus() != pb.IdentityStatus_IDENTITY_STATUS_ACTIVE {
		t.Errorf("Status = %v", resp.GetIdentity().GetStatus())
	}
}

func TestIdentityGRPC_GetIdentity_InternalError(t *testing.T) {
	_, svc := newMemOnboarding(t)
	store.SetErrorForQuery("FROM edge_identities WHERE id = $1", errBoom)
	_, err := svc.GetIdentity(context.Background(), &pb.GetIdentityRequest{Id: "x"})
	if got := status.Code(err); got != codes.Internal {
		t.Fatalf("code = %v, want Internal", got)
	}
}

func TestIdentityGRPC_ListIdentities_Empty(t *testing.T) {
	_, svc := newMemOnboarding(t)
	resp, err := svc.ListIdentities(context.Background(), &pb.ListIdentitiesRequest{})
	if err != nil {
		t.Fatalf("ListIdentities: %v", err)
	}
	if len(resp.GetIdentities()) != 0 {
		t.Errorf("expected 0 identities, got %d", len(resp.GetIdentities()))
	}
}

func TestIdentityGRPC_ListIdentities_WithNodeID(t *testing.T) {
	_, svc := newMemOnboarding(t)
	store.SetRowForQuery("ORDER BY created_at DESC", row(
		"id-1", "n-1", "gw-1", "serial-1", "fp", "Active", "PEM",
		now().Add(24*time.Hour), now(), nil, now(), now(),
	))
	resp, err := svc.ListIdentities(context.Background(), &pb.ListIdentitiesRequest{
		NodeId: "n-1",
	})
	if err != nil {
		t.Fatalf("ListIdentities: %v", err)
	}
	if len(resp.GetIdentities()) != 1 {
		t.Errorf("expected 1 identity, got %d", len(resp.GetIdentities()))
	}
}

func TestIdentityGRPC_ListIdentities_PageConfig(t *testing.T) {
	_, svc := newMemOnboarding(t)
	resp, err := svc.ListIdentities(context.Background(), &pb.ListIdentitiesRequest{
		Page: &pb.PageRequest{PageSize: 50, PageToken: "10"},
	})
	if err != nil {
		t.Fatalf("ListIdentities: %v", err)
	}
	if resp == nil {
		t.Fatal("nil response")
	}
}

func TestIdentityGRPC_ListIdentities_QueryError(t *testing.T) {
	_, svc := newMemOnboarding(t)
	store.SetErrorForQuery("FROM edge_identities WHERE 1=1", errBoom)
	_, err := svc.ListIdentities(context.Background(), &pb.ListIdentitiesRequest{})
	if got := status.Code(err); got != codes.Internal {
		t.Fatalf("code = %v, want Internal", got)
	}
}

func TestIdentityGRPC_RevokeIdentity_MissingID(t *testing.T) {
	_, svc := newMemOnboarding(t)
	_, err := svc.RevokeIdentity(context.Background(), &pb.RevokeIdentityRequest{})
	if got := status.Code(err); got != codes.InvalidArgument {
		t.Fatalf("code = %v, want InvalidArgument", got)
	}
}

func TestIdentityGRPC_RevokeIdentity_NoActive(t *testing.T) {
	_, svc := newMemOnboarding(t)
	store.SetRowsAffectedForQuery("UPDATE edge_identities SET status = 'Revoked'", 0)
	_, err := svc.RevokeIdentity(context.Background(), &pb.RevokeIdentityRequest{Id: "n-1"})
	if err == nil {
		t.Fatal("expected error")
	}
}

// errBoom is a generic error used across the onboarding tests.
var errBoom = errSentinel("kaboom")

type errSentinel string

func (e errSentinel) Error() string { return string(e) }
