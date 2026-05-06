package install

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

func CBMCP(dwytBin string) error {
	binName := "codebase-memory-mcp"
	if runtime.GOOS == "windows" {
		binName += ".exe"
	}
	binPath := filepath.Join(dwytBin, binName)
	os.MkdirAll(dwytBin, 0755)

	// Install the --ui variant so the graph visualization works at localhost:9749
	// The standard binary is stdio-only and has no HTTP server.
	script := fetch("https://raw.githubusercontent.com/DeusData/codebase-memory-mcp/main/install.sh")
	if script == "" {
		return fmt.Errorf("cbmcp: falha ao baixar script de instalação")
	}
	// --ui installs the UI variant; --skip-config skips agent config (DWYT manages that)
	cmd := exec.Command("bash", "-s", "--", "--ui", "--dir="+dwytBin, "--skip-config")
	stdin, _ := cmd.StdinPipe()
	go func() { io.WriteString(stdin, script); stdin.Close() }()
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("cbmcp: %w\n%s", err, string(out))
	}

	// Enable UI mode persistently so it always starts the HTTP server
	exec.Command(binPath, "--ui=true", "--port=9749").Run()

	return nil
}

func RTK(dwytBin string) error {
	binName := "rtk"
	if runtime.GOOS == "windows" {
		binName += ".exe"
	}
	binPath := filepath.Join(dwytBin, binName)
	os.MkdirAll(dwytBin, 0755)

	script := fetch("https://raw.githubusercontent.com/rtk-ai/rtk/refs/heads/master/install.sh")
	if script != "" {
		cmd := exec.Command("sh")
		stdin, _ := cmd.StdinPipe()
		go func() { io.WriteString(stdin, script); stdin.Close() }()
		cmd.Run()
	}

	// Try to find the installed binary and copy it to dwytBin
	home, _ := os.UserHomeDir()
	candidates := []string{
		filepath.Join(home, ".local", "bin", "rtk"),
		"/usr/local/bin/rtk",
	}
	if runtime.GOOS == "windows" {
		appData := os.Getenv("APPDATA")
		candidates = []string{
			filepath.Join(appData, "rtk", "rtk.exe"),
			filepath.Join(home, "AppData", "Local", "rtk", "rtk.exe"),
		}
	}
	for _, candidate := range candidates {
		if data, err := os.ReadFile(candidate); err == nil {
			os.WriteFile(binPath, data, 0755)
			break
		}
	}
	exec.Command(binPath, "init", "-g", "--yes").Run()
	return nil
}

func Headroom(dwytBin, dwytHome string) error {
	wrapperName := "headroom"
	if runtime.GOOS == "windows" {
		wrapperName = "headroom.bat"
	}
	wrapperPath := filepath.Join(dwytBin, wrapperName)

	venvDir := filepath.Join(dwytHome, "headroom-venv")
	os.MkdirAll(dwytHome, 0755)

	pythonBin := "python3"
	if _, err := exec.LookPath("python3"); err != nil {
		pythonBin = "python"
	}

	fmt.Printf("  → headroom venv...\n")
	exec.Command(pythonBin, "-m", "venv", venvDir).Run()

	var pipBin, hrBin string
	if runtime.GOOS == "windows" {
		pipBin = filepath.Join(venvDir, "Scripts", "pip.exe")
		hrBin = filepath.Join(venvDir, "Scripts", "headroom.exe")
	} else {
		pipBin = filepath.Join(venvDir, "bin", "pip")
		hrBin = filepath.Join(venvDir, "bin", "headroom")
	}

	exec.Command(pipBin, "install", "--quiet", "--upgrade", "pip").Run()
	if err := exec.Command(pipBin, "install", "--quiet", "headroom-ai[proxy]").Run(); err != nil {
		return fmt.Errorf("pip install headroom: %w", err)
	}

	if runtime.GOOS == "windows" {
		// On Windows create a .bat launcher instead of a symlink
		bat := fmt.Sprintf("@echo off\r\n%q %%*\r\n", hrBin)
		os.WriteFile(wrapperPath, []byte(bat), 0644)
	} else {
		os.Symlink(hrBin, wrapperPath)
	}
	return nil
}

