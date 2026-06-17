package install

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

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

// installObsidianWindows installs the Obsidian desktop app. Preferred path is
// winget (per-user, silent, no admin, stays updatable); if winget is missing or
// fails it falls back to downloading the official Windows installer from the
// GitHub releases and running it silently. Returns the resolved binary path.
func installObsidianWindows() (string, error) {
	if existing, ok := brain.FindObsidianBinary(); ok {
		return existing, nil
	}

	if _, err := exec.LookPath("winget"); err == nil {
		fmt.Println("  → obsidian: instalando via winget (Obsidian.Obsidian)…")
		if err := runWingetInstall("Obsidian.Obsidian"); err != nil {
			fmt.Printf("  ⚠ obsidian: winget falhou (%v); tentando instalador oficial…\n", err)
		} else if loc, ok := waitForObsidianBinary(5); ok {
			return loc, nil
		}
	}

	loc, err := installObsidianWindowsInstaller()
	if err != nil {
		return "", fmt.Errorf("obsidian: instalação automática falhou (%w); instale manualmente de https://obsidian.md/download", err)
	}
	return loc, nil
}

// installObsidianWindowsInstaller downloads the official Windows installer and
// runs it silently (electron-builder NSIS one-click installer honours "/S";
// older Squirrel builds install per-user regardless of the flag).
func installObsidianWindowsInstaller() (string, error) {
	url, err := latestObsidianWindowsInstallerURL()
	if err != nil {
		return "", err
	}
	exePath := filepath.Join(os.TempDir(), "Obsidian-dwyt-setup.exe")
	defer os.Remove(exePath)

	fmt.Printf("  → obsidian: baixando instalador de %s\n", url)
	if err := downloadObsidianInstaller(url, exePath); err != nil {
		return "", err
	}

	fmt.Println("  → obsidian: executando instalador silencioso…")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	if err := exec.CommandContext(ctx, exePath, "/S").Run(); err != nil {
		// Fall back to a plain run for installers that ignore "/S".
		_ = exec.CommandContext(ctx, exePath).Run()
	}

	if loc, ok := waitForObsidianBinary(15); ok {
		return loc, nil
	}
	return "", fmt.Errorf("instalador executou mas o binário não foi encontrado")
}

// waitForObsidianBinary polls for the installed Obsidian binary for up to
// `seconds`, since the installer drops files asynchronously.
func waitForObsidianBinary(seconds int) (string, bool) {
	for i := 0; i < seconds; i++ {
		if loc, ok := brain.FindObsidianBinary(); ok {
			return loc, true
		}
		time.Sleep(time.Second)
	}
	return "", false
}

func latestObsidianWindowsInstallerURL() (string, error) {
	assets, err := fetchLatestObsidianAssets()
	if err != nil {
		return "", err
	}
	var amd64URL, arm64URL string
	for _, a := range assets {
		n := strings.ToLower(a.Name)
		if !strings.HasSuffix(n, ".exe") || strings.Contains(n, "blockmap") {
			continue
		}
		if strings.Contains(n, "arm64") {
			arm64URL = a.URL
		} else {
			amd64URL = a.URL
		}
	}
	if runtime.GOARCH == "arm64" && arm64URL != "" {
		return arm64URL, nil
	}
	if amd64URL != "" {
		return amd64URL, nil
	}
	if arm64URL != "" {
		return arm64URL, nil
	}
	return "", fmt.Errorf("nenhum instalador .exe de Windows encontrado no release")
}

func downloadObsidianInstaller(url, dest string) error {
	resp, err := httpClient.Get(url)
	if err != nil {
		return fmt.Errorf("obsidian download: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("obsidian download HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("obsidian download read: %w", err)
	}
	// Sanity check against truncated downloads / error pages.
	if len(data) < 10_000_000 {
		return fmt.Errorf("instalador muito pequeno (%d bytes)", len(data))
	}
	return os.WriteFile(dest, data, 0755)
}
