package install

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// pipInstallTimeout caps each pip call. The headroom-ai[proxy] install
// pulls ~50 wheels (onnxruntime, transformers, cryptography, …) — on a
// healthy network it finishes in 1-3 min. 10 min is generous enough for
// slow links and protects against network stalls hanging the wizard.
const pipInstallTimeout = 10 * time.Minute

// Headroom installs headroom-ai into a dedicated Python venv at
// dwytHome/headroom-venv and exposes a wrapper at dwytBin/headroom.
// Idempotent: any partial earlier install is cleared before starting.
func Headroom(dwytBin, dwytHome string) error {
	wrapperPath := filepath.Join(dwytBin, headroomWrapperName())
	venvDir := filepath.Join(dwytHome, "headroom-venv")
	if err := os.MkdirAll(dwytHome, 0755); err != nil {
		return fmt.Errorf("headroom: cannot create %s: %w", dwytHome, err)
	}

	cleanPartialHeadroom(wrapperPath, venvDir)

	python, err := findCompatiblePythonCommand()
	if err != nil {
		return fmt.Errorf("headroom: %w", err)
	}
	fmt.Printf("  → headroom venv (%s)...\n", python)
	if out, vErr := runPythonFromHome(dwytHome, python, "-m", "venv", venvDir); vErr != nil {
		return fmt.Errorf("headroom: venv creation failed: %w\n%s", vErr, string(out))
	}

	pipBin, pyBin, hrBin := venvBinaries(venvDir)
	if err := ensurePipInVenv(dwytHome, pipBin, pyBin); err != nil {
		return err
	}
	if err := pipInstallHeadroom(dwytHome, pyBin); err != nil {
		return err
	}
	if _, err := os.Stat(hrBin); err != nil {
		return fmt.Errorf("headroom: binary not found at %s after install", hrBin)
	}
	return writeHeadroomWrapper(hrBin, wrapperPath)
}

func headroomWrapperName() string {
	if runtime.GOOS == "windows" {
		return "headroom.bat"
	}
	return "headroom"
}

// cleanPartialHeadroom removes leftovers from an aborted previous attempt.
// Without it, retries failed with "venv has no pip" because the broken
// state was inherited.
func cleanPartialHeadroom(wrapperPath, venvDir string) {
	os.Remove(wrapperPath)
	os.RemoveAll(venvDir)
}

func venvBinaries(venvDir string) (pipBin, pyBin, hrBin string) {
	if runtime.GOOS == "windows" {
		return filepath.Join(venvDir, "Scripts", "pip.exe"),
			filepath.Join(venvDir, "Scripts", "python.exe"),
			filepath.Join(venvDir, "Scripts", "headroom.exe")
	}
	return filepath.Join(venvDir, "bin", "pip"),
		filepath.Join(venvDir, "bin", "python"),
		filepath.Join(venvDir, "bin", "headroom")
}

// ensurePipInVenv covers Python builds (notably Homebrew bleeding-edge)
// that ship venvs without pip. Bootstrap via ensurepip before any
// subsequent pip install runs.
func ensurePipInVenv(workDir, pipBin, pyBin string) error {
	if _, err := os.Stat(pipBin); err == nil {
		return nil
	}
	if out, err := runFromHome(workDir, pyBin, "-m", "ensurepip", "--upgrade"); err != nil {
		return fmt.Errorf("headroom: pip missing from venv and ensurepip failed: %w\n%s", err, string(out))
	}
	return nil
}

