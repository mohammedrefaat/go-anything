package capture

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/mohammedrefat/go-anything/internal/redactor"
	"github.com/mohammedrefat/go-anything/internal/state"
	"github.com/mohammedrefat/go-anything/pkg/adapterkit"
)

// ForbiddenRoutingKeywords are keywords that indicate an attempt to exercise routing control.
// Observability tools and capture sources are strictly read-only and have NO control over routing.
var ForbiddenRoutingKeywords = []string{
	"route_to",
	"reroute",
	"route_control",
	"routing_override",
	"routing_rule",
	"routing_action",
	"traffic_split",
	"traffic_weight",
	"promote_route",
	"switch_traffic",
	"alter_route",
	"kill_switch",
	"redirect_target",
	"upstream_override",
}

// Config configures the Ingester.
type Config struct {
	WorkspaceRoot  string
	Store          *state.Store
	Sink           CaseSink
	Redactor       *redactor.Redactor
	SourceRevision string
	Runtime        string
	TimeNow        func() time.Time
}

// Ingester coordinates ingesting production traffic captures, applying fail-closed redaction,
// converting captures to adapterkit.Case records with Kind: 'captured', and persisting them.
type Ingester struct {
	cfg          Config
	mu           sync.Mutex
	unitCounters map[string]uint64
}

// NewIngester creates a new Ingester with the provided configuration.
func NewIngester(cfg Config) *Ingester {
	if cfg.WorkspaceRoot == "" {
		cfg.WorkspaceRoot = "."
	}
	if cfg.SourceRevision == "" {
		cfg.SourceRevision = "live"
	}
	if cfg.Runtime == "" {
		cfg.Runtime = "go1.22"
	}
	if cfg.Redactor == nil {
		cfg.Redactor = redactor.New()
	}
	if cfg.TimeNow == nil {
		cfg.TimeNow = func() time.Time {
			return time.Now().UTC()
		}
	}
	if cfg.Sink == nil {
		if cfg.Store != nil {
			cfg.Sink = NewFileSink(cfg.Store.CasesDir())
		} else {
			casesDir := filepath.Join(cfg.WorkspaceRoot, state.WorkspaceDirName, state.CasesDirName)
			cfg.Sink = NewFileSink(casesDir)
		}
	}

	return &Ingester{
		cfg:          cfg,
		unitCounters: make(map[string]uint64),
	}
}

// IngestHTTP processes captured gateway or sidecar HTTP mirror traffic.
func (ing *Ingester) IngestHTTP(ctx context.Context, unitID string, traffic HTTPTraffic) (*adapterkit.Case, error) {
	if unitID == "" {
		return nil, ErrEmptyUnitID
	}

	method := traffic.Request.Method
	if method == "" {
		method = "GET"
	}
	path := traffic.Request.Path
	if path == "" {
		path = "/"
	}

	input := adapterkit.CaseInput{
		Method:  strings.ToUpper(method),
		Path:    path,
		Headers: cloneStringMap(traffic.Request.Headers),
		Body:    traffic.Request.Body,
	}

	statusCode := traffic.Response.StatusCode
	if statusCode == 0 {
		statusCode = 200
	}

	output := adapterkit.CaseOutput{
		StatusCode: statusCode,
		Headers:    cloneStringMap(traffic.Response.Headers),
		Body:       traffic.Response.Body,
	}

	meta := make(map[string]any)
	if traffic.Metadata != nil {
		for k, v := range traffic.Metadata {
			meta[k] = v
		}
	}
	meta["source"] = "gateway_mirror"
	if traffic.Request.RemoteAddr != "" {
		meta["remote_addr"] = traffic.Request.RemoteAddr
	}
	if traffic.Response.DurationMS > 0 {
		meta["duration_ms"] = traffic.Response.DurationMS
	}

	return ing.processAndSave(ctx, unitID, input, output, meta)
}

