package telemetry

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

// OpenTelemetry instrumentation of the DWYT pipeline (spec §54).
//
// This is a deliberate implementation choice worth stating plainly: DWYT emits
// OTLP directly over HTTP/JSON instead of vendoring the OpenTelemetry Go SDK.
//
// Why: the SDK plus an OTLP exporter pulls in a large dependency tree into a
// binary whose job is to *reduce* overhead, and DWYT needs exactly one thing from
// it — spans with attributes on a local collector. OTLP/HTTP with a JSON body is
// a first-class part of the OTLP specification, so a collector accepts these
// spans like any other client's. If DWYT ever needs metrics, baggage or
// context propagation across processes, that is the point to adopt the SDK.
//
// Behaviour when unconfigured: every function here is a no-op. Tracing must never
// be a reason a request fails or slows down, so a missing endpoint, an
// unreachable collector or a serialization error is swallowed.

// SpanName values are the pipeline spans required by spec §54.
const (
	SpanRequest            = "dwyt.request"
	SpanClassification     = "dwyt.classification"
	SpanContextPlan        = "dwyt.context_plan"
	SpanContextBudget      = "dwyt.context_budget"
	SpanRetrieval          = "dwyt.retrieval"
	SpanMemorySearch       = "dwyt.memory_search"
	SpanSemanticDedup      = "dwyt.semantic_dedup"
	SpanContextCompression = "dwyt.context_compression"
	SpanCachePolicy        = "dwyt.cache_policy"
	SpanLLMRequest         = "dwyt.llm_request"
	SpanToolCall           = "dwyt.tool_call"
	SpanToolCompression    = "dwyt.tool_compression"
	SpanOutputGovernor     = "dwyt.output_governor"
	SpanMemoryCompile      = "dwyt.memory_compile"
	SpanHousekeeper        = "dwyt.housekeeper"
)

// Tracer emits OTLP spans. The zero value is a working no-op tracer.
type Tracer struct {
	mu       sync.Mutex
	endpoint string
	client   *http.Client
	service  string
	queue    []otlpSpan
	// flushAt is the batch size that triggers a send.
	flushAt int
	enabled bool
	stop    chan struct{}
	wg      sync.WaitGroup
	once    sync.Once
}

// NewTracer builds a tracer from the environment.
//
// It honours the standard OTEL_EXPORTER_OTLP_ENDPOINT (and the traces-specific
// override), so a user who already runs a collector needs no DWYT-specific
// configuration. With no endpoint set the tracer is disabled and every call is a
// cheap no-op.
func NewTracer(service string) *Tracer {
	endpoint := firstEnv(
		"OTEL_EXPORTER_OTLP_TRACES_ENDPOINT",
		"OTEL_EXPORTER_OTLP_ENDPOINT",
		"DWYT_OTLP_ENDPOINT",
	)
	t := &Tracer{
		service: service,
		flushAt: 32,
		stop:    make(chan struct{}),
	}
	if strings.TrimSpace(endpoint) == "" {
		return t
	}
	t.endpoint = normalizeTracesEndpoint(endpoint)
	t.client = &http.Client{Timeout: 3 * time.Second}
	t.enabled = true

	// A periodic flush bounds how long a low-traffic span sits unexported.
	t.wg.Add(1)
	go t.loop()
	return t
}

