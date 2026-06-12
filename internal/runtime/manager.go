package runtime

import (
	"context"
	"fmt"
	"sync"
)

// Manager holds registered runtime adapters and dispatches operations
// to the correct adapter by runtime name.
type Manager struct {
	mu       sync.RWMutex
	adapters map[string]RuntimeAdapter
}

// NewManager creates an empty runtime Manager.
func NewManager() *Manager {
	return &Manager{
		adapters: make(map[string]RuntimeAdapter),
	}
}

// Register adds a runtime adapter. Panics if a duplicate name is registered.
func (m *Manager) Register(adapter RuntimeAdapter) {
	m.mu.Lock()
	defer m.mu.Unlock()

	name := adapter.Name()
	if _, exists := m.adapters[name]; exists {
		panic(fmt.Sprintf("runtime: adapter %q already registered", name))
	}
	m.adapters[name] = adapter
}

// Get returns the adapter for the given runtime name.
func (m *Manager) Get(runtime string) (RuntimeAdapter, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	a, ok := m.adapters[runtime]
	if !ok {
		return nil, fmt.Errorf("runtime: unknown adapter %q", runtime)
	}
	return a, nil
}

// Install dispatches to the named adapter's Install method.
func (m *Manager) Install(ctx context.Context, runtime string, cfg InstallConfig) error {
	a, err := m.Get(runtime)
	if err != nil {
		return err
	}
	return a.Install(ctx, cfg)
}

// Start dispatches to the named adapter's Start method.
func (m *Manager) Start(ctx context.Context, runtime, modelName, modelVersion string) error {
	a, err := m.Get(runtime)
	if err != nil {
		return err
	}
	return a.Start(ctx, modelName, modelVersion)
}

// Stop dispatches to the named adapter's Stop method.
func (m *Manager) Stop(ctx context.Context, runtime, modelName, modelVersion string) error {
	a, err := m.Get(runtime)
	if err != nil {
		return err
	}
	return a.Stop(ctx, modelName, modelVersion)
}

// Restart dispatches to the named adapter's Restart method.
func (m *Manager) Restart(ctx context.Context, runtime, modelName, modelVersion string) error {
	a, err := m.Get(runtime)
	if err != nil {
		return err
	}
	return a.Restart(ctx, modelName, modelVersion)
}

// Uninstall dispatches to the named adapter's Uninstall method.
func (m *Manager) Uninstall(ctx context.Context, runtime, modelName, modelVersion string) error {
	a, err := m.Get(runtime)
	if err != nil {
		return err
	}
	return a.Uninstall(ctx, modelName, modelVersion)
}

// Status dispatches to the named adapter's Status method.
func (m *Manager) Status(ctx context.Context, runtime, modelName, modelVersion string) (*Status, error) {
	a, err := m.Get(runtime)
	if err != nil {
		return nil, err
	}
	return a.Status(ctx, modelName, modelVersion)
}

// ListAdapters returns the names of all registered adapters.
func (m *Manager) ListAdapters() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	names := make([]string, 0, len(m.adapters))
	for name := range m.adapters {
		names = append(names, name)
	}
	return names
}
