package state

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/mohammedrefat/go-anything/pkg/adapterkit"
)

var shaRegex = regexp.MustCompile(`^[0-9a-fA-F]{40}$`)

// TransitionOptions carries required metadata and evidence for a state machine transition.
type TransitionOptions struct {
	Actor      string
	LeaseTTL   time.Duration
	Evidence   *adapterkit.Evidence
	SkipShadow bool
	Reason     string
}

// Transition moves a unit to targetState enforcing all guards and evidence profile requirements.
func (s *Store) Transition(unitID string, target adapterkit.UnitState, opts TransitionOptions) (*adapterkit.Unit, error) {
	unit, err := s.GetUnit(unitID)
	if err != nil {
		return nil, err
	}

	from := unit.State

	// Verify if the transition is permitted by lifecycle guards
	switch {
	case from == adapterkit.StatePlanned && target == adapterkit.StateRecorded:
		// Transition Planned -> Recorded

	case from == adapterkit.StateRecorded && target == adapterkit.StateReady:
		// Guard: All dependencies must be in a verified/completed state
		if err := s.checkDependenciesMet(unit); err != nil {
			return nil, err
		}

	case from == adapterkit.StateReady && target == adapterkit.StateInProgress:
		// Guard: Must acquire active lease
		if opts.Actor == "" {
			return nil, fmt.Errorf("transition to In progress requires non-empty Actor")
		}
		if _, err := s.AcquireLease(unit.ID, opts.Actor, opts.LeaseTTL); err != nil {
			return nil, err
		}

	case from == adapterkit.StateInProgress && target == adapterkit.StateReady:
		// Voluntary release or expiration returning to Ready
		_ = s.ReleaseLease(unit.ID, opts.Actor)

	case from == adapterkit.StateInProgress && target == adapterkit.StateBlocked:
		if opts.Reason == "" {
			return nil, fmt.Errorf("transition to Blocked requires a Reason")
		}
		if unit.Metadata == nil {
			unit.Metadata = make(map[string]any)
		}
		unit.Metadata["blocked_reason"] = opts.Reason
		_ = s.ReleaseLease(unit.ID, opts.Actor)

	case from == adapterkit.StateBlocked && target == adapterkit.StateReady:
		if unit.Metadata != nil {
			delete(unit.Metadata, "blocked_reason")
		}

	case from == adapterkit.StateBlocked && target == adapterkit.StateInProgress:
		if opts.Actor == "" {
			return nil, fmt.Errorf("transition to In progress requires non-empty Actor")
		}
		if _, err := s.AcquireLease(unit.ID, opts.Actor, opts.LeaseTTL); err != nil {
			return nil, err
		}
		if unit.Metadata != nil {
			delete(unit.Metadata, "blocked_reason")
		}

	case from == adapterkit.StateInProgress && target == adapterkit.StateVerified:
		// Guard: Must validate evidence and evidence profile
		if err := validateEvidenceForUnit(unit, opts.Evidence); err != nil {
			return nil, err
		}
		// Persist the verified evidence
		if err := s.SaveEvidence(opts.Evidence); err != nil {
			return nil, fmt.Errorf("failed to save evidence: %w", err)
		}
		_ = s.ReleaseLease(unit.ID, opts.Actor)

	case from == adapterkit.StateVerified && target == adapterkit.StateInProgress:
		// Re-opening verified unit for additional changes
		if opts.Actor == "" {
			return nil, fmt.Errorf("re-opening to In progress requires non-empty Actor")
		}
		if _, err := s.AcquireLease(unit.ID, opts.Actor, opts.LeaseTTL); err != nil {
			return nil, err
		}

	case from == adapterkit.StateVerified && target == adapterkit.StateShadow:
		// Enabling shadow mode

	case from == adapterkit.StateShadow && target == adapterkit.StateApproved:
		// Human approval following shadow validation
		if opts.Actor == "" {
			return nil, fmt.Errorf("transition to Approved requires an approving Actor")
		}

	case from == adapterkit.StateVerified && target == adapterkit.StateApproved:
		// Skipping shadow requires explicit SkipShadow flag
		if !opts.SkipShadow {
			return nil, fmt.Errorf("%w: skipping Shadow state requires SkipShadow=true (--skip-shadow)", ErrInvalidTransition)
		}
		if opts.Actor == "" {
			return nil, fmt.Errorf("transition to Approved requires an approving Actor")
		}

	case from == adapterkit.StateApproved && target == adapterkit.StateCutover:
		// Route cutover

	case from == adapterkit.StateCutover && target == adapterkit.StateCompleted:
		// Guard: Fallback count must be zero
		if unit.FallbackCount > 0 {
			return nil, fmt.Errorf("%w: fallback count is %d (must be 0)", ErrFallbackCountNonZero, unit.FallbackCount)
		}

	default:
		return nil, fmt.Errorf("%w: cannot transition from %q to %q", ErrInvalidTransition, from, target)
	}

	unit.State = target
	if err := s.SaveUnit(unit); err != nil {
		return nil, err
	}

	return unit, nil
}

