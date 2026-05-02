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

const (
	DwytHome = "$HOME/.dwyt"
)

func CBMCP(dwytBin string) error {
	binPath := filepath.Join(dwytBin, "codebase-memory-mcp")
	if runtime.GOOS == "windows" {
		binPath += ".exe"
	}
	if _, err := os.Stat(binPath); err == nil {
		fmt.Println("  ✓ codebase-memory-mcp já instalado")
		return nil
	}
	os.MkdirAll(dwytBin, 0755)
	script := fetch("https://raw.githubusercontent.com/DeusData/codebase-memory-mcp/main/install.sh")
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
	binPath := filepath.Join(dwytBin, "rtk")
	if runtime.GOOS == "windows" {
		binPath += ".exe"
	}
	if _, err := os.Stat(binPath); err == nil {
		fmt.Println("  ✓ RTK já instalado")
		exec.Command(binPath, "init", "-g", "--yes").Run()
		return nil
	}
	os.MkdirAll(dwytBin, 0755)
	script := fetch("https://raw.githubusercontent.com/rtk-ai/rtk/refs/heads/master/install.sh")
	cmd := exec.Command("sh")
	stdin, _ := cmd.StdinPipe()
	go func() { io.WriteString(stdin, script); stdin.Close() }()
	cmd.Run()

	for _, candidate := range []string{
		os.Getenv("HOME") + "/.local/bin/rtk",
		"/usr/local/bin/rtk",
	} {
		if data, err := os.ReadFile(candidate); err == nil {
			os.WriteFile(binPath, data, 0755)
			break
		}
	}
	exec.Command(binPath, "init", "-g", "--yes").Run()
	return nil
}

func Headroom(dwytBin, dwytHome string) error {
	wrapperPath := filepath.Join(dwytBin, "headroom")
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

	pipBin := filepath.Join(venvDir, "bin", "pip")
	if runtime.GOOS == "windows" {
		pipBin = filepath.Join(venvDir, "Scripts", "pip.exe")
	}
	exec.Command(pipBin, "install", "--quiet", "--upgrade", "pip").Run()
	if err := exec.Command(pipBin, "install", "--quiet", "headroom-ai[proxy]").Run(); err != nil {
		return fmt.Errorf("pip install headroom: %w", err)
	}

	hrBin := filepath.Join(venvDir, "bin", "headroom")
	if runtime.GOOS == "windows" {
		hrBin = filepath.Join(venvDir, "Scripts", "headroom.exe")
	}
	os.Symlink(hrBin, wrapperPath)
	return nil
}

func MemStack(dwytBin, dwytHome string) error {
	dir := filepath.Join(dwytHome, "memstack")
	if _, err := os.Stat(dir + "/.git"); err == nil {
		fmt.Println("  ✓ MemStack já existe (atualizando...)")
		exec.Command("git", "-C", dir, "pull", "--quiet").Run()
		return nil
	}
	cmd := exec.Command("git", "clone", "--depth=1", "https://github.com/cwinvestments/memstack.git", dir)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("memstack clone: %w", err)
	}
	wrapper := fmt.Sprintf("#!/usr/bin/env bash\nMEMSTACK_HOME=%q\nexec python3 \"${MEMSTACK_HOME}/db/memstack-db.py\" \"$@\"\n", dir)
	os.WriteFile(dwytBin+"/memstack", []byte(wrapper), 0755)
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

