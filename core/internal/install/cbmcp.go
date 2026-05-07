package install

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

// CBMCP instala codebase-memory-mcp em dwytBin via o instalador oficial.
// Usa a variante --ui pra que o servidor HTTP de visualização suba em :9749;
// o binário padrão é stdio-only e não responde HTTP. --skip-config porque o
// próprio DWYT gerencia config dos agentes.
func CBMCP(dwytBin string) error {
	binName := "codebase-memory-mcp"
	if runtime.GOOS == "windows" {
		binName += ".exe"
	}
	binPath := filepath.Join(dwytBin, binName)
	os.MkdirAll(dwytBin, 0755)

	script := fetch("https://raw.githubusercontent.com/DeusData/codebase-memory-mcp/main/install.sh")
	if script == "" {
		return fmt.Errorf("cbmcp: falha ao baixar script de instalação")
	}
	cmd := exec.Command("bash", "-s", "--", "--ui", "--dir="+dwytBin, "--skip-config")
	stdin, _ := cmd.StdinPipe()
	go func() { io.WriteString(stdin, script); stdin.Close() }()
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("cbmcp: %w\n%s", err, string(out))
	}

	exec.Command(binPath, "--ui=true", "--port=9749").Run()
	return nil
}
