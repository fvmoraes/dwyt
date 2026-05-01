package install

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/DeusData/dwyt-orchestrator/internal/detect"
)

func CBMCP(e *detect.Env) error {
	binPath := filepath.Join(e.DwytBin, "codebase-memory-mcp")
	if runtime.GOOS == "windows" {
		binPath += ".exe"
	}
	if _, err := os.Stat(binPath); err == nil {
		fmt.Printf("  ✓ codebase-memory-mcp já instalado\n")
		return nil
	}
	os.MkdirAll(e.DwytBin, 0755)
	script := fetch("https://raw.githubusercontent.com/DeusData/codebase-memory-mcp/main/install.sh")
	cmd := exec.Command("bash", "-s", "--", "--dir="+e.DwytBin, "--skip-config")
	stdin, _ := cmd.StdinPipe()
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Start()
	io.WriteString(stdin, script)
	stdin.Close()
	return cmd.Wait()
}

func RTK(e *detect.Env) error {
	binPath := filepath.Join(e.DwytBin, "rtk")
	if runtime.GOOS == "windows" {
		binPath += ".exe"
	}
	if _, err := os.Stat(binPath); err == nil {
		fmt.Printf("  ✓ RTK já instalado\n")
		return nil
	}
	os.MkdirAll(e.DwytBin, 0755)
	script := fetch("https://raw.githubusercontent.com/rtk-ai/rtk/refs/heads/master/install.sh")
	cmd := exec.Command("sh")
	stdin, _ := cmd.StdinPipe()
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Start()
	io.WriteString(stdin, script)
	stdin.Close()
	cmd.Wait()

	for _, candidate := range []string{
		e.HomeDir + "/.local/bin/rtk",
		"/usr/local/bin/rtk",
	} {
		if data, err := os.ReadFile(candidate); err == nil {
			os.WriteFile(binPath, data, 0755)
			fmt.Printf("  ✓ RTK → %s\n", binPath)
			break
		}
	}
	exec.Command(binPath, "init", "-g", "--yes").Run()
	return nil
}

func Headroom(e *detect.Env) error {
	wrapperPath := filepath.Join(e.DwytBin, "headroom")
	if _, err := os.Stat(wrapperPath); err == nil {
		fmt.Printf("  ✓ Headroom já instalado\n")
		return nil
	}

	venvDir := filepath.Join(e.DwytHome, "headroom-venv")
	os.MkdirAll(e.DwytHome, 0755)

	pythonBin := "python3"
	if _, err := exec.LookPath("python3"); err != nil {
		pythonBin = "python"
	}

	fmt.Printf("  → venv em %s...\n", venvDir)
	exec.Command(pythonBin, "-m", "venv", venvDir).Run()

	pipBin := filepath.Join(venvDir, "bin", "pip")
	if runtime.GOOS == "windows" {
		pipBin = filepath.Join(venvDir, "Scripts", "pip.exe")
	}
	exec.Command(pipBin, "install", "--quiet", "--upgrade", "pip").Run()
	if err := exec.Command(pipBin, "install", "--quiet", "headroom-ai[proxy]").Run(); err != nil {
		return fmt.Errorf("pip install: %w", err)
	}

	hrBin := filepath.Join(venvDir, "bin", "headroom")
	if runtime.GOOS == "windows" {
		hrBin = filepath.Join(venvDir, "Scripts", "headroom.exe")
	}
	os.Symlink(hrBin, wrapperPath)
	fmt.Printf("  ✓ Headroom → %s\n", wrapperPath)
	return nil
}

func MemStack(e *detect.Env) error {
	dir := filepath.Join(e.DwytHome, "memstack")
	if _, err := os.Stat(dir + "/.git"); err == nil {
		fmt.Printf("  ✓ MemStack já existe (atualizando...)\n")
		exec.Command("git", "-C", dir, "pull", "--quiet").Run()
		return nil
	}

	cmd := exec.Command("git", "clone", "--depth=1", "https://github.com/cwinvestments/memstack.git", dir)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("clone: %w", err)
	}

	wrapper := fmt.Sprintf("#!/usr/bin/env bash\nMEMSTACK_HOME=%q\nexec python3 \"${MEMSTACK_HOME}/db/memstack-db.py\" \"$@\"\n", dir)
	os.WriteFile(e.DwytBin+"/memstack", []byte(wrapper), 0755)

	fmt.Printf("  ✓ MemStack → %s\n", dir)
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
