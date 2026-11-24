package cutover

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/mohammedrefat/go-anything/pkg/adapterkit"
)

// UseCase coordinates cutover planning, route status inspection, and kill switch activation.
// Rule R-1: NEVER performs lift or alters permanent code boundaries.
type UseCase struct {
	port CutoverPort
}

// New creates a new cutover UseCase.
func New(port CutoverPort) *UseCase {
	if port == nil {
		port = NewDefaultCutoverPort()
	}
	return &UseCase{port: port}
}

// Run executes the cutover use-case.
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
	case "plan":
		return uc.plan(ctx, absDir, in.UnitID)
	case "status":
		return uc.status(ctx, absDir, in.UnitID)
	case "kill":
		return uc.kill(ctx, absDir, in.UnitID)
	default:
		return nil, fmt.Errorf("invalid cutover action %q (must be plan, status, or kill)", in.Action)
	}
}

func (uc *UseCase) plan(ctx context.Context, dir, unitID string) (*Output, error) {
	unit, err := uc.port.GetUnit(ctx, dir, unitID)
	if err != nil {
		return nil, fmt.Errorf("failed to inspect unit %q: %w", unitID, err)
	}

	killActive := uc.port.IsKillSwitchActive(ctx, dir, unitID)
	fallbacks, _ := uc.port.GetFallbackCount(ctx, dir, unitID)

	var warnings []string
	if unit.State != adapterkit.StateApproved && unit.State != adapterkit.StateVerified {
		warnings = append(warnings, fmt.Sprintf("unit %s is in state %q; cutover recommends state Approved or Verified", unitID, unit.State))
	}
	if killActive {
		warnings = append(warnings, fmt.Sprintf("kill switch is currently active for unit %s", unitID))
	}
	if fallbacks > 0 {
		warnings = append(warnings, fmt.Sprintf("unit %s has recorded %d automatic fallback events", unitID, fallbacks))
	}

	routeState := "modern"
	if killActive || (unit.State != adapterkit.StateApproved && unit.State != adapterkit.StateVerified && unit.State != adapterkit.StateCutover) {
		routeState = "original"
	}

	targetService := ""
	if unit.Metadata != nil {
		if ts, ok := unit.Metadata["target"].(string); ok {
			targetService = ts
		}
	}

	return &Output{
		SchemaVersion:    "v2",
		UnitID:           unitID,
		Action:           "plan",
		RouteState:       routeState,
		KillSwitchActive: killActive,
		TargetService:    targetService,
		FallbackCount:    fallbacks,
		Fallbacks:        fallbacks,
		Warnings:         warnings,
		Message:          fmt.Sprintf("Cutover plan evaluated for unit %s (target route: %s)", unitID, routeState),
	}, nil
}

func (uc *UseCase) status(ctx context.Context, dir, unitID string) (*Output, error) {
	unit, err := uc.port.GetUnit(ctx, dir, unitID)
	if err != nil {
		return nil, fmt.Errorf("failed to inspect unit %q: %w", unitID, err)
	}

	killActive := uc.port.IsKillSwitchActive(ctx, dir, unitID)
	fallbacks, _ := uc.port.GetFallbackCount(ctx, dir, unitID)

	routeState := "modern"
	if killActive || unit.State == adapterkit.StatePlanned || unit.State == adapterkit.StateRecorded || unit.State == adapterkit.StateReady {
		routeState = "original"
	} else if unit.State == adapterkit.StateShadow {
		routeState = "shadow"
	} else if unit.State == adapterkit.StateCutover || unit.State == adapterkit.StateCompleted {
		routeState = "cutover"
	}

	targetService := ""
	if unit.Metadata != nil {
		if ts, ok := unit.Metadata["target"].(string); ok {
			targetService = ts
		}
	}

	return &Output{
		SchemaVersion:    "v2",
		UnitID:           unitID,
		Action:           "status",
		RouteState:       routeState,
		KillSwitchActive: killActive,
		TargetService:    targetService,
		FallbackCount:    fallbacks,
		Fallbacks:        fallbacks,
		Message:          fmt.Sprintf("Unit %s is routing to %s (kill switch: %v)", unitID, routeState, killActive),
	}, nil
}

func (uc *UseCase) kill(ctx context.Context, dir, unitID string) (*Output, error) {
	if err := uc.port.ActivateKillSwitch(ctx, dir, unitID); err != nil {
		return nil, fmt.Errorf("failed to activate kill switch for %s: %w", unitID, err)
	}

	fallbacks, _ := uc.port.GetFallbackCount(ctx, dir, unitID)

	return &Output{
		SchemaVersion:    "v2",
		UnitID:           unitID,
		Action:           "kill",
		RouteState:       "original",
		KillSwitchActive: true,
		FallbackCount:    fallbacks,
		Fallbacks:        fallbacks,
		Warnings:         []string{fmt.Sprintf("emergency kill switch triggered: traffic for unit %s immediately routed back to original implementation", unitID)},
		Message:          fmt.Sprintf("Kill switch successfully engaged for unit %s", unitID),
	}, nil
}

// Run is a convenience function.
func Run(ctx context.Context, in Input, port ...CutoverPort) (*Output, error) {
	var p CutoverPort
	if len(port) > 0 {
		p = port[0]
	}
	return New(p).Run(ctx, in)
}