func (s *Store) checkDependenciesMet(unit *adapterkit.Unit) error {
	for _, depID := range unit.Dependencies {
		dep, err := s.GetUnit(depID)
		if err != nil {
			return fmt.Errorf("%w: dependency %s not found", ErrDependencyNotMet, depID)
		}
		switch dep.State {
		case adapterkit.StateVerified, adapterkit.StateShadow, adapterkit.StateApproved, adapterkit.StateCutover, adapterkit.StateCompleted:
			// OK
		default:
			return fmt.Errorf("%w: dependency %s is in state %q (must be Verified or later)", ErrDependencyNotMet, depID, dep.State)
		}
	}
	return nil
}

func validateEvidenceForUnit(unit *adapterkit.Unit, ev *adapterkit.Evidence) error {
	if ev == nil {
		return ErrEvidenceMissing
	}
	if ev.UnitID != unit.ID {
		return fmt.Errorf("%w: evidence unit ID %q does not match %q", ErrEvidenceMissing, ev.UnitID, unit.ID)
	}
	if !shaRegex.MatchString(ev.CommitSHA) {
		return fmt.Errorf("%w: commit_sha must be a 40-character hex string, got %q", ErrEvidenceMissing, ev.CommitSHA)
	}
	if ev.MismatchesCount != 0 {
		return fmt.Errorf("%w: mismatches count is %d (must be 0)", ErrEvidenceMismatches, ev.MismatchesCount)
	}

	// Profile check
	profile := unit.EvidenceProfile
	if profile == "" {
		profile = adapterkit.ProfileFull
	}

	checksMap := make(map[string]adapterkit.ReportStatus, len(ev.Checks))
	for _, c := range ev.Checks {
		checksMap[strings.ToLower(c.Name)] = c.Status
	}

	switch profile {
	case adapterkit.ProfileReplay:
		// Replay profile requires replay check to be pass
		status, ok := checksMap["replay"]
		if !ok || status != adapterkit.StatusPass {
			return fmt.Errorf("%w: profile 'replay' requires check 'replay' with pass status", ErrEvidenceProfileUnsatisfied)
		}

	case adapterkit.ProfileFull:
		// Full profile requires all quality gate checks: toolchain checks, replay, error_paths, api_compat
		requiredChecks := []string{"build", "format", "vet", "race", "replay", "error_paths", "api_compat"}
		for _, req := range requiredChecks {
			status, ok := checksMap[req]
			if !ok {
				return fmt.Errorf("%w: profile 'full' requires check %q", ErrEvidenceProfileUnsatisfied, req)
			}
			if status != adapterkit.StatusPass {
				return fmt.Errorf("%w: profile 'full' check %q has status %q (must be pass)", ErrEvidenceProfileUnsatisfied, req, status)
			}
		}

		// Vulnerability check: govulncheck or vulncheck
		if checksMap["govulncheck"] != adapterkit.StatusPass && checksMap["vulncheck"] != adapterkit.StatusPass {
			return fmt.Errorf("%w: profile 'full' requires a passing vulnerability check ('govulncheck' or 'vulncheck')", ErrEvidenceProfileUnsatisfied)
		}

		// Fail-closed: any failed checks in evidence are rejected
		for _, c := range ev.Checks {
			if c.Status == adapterkit.StatusFail {
				return fmt.Errorf("%w: gate check %q failed", ErrEvidenceProfileUnsatisfied, c.Name)
			}
		}

	default:
		return fmt.Errorf("unknown evidence profile: %q", profile)
	}

	return nil
}
