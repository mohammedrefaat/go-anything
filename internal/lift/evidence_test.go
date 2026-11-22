package lift

import (
	"testing"

	"github.com/mohammedrefat/go-anything/pkg/adapterkit"
	"github.com/stretchr/testify/assert"
)

func TestFullProfileVulnerabilityAliases(t *testing.T) {
	for _, tc := range []struct {
		name           string
		primary, alias adapterkit.ReportStatus
		want           bool
	}{
		{"passing primary", adapterkit.StatusPass, adapterkit.StatusSkipped, true},
		{"passing alias", adapterkit.StatusSkipped, adapterkit.StatusPass, true},
		{"both skipped", adapterkit.StatusSkipped, adapterkit.StatusSkipped, false},
		{"explicit failure", adapterkit.StatusFail, adapterkit.StatusPass, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			checks := []adapterkit.GateCheck{
				{Name: "govulncheck", Status: tc.primary},
				{Name: "vulncheck", Status: tc.alias},
			}
			for _, name := range []string{"build", "format", "vet", "race", "replay", "error_paths", "api_compat"} {
				checks = append(checks, adapterkit.GateCheck{Name: name, Status: adapterkit.StatusPass})
			}
			ok, reason := isEvidenceValidForProfile(&adapterkit.Evidence{Checks: checks}, adapterkit.ProfileFull)
			assert.Equal(t, tc.want, ok, reason)
		})
	}
}
