package gate_test

import (
	"slices"
	"testing"

	"github.com/mohammedrefaat/go-anything/internal/gate"
	"github.com/stretchr/testify/assert"
)

func TestFullProfileChecks(t *testing.T) {
	full := gate.FullProfileChecks()
	all := gate.AllChecks()

	tests := []struct {
		name     string
		check    string
		expected bool
	}{
		{name: "includes build", check: gate.CheckBuild, expected: true},
		{name: "includes format", check: gate.CheckFormat, expected: true},
		{name: "includes vet", check: gate.CheckVet, expected: true},
		{name: "includes staticcheck", check: gate.CheckStaticcheck, expected: true},
		{name: "includes race", check: gate.CheckRace, expected: true},
		{name: "includes govulncheck", check: gate.CheckGovulncheck, expected: true},
		{name: "includes replay", check: gate.CheckReplay, expected: true},
		{name: "includes error_paths", check: gate.CheckErrorPaths, expected: true},
		{name: "includes api_compat", check: gate.CheckAPICompat, expected: true},
		{name: "excludes golangci-lint (deferred)", check: gate.CheckGolangciLint, expected: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, slices.Contains(full, tc.check))
		})
	}

	t.Run("full profile is not weaker than AllChecks except deferred golangci-lint", func(t *testing.T) {
		for _, c := range all {
			if c == gate.CheckGolangciLint {
				continue
			}
			assert.True(t, slices.Contains(full, c), "FullProfileChecks must include %q from AllChecks", c)
		}
	})
}
