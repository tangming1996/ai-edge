package deployment

import "encoding/json"

// RolloutChecker validates rollout constraints before proceeding with
// deployment to the next batch of nodes.
type RolloutChecker struct{}

// NewRolloutChecker creates a RolloutChecker.
func NewRolloutChecker() *RolloutChecker {
	return &RolloutChecker{}
}

// CanProceed checks whether additional nodes can be updated given the
// current deployment state and rollout configuration. It enforces the
// maxUnavailable constraint: at most maxUnavailable nodes may be
// simultaneously not-ready (i.e. in-progress or failed).
func (rc *RolloutChecker) CanProceed(deploymentRow *DeploymentRow) (bool, int) {
	var st DeploymentStatusJSON
	if err := json.Unmarshal(deploymentRow.Status, &st); err != nil {
		return false, 0
	}

	var rollout RolloutJSON
	if err := json.Unmarshal(deploymentRow.Rollout, &rollout); err != nil {
		return false, 0
	}

	if rollout.MaxUnavailable <= 0 {
		rollout.MaxUnavailable = 1
	}

	unavailable := st.DesiredNodes - st.ReadyNodes
	if unavailable < 0 {
		unavailable = 0
	}

	if unavailable >= rollout.MaxUnavailable {
		return false, 0
	}

	allowedBatch := rollout.MaxUnavailable - unavailable
	remaining := st.DesiredNodes - st.ReadyNodes - st.FailedNodes
	if remaining < 0 {
		remaining = 0
	}
	if allowedBatch > remaining {
		allowedBatch = remaining
	}

	return allowedBatch > 0, allowedBatch
}

// IsComplete returns true if the deployment has reached all desired nodes
// (either successfully or with failures).
func (rc *RolloutChecker) IsComplete(deploymentRow *DeploymentRow) bool {
	var st DeploymentStatusJSON
	if err := json.Unmarshal(deploymentRow.Status, &st); err != nil {
		return false
	}
	return st.ReadyNodes+st.FailedNodes >= st.DesiredNodes
}

// SuggestPhase returns the recommended phase based on current status.
func (rc *RolloutChecker) SuggestPhase(deploymentRow *DeploymentRow) string {
	var st DeploymentStatusJSON
	if err := json.Unmarshal(deploymentRow.Status, &st); err != nil {
		return "Failed"
	}

	if st.ReadyNodes >= st.DesiredNodes {
		return "Active"
	}
	if st.FailedNodes > 0 && st.ReadyNodes+st.FailedNodes >= st.DesiredNodes {
		return "Failed"
	}
	return "RollingOut"
}
