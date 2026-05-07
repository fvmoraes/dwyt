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

	"github.com/fvmoraes/dwyt/internal/brain"
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
	if _, err := os.Stat(binPath); err != nil {
		return fmt.Errorf("rtk: binário não localizado após instalação (procurado em %v)", candidates)
	}
	if out, err := exec.Command(binPath, "init", "--global").CombinedOutput(); err != nil {
		return fmt.Errorf("rtk init --global falhou: %w\n%s", err, string(out))
	}
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

	// Limpa instalação anterior parcial (venv sem pip, symlink quebrado, etc.)
	// para que o passo seja idempotente em caso de retry.
	os.Remove(wrapperPath)
	os.RemoveAll(venvDir)

	pythonBin, err := findCompatiblePython()
	if err != nil {
		return fmt.Errorf("headroom: %w", err)
	}

	fmt.Printf("  → headroom venv (%s)...\n", pythonBin)
	if out, vErr := exec.Command(pythonBin, "-m", "venv", venvDir).CombinedOutput(); vErr != nil {
		return fmt.Errorf("headroom: criação do venv falhou: %w\n%s", vErr, string(out))
	}

	var pipBin, hrBin string
	if runtime.GOOS == "windows" {
		pipBin = filepath.Join(venvDir, "Scripts", "pip.exe")
		hrBin = filepath.Join(venvDir, "Scripts", "headroom.exe")
	} else {
		pipBin = filepath.Join(venvDir, "bin", "pip")
		hrBin = filepath.Join(venvDir, "bin", "headroom")
	}

	// Algumas builds do Python (especialmente Homebrew bleeding-edge) criam
	// venvs sem pip. Bootstrap via ensurepip antes de prosseguir.
	if _, err := os.Stat(pipBin); err != nil {
		venvPython := filepath.Join(filepath.Dir(pipBin), "python")
		if runtime.GOOS == "windows" {
			venvPython = filepath.Join(filepath.Dir(pipBin), "python.exe")
		}
		if out, eErr := exec.Command(venvPython, "-m", "ensurepip", "--upgrade").CombinedOutput(); eErr != nil {
			return fmt.Errorf("headroom: pip ausente no venv e ensurepip falhou: %w\n%s", eErr, string(out))
		}
	}

	if out, err := exec.Command(pipBin, "install", "--upgrade", "pip").CombinedOutput(); err != nil {
		return fmt.Errorf("headroom: upgrade do pip falhou: %w\n%s", err, string(out))
	}
	if out, err := exec.Command(pipBin, "install", "headroom-ai[proxy]").CombinedOutput(); err != nil {
		return fmt.Errorf("headroom: pip install headroom-ai[proxy] falhou: %w\n%s", err, string(out))
	}

	if _, err := os.Stat(hrBin); err != nil {
		return fmt.Errorf("headroom: binário não encontrado em %s após instalação", hrBin)
	}

	if runtime.GOOS == "windows" {
		bat := fmt.Sprintf("@echo off\r\n%q %%*\r\n", hrBin)
		return os.WriteFile(wrapperPath, []byte(bat), 0644)
	}
	return os.Symlink(hrBin, wrapperPath)
}

// findCompatiblePython localiza um interpretador Python compatível com
// headroom-ai. Versões 3.10–3.12 têm wheels disponíveis para todas as
// dependências; 3.13+ frequentemente quebram (faltam wheels para libs com
// extensões C). Em macOS isso costuma se manifestar como "no pip in venv"
// quando o Homebrew default pula pra uma versão muito nova.
//
// Antes de retornar, valida que o interpretador tem `ensurepip` funcional e
// que `xml.parsers.expat` carrega — em macOS+Homebrew o pyexpat às vezes
// fica linkado ao libexpat do sistema e quebra o pip silenciosamente.
func findCompatiblePython() (string, error) {
	candidates := []string{"python3.12", "python3.11", "python3.10", "python3", "python"}
	var lastErr error
	var firstUsable string
	for _, name := range candidates {
		path, err := exec.LookPath(name)
		if err != nil {
			continue
		}
		if firstUsable == "" {
			firstUsable = path
		}
		if maj, min, ok := pythonMajorMinor(path); ok && (maj > 3 || (maj == 3 && min >= 13)) {
			fmt.Printf("  ⚠ headroom: %s reportou Python %d.%d — pode não ter wheels para todas as dependências; recomendado 3.10–3.12\n", path, maj, min)
		}
		if err := validatePython(path); err != nil {
			lastErr = fmt.Errorf("%s: %w", path, err)
			fmt.Printf("  ⚠ headroom: pulando %s (%v)\n", path, err)
			continue
		}
		return path, nil
	}
	if firstUsable != "" && lastErr != nil {
		return "", fmt.Errorf("nenhum Python encontrado passou no pre-flight: %w\n%s",
			lastErr, pythonRemediationHint())
	}
	return "", fmt.Errorf("python3 não encontrado no PATH (instale Python 3.10–3.12; macOS: brew install python@3.12)")
}

