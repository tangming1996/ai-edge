//go:build !integration

package model_test

import (
	"context"
	"testing"
	"time"

	"database/sql/driver"

	pb "github.com/edgeai-platform/ai-edge/api/gen/go/edge/ai/api/v1"
	"github.com/edgeai-platform/ai-edge/internal/model"
	"github.com/edgeai-platform/ai-edge/internal/store"
)

func rowModel(v ...driver.Value) []driver.Value { return v }

func newMemModel(t *testing.T) *model.Service {
	t.Helper()
	store.ResetMemDB()
	db := store.NewMemStore()
	return model.NewService(db)
}

func modelRow(id string) []driver.Value {
	return rowModel(
		id, "model-a", "v1", "ONNX", "sha256", int64(1024),
		"https://example.com/a.onnx", "https://example.com/a.sig",
		[]byte(`{"region":"us"}`), time.Date(2026, 6, 13, 12, 0, 0, 0, time.UTC),
	)
}

func TestService_CreateModel_HappyPath(t *testing.T) {
	svc := newMemModel(t)
	store.SetRowForQuery("INSERT INTO models", rowModel("m-1", time.Date(2026, 6, 13, 12, 0, 0, 0, time.UTC)))
	resp, err := svc.CreateModel(context.Background(), &pb.CreateModelRequest{
		Name: "m", Version: "v1", ArtifactUri: "uri", Format: pb.ModelFormat_MODEL_FORMAT_ONNX,
	})
	if err != nil {
		t.Fatalf("CreateModel: %v", err)
	}
	if resp.GetModel().GetId() != "m-1" {
		t.Errorf("id = %q", resp.GetModel().GetId())
	}
	if resp.GetModel().GetFormat() != pb.ModelFormat_MODEL_FORMAT_ONNX {
		t.Errorf("format = %v", resp.GetModel().GetFormat())
	}
}

