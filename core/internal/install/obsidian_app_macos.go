package install

import (
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

func installObsidianMacOS() (string, error) {
	if existing, ok := brain.FindObsidianBinary(); ok {
		return existing, nil
	}

	if path, ok := tryHomebrewCask(); ok {
		return path, nil
	}
	return installObsidianMacOSDMG()
}

// tryHomebrewCask é o caminho preferido em macOS: brew cuida de Gatekeeper,
// quarantine e atualizações automáticas. Retorna ok=false se brew não está
// instalado ou se o cask falhou — chamadores devem cair pro DMG.
func tryHomebrewCask() (string, bool) {
	brewPath, err := exec.LookPath("brew")
	if err != nil {
		return "", false
	}
	fmt.Printf("  → obsidian via homebrew cask...\n")
	out, bErr := exec.Command(brewPath, "install", "--cask", "obsidian").CombinedOutput()
	if bErr != nil {
		fmt.Printf("  ⚠ homebrew falhou, tentando DMG: %s\n", strings.TrimSpace(string(out)))
		return "", false
	}
	if loc, ok := brain.FindObsidianBinary(); ok {
		return loc, true
	}
	return "", false
}

func installObsidianMacOSDMG() (string, error) {
	url, err := latestObsidianMacDMGURL()
	if err != nil {
		return "", fmt.Errorf("obsidian: %w (instale manualmente: https://obsidian.md/download)", err)
	}
	dmgPath := filepath.Join(os.TempDir(), "Obsidian-dwyt.dmg")
	defer os.Remove(dmgPath)

	if err := downloadDMG(url, dmgPath); err != nil {
		return "", err
	}
	mount, detach, err := mountDMG(dmgPath)
	if err != nil {
		return "", err
	}
	defer detach()

	src := filepath.Join(mount, "Obsidian.app")
	if _, err := os.Stat(src); err != nil {
		return "", fmt.Errorf("obsidian: Obsidian.app não encontrado em %s", mount)
	}
	target, err := chooseObsidianTargetDir()
	if err != nil {
		return "", err
	}
	return copyObsidianApp(src, target)
}

func downloadDMG(url, dmgPath string) error {
	fmt.Printf("  → obsidian: baixando DMG de %s\n", url)
	resp, err := http.Get(url)
	if err != nil {
		return fmt.Errorf("obsidian download: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("obsidian download HTTP %d", resp.StatusCode)
	}
	dmgFile, err := os.Create(dmgPath)
	if err != nil {
		return fmt.Errorf("obsidian: criar dmg: %w", err)
	}
	if _, err := io.Copy(dmgFile, resp.Body); err != nil {
		dmgFile.Close()
		return fmt.Errorf("obsidian: leitura do dmg: %w", err)
	}
	return dmgFile.Close()
}

// mountDMG monta o DMG e retorna o mount point com uma função detach que o
// chamador deve invocar (defer) pra liberar o volume.
func mountDMG(dmgPath string) (mount string, detach func(), err error) {
	fmt.Printf("  → obsidian: montando DMG...\n")
	attachOut, err := exec.Command("hdiutil", "attach", "-nobrowse", "-noautoopen", "-quiet", dmgPath).Output()
	if err != nil {
		return "", nil, fmt.Errorf("obsidian: hdiutil attach: %w", err)
	}
	mount = parseHdiutilMountPoint(string(attachOut))
	if mount == "" {
		return "", nil, fmt.Errorf("obsidian: ponto de montagem não detectado no output do hdiutil")
	}
	detach = func() { exec.Command("hdiutil", "detach", "-quiet", mount).Run() }
	return mount, detach, nil
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

// chooseObsidianTargetDir escolhe /Applications quando gravável (caso
// comum em macOS modernos), senão cai pra ~/Applications.
func chooseObsidianTargetDir() (string, error) {
	if canWriteDir("/Applications") {
		return "/Applications", nil
	}
	home, _ := os.UserHomeDir()
	if home == "" {
		return "", fmt.Errorf("obsidian: /Applications não é gravável e $HOME está vazio")
	}
	target := filepath.Join(home, "Applications")
	if err := os.MkdirAll(target, 0755); err != nil {
		return "", fmt.Errorf("obsidian: criar %s: %w", target, err)
	}
	return target, nil
}

func copyObsidianApp(src, targetDir string) (string, error) {
	target := filepath.Join(targetDir, "Obsidian.app")
	os.RemoveAll(target)
	fmt.Printf("  → obsidian: copiando para %s...\n", target)
	if out, err := exec.Command("cp", "-R", src, targetDir+string(os.PathSeparator)).CombinedOutput(); err != nil {
		return "", fmt.Errorf("obsidian: cp: %w\n%s", err, string(out))
	}
	exec.Command("xattr", "-dr", "com.apple.quarantine", target).Run()
	return filepath.Join(target, "Contents", "MacOS", "Obsidian"), nil
}

func latestObsidianMacDMGURL() (string, error) {
	assets, err := fetchLatestObsidianAssets()
	if err != nil {
		return "", err
	}
	var universal, archMatch string
	for _, a := range assets {
		n := strings.ToLower(a.Name)
		if !strings.HasSuffix(n, ".dmg") {
			continue
		}
		if strings.Contains(n, "universal") {
			universal = a.URL
		}
		if matchesMacArch(n) {
			archMatch = a.URL
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

func matchesMacArch(assetName string) bool {
	switch runtime.GOARCH {
	case "arm64":
		return strings.Contains(assetName, "arm64")
	case "amd64":
		return strings.Contains(assetName, "x64") ||
			strings.Contains(assetName, "amd64") ||
			strings.Contains(assetName, "intel")
	}
	return false
}
