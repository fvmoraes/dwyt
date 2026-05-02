package env

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func Init(dwytHome, dwytBin, dwytData, shellRC, loginRC string) {
	os.MkdirAll(dwytHome, 0755)
	os.MkdirAll(dwytBin, 0755)
	os.MkdirAll(dwytData, 0755)

	envFile := filepath.Join(dwytHome, "env.sh")
	content := fmt.Sprintf("export XDG_CACHE_HOME=%q\nexport PATH=%s:$PATH\n", dwytData, dwytBin)
	os.WriteFile(envFile, []byte(content), 0644)

	injectRC(envFile, shellRC)
	if loginRC != "" {
		injectRC(envFile, loginRC)
	}

	fmt.Printf("  ✓ env.sh criado e shell RC atualizado\n")
}

func injectRC(envFile, rcFile string) {
	if rcFile == "" {
		return
	}
	marker := "# dwyt:source"
	sourceLine := fmt.Sprintf("[[ -f %q ]] && source %q", envFile, envFile)

	data, err := os.ReadFile(rcFile)
	if err != nil && !os.IsNotExist(err) {
		return
	}
	if strings.Contains(string(data), marker) {
		return
	}
	f, _ := os.OpenFile(rcFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if f != nil {
		defer f.Close()
		fmt.Fprintf(f, "\n%s\n%s\n", marker, sourceLine)
	}
}