func TestService_CreateModel_MissingName(t *testing.T) {
	svc := newMemModel(t)
	_, err := svc.CreateModel(context.Background(), &pb.CreateModelRequest{Version: "v", ArtifactUri: "uri"})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestService_CreateModel_MissingVersion(t *testing.T) {
	svc := newMemModel(t)
	_, err := svc.CreateModel(context.Background(), &pb.CreateModelRequest{Name: "m", ArtifactUri: "uri"})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestService_CreateModel_MissingArtifact(t *testing.T) {
	svc := newMemModel(t)
	_, err := svc.CreateModel(context.Background(), &pb.CreateModelRequest{Name: "m", Version: "v"})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestService_CreateModel_DefaultFormat(t *testing.T) {
	svc := newMemModel(t)
	store.SetRowForQuery("INSERT INTO models", rowModel("m-1", time.Now()))
	resp, err := svc.CreateModel(context.Background(), &pb.CreateModelRequest{
		Name: "m", Version: "v", ArtifactUri: "uri",
		// Format intentionally unspecified
	})
	if err != nil {
		t.Fatalf("CreateModel: %v", err)
	}
	if resp.GetModel().GetFormat() != pb.ModelFormat_MODEL_FORMAT_CUSTOM {
		t.Errorf("format = %v, want CUSTOM", resp.GetModel().GetFormat())
	}
}

func TestService_CreateModel_AlreadyExists(t *testing.T) {
	svc := newMemModel(t)
	store.SetErrorForQuery("INSERT INTO models", store.ErrAlreadyExists)
	_, err := svc.CreateModel(context.Background(), &pb.CreateModelRequest{
		Name: "m", Version: "v", ArtifactUri: "uri",
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestService_CreateModel_InternalError(t *testing.T) {
	svc := newMemModel(t)
	store.SetErrorForQuery("INSERT INTO models", errSentinel("db down"))
	_, err := svc.CreateModel(context.Background(), &pb.CreateModelRequest{
		Name: "m", Version: "v", ArtifactUri: "uri",
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestService_GetModel_ByID_HappyPath(t *testing.T) {
	svc := newMemModel(t)
	store.SetRowForQuery("FROM models WHERE id = $1", modelRow("m-1"))
	resp, err := svc.GetModel(context.Background(), &pb.GetModelRequest{Id: "m-1"})
	if err != nil {
		t.Fatalf("GetModel: %v", err)
	}
	if resp.GetModel().GetId() != "m-1" {
		t.Errorf("id = %q", resp.GetModel().GetId())
	}
}

func TestService_GetModel_ByNameVersion_HappyPath(t *testing.T) {
	svc := newMemModel(t)
	store.SetRowForQuery("FROM models WHERE name = $1 AND version = $2", modelRow("m-1"))
	resp, err := svc.GetModel(context.Background(), &pb.GetModelRequest{Name: "m", Version: "v"})
	if err != nil {
		t.Fatalf("GetModel: %v", err)
	}
	if resp.GetModel().GetName() != "model-a" {
		t.Errorf("name = %q", resp.GetModel().GetName())
	}
}

func TestService_GetModel_MissingArgs(t *testing.T) {
	svc := newMemModel(t)
	_, err := svc.GetModel(context.Background(), &pb.GetModelRequest{})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestService_GetModel_NotFound(t *testing.T) {
	svc := newMemModel(t)
	store.SetNoRowsForQuery("FROM models WHERE id = $1")
	_, err := svc.GetModel(context.Background(), &pb.GetModelRequest{Id: "missing"})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestService_ListModels_Empty(t *testing.T) {
	svc := newMemModel(t)
	// COUNT(*) returns int64(0); the data query is unmatched and
	// returns io.EOF (no rows).
	resp, err := svc.ListModels(context.Background(), &pb.ListModelsRequest{})
	if err != nil {
		t.Fatalf("ListModels: %v", err)
	}
	if len(resp.GetModels()) != 0 {
		t.Errorf("expected 0, got %d", len(resp.GetModels()))
	}
	if resp.GetPage().GetTotalCount() != 0 {
		t.Errorf("total = %d", resp.GetPage().GetTotalCount())
	}
}

func TestService_ListModels_WithRows(t *testing.T) {
	svc := newMemModel(t)
	store.SetRowForQuery("SELECT COUNT(*) FROM models", rowModel(int64(2)))
	store.SetRowForQuery("ORDER BY created_at DESC", modelRow("m-1"))
	resp, err := svc.ListModels(context.Background(), &pb.ListModelsRequest{})
	if err != nil {
		t.Fatalf("ListModels: %v", err)
	}
	if len(resp.GetModels()) != 1 {
		t.Errorf("expected 1, got %d", len(resp.GetModels()))
	}
}

func TestService_ListModels_WithFormatFilter(t *testing.T) {
	svc := newMemModel(t)
	// Format filter adds an AND clause; the COUNT query substring
	// "SELECT COUNT(*) FROM models" must still match.
	store.SetRowForQuery("SELECT COUNT(*) FROM models", rowModel(int64(0)))
	resp, err := svc.ListModels(context.Background(), &pb.ListModelsRequest{
		Format: pb.ModelFormat_MODEL_FORMAT_GGUF,
	})
	if err != nil {
		t.Fatalf("ListModels: %v", err)
	}
	if resp.GetPage().GetTotalCount() != 0 {
		t.Errorf("total = %d", resp.GetPage().GetTotalCount())
	}
}

func TestService_ListModels_PageConfig(t *testing.T) {
	svc := newMemModel(t)
	resp, err := svc.ListModels(context.Background(), &pb.ListModelsRequest{
		Page: &pb.PageRequest{PageSize: 50, PageToken: "10"},
	})
	if err != nil {
		t.Fatalf("ListModels: %v", err)
	}
	if resp.GetPage() == nil {
		t.Fatal("nil page")
	}
}

func TestService_ListModels_CountError(t *testing.T) {
	svc := newMemModel(t)
	store.SetErrorForQuery("SELECT COUNT(*) FROM models", errSentinel("count failed"))
	_, err := svc.ListModels(context.Background(), &pb.ListModelsRequest{})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestService_ListModels_QueryError(t *testing.T) {
	svc := newMemModel(t)
	// Force count to succeed with one row, then make the data query fail.
	store.SetRowForQuery("SELECT COUNT(*) FROM models", rowModel(int64(1)))
	store.SetErrorForQuery("ORDER BY created_at DESC", errSentinel("scan failed"))
	_, err := svc.ListModels(context.Background(), &pb.ListModelsRequest{})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestService_ListModels_Labels(t *testing.T) {
	svc := newMemModel(t)
	store.SetRowForQuery("FROM models WHERE id = $1", modelRow("m-1"))
	resp, err := svc.GetModel(context.Background(), &pb.GetModelRequest{Id: "m-1"})
	if err != nil {
		t.Fatalf("GetModel: %v", err)
	}
	items := resp.GetModel().GetLabels().GetItems()
	if items["region"] != "us" {
		t.Errorf("labels = %+v", items)
	}
}

// errSentinel is a typed error used in store.ErrAlreadyExists matching
// tests. The model store converts unique-violation errors to
// ErrAlreadyExists, but only if it knows about the sentinel. To stay
// independent of internal helper wiring, we exercise the conversion
// path by setting the sentinel directly via the mem driver.
type errSentinel string

func (e errSentinel) Error() string { return string(e) }
