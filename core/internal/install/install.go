package install

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

func CBMCP(dwytBin string) error {
	binName := "codebase-memory-mcp"
	if runtime.GOOS == "windows" {
		binName += ".exe"
	}
	binPath := filepath.Join(dwytBin, binName)
	if _, err := os.Stat(binPath); err == nil {
		fmt.Println("  ✓ codebase-memory-mcp já instalado")
		return nil
	}
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
	if _, err := os.Stat(binPath); err == nil {
		fmt.Println("  ✓ RTK já instalado")
		exec.Command(binPath, "init", "-g", "--yes").Run()
		return nil
	}
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
	if _, err := os.Stat(wrapperPath); err == nil {
		fmt.Println("  ✓ Headroom já instalado")
		return nil
	}

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
		hrBin  = filepath.Join(venvDir, "Scripts", "headroom.exe")
	} else {
		pipBin = filepath.Join(venvDir, "bin", "pip")
		hrBin  = filepath.Join(venvDir, "bin", "headroom")
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


func fetch(url string) string {
	resp, err := http.Get(url)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return string(body)
}
