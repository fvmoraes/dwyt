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
	script := fetch("https://raw.githubusercontent.com/DeusData/codebase-memory-mcp/main/install.sh")
	if script == "" {
		return fmt.Errorf("cbmcp: falha ao baixar script de instalação")
	}
	cmd := exec.Command("bash", "-s", "--", "--dir="+dwytBin, "--skip-config")
	stdin, _ := cmd.StdinPipe()
	go func() { io.WriteString(stdin, script); stdin.Close() }()
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("cbmcp: %w\n%s", err, string(out))
	}
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

func MemStack(dwytBin, dwytHome string) error {
	dir := filepath.Join(dwytHome, "memstack")
	if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
		fmt.Println("  ✓ MemStack já existe (atualizando...)")
		exec.Command("git", "-C", dir, "pull", "--quiet").Run()
		return nil
	}
	cmd := exec.Command("git", "clone", "--depth=1", "https://github.com/cwinvestments/memstack.git", dir)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("memstack clone: %w", err)
	}

	if runtime.GOOS == "windows" {
		// Windows: .bat launcher
		bat := fmt.Sprintf(
			"@echo off\r\nset MEMSTACK_HOME=%s\r\npython \"%s\\db\\memstack-db.py\" %%*\r\n",
			dir, dir,
		)
		os.WriteFile(filepath.Join(dwytBin, "memstack.bat"), []byte(bat), 0644)
	} else {
		wrapper := fmt.Sprintf(
			"#!/usr/bin/env bash\nMEMSTACK_HOME=%q\nexec python3 \"${MEMSTACK_HOME}/db/memstack-db.py\" \"$@\"\n",
			dir,
		)
		os.WriteFile(filepath.Join(dwytBin, "memstack"), []byte(wrapper), 0755)
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