func ObsidianMCP(dwytBin string) error {
	binName := "dwyt-obsidian-mcp"
	if runtime.GOOS == "windows" {
		binName += ".exe"
	}
	binPath := filepath.Join(dwytBin, binName)
	os.MkdirAll(dwytBin, 0755)

	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("obsidian-mcp: cannot locate DWYT binary: %w", err)
	}

	candidates := []string{
		filepath.Join(filepath.Dir(exe), binName),
		exe,
	}
	if realExe, err := filepath.EvalSymlinks(exe); err == nil {
		candidates = append([]string{filepath.Join(filepath.Dir(realExe), binName), realExe}, candidates...)
	}

	for _, src := range candidates {
		if src == "" {
			continue
		}
		if sameFile(src, binPath) {
			return nil
		}
		if _, err := os.Stat(src); err != nil {
			continue
		}
		if err := copyExecutable(src, binPath); err != nil {
			return fmt.Errorf("obsidian-mcp: copy %s to %s: %w", src, binPath, err)
		}
		return nil
	}

	return fmt.Errorf("dwyt-obsidian-mcp source binary not found")
}

func copyExecutable(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

func sameFile(a, b string) bool {
	aa, errA := filepath.Abs(a)
	bb, errB := filepath.Abs(b)
	if errA != nil || errB != nil {
		return false
	}
	return aa == bb
}

// InstallObsidianApp downloads and installs the Obsidian desktop app.
// Returns the path to the installed binary or an error.
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

func installObsidianLinux() (string, error) {
	// Try AppImage first (most universal), then flatpak, then snap
	home, _ := os.UserHomeDir()
	binDir := filepath.Join(home, ".local", "bin")
	os.MkdirAll(binDir, 0755)
	appImagePath := filepath.Join(binDir, "Obsidian.AppImage")

	// Check if already installed
	for _, candidate := range []string{
		appImagePath,
		"/usr/bin/obsidian",
		"/usr/local/bin/obsidian",
		"/opt/obsidian/obsidian",
	} {
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}

	// Download the latest Linux AppImage published by Obsidian.
	url, err := latestObsidianLinuxAppImageURL()
	if err != nil {
		return "", err
	}
	resp, err := http.Get(url)
	if err != nil {
		return "", fmt.Errorf("obsidian download failed: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("obsidian download read failed: %w", err)
	}

	if len(data) < 10_000_000 {
		return "", fmt.Errorf("obsidian download too small (%d bytes)", len(data))
	}

	if err := os.WriteFile(appImagePath, data, 0755); err != nil {
		return "", fmt.Errorf("obsidian write failed: %w", err)
	}

	// Create symlink for convenient CLI access
	symlinkPath := filepath.Join(binDir, "obsidian")
	os.Remove(symlinkPath)
	os.Symlink(appImagePath, symlinkPath)

	return appImagePath, nil
}

func latestObsidianLinuxAppImageURL() (string, error) {
	req, err := http.NewRequest("GET", "https://api.github.com/repos/obsidianmd/obsidian-releases/releases/latest", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "dwyt-installer")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("obsidian release lookup failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("obsidian release lookup returned HTTP %d", resp.StatusCode)
	}

	var release struct {
		Assets []struct {
			Name               string `json:"name"`
			BrowserDownloadURL string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return "", fmt.Errorf("obsidian release decode failed: %w", err)
	}
	for _, asset := range release.Assets {
		name := strings.ToLower(asset.Name)
		if strings.HasSuffix(name, ".appimage") && !strings.Contains(name, "arm") {
			return asset.BrowserDownloadURL, nil
		}
	}
	return "", fmt.Errorf("obsidian latest release has no Linux AppImage asset")
}

func installObsidianMacOS() (string, error) {
	// Check common install locations
	locations := []string{
		"/Applications/Obsidian.app/Contents/MacOS/Obsidian",
		"/Applications/Tools/Obsidian.app/Contents/MacOS/Obsidian",
	}
	for _, loc := range locations {
		if _, err := os.Stat(loc); err == nil {
			return loc, nil
		}
	}
	return "", fmt.Errorf("obsidian not found — install from https://obsidian.md/download (macOS)")
}

func installObsidianWindows() (string, error) {
	appData := os.Getenv("LOCALAPPDATA")
	if appData == "" {
		appData = os.Getenv("APPDATA")
	}
	candidates := []string{
		filepath.Join(appData, "obsidian", "Obsidian.exe"),
		filepath.Join(appData, "Programs", "Obsidian", "Obsidian.exe"),
		`C:\Program Files\Obsidian\Obsidian.exe`,
	}
	for _, loc := range candidates {
		if _, err := os.Stat(loc); err == nil {
			return loc, nil
		}
	}
	return "", fmt.Errorf("obsidian not found — install from https://obsidian.md/download (Windows)")
}

func fetch(url string) string {
	resp, err := http.Get(url)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return string(body)
}
