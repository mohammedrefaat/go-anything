package capture

import (
	"context"

	"github.com/mohammedrefat/go-anything/pkg/adapterkit"
)

// ObservabilitySource handles decoded traffic supplied by read-only observability tools
// such as platform-mcp, Datadog, Prometheus/Grafana, or external telemetry monitors.
//
// Invariant: Observability sources have STRICTLY NO control over live routing.
// Any attempt to include routing control fields (e.g. route_to, reroute, route_control)
// is rejected fail-closed with ErrRoutingControlForbidden.
type ObservabilitySource struct {
	ingester *Ingester
}

// NewObservabilitySource creates a new ObservabilitySource.
func NewObservabilitySource(ingester *Ingester) *ObservabilitySource {
	return &ObservabilitySource{
		ingester: ingester,
	}
}

// Ingest passes a single decoded traffic record to the ingester.
func (o *ObservabilitySource) Ingest(ctx context.Context, unitID string, traffic DecodedTraffic) (*adapterkit.Case, error) {
	return o.ingester.IngestDecodedTraffic(ctx, unitID, traffic)
}

// IngestBatch passes a batch of decoded traffic records to the ingester.
func (o *ObservabilitySource) IngestBatch(ctx context.Context, unitID string, records []DecodedTraffic) ([]*adapterkit.Case, error) {
	cases := make([]*adapterkit.Case, 0, len(records))
	for _, rec := range records {
		c, err := o.ingester.IngestDecodedTraffic(ctx, unitID, rec)
		if err != nil {
			return nil, err
		}
		cases = append(cases, c)
	}
	return cases, nil
}
