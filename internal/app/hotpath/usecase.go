package hotpath

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"github.com/mohammedrefaat/go-anything/pkg/adapterkit"
)

// UseCase coordinates the end-to-end guided hot-path migration pipeline.
type UseCase struct {
	port HotpathPort
}

// New creates a new hotpath UseCase.
func New(port HotpathPort) *UseCase {
	if port == nil {
		port = NewDefaultHotpathPort()
	}
	return &UseCase{port: port}
}

// Run executes the hot-path porting pipeline up to the human boundary or implementation pause.
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

	unit, err := uc.port.GetUnit(absDir, in.UnitID)
	if err != nil {
		return nil, fmt.Errorf("failed to load unit state: %w", err)
	}

	if unit.Metadata == nil {
		unit.Metadata = make(map[string]any)
	}
	unit.Metadata["kind"] = "hotpath"

	pipelineState, _ := unit.Metadata["pipeline"].(map[string]any)
	if pipelineState == nil {
		pipelineState = make(map[string]any)
		unit.Metadata["pipeline"] = pipelineState
	}

	fn := in.Function
	if fn == "" {
		if storedFn, ok := unit.Metadata["hotpath_function"].(string); ok && storedFn != "" {
			fn = storedFn
		}
	}

	out := &Output{
		SchemaVersion: "v2",
		UnitID:        in.UnitID,
		Function:      fn,
		UnitState:     string(unit.State),
		Status:        PipelineStatusInProgress,
		Steps:         make([]StepResult, 0, 8),
		Timestamp:     time.Now().UTC().Format(time.RFC3339),
	}

	isStepDone := func(stepName string) bool {
		if in.Force {
			return false
		}
		status, ok := pipelineState[stepName].(string)
		return ok && status == string(StepStatusCompleted)
	}

	markStepDone := func(stepName string, summary string, artifacts []string) {
		pipelineState[stepName] = string(StepStatusCompleted)
		out.Steps = append(out.Steps, StepResult{
			Name:      stepName,
			Status:    StepStatusCompleted,
			Summary:   summary,
			Artifacts: artifacts,
		})
	}

	// ---------------------------------------------------------
	// Step 1: Target Identification
	// ---------------------------------------------------------
	out.CurrentStep = StepTarget
	if isStepDone(StepTarget) && fn != "" {
		out.Steps = append(out.Steps, StepResult{
			Name:    StepTarget,
			Status:  StepStatusSkipped,
			Summary: fmt.Sprintf("Target function %q already selected", fn),
		})
	} else {
		selectedFn, err := uc.port.DetectOrSelectFunction(ctx, absDir, in.UnitID, fn)
		if err != nil {
			out.Status = PipelineStatusFailed
			out.Steps = append(out.Steps, StepResult{
				Name:    StepTarget,
				Status:  StepStatusFailed,
				Summary: fmt.Sprintf("Failed to select target function: %v", err),
			})
			return out, fmt.Errorf("target selection failed: %w", err)
		}
		fn = selectedFn
		out.Function = fn
		unit.Metadata["hotpath_function"] = fn
		if unit.State == "" || unit.State == adapterkit.StatePlanned {
			unit.State = adapterkit.StatePlanned
		}
		markStepDone(StepTarget, fmt.Sprintf("Identified hot-path target function: %s", fn), nil)
		_ = uc.port.SaveUnit(absDir, unit)
	}

	// ---------------------------------------------------------
	// Step 2: Record Cases
	// ---------------------------------------------------------
	out.CurrentStep = StepRecord
	if isStepDone(StepRecord) {
		out.Steps = append(out.Steps, StepResult{
			Name:    StepRecord,
			Status:  StepStatusSkipped,
			Summary: "Behavioral test cases already recorded",
		})
	} else {
		caseCount, err := uc.port.EnsureCases(ctx, absDir, in.UnitID, fn)
		if err != nil {
			out.Status = PipelineStatusFailed
			out.Steps = append(out.Steps, StepResult{
				Name:    StepRecord,
				Status:  StepStatusFailed,
				Summary: fmt.Sprintf("Failed to record behavioral cases: %v", err),
			})
			return out, fmt.Errorf("recording cases failed: %w", err)
		}
		if unit.State == adapterkit.StatePlanned {
			unit.State = adapterkit.StateRecorded
		}
		markStepDone(StepRecord, fmt.Sprintf("Verified %d behavioral test cases", caseCount), []string{
			filepath.Join(".go-anything", "cases", in.UnitID+".jsonl"),
		})
		_ = uc.port.SaveUnit(absDir, unit)
	}

	// ---------------------------------------------------------
	// Step 3: Scaffold Bridge
	// ---------------------------------------------------------
	out.CurrentStep = StepBridge
	if isStepDone(StepBridge) {
		out.Steps = append(out.Steps, StepResult{
			Name:    StepBridge,
			Status:  StepStatusSkipped,
			Summary: "Bridge sidecar scaffold already generated",
		})
	} else {
		generated, err := uc.port.ScaffoldBridge(ctx, absDir, in.UnitID, fn)
		if err != nil {
			out.Status = PipelineStatusFailed
			out.Steps = append(out.Steps, StepResult{
				Name:    StepBridge,
				Status:  StepStatusFailed,
				Summary: fmt.Sprintf("Bridge generation failed: %v", err),
			})
			return out, fmt.Errorf("bridge scaffold failed: %w", err)
		}
		if unit.State == adapterkit.StateRecorded {
			unit.State = adapterkit.StateReady
		}
		markStepDone(StepBridge, fmt.Sprintf("Scaffolded %d bridge files (sidecar + wrappers)", len(generated)), generated)
		_ = uc.port.SaveUnit(absDir, unit)
	}

	// ---------------------------------------------------------
	// Step 4: Check Developer Go Implementation (Rule R-2)
	// ---------------------------------------------------------
	out.CurrentStep = StepImplement
	implemented, checkSummary, err := uc.port.CheckImplementation(ctx, absDir, in.UnitID)
	if err != nil {
		out.Status = PipelineStatusFailed
		out.Steps = append(out.Steps, StepResult{
			Name:    StepImplement,
			Status:  StepStatusFailed,
			Summary: err.Error(),
		})
		return out, err
	}

	if !implemented {
		// Pause pipeline cleanly for developer implementation (Rule R-2)
		out.Status = PipelineStatusPausedForImplementation
		out.Steps = append(out.Steps, StepResult{
			Name:    StepImplement,
			Status:  StepStatusWaiting,
			Summary: checkSummary,
		})
		out.HumanActionRequired = fmt.Sprintf("Implement Go port in bridges/%s/handlers_impl.go, then rerun 'go-anything go --unit %s'", in.UnitID, in.UnitID)
		_ = uc.port.SaveUnit(absDir, unit)
		return out, nil
	}

	unit.State = adapterkit.StateInProgress
	markStepDone(StepImplement, checkSummary, []string{
		filepath.Join("bridges", in.UnitID, "handlers_impl.go"),
	})
	_ = uc.port.SaveUnit(absDir, unit)

	// ---------------------------------------------------------
	// Step 5: Replay and Diff
	// ---------------------------------------------------------
	out.CurrentStep = StepReplayDiff
	if isStepDone(StepReplayDiff) {
		out.Steps = append(out.Steps, StepResult{
			Name:    StepReplayDiff,
			Status:  StepStatusSkipped,
			Summary: "Replay and diff verification already passed",
		})
	} else {
		pass, diffSummary, err := uc.port.ReplayAndDiff(ctx, absDir, in.UnitID)
		if err != nil || !pass {
			out.Status = PipelineStatusFailed
			out.Steps = append(out.Steps, StepResult{
				Name:    StepReplayDiff,
				Status:  StepStatusFailed,
				Summary: fmt.Sprintf("Diff check failed: %s (err: %v)", diffSummary, err),
			})
			return out, fmt.Errorf("replay and diff failed: %s", diffSummary)
		}
		markStepDone(StepReplayDiff, diffSummary, nil)
		_ = uc.port.SaveUnit(absDir, unit)
	}

	// ---------------------------------------------------------
	// Step 6: Fuzz and Bench
	// ---------------------------------------------------------
	out.CurrentStep = StepFuzzBench
	if isStepDone(StepFuzzBench) {
		out.Steps = append(out.Steps, StepResult{
			Name:    StepFuzzBench,
			Status:  StepStatusSkipped,
			Summary: "Differential fuzzing and benchmarks already passed",
		})
	} else {
		pass, fbSummary, err := uc.port.FuzzAndBench(ctx, absDir, in.UnitID)
		if err != nil || !pass {
			out.Status = PipelineStatusFailed
			out.Steps = append(out.Steps, StepResult{
				Name:    StepFuzzBench,
				Status:  StepStatusFailed,
				Summary: fmt.Sprintf("Fuzz/bench failed: %s (err: %v)", fbSummary, err),
			})
			return out, fmt.Errorf("fuzz/bench failed: %s", fbSummary)
		}
		markStepDone(StepFuzzBench, fbSummary, nil)
		_ = uc.port.SaveUnit(absDir, unit)
	}

	// ---------------------------------------------------------
	// Step 7: Quality Gate
	// ---------------------------------------------------------
	out.CurrentStep = StepGate
	if isStepDone(StepGate) {
		out.Steps = append(out.Steps, StepResult{
			Name:    StepGate,
			Status:  StepStatusSkipped,
			Summary: "Quality Gate checks already passed",
		})
	} else {
		pass, gateSummary, err := uc.port.RunGate(ctx, absDir, in.UnitID)
		if err != nil || !pass {
			out.Status = PipelineStatusFailed
			out.Steps = append(out.Steps, StepResult{
				Name:    StepGate,
				Status:  StepStatusFailed,
				Summary: fmt.Sprintf("Quality Gate failed: %s (err: %v)", gateSummary, err),
			})
			return out, fmt.Errorf("gate failed: %s", gateSummary)
		}
		unit.State = adapterkit.StateVerified
		markStepDone(StepGate, gateSummary, nil)
		_ = uc.port.SaveUnit(absDir, unit)
	}

	// ---------------------------------------------------------
	// Step 8: Shadow Mode
	// ---------------------------------------------------------
	out.CurrentStep = StepShadow
	if isStepDone(StepShadow) {
		out.Steps = append(out.Steps, StepResult{
			Name:    StepShadow,
			Status:  StepStatusSkipped,
			Summary: "Shadow mode bridge already active",
		})
	} else {
		if err := uc.port.EnableShadow(ctx, absDir, in.UnitID); err != nil {
			out.Status = PipelineStatusFailed
			out.Steps = append(out.Steps, StepResult{
				Name:    StepShadow,
				Status:  StepStatusFailed,
				Summary: fmt.Sprintf("Failed to enable shadow mode: %v", err),
			})
			return out, fmt.Errorf("shadow activation failed: %w", err)
		}
		unit.State = adapterkit.StateShadow
		markStepDone(StepShadow, "Shadow mode enabled in bridge mode (idempotent traffic)", nil)
		_ = uc.port.SaveUnit(absDir, unit)
	}

	// ---------------------------------------------------------
	// Step 9: Human Boundary Stop (Rule R-1)
	// ---------------------------------------------------------
	out.Status = PipelineStatusReadyForApproval
	out.UnitState = string(unit.State)
	out.HumanActionRequired = fmt.Sprintf("go-anything approve --unit %s", in.UnitID)

	return out, nil
}
