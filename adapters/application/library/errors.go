package library

import (
	"errors"
	"fmt"
)

var (
	// ErrTimeout indicates an invocation exceeded the specified timeout limit.
	ErrTimeout = errors.New("library: invocation timed out")

	// ErrProcessCrashed indicates the subprocess exited abnormally or crashed.
	ErrProcessCrashed = errors.New("library: subprocess crashed or terminated unexpectedly")

	// ErrSerialization indicates an error encoding or decoding the invocation payload.
	ErrSerialization = errors.New("library: serialization error")

	// ErrUnitNotFound indicates no foreign wrapper or Go invoker is registered for the unit.
	ErrUnitNotFound = errors.New("library: unit not registered")

	// ErrInvocationFailed indicates an execution error reported by the foreign wrapper.
	ErrInvocationFailed = errors.New("library: invocation failed")
)

// InvocationError represents a structured error returned by a foreign library execution.
type InvocationError struct {
	UnitID   string
	ExitCode int
	Stderr   string
	Err      error
}

func (e *InvocationError) Error() string {
	if e.Stderr != "" {
		return fmt.Sprintf("library invocation error for unit %q (exit %d): %s: %v", e.UnitID, e.ExitCode, e.Stderr, e.Err)
	}
	return fmt.Sprintf("library invocation error for unit %q (exit %d): %v", e.UnitID, e.ExitCode, e.Err)
}

func (e *InvocationError) Unwrap() error {
	return e.Err
}
