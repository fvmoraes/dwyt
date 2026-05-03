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

// Wrappers creates launcher scripts for dwyt-codex, dwyt-opencode and dwyt-ui.
// On Unix: bash scripts. On Windows: .bat files.
func Wrappers(dwytBin, dwytHome string) error {
	os.MkdirAll(dwytBin, 0755)

	if runtime.GOOS == "windows" {
		return wrappersWindows(dwytBin, dwytHome)
	}
	return wrappersUnix(dwytBin, dwytHome)
}

func wrappersUnix(dwytBin, dwytHome string) error {
	headroomBin := filepath.Join(dwytBin, "headroom")
	codex := fmt.Sprintf(`#!/usr/bin/env bash
HEADROOM_PORT="${HEADROOM_PORT:-8787}"
HEADROOM_URL="http://127.0.0.1:${HEADROOM_PORT}"
HEADROOM_BIN=%q
HEADROOM_PID_FILE="${HOME}/.dwyt/.codex-headroom.pid"
is_headroom_healthy() { curl -fsS "${HEADROOM_URL}/health" >/dev/null 2>&1; }
try_headroom() {
  is_headroom_healthy && return 0
  if [[ -x "$HEADROOM_BIN" ]]; then
    nohup "$HEADROOM_BIN" proxy --port "${HEADROOM_PORT}" >/dev/null 2>&1 &
    echo $! > "${HEADROOM_PID_FILE}"
    for _ in {1..15}; do is_headroom_healthy && return 0; sleep 1; done
    echo "DWYT: Headroom nao iniciou — usando conexao direta" >&2
  fi
  return 1
}
if try_headroom; then
  exec codex -c "openai_base_url=\"${HEADROOM_URL}/v1\"" "$@"
else
  exec codex "$@"
fi
`, headroomBin)
	opencode := fmt.Sprintf(`#!/usr/bin/env bash
HEADROOM_PORT="${HEADROOM_PORT:-8787}"
HEADROOM_URL="http://127.0.0.1:${HEADROOM_PORT}"
HEADROOM_BIN=%q
HEADROOM_PID_FILE="${HOME}/.dwyt/.opencode-headroom.pid"
is_headroom_healthy() { curl -fsS "${HEADROOM_URL}/health" >/dev/null 2>&1; }
try_headroom() {
  is_headroom_healthy && return 0
  if [[ -x "$HEADROOM_BIN" ]]; then
    nohup "$HEADROOM_BIN" proxy --port "${HEADROOM_PORT}" >/dev/null 2>&1 &
    echo $! > "${HEADROOM_PID_FILE}"
    for _ in {1..15}; do is_headroom_healthy && return 0; sleep 1; done
    echo "DWYT: Headroom nao iniciou — usando conexao direta" >&2
  fi
  return 1
}
if try_headroom; then
  export ANTHROPIC_BASE_URL="${HEADROOM_URL}"
  export OPENAI_BASE_URL="${HEADROOM_URL}/v1"
fi
exec opencode "$@"
`, headroomBin)
	dwytUI := fmt.Sprintf(`#!/usr/bin/env bash
DWYT_HOME=%q
DWYT_BIN=%q
UI_PORT=9749
UI_PID_FILE="${DWYT_HOME}/.ui.pid"
stop_ui() {
  [[ -f "$UI_PID_FILE" ]] && kill "$(cat $UI_PID_FILE)" 2>/dev/null; rm -f "$UI_PID_FILE"
}
start_ui() {
  stop_ui 2>/dev/null
  for BIN in "${DWYT_BIN}/codebase-memory-mcp-ui" "${DWYT_BIN}/codebase-memory-mcp"; do
    [[ -x "$BIN" ]] || continue
    if "$BIN" --help 2>&1 | grep -q "\-\-ui="; then
      "$BIN" --ui=true --port="$UI_PORT" &>/dev/null &
    else
      "$BIN" --port "$UI_PORT" &>/dev/null &
    fi
    echo $! > "$UI_PID_FILE"; sleep 2
    kill -0 "$(cat $UI_PID_FILE)" 2>/dev/null && echo "✓ UI: http://localhost:${UI_PORT}" && return 0
    rm -f "$UI_PID_FILE"
  done
  echo "Erro: codebase-memory-mcp não encontrado em ${DWYT_BIN}"; exit 1
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
			continue
		}
		if err := os.WriteFile(p, []byte(content), 0755); err != nil {
			return fmt.Errorf("wrapper %s: %w", name, err)
		}
	}
	return nil
}

func wrappersWindows(dwytBin, dwytHome string) error {
	// dwyt-codex.bat
	codex := fmt.Sprintf(`@echo off
set HEADROOM_PORT=8787
set HEADROOM_URL=http://127.0.0.1:%%HEADROOM_PORT%%
"%s\headroom.bat" proxy --port %%HEADROOM_PORT%%
set OPENAI_BASE_URL=%%HEADROOM_URL%%/v1
codex -c "openai_base_url=\"%%HEADROOM_URL%%/v1\"" %%*
`, dwytBin)

	// dwyt-opencode.bat
	opencode := fmt.Sprintf(`@echo off
set HEADROOM_PORT=8787
set HEADROOM_URL=http://127.0.0.1:%%HEADROOM_PORT%%
"%s\headroom.bat" proxy --port %%HEADROOM_PORT%%
set ANTHROPIC_BASE_URL=%%HEADROOM_URL%%
set OPENAI_BASE_URL=%%HEADROOM_URL%%/v1
opencode %%*
`, dwytBin)

	// dwyt-ui.bat
	dwytUI := fmt.Sprintf(`@echo off
set DWYT_BIN=%s
set UI_PORT=9749
if exist "%%DWYT_BIN%%\codebase-memory-mcp.exe" (
  start "" "%%DWYT_BIN%%\codebase-memory-mcp.exe" --ui=true --port=%%UI_PORT%%
  echo UI iniciada em http://localhost:%%UI_PORT%%
) else (
  echo codebase-memory-mcp nao encontrado em %%DWYT_BIN%%
)
`, dwytBin)

	scripts := map[string]string{
		"dwyt-codex.bat":    codex,
		"dwyt-opencode.bat": opencode,
		"dwyt-ui.bat":       dwytUI,
	}
	for name, content := range scripts {
		p := filepath.Join(dwytBin, name)
		if _, err := os.Stat(p); err == nil {
			continue
		}
		if err := os.WriteFile(p, []byte(content), 0644); err != nil {
			return fmt.Errorf("wrapper %s: %w", name, err)
		}
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