// Enabled reports whether spans are exported.
func (t *Tracer) Enabled() bool {
	if t == nil {
		return false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.enabled
}

// Endpoint returns the configured OTLP traces endpoint, for the dashboard.
func (t *Tracer) Endpoint() string {
	if t == nil {
		return ""
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.endpoint
}

// normalizeTracesEndpoint appends the OTLP traces path when the caller supplied a
// base URL, matching the behaviour of the standard exporters.
func normalizeTracesEndpoint(endpoint string) string {
	e := strings.TrimRight(strings.TrimSpace(endpoint), "/")
	if strings.HasSuffix(e, "/v1/traces") {
		return e
	}
	return e + "/v1/traces"
}

func firstEnv(keys ...string) string {
	for _, k := range keys {
		if v := strings.TrimSpace(os.Getenv(k)); v != "" {
			return v
		}
	}
	return ""
}

// Span is an in-progress span. A nil Span is safe to use, which is what makes
// the disabled path free of nil checks at every call site.
type Span struct {
	tracer  *Tracer
	name    string
	traceID string
	spanID  string
	parent  string
	start   time.Time
	attrs   map[string]interface{}
	// status is "unset", "ok" or "error".
	status string
	errMsg string
}

// Start begins a span. When tracing is disabled it returns nil, and every method
// on a nil Span is a no-op.
func (t *Tracer) Start(ctx context.Context, name string, attrs map[string]interface{}) (context.Context, *Span) {
	if !t.Enabled() {
		return ctx, nil
	}
	parent := spanIDFromContext(ctx)
	traceID := traceIDFromContext(ctx)
	if traceID == "" {
		traceID = randomHex(16)
	}
	s := &Span{
		tracer:  t,
		name:    name,
		traceID: traceID,
		spanID:  randomHex(8),
		parent:  parent,
		start:   time.Now(),
		attrs:   map[string]interface{}{},
		status:  "unset",
	}
	for k, v := range attrs {
		s.attrs[k] = v
	}
	return withSpan(ctx, traceID, s.spanID), s
}

// Set adds or replaces an attribute.
//
// High-cardinality or sensitive values must not be passed here: spec §54 warns
// against it, and a span attribute leaves the process.
func (s *Span) Set(key string, value interface{}) {
	if s == nil {
		return
	}
	s.attrs[key] = value
}

// SetAll adds several attributes.
func (s *Span) SetAll(attrs map[string]interface{}) {
	if s == nil {
		return
	}
	for k, v := range attrs {
		s.attrs[k] = v
	}
}

// Fail marks the span as failed.
func (s *Span) Fail(err error) {
	if s == nil || err == nil {
		return
	}
	s.status = "error"
	s.errMsg = err.Error()
}

// End finishes the span and queues it for export.
func (s *Span) End() {
	if s == nil {
		return
	}
	if s.status == "unset" {
		s.status = "ok"
	}
	s.tracer.enqueue(s.toOTLP())
}

// contextKey is unexported so no other package can collide with it.
type contextKey struct{ name string }

var (
	traceIDKey = contextKey{"dwyt-trace-id"}
	spanIDKey  = contextKey{"dwyt-span-id"}
)

func withSpan(ctx context.Context, traceID, spanID string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx = context.WithValue(ctx, traceIDKey, traceID)
	return context.WithValue(ctx, spanIDKey, spanID)
}

func traceIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	v, _ := ctx.Value(traceIDKey).(string)
	return v
}

func spanIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	v, _ := ctx.Value(spanIDKey).(string)
	return v
}

func randomHex(n int) string {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		// A weak id is better than a dropped span: ids only need to be unique
		// within a trace, and this path is effectively unreachable.
		for i := range buf {
			buf[i] = byte(time.Now().UnixNano() >> (i % 8))
		}
	}
	return hex.EncodeToString(buf)
}

// --- OTLP/HTTP JSON payload -------------------------------------------------

type otlpSpan struct {
	TraceID           string       `json:"traceId"`
	SpanID            string       `json:"spanId"`
	ParentSpanID      string       `json:"parentSpanId,omitempty"`
	Name              string       `json:"name"`
	Kind              int          `json:"kind"`
	StartTimeUnixNano string       `json:"startTimeUnixNano"`
	EndTimeUnixNano   string       `json:"endTimeUnixNano"`
	Attributes        []otlpKeyVal `json:"attributes,omitempty"`
	Status            otlpStatus   `json:"status"`
}

type otlpKeyVal struct {
	Key   string    `json:"key"`
	Value otlpValue `json:"value"`
}

// otlpValue is the OTLP AnyValue union. Exactly one field is set.
type otlpValue struct {
	StringValue *string  `json:"stringValue,omitempty"`
	IntValue    *string  `json:"intValue,omitempty"`
	DoubleValue *float64 `json:"doubleValue,omitempty"`
	BoolValue   *bool    `json:"boolValue,omitempty"`
}

