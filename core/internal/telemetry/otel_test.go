package telemetry

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// A tracer with no endpoint configured must be completely inert: no goroutine,
// no allocation per span, and no nil-pointer risk at any call site.
func TestDisabledTracerIsAnInertNoOp(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
	t.Setenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", "")
	t.Setenv("DWYT_OTLP_ENDPOINT", "")

	tr := NewTracer("dwyt-test")
	if tr.Enabled() {
		t.Fatal("no endpoint configured; the tracer must be disabled")
	}
	ctx, span := tr.Start(context.Background(), SpanContextPlan, map[string]interface{}{"a": 1})
	if span != nil {
		t.Fatal("a disabled tracer must return a nil span")
	}
	// Every method must survive a nil receiver.
	span.Set("k", "v")
	span.SetAll(map[string]interface{}{"x": 1})
	span.Fail(io.EOF)
	span.End()
	tr.Flush()
	tr.Close()
	if ctx == nil {
		t.Fatal("the context must be returned unchanged")
	}
}

func TestNilTracerIsSafe(t *testing.T) {
	var tr *Tracer
	if tr.Enabled() {
		t.Fatal("a nil tracer is not enabled")
	}
	if tr.Endpoint() != "" {
		t.Fatal("a nil tracer has no endpoint")
	}
	_, span := tr.Start(context.Background(), SpanRequest, nil)
	span.End()
	tr.Flush()
	tr.Close()
}

func TestEndpointNormalization(t *testing.T) {
	cases := map[string]string{
		"http://localhost:4318":           "http://localhost:4318/v1/traces",
		"http://localhost:4318/":          "http://localhost:4318/v1/traces",
		"http://localhost:4318/v1/traces": "http://localhost:4318/v1/traces",
	}
	for in, want := range cases {
		if got := normalizeTracesEndpoint(in); got != want {
			t.Fatalf("normalizeTracesEndpoint(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestTracerExportsOTLPSpans(t *testing.T) {
	var mu sync.Mutex
	var bodies [][]byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("OTLP/HTTP JSON requires application/json, got %q", r.Header.Get("Content-Type"))
		}
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		bodies = append(bodies, body)
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", srv.URL)
	tr := NewTracer("dwyt-test")
	defer tr.Close()
	if !tr.Enabled() {
		t.Fatal("an endpoint was configured; the tracer must be enabled")
	}

	ctx, parent := tr.Start(context.Background(), SpanRequest, map[string]interface{}{"phase": "fix"})
	_, child := tr.Start(ctx, SpanContextPlan, map[string]interface{}{"budget_total": 48000})
	child.Set("level", "symbol")
	child.End()
	parent.End()
	tr.Flush()

	mu.Lock()
	defer mu.Unlock()
	if len(bodies) == 0 {
		t.Fatal("no OTLP payload was exported")
	}

	var payload struct {
		ResourceSpans []struct {
			Resource struct {
				Attributes []struct {
					Key   string `json:"key"`
					Value struct {
						StringValue string `json:"stringValue"`
					} `json:"value"`
				} `json:"attributes"`
			} `json:"resource"`
			ScopeSpans []struct {
				Spans []struct {
					TraceID      string `json:"traceId"`
					SpanID       string `json:"spanId"`
					ParentSpanID string `json:"parentSpanId"`
					Name         string `json:"name"`
					Attributes   []struct {
						Key   string `json:"key"`
						Value struct {
							StringValue *string `json:"stringValue"`
							IntValue    *string `json:"intValue"`
						} `json:"value"`
					} `json:"attributes"`
					Status struct {
						Code int `json:"code"`
					} `json:"status"`
				} `json:"spans"`
			} `json:"scopeSpans"`
		} `json:"resourceSpans"`
	}
	if err := json.Unmarshal(bodies[len(bodies)-1], &payload); err != nil {
		t.Fatalf("exported payload is not valid OTLP JSON: %v\n%s", err, bodies[len(bodies)-1])
	}
	if len(payload.ResourceSpans) != 1 {
		t.Fatalf("expected one resourceSpans entry: %s", bodies[len(bodies)-1])
	}
	rs := payload.ResourceSpans[0]
	foundService := false
	for _, attr := range rs.Resource.Attributes {
		if attr.Key == "service.name" && attr.Value.StringValue == "dwyt-test" {
			foundService = true
		}
	}
	if !foundService {
		t.Fatal("service.name must be present on the resource")
	}

	spans := rs.ScopeSpans[0].Spans
	byName := map[string]int{}
	for i, s := range spans {
		byName[s.Name] = i
		if s.TraceID == "" || s.SpanID == "" {
			t.Fatalf("span %s is missing ids", s.Name)
		}
		if s.Status.Code != 1 {
			t.Fatalf("a span that ended cleanly should report OK, got %d", s.Status.Code)
		}
	}
	childIdx, ok := byName[SpanContextPlan]
	if !ok {
		t.Fatalf("the child span is missing: %v", byName)
	}
	parentIdx, ok := byName[SpanRequest]
	if !ok {
		t.Fatalf("the parent span is missing: %v", byName)
	}
	if spans[childIdx].ParentSpanID != spans[parentIdx].SpanID {
		t.Fatal("the child span must reference its parent")
	}
	if spans[childIdx].TraceID != spans[parentIdx].TraceID {
		t.Fatal("both spans must share one trace id")
	}

	// Integer attributes must be encoded as OTLP intValue strings.
	foundBudget := false
	for _, attr := range spans[childIdx].Attributes {
		if attr.Key == "budget_total" {
			if attr.Value.IntValue == nil || *attr.Value.IntValue != "48000" {
				t.Fatalf("budget_total encoded incorrectly: %+v", attr.Value)
			}
			foundBudget = true
		}
	}
	if !foundBudget {
		t.Fatalf("attributes were lost: %+v", spans[childIdx].Attributes)
	}
}

func TestFailedSpanReportsErrorStatus(t *testing.T) {
	var mu sync.Mutex
	var body []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		mu.Lock()
		body = data
		mu.Unlock()
	}))
	defer srv.Close()

	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", srv.URL)
	tr := NewTracer("dwyt-test")
	defer tr.Close()

	_, span := tr.Start(context.Background(), SpanToolCall, nil)
	span.Fail(io.ErrUnexpectedEOF)
	span.End()
	tr.Flush()

	mu.Lock()
	defer mu.Unlock()
	if body == nil {
		t.Fatal("nothing exported")
	}
	var payload map[string]interface{}
	json.Unmarshal(body, &payload)
	// Walk to the status code without a full struct definition.
	rs := payload["resourceSpans"].([]interface{})[0].(map[string]interface{})
	spans := rs["scopeSpans"].([]interface{})[0].(map[string]interface{})["spans"].([]interface{})
	status := spans[0].(map[string]interface{})["status"].(map[string]interface{})
	if code, _ := status["code"].(float64); int(code) != 2 {
		t.Fatalf("a failed span must report status code 2 (ERROR), got %v", status)
	}
}