// validatePython garante que o interpretador tem ensurepip e que pyexpat
// carrega corretamente. Sem isso o `python -m venv` cria um venv quebrado
// que aparece muito depois, no `pip install`.
func validatePython(bin string) error {
	if out, err := exec.Command(bin, "-m", "ensurepip", "--version").CombinedOutput(); err != nil {
		return fmt.Errorf("ensurepip indisponível: %w\n%s", err, strings.TrimSpace(string(out)))
	}
	if out, err := exec.Command(bin, "-c", "from xml.parsers import expat").CombinedOutput(); err != nil {
		return fmt.Errorf("pyexpat quebrado (provável dessincronia libexpat ↔ Python): %w\n%s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func pythonRemediationHint() string {
	switch runtime.GOOS {
	case "darwin":
		return "  Tente: brew reinstall python@3.12 expat\n" +
			"  Se persistir (pyexpat dessincronizado), aponte o pyexpat.so pro libexpat do Homebrew:\n" +
			"    install_name_tool -change /usr/lib/libexpat.1.dylib \\\n" +
			"      /opt/homebrew/opt/expat/lib/libexpat.1.dylib \\\n" +
			"      $(python3.12 -c 'import pyexpat,os;print(pyexpat.__file__)')\n" +
			"    codesign --force --sign - <pyexpat-path-acima>"
	case "linux":
		return "  Tente: instale o pacote dev do Python (ex: apt install python3.12-venv) e libexpat1"
	default:
		return "  Reinstale o Python 3.10–3.12"
	}
}

func pythonMajorMinor(bin string) (int, int, bool) {
	out, err := exec.Command(bin, "-c", "import sys;print(sys.version_info[0],sys.version_info[1])").Output()
	if err != nil {
		return 0, 0, false
	}
	var maj, min int
	if _, err := fmt.Sscanf(strings.TrimSpace(string(out)), "%d %d", &maj, &min); err != nil {
		return 0, 0, false
	}
	return maj, min, true
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
	if existing, ok := brain.FindObsidianBinary(); ok {
		return existing, nil
	}
	// Try AppImage first (most universal), then flatpak, then snap
	home, _ := os.UserHomeDir()
	binDir := filepath.Join(home, ".local", "bin")
	os.MkdirAll(binDir, 0755)
	appImagePath := filepath.Join(binDir, "Obsidian.AppImage")

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
	if existing, ok := brain.FindObsidianBinary(); ok {
		return existing, nil
	}
	home, _ := os.UserHomeDir()

	// 1) Homebrew Cask: caminho mais limpo, lida com gatekeeper e atualizações.
	if brewPath, err := exec.LookPath("brew"); err == nil {
		fmt.Printf("  → obsidian via homebrew cask...\n")
		if out, bErr := exec.Command(brewPath, "install", "--cask", "obsidian").CombinedOutput(); bErr != nil {
			fmt.Printf("  ⚠ homebrew falhou, tentando DMG: %s\n", strings.TrimSpace(string(out)))
		} else {
			if loc, ok := brain.FindObsidianBinary(); ok {
				return loc, nil
			}
		}
	}

	// 2) Fallback: baixar DMG oficial do release mais recente e copiar via hdiutil.
	url, err := latestObsidianMacDMGURL()
	if err != nil {
		return "", fmt.Errorf("obsidian: %w (instale manualmente: https://obsidian.md/download)", err)
	}

	dmgPath := filepath.Join(os.TempDir(), "Obsidian-dwyt.dmg")
	fmt.Printf("  → obsidian: baixando DMG de %s\n", url)
	resp, err := http.Get(url)
	if err != nil {
		return "", fmt.Errorf("obsidian download: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("obsidian download HTTP %d", resp.StatusCode)
	}
	dmgFile, err := os.Create(dmgPath)
	if err != nil {
		return "", fmt.Errorf("obsidian: criar dmg: %w", err)
	}
	if _, err := io.Copy(dmgFile, resp.Body); err != nil {
		dmgFile.Close()
		return "", fmt.Errorf("obsidian: leitura do dmg: %w", err)
	}
	dmgFile.Close()
	defer os.Remove(dmgPath)

	fmt.Printf("  → obsidian: montando DMG...\n")
	attachOut, err := exec.Command("hdiutil", "attach", "-nobrowse", "-noautoopen", "-quiet", dmgPath).Output()
	if err != nil {
		return "", fmt.Errorf("obsidian: hdiutil attach: %w", err)
	}
	mount := parseHdiutilMountPoint(string(attachOut))
	if mount == "" {
		return "", fmt.Errorf("obsidian: ponto de montagem não detectado no output do hdiutil")
	}
	defer exec.Command("hdiutil", "detach", "-quiet", mount).Run()

	src := filepath.Join(mount, "Obsidian.app")
	if _, err := os.Stat(src); err != nil {
		return "", fmt.Errorf("obsidian: Obsidian.app não encontrado em %s", mount)
	}

	// /Applications normalmente é gravável pelo usuário em macOS recentes,
	// mas se não for caímos pra ~/Applications.
	targetDir := "/Applications"
	if !canWriteDir(targetDir) {
		if home == "" {
			return "", fmt.Errorf("obsidian: /Applications não é gravável e $HOME está vazio")
		}
		targetDir = filepath.Join(home, "Applications")
		if err := os.MkdirAll(targetDir, 0755); err != nil {
			return "", fmt.Errorf("obsidian: criar %s: %w", targetDir, err)
		}
	}
	target := filepath.Join(targetDir, "Obsidian.app")
	os.RemoveAll(target)
	fmt.Printf("  → obsidian: copiando para %s...\n", target)
	if out, err := exec.Command("cp", "-R", src, targetDir+string(os.PathSeparator)).CombinedOutput(); err != nil {
		return "", fmt.Errorf("obsidian: cp: %w\n%s", err, string(out))
	}
	// Remove flag de quarentena para o app abrir sem prompt extra do Gatekeeper.
	exec.Command("xattr", "-dr", "com.apple.quarantine", target).Run()

	return filepath.Join(target, "Contents", "MacOS", "Obsidian"), nil
}

// parseHdiutilMountPoint extrai o mount point (ex: /Volumes/Obsidian 1.5.x)
// da saída tabular de `hdiutil attach`. Cada linha tem
// <device>\t<filesystem>\t<mount point> (campos podem faltar).
func parseHdiutilMountPoint(out string) string {
	for _, line := range strings.Split(out, "\n") {
		cols := strings.Split(line, "\t")
		if len(cols) < 3 {
			continue
		}
		mp := strings.TrimSpace(cols[len(cols)-1])
		if strings.HasPrefix(mp, "/Volumes/") {
			return mp
		}
	}
	return ""
}

func canWriteDir(dir string) bool {
	probe := filepath.Join(dir, ".dwyt-write-probe")
	f, err := os.Create(probe)
	if err != nil {
		return false
	}
	f.Close()
	os.Remove(probe)
	return true
}

func latestObsidianMacDMGURL() (string, error) {
	req, err := http.NewRequest("GET", "https://api.github.com/repos/obsidianmd/obsidian-releases/releases/latest", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "dwyt-installer")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("release lookup: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("release lookup HTTP %d", resp.StatusCode)
	}
	var release struct {
		Assets []struct {
			Name               string `json:"name"`
			BrowserDownloadURL string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return "", fmt.Errorf("release decode: %w", err)
	}
	var universal, archMatch string
	for _, a := range release.Assets {
		n := strings.ToLower(a.Name)
		if !strings.HasSuffix(n, ".dmg") {
			continue
		}
		if strings.Contains(n, "universal") {
			universal = a.BrowserDownloadURL
		}
		switch runtime.GOARCH {
		case "arm64":
			if strings.Contains(n, "arm64") {
				archMatch = a.BrowserDownloadURL
			}
		case "amd64":
			if strings.Contains(n, "x64") || strings.Contains(n, "amd64") || strings.Contains(n, "intel") {
				archMatch = a.BrowserDownloadURL
			}
		}
	}
	if universal != "" {
		return universal, nil
	}
	if archMatch != "" {
		return archMatch, nil
	}
	return "", fmt.Errorf("nenhum DMG de macOS encontrado no release")
}

func installObsidianWindows() (string, error) {
	if existing, ok := brain.FindObsidianBinary(); ok {
		return existing, nil
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