type otlpStatus struct {
	Code    int    `json:"code"`
	Message string `json:"message,omitempty"`
}

func (s *Span) toOTLP() otlpSpan {
	end := time.Now()
	out := otlpSpan{
		TraceID:           s.traceID,
		SpanID:            s.spanID,
		ParentSpanID:      s.parent,
		Name:              s.name,
		Kind:              1, // SPAN_KIND_INTERNAL
		StartTimeUnixNano: fmt.Sprint(s.start.UnixNano()),
		EndTimeUnixNano:   fmt.Sprint(end.UnixNano()),
	}
	keys := make([]string, 0, len(s.attrs))
	for k := range s.attrs {
		keys = append(keys, k)
	}
	// Sorted attributes make the payload deterministic, which makes it testable.
	sortStrings(keys)
	for _, k := range keys {
		out.Attributes = append(out.Attributes, otlpKeyVal{Key: k, Value: toOTLPValue(s.attrs[k])})
	}
	switch s.status {
	case "error":
		out.Status = otlpStatus{Code: 2, Message: s.errMsg}
	case "ok":
		out.Status = otlpStatus{Code: 1}
	default:
		out.Status = otlpStatus{Code: 0}
	}
	return out
}

func toOTLPValue(v interface{}) otlpValue {
	switch value := v.(type) {
	case string:
		return otlpValue{StringValue: &value}
	case bool:
		return otlpValue{BoolValue: &value}
	case int:
		s := fmt.Sprint(value)
		return otlpValue{IntValue: &s}
	case int64:
		s := fmt.Sprint(value)
		return otlpValue{IntValue: &s}
	case float64:
		return otlpValue{DoubleValue: &value}
	case nil:
		empty := ""
		return otlpValue{StringValue: &empty}
	default:
		s := fmt.Sprint(value)
		return otlpValue{StringValue: &s}
	}
}

func (t *Tracer) enqueue(span otlpSpan) {
	t.mu.Lock()
	if !t.enabled {
		t.mu.Unlock()
		return
	}
	t.queue = append(t.queue, span)
	// Bound the queue: a collector that is down must not turn into a memory leak.
	const maxQueue = 2048
	if len(t.queue) > maxQueue {
		t.queue = t.queue[len(t.queue)-maxQueue:]
	}
	ready := len(t.queue) >= t.flushAt
	t.mu.Unlock()

	if ready {
		go t.Flush()
	}
}

func (t *Tracer) loop() {
	defer t.wg.Done()
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			t.Flush()
		case <-t.stop:
			t.Flush()
			return
		}
	}
}

// Flush exports queued spans. Errors are dropped: tracing is best-effort.
func (t *Tracer) Flush() {
	if t == nil {
		return
	}
	t.mu.Lock()
	if !t.enabled || len(t.queue) == 0 {
		t.mu.Unlock()
		return
	}
	batch := t.queue
	t.queue = nil
	endpoint := t.endpoint
	service := t.service
	client := t.client
	t.mu.Unlock()

	payload := map[string]interface{}{
		"resourceSpans": []map[string]interface{}{{
			"resource": map[string]interface{}{
				"attributes": []otlpKeyVal{
					{Key: "service.name", Value: toOTLPValue(service)},
				},
			},
			"scopeSpans": []map[string]interface{}{{
				"scope": map[string]interface{}{"name": "dwyt"},
				"spans": batch,
			}},
		}},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return
	}
	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return
	}
	_ = resp.Body.Close()
}

// Close flushes and stops the exporter. It is idempotent.
func (t *Tracer) Close() {
	if t == nil {
		return
	}
	t.once.Do(func() {
		t.mu.Lock()
		enabled := t.enabled
		t.mu.Unlock()
		if !enabled {
			return
		}
		close(t.stop)
		t.wg.Wait()
	})
}

func sortStrings(in []string) {
	for i := 1; i < len(in); i++ {
		for j := i; j > 0 && in[j] < in[j-1]; j-- {
			in[j], in[j-1] = in[j-1], in[j]
		}
	}
}
