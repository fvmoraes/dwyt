package env

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestWindowsEnvContentIncludesHeadroomAndPathParity(t *testing.T) {
	content := windowsEnvContent(`C:\Users\Ana O'Brien\AppData\Roaming\dwyt`, `C:\Users\Ana O'Brien\AppData\Roaming\dwyt\bin`, `C:\Users\Ana O'Brien\AppData\Roaming\dwyt\data`, 8788)
	for _, want := range []string{
		"$env:XDG_CACHE_HOME = 'C:\\Users\\Ana O''Brien\\AppData\\Roaming\\dwyt\\data'",
		"$env:DWYT_HOME = 'C:\\Users\\Ana O''Brien\\AppData\\Roaming\\dwyt'",
		"$env:PATH = 'C:\\Users\\Ana O''Brien\\AppData\\Roaming\\dwyt\\bin' + ';' + $env:PATH",
		"$env:HEADROOM_PORT = '8788'",
		"$env:OPENAI_BASE_URL = 'http://127.0.0.1:8788/v1'",
		"$env:ANTHROPIC_BASE_URL = 'http://127.0.0.1:8788'",
		"$env:CBM_CACHE_DIR = 'C:\\Users\\Ana O''Brien\\AppData\\Roaming\\dwyt\\codebase'",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("Windows env content missing %q:\n%s", want, content)
		}
	}
	if strings.Contains(content, `C:\\Users`) {
		t.Fatalf("PowerShell content must use literal Windows separators, got:\n%s", content)
	}
	if got, want := powerShellProfileSourceLine(`C:\Users\Ana O'Brien\AppData\Roaming\dwyt\env.ps1`), `. 'C:\Users\Ana O''Brien\AppData\Roaming\dwyt\env.ps1'`; got != want {
		t.Fatalf("PowerShell profile source line = %q, want %q", got, want)
	}
}

func TestWindowsPathContainsComparesWholeSegments(t *testing.T) {
	if windowsPathContains(`C:\dwyt\binary;C:\Tools`, `C:\dwyt\bin`) {
		t.Fatal("substring must not be treated as an existing PATH entry")
	}
	if !windowsPathContains(`C:\Tools; c:\DWYT\BIN ;C:\Other`, `C:\dwyt\bin`) {
		t.Fatal("case-insensitive full PATH entry should match")
	}
}

func TestParseWindowsUserPathPreservesSpacedEntries(t *testing.T) {
	output := "\r\nHKEY_CURRENT_USER\\Environment\r\n    PATH\tREG_EXPAND_SZ\tC:\\Program Files\\DWYT;C:\\Tools\r\n"
	if got, want := parseWindowsUserPath(output), `C:\Program Files\DWYT;C:\Tools`; got != want {
		t.Fatalf("parsed PATH = %q, want %q", got, want)
	}
}

func TestPowerShellProfilesCoverWindowsPowerShellAndPowerShell7(t *testing.T) {
	profiles := powerShellProfilesForHome(`C:\Users\Ana`)
	want := []string{
		filepath.Join(`C:\Users\Ana`, "Documents", "PowerShell", "Microsoft.PowerShell_profile.ps1"),
		filepath.Join(`C:\Users\Ana`, "Documents", "WindowsPowerShell", "Microsoft.PowerShell_profile.ps1"),
	}
	if len(profiles) != len(want) {
		t.Fatalf("profiles = %v, want %v", profiles, want)
	}
	for i := range want {
		if profiles[i] != want[i] {
			t.Fatalf("profile[%d] = %q, want %q", i, profiles[i], want[i])
		}
	}
}

func TestUnixEnvContentQuotesShellPaths(t *testing.T) {
	content := unixEnvContent(`/tmp/O'Brien $home/dwyt`, `/tmp/O'Brien $home/dwyt/bin dir`, `/tmp/O'Brien $home/dwyt/data`, 8788)
	for _, want := range []string{
		"export XDG_CACHE_HOME='/tmp/O'\"'\"'Brien $home/dwyt/data'",
		"export DWYT_HOME='/tmp/O'\"'\"'Brien $home/dwyt'",
		"export PATH='/tmp/O'\"'\"'Brien $home/dwyt/bin dir':$PATH",
		"export CBM_CACHE_DIR='/tmp/O'\"'\"'Brien $home/dwyt/codebase'",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("Unix env content missing %q:\n%s", want, content)
		}
	}
	if got, want := unixSourceLine(`/tmp/O'Brien $home/dwyt/env.sh`), `[[ -f '/tmp/O'"'"'Brien $home/dwyt/env.sh' ]] && source '/tmp/O'"'"'Brien $home/dwyt/env.sh'`; got != want {
		t.Fatalf("Unix source line = %q, want %q", got, want)
	}
}

