package task

import (
	"fmt"

	pb "github.com/edgeai-platform/ai-edge/api/gen/go/edge/ai/api/v1"
)

// statusToProto maps DB text status to proto enum.
var statusToProto = map[string]pb.TaskStatus{
	"Pending":            pb.TaskStatus_TASK_STATUS_PENDING,
	"Dispatching":        pb.TaskStatus_TASK_STATUS_DISPATCHING,
	"Running":            pb.TaskStatus_TASK_STATUS_RUNNING,
	"Success":            pb.TaskStatus_TASK_STATUS_SUCCESS,
	"Failed":             pb.TaskStatus_TASK_STATUS_FAILED,
	"Retrying":           pb.TaskStatus_TASK_STATUS_RETRYING,
	"Timeout":            pb.TaskStatus_TASK_STATUS_TIMEOUT,
	"Cancelled":          pb.TaskStatus_TASK_STATUS_CANCELLED,
	"PartiallySucceeded": pb.TaskStatus_TASK_STATUS_PARTIALLY_SUCCEEDED,
}

// protoToStatus maps proto enum to DB text status.
var protoToStatus = map[pb.TaskStatus]string{
	pb.TaskStatus_TASK_STATUS_PENDING:             "Pending",
	pb.TaskStatus_TASK_STATUS_DISPATCHING:         "Dispatching",
	pb.TaskStatus_TASK_STATUS_RUNNING:             "Running",
	pb.TaskStatus_TASK_STATUS_SUCCESS:             "Success",
	pb.TaskStatus_TASK_STATUS_FAILED:              "Failed",
	pb.TaskStatus_TASK_STATUS_RETRYING:            "Retrying",
	pb.TaskStatus_TASK_STATUS_TIMEOUT:             "Timeout",
	pb.TaskStatus_TASK_STATUS_CANCELLED:           "Cancelled",
	pb.TaskStatus_TASK_STATUS_PARTIALLY_SUCCEEDED: "PartiallySucceeded",
}

// dispatchToProto maps DB text dispatch status to proto enum.
var dispatchToProto = map[string]pb.DispatchStatus{
	"Unclaimed": pb.DispatchStatus_DISPATCH_STATUS_UNCLAIMED,
	"Claimed":   pb.DispatchStatus_DISPATCH_STATUS_CLAIMED,
	"Delivered": pb.DispatchStatus_DISPATCH_STATUS_DELIVERED,
}

// scopeToProto maps DB text scope to proto enum.
var scopeToProto = map[string]pb.TaskScope{
	"Region": pb.TaskScope_TASK_SCOPE_REGION,
	"Node":   pb.TaskScope_TASK_SCOPE_NODE,
}

// protoToScope maps proto enum to DB text scope.
var protoToScope = map[pb.TaskScope]string{
	pb.TaskScope_TASK_SCOPE_REGION: "Region",
	pb.TaskScope_TASK_SCOPE_NODE:   "Node",
}

// terminalStatuses are states that cannot transition further.
var terminalStatuses = map[pb.TaskStatus]bool{
	pb.TaskStatus_TASK_STATUS_SUCCESS:             true,
	pb.TaskStatus_TASK_STATUS_CANCELLED:           true,
	pb.TaskStatus_TASK_STATUS_TIMEOUT:             true,
	pb.TaskStatus_TASK_STATUS_PARTIALLY_SUCCEEDED: true,
}

// allowedTransitions defines valid (from → to) status transitions.
var allowedTransitions = map[pb.TaskStatus][]pb.TaskStatus{
	pb.TaskStatus_TASK_STATUS_PENDING: {
		pb.TaskStatus_TASK_STATUS_DISPATCHING,
		pb.TaskStatus_TASK_STATUS_CANCELLED,
	},
	pb.TaskStatus_TASK_STATUS_DISPATCHING: {
		pb.TaskStatus_TASK_STATUS_RUNNING,
		pb.TaskStatus_TASK_STATUS_CANCELLED,
		pb.TaskStatus_TASK_STATUS_TIMEOUT,
	},
	pb.TaskStatus_TASK_STATUS_RUNNING: {
		pb.TaskStatus_TASK_STATUS_SUCCESS,
		pb.TaskStatus_TASK_STATUS_FAILED,
		pb.TaskStatus_TASK_STATUS_TIMEOUT,
		pb.TaskStatus_TASK_STATUS_CANCELLED,
		pb.TaskStatus_TASK_STATUS_PARTIALLY_SUCCEEDED,
	},
	pb.TaskStatus_TASK_STATUS_FAILED: {
		pb.TaskStatus_TASK_STATUS_RETRYING,
		pb.TaskStatus_TASK_STATUS_CANCELLED,
	},
	pb.TaskStatus_TASK_STATUS_RETRYING: {
		pb.TaskStatus_TASK_STATUS_RUNNING,
		pb.TaskStatus_TASK_STATUS_CANCELLED,
	},
}

// ValidateTransition returns true if transitioning from → to is allowed.
func ValidateTransition(from, to pb.TaskStatus) bool {
	targets, ok := allowedTransitions[from]
	if !ok {
		return false
	}
	for _, t := range targets {
		if t == to {
			return true
		}
	}
	return false
}

// IsTerminal returns true if the status is a terminal (final) state.
func IsTerminal(status pb.TaskStatus) bool {
	return terminalStatuses[status]
}

// ErrInvalidTransition is returned when a status transition is not allowed.
type ErrInvalidTransition struct {
	From pb.TaskStatus
	To   pb.TaskStatus
}

func (e *ErrInvalidTransition) Error() string {
	return fmt.Sprintf("invalid status transition: %s → %s",
		protoToStatus[e.From], protoToStatus[e.To])
}
