package mcpproxy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Optimized mode (spec §32).
//
// `transparent` is and remains the default: byte-exact passthrough with passive
// counting. `optimized` is opt-in and compresses *responses* of known tools whose
// output is large, replacing the bulk with a deterministic summary plus a
// `dwyt://objects/<id>` reference.
//
// The overriding rule is spec §32's: **never break a tool to save tokens**. Every
// failure path in this file bypasses to the original bytes:
//
//   - unparseable frame                → passthrough
//   - not a tools/call response        → passthrough
//   - tool not on the known list       → passthrough
//   - output below the threshold       → passthrough
//   - daemon unreachable or erroring   → passthrough
//   - archiving failed (no raw_ref)    → passthrough
//   - re-encoding failed               → passthrough
//
// The last one matters most: a compression that cannot produce a raw reference
// must not be applied, because that would discard evidence with no way to get it
// back.

// Mode selects the proxy behaviour.
type Mode string

const (
	// ModeTransparent is byte-exact passthrough. The default and the fallback.
	ModeTransparent Mode = "transparent"
	// ModeOptimized compresses known large responses. Opt-in.
	ModeOptimized Mode = "optimized"
)

// ParseMode maps a string to a Mode, defaulting to transparent. An unknown value
// is not an error: an unrecognised mode must degrade to the safe behaviour rather
// than refuse to start the MCP server.
func ParseMode(s string) Mode {
	if strings.EqualFold(strings.TrimSpace(s), string(ModeOptimized)) {
		return ModeOptimized
	}
	return ModeTransparent
}

// optimizedMinTokens is the size below which compression is not worth the risk.
// A small response is cheap to send verbatim, and compressing it could only
// introduce a failure mode for no gain.
const optimizedMinTokens = 400

// knownCompressibleTools are the tools whose responses DWYT knows how to
// summarize deterministically: build, test, lint and log-shaped output.
//
// The list is explicit rather than pattern-based because spec §32 restricts
// optimized mode to "known schemas". Adding a tool here is a deliberate act.
var knownCompressibleTools = map[string]bool{
	// Codebase MCP: large graph and snippet payloads.
	"search_graph":     true,
	"trace_path":       true,
	"get_code_snippet": true,
	"get_references":   true,
	"get_dependencies": true,
	"get_tests":        true,
	// Shell/build oriented servers.
	"run_command": true,
	"run_tests":   true,
	"run_build":   true,
	"execute":     true,
	"bash":        true,
	"shell":       true,
	"lint":        true,
	"test":        true,
	"build":       true,
	"read_logs":   true,
	"get_logs":    true,
	"diagnostics": true,
}

// CompactClient asks the DWYT daemon to compact a payload and archive the raw
// bytes. It is an interface so tests can exercise optimized mode without a
// running daemon.
type CompactClient interface {
	// Compact returns the rendered summary and the raw reference. An error means
	// "do not compress" — the caller passes the original through.
	Compact(content, kind, label string) (rendered string, rawRef string, err error)
}

// HTTPCompactClient talks to /api/optimizer/compact.
type HTTPCompactClient struct {
	url    string
	client *http.Client
}

// NewHTTPCompactClient builds a client for the daemon compaction endpoint.
//
// The timeout is short on purpose: optimized mode sits in the live response path
// of an MCP call, so a slow daemon must degrade to passthrough quickly rather
// than stall the agent's turn.
func NewHTTPCompactClient(url string) *HTTPCompactClient {
	return &HTTPCompactClient{url: url, client: &http.Client{Timeout: 3 * time.Second}}
}

