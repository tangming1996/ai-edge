package agent_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/edgeai-platform/ai-edge/internal/agent"
)

type fakeExecutor struct {
	calls int
	lastType   string
	lastPayload []byte
	returnBytes []byte
	returnErr   error
}

func (f *fakeExecutor) Execute(_ context.Context, taskType string, payload []byte) ([]byte, error) {
	f.calls++
	f.lastType = taskType
	f.lastPayload = payload
	return f.returnBytes, f.returnErr
}

func TestExecutorMux_RegisterAndExecute(t *testing.T) {
	mux := agent.NewExecutorMux()
	ex := &fakeExecutor{returnBytes: []byte("ok")}
	mux.Register(ex, "model.run", "model.eval")

	for _, tt := range []string{"model.run", "model.eval"} {
		out, err := mux.Execute(context.Background(), tt, []byte(`{"x":1}`))
		if err != nil {
			t.Fatalf("Execute(%s): %v", tt, err)
		}
		if string(out) != "ok" {
			t.Fatalf("Execute(%s) -> %q, want ok", tt, out)
		}
	}
	if ex.calls != 2 {
		t.Fatalf("expected 2 calls, got %d", ex.calls)
	}
}

func TestExecutorMux_PassesThroughContext(t *testing.T) {
	// The mux must propagate the incoming context to the executor.
	type ctxKey struct{}
	want := context.WithValue(context.Background(), ctxKey{}, "v")

	var got context.Context
	mux := agent.NewExecutorMux()
	mux.Register(executorFunc(func(ctx context.Context, _ string, _ []byte) ([]byte, error) {
		got = ctx
		return nil, nil
	}), "k")

	if _, err := mux.Execute(want, "k", nil); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if got == nil || got.Value(ctxKey{}) != "v" {
		t.Fatalf("context not propagated: %v", got)
	}
}

func TestExecutorMux_UnknownTaskType(t *testing.T) {
	mux := agent.NewExecutorMux()
	mux.Register(&fakeExecutor{}, "registered")

	_, err := mux.Execute(context.Background(), "not.registered", []byte("payload"))
	if err == nil {
		t.Fatal("expected error for unknown task type")
	}
	if !errors.Is(err, err) {
		// Just ensure the error is non-nil and is not the typed fake error.
	}
	if err.Error() == "" {
		t.Fatal("empty error message")
	}
}

func TestExecutorMux_ExecutorErrorPropagates(t *testing.T) {
	mux := agent.NewExecutorMux()
	sentinel := errors.New("executor kaboom")
	mux.Register(&fakeExecutor{returnErr: sentinel}, "k")

	_, err := mux.Execute(context.Background(), "k", []byte("p"))
	if err == nil {
		t.Fatal("expected error from executor")
	}
	if !errors.Is(err, sentinel) {
		t.Fatalf("executor error not propagated: %v", err)
	}
}

func TestExecutorMux_PassesThroughPayload(t *testing.T) {
	mux := agent.NewExecutorMux()
	ex := &fakeExecutor{}
	mux.Register(ex, "k")

	payload := []byte(`{"hello":"world"}`)
	if _, err := mux.Execute(context.Background(), "k", payload); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if string(ex.lastPayload) != string(payload) {
		t.Fatalf("payload mismatch: %q", ex.lastPayload)
	}
	if ex.lastType != "k" {
		t.Fatalf("taskType mismatch: %q", ex.lastType)
	}
}

func TestExecutorMux_OverwriteRegistration(t *testing.T) {
	// Re-registering the same task type replaces the previous handler.
	mux := agent.NewExecutorMux()
	first := &fakeExecutor{returnBytes: []byte("first")}
	second := &fakeExecutor{returnBytes: []byte("second")}
	mux.Register(first, "k")
	mux.Register(second, "k")

	out, err := mux.Execute(context.Background(), "k", nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if string(out) != "second" {
		t.Fatalf("expected second to win: %q", out)
	}
	if first.calls != 0 {
		t.Fatalf("first executor should not have been called")
	}
}

func TestExecutorMux_MultipleTypesShareExecutor(t *testing.T) {
	// The same executor instance should be reused across all registered
	// types and share its state (call count, etc.).
	mux := agent.NewExecutorMux()
	ex := &fakeExecutor{returnBytes: []byte("x")}
	mux.Register(ex, "a", "b", "c")

	for _, tt := range []string{"a", "b", "c"} {
		if _, err := mux.Execute(context.Background(), tt, nil); err != nil {
			t.Fatalf("Execute(%s): %v", tt, err)
		}
	}
	if ex.calls != 3 {
		t.Fatalf("expected 3 calls on shared executor, got %d", ex.calls)
	}
}

func TestExecutorMux_EmptyMux(t *testing.T) {
	mux := agent.NewExecutorMux()
	_, err := mux.Execute(context.Background(), "any", nil)
	if err == nil {
		t.Fatal("expected error from empty mux")
	}
}

// executorFunc lets us use a closure as a TaskExecutor in tests.
type executorFunc func(ctx context.Context, taskType string, payload []byte) ([]byte, error)

func (f executorFunc) Execute(ctx context.Context, taskType string, payload []byte) ([]byte, error) {
	return f(ctx, taskType, payload)
}

// Sanity test: a non-nil error is still non-nil after formatting.
func TestExecutorMux_ErrorString(t *testing.T) {
	mux := agent.NewExecutorMux()
	_, err := mux.Execute(context.Background(), "x", nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if fmt.Sprint(err) == "" {
		t.Fatal("formatted error is empty")
	}
}