// pipInstallHeadroom installs headroom-ai, preferring a prebuilt wheel and only
// falling back to a source build when no compatible wheel exists for the
// platform. The wheel path needs no build toolchain (it covers modern Linux
// x86_64/arm64 and macOS Apple Silicon). The source path — required on Windows,
// macOS Intel, and old/musl Linux, since headroom-ai is a Rust/maturin project
// — first provisions the native toolchain (Rust + C/C++ linker) where it can.
//
// `python -m pip` is used instead of the pip binary directly: `pip install
// --upgrade pip` fails with OSError "[Errno 2] No such file or directory" when
// pip tries to overwrite its own script while running; loading pip as a module
// avoids that conflict.
func pipInstallHeadroom(workDir, pyBin string) error {
	if err := runPip(workDir, pyBin, "upgrade pip", "install", "--upgrade", "pip"); err != nil {
		return err
	}

	// Preferred: install headroom-ai from a prebuilt wheel (no toolchain). The
	// --only-binary scope is limited to headroom-ai so its dependencies may
	// still come from sdists as usual; we just refuse to *build headroom-ai
	// itself* from source here.
	if err := runPip(workDir, pyBin, "pip install headroom-ai[proxy] (wheel)",
		"install", "--only-binary=headroom-ai", "headroom-ai[proxy]"); err == nil {
		return nil
	}

	// Fallback: no compatible wheel for this platform — build from source.
	fmt.Println("  → headroom: sem wheel para esta plataforma; compilando do código-fonte…")
	if err := ensureWindowsBuildTools(); err != nil {
		return err
	}
	if err := runPip(workDir, pyBin, "pip install headroom-ai[proxy] (source)",
		"install", "headroom-ai[proxy]"); err != nil {
		if runtime.GOOS == "windows" {
			return fmt.Errorf("%w\n%s", err, windowsToolchainHint())
		}
		return err
	}
	return nil
}

// runPip runs `python -m pip <args...>` with a timeout, anchoring the
// child process at workDir. Anchoring matters because dwyt may be
// launched from a transient cwd (e.g. when piped via curl|bash from a
// tmpdir that gets cleaned up); pip then fails to resolve the cwd with
// "FileNotFoundError: [Errno 2] No such file or directory" before any
// install work begins. workDir must be a stable, existing directory
// (typically dwytHome).
func runPip(workDir, pyBin, label string, args ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), pipInstallTimeout)
	defer cancel()
	full := append([]string{"-m", "pip"}, args...)
	cmd := exec.CommandContext(ctx, pyBin, full...)
	cmd.Dir = safeWorkDir(workDir)
	out, err := cmd.CombinedOutput()
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return fmt.Errorf("headroom: %s timed out after %s (slow network?):\n%s", label, pipInstallTimeout, string(out))
	}
	if err != nil {
		return fmt.Errorf("headroom: %s failed: %w\n%s", label, err, string(out))
	}
	return nil
}

// runFromHome wraps exec.Command + CombinedOutput, anchoring cwd to a
// stable directory. Same rationale as runPip but for non-pip subprocesses
// invoked during headroom bootstrap (venv creation, ensurepip).
func runFromHome(workDir, bin string, args ...string) ([]byte, error) {
	cmd := exec.Command(bin, args...)
	cmd.Dir = safeWorkDir(workDir)
	return cmd.CombinedOutput()
}

// runPythonFromHome is the command-aware version of runFromHome. It preserves
// launcher arguments such as Windows' `py -3.12` while still anchoring the
// child process in a stable work directory.
func runPythonFromHome(workDir string, python pythonCommand, args ...string) ([]byte, error) {
	cmd := exec.Command(python.bin, python.commandArgs(args...)...)
	cmd.Dir = safeWorkDir(workDir)
	return cmd.CombinedOutput()
}

// safeWorkDir picks a guaranteed-existing directory for child processes.
// dwytHome is preferred (always created by the caller); falls back to
// $HOME and finally "/" so we never hand a non-existent cwd to a child.
func safeWorkDir(preferred string) string {
	if preferred != "" {
		if info, err := os.Stat(preferred); err == nil && info.IsDir() {
			return preferred
		}
	}
	if home, err := os.UserHomeDir(); err == nil {
		if info, err := os.Stat(home); err == nil && info.IsDir() {
			return home
		}
	}
	return string(filepath.Separator)
}

// writeHeadroomWrapper drops a callable shim (symlink on POSIX, .bat on
// Windows) pointing at the venv-internal binary.
func writeHeadroomWrapper(hrBin, wrapperPath string) error {
	if runtime.GOOS == "windows" {
		return os.WriteFile(wrapperPath, []byte(headroomBatchWrapper(hrBin)), 0644)
	}
	return os.Symlink(hrBin, wrapperPath)
}

// headroomBatchWrapper uses cmd.exe quoting rather than Go's %q. Go escapes
// every Windows separator (C:\\Users), while cmd.exe keeps those backslashes
// literal and may therefore try to execute a non-existent path. Percent signs
// are doubled because this string is evaluated as a batch file.
func headroomBatchWrapper(hrBin string) string {
	quotedPath := `"` + strings.ReplaceAll(hrBin, "%", "%%") + `"`
	return "@echo off\r\n" + quotedPath + " %*\r\n"
}
