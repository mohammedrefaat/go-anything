package main

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubPatterns matches placeholder messages indicating an un-wired or stubbed CLI command.
var stubPatterns = regexp.MustCompile(`(?i)(will be wired|not implemented|TODO)`)

// TestAllRegisteredCommandsHaveHandlers verifies that every single registered CLI command
// has an active handler and does not return any stub or placeholder messages.
func TestAllRegisteredCommandsHaveHandlers(t *testing.T) {
	commands := []string{
		"init",
		"modernize",
		"status",
		"doctor",
		"analyze",
		"record",
		"replay",
		"verify",
		"approve",
		"promote",
		"diff",
		"fuzz",
		"bench",
		"profile",
		"bridge",
		"go",
		"enable",
		"disable",
		"refresh",
		"clean",
		"report",
		"map",
		"gate",
		"shadow",
		"cutover",
		"lift",
		"migrate",
		"template",
		"setup",
		"version",
		"help",
	}

	for _, cmd := range commands {
		t.Run(cmd, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			err := runCLI([]string{cmd, "--help"}, &stdout, &stderr)
			require.NoError(t, err, "command %q --help returned unexpected error: %v", cmd, err)

			combined := stdout.String() + "\n" + stderr.String()

			if match := stubPatterns.FindString(combined); match != "" {
				t.Fatalf("command %q contains stub placeholder matching %q. Output:\n%s", cmd, match, combined)
			}

			// Ensure help/version output was actually produced on stdout or stderr
			assert.NotEmpty(t, strings.TrimSpace(combined), "command %q produced empty output on --help", cmd)
		})
	}
}

// TestStubRegexDetection verifies that the guard regex accurately catches dummy stubs.
func TestStubRegexDetection(t *testing.T) {
	stubSamples := []string{
		"command 'diff' is registered and will be wired in milestone M1-M4.",
		"ERROR: command not implemented yet",
		"TODO: implement this handler",
		"Will Be Wired in next release",
		"Feature is NOT IMPLEMENTED",
	}

	for _, sample := range stubSamples {
		assert.True(t, stubPatterns.MatchString(sample), "expected stubPatterns to match %q", sample)
	}

	cleanSamples := []string{
		"Usage: go-anything analyze [flags]",
		"Execute concurrent deterministic replay of recorded cases",
		"Manage and inspect shadow-mode traffic mirroring",
		"Exit Codes:\n  0   Success / OK",
	}

	for _, sample := range cleanSamples {
		assert.False(t, stubPatterns.MatchString(sample), "expected clean sample %q not to match stubPatterns", sample)
	}
}

// CheckCommandOutputForStubs is a reusable validation helper that fails if text contains stub keywords.
func CheckCommandOutputForStubs(output string) error {
	if match := stubPatterns.FindString(output); match != "" {
		return fmt.Errorf("stub placeholder %q detected in command output: %s", match, output)
	}
	return nil
}
