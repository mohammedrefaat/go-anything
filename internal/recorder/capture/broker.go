package capture

import (
	"context"

	"github.com/mohammedrefaat/go-anything/pkg/adapterkit"
)

// BrokerConsumerSource consumes messages from event and message brokers (e.g. Kafka, RabbitMQ, SQS, NATS).
// It operates purely as a passive observer / consumer with NO control over broker routing or message forwarding.
type BrokerConsumerSource struct {
	ingester *Ingester
}

// NewBrokerConsumerSource creates a new BrokerConsumerSource.
func NewBrokerConsumerSource(ingester *Ingester) *BrokerConsumerSource {
	return &BrokerConsumerSource{
		ingester: ingester,
	}
}

// Ingest ingests a single broker message event.
func (b *BrokerConsumerSource) Ingest(ctx context.Context, unitID string, msg BrokerMessage) (*adapterkit.Case, error) {
	return b.ingester.IngestBrokerMessage(ctx, unitID, msg)
}

// IngestBatch ingests a slice of broker messages, returning the created cases.
func (b *BrokerConsumerSource) IngestBatch(ctx context.Context, unitID string, msgs []BrokerMessage) ([]*adapterkit.Case, error) {
	cases := make([]*adapterkit.Case, 0, len(msgs))
	for _, m := range msgs {
		c, err := b.ingester.IngestBrokerMessage(ctx, unitID, m)
		if err != nil {
			return nil, err
		}
		cases = append(cases, c)
	}
	return cases, nil
}
