//go:build !integration

package gateway

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/edgeai-platform/ai-edge/api/gen/go/edge/ai/api/v1"
	"github.com/edgeai-platform/ai-edge/internal/store"
)

// Substrings used to match SQL queries issued by the gateway service.
// The mem driver dispatches by substring; keep these constant so a
// rename of an SQL fragment does not silently break tests.
const (
	insertGatewaySQL  = "INSERT INTO gateways"
	selectGatewayByID = "FROM gateways WHERE id = $1"
	updateGatewaySQL  = "UPDATE gateways SET labels"
	deleteGatewaySQL  = "UPDATE gateways SET status = 'Deleted'"
	countGatewaysSQL  = "SELECT COUNT(*) FROM gateways"
)

func newTestGatewayService(t *testing.T) *GatewayManagementService {
	t.Helper()
	store.ResetMemDB()
	db := store.NewMemStore()
	return NewGatewayManagementService(db)
}

func TestGatewayManagementService_NewGatewayManagementService(t *testing.T) {
	svc := newTestGatewayService(t)
	if svc == nil {
		t.Fatal("nil service")
	}
}

func TestGatewayManagementService_CreateGateway_MissingName(t *testing.T) {
	svc := newTestGatewayService(t)
	_, err := svc.CreateGateway(context.Background(), &pb.CreateGatewayRequest{})
	if err == nil {
		t.Fatal("expected error for missing name")
	}
	if got := status.Code(err); got != codes.InvalidArgument {
		t.Fatalf("code = %v, want InvalidArgument", got)
	}
}

func TestGatewayManagementService_CreateGateway_HappyPath(t *testing.T) {
	svc := newTestGatewayService(t)
	now := time.Date(2026, 6, 13, 12, 0, 0, 0, time.UTC)
	store.SetRowForQuery(insertGatewaySQL, []driver.Value{
		"id-1", "gw-1", "us", []byte(`{}`), "Active", "https://gw-1:7443", now, now,
	})
	resp, err := svc.CreateGateway(context.Background(), &pb.CreateGatewayRequest{
		Name:     "gw-1",
		Region:   "us",
		Endpoint: "https://gw-1:7443",
	})
	if err != nil {
		t.Fatalf("CreateGateway: %v", err)
	}
	if resp.GetGateway().GetId() != "id-1" {
		t.Errorf("Id = %q, want id-1", resp.GetGateway().GetId())
	}
	if resp.GetGateway().GetName() != "gw-1" {
		t.Errorf("Name = %q, want gw-1", resp.GetGateway().GetName())
	}
}

func TestGatewayManagementService_CreateGateway_UniqueViolation(t *testing.T) {
	svc := newTestGatewayService(t)
	store.SetErrorForQuery(insertGatewaySQL, &uniqueViolation{})
	_, err := svc.CreateGateway(context.Background(), &pb.CreateGatewayRequest{
		Name: "gw-1",
	})
	if err == nil {
		t.Fatal("expected error for unique violation")
	}
	if got := status.Code(err); got != codes.AlreadyExists {
		t.Fatalf("code = %v, want AlreadyExists", got)
	}
}

