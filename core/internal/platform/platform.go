// Package platform is the single front door for OS-specific behaviour. It
// centralizes path layout, executable discovery, process control, and
// "open in OS" actions so the rest of the codebase never has to branch on
// runtime.GOOS directly. Cross-platform by construction:
//
//	Linux/macOS → ~/.dwyt, POSIX signals, xdg-open/open
//	Windows     → %APPDATA%\dwyt, taskkill, cmd /c start, .exe suffixes
package platform

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/fvmoraes/dwyt/internal/procutil"
)

// ── OS predicates ──────────────────────────────────────────────────────────

func IsWindows() bool { return runtime.GOOS == "windows" }
func IsMacOS() bool   { return runtime.GOOS == "darwin" }
func IsLinux() bool   { return runtime.GOOS == "linux" }

// ── Directory layout ─────────────────────────────────────────────────────────

// GetUserHome returns the current user's home directory, with Windows and
// Unix fallbacks if the standard lookup fails.
func GetUserHome() string {
	if h, err := os.UserHomeDir(); err == nil && h != "" {
		return h
	}
	if IsWindows() {
		if up := os.Getenv("USERPROFILE"); up != "" {
			return up
		}
	}
	return os.Getenv("HOME")
}

// GetDWYTDir returns the DWYT home directory, honouring the DWYT_HOME override.
//
//	Windows → %APPDATA%\dwyt   (e.g. C:\Users\<user>\AppData\Roaming\dwyt)
//	Unix    → ~/.dwyt
func GetDWYTDir() string {
	if h := os.Getenv("DWYT_HOME"); h != "" {
		return h
	}
	if IsWindows() {
		appData := os.Getenv("APPDATA")
		if appData == "" {
			appData = filepath.Join(GetUserHome(), "AppData", "Roaming")
		}
		return filepath.Join(appData, "dwyt")
	}
	return filepath.Join(GetUserHome(), ".dwyt")
}

func GetBinDir() string    { return filepath.Join(GetDWYTDir(), "bin") }
func GetDataDir() string   { return filepath.Join(GetDWYTDir(), "data") }
func GetConfigDir() string { return filepath.Join(GetDWYTDir(), "config") }
func GetLogDir() string    { return filepath.Join(GetDWYTDir(), "logs") }
func GetRunDir() string    { return procutil.PIDDir(GetDWYTDir()) }

// ── Executables ──────────────────────────────────────────────────────────────

// ExecutableName appends the platform's executable suffix (.exe on Windows).
func ExecutableName(base string) string {
	if IsWindows() && !strings.HasSuffix(strings.ToLower(base), ".exe") {
		return base + ".exe"
	}
	return base
}

// GetExecutablePath returns the absolute path of a DWYT-managed binary inside
// the bin directory, with the correct extension for the platform.
func GetExecutablePath(name string) string {
	return filepath.Join(GetBinDir(), ExecutableName(name))
}

// LookPath finds an executable on PATH (cross-platform via exec.LookPath).
func LookPath(name string) (string, bool) {
	p, err := exec.LookPath(name)
	if err != nil {
		return "", false
	}
	return p, true
}

// ── Process control ──────────────────────────────────────────────────────────

// StopProcess terminates a PID cross-platform (SIGTERM→SIGKILL on Unix,
// taskkill /F /T on Windows).
func StopProcess(pid int) error { return procutil.Terminate(pid) }

// ProcessAlive reports whether a PID is a live process.
func ProcessAlive(pid int) bool { return procutil.Alive(pid) }

// DetachProcess configures a command to run detached from the parent's job
// control / console so it keeps running after the launcher exits. The
// platform-specific attributes live in proc_unix.go / proc_windows.go.
func DetachProcess(cmd *exec.Cmd) { detach(cmd) }

// ── Open in OS ───────────────────────────────────────────────────────────────

// OpenURL opens a URL in the default browser.
func OpenURL(url string) error { return opener(url).Start() }

// OpenPath opens a file or directory in the OS file manager.
func OpenPath(path string) error { return opener(path).Start() }

func opener(target string) *exec.Cmd {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", target)
	case "windows":
		// The empty title arg lets `start` treat a quoted target correctly.
		return exec.Command("cmd", "/c", "start", "", target)
	default:
		return exec.Command("xdg-open", target)
	}
}
