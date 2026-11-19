package recorder

import (
	"testing"

	"github.com/mohammedrefat/go-anything/pkg/adapterkit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDetectVolatileFields_AlwaysWildcardsDateHeader(t *testing.T) {
	// Even when both recording runs observe the same clock second, the Date
	// header must be wildcarded so replays against a later process do not fail.
	out1 := &adapterkit.CaseOutput{
		StatusCode: 200,
		Headers: map[string]string{
			"Date":         "Thu, 19 Nov 2026 15:00:00 GMT",
			"Content-Type": "application/json",
		},
		Body: `{"ok":true}`,
	}
	out2 := &adapterkit.CaseOutput{
		StatusCode: 200,
		Headers: map[string]string{
			"Date":         "Thu, 19 Nov 2026 15:00:00 GMT",
			"Content-Type": "application/json",
		},
		Body: `{"ok":true}`,
	}

	rules := DetectVolatileFields(out1, out2)
	require.Len(t, rules, 1)
	assert.Equal(t, "headers.Date", rules[0].Path)
	assert.Equal(t, "timestamp", rules[0].Type)

	// The content type is stable and must not be wildcarded.
	for _, r := range rules {
		assert.NotEqual(t, "headers.Content-Type", r.Path)
	}
}

func TestDetectVolatileFields_Table(t *testing.T) {
	tests := []struct {
		name      string
		headers   []string
		wantPaths []string
	}{
		{
			name:      "date only",
			headers:   []string{"Date", "Content-Type", "Content-Length"},
			wantPaths: []string{"headers.Date"},
		},
		{
			name:      "request id and trace",
			headers:   []string{"X-Request-Id", "X-Trace-Id", "Traceparent", "Etag"},
			wantPaths: []string{"headers.Traceparent", "headers.X-Request-Id", "headers.X-Trace-Id"},
		},
		{
			name:      "stable headers untouched",
			headers:   []string{"Content-Type", "Content-Length", "Etag"},
			wantPaths: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			headers := make(map[string]string, len(tt.headers))
			for _, h := range tt.headers {
				headers[h] = "stable"
			}
			rules := DetectVolatileFields(
				&adapterkit.CaseOutput{Headers: headers},
				&adapterkit.CaseOutput{Headers: headers},
			)

			got := make([]string, 0, len(rules))
			for _, r := range rules {
				got = append(got, r.Path)
			}
			assert.ElementsMatch(t, tt.wantPaths, got)
		})
	}
}
