// Package mcpproxy is a transparent stdio shim that sits between an AI client
// (any IDE/harness: Kiro, Cursor, VSCode, Claude Desktop, Codex, ...) and a
// real MCP server binary spawned over stdio.
//
// It exists so the DWYT dashboard can count MCP tool calls regardless of which
// client made them: every client just runs the command DWYT writes into its
// MCP config, so routing that command through this shim makes the calls
// observable in one place.
//
// Safety by construction:
//   - The data path is byte-exact. stdout/stderr are wired straight to the
//     child's file descriptors; stdin is duplicated with io.TeeReader so the
//     child receives the exact same bytes the client sent.
//   - Counting is a passive observer on a copy of the client→server stream. It
//     never blocks, mutates, or fails the stream: its Write always succeeds.
//   - Counting is framing-agnostic-safe: if the child speaks a framing this
//     observer does not recognize, calls simply go uncounted — the proxied
//     session keeps working.
//   - Usage reports are best-effort fire-and-forget. If the dashboard is down,
//     reports are dropped and the session is unaffected.
package mcpproxy

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"
)

// maxLineBuffer caps how many bytes the observer retains while waiting for a
// newline, so a child that never emits newline-delimited JSON can't grow the
// buffer without bound.
const maxLineBuffer = 1 << 20 // 1 MiB

// UsageReport is the payload posted to the dashboard for each observed call.
type UsageReport struct {
	Server string `json:"server"`
	Tool   string `json:"tool"`
}

// Reporter delivers usage reports. It is abstracted so tests can capture
// reports without a real HTTP server.
type Reporter interface {
	Report(UsageReport)
	Close()
}

// Config controls a proxy run.
type Config struct {
	// Target is the absolute path to the real MCP server binary.
	Target string
	// Name is the logical server name credited in reports (e.g. "codebase").
	Name string
	// Args are passed through to the target binary.
	Args []string
	// Reporter receives one report per observed tools/call. May be nil.
	Reporter Reporter
	// Stdin/Stdout/Stderr default to the process std streams; overridable in
	// tests.
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
}

// Run spawns the target, transparently bridges stdio, counts tools/call
// requests, and returns the target's exit code. A non-nil error means the
// target could not be started or waited on; a clean child exit (even non-zero)
// returns the code with a nil error.
func Run(cfg Config) (int, error) {
	stdin := cfg.Stdin
	if stdin == nil {
		stdin = os.Stdin
	}
	stdout := cfg.Stdout
	if stdout == nil {
		stdout = os.Stdout
	}
	stderr := cfg.Stderr
	if stderr == nil {
		stderr = os.Stderr
	}

	cmd := exec.Command(cfg.Target, cfg.Args...)
	cmd.Env = os.Environ()
	cmd.Stdout = stdout
	cmd.Stderr = stderr

	counter := &callCounter{server: cfg.Name, reporter: cfg.Reporter}

	// Bridge stdin through our own OS pipe and hand the child a real *os.File.
	// This is deliberate: if we set cmd.Stdin to a non-*os.File reader, os/exec
	// spawns an internal copy goroutine that cmd.Wait blocks on — so a child
	// that exits while the client keeps stdin open would hang Wait (and thus the
	// MCP server) on every platform. Owning the pipe means exec waits on nothing
	// but the process, and our copy goroutine is fire-and-forget.
	pr, pw, err := os.Pipe()
	if err != nil {
		return 1, err
	}
	cmd.Stdin = pr

	if err := cmd.Start(); err != nil {
		_ = pr.Close()
		_ = pw.Close()
		return 1, err
	}

	// Feed the child: copy client stdin into the pipe while teeing a copy to the
	// call counter. Closing pw on EOF propagates EOF to the child for a clean
	// shutdown. Not awaited — if the child exits first, this goroutine may stay
	// blocked reading stdin, but the process exit reaps it.
	go func() {
		_, _ = io.Copy(pw, io.TeeReader(stdin, counter))
		_ = pw.Close()
	}()

	// Forward termination signals to the child so closing the client tears the
	// whole tree down cleanly.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	done := make(chan struct{})
	go func() {
		select {
		case s := <-sigCh:
			if cmd.Process != nil {
				_ = cmd.Process.Signal(s)
			}
		case <-done:
		}
	}()

	waitErr := cmd.Wait()
	close(done)
	signal.Stop(sigCh)
	_ = pr.Close()
	if cfg.Reporter != nil {
		cfg.Reporter.Close()
	}

	if waitErr == nil {
		return 0, nil
	}
	if exitErr, ok := waitErr.(*exec.ExitError); ok {
		return exitErr.ExitCode(), nil
	}
	return 1, waitErr
}

