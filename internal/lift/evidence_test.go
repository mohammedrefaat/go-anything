package lift

import (
	"testing"

	"github.com/mohammedrefaat/go-anything/pkg/adapterkit"
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
			profileChecks := []string{"build", "format", "vet", "staticcheck", "race", "govulncheck", "vulncheck", "replay", "error_paths", "api_compat"}
			for _, name := range []string{"build", "format", "vet", "staticcheck", "race", "replay", "error_paths", "api_compat"} {
				checks = append(checks, adapterkit.GateCheck{Name: name, Status: adapterkit.StatusPass})
			}
			ok, reason := isEvidenceValidForProfile(&adapterkit.Evidence{
				SchemaVersion:  adapterkit.SchemaVersionV2,
				ProfileVersion: adapterkit.FullProfileVersionV201,
				ProfileChecks:  profileChecks,
				Checks:         checks,
			}, adapterkit.ProfileFull)
			assert.Equal(t, tc.want, ok, reason)
		})
	}
}

func TestFullProfileRejectsStaleMetadata(t *testing.T) {
	checks := []adapterkit.GateCheck{{Name: "govulncheck", Status: adapterkit.StatusPass}}
	for _, name := range []string{"build", "format", "vet", "staticcheck", "race", "replay", "error_paths", "api_compat"} {
		checks = append(checks, adapterkit.GateCheck{Name: name, Status: adapterkit.StatusPass})
	}
	profileChecks := []string{"build", "format", "vet", "staticcheck", "race", "govulncheck", "replay", "error_paths", "api_compat"}

	ok, _ := isEvidenceValidForProfile(&adapterkit.Evidence{
		SchemaVersion:  adapterkit.SchemaVersionV1,
		ProfileVersion: adapterkit.FullProfileVersionV201,
		ProfileChecks:  profileChecks,
		Checks:         checks,
	}, adapterkit.ProfileFull)
	assert.False(t, ok, "stale v1 schema must be rejected")

	ok, _ = isEvidenceValidForProfile(&adapterkit.Evidence{
		SchemaVersion: adapterkit.SchemaVersionV2,
		ProfileChecks: profileChecks,
		Checks:        checks,
	}, adapterkit.ProfileFull)
	assert.False(t, ok, "missing profile_version must be rejected")
}
