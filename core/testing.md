# DWYT Testing Guide

This guide explains how to run and write tests for DWYT.

## Test memory in Obsidian

Before changing or running relevant validations, consult the project's
Obsidian vault for context. During an investigation, save technical
decisions as `decision` entries and validation status as `task` entries.
When finished, save the full context — request, summary, files, decisions,
actions, commands, errors, outcome, next steps — so future agents inherit
the state.

See also [`../docs/06-obsidian-law.md`](../docs/06-obsidian-law.md).

## 📋 Test Types

### 1. Unit tests

Individual components, isolated.

**Location:** `core/internal/*/`

**Run everything:**
```bash
cd core
go test ./... -shuffle=on
```

**Run one package:**
```bash
go test ./internal/procman -v
go test ./internal/state -v
go test ./internal/brain -v
```

**With coverage:**
```bash
go test ./... -cover
go test ./... -coverprofile=coverage.out
go tool cover -html=coverage.out
```

---

### 2. Integration tests

Component-to-component interaction.

**Run:**
```bash
go test ./internal/server -v -tags=integration
```

---

### 3. E2E tests

The complete system, end to end.

**Run:**
```bash
cd core
./test-e2e.sh
```

**What is covered:**
- Daemon startup and health
- Brain save, search, summarize
- Project switching
- Brain isolation between projects
- State persistence across restarts
- All API endpoints

---

## 🧪 Writing Tests

### Unit test structure

```go
package mypackage

import (
	"testing"
)

func TestMyFunction(t *testing.T) {
	// Arrange
	input := "test"
	expected := "expected result"

	// Act
	result := MyFunction(input)

	// Assert
	if result != expected {
		t.Errorf("Expected %s, got %s", expected, result)
	}
}
```

### Using t.TempDir()

For tests that need a filesystem:

```go
func TestWithFiles(t *testing.T) {
	tmpDir := t.TempDir() // cleaned up automatically

	filePath := filepath.Join(tmpDir, "test.txt")
	os.WriteFile(filePath, []byte("content"), 0644)

	// Test code...
}
```

### Testing concurrency

```go
func TestConcurrent(t *testing.T) {
	var wg sync.WaitGroup

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			// Test code...
		}(i)
	}

	wg.Wait()
	// Verify results...
}
```

### Testing HTTP endpoints

```go
func TestAPIEndpoint(t *testing.T) {
	// Start test server
	srv := setupTestServer(t)
	defer srv.Close()

	// Make request
	resp, err := http.Get(srv.URL + "/api/health")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	// Verify response
	if resp.StatusCode != 200 {
		t.Errorf("Expected 200, got %d", resp.StatusCode)
	}
}
```

---

## 🎯 Coverage

The CI matrix (`.github/workflows/ci.yml`) runs the full suite with
`-shuffle=on` on Linux, macOS and Windows, plus a dedicated race-detector
job. Coverage is a gap finder, not a target: cover behavior, not lines.
The packages with the strictest expectations are the concurrency-heavy ones
(`procman`, `state`, `server`, `housekeeper`).

---

## 🐛 Debugging Tests

### Verbose output

```bash
go test -v ./internal/procman
```

### Run a specific test

```bash
go test -v -run TestProcessManager_StartStop ./internal/procman
```

### Race detector

```bash
go test -race ./...
```

### Memory profiling

```bash
go test -memprofile=mem.prof ./internal/state
go tool pprof mem.prof
```

### CPU profiling

```bash
go test -cpuprofile=cpu.prof ./internal/brain
go tool pprof cpu.prof
```

---

## 🔍 Regression Tests

### Known cases

