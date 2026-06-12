package task

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/edgeai-platform/ai-edge/internal/store"
)

// TaskRow is the row model for the tasks table.
type TaskRow struct {
	ID              string
	ParentTaskID    sql.NullString
	Scope           string
	Type            string
	Status          string
	TargetGatewayID sql.NullString
	TargetNodeID    sql.NullString
	Payload         json.RawMessage
	Result          json.RawMessage

	DispatchStatus string
	OwnerInstance  sql.NullString
	ClaimExpireAt  sql.NullTime

	MaxRetries     int
	RetryCount     int
	TimeoutSeconds int

	IdempotencyKey sql.NullString
	CreatedBy      string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// Store provides data access for tasks, task_runs, and task_events.
type Store struct {
	db *store.DB
}

// NewStore creates a task Store.
func NewStore(db *store.DB) *Store {
	return &Store{db: db}
}

// CreateTask inserts a new task and returns the created row.
func (s *Store) CreateTask(ctx context.Context, tx *store.Tx, row *TaskRow) error {
	const q = `
		INSERT INTO tasks (
			parent_task_id, scope, type, status,
			target_gateway_id, target_node_id,
			payload, dispatch_status,
			max_retries, timeout_seconds,
			idempotency_key, created_by
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
		RETURNING id, created_at, updated_at`

	return tx.QueryRowContext(ctx, q,
		row.ParentTaskID,
		row.Scope,
		row.Type,
		row.Status,
		row.TargetGatewayID,
		row.TargetNodeID,
		row.Payload,
		row.DispatchStatus,
		row.MaxRetries,
		row.TimeoutSeconds,
		row.IdempotencyKey,
		row.CreatedBy,
	).Scan(&row.ID, &row.CreatedAt, &row.UpdatedAt)
}

// GetTask loads a single task by ID.
func (s *Store) GetTask(ctx context.Context, id string) (*TaskRow, error) {
	const q = `
		SELECT id, parent_task_id, scope, type, status,
		       target_gateway_id, target_node_id,
		       payload, result,
		       dispatch_status, owner_instance, claim_expire_at,
		       max_retries, retry_count, timeout_seconds,
		       idempotency_key, created_by, created_at, updated_at
		FROM tasks WHERE id = $1`

	row := &TaskRow{}
	err := s.db.QueryRowContext(ctx, q, id).Scan(
		&row.ID, &row.ParentTaskID, &row.Scope, &row.Type, &row.Status,
		&row.TargetGatewayID, &row.TargetNodeID,
		&row.Payload, &row.Result,
		&row.DispatchStatus, &row.OwnerInstance, &row.ClaimExpireAt,
		&row.MaxRetries, &row.RetryCount, &row.TimeoutSeconds,
		&row.IdempotencyKey, &row.CreatedBy, &row.CreatedAt, &row.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, store.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get task: %w", err)
	}
	return row, nil
}

// ListFilter holds optional query filters for ListTasks.
type ListFilter struct {
	TargetGatewayID string
	TargetNodeID    string
	Status          string
	Type            string
	Limit           int
	Offset          int
}

// ListTasks returns a slice of tasks matching the filter. It also returns the
// total count for pagination.
func (s *Store) ListTasks(ctx context.Context, f ListFilter) ([]*TaskRow, int, error) {
	where := "WHERE 1=1"
	args := []any{}
	idx := 1

	if f.TargetGatewayID != "" {
		where += fmt.Sprintf(" AND target_gateway_id = $%d", idx)
		args = append(args, f.TargetGatewayID)
		idx++
	}
	if f.TargetNodeID != "" {
		where += fmt.Sprintf(" AND target_node_id = $%d", idx)
		args = append(args, f.TargetNodeID)
		idx++
	}
	if f.Status != "" {
		where += fmt.Sprintf(" AND status = $%d", idx)
		args = append(args, f.Status)
		idx++
	}
	if f.Type != "" {
		where += fmt.Sprintf(" AND type = $%d", idx)
		args = append(args, f.Type)
		idx++
	}

	var total int
	countQ := "SELECT COUNT(*) FROM tasks " + where
	if err := s.db.QueryRowContext(ctx, countQ, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("list tasks count: %w", err)
	}

	limit := f.Limit
	if limit <= 0 || limit > 100 {
		limit = 20
	}

	dataQ := fmt.Sprintf(`
		SELECT id, parent_task_id, scope, type, status,
		       target_gateway_id, target_node_id,
		       payload, result,
		       dispatch_status, owner_instance, claim_expire_at,
		       max_retries, retry_count, timeout_seconds,
		       idempotency_key, created_by, created_at, updated_at
		FROM tasks %s
		ORDER BY created_at DESC
		LIMIT $%d OFFSET $%d`, where, idx, idx+1)
	args = append(args, limit, f.Offset)

	rows, err := s.db.QueryContext(ctx, dataQ, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list tasks: %w", err)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			log.Printf("task: close list rows: %v", err)
		}
	}()

	var result []*TaskRow
	for rows.Next() {
		r := &TaskRow{}
		if err := rows.Scan(
			&r.ID, &r.ParentTaskID, &r.Scope, &r.Type, &r.Status,
			&r.TargetGatewayID, &r.TargetNodeID,
			&r.Payload, &r.Result,
			&r.DispatchStatus, &r.OwnerInstance, &r.ClaimExpireAt,
			&r.MaxRetries, &r.RetryCount, &r.TimeoutSeconds,
			&r.IdempotencyKey, &r.CreatedBy, &r.CreatedAt, &r.UpdatedAt,
		); err != nil {
			return nil, 0, fmt.Errorf("scan task row: %w", err)
		}
		result = append(result, r)
	}
	return result, total, rows.Err()
}

