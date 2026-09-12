//go:build linux

package procutil

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
)

func TestLinuxProcessIdentityUsesBootAndExecutableFileIdentity(t *testing.T) {
	pid := os.Getpid()
	identity, err := inspectProcess(pid)
	if err != nil {
		t.Fatal(err)
	}
	bootID, err := linuxBootID()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(identity.StartTimeToken, bootID+":") {
		t.Fatalf("start token %q is not scoped to boot %q", identity.StartTimeToken, bootID)
	}

	info, err := os.Stat(filepath.Join("/proc", strconv.Itoa(pid), "exe"))
	if err != nil {
		t.Fatal(err)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		t.Fatal("executable stat does not expose Linux file identity")
	}
	wantExecutable := fmt.Sprintf("%x:%x", stat.Dev, stat.Ino)
	if identity.ExecutableIdentity != wantExecutable {
		t.Fatalf("executable identity = %q, want %q", identity.ExecutableIdentity, wantExecutable)
	}
}
