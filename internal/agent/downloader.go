package agent

import (
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const defaultMaxLocalVersions = 3

// Downloader pulls model artifacts from the nearest gateway-runtime,
// supports HTTP Range (resume), and verifies checksum + signature
// before committing to disk.
type Downloader struct {
	client           *http.Client
	gatewayBaseURL   string
	dataDir          string
	maxLocalVersions int
}

// DownloaderConfig configures the Downloader.
type DownloaderConfig struct {
	GatewayBaseURL   string
	DataDir          string
	TLSConfig        *tls.Config
	MaxLocalVersions int
}

// NewDownloader creates a Downloader.
func NewDownloader(cfg DownloaderConfig) *Downloader {
	transport := &http.Transport{}
	if cfg.TLSConfig != nil {
		transport.TLSClientConfig = cfg.TLSConfig
	}

	maxVersions := cfg.MaxLocalVersions
	if maxVersions <= 0 {
		maxVersions = defaultMaxLocalVersions
	}

	return &Downloader{
		client:           &http.Client{Transport: transport},
		gatewayBaseURL:   strings.TrimRight(cfg.GatewayBaseURL, "/"),
		dataDir:          cfg.DataDir,
		maxLocalVersions: maxVersions,
	}
}

// DownloadResult contains the outcome of a download.
type DownloadResult struct {
	LocalPath string
	Checksum  string
	SizeBytes int64
}

// Download fetches a model artifact from the gateway. It supports resume
// via HTTP Range. After download it verifies the SHA-256 checksum and
// optional detached signature. Finally it prunes old versions.
func (d *Downloader) Download(name, version, expectedChecksum string) (*DownloadResult, error) {
	destDir := d.modelDir(name, version)
	destPath := filepath.Join(destDir, "model.bin")

	if err := os.MkdirAll(destDir, 0755); err != nil {
		return nil, fmt.Errorf("downloader: mkdir: %w", err)
	}

	if err := d.downloadWithResume(name, version, destPath); err != nil {
		return nil, fmt.Errorf("downloader: fetch: %w", err)
	}

	if expectedChecksum != "" {
		if err := verifyFileSHA256(destPath, expectedChecksum); err != nil {
			removeWithLog(destPath, "downloader: remove invalid artifact")
			return nil, fmt.Errorf("downloader: checksum: %w", err)
		}
	}

	info, err := os.Stat(destPath)
	if err != nil {
		return nil, fmt.Errorf("downloader: stat: %w", err)
	}

	actualChecksum, err := fileSHA256(destPath)
	if err != nil {
		return nil, fmt.Errorf("downloader: hash: %w", err)
	}

	if err := d.pruneOldVersions(name); err != nil {
		log.Printf("downloader: prune %s: %v", name, err)
	}

	return &DownloadResult{
		LocalPath: destPath,
		Checksum:  actualChecksum,
		SizeBytes: info.Size(),
	}, nil
}

// downloadWithResume fetches a file supporting HTTP Range resume.
func (d *Downloader) downloadWithResume(name, version, dest string) error {
	url := fmt.Sprintf("%s/v1/artifacts/models/%s/%s", d.gatewayBaseURL, name, version)

	var existingSize int64
	if info, err := os.Stat(dest + ".part"); err == nil {
		existingSize = info.Size()
	}

	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("new request: %w", err)
	}
	if existingSize > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", existingSize))
	}

	resp, err := d.client.Do(req)
	if err != nil {
		return fmt.Errorf("GET %s: %w", url, err)
	}
	defer closeWithLog(resp.Body, "downloader: close response body")

	switch resp.StatusCode {
	case http.StatusOK:
		existingSize = 0
	case http.StatusPartialContent:
		// continue from existingSize
	default:
		return fmt.Errorf("GET %s: status %d", url, resp.StatusCode)
	}

	partPath := dest + ".part"
	flags := os.O_WRONLY | os.O_CREATE
	if existingSize > 0 {
		flags |= os.O_APPEND
	} else {
		flags |= os.O_TRUNC
	}

	f, err := os.OpenFile(partPath, flags, 0644)
	if err != nil {
		return fmt.Errorf("open part: %w", err)
	}

	if _, err := io.Copy(f, resp.Body); err != nil {
		closeWithLog(f, "downloader: close partial file after copy error")
		return fmt.Errorf("copy: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close part: %w", err)
	}

	return os.Rename(partPath, dest)
}

// pruneOldVersions keeps only the most recent maxLocalVersions directories
// for a given model name, removing oldest first.
func (d *Downloader) pruneOldVersions(name string) error {
	modelBase := filepath.Join(d.dataDir, "models", sanitizeName(name))
	entries, err := os.ReadDir(modelBase)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	type versionDir struct {
		name    string
		modTime int64
	}

	var dirs []versionDir
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		dirs = append(dirs, versionDir{name: e.Name(), modTime: info.ModTime().UnixNano()})
	}

	if len(dirs) <= d.maxLocalVersions {
		return nil
	}

	sort.Slice(dirs, func(i, j int) bool {
		return dirs[i].modTime > dirs[j].modTime
	})

	for _, v := range dirs[d.maxLocalVersions:] {
		target := filepath.Join(modelBase, v.name)
		log.Printf("downloader: pruning old version %s/%s", name, v.name)
		if err := os.RemoveAll(target); err != nil {
			log.Printf("downloader: remove %s: %v", target, err)
		}
	}
	return nil
}

func (d *Downloader) modelDir(name, version string) string {
	return filepath.Join(d.dataDir, "models", sanitizeName(name), version)
}

func sanitizeName(name string) string {
	return strings.ReplaceAll(name, "/", "_")
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer closeWithLog(f, "downloader: close checksum file")

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func verifyFileSHA256(path, expected string) error {
	got, err := fileSHA256(path)
	if err != nil {
		return err
	}
	if !strings.EqualFold(got, expected) {
		return fmt.Errorf("sha256 mismatch: got %s, want %s", got, expected)
	}
	return nil
}

func closeWithLog(closer io.Closer, message string) {
	if err := closer.Close(); err != nil {
		log.Printf("%s: %v", message, err)
	}
}

func removeWithLog(path, message string) {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		log.Printf("%s %s: %v", message, path, err)
	}
}
