package mcpproxy

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"
)

type capReporter struct {
	mu    sync.Mutex
	calls []UsageReport
}

func (c *capReporter) Report(r UsageReport) {
	c.mu.Lock()
	c.calls = append(c.calls, r)
	c.mu.Unlock()
}

func (c *capReporter) Close() {}

func (c *capReporter) snapshot() []UsageReport {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]UsageReport, len(c.calls))
	copy(out, c.calls)
	return out
}

// TestRunPassthroughAndCount verifies the shim forwards stdin to the child
// byte-for-byte (using `cat` as a transparent echo) while counting tools/call
// requests and ignoring other methods.
func TestRunPassthroughAndCount(t *testing.T) {
	catPath, err := exec.LookPath("cat")
	if err != nil {
		t.Skip("cat not available on PATH")
	}

	lines := []string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"search_graph","arguments":{}}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"trace_path"}}`,
	}
	in := strings.Join(lines, "\n") + "\n"

	var out strings.Builder
	rep := &capReporter{}
	code, err := Run(Config{
		Target:   catPath,
		Name:     "codebase",
		Reporter: rep,
		Stdin:    strings.NewReader(in),
		Stdout:   &out,
		Stderr:   io.Discard,
	})
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}
	if code != 0 {
		t.Fatalf("expected exit 0, got %d", code)
	}
	if out.String() != in {
		t.Fatalf("passthrough mismatch:\nwant %q\ngot  %q", in, out.String())
	}

	calls := rep.snapshot()
	if len(calls) != 2 {
		t.Fatalf("expected 2 tools/call, got %d: %#v", len(calls), calls)
	}
	if calls[0].Tool != "search_graph" || calls[1].Tool != "trace_path" {
		t.Fatalf("unexpected tool names: %#v", calls)
	}
	for _, c := range calls {
		if c.Server != "codebase" {
			t.Fatalf("expected server codebase, got %q", c.Server)
		}
	}
}

// TestRunMissingTarget ensures a bad target surfaces as a non-zero exit with an
// error rather than panicking.
func TestRunMissingTarget(t *testing.T) {
	rep := &capReporter{}
	code, err := Run(Config{
		Target:   "/nonexistent/dwyt-mcp-target",
		Name:     "codebase",
		Reporter: rep,
		Stdin:    strings.NewReader(""),
		Stdout:   io.Discard,
		Stderr:   io.Discard,
	})
	if err == nil {
		t.Fatal("expected error for missing target")
	}
	if code == 0 {
		t.Fatal("expected non-zero exit for missing target")
	}
}

// TestCallCounterChunkedLines verifies counting works when JSON arrives split
// across writes (as it does over a real pipe).
func TestCallCounterChunkedLines(t *testing.T) {
	rep := &capReporter{}
	c := &callCounter{server: "codebase", reporter: rep}
	full := `{"jsonrpc":"2.0","id":9,"method":"tools/call","params":{"name":"get_code_snippet"}}` + "\n"
	// Feed one byte at a time.
	for i := 0; i < len(full); i++ {
		if _, err := c.Write([]byte{full[i]}); err != nil {
			t.Fatalf("write error: %v", err)
		}
	}
	calls := rep.snapshot()
	if len(calls) != 1 || calls[0].Tool != "get_code_snippet" {
		t.Fatalf("expected 1 get_code_snippet call, got %#v", calls)
	}
}

func TestHTTPReporterCloseCancelsQueuedReportsAndAllowsLateReport(t *testing.T) {
	requestStarted := make(chan struct{})
	releaseRequest := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(requestStarted)
		<-releaseRequest
	}))
	defer server.Close()

	reporter := NewHTTPReporter(server.URL)
	reporter.Report(UsageReport{Server: "codebase", Tool: "first"})
	select {
	case <-requestStarted:
	case <-time.After(time.Second):
		t.Fatal("usage request did not reach the test server")
	}
	// Fill the best-effort queue. The old Close drained this one request at a
	// time (up to 256 × HTTP timeout); shutdown must instead cancel promptly.
	for i := 0; i < cap(reporter.ch); i++ {
		reporter.Report(UsageReport{Server: "codebase", Tool: "queued"})
	}

	start := time.Now()
	reporter.Close()
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Fatalf("Close took %s while dashboard request was stalled", elapsed)
	}
	close(releaseRequest)

	// Run's stdin tee can still call Report after the child exits. This must be
	// a harmless drop rather than a send-on-closed-channel panic.
	for i := 0; i < 10; i++ {
		reporter.Report(UsageReport{Server: "codebase", Tool: "late"})
	}
}
