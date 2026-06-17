package procutil

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPIDFileRoundTrip(t *testing.T) {
	home := t.TempDir()

	if err := WritePID(home, "daemon", 1234); err != nil {
		t.Fatal(err)
	}
	if err := WritePID(home, "codebase", 5678); err != nil {
		t.Fatal(err)
	}

	if got := ReadPID(filepath.Join(PIDDir(home), "daemon.pid")); got != 1234 {
		t.Fatalf("ReadPID daemon = %d, want 1234", got)
	}

	pids := ListPIDs(home)
	if pids["daemon"] != 1234 || pids["codebase"] != 5678 {
		t.Fatalf("ListPIDs = %#v, want daemon=1234 codebase=5678", pids)
	}

	RemovePID(home, "daemon")
	if _, err := os.Stat(filepath.Join(PIDDir(home), "daemon.pid")); !os.IsNotExist(err) {
		t.Fatalf("daemon.pid should have been removed")
	}
	if pids := ListPIDs(home); len(pids) != 1 || pids["codebase"] != 5678 {
		t.Fatalf("after remove, ListPIDs = %#v, want only codebase", pids)
	}

	// Missing / malformed files read as 0, never panic.
	if got := ReadPID(filepath.Join(PIDDir(home), "nope.pid")); got != 0 {
		t.Fatalf("missing pid file = %d, want 0", got)
	}
}

func TestAlive(t *testing.T) {
	if !Alive(os.Getpid()) {
		t.Fatalf("current process should be reported alive")
	}
	if Alive(0) || Alive(-1) {
		t.Fatalf("invalid PIDs must not be alive")
	}
	// A very high PID is almost certainly not a running process.
	if Alive(2_000_000_000) {
		t.Fatalf("unused high PID should not be alive")
	}
}
