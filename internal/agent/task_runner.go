package agent

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	pb "github.com/edgeai-platform/ai-edge/api/gen/go/edge/ai/api/v1"
	"google.golang.org/grpc"
)

// TaskExecutor is the interface that concrete task handlers must implement.
// Each task type can have its own executor registered with the runner.
type TaskExecutor interface {
	Execute(ctx context.Context, taskType string, payload []byte) (result []byte, err error)
}

// TaskRunnerConfig holds configuration for the task runner loop.
type TaskRunnerConfig struct {
	PollInterval time.Duration
	MaxTasks     int32
	DataDir      string
}

func (c *TaskRunnerConfig) applyDefaults() {
	if c.PollInterval == 0 {
		c.PollInterval = 5 * time.Second
	}
	if c.MaxTasks == 0 {
		c.MaxTasks = 10
	}
}

// pendingResult is the on-disk representation of a task result that
// has not yet been reported to the gateway (crash recovery).
type pendingResult struct {
	TaskID       string `json:"task_id"`
	NodeID       string `json:"node_id"`
	Status       string `json:"status"`
	ErrorMessage string `json:"error_message,omitempty"`
	Result       []byte `json:"result,omitempty"`
}

// TaskRunner polls the gateway for tasks, deduplicates by task ID,
// executes them through a TaskExecutor, and reports results back.
type TaskRunner struct {
	nodeID   string
	cfg      TaskRunnerConfig
	client   pb.AgentServiceClient
	executor TaskExecutor

	mu        sync.Mutex
	processed map[string]struct{}
}

// NewTaskRunner creates a TaskRunner.
func NewTaskRunner(
	conn *grpc.ClientConn,
	nodeID string,
	executor TaskExecutor,
	cfg TaskRunnerConfig,
) *TaskRunner {
	cfg.applyDefaults()
	return &TaskRunner{
		nodeID:    nodeID,
		cfg:       cfg,
		client:    pb.NewAgentServiceClient(conn),
		executor:  executor,
		processed: make(map[string]struct{}),
	}
}

// Run starts the poll loop. It first drains any pending results from a
// previous crash, then enters the poll-execute-report cycle until ctx
// is cancelled.
func (r *TaskRunner) Run(ctx context.Context) {
	r.drainPendingResults(ctx)

	ticker := time.NewTicker(r.cfg.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Println("agent: task runner stopped")
			return
		case <-ticker.C:
			r.poll(ctx)
		}
	}
}

func (r *TaskRunner) poll(ctx context.Context) {
	pullCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	resp, err := r.client.PullTasks(pullCtx, &pb.PullTasksRequest{
		NodeId:   r.nodeID,
		MaxTasks: r.cfg.MaxTasks,
	})
	if err != nil {
		log.Println("agent: PullTasks failed:", err)
		return
	}

	for _, t := range resp.GetTasks() {
		if r.isDuplicate(t.GetTaskId()) {
			continue
		}
		r.markProcessed(t.GetTaskId())
		go r.executeAndReport(ctx, t)
	}
}

func (r *TaskRunner) isDuplicate(taskID string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, ok := r.processed[taskID]
	return ok
}

func (r *TaskRunner) markProcessed(taskID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.processed[taskID] = struct{}{}
}

func (r *TaskRunner) executeAndReport(ctx context.Context, t *pb.NodeTask) {
	timeout := time.Duration(t.GetTimeoutSeconds()) * time.Second
	if timeout == 0 {
		timeout = 10 * time.Minute
	}

	execCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	resultBytes, execErr := r.executor.Execute(execCtx, t.GetType(), t.GetPayload())

	pr := pendingResult{
		TaskID: t.GetTaskId(),
		NodeID: r.nodeID,
		Status: "Success",
		Result: resultBytes,
	}
	if execErr != nil {
		pr.Status = "Failed"
		pr.ErrorMessage = execErr.Error()
	}

	r.savePendingResult(pr)

	if err := r.reportResult(ctx, pr); err != nil {
		log.Printf("agent: ReportTaskResult failed for task %s (will retry on restart): %v",
			t.GetTaskId(), err)
		return
	}

	r.removePendingResult(pr.TaskID)
}

func (r *TaskRunner) reportResult(ctx context.Context, pr pendingResult) error {
	reportCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	_, err := r.client.ReportTaskResult(reportCtx, &pb.ReportTaskResultRequest{
		TaskId:       pr.TaskID,
		NodeId:       pr.NodeID,
		Status:       pr.Status,
		ErrorMessage: pr.ErrorMessage,
		Result:       pr.Result,
	})
	return err
}

// --- pending result persistence for crash recovery ---

func (r *TaskRunner) pendingDir() string {
	return filepath.Join(r.cfg.DataDir, "pending-results")
}

func (r *TaskRunner) pendingFilePath(taskID string) string {
	return filepath.Join(r.pendingDir(), taskID+".json")
}

func (r *TaskRunner) savePendingResult(pr pendingResult) {
	dir := r.pendingDir()
	if err := os.MkdirAll(dir, 0700); err != nil {
		log.Printf("agent: mkdir pending-results: %v", err)
		return
	}

	data, err := json.Marshal(pr)
	if err != nil {
		log.Printf("agent: marshal pending result: %v", err)
		return
	}

	if err := os.WriteFile(r.pendingFilePath(pr.TaskID), data, 0600); err != nil {
		log.Printf("agent: write pending result: %v", err)
	}
}

func (r *TaskRunner) removePendingResult(taskID string) {
	_ = os.Remove(r.pendingFilePath(taskID))
}

// drainPendingResults replays any results that were saved to disk but
// not successfully reported before the previous process exited.
func (r *TaskRunner) drainPendingResults(ctx context.Context) {
	dir := r.pendingDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}

	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}

		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			log.Printf("agent: read pending result %s: %v", entry.Name(), err)
			continue
		}

		var pr pendingResult
		if err := json.Unmarshal(data, &pr); err != nil {
			log.Printf("agent: unmarshal pending result %s: %v", entry.Name(), err)
			continue
		}

		r.markProcessed(pr.TaskID)

		if err := r.reportResult(ctx, pr); err != nil {
			log.Printf("agent: drain pending result %s failed: %v", pr.TaskID, err)
			continue
		}

		r.removePendingResult(pr.TaskID)
		log.Printf("agent: drained pending result for task %s", pr.TaskID)
	}
}
