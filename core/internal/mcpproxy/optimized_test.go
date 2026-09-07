package mcpproxy

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
)

// stubCompactor lets a test control exactly what the daemon would return.
type stubCompactor struct {
	rendered string
	rawRef   string
	err      error
	calls    int
}

func (s *stubCompactor) Compact(content, kind, label string) (string, string, error) {
	s.calls++
	if s.err != nil {
		return "", "", s.err
	}
	return s.rendered, s.rawRef, nil
}

// callFrame builds a tools/call request frame.
func callFrame(id int, tool string) string {
	return fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"method":"tools/call","params":{"name":%q,"arguments":{}}}`+"\n", id, tool)
}

// resultFrame builds a tools/call response frame carrying text content.
func resultFrame(id int, text string) string {
	payload := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      id,
		"result": map[string]interface{}{
			"content": []map[string]string{{"type": "text", "text": text}},
		},
	}
	data, _ := json.Marshal(payload)
	return string(data) + "\n"
}

func bigOutput() string {
	var b strings.Builder
	for i := 0; i < 400; i++ {
		fmt.Fprintf(&b, "[INFO] compiling module %d\n", i)
	}
	return b.String()
}

// runOptimized feeds a request then a response through the optimized pipeline and
// returns what the client would see.
func runOptimized(t *testing.T, tool string, response string, compactor CompactClient) (string, *responseOptimizer) {
	t.Helper()
	pending := newPendingCalls()
	counter := &callCounter{server: "codebase", pending: pending}
	if _, err := counter.Write([]byte(callFrame(1, tool))); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	gov := newResponseOptimizer("codebase", compactor, pending, &out)
	if _, err := gov.Write([]byte(response)); err != nil {
		t.Fatal(err)
	}
	if err := gov.Flush(); err != nil {
		t.Fatal(err)
	}
	return out.String(), gov
}

func TestParseModeDefaultsToTransparent(t *testing.T) {
	if ParseMode("optimized") != ModeOptimized {
		t.Fatal("optimized should parse")
	}
	if ParseMode("OPTIMIZED") != ModeOptimized {
		t.Fatal("mode parsing should be case-insensitive")
	}
	for _, in := range []string{"", "transparent", "nonsense", "   "} {
		if ParseMode(in) != ModeTransparent {
			t.Fatalf("%q must default to transparent", in)
		}
	}
}

func TestOptimizedCompactsKnownLargeResponse(t *testing.T) {
	raw := bigOutput()
	stub := &stubCompactor{rendered: "status: pass\nsummary: 400 lines suppressed\n", rawRef: "dwyt://objects/abc123"}

	out, gov := runOptimized(t, "run_tests", resultFrame(1, raw), stub)
	if gov.compressed != 1 {
		t.Fatalf("expected the response to be compacted, got compressed=%d bypassed=%d", gov.compressed, gov.bypassed)
	}
	if strings.Contains(out, "compiling module 399") {
		t.Fatalf("the bulk should be gone:\n%s", out)
	}
	if !strings.Contains(out, "400 lines suppressed") {
		t.Fatalf("the summary should be present:\n%s", out)
	}

	// The frame must remain a valid JSON-RPC response with the same id.
	var decoded map[string]interface{}
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &decoded); err != nil {
		t.Fatalf("rewritten frame is not valid JSON: %v\n%s", err, out)
	}
	if decoded["jsonrpc"] != "2.0" {
		t.Fatalf("jsonrpc field lost: %s", out)
	}
	if id, _ := decoded["id"].(float64); int(id) != 1 {
		t.Fatalf("the response id must be preserved: %s", out)
	}
}

func TestOptimizedBypassesUnknownTools(t *testing.T) {
	raw := bigOutput()
	stub := &stubCompactor{rendered: "summary", rawRef: "dwyt://objects/abc"}

	out, gov := runOptimized(t, "some_unknown_tool", resultFrame(1, raw), stub)
	if gov.compressed != 0 {
		t.Fatal("an unknown tool must never be compacted: DWYT does not know its schema")
	}
	if stub.calls != 0 {
		t.Fatal("the daemon should not even be contacted for an unknown tool")
	}
	if !strings.Contains(out, "compiling module 399") {
		t.Fatal("the original bytes must pass through untouched")
	}
}

func TestOptimizedBypassesSmallResponses(t *testing.T) {
	stub := &stubCompactor{rendered: "s", rawRef: "dwyt://objects/abc"}
	out, gov := runOptimized(t, "run_tests", resultFrame(1, "ok"), stub)
	if gov.compressed != 0 {
		t.Fatal("a tiny response must not be compacted")
	}
	if !strings.Contains(out, `"ok"`) {
		t.Fatalf("the original content should survive:\n%s", out)
	}
}

func TestOptimizedBypassesWhenArchivingFails(t *testing.T) {
	raw := bigOutput()

	// Daemon unreachable.
	out, gov := runOptimized(t, "run_tests", resultFrame(1, raw), &stubCompactor{err: errors.New("connection refused")})
	if gov.compressed != 0 {
		t.Fatal("a failed compaction must bypass, not drop the output")
	}
	if !strings.Contains(out, "compiling module 0") {
		t.Fatal("the original bytes must survive a daemon failure")
	}

	// Daemon answered but produced no raw reference: compressing would discard
	// evidence with no way to retrieve it.
	out, gov = runOptimized(t, "run_tests", resultFrame(1, raw), &stubCompactor{rendered: "summary only"})
	if gov.compressed != 0 {
		t.Fatal("without a raw_ref the compaction must be refused")
	}
	if !strings.Contains(out, "compiling module 0") {
		t.Fatal("the original bytes must survive a missing raw reference")
	}
}

func TestOptimizedBypassesWhenCompactionDoesNotReduce(t *testing.T) {
	raw := bigOutput()
	stub := &stubCompactor{rendered: raw + raw, rawRef: "dwyt://objects/abc"}

	_, gov := runOptimized(t, "run_tests", resultFrame(1, raw), stub)
	if gov.compressed != 0 {
		t.Fatal("a compaction that grows the payload is pure risk and must be refused")
	}
}

func TestOptimizedBypassesNonTextContent(t *testing.T) {
	pending := newPendingCalls()
	counter := &callCounter{server: "codebase", pending: pending}
	counter.Write([]byte(callFrame(1, "run_tests")))

	payload := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"result": map[string]interface{}{
			"content": []map[string]string{
				{"type": "image", "data": strings.Repeat("A", 8000)},
			},
		},
	}
	data, _ := json.Marshal(payload)

	var out bytes.Buffer
	gov := newResponseOptimizer("codebase", &stubCompactor{rendered: "x", rawRef: "dwyt://objects/a"}, pending, &out)
	gov.Write(append(data, '\n'))
	if gov.compressed != 0 {
		t.Fatal("non-text content has an unknown schema and must pass through")
	}
	if !strings.Contains(out.String(), "image") {
		t.Fatal("the original frame must be preserved")
	}
}

func TestOptimizedBypassesErrorResponses(t *testing.T) {
	pending := newPendingCalls()
	counter := &callCounter{server: "codebase", pending: pending}
	counter.Write([]byte(callFrame(1, "run_tests")))

	frame := `{"jsonrpc":"2.0","id":1,"error":{"code":-32603,"message":"boom"}}` + "\n"
	var out bytes.Buffer
	gov := newResponseOptimizer("codebase", &stubCompactor{rendered: "x", rawRef: "dwyt://objects/a"}, pending, &out)
	gov.Write([]byte(frame))

	if gov.compressed != 0 {
		t.Fatal("an error response is small and diagnostic; it must never be touched")
	}
	if out.String() != frame {
		t.Fatalf("error frames must be byte-exact:\n%q", out.String())
	}
}

func TestOptimizedPreservesUnrelatedFramesByteExactly(t *testing.T) {
	frames := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2024-11-05"}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`not json at all`,
	}, "\n") + "\n"

	var out bytes.Buffer
	gov := newResponseOptimizer("codebase", &stubCompactor{}, newPendingCalls(), &out)
	gov.Write([]byte(frames))
	if err := gov.Flush(); err != nil {
		t.Fatal(err)
	}
	if out.String() != frames {
		t.Fatalf("unrelated frames must be byte-exact:\nwant %q\ngot  %q", frames, out.String())
	}
}

func TestOptimizedPreservesIsErrorAndExtensionFields(t *testing.T) {
	raw := bigOutput()
	pending := newPendingCalls()
	counter := &callCounter{server: "codebase", pending: pending}
	counter.Write([]byte(callFrame(7, "run_tests")))

	payload := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      7,
		"result": map[string]interface{}{
			"content":     []map[string]string{{"type": "text", "text": raw}},
			"isError":     true,
			"_serverMeta": map[string]string{"vendor": "acme"},
		},
	}
	data, _ := json.Marshal(payload)

	var out bytes.Buffer
	gov := newResponseOptimizer("codebase",
		&stubCompactor{rendered: "status: fail\n", rawRef: "dwyt://objects/abc"}, pending, &out)
	gov.Write(append(data, '\n'))
	if gov.compressed != 1 {
		t.Fatalf("expected compaction, got %+v", gov)
	}

	var decoded map[string]interface{}
	json.Unmarshal([]byte(strings.TrimSpace(out.String())), &decoded)
	result := decoded["result"].(map[string]interface{})
	if isErr, _ := result["isError"].(bool); !isErr {
		t.Fatalf("isError must survive the rewrite: %s", out.String())
	}
	if _, ok := result["_serverMeta"]; !ok {
		t.Fatalf("server extension fields must not be dropped: %s", out.String())
	}
}

func TestPendingCallsMatchStringAndNumberIDs(t *testing.T) {
	p := newPendingCalls()
	p.record(idKey(json.RawMessage(`5`)), "run_tests")
	p.record(idKey(json.RawMessage(`"abc"`)), "run_build")

	if tool, ok := p.take(idKey(json.RawMessage(`5`))); !ok || tool != "run_tests" {
		t.Fatalf("numeric id lookup failed: %q %v", tool, ok)
	}
	if tool, ok := p.take(idKey(json.RawMessage(`"abc"`))); !ok || tool != "run_build" {
		t.Fatalf("string id lookup failed: %q %v", tool, ok)
	}
	// Taking consumes: a duplicate response must not match twice.
	if _, ok := p.take(idKey(json.RawMessage(`5`))); ok {
		t.Fatal("take must consume the entry")
	}
}

func TestPendingCallsAreBounded(t *testing.T) {
	p := newPendingCalls()
	for i := 0; i < maxPendingCalls+50; i++ {
		p.record(fmt.Sprintf("n:%d", i), "run_tests")
	}
	if len(p.byID) > maxPendingCalls {
		t.Fatalf("pending map is unbounded: %d entries", len(p.byID))
	}
}

func TestResponseOptimizerFlushesPartialFrames(t *testing.T) {
	var out bytes.Buffer
	gov := newResponseOptimizer("codebase", &stubCompactor{}, newPendingCalls(), &out)
	// A final frame with no trailing newline must still reach the client.
	gov.Write([]byte(`{"jsonrpc":"2.0","id":9,"result":{}}`))
	if out.Len() != 0 {
		t.Fatal("an incomplete frame should be buffered, not forwarded early")
	}
	if err := gov.Flush(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"id":9`) {
		t.Fatalf("the buffered frame was lost:\n%s", out.String())
	}
}
