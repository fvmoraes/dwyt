package server

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/fvmoraes/dwyt/internal/procman"
	"github.com/fvmoraes/dwyt/internal/state"
	"github.com/fvmoraes/dwyt/internal/toolsource"
	"github.com/gin-gonic/gin"
)

func TestHeadroomInitArgsUseDurableNonInteractiveSetup(t *testing.T) {
	cases := []struct {
		client string
		want   []string
	}{
		{"claude", []string{"init", "--port", "8788", "claude"}},
		{"codex", []string{"init", "--port", "8788", "codex"}},
		{"copilot", []string{"init", "--global", "--port", "8788", "copilot"}},
	}
	for _, tc := range cases {
		got, ok := headroomInitArgs(tc.client, 8788)
		if !ok || strings.Join(got, "\x00") != strings.Join(tc.want, "\x00") {
			t.Fatalf("headroomInitArgs(%q) = %q, %v; want %q, true", tc.client, got, ok, tc.want)
		}
		for _, arg := range got {
			if arg == "wrap" || arg == "--no-proxy" {
				t.Fatalf("durable setup for %s must not invoke interactive wrap: %q", tc.client, got)
			}
		}
	}
	if args, ok := headroomInitArgs("cursor", 8788); ok || args != nil {
		t.Fatalf("Cursor has no durable init and must not start a second proxy: %q, %v", args, ok)
	}
}

func TestHeadroomCommandEnvOverridesStaleFallbackValues(t *testing.T) {
	env := headroomCommandEnv([]string{
		"HEADROOM_PORT=8787",
		"OPENAI_BASE_URL=http://127.0.0.1:8787/v1",
		"ANTHROPIC_BASE_URL=http://127.0.0.1:8787",
		"DWYT_HEADROOM_PORT=8787",
		"KEEP=this",
	}, 8788)
	values := make(map[string]string)
	for _, entry := range env {
		key, value, ok := strings.Cut(entry, "=")
		if ok {
			values[key] = value
		}
	}
	for key, want := range map[string]string{
		"HEADROOM_PORT":      "8788",
		"OPENAI_BASE_URL":    "http://127.0.0.1:8788/v1",
		"ANTHROPIC_BASE_URL": "http://127.0.0.1:8788",
		"DWYT_HEADROOM_PORT": "8788",
		"KEEP":               "this",
	} {
		if got := values[key]; got != want {
			t.Fatalf("%s = %q, want %q; env=%q", key, got, want, env)
		}
	}
}

func TestConfiguredHeadroomPortRejectsInvalidEnvironment(t *testing.T) {
	t.Setenv("DWYT_HEADROOM_PORT", "8788")
	if got := configuredHeadroomPort(); got != 8788 {
		t.Fatalf("configured port = %d, want 8788", got)
	}
	for _, value := range []string{"0", "70000", "invalid"} {
		t.Setenv("DWYT_HEADROOM_PORT", value)
		if got := configuredHeadroomPort(); got != 8787 {
			t.Fatalf("configured port for %q = %d, want default 8787", value, got)
		}
	}
}

func TestHeadroomLifecycleUsesSelectedExternalToolPath(t *testing.T) {
	external := filepath.Join(t.TempDir(), "headroom-external")
	if err := os.WriteFile(external, []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
		t.Fatal(err)
	}
	runtimeState := state.Init(t.TempDir())
	runtimeState.SetToolSources(map[string]toolsource.Selection{
		toolsource.ToolHeadroom: {Mode: toolsource.ModeExternal, Path: external},
	})
	ds := &DashboardServer{DwytBin: t.TempDir(), RuntimeState: runtimeState}
	if got := ds.headroomPath(); got != external {
		t.Fatalf("headroom lifecycle path = %q, want selected external path %q", got, external)
	}
}

// TestAPIHeadroomStatsURLUsesRegisteredProcessManager proves that the legacy
// direct `bin/headroom` launcher is not used. The configured DWYT bin is
// intentionally empty; only the registered managed service can satisfy this
// request. The occupied starting port also verifies that the selected fallback
// port is published to the dashboard response.
func TestAPIHeadroomStatsURLUsesRegisteredProcessManager(t *testing.T) {
	t.Setenv("DWYT_HEADROOM_STATS_HELPER", "1")
	t.Setenv("DWYT_DAEMON_HEALTHCHECK_TIMEOUT_SECONDS", "5")
	blocker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "occupied", http.StatusServiceUnavailable)
	}))
	defer blocker.Close()

	u, err := url.Parse(blocker.URL)
	if err != nil {
		t.Fatal(err)
	}
	_, portText, err := net.SplitHostPort(u.Host)
	if err != nil {
		t.Fatal(err)
	}
	blockedPort, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatal(err)
	}

	pm := procman.New(t.TempDir())
	pm.Register("headroom", os.Args[0], "/health", blockedPort,
		"-test.run=^TestHeadroomStatsProxyHelper$", "--", "--port", "{port}")
	ds := &DashboardServer{
		DwytBin:      t.TempDir(), // no legacy headroom binary exists here
		ProcMan:      pm,
		HeadroomPort: blockedPort,
	}
	t.Cleanup(func() { _, _ = pm.Stop("headroom") })

	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/headroom/stats-url", nil)
	ds.apiHeadroomStatsURL(ctx)

	if rec.Code != http.StatusOK {
		t.Fatalf("stats URL status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		URL     string `json:"url"`
		Started bool   `json:"started"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	status := pm.Status("headroom")
	if !status.Running || !status.Healthy {
		t.Fatalf("registered headroom was not started healthily: %+v", status)
	}
	if !body.Started {
		t.Fatalf("expected managed service startup response, got %+v", body)
	}
	wantURL := "http://127.0.0.1:" + strconv.Itoa(status.Port) + "/stats"
	if body.URL != wantURL {
		t.Fatalf("stats URL = %q, want %q", body.URL, wantURL)
	}
	if got := ds.headroomPort(); got != status.Port {
		t.Fatalf("dashboard headroom port = %d, want ProcessManager port %d", got, status.Port)
	}
	if status.Port == blockedPort {
		t.Fatalf("managed process reused occupied port %d", blockedPort)
	}
}

// TestHeadroomStatsProxyHelper is invoked as a child test binary by the
// ProcessManager test above. It is intentionally a real process so the test
// covers the platform-specific command attributes and lifecycle path.
func TestHeadroomStatsProxyHelper(t *testing.T) {
	if os.Getenv("DWYT_HEADROOM_STATS_HELPER") != "1" {
		return
	}
	port := helperPort(t, os.Args)
	listener, err := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(port))
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"healthy","ready":true}`))
	})
	mux.HandleFunc("/stats", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{}`))
	})
	if err := http.Serve(listener, mux); err != nil {
		t.Fatal(err)
	}
}

func helperPort(t *testing.T, args []string) int {
	t.Helper()
	for i, arg := range args {
		if arg != "--port" || i+1 >= len(args) {
			continue
		}
		port, err := strconv.Atoi(args[i+1])
		if err == nil && port > 0 {
			return port
		}
	}
	t.Fatalf("helper port not found in arguments: %q", args)
	return 0
}
