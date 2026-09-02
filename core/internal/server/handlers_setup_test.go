package server

import (
	"bytes"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"testing"

	"github.com/fvmoraes/dwyt/internal/db"
	"github.com/fvmoraes/dwyt/internal/procman"
	"github.com/fvmoraes/dwyt/internal/state"
	"github.com/gin-gonic/gin"
)

// TestSetupLoadMigratesLegacyClientsToIas protects upgrades from releases
// which persisted the selected AI clients under `clients` before `ias` became
// the canonical field. The wizard must still render the saved choices.
func TestSetupLoadMigratesLegacyClientsToIas(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store, err := db.New(filepath.Join(t.TempDir(), "dwyt.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.SetConfig("setup", `{"configured":true,"clients":["continue","cursor"],"tools":["rtk"]}`); err != nil {
		t.Fatal(err)
	}

	ds := &DashboardServer{Store: store}
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest("GET", "/api/setup/load", nil)
	ds.apiSetupLoad(c)

	if rec.Code != 200 {
		t.Fatalf("expected HTTP 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var got Config
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if want := []string{"continue", "cursor"}; !reflect.DeepEqual(got.Ias, want) {
		t.Fatalf("legacy clients selection was not exposed as ias: got %#v, want %#v", got.Ias, want)
	}
	if !contains(got.Tools, "obsidian") {
		t.Fatalf("expected required obsidian tool after migration, got %#v", got.Tools)
	}
}

// TestSetupSaveCanonicalizesLegacyClients ensures API consumers that still
// submit the old shape are upgraded before persistence, so install and later
// daemon starts retain the selected clients.
func TestSetupSaveCanonicalizesLegacyClients(t *testing.T) {
	gin.SetMode(gin.TestMode)
	home := t.TempDir()
	store, err := db.New(filepath.Join(home, "dwyt.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	ds := &DashboardServer{Store: store, RuntimeState: state.Init(home)}
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest("POST", "/api/setup/save", bytes.NewBufferString(`{"clients":["continue"],"tools":["rtk"]}`))
	c.Request.Header.Set("Content-Type", "application/json")
	ds.apiSetupSave(c)

	if rec.Code != 200 {
		t.Fatalf("expected HTTP 200, got %d: %s", rec.Code, rec.Body.String())
	}
	raw, err := store.GetConfig("setup")
	if err != nil {
		t.Fatal(err)
	}
	var got Config
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatal(err)
	}
	if want := []string{"continue"}; !reflect.DeepEqual(got.Ias, want) {
		t.Fatalf("legacy clients selection was not persisted as ias: got %#v, want %#v", got.Ias, want)
	}
	if !got.Configured || got.LastSetup == "" {
		t.Fatalf("expected setup metadata to be persisted, got %#v", got)
	}
	if want := []string{"continue"}; !reflect.DeepEqual(state.Init(home).Clients, want) {
		t.Fatalf("legacy clients selection was not persisted to runtime state: got %#v, want %#v", state.Init(home).Clients, want)
	}
}

// TestSetupSaveRoundTripsHubToolAndClientMatrix keeps the setup contract in
// lockstep with the wizard: all four Hub tools and every client supported by
// the registry must survive a save/load cycle. Continue is deliberately in
// this list because it was previously absent from the visible wizard.
func TestSetupSaveRoundTripsHubToolAndClientMatrix(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store, err := db.New(filepath.Join(t.TempDir(), "dwyt.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	tools := []string{"cbmcp", "obsidian", "headroom", "rtk"}
	clients := []string{"claude", "codex", "copilot", "kiro", "cursor", "opencode", "windsurf", "continue"}
	body, err := json.Marshal(Config{Tools: tools, Ias: clients, ProjectPath: "/workspace/project"})
	if err != nil {
		t.Fatal(err)
	}

	ds := &DashboardServer{Store: store}
	save := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(save)
	c.Request = httptest.NewRequest("POST", "/api/setup/save", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	ds.apiSetupSave(c)
	if save.Code != 200 {
		t.Fatalf("expected save HTTP 200, got %d: %s", save.Code, save.Body.String())
	}

	load := httptest.NewRecorder()
	c, _ = gin.CreateTestContext(load)
	c.Request = httptest.NewRequest("GET", "/api/setup/load", nil)
	ds.apiSetupLoad(c)
	if load.Code != 200 {
		t.Fatalf("expected load HTTP 200, got %d: %s", load.Code, load.Body.String())
	}
	var got Config
	if err := json.Unmarshal(load.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Tools, tools) {
		t.Fatalf("Hub tools changed across setup round trip: got %#v, want %#v", got.Tools, tools)
	}
	if !reflect.DeepEqual(got.Ias, clients) {
		t.Fatalf("clients changed across setup round trip: got %#v, want %#v", got.Ias, clients)
	}
}

// runInstall also persists setup when invoked by the asynchronous installer;
// it must update state.json independently of the API save that precedes it.
func TestRunInstallMigratesLegacyClientsIntoRuntimeState(t *testing.T) {
	home := t.TempDir()
	store, err := db.New(filepath.Join(home, "dwyt.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	bin := filepath.Join(home, "bin")
	binName := "dwyt"
	if runtime.GOOS == "windows" {
		binName += ".exe"
	}
	if err := os.MkdirAll(bin, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, binName), []byte("test dwyt binary"), 0755); err != nil {
		t.Fatal(err)
	}
	ds := &DashboardServer{
		Store:         store,
		RuntimeState:  state.Init(home),
		DwytBin:       bin,
		DwytHome:      home,
		installStatus: make(map[string]string),
	}

	ds.runInstall(Config{Clients: []string{"continue", "cursor"}}, false)
	if want := []string{"continue", "cursor"}; !reflect.DeepEqual(state.Init(home).Clients, want) {
		t.Fatalf("installer did not persist migrated clients to state: got %#v, want %#v", state.Init(home).Clients, want)
	}
}

// TestAPIServicesStartStopAllUpdatesRuntimeState exercises the bulk service
// controls with real managed helper processes. Dashboard status is driven by
// RuntimeState, so both successful starts and stops must be reflected there.
func TestAPIServicesStartStopAllUpdatesRuntimeState(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Setenv("DWYT_HEADROOM_STATS_HELPER", "1")
	t.Setenv("DWYT_DAEMON_HEALTHCHECK_TIMEOUT_SECONDS", "5")
	home := t.TempDir()
	pm := procman.New(home)
	codebasePort := reserveTestPort(t)
	headroomPort := reserveTestPort(t)
	helperArgs := []string{"-test.run=^TestHeadroomStatsProxyHelper$", "--", "--port", "{port}"}
	pm.Register("codebase", os.Args[0], "/health", codebasePort, helperArgs...)
	pm.Register("headroom", os.Args[0], "/health", headroomPort, helperArgs...)
	t.Cleanup(func() {
		_, _ = pm.Stop("codebase")
		_, _ = pm.Stop("headroom")
	})

	runtimeState := state.Init(home)
	ds := &DashboardServer{ProcMan: pm, RuntimeState: runtimeState, HeadroomPort: headroomPort}
	start := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(start)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/services/start-all", nil)
	ds.apiServicesStartAll(c)
	if start.Code != http.StatusOK {
		t.Fatalf("expected start HTTP 200, got %d: %s", start.Code, start.Body.String())
	}
	for _, name := range []string{"codebase", "headroom"} {
		proc, ok := runtimeState.GetProcess(name)
		if !ok || proc.PID == 0 || !proc.Healthy {
			t.Fatalf("%s missing or unhealthy in runtime state after start: %+v (exists=%t)", name, proc, ok)
		}
	}

	stop := httptest.NewRecorder()
	c, _ = gin.CreateTestContext(stop)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/services/stop-all", nil)
	ds.apiServicesStopAll(c)
	if stop.Code != http.StatusOK {
		t.Fatalf("expected stop HTTP 200, got %d: %s", stop.Code, stop.Body.String())
	}
	for _, name := range []string{"codebase", "headroom"} {
		if _, ok := runtimeState.GetProcess(name); ok {
			t.Fatalf("%s still present in runtime state after stop", name)
		}
	}
}

func reserveTestPort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	_, rawPort, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(rawPort)
	if err != nil {
		t.Fatal(err)
	}
	return port
}
