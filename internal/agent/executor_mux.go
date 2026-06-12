package agent

import (
	"context"
	"fmt"
)

// ExecutorMux dispatches task types to concrete executors.
type ExecutorMux struct {
	handlers map[string]TaskExecutor
}

// NewExecutorMux creates an empty ExecutorMux.
func NewExecutorMux() *ExecutorMux {
	return &ExecutorMux{
		handlers: make(map[string]TaskExecutor),
	}
}

// Register binds one or more task types to the same executor.
func (m *ExecutorMux) Register(executor TaskExecutor, taskTypes ...string) {
	for _, taskType := range taskTypes {
		m.handlers[taskType] = executor
	}
}

// Execute dispatches to the registered executor for the given task type.
func (m *ExecutorMux) Execute(ctx context.Context, taskType string, payload []byte) ([]byte, error) {
	executor, ok := m.handlers[taskType]
	if !ok {
		return nil, fmt.Errorf("executor mux: unsupported task type %q", taskType)
	}
	return executor.Execute(ctx, taskType, payload)
}
