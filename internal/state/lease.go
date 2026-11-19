package state

import (
	"time"
)

// DefaultLeaseTTL is the default duration for agent claims in .go-anything/local/.
const DefaultLeaseTTL = 30 * time.Minute

// Lease represents a transient local lock held by an agent in .go-anything/local/<unit>.lease.
type Lease struct {
	UnitID    string    `json:"unit_id"`
	Owner     string    `json:"owner"`
	ClaimedAt time.Time `json:"claimed_at"`
	ExpiresAt time.Time `json:"expires_at"`
}

// IsValid reports whether the lease is active and has not expired.
func (l *Lease) IsValid() bool {
	if l == nil {
		return false
	}
	return time.Now().UTC().Before(l.ExpiresAt)
}

// IsExpired reports whether the lease has expired.
func (l *Lease) IsExpired() bool {
	return !l.IsValid()
}