// IngestBrokerMessage processes an incoming message broker consumer event.
func (ing *Ingester) IngestBrokerMessage(ctx context.Context, unitID string, msg BrokerMessage) (*adapterkit.Case, error) {
	if unitID == "" {
		return nil, ErrEmptyUnitID
	}

	path := msg.Topic
	if path == "" {
		path = "topic:default"
	} else if !strings.HasPrefix(path, "topic:") && !strings.HasPrefix(path, "queue:") {
		path = "topic:" + path
	}

	headers := cloneStringMap(msg.Headers)
	if headers == nil {
		headers = make(map[string]string)
	}
	if msg.Key != "" {
		headers["message_key"] = msg.Key
	}

	input := adapterkit.CaseInput{
		Method:  "MESSAGE",
		Path:    path,
		Headers: headers,
		Body:    msg.Payload,
	}

	statusCode := msg.StatusCode
	if statusCode == 0 {
		statusCode = 200
	}

	output := adapterkit.CaseOutput{
		StatusCode: statusCode,
		Headers:    cloneStringMap(msg.ResponseHeaders),
		Body:       msg.ResponsePayload,
	}

	meta := make(map[string]any)
	if msg.Metadata != nil {
		for k, v := range msg.Metadata {
			meta[k] = v
		}
	}
	meta["source"] = "broker_consumer"
	meta["topic"] = msg.Topic
	if msg.Key != "" {
		meta["key"] = msg.Key
	}

	return ing.processAndSave(ctx, unitID, input, output, meta)
}

// IngestTrace processes an ingested distributed trace span into a behavioral case.
func (ing *Ingester) IngestTrace(ctx context.Context, unitID string, span TraceSpan) (*adapterkit.Case, error) {
	if unitID == "" {
		return nil, ErrEmptyUnitID
	}

	var input adapterkit.CaseInput
	var output adapterkit.CaseOutput

	if span.HTTP != nil {
		method := span.HTTP.Request.Method
		if method == "" {
			method = "GET"
		}
		path := span.HTTP.Request.Path
		if path == "" {
			path = "/"
		}
		input = adapterkit.CaseInput{
			Method:  strings.ToUpper(method),
			Path:    path,
			Headers: cloneStringMap(span.HTTP.Request.Headers),
			Body:    span.HTTP.Request.Body,
		}
		statusCode := span.HTTP.Response.StatusCode
		if statusCode == 0 {
			statusCode = 200
		}
		output = adapterkit.CaseOutput{
			StatusCode: statusCode,
			Headers:    cloneStringMap(span.HTTP.Response.Headers),
			Body:       span.HTTP.Response.Body,
		}
	} else {
		// Extract from OpenTelemetry attributes
		method := getStringAttr(span.Attributes, "http.method", "http.request.method", "rpc.method")
		if method == "" {
			method = "GET"
		}
		path := getStringAttr(span.Attributes, "http.target", "http.route", "http.url", "url.path")
		if path == "" {
			path = span.Name
		}
		if path == "" {
			path = "/"
		}

		headers := extractPrefixedAttrs(span.Attributes, "http.request.header.")
		input = adapterkit.CaseInput{
			Method:  strings.ToUpper(method),
			Path:    path,
			Headers: headers,
			Body:    span.Attributes["http.request.body"],
		}

		statusCode := getIntAttr(span.Attributes, "http.status_code", "http.response.status_code", "rpc.grpc.status_code")
		if statusCode == 0 {
			statusCode = 200
		}

		respHeaders := extractPrefixedAttrs(span.Attributes, "http.response.header.")
		output = adapterkit.CaseOutput{
			StatusCode: statusCode,
			Headers:    respHeaders,
			Body:       span.Attributes["http.response.body"],
		}
	}

	meta := make(map[string]any)
	if span.Metadata != nil {
		for k, v := range span.Metadata {
			meta[k] = v
		}
	}
	meta["source"] = "distributed_trace"
	if span.TraceID != "" {
		meta["trace_id"] = span.TraceID
	}
	if span.SpanID != "" {
		meta["span_id"] = span.SpanID
	}
	if span.ParentSpanID != "" {
		meta["parent_span_id"] = span.ParentSpanID
	}
	if span.Name != "" {
		meta["operation"] = span.Name
	}
	if span.Kind != "" {
		meta["span_kind"] = span.Kind
	}

	return ing.processAndSave(ctx, unitID, input, output, meta)
}