func TestUnixEnvContentCanBeSourcedByPOSIXShell(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell is not available on the Windows runner")
	}

	dwytHome := `/tmp/O'Brien $home/dwyt`
	dwytBin := `/tmp/O'Brien $home/dwyt/bin dir`
	file := filepath.Join(t.TempDir(), "env.sh")
	if err := os.WriteFile(file, []byte(unixEnvContent(dwytHome, dwytBin, "/tmp/data", 8788)), 0600); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command("/bin/sh", "-c", `. "$1"; printf '%s\n%s\n%s\n' "$DWYT_HOME" "$PATH" "$CBM_CACHE_DIR"`, "sh", file)
	cmd.Env = append(os.Environ(), "PATH=/usr/bin:/bin")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("source generated env.sh: %v\n%s", err, output)
	}
	lines := strings.Split(strings.TrimSuffix(string(output), "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("sourced values = %q, want three lines", output)
	}
	if lines[0] != dwytHome {
		t.Fatalf("DWYT_HOME = %q, want %q", lines[0], dwytHome)
	}
	if lines[1] != dwytBin+":/usr/bin:/bin" {
		t.Fatalf("PATH = %q, want generated bin prepended", lines[1])
	}
	if lines[2] != dwytHome+"/codebase" {
		t.Fatalf("CBM_CACHE_DIR = %q, want POSIX path", lines[2])
	}
}

func TestUpdateHeadroomEnvContentPropagatesFallbackPort(t *testing.T) {
	unix := updateHeadroomEnvContent(unixEnvContent("/dwyt", "/dwyt/bin", "/dwyt/data", 8787), 8788, false)
	for _, want := range []string{
		"export HEADROOM_PORT=8788",
		"export OPENAI_BASE_URL=http://127.0.0.1:8788/v1",
		"export ANTHROPIC_BASE_URL=http://127.0.0.1:8788",
	} {
		if !strings.Contains(unix, want) {
			t.Fatalf("Unix env content missing %q:\n%s", want, unix)
		}
	}
	if strings.Contains(unix, "8787") {
		t.Fatalf("Unix env still references old port:\n%s", unix)
	}

	windows := updateHeadroomEnvContent(windowsEnvContent(`C:\dwyt`, `C:\dwyt\bin`, `C:\dwyt\data`, 8787), 8788, true)
	for _, want := range []string{
		"$env:HEADROOM_PORT = '8788'",
		"$env:OPENAI_BASE_URL = 'http://127.0.0.1:8788/v1'",
		"$env:ANTHROPIC_BASE_URL = 'http://127.0.0.1:8788'",
	} {
		if !strings.Contains(windows, want) {
			t.Fatalf("Windows env content missing %q:\n%s", want, windows)
		}
	}
	if strings.Contains(windows, "8787") {
		t.Fatalf("Windows env still references old port:\n%s", windows)
	}
}

func TestSetHeadroomPortUpdatesRuntimeAndManagedEnvFile(t *testing.T) {
	dir := t.TempDir()
	name := "env.sh"
	content := unixEnvContent(dir, filepath.Join(dir, "bin"), filepath.Join(dir, "data"), 8787)
	if runtime.GOOS == "windows" {
		name = "env.ps1"
		content = windowsEnvContent(dir, filepath.Join(dir, "bin"), filepath.Join(dir, "data"), 8787)
	}
	file := filepath.Join(dir, name)
	if err := os.WriteFile(file, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HEADROOM_PORT", "8787")
	t.Setenv("OPENAI_BASE_URL", "http://127.0.0.1:8787/v1")
	t.Setenv("ANTHROPIC_BASE_URL", "http://127.0.0.1:8787")

	if err := SetHeadroomPort(dir, 8788); err != nil {
		t.Fatalf("SetHeadroomPort: %v", err)
	}
	if got := os.Getenv("HEADROOM_PORT"); got != "8788" {
		t.Fatalf("runtime HEADROOM_PORT = %q, want 8788", got)
	}
	updated, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(updated), "8787") || !strings.Contains(string(updated), "8788") {
		t.Fatalf("managed env file did not receive fallback port:\n%s", updated)
	}
}

func TestCopyFileSamePathDoesNotTruncateRunningSource(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dwyt.exe")
	want := []byte("running-dwyt-binary-content")
	if err := os.WriteFile(path, want, 0755); err != nil {
		t.Fatal(err)
	}

	if err := copyFile(path, path); err != nil {
		t.Fatalf("self copy: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("self copy changed source: got %q, want %q", got, want)
	}
}

func TestCopyFileHardLinkDestinationDoesNotTruncate(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "source.exe")
	dst := filepath.Join(dir, "dwyt.exe")
	want := []byte("hard-linked-running-dwyt")
	if err := os.WriteFile(src, want, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(src, dst); err != nil {
		t.Skipf("hard links unavailable on this filesystem: %v", err)
	}

	if err := copyFile(src, dst); err != nil {
		t.Fatalf("hard-link self copy: %v", err)
	}
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("hard-link copy changed executable: got %q, want %q", got, want)
	}
}

func TestCopyFileReplacesDestinationAfterCompleteCopy(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "new.exe")
	dst := filepath.Join(dir, "dwyt.exe")
	want := []byte("new-complete-executable")
	if err := os.WriteFile(src, want, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, []byte("old-executable"), 0755); err != nil {
		t.Fatal(err)
	}

	if err := copyFile(src, dst); err != nil {
		t.Fatalf("replace executable: %v", err)
	}
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("destination = %q, want %q", got, want)
	}
}