// Compact implements CompactClient.
func (c *HTTPCompactClient) Compact(content, kind, label string) (string, string, error) {
	body, err := json.Marshal(map[string]interface{}{
		"content": content,
		"kind":    kind,
		"label":   label,
	})
	if err != nil {
		return "", "", err
	}
	resp, err := c.client.Post(c.url, "application/json", bytes.NewReader(body))
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return "", "", fmt.Errorf("compact endpoint returned HTTP %d", resp.StatusCode)
	}
	var payload struct {
		Rendered  string `json:"rendered"`
		Compacted struct {
			RawRef string `json:"raw_ref"`
		} `json:"compacted"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", "", err
	}
	return payload.Rendered, payload.Compacted.RawRef, nil
}

// pendingCalls tracks which JSON-RPC request id belongs to which tool, so a
// response (which carries only the id) can be matched to its tool name.
type pendingCalls struct {
	byID map[string]string
}

func newPendingCalls() *pendingCalls {
	return &pendingCalls{byID: map[string]string{}}
}

// maxPendingCalls bounds the map for a long-lived session. MCP ids are
// monotonic in practice, so evicting the whole map on overflow costs at most a
// few uncompressed responses and cannot leak.
const maxPendingCalls = 512

func (p *pendingCalls) record(id, tool string) {
	if id == "" || tool == "" {
		return
	}
	if len(p.byID) >= maxPendingCalls {
		p.byID = map[string]string{}
	}
	p.byID[id] = tool
}

func (p *pendingCalls) take(id string) (string, bool) {
	tool, ok := p.byID[id]
	if ok {
		delete(p.byID, id)
	}
	return tool, ok
}

// idKey renders a JSON-RPC id (number or string) as a map key.
func idKey(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var asString string
	if err := json.Unmarshal(raw, &asString); err == nil {
		return "s:" + asString
	}
	var asNumber json.Number
	if err := json.Unmarshal(raw, &asNumber); err == nil {
		return "n:" + asNumber.String()
	}
	return ""
}

// responseOptimizer rewrites server→client frames in optimized mode.
type responseOptimizer struct {
	server  string
	client  CompactClient
	pending *pendingCalls
	out     io.Writer
	buf     []byte
	// bypassed records how many frames were passed through untouched, and
	// compressed how many were rewritten. Exposed for diagnostics/tests.
	bypassed   int
	compressed int
}

func newResponseOptimizer(server string, client CompactClient, pending *pendingCalls, out io.Writer) *responseOptimizer {
	return &responseOptimizer{server: server, client: client, pending: pending, out: out}
}

// Write consumes server output, rewriting complete newline-delimited frames when
// they are eligible and passing everything else through unchanged.
//
// It always reports len(p) consumed: this writer sits in the live response path,
// so a short write would look like a broken pipe to the child process.
func (g *responseOptimizer) Write(p []byte) (int, error) {
	g.buf = append(g.buf, p...)
	for {
		i := bytes.IndexByte(g.buf, '\n')
		if i < 0 {
			break
		}
		frame := g.buf[:i+1]
		g.buf = g.buf[i+1:]
		if _, err := g.out.Write(g.transform(frame)); err != nil {
			return len(p), err
		}
	}
	// A framing without newlines (or a partial frame) must still reach the
	// client. Flush the tail once it exceeds the buffer bound, unchanged.
	if len(g.buf) > maxLineBuffer {
		tail := g.buf
		g.buf = nil
		g.bypassed++
		if _, err := g.out.Write(tail); err != nil {
			return len(p), err
		}
	}
	return len(p), nil
}

// Flush writes any buffered partial frame. Called when the child's stdout closes,
// so a final frame without a trailing newline is not lost.
func (g *responseOptimizer) Flush() error {
	if len(g.buf) == 0 {
		return nil
	}
	tail := g.buf
	g.buf = nil
	_, err := g.out.Write(g.transform(tail))
	return err
}

// jsonRPCFrame is the minimal shape needed to decide on a frame.
type jsonRPCFrame struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   json.RawMessage `json:"error,omitempty"`
}

type callToolResult struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	IsError *bool `json:"isError,omitempty"`
}

// transform returns the frame to forward: either a rewritten frame or the
// original bytes.
func (g *responseOptimizer) transform(frame []byte) []byte {
	trimmed := bytes.TrimSpace(frame)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		g.bypassed++
		return frame
	}

	var envelope jsonRPCFrame
	if err := json.Unmarshal(trimmed, &envelope); err != nil {
		g.bypassed++
		return frame
	}
	// An error response is small and diagnostic; never touch it.
	if len(envelope.Error) > 0 || len(envelope.Result) == 0 {
		g.bypassed++
		return frame
	}
	tool, ok := g.pending.take(idKey(envelope.ID))
	if !ok || !knownCompressibleTools[tool] {
		g.bypassed++
		return frame
	}

	var result callToolResult
	if err := json.Unmarshal(envelope.Result, &result); err != nil || len(result.Content) == 0 {
		g.bypassed++
		return frame
	}
	// Only plain-text content is compressible. Anything else (images, embedded
	// resources) is passed through: DWYT does not understand its schema.
	var textParts []string
	for _, part := range result.Content {
		if part.Type != "text" {
			g.bypassed++
			return frame
		}
		textParts = append(textParts, part.Text)
	}
	joined := strings.Join(textParts, "\n")
	if estimateTokens(joined) < optimizedMinTokens {
		g.bypassed++
		return frame
	}

	rendered, rawRef, err := g.client.Compact(joined, "tool_output", g.server+":"+tool)
	if err != nil || rawRef == "" || strings.TrimSpace(rendered) == "" {
		// No archive means no way back to the evidence. Do not compress.
		g.bypassed++
		return frame
	}
	if estimateTokens(rendered) >= estimateTokens(joined) {
		// Compression that does not reduce is pure risk.
		g.bypassed++
		return frame
	}

	rebuilt, err := rewriteResult(trimmed, rendered)
	if err != nil {
		g.bypassed++
		return frame
	}
	g.compressed++
	return append(rebuilt, '\n')
}

// rewriteResult replaces the text content of a tools/call result, preserving
// every other field of the frame (including isError and any extension fields the
// server added, which DWYT must not silently drop).
func rewriteResult(frame []byte, rendered string) ([]byte, error) {
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(frame, &envelope); err != nil {
		return nil, err
	}
	resultRaw, ok := envelope["result"]
	if !ok {
		return nil, fmt.Errorf("frame has no result")
	}
	var result map[string]interface{}
	if err := json.Unmarshal(resultRaw, &result); err != nil {
		return nil, err
	}
	result["content"] = []map[string]string{{"type": "text", "text": rendered}}
	newResult, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}
	envelope["result"] = newResult
	return json.Marshal(envelope)
}

// estimateTokens mirrors contextopt.EstimateTokens. Duplicated so the proxy — a
// process that must start fast and depend on as little as possible — does not
// pull in the optimizer package.
func estimateTokens(content string) int {
	if content == "" {
		return 0
	}
	byChars := (len(content) + 3) / 4
	byWords := len(strings.Fields(content))
	if byWords > byChars {
		return byWords
	}
	return byChars
}
