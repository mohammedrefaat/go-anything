package state

import "errors"

var (
	// ErrUnitNotFound indicates the requested unit does not exist in the state store.
	ErrUnitNotFound = errors.New("unit not found")
	// ErrUnitAlreadyExists indicates a unit with the given ID already exists.
	ErrUnitAlreadyExists = errors.New("unit already exists")
	// ErrInvalidTransition indicates the requested lifecycle transition is not permitted.
	ErrInvalidTransition = errors.New("invalid state transition")
	// ErrEvidenceMissing indicates required verification evidence is absent.
	ErrEvidenceMissing = errors.New("transition rejected: missing required evidence")
	// ErrEvidenceProfileUnsatisfied indicates the provided evidence does not satisfy the unit's required profile.
	ErrEvidenceProfileUnsatisfied = errors.New("transition rejected: evidence does not satisfy required profile")
	// ErrEvidenceMismatches indicates evidence contains non-zero mismatches.
	ErrEvidenceMismatches = errors.New("transition rejected: evidence contains mismatches")
	// ErrDependencyNotMet indicates an unverified dependency blocks transitioning to Ready.
	ErrDependencyNotMet = errors.New("transition rejected: unverified dependency")
	// ErrLeaseActive indicates another owner currently holds an active lease for the unit.
	ErrLeaseActive = errors.New("unit is claimed by another active lease")
	// ErrLeaseExpired indicates the lease has expired.
	ErrLeaseExpired = errors.New("lease has expired")
	// ErrLeaseOwnerMismatch indicates the operation was attempted by an actor who does not own the lease.
	ErrLeaseOwnerMismatch = errors.New("lease owner mismatch")
	// ErrFallbackCountNonZero indicates lift or completion is blocked because fallback executions are non-zero.
	ErrFallbackCountNonZero = errors.New("transition rejected: fallback count must be zero")
)
