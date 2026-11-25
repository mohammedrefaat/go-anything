package library

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/mohammedrefaat/go-anything/pkg/adapterkit"
)

// Invoker is an in-process library function invoker.
type Invoker func(ctx context.Context, input any) (any, error)

// Adapter implements adapterkit.Replay for in-process library functions.
type Adapter struct {
	mu        sync.RWMutex
	functions map[string]Invoker
	name      string
}

// NewAdapter creates a new in-process library application adapter.
func NewAdapter(name string) *Adapter {
	if name == "" {
		name = "library"
	}
	return &Adapter{
		name:      name,
		functions: make(map[string]Invoker),
	}
}

// Register registers an in-process function invoker by unit ID.
func (a *Adapter) Register(unitID string, invoker Invoker) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.functions[unitID] = invoker
}

// DriverName returns the adapter identifier.
func (a *Adapter) DriverName() string {
	return "library:" + a.name
}

// ExecuteCase replays a test case against the registered in-process function.
func (a *Adapter) ExecuteCase(ctx context.Context, testCase *adapterkit.Case) (*adapterkit.CaseOutput, error) {
	if testCase == nil {
		return nil, errors.New("library: testCase cannot be nil")
	}

	a.mu.RLock()
	fn, exists := a.functions[testCase.UnitID]
	a.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("library: no function registered for unit %q", testCase.UnitID)
	}

	out, err := fn(ctx, testCase.Input.Body)
	if err != nil {
		return &adapterkit.CaseOutput{
			ExitCode:     1,
			ErrorMessage: err.Error(),
		}, nil
	}

	return &adapterkit.CaseOutput{
		ExitCode: 0,
		Body:     out,
	}, nil
}
