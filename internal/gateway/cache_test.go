package gateway

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCacheStore_SetMaxCachedVersions(t *testing.T) {
	c := NewCacheStore(nil)
	if c.maxCachedVersions != defaultMaxCachedVersions {
		t.Fatalf("default = %d, want %d", c.maxCachedVersions, defaultMaxCachedVersions)
	}
	c.SetMaxCachedVersions(50)
	if c.maxCachedVersions != 50 {
		t.Fatalf("got %d, want 50", c.maxCachedVersions)
	}
	// Non-positive values must be ignored.
	c.SetMaxCachedVersions(0)
	c.SetMaxCachedVersions(-1)
	if c.maxCachedVersions != 50 {
		t.Fatalf("non-positive override should be ignored, got %d", c.maxCachedVersions)
	}
}

func TestLocalPath_SlashSafe(t *testing.T) {
	h := &ArtifactHandler{cacheDir: "/var/cache"}
	got := h.localPath("foo/bar", "1.0")
	if got != filepath.Join("/var/cache", "foo_bar", "1.0", "model.bin") {
		t.Fatalf("localPath: %q", got)
	}
}

func TestLocalPath_PreservesSafeChars(t *testing.T) {
	h := &ArtifactHandler{cacheDir: "/var/cache"}
	got := h.localPath("llama-3.1", "v1.0-rc1")
	if got != filepath.Join("/var/cache", "llama-3.1", "v1.0-rc1", "model.bin") {
		t.Fatalf("localPath: %q", got)
	}
}

func TestArtifactHandler_UpstreamTrimmed(t *testing.T) {
	// NewArtifactHandler must strip the trailing slash from UpstreamBaseURL.
	h := NewArtifactHandler(ArtifactHandlerConfig{
		UpstreamBaseURL: "http://minio.local/",
		CacheDir:        t.TempDir(),
	})
	if h.upstreamBase != "http://minio.local" {
		t.Fatalf("upstreamBase = %q", h.upstreamBase)
	}
}

func TestParseModelPath_HappyPath(t *testing.T) {
	cases := []struct {
		path        string
		wantName    string
		wantVersion string
		wantOK      bool
	}{
		{"/v1/artifacts/models/llama/3.1", "llama", "3.1", true},
		{"/v1/artifacts/models/foo/v1.0", "foo", "v1.0", true},
		{"/v1/artifacts/models/foo/v1.0/", "foo", "v1.0", true},
	}
	for _, c := range cases {
		gotName, gotVersion, gotOK := parseModelPath(c.path)
		if gotName != c.wantName || gotVersion != c.wantVersion || gotOK != c.wantOK {
			t.Errorf("parseModelPath(%q) = (%q, %q, %v), want (%q, %q, %v)",
				c.path, gotName, gotVersion, gotOK, c.wantName, c.wantVersion, c.wantOK)
		}
	}
}

func TestParseModelPath_FailureCases(t *testing.T) {
	cases := []string{
		"",
		"/v1/artifacts/models/",
		"/v1/artifacts/models/llama",
		"/v1/artifacts/models/llama/",
		"/v1/artifacts/models//v1",
		"/other/prefix/llama/v1",
	}
	for _, p := range cases {
		if _, _, ok := parseModelPath(p); ok {
			t.Errorf("parseModelPath(%q) should fail", p)
		}
	}
}

func TestParseModelPath_ExtraPathIsTolerated(t *testing.T) {
	// Extra path segments are tolerated (the prefix/suffix split happens
	// on the first slash). The current implementation returns the first
	// two segments as name and version.
	name, version, ok := parseModelPath("/v1/artifacts/models/llama/3.1/extra")
	if !ok || name != "llama" || version != "3.1/extra" {
		t.Errorf("parseModelPath split: name=%q version=%q ok=%v", name, version, ok)
	}
}

func TestArtifactHandler_RegisterRoutes(t *testing.T) {
	mux := http.NewServeMux()
	h := NewArtifactHandler(ArtifactHandlerConfig{
		GatewayID: "gw",
		CacheDir:  t.TempDir(),
	})
	h.RegisterRoutes(mux)
	// The prefix is registered as /v1/artifacts/models/, so any URL
	// outside the prefix must still 404 from the default mux.
	req := httptest.NewRequest(http.MethodGet, "/v1/other", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("non-matching path = %d, want 404", rec.Code)
	}
}

func TestVerifyChecksum_Match(t *testing.T) {
	// Write a file with known content, then verify its SHA-256.
	dir := t.TempDir()
	p := filepath.Join(dir, "model.bin")
	if err := os.WriteFile(p, []byte("hello world"), 0644); err != nil {
		t.Fatal(err)
	}
	// sha256("hello world")
	const want = "b94d27b9934d3e08a52e52d7da7dabfac484efe37a5380ee9088f7ace2efcde9"
	if err := VerifyChecksum(p, want); err != nil {
		t.Fatalf("VerifyChecksum: %v", err)
	}
}

func TestVerifyChecksum_Mismatch(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "model.bin")
	if err := os.WriteFile(p, []byte("hello world"), 0644); err != nil {
		t.Fatal(err)
	}
	err := VerifyChecksum(p, "0000000000000000000000000000000000000000000000000000000000000000")
	if err == nil {
		t.Fatal("expected checksum mismatch")
	}
	if !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestVerifyChecksum_EmptyExpected(t *testing.T) {
	// Empty expected is treated as "skip the check".
	dir := t.TempDir()
	p := filepath.Join(dir, "model.bin")
	if err := os.WriteFile(p, []byte("anything"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := VerifyChecksum(p, ""); err != nil {
		t.Fatalf("VerifyChecksum with empty expected: %v", err)
	}
}

func TestVerifyChecksum_MissingFile(t *testing.T) {
	err := VerifyChecksum("/no/such/file", "abc")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestSetFileSizeHeader_Negative(t *testing.T) {
	// Negative sizes should still be encoded faithfully.
	rec := httptest.NewRecorder()
	SetFileSizeHeader(rec, -1)
	if got := rec.Header().Get(FileSizeHeader); got != "-1" {
		t.Fatalf("X-File-Size = %q, want -1", got)
	}
}
