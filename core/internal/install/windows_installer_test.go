package install

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestWindowsInstallerStopsDaemonTreeBeforeReplacement is intentionally a
// source-level contract: the PowerShell installer is executed outside Go, but
// this test runs on every Go matrix target and prevents a future edit from
// reintroducing an in-place replacement of a live dwyt.exe.
func TestWindowsInstallerStopsDaemonTreeBeforeReplacement(t *testing.T) {
	t.Helper()
	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("could not locate test source")
	}
	installerPath := filepath.Clean(filepath.Join(filepath.Dir(sourceFile), "..", "..", "..", "install.ps1"))
	data, err := os.ReadFile(installerPath)
	if err != nil {
		t.Fatalf("read %s: %v", installerPath, err)
	}
	script := string(data)

	for _, want := range []string{
		"function Test-DwytDaemonProcess",
		"function Stop-DwytDaemon",
		"& taskkill.exe /F /T /PID $daemonPid",
		"Stop-DwytDaemon -DaemonPath $dest -DwytHome $dwytHome",
		"Copy-Item -Path $exe.FullName -Destination $dest -Force",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("Windows installer is missing required safe-upgrade behavior: %q", want)
		}
	}

	stopAt := strings.Index(script, "Stop-DwytDaemon -DaemonPath $dest -DwytHome $dwytHome")
	copyAt := strings.Index(script, "Copy-Item -Path $exe.FullName -Destination $dest -Force")
	if stopAt < 0 || copyAt < 0 || stopAt > copyAt {
		t.Fatal("installer must stop the daemon tree before replacing dwyt.exe")
	}
	if !strings.Contains(script, "$Process.ExecutablePath") || !strings.Contains(script, "$Process.CommandLine") {
		t.Fatal("installer must validate the PID belongs to the DWYT daemon before taskkill")
	}
}
