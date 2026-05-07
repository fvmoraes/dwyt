package brain

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// ObsidianBinaryCandidates retorna a lista canônica de paths para o binário
// do Obsidian, na ordem de preferência. Centralizada aqui pra que detecção
// e install usem a mesma fonte de verdade — antes essa lista vivia em três
// arquivos diferentes e divergiu (alguns sites perdiam Setapp,
// ~/Applications, etc.) causando "instalado mas não detectado".
func ObsidianBinaryCandidates() []string {
	switch runtime.GOOS {
	case "darwin":
		return obsidianMacCandidates()
	case "windows":
		return obsidianWindowsCandidates()
	default:
		return obsidianLinuxCandidates()
	}
}

func obsidianMacCandidates() []string {
	out := []string{
		"/Applications/Obsidian.app/Contents/MacOS/Obsidian",
		"/Applications/Tools/Obsidian.app/Contents/MacOS/Obsidian",
		"/Applications/Setapp/Obsidian.app/Contents/MacOS/Obsidian",
	}
	if home, _ := os.UserHomeDir(); home != "" {
		out = append(out,
			filepath.Join(home, "Applications", "Obsidian.app", "Contents", "MacOS", "Obsidian"),
			filepath.Join(home, "Applications", "Tools", "Obsidian.app", "Contents", "MacOS", "Obsidian"),
		)
	}
	return out
}

func obsidianWindowsCandidates() []string {
	appData := os.Getenv("LOCALAPPDATA")
	if appData == "" {
		appData = os.Getenv("APPDATA")
	}
	out := []string{
		filepath.Join(appData, "Obsidian", "Obsidian.exe"),
		filepath.Join(appData, "obsidian", "Obsidian.exe"),
		filepath.Join(appData, "Programs", "Obsidian", "Obsidian.exe"),
		`C:\Program Files\Obsidian\Obsidian.exe`,
	}
	if home, _ := os.UserHomeDir(); home != "" {
		out = append(out, filepath.Join(home, "AppData", "Local", "Obsidian", "Obsidian.exe"))
	}
	return out
}

func obsidianLinuxCandidates() []string {
	out := []string{
		"/usr/bin/obsidian",
		"/usr/local/bin/obsidian",
		"/opt/obsidian/obsidian",
	}
	if home, _ := os.UserHomeDir(); home != "" {
		out = append(out,
			filepath.Join(home, ".local", "bin", "Obsidian.AppImage"),
			filepath.Join(home, ".local", "bin", "obsidian"),
		)
	}
	return out
}

// FindObsidianBinary retorna o path do primeiro binário existente da lista
// canônica (e/ou o LookPath de "obsidian"), com bool indicando se foi achado.
// Em macOS, faz fallback para Spotlight pra cobrir instalações em paths
// não-padrão (Setapp custom, /Applications/Productivity, etc.).
func FindObsidianBinary() (string, bool) {
	if path, err := exec.LookPath("obsidian"); err == nil {
		return path, true
	}
	for _, loc := range ObsidianBinaryCandidates() {
		if _, err := os.Stat(loc); err == nil {
			return loc, true
		}
	}
	if runtime.GOOS == "darwin" {
		if bin, ok := findObsidianViaSpotlight(); ok {
			return bin, true
		}
	}
	return "", false
}

func findObsidianViaSpotlight() (string, bool) {
	out, err := exec.Command("mdfind",
		"kMDItemCFBundleIdentifier == 'md.obsidian'").Output()
	if err != nil {
		return "", false
	}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		bin := filepath.Join(line, "Contents", "MacOS", "Obsidian")
		if _, err := os.Stat(bin); err == nil {
			return bin, true
		}
	}
	return "", false
}

func ObsidianInstalled() bool {
	_, ok := FindObsidianBinary()
	return ok
}
