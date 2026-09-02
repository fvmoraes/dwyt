package server

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/fvmoraes/dwyt/internal/procman"
	"github.com/fvmoraes/dwyt/internal/state"
	"github.com/fvmoraes/dwyt/internal/toolsource"
)

func TestCodebaseProcessArgsUseStandalonePortPlaceholder(t *testing.T) {
	args := codebaseProcessArgs()
	for _, arg := range args {
		if strings.Contains(arg, "{port}") && arg != "{port}" {
			t.Fatalf("embedded port placeholder will not be expanded by ProcessManager: %q in %q", arg, args)
		}
	}
	if len(args) < 2 || args[len(args)-2] != "--port" || args[len(args)-1] != "{port}" {
		t.Fatalf("codebase port arguments = %q, want trailing --port {port}", args)
	}
}

func TestApplyToolSourceProcessesIdenticalConfigPreservesRunningProcess(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "old-starts")
	oldPath := writeToolSourceTestLauncher(t, "old-headroom", marker, false)
	ds, previousSources := startToolSourceTestServer(t, oldPath)
	before := ds.ProcMan.Status("headroom")

	if err := ds.applyToolSourceProcesses(previousSources, Config{ToolSources: previousSources}); err != nil {
		t.Fatalf("apply identical tool sources: %v", err)
	}

	after := ds.ProcMan.Status("headroom")
	if !after.Running || !after.Healthy {
		t.Fatalf("headroom stopped after identical save: %+v", after)
	}
	if after.PID != before.PID {
		t.Fatalf("identical save restarted headroom: PID changed from %d to %d", before.PID, after.PID)
	}
	if starts := toolSourceTestStarts(t, marker); starts != 1 {
		t.Fatalf("identical save launched headroom %d times, want 1", starts)
	}
}

func TestApplyToolSourceProcessesChangedSourceRestartsRunningProcess(t *testing.T) {
	oldMarker := filepath.Join(t.TempDir(), "old-starts")
	newMarker := filepath.Join(t.TempDir(), "new-starts")
	oldPath := writeToolSourceTestLauncher(t, "old-headroom", oldMarker, false)
	newPath := writeToolSourceTestLauncher(t, "new-headroom", newMarker, false)
	ds, previousSources := startToolSourceTestServer(t, oldPath)
	before := ds.ProcMan.Status("headroom")
	nextSources := toolsource.NormalizeAll(map[string]toolsource.Selection{
		toolsource.ToolHeadroom: {Mode: toolsource.ModeExternal, Path: newPath},
	})

	if err := ds.applyToolSourceProcesses(previousSources, Config{ToolSources: nextSources}); err != nil {
		t.Fatalf("apply changed tool source: %v", err)
	}

	after := ds.ProcMan.Status("headroom")
	if !after.Running || !after.Healthy {
		t.Fatalf("headroom did not remain running after source change: %+v", after)
	}
	if after.PID == before.PID {
		t.Fatalf("source change did not replace the running process: PID remained %d", after.PID)
	}
	if starts := toolSourceTestStarts(t, newMarker); starts != 1 {
		t.Fatalf("new headroom source launched %d times, want 1", starts)
	}
	process, ok := ds.RuntimeState.GetProcess("headroom")
	if !ok || process.PID != after.PID || process.Port != after.Port || !process.Healthy {
		t.Fatalf("runtime state did not follow replacement: %+v (exists=%t), status=%+v", process, ok, after)
	}
}

func TestApplyToolSourceProcessesFailedStartRestoresPreviousRunningProcess(t *testing.T) {
	t.Setenv("DWYT_DAEMON_HEALTHCHECK_TIMEOUT_SECONDS", "1")
	oldMarker := filepath.Join(t.TempDir(), "old-starts")
	failingMarker := filepath.Join(t.TempDir(), "failing-starts")
	oldPath := writeToolSourceTestLauncher(t, "old-headroom", oldMarker, false)
	failingPath := writeToolSourceTestLauncher(t, "failing-headroom", failingMarker, true)
	ds, previousSources := startToolSourceTestServer(t, oldPath)
	nextSources := toolsource.NormalizeAll(map[string]toolsource.Selection{
		toolsource.ToolHeadroom: {Mode: toolsource.ModeExternal, Path: failingPath},
	})

	err := ds.applyToolSourceProcesses(previousSources, Config{ToolSources: nextSources})
	if err == nil || !strings.Contains(err.Error(), "previous source restored") {
		t.Fatalf("failed replacement error = %v, want restored-source context", err)
	}

	after := ds.ProcMan.Status("headroom")
	if !after.Running || !after.Healthy {
		t.Fatalf("previous headroom source was not restored: %+v", after)
	}
	if starts := toolSourceTestStarts(t, oldMarker); starts != 2 {
		t.Fatalf("previous source launch count = %d, want 2 after rollback", starts)
	}
	if starts := toolSourceTestStarts(t, failingMarker); starts != 1 {
		t.Fatalf("failing source launch count = %d, want 1", starts)
	}
	process, ok := ds.RuntimeState.GetProcess("headroom")
	if !ok || process.PID != after.PID || process.Port != after.Port || !process.Healthy {
		t.Fatalf("runtime state did not follow rollback: %+v (exists=%t), status=%+v", process, ok, after)
	}
}

