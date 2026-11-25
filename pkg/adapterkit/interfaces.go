package adapterkit

import (
	"context"
)

// TransportType identifies the communication channel between Core and an adapter.
type TransportType string

const (
	TransportInProcess  TransportType = "InProcess"
	TransportSubprocess TransportType = "Subprocess"
	TransportHTTP       TransportType = "HTTP"
	TransportCShared    TransportType = "CShared"
)

// ValidTransportTypes lists supported adapter transports.
var ValidTransportTypes = map[TransportType]struct{}{
	TransportInProcess:  {},
	TransportSubprocess: {},
	TransportHTTP:       {},
	TransportCShared:    {},
}

// Transport provides the underlying transport channel for communicating with adapters.
type Transport interface {
	// Type returns the active transport mechanism.
	Type() TransportType
	// Invoke sends a serialized request to the adapter and returns the response.
	Invoke(ctx context.Context, method string, payload []byte) ([]byte, error)
	// Close releases any resources associated with the transport connection or process.
	Close() error
}

// LanguageAdapter defines the transport-agnostic contract for source language adapters.
//
// Responsibilities:
//   - Parsing, AST inspection, and dependency discovery.
//   - Unit discovery and wave planning.
//   - Declaring required capabilities and field semantics.
//   - Running the original application and discovering tests.
//   - Driving replay against ported code.
//
// In accordance with Rule 8, LanguageAdapter never owns Redactor or ShadowPolicy.
type LanguageAdapter interface {
	// Name returns the identifier of the adapter (e.g. "golang", "python", "typescript").
	Name() string
	// Version returns the version of this adapter.
	Version() string
	// SourceLanguage returns the primary source language handled.
	SourceLanguage() string
	// SupportedTransports returns the list of transports supported by this adapter.
	SupportedTransports() []TransportType
	// Capabilities returns the explicit capability privileges declared by this adapter.
	Capabilities() []Capability
	// FieldSemantics returns the declared data semantics for fields produced or analyzed.
	FieldSemantics() []FieldSemantic
	// Analyze inspects a repository and generates an initial migration manifest.
	Analyze(ctx context.Context, repoPath string) (*Manifest, error)
	// DiscoverUnits discovers all candidate migration units within the repository.
	DiscoverUnits(ctx context.Context, repoPath string) ([]Unit, error)
	// CreateReplayDriver returns a replay driver configured for this adapter.
	CreateReplayDriver(ctx context.Context, unitID string) (Replay, error)
}

// ManifestProvider is an optional interface that a LanguageAdapter may implement
// to expose its static adapter manifest (including handled file extensions).
type ManifestProvider interface {
	AdapterManifest() AdapterManifest
}

// Replay defines the transport-agnostic replay execution interface.
// Replay executes recorded test cases against target ported implementations.
type Replay interface {
	// DriverName returns the identifier of the replay driver.
	DriverName() string
	// ExecuteCase replays a single behavioral case against the ported target.
	ExecuteCase(ctx context.Context, testCase *Case) (*CaseOutput, error)
	// Close terminates the replay driver and cleans up any ephemeral processes or instances.
	Close() error
}
