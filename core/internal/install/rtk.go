package install

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

// RTK instala o binário rtk em dwytBin e roda `rtk init --global` pra
// registrar os hooks no shell.
func RTK(dwytBin string) error {
	binPath := filepath.Join(dwytBin, rtkBinaryName())
	os.MkdirAll(dwytBin, 0755)

	runRTKUpstreamInstaller()

	if err := copyRTKBinary(binPath); err != nil {
		return err
	}
	if out, err := exec.Command(binPath, "init", "--global").CombinedOutput(); err != nil {
		return fmt.Errorf("rtk init --global falhou: %w\n%s", err, string(out))
	}
	return nil
}

func rtkBinaryName() string {
	if runtime.GOOS == "windows" {
		return "rtk.exe"
	}
	return "rtk"
}

// runRTKUpstreamInstaller dispara o instalador oficial do rtk; ignora
// falhas porque o copyRTKBinary subsequente lida com binários já presentes
// (homebrew, install manual anterior, etc.).
func runRTKUpstreamInstaller() {
	script := fetch("https://raw.githubusercontent.com/rtk-ai/rtk/refs/heads/master/install.sh")
	if script == "" {
		return
	}
	cmd := exec.Command("sh")
	stdin, _ := cmd.StdinPipe()
	go func() { io.WriteString(stdin, script); stdin.Close() }()
	cmd.Run()
}

// copyRTKBinary procura um rtk já instalado nos paths comuns e copia para
// dwytBin. Retorna erro se nenhum candidato existir.
func copyRTKBinary(binPath string) error {
	candidates := rtkCandidatePaths()
	for _, candidate := range candidates {
		data, err := os.ReadFile(candidate)
		if err != nil {
			continue
		}
		if err := os.WriteFile(binPath, data, 0755); err != nil {
			return fmt.Errorf("rtk: copiar de %s para %s: %w", candidate, binPath, err)
		}
		return nil
	}
	return fmt.Errorf("rtk: binário não localizado após instalação (procurado em %v)", candidates)
}

func rtkCandidatePaths() []string {
	home, _ := os.UserHomeDir()
	if runtime.GOOS == "windows" {
		appData := os.Getenv("APPDATA")
		return []string{
			filepath.Join(appData, "rtk", "rtk.exe"),
			filepath.Join(home, "AppData", "Local", "rtk", "rtk.exe"),
		}
	}
	return []string{
		filepath.Join(home, ".local", "bin", "rtk"),
		"/usr/local/bin/rtk",
		"/opt/homebrew/bin/rtk",
	}
}
