package deployment

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/edgeai-platform/ai-edge/internal/store"
)

// DeploymentRow is the row model for the model_deployments table.
type DeploymentRow struct {
	ID           string
	ModelName    string
	ModelVersion string
	Target       json.RawMessage
	Runtime      string
	Rollout      json.RawMessage
	Status       json.RawMessage
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// DeploymentStatusJSON is the JSON shape stored in the status column.
type DeploymentStatusJSON struct {
	DesiredNodes int    `json:"desired_nodes"`
	ReadyNodes   int    `json:"ready_nodes"`
	FailedNodes  int    `json:"failed_nodes"`
	Phase        string `json:"phase"`
}

// DeploymentTargetJSON is the JSON shape stored in the target column.
type DeploymentTargetJSON struct {
	GatewayIDs    []string          `json:"gateway_ids,omitempty"`
	LabelSelector map[string]string `json:"label_selector,omitempty"`
}

// RolloutJSON is the JSON shape stored in the rollout column.
type RolloutJSON struct {
	MaxUnavailable int `json:"max_unavailable"`
}

// RuntimeProfileRow is the row model for the runtime_profiles table.
type RuntimeProfileRow struct {
	ID            string
	Name          string
	Selector      json.RawMessage
	Runtime       string
	Priority      int
	RuntimeConfig json.RawMessage
	CreatedAt     time.Time
}

// Store provides data access for model_deployments and runtime_profiles.
type Store struct {
	db *store.DB
}

// NewStore creates a deployment Store.
func NewStore(db *store.DB) *Store {
	return &Store{db: db}
}

// CreateDeployment inserts a new model deployment.
func (s *Store) CreateDeployment(ctx context.Context, row *DeploymentRow) error {
	const q = `
		INSERT INTO model_deployments (model_name, model_version, target, runtime, rollout, status)
		VALUES ($1,$2,$3,$4,$5,$6)
		RETURNING id, created_at, updated_at`

	return s.db.QueryRowContext(ctx, q,
		row.ModelName,
		row.ModelVersion,
		row.Target,
		row.Runtime,
		row.Rollout,
		row.Status,
	).Scan(&row.ID, &row.CreatedAt, &row.UpdatedAt)
}

// GetDeployment loads a single deployment by ID.
func (s *Store) GetDeployment(ctx context.Context, id string) (*DeploymentRow, error) {
	const q = `
		SELECT id, model_name, model_version, target, runtime, rollout, status, created_at, updated_at
		FROM model_deployments WHERE id = $1`

	return s.scanOne(ctx, q, id)
}

// GetDeploymentForUpdate loads a deployment with a FOR UPDATE lock inside a transaction.
func (s *Store) GetDeploymentForUpdate(ctx context.Context, tx *store.Tx, id string) (*DeploymentRow, error) {
	const q = `
		SELECT id, model_name, model_version, target, runtime, rollout, status, created_at, updated_at
		FROM model_deployments WHERE id = $1 FOR UPDATE`

	row := &DeploymentRow{}
	err := tx.QueryRowContext(ctx, q, id).Scan(
		&row.ID, &row.ModelName, &row.ModelVersion,
		&row.Target, &row.Runtime, &row.Rollout, &row.Status,
		&row.CreatedAt, &row.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, store.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("deployment: get for update: %w", err)
	}
	return row, nil
}

func (s *Store) scanOne(ctx context.Context, query string, args ...any) (*DeploymentRow, error) {
	row := &DeploymentRow{}
	err := s.db.QueryRowContext(ctx, query, args...).Scan(
		&row.ID, &row.ModelName, &row.ModelVersion,
		&row.Target, &row.Runtime, &row.Rollout, &row.Status,
		&row.CreatedAt, &row.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, store.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("deployment: get: %w", err)
	}
	return row, nil
}

// DeploymentListFilter holds optional query filters for ListDeployments.
type DeploymentListFilter struct {
	ModelName string
	Phase     string
	Limit     int
	Offset    int
}

// ListDeployments returns a paginated slice of deployments.
func (s *Store) ListDeployments(ctx context.Context, f DeploymentListFilter) ([]*DeploymentRow, int, error) {
	where := "WHERE 1=1"
	args := []any{}
	idx := 1

	if f.ModelName != "" {
		where += fmt.Sprintf(" AND model_name = $%d", idx)
		args = append(args, f.ModelName)
		idx++
	}
	if f.Phase != "" {
		where += fmt.Sprintf(" AND status->>'phase' = $%d", idx)
		args = append(args, f.Phase)
		idx++
	}

	var total int
	countQ := "SELECT COUNT(*) FROM model_deployments " + where
	if err := s.db.QueryRowContext(ctx, countQ, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("deployment: list count: %w", err)
	}

	limit := f.Limit
	if limit <= 0 || limit > 100 {
		limit = 20
	}

	dataQ := fmt.Sprintf(`
		SELECT id, model_name, model_version, target, runtime, rollout, status, created_at, updated_at
		FROM model_deployments %s
		ORDER BY created_at DESC
		LIMIT $%d OFFSET $%d`, where, idx, idx+1)
	args = append(args, limit, f.Offset)

	rows, err := s.db.QueryContext(ctx, dataQ, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("deployment: list: %w", err)
	}
	defer rows.Close()

	var result []*DeploymentRow
	for rows.Next() {
		r := &DeploymentRow{}
		if err := rows.Scan(
			&r.ID, &r.ModelName, &r.ModelVersion,
			&r.Target, &r.Runtime, &r.Rollout, &r.Status,
			&r.CreatedAt, &r.UpdatedAt,
		); err != nil {
			return nil, 0, fmt.Errorf("deployment: scan row: %w", err)
		}
		result = append(result, r)
	}
	return result, total, rows.Err()
}

// ListPendingDeployments returns deployments in Pending or RollingOut phase.
func (s *Store) ListPendingDeployments(ctx context.Context) ([]*DeploymentRow, error) {
	const q = `
		SELECT id, model_name, model_version, target, runtime, rollout, status, created_at, updated_at
		FROM model_deployments
		WHERE status->>'phase' IN ('Pending', 'RollingOut')
		ORDER BY created_at ASC`

	rows, err := s.db.QueryContext(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("deployment: list pending: %w", err)
	}
	defer rows.Close()

	var result []*DeploymentRow
	for rows.Next() {
		r := &DeploymentRow{}
		if err := rows.Scan(
			&r.ID, &r.ModelName, &r.ModelVersion,
			&r.Target, &r.Runtime, &r.Rollout, &r.Status,
			&r.CreatedAt, &r.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("deployment: scan pending row: %w", err)
		}
		result = append(result, r)
	}
	return result, rows.Err()
}

// UpdateDeployment updates mutable fields on a deployment.
func (s *Store) UpdateDeployment(ctx context.Context, tx *store.Tx, id string, runtime string, rollout json.RawMessage, status json.RawMessage) error {
	const q = `
		UPDATE model_deployments
		SET runtime = $1, rollout = $2, status = $3, updated_at = now()
		WHERE id = $4`

	res, err := tx.ExecContext(ctx, q, runtime, rollout, status, id)
	if err != nil {
		return fmt.Errorf("deployment: update: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return store.ErrNotFound
	}
	return nil
}

// UpdateDeploymentStatus updates only the status JSON column.
func (s *Store) UpdateDeploymentStatus(ctx context.Context, tx *store.Tx, id string, status json.RawMessage) error {
	const q = `
		UPDATE model_deployments
		SET status = $1, updated_at = now()
		WHERE id = $2`

	res, err := tx.ExecContext(ctx, q, status, id)
	if err != nil {
		return fmt.Errorf("deployment: update status: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return store.ErrNotFound
	}
	return nil
}

// --- RuntimeProfile CRUD ---

// CreateRuntimeProfile inserts a new runtime profile.
func (s *Store) CreateRuntimeProfile(ctx context.Context, row *RuntimeProfileRow) error {
	const q = `
		INSERT INTO runtime_profiles (name, selector, runtime, priority, runtime_config)
		VALUES ($1,$2,$3,$4,$5)
		RETURNING id, created_at`

	return s.db.QueryRowContext(ctx, q,
		row.Name,
		row.Selector,
		row.Runtime,
		row.Priority,
		row.RuntimeConfig,
	).Scan(&row.ID, &row.CreatedAt)
}

// GetRuntimeProfile loads a runtime profile by ID.
func (s *Store) GetRuntimeProfile(ctx context.Context, id string) (*RuntimeProfileRow, error) {
	const q = `
		SELECT id, name, selector, runtime, priority, runtime_config, created_at
		FROM runtime_profiles WHERE id = $1`

	row := &RuntimeProfileRow{}
	err := s.db.QueryRowContext(ctx, q, id).Scan(
		&row.ID, &row.Name, &row.Selector, &row.Runtime,
		&row.Priority, &row.RuntimeConfig, &row.CreatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, store.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("runtime profile: get: %w", err)
	}
	return row, nil
}

// ListRuntimeProfiles returns all runtime profiles ordered by priority descending.
func (s *Store) ListRuntimeProfiles(ctx context.Context) ([]*RuntimeProfileRow, error) {
	const q = `
		SELECT id, name, selector, runtime, priority, runtime_config, created_at
		FROM runtime_profiles
		ORDER BY priority DESC, created_at ASC`

	rows, err := s.db.QueryContext(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("runtime profile: list: %w", err)
	}
	defer rows.Close()

	var result []*RuntimeProfileRow
	for rows.Next() {
		r := &RuntimeProfileRow{}
		if err := rows.Scan(
			&r.ID, &r.Name, &r.Selector, &r.Runtime,
			&r.Priority, &r.RuntimeConfig, &r.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("runtime profile: scan row: %w", err)
		}
		result = append(result, r)
	}
	return result, rows.Err()
}

// DeleteRuntimeProfile removes a runtime profile by ID.
func (s *Store) DeleteRuntimeProfile(ctx context.Context, id string) error {
	const q = `DELETE FROM runtime_profiles WHERE id = $1`
	res, err := s.db.ExecContext(ctx, q, id)
	if err != nil {
		return fmt.Errorf("runtime profile: delete: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return store.ErrNotFound
	}
	return nil
}

// --- EdgeRuntimeState ---

// UpsertRuntimeState inserts or updates a runtime state snapshot.
func (s *Store) UpsertRuntimeState(ctx context.Context, nodeID, modelName, modelVersion, runtime, status string, metrics json.RawMessage) error {
	const q = `
		INSERT INTO edge_runtime_states (node_id, model_name, model_version, runtime, status, metrics, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6, now())
		ON CONFLICT (node_id, model_name, model_version)
		DO UPDATE SET runtime = $4, status = $5, metrics = $6, updated_at = now()`

	_, err := s.db.ExecContext(ctx, q, nodeID, modelName, modelVersion, runtime, status, metrics)
	if err != nil {
		return fmt.Errorf("runtime state: upsert: %w", err)
	}
	return nil
}

// CountRuntimeStatesByDeployment counts ready and failed nodes for a given model deployment.
func (s *Store) CountRuntimeStatesByDeployment(ctx context.Context, modelName, modelVersion string) (ready, failed int, err error) {
	const q = `
		SELECT
			COUNT(*) FILTER (WHERE status = 'Running') AS ready,
			COUNT(*) FILTER (WHERE status = 'Failed') AS failed
		FROM edge_runtime_states
		WHERE model_name = $1 AND model_version = $2`

	err = s.db.QueryRowContext(ctx, q, modelName, modelVersion).Scan(&ready, &failed)
	if err != nil {
		return 0, 0, fmt.Errorf("runtime state: count: %w", err)
	}
	return ready, failed, nil
}

// ListNodesByGatewayAndLabels returns node IDs that belong to any of the given
// gateway IDs and match the label selector.
func (s *Store) ListNodesByGatewayAndLabels(ctx context.Context, gatewayIDs []string, labelSelector map[string]string) ([]string, error) {
	where := "WHERE online = true"
	args := []any{}
	idx := 1

	if len(gatewayIDs) > 0 {
		where += fmt.Sprintf(" AND gateway_id = ANY($%d::uuid[])", idx)
		args = append(args, pgUUIDArray(gatewayIDs))
		idx++
	}

	for k, v := range labelSelector {
		where += fmt.Sprintf(" AND labels->>$%d = $%d", idx, idx+1)
		args = append(args, k, v)
		idx += 2
	}

	q := fmt.Sprintf(`SELECT id FROM edge_nodes %s`, where)
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("deployment: list target nodes: %w", err)
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("deployment: scan node id: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// pgUUIDArray formats a string slice as a Postgres UUID array literal.
func pgUUIDArray(ids []string) string {
	result := "{"
	for i, id := range ids {
		if i > 0 {
			result += ","
		}
		result += id
	}
	result += "}"
	return result
}
