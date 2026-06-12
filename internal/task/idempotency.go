package task

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/edgeai-platform/ai-edge/internal/store"
)

// IdempotencyChecker handles deduplication for task results.
type IdempotencyChecker struct {
	db *store.DB
}

// NewIdempotencyChecker creates an IdempotencyChecker.
func NewIdempotencyChecker(db *store.DB) *IdempotencyChecker {
	return &IdempotencyChecker{db: db}
}

// TaskResultKey uniquely identifies a task result report.
func TaskResultKey(taskID, nodeID string) string {
	return taskID + ":" + nodeID
}

// CheckAndSetTaskRun returns an existing completed task_run for the given
// (task_id, node_id) pair if one exists (idempotent replay). Otherwise it
// returns nil, indicating the caller should proceed with creating a new run.
func (c *IdempotencyChecker) CheckAndSetTaskRun(
	ctx context.Context,
	tx *store.Tx,
	taskID, nodeID string,
) (*TaskRunRow, error) {
	const q = `
		SELECT id, task_id, node_id, attempt, status,
		       started_at, finished_at, error_msg, result, created_at
		FROM task_runs
		WHERE task_id = $1 AND node_id = $2 AND status IN ('Success', 'Failed')
		ORDER BY attempt DESC
		LIMIT 1`

	var r TaskRunRow
	var finishedAt sql.NullTime
	var errMsg sql.NullString
	var result sql.NullString

	err := tx.QueryRowContext(ctx, q, taskID, nodeID).Scan(
		&r.ID, &r.TaskID, &r.NodeID, &r.Attempt, &r.Status,
		&r.StartedAt, &finishedAt, &errMsg, &result, &r.CreatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("idempotency check: %w", err)
	}

	if finishedAt.Valid {
		r.FinishedAt = &finishedAt.Time
	}
	r.ErrorMsg = errMsg.String
	r.Result = result.String

	return &r, nil
}

// CheckIdempotencyKey returns the existing task ID if a task with the given
// idempotency key already exists. Returns empty string if no duplicate found.
func (c *IdempotencyChecker) CheckIdempotencyKey(
	ctx context.Context,
	tx *store.Tx,
	key string,
) (string, error) {
	if key == "" {
		return "", nil
	}
	var id string
	err := tx.QueryRowContext(ctx,
		`SELECT id FROM tasks WHERE idempotency_key = $1 LIMIT 1`, key,
	).Scan(&id)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("idempotency key check: %w", err)
	}
	return id, nil
}

// TaskRunRow is the row model for task_runs.
type TaskRunRow struct {
	ID         string
	TaskID     string
	NodeID     string
	Attempt    int
	Status     string
	StartedAt  time.Time
	FinishedAt *time.Time
	ErrorMsg   string
	Result     string
	CreatedAt  time.Time
}
