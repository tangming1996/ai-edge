package deployment

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/edgeai-platform/ai-edge/internal/store"
)

// ControllerConfig holds configuration for the deployment controller.
type ControllerConfig struct {
	PollInterval time.Duration
}

func (c *ControllerConfig) applyDefaults() {
	if c.PollInterval == 0 {
		c.PollInterval = 10 * time.Second
	}
}

// Controller reconciles model deployments by watching for pending deployments,
// resolving target nodes, generating tasks, and updating deployment status.
type Controller struct {
	store     *Store
	taskStore TaskCreator
	db        *store.DB
	cfg       ControllerConfig
}

// TaskCreator abstracts the task creation dependency so the controller does
// not import the task package directly.
type TaskCreator interface {
	CreateDeploymentTask(ctx context.Context, tx *store.Tx, deploymentID, modelName, modelVersion, runtime string, nodeIDs []string, payload json.RawMessage) error
}

// NewController creates a deployment Controller.
func NewController(db *store.DB, taskCreator TaskCreator, cfg ControllerConfig) *Controller {
	cfg.applyDefaults()
	return &Controller{
		store:     NewStore(db),
		taskStore: taskCreator,
		db:        db,
		cfg:       cfg,
	}
}

// Run starts the controller reconciliation loop.
func (c *Controller) Run(ctx context.Context) {
	ticker := time.NewTicker(c.cfg.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Println("deployment: controller stopped")
			return
		case <-ticker.C:
			if err := c.reconcile(ctx); err != nil {
				log.Printf("deployment: reconcile error: %v", err)
			}
		}
	}
}

func (c *Controller) reconcile(ctx context.Context) error {
	deployments, err := c.store.ListPendingDeployments(ctx)
	if err != nil {
		return fmt.Errorf("list pending: %w", err)
	}

	for _, d := range deployments {
		if err := c.reconcileOne(ctx, d); err != nil {
			log.Printf("deployment: reconcile %s failed: %v", d.ID, err)
		}
	}
	return nil
}

func (c *Controller) reconcileOne(ctx context.Context, d *DeploymentRow) error {
	var target DeploymentTargetJSON
	if err := json.Unmarshal(d.Target, &target); err != nil {
		return fmt.Errorf("unmarshal target: %w", err)
	}

	nodeIDs, err := c.store.ListNodesByGatewayAndLabels(ctx, target.GatewayIDs, target.LabelSelector)
	if err != nil {
		return fmt.Errorf("list target nodes: %w", err)
	}

	runtime, err := c.resolveRuntime(ctx, d.Runtime, d.ModelName)
	if err != nil {
		return fmt.Errorf("resolve runtime: %w", err)
	}

	return c.db.WithTx(ctx, func(tx *store.Tx) error {
		row, err := c.store.GetDeploymentForUpdate(ctx, tx, d.ID)
		if err != nil {
			return err
		}

		var st DeploymentStatusJSON
		_ = json.Unmarshal(row.Status, &st)

		// Only generate tasks for Pending deployments that haven't been dispatched yet.
		if st.Phase == "Pending" && len(nodeIDs) > 0 {
			payload, _ := json.Marshal(map[string]string{
				"deployment_id": d.ID,
				"model_name":    d.ModelName,
				"model_version": d.ModelVersion,
				"runtime":       runtime,
			})

			if err := c.taskStore.CreateDeploymentTask(ctx, tx, d.ID, d.ModelName, d.ModelVersion, runtime, nodeIDs, payload); err != nil {
				return fmt.Errorf("create deployment task: %w", err)
			}

			st.Phase = "RollingOut"
			st.DesiredNodes = len(nodeIDs)
		}

		// For RollingOut deployments, aggregate runtime state counts.
		if st.Phase == "RollingOut" {
			ready, failed, err := c.store.CountRuntimeStatesByDeployment(ctx, d.ModelName, d.ModelVersion)
			if err != nil {
				return fmt.Errorf("count runtime states: %w", err)
			}
			st.ReadyNodes = ready
			st.FailedNodes = failed

			if ready >= st.DesiredNodes {
				st.Phase = "Active"
			} else if failed > 0 && ready+failed >= st.DesiredNodes {
				st.Phase = "Failed"
			}
		}

		statusJSON, _ := json.Marshal(st)
		return c.store.UpdateDeploymentStatus(ctx, tx, row.ID, statusJSON)
	})
}

// resolveRuntime resolves "auto" runtime by looking up RuntimeProfiles.
// If a matching profile is found, its runtime value is used; otherwise
// the deployment's original runtime value is returned.
func (c *Controller) resolveRuntime(ctx context.Context, runtime, modelName string) (string, error) {
	if runtime != "auto" {
		return runtime, nil
	}

	profiles, err := c.store.ListRuntimeProfiles(ctx)
	if err != nil {
		return "", fmt.Errorf("list runtime profiles: %w", err)
	}

	for _, p := range profiles {
		var selector map[string]string
		if err := json.Unmarshal(p.Selector, &selector); err != nil {
			continue
		}

		if matchesSelector(selector, modelName) {
			return p.Runtime, nil
		}
	}

	return "llamacpp", nil
}

// matchesSelector checks if the model name matches the profile selector.
// A selector with key "model_name" matches if the value equals the model name.
// An empty selector matches everything.
func matchesSelector(selector map[string]string, modelName string) bool {
	if len(selector) == 0 {
		return true
	}
	if v, ok := selector["model_name"]; ok {
		return v == modelName
	}
	return true
}

// DeploymentTaskCreator is a default implementation of TaskCreator that
// creates tasks directly in the tasks table.
type DeploymentTaskCreator struct {
	db *store.DB
}

// NewDeploymentTaskCreator creates a DeploymentTaskCreator.
func NewDeploymentTaskCreator(db *store.DB) *DeploymentTaskCreator {
	return &DeploymentTaskCreator{db: db}
}

// CreateDeploymentTask creates a parent deployment task and child node tasks.
func (tc *DeploymentTaskCreator) CreateDeploymentTask(
	ctx context.Context,
	tx *store.Tx,
	deploymentID, modelName, modelVersion, runtime string,
	nodeIDs []string,
	payload json.RawMessage,
) error {
	var parentID string
	err := tx.QueryRowContext(ctx, `
		INSERT INTO tasks (scope, type, status, dispatch_status, payload, created_by)
		VALUES ('Region', 'DeployModel', 'Pending', 'Unclaimed', $1, 'controller')
		RETURNING id`,
		payload,
	).Scan(&parentID)
	if err != nil {
		return fmt.Errorf("create parent task: %w", err)
	}

	for _, nodeID := range nodeIDs {
		nodePayload, _ := json.Marshal(map[string]string{
			"deployment_id": deploymentID,
			"model_name":    modelName,
			"model_version": modelVersion,
			"runtime":       runtime,
			"action":        "StartRuntime",
		})

		_, err := tx.ExecContext(ctx, `
			INSERT INTO tasks (parent_task_id, scope, type, status, target_node_id, dispatch_status, payload, created_by)
			VALUES ($1, 'Node', 'StartRuntime', 'Pending', $2, 'Unclaimed', $3, 'controller')`,
			parentID,
			sql.NullString{String: nodeID, Valid: true},
			nodePayload,
		)
		if err != nil {
			return fmt.Errorf("create node task for %s: %w", nodeID, err)
		}
	}

	return nil
}
