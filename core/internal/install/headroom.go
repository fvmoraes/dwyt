package install

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

// Headroom instala o headroom-ai num venv Python dedicado em
// dwytHome/headroom-venv e cria um wrapper em dwytBin/headroom.
// Idempotente: limpa instalação parcial anterior antes de começar.
func Headroom(dwytBin, dwytHome string) error {
	wrapperPath := filepath.Join(dwytBin, headroomWrapperName())
	venvDir := filepath.Join(dwytHome, "headroom-venv")
	os.MkdirAll(dwytHome, 0755)

	cleanPartialHeadroom(wrapperPath, venvDir)

	pythonBin, err := findCompatiblePython()
	if err != nil {
		return fmt.Errorf("headroom: %w", err)
	}
	fmt.Printf("  → headroom venv (%s)...\n", pythonBin)
	if out, vErr := exec.Command(pythonBin, "-m", "venv", venvDir).CombinedOutput(); vErr != nil {
		return fmt.Errorf("headroom: criação do venv falhou: %w\n%s", vErr, string(out))
	}

	pipBin, hrBin := venvBinaries(venvDir)
	if err := ensurePipInVenv(pipBin); err != nil {
		return err
	}
	if err := pipInstallHeadroom(pipBin); err != nil {
		return err
	}
	if _, err := os.Stat(hrBin); err != nil {
		return fmt.Errorf("headroom: binário não encontrado em %s após instalação", hrBin)
	}
	return writeHeadroomWrapper(hrBin, wrapperPath)
}

func headroomWrapperName() string {
	if runtime.GOOS == "windows" {
		return "headroom.bat"
	}
	return "headroom"
}

// cleanPartialHeadroom remove restos de uma tentativa anterior incompleta.
// Sem isso, retries falhavam com "venv sem pip" porque herdavam estado quebrado.
func cleanPartialHeadroom(wrapperPath, venvDir string) {
	os.Remove(wrapperPath)
	os.RemoveAll(venvDir)
}

func venvBinaries(venvDir string) (pipBin, hrBin string) {
	if runtime.GOOS == "windows" {
		return filepath.Join(venvDir, "Scripts", "pip.exe"),
			filepath.Join(venvDir, "Scripts", "headroom.exe")
	}
	return filepath.Join(venvDir, "bin", "pip"),
		filepath.Join(venvDir, "bin", "headroom")
}

// ensurePipInVenv lida com builds de Python (especialmente Homebrew
// bleeding-edge) que criam venvs sem pip. Bootstrap via ensurepip antes
// dos pip installs subsequentes.
func ensurePipInVenv(pipBin string) error {
	if _, err := os.Stat(pipBin); err == nil {
		return nil
	}
	venvPython := filepath.Join(filepath.Dir(pipBin), "python")
	if runtime.GOOS == "windows" {
		venvPython = filepath.Join(filepath.Dir(pipBin), "python.exe")
	}
	if out, err := exec.Command(venvPython, "-m", "ensurepip", "--upgrade").CombinedOutput(); err != nil {
		return fmt.Errorf("headroom: pip ausente no venv e ensurepip falhou: %w\n%s", err, string(out))
	}
	return nil
}

func pipInstallHeadroom(pipBin string) error {
	if out, err := exec.Command(pipBin, "install", "--upgrade", "pip").CombinedOutput(); err != nil {
		return fmt.Errorf("headroom: upgrade do pip falhou: %w\n%s", err, string(out))
	}
	if out, err := exec.Command(pipBin, "install", "headroom-ai[proxy]").CombinedOutput(); err != nil {
		return fmt.Errorf("headroom: pip install headroom-ai[proxy] falhou: %w\n%s", err, string(out))
	}
	return nil
}

// writeHeadroomWrapper cria um símbolo invocável (symlink no POSIX, .bat no
// Windows) que aponta para o binário dentro do venv.
func writeHeadroomWrapper(hrBin, wrapperPath string) error {
	if runtime.GOOS == "windows" {
		bat := fmt.Sprintf("@echo off\r\n%q %%*\r\n", hrBin)
		return os.WriteFile(wrapperPath, []byte(bat), 0644)
	}
	return os.Symlink(hrBin, wrapperPath)
}