// IngestDecodedTraffic ingests decoded traffic from read-only observability tools such as platform-mcp.
// Observability tools have NO control over routing (read-only ingestion only).
func (ing *Ingester) IngestDecodedTraffic(ctx context.Context, unitID string, decoded DecodedTraffic) (*adapterkit.Case, error) {
	if unitID == "" {
		return nil, ErrEmptyUnitID
	}

	source := decoded.Source
	if source == "" {
		source = "platform-mcp"
	}

	var input adapterkit.CaseInput
	switch inVal := decoded.Input.(type) {
	case HTTPRequest:
		input = adapterkit.CaseInput{
			Method:  inVal.Method,
			Path:    inVal.Path,
			Headers: cloneStringMap(inVal.Headers),
			Body:    inVal.Body,
		}
	case map[string]any:
		// Check for method/path/headers
		method, _ := inVal["method"].(string)
		path, _ := inVal["path"].(string)
		if path == "" {
			path = decoded.Operation
		}
		if path == "" {
			path = "/"
		}
		headersMap, _ := inVal["headers"].(map[string]any)
		headers := make(map[string]string)
		for k, v := range headersMap {
			if s, ok := v.(string); ok {
				headers[k] = s
			}
		}
		input = adapterkit.CaseInput{
			Method:  method,
			Path:    path,
			Headers: headers,
			Body:    inVal["body"],
		}
		if input.Body == nil && len(headers) == 0 && method == "" {
			input.Body = inVal
		}
	default:
		path := decoded.Operation
		if path == "" {
			path = "/"
		}
		input = adapterkit.CaseInput{
			Path: path,
			Body: decoded.Input,
		}
	}

	var output adapterkit.CaseOutput
	switch outVal := decoded.Output.(type) {
	case HTTPResponse:
		statusCode := outVal.StatusCode
		if statusCode == 0 {
			statusCode = 200
		}
		output = adapterkit.CaseOutput{
			StatusCode: statusCode,
			Headers:    cloneStringMap(outVal.Headers),
			Body:       outVal.Body,
		}
	case map[string]any:
		statusCode := 200
		if sc, ok := outVal["status_code"].(float64); ok {
			statusCode = int(sc)
		} else if sc, ok := outVal["status_code"].(int); ok {
			statusCode = sc
		}
		headersMap, _ := outVal["headers"].(map[string]any)
		headers := make(map[string]string)
		for k, v := range headersMap {
			if s, ok := v.(string); ok {
				headers[k] = s
			}
		}
		output = adapterkit.CaseOutput{
			StatusCode: statusCode,
			Headers:    headers,
			Body:       outVal["body"],
		}
		if output.Body == nil && len(headers) == 0 && outVal["status_code"] == nil {
			output.Body = outVal
		}
	default:
		output = adapterkit.CaseOutput{
			StatusCode: 200,
			Body:       decoded.Output,
		}
	}

	meta := make(map[string]any)
	if decoded.Metadata != nil {
		for k, v := range decoded.Metadata {
			meta[k] = v
		}
	}
	meta["source"] = source
	if decoded.Protocol != "" {
		meta["protocol"] = decoded.Protocol
	}
	if decoded.Operation != "" {
		meta["operation"] = decoded.Operation
	}

	return ing.processAndSave(ctx, unitID, input, output, meta)
}

