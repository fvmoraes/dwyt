package procutil

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPIDFileRoundTrip(t *testing.T) {
	home := t.TempDir()
	pid := os.Getpid()

	if err := WritePID(home, "daemon", pid); err != nil {
		t.Fatal(err)
	}
	if err := WritePID(home, "codebase", pid); err != nil {
		t.Fatal(err)
	}

	if got := ReadPID(filepath.Join(PIDDir(home), "daemon.pid")); got != pid {
		t.Fatalf("ReadPID daemon = %d, want %d", got, pid)
	}

	pids := ListPIDs(home)
	if pids["daemon"] != pid || pids["codebase"] != pid {
		t.Fatalf("ListPIDs = %#v, want daemon=%d codebase=%d", pids, pid, pid)
	}

	RemovePID(home, "daemon")
	if _, err := os.Stat(filepath.Join(PIDDir(home), "daemon.pid")); !os.IsNotExist(err) {
		t.Fatalf("daemon.pid should have been removed")
	}
	if pids := ListPIDs(home); len(pids) != 1 || pids["codebase"] != pid {
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
