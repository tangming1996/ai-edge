package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"os/exec"
	"time"
)

const TaskTypeCollectLogs = "CollectLogs"

// CollectLogsPayload is the JSON payload for a CollectLogs task.
type CollectLogsPayload struct {
	Since     string `json:"since"`
	Until     string `json:"until"`
	Unit      string `json:"unit"`
	UploadURL string `json:"upload_url"`
}

// LogCollector handles CollectLogs tasks by gathering journal logs
// for a time range and uploading them.
type LogCollector struct {
	nodeID         string
	gatewayBaseURL string
}

// NewLogCollector creates a LogCollector.
func NewLogCollector(nodeID, gatewayBaseURL string) *LogCollector {
	return &LogCollector{
		nodeID:         nodeID,
		gatewayBaseURL: gatewayBaseURL,
	}
}

// Execute implements TaskExecutor for CollectLogs tasks.
func (lc *LogCollector) Execute(ctx context.Context, taskType string, payload []byte) ([]byte, error) {
	if taskType != TaskTypeCollectLogs {
		return nil, fmt.Errorf("log_collector: unexpected task type %q", taskType)
	}

	var p CollectLogsPayload
	if err := json.Unmarshal(payload, &p); err != nil {
		return nil, fmt.Errorf("log_collector: parse payload: %w", err)
	}

	if p.Unit == "" {
		p.Unit = "edge-agent"
	}

	logs, err := lc.collectJournalLogs(ctx, p)
	if err != nil {
		return nil, fmt.Errorf("log_collector: collect: %w", err)
	}

	uploadURL := p.UploadURL
	if uploadURL == "" {
		uploadURL = fmt.Sprintf("%s/v1/logs/%s", lc.gatewayBaseURL, lc.nodeID)
	}

	if err := lc.upload(ctx, uploadURL, logs, p); err != nil {
		return nil, fmt.Errorf("log_collector: upload: %w", err)
	}

	result, _ := json.Marshal(map[string]interface{}{
		"size_bytes": len(logs),
		"node_id":    lc.nodeID,
	})
	return result, nil
}

func (lc *LogCollector) collectJournalLogs(ctx context.Context, p CollectLogsPayload) ([]byte, error) {
	args := []string{"-u", p.Unit, "--no-pager", "-o", "short-iso"}

	if p.Since != "" {
		args = append(args, "--since", p.Since)
	}
	if p.Until != "" {
		args = append(args, "--until", p.Until)
	}

	cmd := exec.CommandContext(ctx, "journalctl", args...)
	output, err := cmd.Output()
	if err != nil {
		log.Printf("log_collector: journalctl failed, falling back to dmesg: %v", err)
		return lc.fallbackCollect(ctx, p)
	}
	return output, nil
}

func (lc *LogCollector) fallbackCollect(ctx context.Context, p CollectLogsPayload) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "dmesg", "-T")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("fallback dmesg: %w", err)
	}
	return output, nil
}

func (lc *LogCollector) upload(ctx context.Context, url string, data []byte, p CollectLogsPayload) error {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)

	fw, err := w.CreateFormFile("logs", fmt.Sprintf("logs-%s-%s.txt", lc.nodeID, time.Now().Format("20060102T150405")))
	if err != nil {
		return fmt.Errorf("create form file: %w", err)
	}
	if _, err := fw.Write(data); err != nil {
		return fmt.Errorf("write log data: %w", err)
	}

	_ = w.WriteField("node_id", lc.nodeID)
	_ = w.WriteField("since", p.Since)
	_ = w.WriteField("until", p.Until)

	if err := w.Close(); err != nil {
		return fmt.Errorf("close multipart: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, &buf)
	if err != nil {
		return fmt.Errorf("create upload request: %w", err)
	}
	req.Header.Set("Content-Type", w.FormDataContentType())

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("upload: %w", err)
	}
	defer func() { _, _ = io.Copy(io.Discard, resp.Body); resp.Body.Close() }()

	if resp.StatusCode >= 300 {
		return fmt.Errorf("upload status %d", resp.StatusCode)
	}

	log.Printf("log_collector: uploaded %d bytes to %s", len(data), url)
	return nil
}
