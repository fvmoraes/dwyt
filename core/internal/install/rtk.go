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
// On Linux/macOS it downloads the prebuilt release archive natively in Go
// (with SHA-256 verification) — no shell/curl/tar needed. On Windows the rtk
// project publishes no binary, so we try a user-provided rtk.exe and otherwise
// fail with an actionable message; the other DWYT tools work regardless.
func RTK(dwytBin string) error {
	binPath := filepath.Join(dwytBin, rtkBinaryName())
	os.MkdirAll(dwytBin, 0755)

	if runtime.GOOS == "windows" {
		if err := copyRTKBinary(binPath); err != nil {
			return fmt.Errorf("rtk: sem binário oficial para Windows. Instale manualmente ou use o WSL (https://github.com/rtk-ai/rtk); as demais ferramentas DWYT funcionam normalmente")
		}
		exec.Command(binPath, "init", "--global").Run()
		return nil
	}

	if err := installRTKNative(binPath); err != nil {
		// Fall back to a pre-installed rtk (homebrew, manual install, ...).
		if cpErr := copyRTKBinary(binPath); cpErr != nil {
			return fmt.Errorf("rtk: %w", err)
		}
	}
	if out, err := exec.Command(binPath, "init", "--global").CombinedOutput(); err != nil {
		return fmt.Errorf("rtk init --global falhou: %w\n%s", err, string(out))
	}
	return nil
}

// installRTKNative downloads and installs the rtk release binary for the
// current Unix platform.
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
	archive := fmt.Sprintf("rtk-%s.tar.gz", target)
	return installReleaseBinary(releaseBinary{
		archiveURL:  base + "/" + archive,
		checksumURL: base + "/checksums.txt",
		archiveName: archive,
		destPath:    binPath,
		innerNames:  []string{"rtk"},
		isZip:       false,
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
		if err := os.WriteFile(binPath, data, 0755); err != nil {
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
