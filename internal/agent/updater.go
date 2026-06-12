package agent

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strings"

	"golang.org/x/mod/semver"
)

const TaskTypeUpgradeAgent = "UpgradeAgent"

// ReleaseManifest describes an agent release that the updater validates
// before applying.
type ReleaseManifest struct {
	Version           string `json:"version"`
	SHA256            string `json:"sha256"`
	Signature         string `json:"signature"`
	MinAllowedVersion string `json:"min_allowed_version"`
	DownloadURL       string `json:"download_url"`
}

// Updater handles UpgradeAgent tasks: download, verify, replace, restart.
type Updater struct {
	currentVersion string
	binaryPath     string
	dataDir        string
	verifyKey      ed25519.PublicKey
	downloader     *Downloader
}

// UpdaterConfig configures the Updater.
type UpdaterConfig struct {
	CurrentVersion string
	BinaryPath     string
	DataDir        string
	VerifyKey      ed25519.PublicKey
	Downloader     *Downloader
}

// NewUpdater creates an Updater.
func NewUpdater(cfg UpdaterConfig) *Updater {
	return &Updater{
		currentVersion: cfg.CurrentVersion,
		binaryPath:     cfg.BinaryPath,
		dataDir:        cfg.DataDir,
		verifyKey:      cfg.VerifyKey,
		downloader:     cfg.Downloader,
	}
}

// Execute implements TaskExecutor for UpgradeAgent tasks.
func (u *Updater) Execute(ctx context.Context, taskType string, payload []byte) ([]byte, error) {
	if taskType != TaskTypeUpgradeAgent {
		return nil, fmt.Errorf("updater: unexpected task type %q", taskType)
	}

	var manifest ReleaseManifest
	if err := json.Unmarshal(payload, &manifest); err != nil {
		return nil, fmt.Errorf("updater: parse manifest: %w", err)
	}

	if err := u.validateManifest(&manifest); err != nil {
		return nil, err
	}

	tmpPath := u.binaryPath + ".new"
	if err := u.downloadBinary(ctx, manifest.DownloadURL, tmpPath); err != nil {
		return nil, err
	}

	if err := u.verifyChecksum(tmpPath, manifest.SHA256); err != nil {
		os.Remove(tmpPath)
		return nil, err
	}

	if err := u.verifySignature(manifest.SHA256, manifest.Signature); err != nil {
		os.Remove(tmpPath)
		return nil, err
	}

	if err := u.replaceBinary(tmpPath); err != nil {
		return nil, err
	}

	log.Printf("updater: upgrade %s -> %s complete, restarting...", u.currentVersion, manifest.Version)
	u.restart()

	result, _ := json.Marshal(map[string]string{
		"old_version": u.currentVersion,
		"new_version": manifest.Version,
	})
	return result, nil
}

func (u *Updater) validateManifest(m *ReleaseManifest) error {
	if m.Version == "" || m.SHA256 == "" || m.Signature == "" {
		return fmt.Errorf("updater: incomplete manifest")
	}
	if m.DownloadURL == "" {
		return fmt.Errorf("updater: download_url is required")
	}

	newVer := ensureVPrefix(m.Version)
	minVer := ensureVPrefix(m.MinAllowedVersion)

	if m.MinAllowedVersion != "" && semver.IsValid(newVer) && semver.IsValid(minVer) {
		if semver.Compare(newVer, minVer) < 0 {
			return fmt.Errorf("updater: version %s is below min_allowed_version %s (rollback rejected)",
				m.Version, m.MinAllowedVersion)
		}
	}

	return nil
}

func (u *Updater) downloadBinary(ctx context.Context, url, dest string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("updater: create request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("updater: download: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("updater: download status %d", resp.StatusCode)
	}

	f, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
	if err != nil {
		return fmt.Errorf("updater: create file: %w", err)
	}

	if _, err := io.Copy(f, resp.Body); err != nil {
		f.Close()
		os.Remove(dest)
		return fmt.Errorf("updater: write: %w", err)
	}
	return f.Close()
}

func (u *Updater) verifyChecksum(path, expected string) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("updater: open for checksum: %w", err)
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return fmt.Errorf("updater: read for checksum: %w", err)
	}

	got := hex.EncodeToString(h.Sum(nil))
	if !strings.EqualFold(got, expected) {
		return fmt.Errorf("updater: sha256 mismatch: got %s, want %s", got, expected)
	}
	return nil
}

func (u *Updater) verifySignature(sha256Hex, signatureHex string) error {
	if len(u.verifyKey) == 0 {
		log.Println("updater: no ed25519 public key configured, skipping signature verification")
		return nil
	}

	sig, err := hex.DecodeString(signatureHex)
	if err != nil {
		return fmt.Errorf("updater: decode signature: %w", err)
	}

	message := []byte(sha256Hex)
	if !ed25519.Verify(u.verifyKey, message, sig) {
		return fmt.Errorf("updater: ed25519 signature verification failed")
	}
	return nil
}

func (u *Updater) replaceBinary(tmpPath string) error {
	backupPath := u.binaryPath + ".bak"
	if err := os.Rename(u.binaryPath, backupPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("updater: backup old binary: %w", err)
	}

	if err := os.Rename(tmpPath, u.binaryPath); err != nil {
		// Attempt to restore backup
		_ = os.Rename(backupPath, u.binaryPath)
		return fmt.Errorf("updater: replace binary: %w", err)
	}
	return nil
}

func (u *Updater) restart() {
	cmd := exec.Command("systemctl", "restart", "edge-agent")
	if err := cmd.Start(); err != nil {
		log.Printf("updater: systemctl restart failed: %v", err)
	}
}

func ensureVPrefix(v string) string {
	if v == "" {
		return ""
	}
	if !strings.HasPrefix(v, "v") {
		return "v" + v
	}
	return v
}