// processAndSave validates security invariants, checks routing controls, redacts in memory,
// and persists the case.
func (ing *Ingester) processAndSave(
	ctx context.Context,
	unitID string,
	input adapterkit.CaseInput,
	output adapterkit.CaseOutput,
	meta map[string]any,
) (*adapterkit.Case, error) {
	// 1. Verify observability/capture payload does not attempt routing control
	if err := validateNoRoutingControl(meta); err != nil {
		return nil, err
	}

	// 2. Generate deterministic case ID
	caseID := ing.nextCaseID(unitID)

	// 3. Assemble adapterkit.Case
	c := &adapterkit.Case{
		SchemaVersion:  adapterkit.SchemaVersionV1Alpha1,
		CaseID:         caseID,
		UnitID:         unitID,
		Kind:           adapterkit.KindCaptured,
		SourceRevision: ing.cfg.SourceRevision,
		Runtime:        ing.cfg.Runtime,
		CapturedAt:     ing.cfg.TimeNow().UTC().Format(time.RFC3339),
		Input:          input,
		ExpectedOutput: output,
		Metadata:       meta,
	}

	// 4. Fail-closed in-memory redaction (ADR / Execution Rule 3)
	// Secrets must be substituted before any disk write.
	redactedCase, err := ing.cfg.Redactor.RedactCase(c)
	if err != nil {
		return nil, fmt.Errorf("fail-closed redaction failed: %w", err)
	}

	// 5. Persist to sink
	if err := ing.cfg.Sink.SaveCase(unitID, redactedCase); err != nil {
		return nil, fmt.Errorf("failed to save captured case: %w", err)
	}

	return redactedCase, nil
}

// validateNoRoutingControl checks that no metadata fields contain routing control instructions.
func validateNoRoutingControl(meta map[string]any) error {
	if meta == nil {
		return nil
	}
	for k := range meta {
		lower := strings.ToLower(k)
		for _, kw := range ForbiddenRoutingKeywords {
			if strings.Contains(lower, kw) {
				return fmt.Errorf("%w: forbidden key '%s' found", ErrRoutingControlForbidden, k)
			}
		}
	}
	return nil
}

// nextCaseID allocates a deterministic sequence ID for the unit.
func (ing *Ingester) nextCaseID(unitID string) string {
	ing.mu.Lock()
	defer ing.mu.Unlock()

	if _, exists := ing.unitCounters[unitID]; !exists {
		ing.unitCounters[unitID] = ing.initCounter(unitID)
	}

	ing.unitCounters[unitID]++
	return fmt.Sprintf("case-%s-cap-%04d", unitID, ing.unitCounters[unitID])
}

func (ing *Ingester) initCounter(unitID string) uint64 {
	// If FileSink is used, count existing lines in cases file
	if fs, ok := ing.cfg.Sink.(*FileSink); ok {
		caseFile := filepath.Join(fs.casesDir, filepath.FromSlash(unitID)+".jsonl")
		if data, err := os.ReadFile(caseFile); err == nil {
			var count uint64
			for _, line := range bytes.Split(data, []byte{'\n'}) {
				if len(bytes.TrimSpace(line)) > 0 {
					count++
				}
			}
			return count
		}
	}
	if ms, ok := ing.cfg.Sink.(*MemorySink); ok {
		return uint64(len(ms.CasesForUnit(unitID)))
	}
	return 0
}

func cloneStringMap(m map[string]string) map[string]string {
	if m == nil {
		return nil
	}
	res := make(map[string]string, len(m))
	for k, v := range m {
		res[k] = v
	}
	return res
}

func getStringAttr(attrs map[string]any, keys ...string) string {
	if attrs == nil {
		return ""
	}
	for _, k := range keys {
		if v, ok := attrs[k]; ok {
			if s, ok := v.(string); ok && s != "" {
				return s
			}
		}
	}
	return ""
}

func getIntAttr(attrs map[string]any, keys ...string) int {
	if attrs == nil {
		return 0
	}
	for _, k := range keys {
		if v, ok := attrs[k]; ok {
			switch val := v.(type) {
			case int:
				return val
			case int64:
				return int(val)
			case float64:
				return int(val)
			}
		}
	}
	return 0
}

func extractPrefixedAttrs(attrs map[string]any, prefix string) map[string]string {
	if attrs == nil {
		return nil
	}
	var res map[string]string
	for k, v := range attrs {
		if strings.HasPrefix(strings.ToLower(k), prefix) {
			if res == nil {
				res = make(map[string]string)
			}
			headerKey := k[len(prefix):]
			if s, ok := v.(string); ok {
				res[headerKey] = s
			} else {
				res[headerKey] = fmt.Sprint(v)
			}
		}
	}
	return res
}