// callCounter is an io.Writer that scans a JSON-RPC stream for tools/call
// requests. Its Write never errors, so it is safe inside an io.TeeReader.
type callCounter struct {
	server   string
	reporter Reporter
	buf      []byte
}

type rpcPeek struct {
	Method string `json:"method"`
	Params struct {
		Name string `json:"name"`
	} `json:"params"`
}

func (c *callCounter) Write(p []byte) (int, error) {
	c.buf = append(c.buf, p...)
	for {
		i := bytes.IndexByte(c.buf, '\n')
		if i < 0 {
			break
		}
		line := c.buf[:i]
		c.buf = c.buf[i+1:]
		c.inspect(line)
	}
	// Bound the buffer for non-newline-delimited framings: keep only the tail,
	// which is where a complete object would still be forming.
	if len(c.buf) > maxLineBuffer {
		c.buf = c.buf[len(c.buf)-(maxLineBuffer/2):]
	}
	return len(p), nil
}

func (c *callCounter) inspect(line []byte) {
	line = bytes.TrimSpace(line)
	if len(line) == 0 || c.reporter == nil {
		return
	}
	// Cheap guard before paying for JSON parsing.
	if !bytes.Contains(line, []byte("tools/call")) {
		return
	}
	var peek rpcPeek
	if err := json.Unmarshal(line, &peek); err != nil {
		return
	}
	if peek.Method != "tools/call" {
		return
	}
	tool := strings.TrimSpace(peek.Params.Name)
	c.reporter.Report(UsageReport{Server: c.server, Tool: tool})
}

// HTTPReporter posts usage reports to the dashboard on a single background
// worker, so a burst of calls never spawns unbounded goroutines and a slow or
// dead dashboard never stalls the proxied session.
type HTTPReporter struct {
	url    string
	client *http.Client
	ch     chan UsageReport
	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{}
	wg     sync.WaitGroup
	once   sync.Once
}

// NewHTTPReporter builds a reporter targeting the given dashboard usage URL.
func NewHTTPReporter(url string) *HTTPReporter {
	ctx, cancel := context.WithCancel(context.Background())
	r := &HTTPReporter{
		url:    url,
		client: &http.Client{Timeout: 2 * time.Second},
		ch:     make(chan UsageReport, 256),
		ctx:    ctx,
		cancel: cancel,
		done:   make(chan struct{}),
	}
	r.wg.Add(1)
	go r.loop()
	return r
}

func (r *HTTPReporter) loop() {
	defer r.wg.Done()
	for {
		// Prefer shutdown over a queued report. Close must not spend up to one
		// HTTP timeout per buffered event while an MCP client is exiting.
		select {
		case <-r.done:
			return
		default:
		}

		var rep UsageReport
		select {
		case <-r.done:
			return
		case rep = <-r.ch:
		}
		body, err := json.Marshal(rep)
		if err != nil {
			continue
		}
		req, err := http.NewRequestWithContext(r.ctx, http.MethodPost, r.url, bytes.NewReader(body))
		if err != nil {
			continue
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := r.client.Do(req)
		if err != nil {
			continue
		}
		_ = resp.Body.Close()
	}
}

// Report enqueues a usage report, dropping it if the queue is full rather than
// blocking the proxied stream.
func (r *HTTPReporter) Report(rep UsageReport) {
	// Keep the queue open permanently. Run's stdin tee goroutine can outlive
	// the target process; it may observe another tools/call after Close, and
	// sending to a closed channel would crash the proxy process.
	select {
	case <-r.done:
		return
	default:
	}
	select {
	case <-r.done:
		return
	case r.ch <- rep:
	default:
		// Queue full: drop. Counting is best-effort and must never back-pressure
		// the live MCP session.
	}
}

// Close cancels in-flight reports and stops the worker without draining the
// queue. Reporting is deliberately best-effort, so a slow/dead dashboard must
// never delay MCP shutdown; keeping ch open also makes a late Report harmless.
func (r *HTTPReporter) Close() {
	r.once.Do(func() {
		r.cancel()
		close(r.done)
		r.wg.Wait()
	})
}