func TestApplyToolSourceProcessesRollsBackEarlierServiceWhenLaterServiceFails(t *testing.T) {
	t.Setenv("DWYT_TOOL_SOURCE_PROCESS_HELPER", "1")
	t.Setenv("DWYT_DAEMON_HEALTHCHECK_TIMEOUT_SECONDS", "1")
	oldCodebaseMarker := filepath.Join(t.TempDir(), "old-codebase-starts")
	newCodebaseMarker := filepath.Join(t.TempDir(), "new-codebase-starts")
	oldHeadroomMarker := filepath.Join(t.TempDir(), "old-headroom-starts")
	failingHeadroomMarker := filepath.Join(t.TempDir(), "failing-headroom-starts")
	oldCodebasePath := writeToolSourceTestLauncher(t, "old-codebase", oldCodebaseMarker, false)
	newCodebasePath := writeToolSourceTestLauncher(t, "new-codebase", newCodebaseMarker, false)
	oldHeadroomPath := writeToolSourceTestLauncher(t, "old-headroom", oldHeadroomMarker, false)
	failingHeadroomPath := writeToolSourceTestLauncher(t, "failing-headroom", failingHeadroomMarker, true)
	previousSources := toolsource.NormalizeAll(map[string]toolsource.Selection{
		toolsource.ToolCodebase: {Mode: toolsource.ModeExternal, Path: oldCodebasePath},
		toolsource.ToolHeadroom: {Mode: toolsource.ModeExternal, Path: oldHeadroomPath},
	})
	nextSources := toolsource.NormalizeAll(map[string]toolsource.Selection{
		toolsource.ToolCodebase: {Mode: toolsource.ModeExternal, Path: newCodebasePath},
		toolsource.ToolHeadroom: {Mode: toolsource.ModeExternal, Path: failingHeadroomPath},
	})

	home := t.TempDir()
	codebasePort := reserveTestPort(t)
	headroomPort := reserveTestPort(t)
	for headroomPort == codebasePort {
		headroomPort = reserveTestPort(t)
	}
	runtimeState := state.Init(home)
	runtimeState.SetToolSources(previousSources)
	pm := procman.New(home)
	pm.Register("codebase", oldCodebasePath, "/health", codebasePort, codebaseProcessArgs()...)
	pm.Register("headroom", oldHeadroomPath, "/health", headroomPort, "proxy", "--port", "{port}")
	ds := &DashboardServer{
		DwytBin:      t.TempDir(),
		DwytHome:     home,
		ProcMan:      pm,
		RuntimeState: runtimeState,
		HeadroomPort: headroomPort,
	}
	t.Cleanup(func() {
		_, _ = pm.Stop("codebase")
		_, _ = pm.Stop("headroom")
	})
	codebaseStatus, err := pm.Start("codebase")
	if err != nil {
		t.Fatalf("start previous codebase source: %v", err)
	}
	ds.recordToolProcess("codebase", codebaseStatus)
	headroomStatus, err := ds.startHeadroom()
	if err != nil {
		t.Fatalf("start previous headroom source: %v", err)
	}
	ds.recordToolProcess("headroom", headroomStatus)

	err = ds.applyToolSourceProcesses(previousSources, Config{ToolSources: nextSources})
	if err == nil || !strings.Contains(err.Error(), "previous source restored") {
		t.Fatalf("two-service transition error = %v, want restored-source context", err)
	}

	for _, service := range []string{"codebase", "headroom"} {
		status := pm.Status(service)
		if !status.Running || !status.Healthy {
			t.Fatalf("%s was not running after transaction rollback: %+v", service, status)
		}
		process, ok := runtimeState.GetProcess(service)
		if !ok || process.PID != status.PID || process.Port != status.Port || !process.Healthy {
			t.Fatalf("runtime state for %s did not follow rollback: %+v (exists=%t), status=%+v", service, process, ok, status)
		}
	}
	for marker, want := range map[string]int{
		oldCodebaseMarker:     2,
		newCodebaseMarker:     1,
		oldHeadroomMarker:     2,
		failingHeadroomMarker: 1,
	} {
		if starts := toolSourceTestStarts(t, marker); starts != want {
			t.Fatalf("launcher %s start count = %d, want %d", filepath.Base(marker), starts, want)
		}
	}
}