1. **Daemon startup racing the Codebase healthcheck** (PR #24)
   - Test: `TestWarmCodebaseDoesNotBlockCaller`
   - Proves the Codebase warmup never blocks the caller and the dashboard
     bind never waits for a failing service.

2. **Dashboard-first startup** (startup.go)
   - Tests: `TestDashboardServesWhileStartupTasksRun`,
     `TestStartupTaskFailureDoesNotKillDashboard`
   - Prove the dashboard answers while background startup tasks are still
     running and a failing task never kills the daemon.

3. **Service reconciler** (svcctl.go)
   - Tests: adoption, no-restart-while-running, bounded backoff,
     hysteresis
   - Pin the single-owner lifecycle: no double starts, no restart storms.

4. **Unknown is not offline** (handlers_status)
   - Tests: `TestEnrichSystemStatusCarriesRuntimeState` and friends
   - Prove the status payload carries the lifecycle state so the UI never
     collapses unknown into offline.

5. **State data loss**
   - Test: `TestRuntimeState_SaveFailureBackup`
   - Verifies a backup is created when a save fails.

---

## 📊 CI/CD Integration

CI lives in `.github/workflows/ci.yml`: a three-OS test matrix
(`fail-fast: false`), a race job on Linux/macOS, govulncheck and frontend
lint/build. Releases are handled by `release.yml` (scope-aware semver). Do
not duplicate that logic in ad-hoc scripts; extend the workflows instead.

---

## 🚀 Performance Tests

### Benchmarks

Follow the benchmark methodology: `-benchmem -count=10`, compare with
`benchstat`, never claim an improvement from a single run.

```go
func BenchmarkBrainSearch(b *testing.B) {
	brain := setupTestBrain(b)

	for b.Loop() {
		brain.Search("keyword")
	}
}
```

**Run:**
```bash
go test -bench=. -benchmem -count=10 ./internal/brain
```

### Load testing

```bash
# Install hey
go install github.com/rakyll/hey@latest

# Test an API endpoint
hey -n 1000 -c 10 http://127.0.0.1:2737/api/health
```

---

## ✅ Test Checklist

Before committing:

- [ ] All unit tests pass (`go test ./... -shuffle=on`)
- [ ] No data races detected (`go test -race`)
- [ ] Coverage did not regress
- [ ] E2E tests pass (if server/API changed)
- [ ] Frontend build and lint pass (if `core/web` changed)
- [ ] Documentation updated if behavior changed

Before a release:

- [ ] All tests pass on Linux/macOS/Windows (CI matrix green)
- [ ] Race job green
- [ ] Benchmarks did not regress (benchstat, when performance-related)
- [ ] Documentation complete

---

## 📚 Resources

- [Go Testing Package](https://pkg.go.dev/testing)
- [Table Driven Tests](https://go.dev/wiki/TableDrivenTests)
- [Go Race Detector](https://go.dev/doc/articles/race_detector)
- [testing/synctest](https://go.dev/blog/synctest) — deterministic time in tests (Go 1.25+)

---

## 🤝 Contributing Tests

### Priorities

1. **High:** tests for known critical bugs (regression tests first, red-green)
2. **Medium:** deepen coverage of concurrency-heavy packages
3. **Low:** edge cases

### Guidelines

- One test should test one thing
- Descriptive names: `TestFunctionName_Scenario_ExpectedBehavior`
- Use `t.Helper()` in test helpers
- Automatic cleanup with `t.Cleanup()` or `defer`
- No sleeps — synchronize on channels, state or `testing/synctest`;
  never on wall-clock timing

### Example of a good test

```go
func TestProcessManager_StartStop_ProcessIsKilled(t *testing.T) {
	t.Helper()

	// Arrange
	tmpDir := t.TempDir()
	pm := New(tmpDir)
	pm.Register("test", "/bin/sleep", "", 0, "10")

	// Act - Start
	status, err := pm.Start("test")
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// Assert - Running
	if !status.Running {
		t.Error("Expected process to be running")
	}

	// Act - Stop
	status, err = pm.Stop("test")
	if err != nil {
		t.Fatalf("Stop failed: %v", err)
	}

	// Assert - Stopped
	if status.Running {
		t.Error("Expected process to be stopped")
	}

	// Verify the process was actually killed
	time.Sleep(100 * time.Millisecond)
	proc, _ := os.FindProcess(status.PID)
	if err := proc.Signal(syscall.Signal(0)); err == nil {
		t.Error("Process should be dead but is still running")
	}
}
```

---

**Last updated:** 2026-09-10
