package install

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/fvmoraes/dwyt/internal/brain"
)

// InstallObsidianApp baixa e instala a aplicação desktop do Obsidian.
// Retorna o path do binário instalado, ou um existente caso o app já
// esteja presente no sistema.
//
// O fluxo macOS é grande o suficiente para morar em obsidian_app_macos.go.
// Linux e Windows ficam aqui — são curtos e raramente mudam.
// (Nota: nomes terminados em _linux.go/_windows.go ativariam build tags
// implícitos no Go, então mantemos tudo em obsidian_app.go.)
func InstallObsidianApp() (string, error) {
	switch runtime.GOOS {
	case "linux":
		return installObsidianLinux()
	case "darwin":
		return installObsidianMacOS()
	case "windows":
		return installObsidianWindows()
	default:
		return "", fmt.Errorf("unsupported OS: %s", runtime.GOOS)
	}
}

// ── Linux ────────────────────────────────────────────────────────────────────

func installObsidianLinux() (string, error) {
	if existing, ok := brain.FindObsidianBinary(); ok {
		return existing, nil
	}

	home, _ := os.UserHomeDir()
	binDir := filepath.Join(home, ".local", "bin")
	os.MkdirAll(binDir, 0755)
	appImagePath := filepath.Join(binDir, "Obsidian.AppImage")

	url, err := latestObsidianLinuxAppImageURL()
	if err != nil {
		return "", err
	}
	if err := downloadObsidianAppImage(url, appImagePath); err != nil {
		return "", err
	}
	createObsidianSymlink(binDir, appImagePath)
	return appImagePath, nil
}

func latestObsidianLinuxAppImageURL() (string, error) {
	assets, err := fetchLatestObsidianAssets()
	if err != nil {
		return "", err
	}
	for _, a := range assets {
		name := strings.ToLower(a.Name)
		if strings.HasSuffix(name, ".appimage") && !strings.Contains(name, "arm") {
			return a.URL, nil
		}
	}
	return "", fmt.Errorf("obsidian latest release has no Linux AppImage asset")
}

func downloadObsidianAppImage(url, dest string) error {
	resp, err := httpClient.Get(url)
	if err != nil {
		return fmt.Errorf("obsidian download failed: %w", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("obsidian download read failed: %w", err)
	}
	// Sanity check: rejeita downloads obviamente truncados (página de erro,
	// redirect quebrado). AppImages reais ficam acima de 80MB.
	if len(data) < 10_000_000 {
		return fmt.Errorf("obsidian download too small (%d bytes)", len(data))
	}
	if err := os.WriteFile(dest, data, 0755); err != nil {
		return fmt.Errorf("obsidian write failed: %w", err)
	}
	return nil
}

func createObsidianSymlink(binDir, appImagePath string) {
	symlinkPath := filepath.Join(binDir, "obsidian")
	os.Remove(symlinkPath)
	os.Symlink(appImagePath, symlinkPath)
}

// ── Windows ──────────────────────────────────────────────────────────────────

func installObsidianWindows() (string, error) {
	if existing, ok := brain.FindObsidianBinary(); ok {
		return existing, nil
	}
	return "", fmt.Errorf("obsidian not found — install from https://obsidian.md/download (Windows)")
}