// UpdateStatus sets a new status on a task inside a transaction and records an
// event. It returns the updated row.
func (s *Store) UpdateStatus(
	ctx context.Context,
	tx *store.Tx,
	taskID string,
	oldStatus, newStatus string,
	actor string,
	detail json.RawMessage,
) error {
	const q = `
		UPDATE tasks
		SET status = $1, updated_at = now()
		WHERE id = $2 AND status = $3`

	res, err := tx.ExecContext(ctx, q, newStatus, taskID, oldStatus)
	if err != nil {
		return fmt.Errorf("update task status: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return store.ErrPrecondition
	}

	return s.createEventTx(ctx, tx, taskID, "status_change", oldStatus, newStatus, actor, detail)
}

// AtomicClaim attempts to claim an unclaimed (or expired-claim) NodeTask for
// the given owner instance. Returns true if this instance won the claim race.
func (s *Store) AtomicClaim(
	ctx context.Context,
	tx *store.Tx,
	taskID string,
	ownerInstance string,
	claimDuration time.Duration,
) (bool, error) {
	const q = `
		UPDATE tasks
		SET dispatch_status = 'Claimed',
		    owner_instance  = $1,
		    claim_expire_at = $2,
		    updated_at      = now()
		WHERE id = $3
		  AND (dispatch_status = 'Unclaimed' OR claim_expire_at < now())`

	expireAt := time.Now().Add(claimDuration)
	res, err := tx.ExecContext(ctx, q, ownerInstance, expireAt, taskID)
	if err != nil {
		return false, fmt.Errorf("atomic claim: %w", err)
	}
	n, _ := res.RowsAffected()
	return n == 1, nil
}

// MarkDelivered sets dispatch_status to Delivered.
func (s *Store) MarkDelivered(ctx context.Context, tx *store.Tx, taskID string) error {
	const q = `
		UPDATE tasks
		SET dispatch_status = 'Delivered', updated_at = now()
		WHERE id = $1 AND dispatch_status = 'Claimed'`

	res, err := tx.ExecContext(ctx, q, taskID)
	if err != nil {
		return fmt.Errorf("mark delivered: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return store.ErrPrecondition
	}
	return nil
}

// UpdateResult writes the result and status back to the task.
func (s *Store) UpdateResult(
	ctx context.Context,
	tx *store.Tx,
	taskID string,
	status string,
	result json.RawMessage,
	retryCount int,
) error {
	const q = `
		UPDATE tasks
		SET status = $1, result = $2, retry_count = $3, updated_at = now()
		WHERE id = $4`

	_, err := tx.ExecContext(ctx, q, status, result, retryCount, taskID)
	if err != nil {
		return fmt.Errorf("update result: %w", err)
	}
	return nil
}

// CreateTaskRun inserts a new task_run record.
func (s *Store) CreateTaskRun(
	ctx context.Context,
	tx *store.Tx,
	taskID, nodeID string,
	attempt int,
) (*TaskRunRow, error) {
	const q = `
		INSERT INTO task_runs (task_id, node_id, attempt, status)
		VALUES ($1, $2, $3, 'Running')
		RETURNING id, started_at, created_at`

	r := &TaskRunRow{
		TaskID:  taskID,
		NodeID:  nodeID,
		Attempt: attempt,
		Status:  "Running",
	}
	err := tx.QueryRowContext(ctx, q, taskID, nodeID, attempt).
		Scan(&r.ID, &r.StartedAt, &r.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("create task run: %w", err)
	}
	return r, nil
}

// UpdateTaskRun finalizes a task_run with a status and optional error.
func (s *Store) UpdateTaskRun(
	ctx context.Context,
	tx *store.Tx,
	runID string,
	status string,
	errMsg string,
	result json.RawMessage,
) error {
	const q = `
		UPDATE task_runs
		SET status = $1, error_msg = $2, result = $3, finished_at = now()
		WHERE id = $4`

	_, err := tx.ExecContext(ctx, q, status, errMsg, result, runID)
	if err != nil {
		return fmt.Errorf("update task run: %w", err)
	}
	return nil
}

// CreateTaskEvent records an event for audit/debugging.
func (s *Store) CreateTaskEvent(
	ctx context.Context,
	tx *store.Tx,
	taskID, eventType, oldStatus, newStatus, actor string,
	detail json.RawMessage,
) error {
	return s.createEventTx(ctx, tx, taskID, eventType, oldStatus, newStatus, actor, detail)
}

func (s *Store) createEventTx(
	ctx context.Context,
	tx *store.Tx,
	taskID, eventType, oldStatus, newStatus, actor string,
	detail json.RawMessage,
) error {
	if detail == nil {
		detail = json.RawMessage(`{}`)
	}
	const q = `
		INSERT INTO task_events (task_id, event_type, old_status, new_status, actor, detail)
		VALUES ($1, $2, $3, $4, $5, $6)`

	_, err := tx.ExecContext(ctx, q, taskID, eventType, oldStatus, newStatus, actor, detail)
	if err != nil {
		return fmt.Errorf("create task event: %w", err)
	}
	return nil
}

// GetTaskForUpdate loads a task row within a transaction with FOR UPDATE lock.
func (s *Store) GetTaskForUpdate(ctx context.Context, tx *store.Tx, id string) (*TaskRow, error) {
	const q = `
		SELECT id, parent_task_id, scope, type, status,
		       target_gateway_id, target_node_id,
		       payload, result,
		       dispatch_status, owner_instance, claim_expire_at,
		       max_retries, retry_count, timeout_seconds,
		       idempotency_key, created_by, created_at, updated_at
		FROM tasks WHERE id = $1 FOR UPDATE`

	row := &TaskRow{}
	err := tx.QueryRowContext(ctx, q, id).Scan(
		&row.ID, &row.ParentTaskID, &row.Scope, &row.Type, &row.Status,
		&row.TargetGatewayID, &row.TargetNodeID,
		&row.Payload, &row.Result,
		&row.DispatchStatus, &row.OwnerInstance, &row.ClaimExpireAt,
		&row.MaxRetries, &row.RetryCount, &row.TimeoutSeconds,
		&row.IdempotencyKey, &row.CreatedBy, &row.CreatedAt, &row.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, store.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get task for update: %w", err)
	}
	return row, nil
}

// LatestAttempt returns the highest attempt number for a task.
func (s *Store) LatestAttempt(ctx context.Context, tx *store.Tx, taskID string) (int, error) {
	var attempt int
	err := tx.QueryRowContext(ctx,
		`SELECT COALESCE(MAX(attempt), 0) FROM task_runs WHERE task_id = $1`,
		taskID,
	).Scan(&attempt)
	if err != nil {
		return 0, fmt.Errorf("latest attempt: %w", err)
	}
	return attempt, nil
}