// A dead collector must never affect the traced process.
func TestUnreachableCollectorIsHarmless(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://127.0.0.1:1/")
	tr := NewTracer("dwyt-test")
	defer tr.Close()

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 50; i++ {
			_, span := tr.Start(context.Background(), SpanRetrieval, map[string]interface{}{"i": i})
			span.End()
		}
		tr.Flush()
	}()
	select {
	case <-done:
	case <-time.After(15 * time.Second):
		t.Fatal("a dead collector stalled the traced process")
	}
}

func TestQueueIsBounded(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://127.0.0.1:1/")
	tr := NewTracer("dwyt-test")
	defer tr.Close()

	// flushAt is small, so raise it to keep spans queued for the assertion.
	tr.mu.Lock()
	tr.flushAt = 1 << 30
	tr.mu.Unlock()

	for i := 0; i < 5000; i++ {
		_, span := tr.Start(context.Background(), SpanMemorySearch, nil)
		span.End()
	}
	tr.mu.Lock()
	queued := len(tr.queue)
	tr.mu.Unlock()
	if queued > 2048 {
		t.Fatalf("the span queue is unbounded: %d entries", queued)
	}
}

func TestCloseIsIdempotent(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://127.0.0.1:1/")
	tr := NewTracer("dwyt-test")
	tr.Close()
	tr.Close() // must not panic on a double close
}

func TestSpanNamesCoverThePipeline(t *testing.T) {
	// Spec §54 enumerates the pipeline spans; a missing constant means a phase
	// silently disappears from every trace.
	required := []string{
		SpanRequest, SpanClassification, SpanContextPlan, SpanContextBudget,
		SpanRetrieval, SpanMemorySearch, SpanSemanticDedup, SpanContextCompression,
		SpanCachePolicy, SpanLLMRequest, SpanToolCall, SpanToolCompression,
		SpanOutputGovernor, SpanMemoryCompile, SpanHousekeeper,
	}
	seen := map[string]bool{}
	for _, name := range required {
		if name == "" {
			t.Fatal("an empty span name would produce anonymous spans")
		}
		if seen[name] {
			t.Fatalf("duplicate span name %q", name)
		}
		seen[name] = true
	}
	if len(seen) != 15 {
		t.Fatalf("expected 15 distinct pipeline spans, got %d", len(seen))
	}
}