func TestGatewayManagementService_CreateGateway_InternalError(t *testing.T) {
	svc := newTestGatewayService(t)
	store.SetErrorForQuery(insertGatewaySQL, errInternal)
	_, err := svc.CreateGateway(context.Background(), &pb.CreateGatewayRequest{
		Name: "gw-1",
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if got := status.Code(err); got != codes.Internal {
		t.Fatalf("code = %v, want Internal", got)
	}
}

func TestGatewayManagementService_GetGateway_MissingID(t *testing.T) {
	svc := newTestGatewayService(t)
	_, err := svc.GetGateway(context.Background(), &pb.GetGatewayRequest{})
	if err == nil {
		t.Fatal("expected error for missing id")
	}
	if got := status.Code(err); got != codes.InvalidArgument {
		t.Fatalf("code = %v, want InvalidArgument", got)
	}
}

func TestGatewayManagementService_GetGateway_NotFound(t *testing.T) {
	svc := newTestGatewayService(t)
	store.SetNoRowsForQuery(selectGatewayByID)
	_, err := svc.GetGateway(context.Background(), &pb.GetGatewayRequest{Id: "missing-id"})
	if err == nil {
		t.Fatal("expected error for missing row")
	}
	if got := status.Code(err); got != codes.NotFound {
		t.Fatalf("code = %v, want NotFound", got)
	}
}

func TestGatewayManagementService_GetGateway_InternalError(t *testing.T) {
	svc := newTestGatewayService(t)
	store.SetErrorForQuery(selectGatewayByID, errInternal)
	_, err := svc.GetGateway(context.Background(), &pb.GetGatewayRequest{Id: "x"})
	if err == nil {
		t.Fatal("expected error")
	}
	if got := status.Code(err); got != codes.Internal {
		t.Fatalf("code = %v, want Internal", got)
	}
}

func TestGatewayManagementService_GetGateway_HappyPath(t *testing.T) {
	svc := newTestGatewayService(t)
	now := time.Date(2026, 6, 13, 12, 0, 0, 0, time.UTC)
	store.SetRowForQuery(selectGatewayByID, []driver.Value{
		"id-1", "gw-1", "us", []byte(`{"env":"prod"}`), "Active", "https://gw-1:7443", now, now,
	})
	resp, err := svc.GetGateway(context.Background(), &pb.GetGatewayRequest{Id: "id-1"})
	if err != nil {
		t.Fatalf("GetGateway: %v", err)
	}
	if resp.GetGateway().GetId() != "id-1" {
		t.Errorf("Id = %q", resp.GetGateway().GetId())
	}
	if got := resp.GetGateway().GetLabels().GetItems()["env"]; got != "prod" {
		t.Errorf("env label = %q", got)
	}
}

func TestGatewayManagementService_UpdateGateway_MissingID(t *testing.T) {
	svc := newTestGatewayService(t)
	_, err := svc.UpdateGateway(context.Background(), &pb.UpdateGatewayRequest{})
	if err == nil {
		t.Fatal("expected error for missing id")
	}
	if got := status.Code(err); got != codes.InvalidArgument {
		t.Fatalf("code = %v, want InvalidArgument", got)
	}
}

func TestGatewayManagementService_UpdateGateway_NotFound(t *testing.T) {
	svc := newTestGatewayService(t)
	store.SetNoRowsForQuery(updateGatewaySQL)
	_, err := svc.UpdateGateway(context.Background(), &pb.UpdateGatewayRequest{Id: "nope"})
	if err == nil {
		t.Fatal("expected error for missing row")
	}
	if got := status.Code(err); got != codes.NotFound {
		t.Fatalf("code = %v, want NotFound", got)
	}
}

func TestGatewayManagementService_UpdateGateway_InternalError(t *testing.T) {
	svc := newTestGatewayService(t)
	store.SetErrorForQuery(updateGatewaySQL, errInternal)
	_, err := svc.UpdateGateway(context.Background(), &pb.UpdateGatewayRequest{Id: "x"})
	if err == nil {
		t.Fatal("expected error")
	}
	if got := status.Code(err); got != codes.Internal {
		t.Fatalf("code = %v, want Internal", got)
	}
}

func TestGatewayManagementService_UpdateGateway_HappyPath(t *testing.T) {
	svc := newTestGatewayService(t)
	now := time.Date(2026, 6, 13, 12, 0, 0, 0, time.UTC)
	store.SetRowForQuery(updateGatewaySQL, []driver.Value{
		"id-1", "gw-1", "us", []byte(`{"env":"prod"}`), "Active", "https://gw-1:7443", now, now,
	})
	resp, err := svc.UpdateGateway(context.Background(), &pb.UpdateGatewayRequest{
		Id:       "id-1",
		Endpoint: "https://gw-1:8443",
		Labels:   &pb.Labels{Items: map[string]string{"env": "prod"}},
	})
	if err != nil {
		t.Fatalf("UpdateGateway: %v", err)
	}
	if resp.GetGateway().GetId() != "id-1" {
		t.Errorf("Id = %q", resp.GetGateway().GetId())
	}
}

func TestGatewayManagementService_DeleteGateway_MissingID(t *testing.T) {
	svc := newTestGatewayService(t)
	_, err := svc.DeleteGateway(context.Background(), &pb.DeleteGatewayRequest{})
	if err == nil {
		t.Fatal("expected error for missing id")
	}
	if got := status.Code(err); got != codes.InvalidArgument {
		t.Fatalf("code = %v, want InvalidArgument", got)
	}
}

func TestGatewayManagementService_DeleteGateway_NotFound(t *testing.T) {
	svc := newTestGatewayService(t)
	store.SetRowsAffectedForQuery(deleteGatewaySQL, 0)
	_, err := svc.DeleteGateway(context.Background(), &pb.DeleteGatewayRequest{Id: "nope"})
	if err == nil {
		t.Fatal("expected error for missing row")
	}
	if got := status.Code(err); got != codes.NotFound {
		t.Fatalf("code = %v, want NotFound", got)
	}
}

func TestGatewayManagementService_DeleteGateway_InternalError(t *testing.T) {
	svc := newTestGatewayService(t)
	store.SetErrorForQuery(deleteGatewaySQL, errInternal)
	_, err := svc.DeleteGateway(context.Background(), &pb.DeleteGatewayRequest{Id: "x"})
	if err == nil {
		t.Fatal("expected error")
	}
	if got := status.Code(err); got != codes.Internal {
		t.Fatalf("code = %v, want Internal", got)
	}
}

func TestGatewayManagementService_DeleteGateway_HappyPath(t *testing.T) {
	svc := newTestGatewayService(t)
	// default for exec with no fixture is 1 row, so the happy path
	// requires no setup.
	_, err := svc.DeleteGateway(context.Background(), &pb.DeleteGatewayRequest{Id: "id-1"})
	if err != nil {
		t.Fatalf("DeleteGateway: %v", err)
	}
}

func TestGatewayManagementService_ListGateways_Empty(t *testing.T) {
	svc := newTestGatewayService(t)
	resp, err := svc.ListGateways(context.Background(), &pb.ListGatewaysRequest{})
	if err != nil {
		t.Fatalf("ListGateways: %v", err)
	}
	if resp == nil {
		t.Fatal("nil response")
	}
	if len(resp.GetGateways()) != 0 {
		t.Errorf("expected empty list, got %d", len(resp.GetGateways()))
	}
	if resp.GetPage() == nil {
		t.Fatal("missing page response")
	}
	if resp.GetPage().GetTotalCount() != 0 {
		t.Errorf("total = %d, want 0", resp.GetPage().GetTotalCount())
	}
}

func TestGatewayManagementService_ListGateways_CountError(t *testing.T) {
	svc := newTestGatewayService(t)
	store.SetErrorForQuery(countGatewaysSQL, errInternal)
	_, err := svc.ListGateways(context.Background(), &pb.ListGatewaysRequest{})
	if err == nil {
		t.Fatal("expected error")
	}
	if got := status.Code(err); got != codes.Internal {
		t.Fatalf("code = %v, want Internal", got)
	}
}

func TestGatewayManagementService_ListGateways_WithRegion(t *testing.T) {
	svc := newTestGatewayService(t)
	// count returns 1; data returns one row matching the region.
	store.SetRowForQuery(countGatewaysSQL, []driver.Value{int64(1)})
	now := time.Date(2026, 6, 13, 12, 0, 0, 0, time.UTC)
	store.SetRowForQuery("AND region = $1", []driver.Value{
		"id-1", "gw-1", "us-east-1", []byte(`{}`), "Active", "https://gw-1:7443", now, now,
	})
	resp, err := svc.ListGateways(context.Background(), &pb.ListGatewaysRequest{
		Region: "us-east-1",
	})
	if err != nil {
		t.Fatalf("ListGateways: %v", err)
	}
	if len(resp.GetGateways()) != 1 {
		t.Errorf("expected 1 gateway, got %d", len(resp.GetGateways()))
	}
	if resp.GetPage().GetTotalCount() != 1 {
		t.Errorf("total = %d, want 1", resp.GetPage().GetTotalCount())
	}
}

func TestGatewayManagementService_ListGateways_PageConfig(t *testing.T) {
	svc := newTestGatewayService(t)
	// count returns 200, pageSize=50, so next token should be "50".
	store.SetRowForQuery(countGatewaysSQL, []driver.Value{int64(200)})
	resp, err := svc.ListGateways(context.Background(), &pb.ListGatewaysRequest{
		Page: &pb.PageRequest{PageSize: 50, PageToken: "10"},
	})
	if err != nil {
		t.Fatalf("ListGateways: %v", err)
	}
	if resp.GetPage().GetNextPageToken() != "60" {
		t.Errorf("next = %q, want 60", resp.GetPage().GetNextPageToken())
	}
	if resp.GetPage().GetTotalCount() != 200 {
		t.Errorf("total = %d, want 200", resp.GetPage().GetTotalCount())
	}
}

func TestGatewayManagementService_ListGateways_InvalidPageToken(t *testing.T) {
	// Non-numeric page token is silently ignored.
	svc := newTestGatewayService(t)
	resp, err := svc.ListGateways(context.Background(), &pb.ListGatewaysRequest{
		Page: &pb.PageRequest{PageToken: "not-a-number"},
	})
	if err != nil {
		t.Fatalf("ListGateways: %v", err)
	}
	if resp == nil {
		t.Fatal("nil response")
	}
}

func TestGatewayManagementService_ListGateways_NoMorePages(t *testing.T) {
	svc := newTestGatewayService(t)
	store.SetRowForQuery(countGatewaysSQL, []driver.Value{int64(0)})
	resp, err := svc.ListGateways(context.Background(), &pb.ListGatewaysRequest{
		Page: &pb.PageRequest{PageSize: 20, PageToken: "0"},
	})
	if err != nil {
		t.Fatalf("ListGateways: %v", err)
	}
	if resp.GetPage().GetNextPageToken() != "" {
		t.Errorf("next = %q, want empty", resp.GetPage().GetNextPageToken())
	}
}

func TestGatewayManagementService_ListGateways_QueryError(t *testing.T) {
	svc := newTestGatewayService(t)
	store.SetErrorForQuery("FROM gateways WHERE 1=1", errInternal)
	_, err := svc.ListGateways(context.Background(), &pb.ListGatewaysRequest{})
	if err == nil {
		t.Fatal("expected error")
	}
	if got := status.Code(err); got != codes.Internal {
		t.Fatalf("code = %v, want Internal", got)
	}
}

func TestMarshalLabels_NilInput(t *testing.T) {
	got := marshalLabels(nil)
	if string(got) != "{}" {
		t.Errorf("marshalLabels(nil) = %q, want %q", got, "{}")
	}
}

func TestMarshalLabels_EmptyItems(t *testing.T) {
	got := marshalLabels(&pb.Labels{Items: map[string]string{}})
	if string(got) != "{}" {
		t.Errorf("marshalLabels(empty) = %q, want %q", got, "{}")
	}
}

func TestMarshalLabels_NonEmptyItems(t *testing.T) {
	got := marshalLabels(&pb.Labels{Items: map[string]string{"env": "prod"}})
	if string(got) != `{"env":"prod"}` {
		t.Errorf("marshalLabels = %q, want %q", got, `{"env":"prod"}`)
	}
}

func TestRowToGatewayProto_NoLabels(t *testing.T) {
	in := &gatewayRow{
		ID:       "id-1",
		Name:     "gw-1",
		Region:   "us",
		Labels:   nil,
		Status:   "Active",
		Endpoint: "https://gw-1:7443",
	}
	got := rowToGatewayProto(in)
	if got.GetId() != "id-1" {
		t.Errorf("Id = %q", got.GetId())
	}
	if got.GetName() != "gw-1" {
		t.Errorf("Name = %q", got.GetName())
	}
	if got.GetRegion() != "us" {
		t.Errorf("Region = %q", got.GetRegion())
	}
	if got.GetStatus() != "Active" {
		t.Errorf("Status = %q", got.GetStatus())
	}
	if got.GetLabels() != nil {
		t.Errorf("Labels = %+v, want nil", got.GetLabels())
	}
}

func TestRowToGatewayProto_EmptyLabelsJSON(t *testing.T) {
	in := &gatewayRow{
		ID:     "id-1",
		Labels: []byte(`{}`),
	}
	got := rowToGatewayProto(in)
	if got.GetLabels() != nil {
		t.Errorf("empty labels should map to nil, got %+v", got.GetLabels())
	}
}

func TestRowToGatewayProto_WithLabels(t *testing.T) {
	in := &gatewayRow{
		ID:     "id-1",
		Labels: []byte(`{"env":"prod","region":"us"}`),
	}
	got := rowToGatewayProto(in)
	if got.GetLabels() == nil {
		t.Fatal("expected labels to be set")
	}
	if got.GetLabels().GetItems()["env"] != "prod" {
		t.Errorf("env = %q", got.GetLabels().GetItems()["env"])
	}
}

func TestRowToGatewayProto_InvalidLabelsJSON(t *testing.T) {
	in := &gatewayRow{
		ID:     "id-1",
		Labels: []byte(`{ not json`),
	}
	got := rowToGatewayProto(in)
	if got.GetLabels() != nil {
		t.Errorf("invalid JSON should map to nil labels, got %+v", got.GetLabels())
	}
}

// errInternal is a generic internal error used across tests.
var errInternal = sql.ErrConnDone

// uniqueViolation mimics a Postgres UNIQUE_VIOLATION error.
type uniqueViolation struct{}

func (u *uniqueViolation) Error() string { return "duplicate key value violates unique constraint" }

// Compile-time assurance: the gateway service must continue to satisfy
// the gRPC service interface.
var _ pb.GatewayServiceServer = (*GatewayManagementService)(nil)
