package env

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/DeusData/dwyt-orchestrator/internal/detect"
	"github.com/DeusData/dwyt-orchestrator/internal/state"
)

func Init(e *detect.Env) {
	os.MkdirAll(e.DwytHome, 0755)
	os.MkdirAll(e.DwytBin, 0755)
	os.MkdirAll(e.DwytData, 0755)

	envFile := filepath.Join(e.DwytHome, "env.sh")
	content := fmt.Sprintf("export XDG_CACHE_HOME=%q\nexport PATH=%s:$PATH\n", e.DwytData, e.DwytBin)
	os.WriteFile(envFile, []byte(content), 0644)

	injectShellRC(e, envFile, e.ShellRC)
	if e.LoginRC != "" {
		injectShellRC(e, envFile, e.LoginRC)
	}
}

func Finalize(e *detect.Env, s *state.State) {
	// ensure PATH
	envFile := filepath.Join(e.DwytHome, "env.sh")
	pathLine := fmt.Sprintf("export PATH=%q:$PATH", e.DwytBin)
	content, _ := os.ReadFile(envFile)
	if !strings.Contains(string(content), pathLine) {
		f, _ := os.OpenFile(envFile, os.O_APPEND|os.O_WRONLY, 0644)
		f.Write([]byte(pathLine + "\n"))
		f.Close()
	}
}

func injectShellRC(e *detect.Env, envFile, rcFile string) {
	marker := "# dwyt:source"
	sourceLine := fmt.Sprintf("[[ -f %q ]] && source %q", envFile, envFile)

	data, err := os.ReadFile(rcFile)
	if err != nil && !os.IsNotExist(err) {
		return
	}
	if strings.Contains(string(data), marker) {
		return
	}

	f, err := os.OpenFile(rcFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "\n%s\n%s\n", marker, sourceLine)
}