func startToolSourceTestServer(t *testing.T, headroomPath string) (*DashboardServer, map[string]toolsource.Selection) {
	t.Helper()
	t.Setenv("DWYT_TOOL_SOURCE_PROCESS_HELPER", "1")
	if os.Getenv("DWYT_DAEMON_HEALTHCHECK_TIMEOUT_SECONDS") == "" {
		t.Setenv("DWYT_DAEMON_HEALTHCHECK_TIMEOUT_SECONDS", "5")
	}
	home := t.TempDir()
	binDir := t.TempDir()
	port := reserveTestPort(t)
	previousSources := toolsource.NormalizeAll(map[string]toolsource.Selection{
		toolsource.ToolHeadroom: {Mode: toolsource.ModeExternal, Path: headroomPath},
	})
	runtimeState := state.Init(home)
	runtimeState.SetToolSources(previousSources)
	pm := procman.New(home)
	pm.Register("codebase", toolsource.ManagedPath(binDir, toolsource.ToolCodebase), "/health", 9749, codebaseProcessArgs()...)
	pm.Register("headroom", headroomPath, "/health", port, "proxy", "--port", "{port}")
	ds := &DashboardServer{
		DwytBin:      binDir,
		DwytHome:     home,
		ProcMan:      pm,
		RuntimeState: runtimeState,
		HeadroomPort: port,
	}
	t.Cleanup(func() {
		_, _ = pm.Stop("codebase")
		_, _ = pm.Stop("headroom")
	})
	status, err := ds.startHeadroom()
	if err != nil {
		t.Fatalf("start previous headroom source: %v", err)
	}
	if status == nil || !status.Running || !status.Healthy {
		t.Fatalf("previous headroom source did not become healthy: %+v", status)
	}
	ds.recordToolProcess("headroom", status)
	return ds, previousSources
}

func writeToolSourceTestLauncher(t *testing.T, name, marker string, fail bool) string {
	t.Helper()
	extension := ""
	if runtime.GOOS == "windows" {
		extension = ".bat"
	}
	path := filepath.Join(t.TempDir(), name+extension)
	var content string
	if runtime.GOOS == "windows" {
		content = fmt.Sprintf("@echo off\r\necho started>>\"%s\"\r\n", marker)
		if fail {
			content += "exit /b 1\r\n"
		} else {
			content += fmt.Sprintf("\"%s\" -test.run=^TestToolSourceProcessHelper$ -- %%*\r\n", os.Args[0])
		}
	} else {
		content = fmt.Sprintf("#!/bin/sh\nprintf 'started\\n' >> %q\n", marker)
		if fail {
			content += "exit 1\n"
		} else {
			content += fmt.Sprintf("exec %q -test.run=^TestToolSourceProcessHelper$ -- \"$@\"\n", os.Args[0])
		}
	}
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func toolSourceTestStarts(t *testing.T, marker string) int {
	t.Helper()
	content, err := os.ReadFile(marker)
	if err != nil {
		t.Fatalf("read launcher marker: %v", err)
	}
	return len(strings.Fields(string(content)))
}

func TestToolSourceProcessHelper(t *testing.T) {
	if os.Getenv("DWYT_TOOL_SOURCE_PROCESS_HELPER") != "1" {
		return
	}
	port := toolSourceHelperPort(t, os.Args)
	listener, err := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(port))
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	if err := http.Serve(listener, mux); err != nil {
		t.Fatal(err)
	}
}

func toolSourceHelperPort(t *testing.T, args []string) int {
	t.Helper()
	for i, arg := range args {
		if value, ok := strings.CutPrefix(arg, "--port="); ok {
			port, err := strconv.Atoi(value)
			if err == nil && port > 0 {
				return port
			}
		}
		if arg == "--port" && i+1 < len(args) {
			port, err := strconv.Atoi(args[i+1])
			if err == nil && port > 0 {
				return port
			}
		}
	}
	t.Fatalf("helper port not found in arguments: %q", args)
	return 0
}
