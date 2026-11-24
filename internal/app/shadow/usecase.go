package shadow

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
)

// UseCase coordinates starting, stopping, and inspecting shadow traffic evaluations.
type UseCase struct {
	port ShadowPort
}

// New creates a new shadow UseCase.
func New(port ShadowPort) *UseCase {
	if port == nil {
		port = NewDefaultShadowPort()
	}
	return &UseCase{port: port}
}

// Run executes the shadow command action.
func (uc *UseCase) Run(ctx context.Context, in Input) (*Output, error) {
	if in.UnitID == "" {
		return nil, fmt.Errorf("unit ID is required")
	}

	dir := in.Dir
	if dir == "" {
		dir = "."
	}
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve directory path: %w", err)
	}

	action := strings.ToLower(strings.TrimSpace(in.Action))
	if action == "" {
		action = "status"
	}

	switch action {
	case "start":
		sampleRate := in.SampleRate
		if sampleRate <= 0.0 {
			sampleRate = 1.0
		} else if sampleRate > 1.0 {
			sampleRate = 1.0
		}
		return uc.port.StartShadow(ctx, absDir, in.UnitID, in.Port, sampleRate)

	case "status":
		return uc.port.GetShadowStatus(ctx, absDir, in.UnitID)

	case "stop":
		return uc.port.StopShadow(ctx, absDir, in.UnitID)

	default:
		return nil, fmt.Errorf("invalid shadow action %q (must be start, status, or stop)", in.Action)
	}
}

// Run is a convenience function.
func Run(ctx context.Context, in Input, port ...ShadowPort) (*Output, error) {
	var p ShadowPort
	if len(port) > 0 {
		p = port[0]
	}
	return New(p).Run(ctx, in)
}
