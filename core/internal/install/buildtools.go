package install

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// headroom-ai ships no Windows wheel (only macOS arm64 + manylinux x86_64/aarch64
// + sdist), so on Windows pip must compile it from source. It is a Rust/maturin
// project, so the build needs the Rust toolchain and the MSVC C++ build tools
// that provide link.exe. ensureWindowsBuildTools provisions both, best-effort,
// via winget; on platforms that already have a wheel it is a no-op.

// wingetInstallTimeout caps each winget install. The VS Build Tools package is
// multi-GB and slow on cold networks; 45 min is generous without hanging forever.
const wingetInstallTimeout = 45 * time.Minute

// ensureWindowsBuildTools makes sure Rust + the MSVC C++ build tools are present
// so the headroom-ai sdist can compile. It returns nil immediately off Windows
// or when both are already available. On Windows it tries to install whatever is
// missing through winget and, on success, makes the freshly installed cargo
// visible to the current process so the subsequent pip build can find it.
func ensureWindowsBuildTools() error {
	if runtime.GOOS != "windows" {
		return nil
	}

	haveRust := hasRust()
	haveMSVC := hasMSVCBuildTools()
	if haveRust && haveMSVC {
		return nil
	}

	if _, err := exec.LookPath("winget"); err != nil {
		return fmt.Errorf("headroom precisa da toolchain de build no Windows (Rust + VS Build Tools com C++),"+
			" mas o winget não está disponível para instalar automaticamente.\n%s", windowsToolchainHint())
	}

	if !haveRust {
		fmt.Println("  → headroom: instalando Rust (rustup) via winget…")
		if err := runWingetInstall("Rustlang.Rustup",
			"--override", "-y --default-toolchain stable --profile minimal"); err != nil {
			fmt.Printf("  ⚠ headroom: winget Rust falhou: %v\n", err)
		}
		ensureCargoOnPath()
	}

	if !haveMSVC {
		fmt.Println("  → headroom: instalando Visual Studio Build Tools (workload C++) via winget — download grande, pode pedir elevação (UAC)…")
		if err := runWingetInstall("Microsoft.VisualStudio.2022.BuildTools",
			"--override", "--quiet --wait --norestart --nocache --add Microsoft.VisualStudio.Workload.VCTools --includeRecommended"); err != nil {
			fmt.Printf("  ⚠ headroom: winget VS Build Tools falhou: %v\n", err)
		}
	}

	// Re-check after the install attempts.
	if !hasRust() {
		return fmt.Errorf("headroom: Rust ainda indisponível após a instalação automática.\n%s", windowsToolchainHint())
	}
	if !hasMSVCBuildTools() {
		return fmt.Errorf("headroom: MSVC C++ build tools (link.exe) ainda indisponíveis após a instalação automática.\n%s", windowsToolchainHint())
	}
	return nil
}

// hasRust reports whether a Rust toolchain (cargo) is reachable, either on PATH
// or at the default rustup location (~/.cargo/bin), since a fresh winget install
// won't be on the current process PATH yet.
func hasRust() bool {
	if _, err := exec.LookPath("cargo"); err == nil {
		return true
	}
	if dir := cargoBinDir(); dir != "" {
		if _, err := os.Stat(filepath.Join(dir, "cargo.exe")); err == nil {
			return true
		}
	}
	return false
}

func cargoBinDir() string {
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		return filepath.Join(home, ".cargo", "bin")
	}
	return ""
}

// ensureCargoOnPath prepends the rustup cargo bin dir to the current process
// PATH so the immediately-following pip→maturin→cargo build can resolve it
// without waiting for a new shell session.
func ensureCargoOnPath() {
	dir := cargoBinDir()
	if dir == "" {
		return
	}
	if _, err := os.Stat(dir); err != nil {
		return
	}
	path := os.Getenv("PATH")
	if strings.Contains(strings.ToLower(path), strings.ToLower(dir)) {
		return
	}
	os.Setenv("PATH", dir+string(os.PathListSeparator)+path)
}

// hasMSVCBuildTools reports whether the MSVC C++ build tools (which provide
// link.exe) are installed, detected via vswhere querying for the VC.Tools
// component. The Rust build (cc-rs) locates link.exe through the same mechanism,
// so the component being present is enough — it need not be on PATH.
func hasMSVCBuildTools() bool {
	vswhere := vswherePath()
	if vswhere == "" {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, vswhere,
		"-latest", "-products", "*",
		"-requires", "Microsoft.VisualStudio.Component.VC.Tools.x86.x64",
		"-property", "installationPath",
	).Output()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(out)) != ""
}

func vswherePath() string {
	for _, env := range []string{"ProgramFiles(x86)", "ProgramFiles"} {
		base := os.Getenv(env)
		if base == "" {
			continue
		}
		p := filepath.Join(base, "Microsoft Visual Studio", "Installer", "vswhere.exe")
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

// runWingetInstall runs `winget install -e --id <id> [extra args]` streaming
// output so the user sees progress, accepting the source/package agreements
// non-interactively.
func runWingetInstall(id string, extra ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), wingetInstallTimeout)
	defer cancel()
	args := append([]string{
		"install", "-e", "--id", id,
		"--accept-source-agreements", "--accept-package-agreements",
	}, extra...)
	cmd := exec.CommandContext(ctx, "winget", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return fmt.Errorf("winget install %s expirou após %s", id, wingetInstallTimeout)
		}
		return err
	}
	return nil
}

func windowsToolchainHint() string {
	return "  headroom-ai não publica binário (wheel) para Windows, então o pip compila do código-fonte (Rust).\n" +
		"  Para isso, instale a toolchain de build e rode o install novamente:\n" +
		"    winget install -e --id Rustlang.Rustup\n" +
		"    winget install -e --id Microsoft.VisualStudio.2022.BuildTools --override \"--quiet --wait --add Microsoft.VisualStudio.Workload.VCTools --includeRecommended\"\n" +
		"  Ou instale o Visual Studio Build Tools manualmente marcando \"Desktop development with C++\":\n" +
		"    https://visualstudio.microsoft.com/visual-cpp-build-tools/  e o Rust em https://rustup.rs\n" +
		"  As demais ferramentas do DWYT funcionam normalmente sem o headroom."
}
