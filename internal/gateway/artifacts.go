package gateway

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	modelstore "github.com/edgeai-platform/ai-edge/internal/model"
	"github.com/edgeai-platform/ai-edge/internal/store"
)

// ArtifactHandlerConfig configures the HTTP artifact handler.
type ArtifactHandlerConfig struct {
	// DB is the main database connection used for model lookups and cache
	// metadata.
	DB *store.DB
	// GatewayID is this gateway-runtime's own identity.
	GatewayID string
	// CacheDir is the local directory where downloaded artifacts are staged.
	CacheDir string
	// UpstreamBaseURL is the object-storage (MinIO) endpoint used for cache
	// miss back-fills. The handler appends the artifact_uri path to this base.
	UpstreamBaseURL string
}

// ArtifactHandler serves model artifacts with HTTP Range support.
type ArtifactHandler struct {
	models       *modelstore.Store
	cache        *CacheStore
	gatewayID    string
	cacheDir     string
	upstreamBase string
}

// NewArtifactHandler creates a handler backed by DB and object storage.
func NewArtifactHandler(cfg ArtifactHandlerConfig) *ArtifactHandler {
	return &ArtifactHandler{
		models:       modelstore.NewStore(cfg.DB),
		cache:        NewCacheStore(cfg.DB),
		gatewayID:    cfg.GatewayID,
		cacheDir:     cfg.CacheDir,
		upstreamBase: strings.TrimRight(cfg.UpstreamBaseURL, "/"),
	}
}

// RegisterRoutes registers the artifact endpoint on the given mux.
// Pattern: /v1/artifacts/models/{name}/{version}
func (h *ArtifactHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/v1/artifacts/models/", h.serveModel)
}

// serveModel handles GET /v1/artifacts/models/{name}/{version}.
func (h *ArtifactHandler) serveModel(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	name, version, ok := parseModelPath(r.URL.Path)
	if !ok {
		http.Error(w, "invalid path; expected /v1/artifacts/models/{name}/{version}", http.StatusBadRequest)
		return
	}

	ctx := r.Context()

	row, err := h.models.GetByNameVersion(ctx, name, version)
	if err != nil {
		http.Error(w, "model not found", http.StatusNotFound)
		return
	}

	localPath := h.localPath(row.Name, row.Version)

	if _, err := os.Stat(localPath); os.IsNotExist(err) {
		if fetchErr := h.fetchFromUpstream(row.ArtifactURI, localPath); fetchErr != nil {
			log.Printf("artifact: fetch upstream %s: %v", row.ArtifactURI, fetchErr)
			http.Error(w, "failed to fetch artifact", http.StatusBadGateway)
			return
		}
	}

	if err := h.cache.Touch(ctx, h.gatewayID, row.ID, row.Version, row.SizeBytes); err != nil {
		log.Printf("artifact: cache touch: %v", err)
	}
	if _, err := h.cache.EvictLRU(ctx, h.gatewayID); err != nil {
		log.Printf("artifact: cache evict: %v", err)
	}

	w.Header().Set("X-Checksum-SHA256", row.Checksum)
	w.Header().Set("X-Model-Name", row.Name)
	w.Header().Set("X-Model-Version", row.Version)
	w.Header().Set("Content-Type", "application/octet-stream")

	f, err := os.Open(localPath)
	if err != nil {
		http.Error(w, "open artifact", http.StatusInternalServerError)
		return
	}
	defer func() {
		if err := f.Close(); err != nil {
			log.Printf("artifact: close local file: %v", err)
		}
	}()

	stat, err := f.Stat()
	if err != nil {
		http.Error(w, "stat artifact", http.StatusInternalServerError)
		return
	}

	http.ServeContent(w, r, row.Name, stat.ModTime(), f)
}

// fetchFromUpstream downloads an artifact from the object store (MinIO)
// into the local cache directory.
func (h *ArtifactHandler) fetchFromUpstream(artifactURI, dest string) error {
	url := h.upstreamBase + "/" + strings.TrimLeft(artifactURI, "/")

	resp, err := http.Get(url)
	if err != nil {
		return fmt.Errorf("GET %s: %w", url, err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			log.Printf("artifact: close upstream response body: %v", err)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: status %d", url, resp.StatusCode)
	}

	if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}

	tmp := dest + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return fmt.Errorf("create tmp: %w", err)
	}

	hasher := sha256.New()
	if _, err := io.Copy(f, io.TeeReader(resp.Body, hasher)); err != nil {
		if closeErr := f.Close(); closeErr != nil {
			log.Printf("artifact: close temp file after copy error: %v", closeErr)
		}
		if removeErr := os.Remove(tmp); removeErr != nil && !os.IsNotExist(removeErr) {
			log.Printf("artifact: remove temp file after copy error: %v", removeErr)
		}
		return fmt.Errorf("download: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close tmp: %w", err)
	}

	if err := os.Rename(tmp, dest); err != nil {
		if removeErr := os.Remove(tmp); removeErr != nil && !os.IsNotExist(removeErr) {
			log.Printf("artifact: remove temp file after rename error: %v", removeErr)
		}
		return fmt.Errorf("rename: %w", err)
	}
	return nil
}

func (h *ArtifactHandler) localPath(name, version string) string {
	safe := strings.ReplaceAll(name, "/", "_")
	return filepath.Join(h.cacheDir, safe, version, "model.bin")
}

// parseModelPath extracts (name, version) from
// /v1/artifacts/models/{name}/{version}
func parseModelPath(path string) (name, version string, ok bool) {
	const prefix = "/v1/artifacts/models/"
	if !strings.HasPrefix(path, prefix) {
		return "", "", false
	}
	rest := strings.TrimPrefix(path, prefix)
	rest = strings.TrimRight(rest, "/")
	parts := strings.SplitN(rest, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	return parts[0], parts[1], true
}

// VerifyChecksum reads a file and checks it against the expected SHA-256 hex
// digest. Returns nil on match.
func VerifyChecksum(path, expected string) error {
	if expected == "" {
		return nil
	}

	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open for checksum: %w", err)
	}
	defer func() {
		if err := f.Close(); err != nil {
			log.Printf("artifact: close checksum file: %v", err)
		}
	}()

	hasher := sha256.New()
	if _, err := io.Copy(hasher, f); err != nil {
		return fmt.Errorf("read for checksum: %w", err)
	}

	got := hex.EncodeToString(hasher.Sum(nil))
	if !strings.EqualFold(got, expected) {
		return fmt.Errorf("checksum mismatch: got %s, want %s", got, expected)
	}
	return nil
}

// FileSizeHeader is the header key carrying the total file size for Range
// support awareness.
const FileSizeHeader = "X-File-Size"

// SetFileSizeHeader adds the total file size header to the response.
func SetFileSizeHeader(w http.ResponseWriter, size int64) {
	w.Header().Set(FileSizeHeader, strconv.FormatInt(size, 10))
}
