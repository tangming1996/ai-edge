package model

import (
	"context"
	"encoding/json"
	"strconv"

	pb "github.com/edgeai-platform/ai-edge/api/gen/go/edge/ai/api/v1"
	"github.com/edgeai-platform/ai-edge/internal/store"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// formatToText maps proto enum to DB text.
var formatToText = map[pb.ModelFormat]string{
	pb.ModelFormat_MODEL_FORMAT_ONNX:        "ONNX",
	pb.ModelFormat_MODEL_FORMAT_TFLITE:      "TFLITE",
	pb.ModelFormat_MODEL_FORMAT_GGUF:        "GGUF",
	pb.ModelFormat_MODEL_FORMAT_SAFETENSORS: "SAFETENSORS",
	pb.ModelFormat_MODEL_FORMAT_CUSTOM:      "CUSTOM",
}

// textToFormat maps DB text to proto enum.
var textToFormat = map[string]pb.ModelFormat{
	"ONNX":        pb.ModelFormat_MODEL_FORMAT_ONNX,
	"TFLITE":      pb.ModelFormat_MODEL_FORMAT_TFLITE,
	"GGUF":        pb.ModelFormat_MODEL_FORMAT_GGUF,
	"SAFETENSORS": pb.ModelFormat_MODEL_FORMAT_SAFETENSORS,
	"CUSTOM":      pb.ModelFormat_MODEL_FORMAT_CUSTOM,
}

// Service implements pb.ModelServiceServer.
type Service struct {
	pb.UnimplementedModelServiceServer

	store *Store
}

// NewService creates a Model gRPC service.
func NewService(db *store.DB) *Service {
	return &Service{
		store: NewStore(db),
	}
}

// CreateModel registers a new model version.
func (svc *Service) CreateModel(
	ctx context.Context,
	req *pb.CreateModelRequest,
) (*pb.CreateModelResponse, error) {
	if req.GetName() == "" {
		return nil, status.Error(codes.InvalidArgument, "name is required")
	}
	if req.GetVersion() == "" {
		return nil, status.Error(codes.InvalidArgument, "version is required")
	}
	if req.GetArtifactUri() == "" {
		return nil, status.Error(codes.InvalidArgument, "artifact_uri is required")
	}

	format := "CUSTOM"
	if f, ok := formatToText[req.GetFormat()]; ok && req.GetFormat() != pb.ModelFormat_MODEL_FORMAT_UNSPECIFIED {
		format = f
	}

	var labels json.RawMessage
	if req.GetLabels() != nil && len(req.GetLabels().GetItems()) > 0 {
		var err error
		labels, err = json.Marshal(req.GetLabels().GetItems())
		if err != nil {
			return nil, status.Errorf(codes.InvalidArgument, "invalid labels: %v", err)
		}
	}

	row := &ModelRow{
		Name:         req.GetName(),
		Version:      req.GetVersion(),
		Format:       format,
		Checksum:     req.GetChecksum(),
		SizeBytes:    req.GetSize(),
		ArtifactURI:  req.GetArtifactUri(),
		SignatureURI: req.GetSignatureUri(),
		Labels:       labels,
	}

	if err := svc.store.Create(ctx, row); err != nil {
		if store.IsUniqueViolation(err) {
			return nil, status.Errorf(codes.AlreadyExists, "model %s:%s already exists", req.GetName(), req.GetVersion())
		}
		return nil, status.Errorf(codes.Internal, "create model: %v", err)
	}

	return &pb.CreateModelResponse{Model: rowToProto(row)}, nil
}

// GetModel retrieves a model by ID or by (name, version).
func (svc *Service) GetModel(
	ctx context.Context,
	req *pb.GetModelRequest,
) (*pb.GetModelResponse, error) {
	var row *ModelRow
	var err error

	switch {
	case req.GetId() != "":
		row, err = svc.store.GetByID(ctx, req.GetId())
	case req.GetName() != "" && req.GetVersion() != "":
		row, err = svc.store.GetByNameVersion(ctx, req.GetName(), req.GetVersion())
	default:
		return nil, status.Error(codes.InvalidArgument, "id or (name + version) is required")
	}

	if err != nil {
		return nil, errToStatus(err)
	}
	return &pb.GetModelResponse{Model: rowToProto(row)}, nil
}

// ListModels returns a paginated list of models.
func (svc *Service) ListModels(
	ctx context.Context,
	req *pb.ListModelsRequest,
) (*pb.ListModelsResponse, error) {
	pageSize := int32(20)
	offset := 0
	if p := req.GetPage(); p != nil {
		if p.PageSize > 0 {
			pageSize = p.PageSize
		}
		if p.PageToken != "" {
			if v, err := strconv.Atoi(p.PageToken); err == nil {
				offset = v
			}
		}
	}

	var formatFilter string
	if req.GetFormat() != pb.ModelFormat_MODEL_FORMAT_UNSPECIFIED {
		if f, ok := formatToText[req.GetFormat()]; ok {
			formatFilter = f
		}
	}

	rows, total, err := svc.store.List(ctx, ListFilter{
		Name:   req.GetName(),
		Format: formatFilter,
		Limit:  int(pageSize),
		Offset: offset,
	})
	if err != nil {
		return nil, status.Errorf(codes.Internal, "list models: %v", err)
	}

	models := make([]*pb.Model, 0, len(rows))
	for _, r := range rows {
		models = append(models, rowToProto(r))
	}

	nextToken := ""
	nextOffset := offset + int(pageSize)
	if nextOffset < total {
		nextToken = strconv.Itoa(nextOffset)
	}

	return &pb.ListModelsResponse{
		Models: models,
		Page: &pb.PageResponse{
			NextPageToken: nextToken,
			TotalCount:    int32(total),
		},
	}, nil
}

func rowToProto(r *ModelRow) *pb.Model {
	m := &pb.Model{
		Id:           r.ID,
		Name:         r.Name,
		Version:      r.Version,
		Format:       textToFormat[r.Format],
		Checksum:     r.Checksum,
		Size:         r.SizeBytes,
		ArtifactUri:  r.ArtifactURI,
		SignatureUri: r.SignatureURI,
		CreatedAt:    timestamppb.New(r.CreatedAt),
	}

	if len(r.Labels) > 0 {
		var items map[string]string
		if json.Unmarshal(r.Labels, &items) == nil && len(items) > 0 {
			m.Labels = &pb.Labels{Items: items}
		}
	}
	return m
}

func errToStatus(err error) error {
	switch err {
	case store.ErrNotFound:
		return status.Error(codes.NotFound, "model not found")
	case store.ErrAlreadyExists, store.ErrConflict:
		return status.Error(codes.AlreadyExists, err.Error())
	}
	return status.Errorf(codes.Internal, "%v", err)
}
