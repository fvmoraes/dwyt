package install

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

// RTK installs the rtk binary into dwytBin and runs `rtk init --global` to
// register the shell hooks.
//
// It downloads the prebuilt release archive natively in Go (with SHA-256
// verification) — no shell/curl/tar needed. rtk now publishes an official
// Windows build (rtk-x86_64-pc-windows-msvc.zip), so Windows is handled the
// same way as Linux/macOS. If the download fails we fall back to an rtk
// already on disk (homebrew/manual install/user-provided rtk.exe).
func RTK(dwytBin string) error {
	binPath := filepath.Join(dwytBin, rtkBinaryName())
	os.MkdirAll(dwytBin, 0755)

	if err := installRTKNative(binPath); err != nil {
		// Fall back to a pre-installed rtk (homebrew, manual install, or a
		// user-provided rtk.exe on Windows).
		if cpErr := copyRTKBinary(binPath); cpErr != nil {
			return fmt.Errorf("rtk: download falhou (%w); e nenhum rtk encontrado localmente — instale manualmente de https://github.com/rtk-ai/rtk", err)
		}
	}

	// init --global registers the shell hooks. On Windows the hooks are
	// optional (the binary works when invoked directly), so a failure there is
	// not fatal.
	out, err := exec.Command(binPath, "init", "--global").CombinedOutput()
	if err != nil && runtime.GOOS != "windows" {
		return fmt.Errorf("rtk init --global falhou: %w\n%s", err, string(out))
	}
	return nil
}

// installRTKNative downloads and installs the rtk release binary for the
// current platform. Windows ships a .zip with rtk.exe inside; Unix ships a
// .tar.gz with rtk.
func installRTKNative(binPath string) error {
	target, err := rtkTargetTriple()
	if err != nil {
		return err
	}
	version, err := latestGitHubTag("rtk-ai/rtk")
	if err != nil {
		return fmt.Errorf("resolver versão do rtk: %w", err)
	}
	base := fmt.Sprintf("https://github.com/rtk-ai/rtk/releases/download/%s", version)

	isZip := runtime.GOOS == "windows"
	ext := "tar.gz"
	if isZip {
		ext = "zip"
	}
	archive := fmt.Sprintf("rtk-%s.%s", target, ext)
	return installReleaseBinary(releaseBinary{
		archiveURL:  base + "/" + archive,
		checksumURL: base + "/checksums.txt",
		archiveName: archive,
		destPath:    binPath,
		innerNames:  []string{"rtk", "rtk.exe"},
		isZip:       isZip,
	})
}

// rtkTargetTriple maps the Go platform to rtk's Rust release target triple.
func rtkTargetTriple() (string, error) {
	switch runtime.GOOS {
	case "linux":
		switch runtime.GOARCH {
		case "amd64":
			return "x86_64-unknown-linux-musl", nil
		case "arm64":
			return "aarch64-unknown-linux-gnu", nil
		}
	case "darwin":
		switch runtime.GOARCH {
		case "amd64":
			return "x86_64-apple-darwin", nil
		case "arm64":
			return "aarch64-apple-darwin", nil
		}
	case "windows":
		switch runtime.GOARCH {
		case "amd64":
			return "x86_64-pc-windows-msvc", nil
		}
	}
	return "", fmt.Errorf("rtk: plataforma não suportada %s/%s", runtime.GOOS, runtime.GOARCH)
}

func rtkBinaryName() string {
	if runtime.GOOS == "windows" {
		return "rtk.exe"
	}
	return "rtk"
}

// copyRTKBinary procura um rtk já instalado nos paths comuns e copia para
// dwytBin. Retorna erro se nenhum candidato existir.
func copyRTKBinary(binPath string) error {
	candidates := rtkCandidatePaths()
	for _, candidate := range candidates {
		data, err := os.ReadFile(candidate)
		if err != nil {
			continue
		}
		if err := writeExecutableReplacingRunning(binPath, data); err != nil {
			return fmt.Errorf("rtk: copiar de %s para %s: %w", candidate, binPath, err)
		}
		return nil
	}
	return fmt.Errorf("rtk: binário não localizado após instalação (procurado em %v)", candidates)
}

func rtkCandidatePaths() []string {
	home, _ := os.UserHomeDir()
	if runtime.GOOS == "windows" {
		appData := os.Getenv("APPDATA")
		return []string{
			filepath.Join(appData, "rtk", "rtk.exe"),
			filepath.Join(home, "AppData", "Local", "rtk", "rtk.exe"),
		}
	}
	return []string{
		filepath.Join(home, ".local", "bin", "rtk"),
		"/usr/local/bin/rtk",
		"/opt/homebrew/bin/rtk",
	}
}
