package capture

import (
	"context"

	"github.com/mohammedrefat/go-anything/pkg/adapterkit"
)

// TraceSource ingests distributed trace spans (e.g. OpenTelemetry, Jaeger, Zipkin, W3C).
// It acts solely as a read-only consumer extracting behavioral inputs and outputs without
// interfering with trace context propagation, telemetry collection, or service routing.
type TraceSource struct {
	ingester *Ingester
}

// NewTraceSource creates a new TraceSource backed by an Ingester.
func NewTraceSource(ingester *Ingester) *TraceSource {
	return &TraceSource{
		ingester: ingester,
	}
}

// IngestSpan converts a trace span into a redacted captured case.
func (t *TraceSource) IngestSpan(ctx context.Context, unitID string, span TraceSpan) (*adapterkit.Case, error) {
	return t.ingester.IngestTrace(ctx, unitID, span)
}

// IngestSpans processes a batch of trace spans for a given unit.
func (t *TraceSource) IngestSpans(ctx context.Context, unitID string, spans []TraceSpan) ([]*adapterkit.Case, error) {
	cases := make([]*adapterkit.Case, 0, len(spans))
	for _, s := range spans {
		c, err := t.ingester.IngestTrace(ctx, unitID, s)
		if err != nil {
			return nil, err
		}
		cases = append(cases, c)
	}
	return cases, nil
}