// Wrappers creates the dwyt-codex, dwyt-opencode and dwyt-ui shell scripts in dwytBin.
func Wrappers(dwytBin, dwytHome string) error {
	os.MkdirAll(dwytBin, 0755)

	codex := `#!/usr/bin/env bash
set -euo pipefail
HEADROOM_PORT="${HEADROOM_PORT:-8787}"
HEADROOM_URL="http://127.0.0.1:${HEADROOM_PORT}"
HEADROOM_PID_FILE="${HOME}/.dwyt/.codex-headroom.pid"
is_headroom_healthy() { curl -fsS "${HEADROOM_URL}/health" >/dev/null 2>&1; }
start_headroom() {
  is_headroom_healthy && return 0
  nohup headroom proxy --port "${HEADROOM_PORT}" >/dev/null 2>&1 &
  echo $! > "${HEADROOM_PID_FILE}"
  for _ in {1..20}; do is_headroom_healthy && return 0; sleep 1; done
  echo "Falha ao iniciar o Headroom em ${HEADROOM_URL}" >&2; exit 1
}
start_headroom
exec codex -c "openai_base_url=\"${HEADROOM_URL}/v1\"" "$@"
`
	opencode := `#!/usr/bin/env bash
set -euo pipefail
HEADROOM_PORT="${HEADROOM_PORT:-8787}"
HEADROOM_URL="http://127.0.0.1:${HEADROOM_PORT}"
HEADROOM_PID_FILE="${HOME}/.dwyt/.opencode-headroom.pid"
is_headroom_healthy() { curl -fsS "${HEADROOM_URL}/health" >/dev/null 2>&1; }
start_headroom() {
  is_headroom_healthy && return 0
  nohup headroom proxy --port "${HEADROOM_PORT}" >/dev/null 2>&1 &
  echo $! > "${HEADROOM_PID_FILE}"
  for _ in {1..20}; do is_headroom_healthy && return 0; sleep 1; done
  echo "Falha ao iniciar o Headroom em ${HEADROOM_URL}" >&2; exit 1
}
start_headroom
export ANTHROPIC_BASE_URL="${HEADROOM_URL}"
export OPENAI_BASE_URL="${HEADROOM_URL}/v1"
exec opencode "$@"
`
	dwytUI := fmt.Sprintf(`#!/usr/bin/env bash
DWYT_HOME="%s"
DWYT_BIN="%s"
UI_PORT=9749
UI_PID_FILE="${DWYT_HOME}/.ui.pid"
stop_ui() {
  if [[ -f "$UI_PID_FILE" ]]; then
    kill "$(cat $UI_PID_FILE)" 2>/dev/null && echo "UI parada." || echo "Processo já encerrado."
    rm -f "$UI_PID_FILE"
  else echo "UI não está rodando."; fi
}
start_ui() {
  stop_ui 2>/dev/null
  for BIN in "${DWYT_BIN}/codebase-memory-mcp-ui" "${DWYT_BIN}/codebase-memory-mcp"; do
    if [[ -x "$BIN" ]]; then
      echo "Iniciando UI na porta $UI_PORT..."
      if "$BIN" --help 2>&1 | grep -q "\-\-ui="; then
        "$BIN" --ui=true --port="$UI_PORT" &>/dev/null &
      elif "$BIN" --help 2>&1 | grep -q "serve"; then
        "$BIN" serve --port "$UI_PORT" &>/dev/null &
      else "$BIN" --port "$UI_PORT" &>/dev/null &; fi
      echo $! > "$UI_PID_FILE"
      sleep 2
      if kill -0 "$(cat $UI_PID_FILE)" 2>/dev/null; then
        echo "✓ UI rodando: http://localhost/${UI_PORT}  (PID $(cat $UI_PID_FILE))"
      else rm -f "$UI_PID_FILE"; echo "✗ UI não iniciou com $BIN"; continue; fi
      return 0
    fi
  done
  echo "Erro: nenhum binário encontrado. Verifique: ls ${DWYT_BIN}"; exit 1
}
case "${1:-start}" in stop) stop_ui ;; *) start_ui ;; esac
`, dwytHome, dwytBin)

	scripts := map[string]string{
		"dwyt-codex":    codex,
		"dwyt-opencode": opencode,
		"dwyt-ui":       dwytUI,
	}
	for name, content := range scripts {
		p := filepath.Join(dwytBin, name)
		if _, err := os.Stat(p); err == nil {
			continue // already exists
		}
		if err := os.WriteFile(p, []byte(content), 0755); err != nil {
			return fmt.Errorf("wrapper %s: %w", name, err)
		}
	}
	return nil
}
