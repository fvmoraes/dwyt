#!/usr/bin/env bash
# =============================================================================
#  dwyt.sh — Don't Waste Your Tokens v2.0
#  Instala: codebase-memory-mcp + RTK + Headroom + MemStack
#  Tudo em ~/.dwyt/ — Linux (Ubuntu/Debian/Fedora), macOS e Windows (Git Bash)
#
#  Uso:
#    ./dwyt.sh            — instalação normal (com checklist)
#    ./dwyt.sh --reinstall — apaga ~/.dwyt e reinstala tudo do zero
# =============================================================================

set -euo pipefail

# ─── Cores & helpers ─────────────────────────────────────────────────────────
RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'
BLUE='\033[0;34m'; CYAN='\033[0;36m'; BOLD='\033[1m'; NC='\033[0m'
SYSTEM_PYTHON=""

info()    { echo -e "${CYAN}  →  $*${NC}"; }
success() { echo -e "${GREEN}  ✓  $*${NC}"; }
warn()    { echo -e "${YELLOW}  ⚠  $*${NC}"; }
error()   { echo -e "${RED}  ✗  $*${NC}" >&2; }
header()  { echo -e "\n${BOLD}${BLUE}══════════════════════════════════════════════${NC}";
            echo -e "${BOLD}${BLUE}  $*${NC}";
            echo -e "${BOLD}${BLUE}══════════════════════════════════════════════${NC}\n"; }
step()    { echo -e "\n${BOLD}${CYAN}  [$1] $2${NC}"; }

run_with_timeout() {
  local seconds="$1"
  shift

  if command -v timeout &>/dev/null; then
    timeout "$seconds" "$@"
  elif command -v gtimeout &>/dev/null; then
    gtimeout "$seconds" "$@"
  else
    "$@"
  fi
}

require_brew() {
  if ! command -v brew &>/dev/null; then
    error "Homebrew não encontrado."
    error "Instale em https://brew.sh e rode novamente: ./dwyt.sh"
    exit 1
  fi
}

can_use_dialog() {
  command -v dialog &>/dev/null && [[ -t 0 ]] && [[ -t 1 ]]
}

confirm_yes_no() {
  local prompt="$1"
  local default="${2:-y}"
  local hint="[Y/n]"

  [[ "$default" == "n" ]] && hint="[y/N]"

  while true; do
    local reply=""
    read -r -p "${prompt} ${hint} " reply || reply=""
    reply="${reply:-$default}"
    case "${reply,,}" in
      y|yes|s|sim) return 0 ;;
      n|no|nao|não) return 1 ;;
    esac
    warn "Responda com y/n."
  done
}

resolve_system_python() {
  local candidate=""

  if [[ -n "$SYSTEM_PYTHON" ]] && [[ -x "$SYSTEM_PYTHON" || -f "$SYSTEM_PYTHON" ]]; then
    printf '%s\n' "$SYSTEM_PYTHON"
    return 0
  fi

  for candidate in python3 python py py.exe; do
    if command -v "$candidate" &>/dev/null; then
      local resolved
      resolved="$(command -v "$candidate")"
      if "$resolved" -c 'import sys; raise SystemExit(0 if sys.version_info.major == 3 else 1)' &>/dev/null; then
        SYSTEM_PYTHON="$resolved"
        printf '%s\n' "$SYSTEM_PYTHON"
        return 0
      fi
    fi
  done

  return 1
}

venv_python_path() {
  local venv_dir="$1"
  local candidates=(
    "${venv_dir}/Scripts/python.exe"
    "${venv_dir}/Scripts/python"
    "${venv_dir}/bin/python3"
    "${venv_dir}/bin/python"
  )
  local candidate=""

  for candidate in "${candidates[@]}"; do
    if [[ -x "$candidate" || -f "$candidate" ]]; then
      printf '%s\n' "$candidate"
      return 0
    fi
  done

  return 1
}

venv_pip_path() {
  local venv_dir="$1"
  local candidates=(
    "${venv_dir}/Scripts/pip.exe"
    "${venv_dir}/Scripts/pip"
    "${venv_dir}/bin/pip3"
    "${venv_dir}/bin/pip"
  )
  local candidate=""

  for candidate in "${candidates[@]}"; do
    if [[ -x "$candidate" || -f "$candidate" ]]; then
      printf '%s\n' "$candidate"
      return 0
    fi
  done

  return 1
}

venv_headroom_path() {
  local venv_dir="$1"
  local candidates=(
    "${venv_dir}/Scripts/headroom.exe"
    "${venv_dir}/Scripts/headroom"
    "${venv_dir}/bin/headroom"
  )
  local candidate=""

  for candidate in "${candidates[@]}"; do
    if [[ -x "$candidate" || -f "$candidate" ]]; then
      printf '%s\n' "$candidate"
      return 0
    fi
  done

  return 1
}

find_windows_package_manager() {
  local candidate=""

  for candidate in winget winget.exe choco choco.exe scoop scoop.cmd; do
    if command -v "$candidate" &>/dev/null; then
      printf '%s\n' "$candidate"
      return 0
    fi
  done

  return 1
}

install_windows_dependency() {
  local dep="$1"
  local pkg_manager=""

  pkg_manager="$(find_windows_package_manager)" || return 1

  case "$pkg_manager" in
    winget|winget.exe)
      case "$dep" in
        git) "$pkg_manager" install --id Git.Git -e --accept-package-agreements --accept-source-agreements ;;
        python3)
          "$pkg_manager" install --id Python.Python.3.13 -e --accept-package-agreements --accept-source-agreements \
            || "$pkg_manager" install --id Python.Python.3.12 -e --accept-package-agreements --accept-source-agreements \
            || "$pkg_manager" install --id Python.Python.3.11 -e --accept-package-agreements --accept-source-agreements
          ;;
        node) "$pkg_manager" install --id OpenJS.NodeJS.LTS -e --accept-package-agreements --accept-source-agreements ;;
        *) return 1 ;;
      esac
      ;;
    choco|choco.exe)
      case "$dep" in
        git) "$pkg_manager" install -y git ;;
        python3) "$pkg_manager" install -y python ;;
        node) "$pkg_manager" install -y nodejs-lts ;;
        *) return 1 ;;
      esac
      ;;
    scoop|scoop.cmd)
      case "$dep" in
        git) "$pkg_manager" install git ;;
        python3) "$pkg_manager" install python ;;
        node) "$pkg_manager" install nodejs-lts ;;
        *) return 1 ;;
      esac
      ;;
    *)
      return 1
      ;;
  esac
}

link_or_copy_dir() {
  local source_dir="$1"
  local dest_path="$2"
  local label="${3:-diretório}"

  if ln -s "$source_dir" "$dest_path" 2>/dev/null; then
    success "${label} → symlink em $dest_path"
    return 0
  fi

  cp -R "$source_dir" "$dest_path"
  success "${label} → cópia criada em $dest_path"
}

extract_zip_archive() {
  local archive_path="$1"
  local dest_dir="$2"

  if command -v unzip &>/dev/null; then
    unzip -q "$archive_path" -d "$dest_dir"
    return 0
  fi

  if command -v powershell.exe &>/dev/null; then
    powershell.exe -NoLogo -NoProfile -Command \
      "Expand-Archive -LiteralPath '$archive_path' -DestinationPath '$dest_dir' -Force" \
      >/dev/null
    return 0
  fi

  return 1
}

patch_headroom_codex_ws() {
  local server_py=""
  local patch_python=""

  if [[ ! -d "$HEADROOM_VENV" ]]; then
    return 0
  fi

  patch_python="$(venv_python_path "$HEADROOM_VENV" || true)"
  [[ -z "$patch_python" ]] && patch_python="$(resolve_system_python || true)"

  if [[ -z "$patch_python" ]]; then
    warn "Patch do Headroom: Python não encontrado; pulando ajuste do Codex"
    return 0
  fi

  while IFS= read -r candidate; do
    server_py="$candidate"
    break
  done < <(find "$HEADROOM_VENV" -path '*/site-packages/headroom/proxy/server.py' 2>/dev/null | sort)

  if [[ -z "$server_py" ]]; then
    warn "Patch do Headroom: server.py não encontrado; pulando ajuste do Codex"
    return 0
  fi

  if "$patch_python" - "$server_py" <<'PY'
from pathlib import Path
import sys

path = Path(sys.argv[1])
text = path.read_text()
marker = "DWYT patch: bridge local Codex WebSocket traffic to upstream OpenAI SSE."

if marker in text:
    raise SystemExit(0)

start = "    async def handle_openai_responses_ws(self, websocket: WebSocket) -> None:\n"
end = "    async def handle_gemini_generate_content(\n"

start_idx = text.find(start)
end_idx = text.find(end, start_idx)

if start_idx == -1 or end_idx == -1:
    raise SystemExit(1)

replacement = '''    async def handle_openai_responses_ws(self, websocket: WebSocket) -> None:
        """DWYT patch: bridge local Codex WebSocket traffic to upstream OpenAI SSE.

        OpenAI's public Responses API streams over HTTP/SSE. Newer Codex clients
        speak WebSocket locally, so the proxy terminates the local socket and
        relays upstream SSE events as JSON frames.
        """
        request_id = await self._next_request_id()
        start_time = time.time()
        tokens_saved = 0
        body: dict[str, Any] | None = None

        ws_headers = dict(websocket.headers)

        client_subprotocols: list[str] = []
        raw_protocol = ws_headers.get("sec-websocket-protocol", "")
        if raw_protocol:
            client_subprotocols = [p.strip() for p in raw_protocol.split(",") if p.strip()]

        if client_subprotocols:
            await websocket.accept(subprotocol=client_subprotocols[0])
        else:
            await websocket.accept()

        _skip_headers = frozenset(
            {
                "host",
                "connection",
                "upgrade",
                "sec-websocket-key",
                "sec-websocket-version",
                "sec-websocket-extensions",
                "sec-websocket-accept",
                "sec-websocket-protocol",
                "content-length",
                "transfer-encoding",
            }
        )
        upstream_headers: dict[str, str] = {}
        for k, v in ws_headers.items():
            if k.lower() not in _skip_headers:
                upstream_headers[k] = v

        _has_auth = "authorization" in {k.lower() for k in upstream_headers}
        if not _has_auth:
            api_key = os.environ.get("OPENAI_API_KEY")
            if api_key:
                upstream_headers["Authorization"] = f"Bearer {api_key}"
                logger.debug(f"[{request_id}] WS bridge: injected Authorization from OPENAI_API_KEY")
            else:
                logger.warning(
                    f"[{request_id}] WS bridge: no Authorization header from client and "
                    f"OPENAI_API_KEY not set"
                )

        if "openai-beta" not in {k.lower() for k in upstream_headers}:
            upstream_headers["OpenAI-Beta"] = "responses-api=v1"

        upstream_headers["Accept"] = "text/event-stream"
        upstream_headers["Content-Type"] = "application/json"

        try:
            first_msg_raw = await websocket.receive_text()

            try:
                body = json.loads(first_msg_raw)
            except json.JSONDecodeError as e:
                logger.error(f"[{request_id}] WS bridge: invalid initial payload: {e}")
                await websocket.close(code=1003, reason="Invalid JSON payload")
                return

            if not isinstance(body, dict):
                logger.error(f"[{request_id}] WS bridge: unexpected initial payload type")
                await websocket.close(code=1003, reason="Unexpected payload type")
                return

            try:
                input_data = body.get("input")
                should_compress = (
                    self.config.optimize
                    and isinstance(input_data, list)
                    and len(input_data) > 1
                    and not body.get("previous_response_id")
                )
                if should_compress:
                    from headroom.proxy.responses_converter import (
                        messages_to_responses_items,
                        responses_items_to_messages,
                    )

                    model = body.get("model", "gpt-4o")
                    converted, preserved = responses_items_to_messages(input_data)

                    messages: list[dict[str, Any]] = []
                    instructions = body.get("instructions")
                    if instructions:
                        messages.append({"role": "system", "content": instructions})
                    messages.extend(converted)

                    tokenizer = get_tokenizer(model)
                    original_tokens = tokenizer.count_messages(messages)

                    context_limit = self.openai_provider.get_context_limit(model)
                    result = await asyncio.wait_for(
                        asyncio.to_thread(
                            lambda: self.openai_pipeline.apply(
                                messages=messages,
                                model=model,
                                model_limit=context_limit,
                                context=extract_user_query(messages),
                            )
                        ),
                        timeout=COMPRESSION_TIMEOUT_SECONDS,
                    )

                    if result.messages != messages:
                        opt = result.messages
                        if instructions and opt and opt[0].get("role") == "system":
                            body["instructions"] = opt[0]["content"]
                            opt = opt[1:]
                        body["input"] = messages_to_responses_items(opt, input_data, preserved)
                        tokens_saved = max(0, original_tokens - result.tokens_after)
                        logger.info(
                            f"[{request_id}] WS /v1/responses compressed via SSE bridge: "
                            f"saved {tokens_saved} tokens"
                        )
            except Exception as e:
                logger.warning(f"[{request_id}] WS bridge compression failed: {e}")

            body["stream"] = True

            assert self.http_client is not None, "http_client must be initialized before WS bridge"

            upstream_url = f"{self.OPENAI_API_URL}/v1/responses"
            upstream_request = self.http_client.build_request(
                "POST",
                upstream_url,
                json=body,
                headers=upstream_headers,
            )
            upstream_response = await self.http_client.send(upstream_request, stream=True)

            if upstream_response.is_error:
                error_bytes = await upstream_response.aread()
                error_text = error_bytes[:500].decode("utf-8", errors="replace").strip()
                logger.error(
                    f"[{request_id}] WS bridge upstream HTTP {upstream_response.status_code}: "
                    f"{error_text or 'empty body'}"
                )
                error_event = {
                    "type": "error",
                    "error": {
                        "type": "upstream_error",
                        "message": error_text or f"Unexpected upstream status {upstream_response.status_code}",
                    },
                }
                with contextlib.suppress(Exception):
                    await websocket.send_text(json.dumps(error_event))
                with contextlib.suppress(Exception):
                    await websocket.close(
                        code=1011,
                        reason=f"upstream {upstream_response.status_code}",
                    )
                return

            stream_state: dict[str, Any] = {
                "input_tokens": None,
                "output_tokens": None,
                "cache_read_input_tokens": 0,
                "sse_buffer": "",
            }
            event_buffer = ""

            async with contextlib.aclosing(upstream_response) as response:
                async for chunk in response.aiter_bytes():
                    chunk_text = chunk.decode("utf-8", errors="ignore")
                    event_buffer += chunk_text
                    stream_state["sse_buffer"] += chunk_text

                    if len(event_buffer) > MAX_SSE_BUFFER_SIZE:
                        event_buffer = event_buffer[-MAX_SSE_BUFFER_SIZE // 2 :]
                    if len(stream_state["sse_buffer"]) > MAX_SSE_BUFFER_SIZE:
                        stream_state["sse_buffer"] = stream_state["sse_buffer"][
                            -MAX_SSE_BUFFER_SIZE // 2 :
                        ]

                    usage = self._parse_sse_usage_from_buffer(stream_state, "openai")
                    if usage:
                        if "input_tokens" in usage:
                            stream_state["input_tokens"] = usage["input_tokens"]
                        if "output_tokens" in usage:
                            stream_state["output_tokens"] = usage["output_tokens"]
                        if "cache_read_input_tokens" in usage:
                            stream_state["cache_read_input_tokens"] = usage[
                                "cache_read_input_tokens"
                            ]

                    while "\\n\\n" in event_buffer:
                        event_end = event_buffer.index("\\n\\n")
                        event_text = event_buffer[: event_end + 2]
                        event_buffer = event_buffer[event_end + 2 :]

                        for line in event_text.split("\\n"):
                            if not line.startswith("data: "):
                                continue
                            data_str = line[6:].strip()
                            if not data_str or data_str == "[DONE]":
                                continue
                            await websocket.send_text(data_str)

            total_latency = (time.time() - start_time) * 1000
            model_name = body.get("model", "unknown")
            await self.metrics.record_request(
                provider="openai",
                model=model_name,
                input_tokens=stream_state["input_tokens"] or 0,
                output_tokens=stream_state["output_tokens"] or 0,
                tokens_saved=tokens_saved,
                latency_ms=total_latency,
            )

            with contextlib.suppress(Exception):
                await websocket.close()

        except Exception as e:
            if "WebSocketDisconnect" not in type(e).__name__:
                logger.error(f"[{request_id}] WS SSE bridge error: {e}")
                error_event = {
                    "type": "error",
                    "error": {"type": "proxy_error", "message": str(e)},
                }
                with contextlib.suppress(Exception):
                    await websocket.send_text(json.dumps(error_event))
            with contextlib.suppress(Exception):
                await websocket.close(code=1011, reason=str(e)[:120])
'''

text = text[:start_idx] + replacement + "\n\n" + text[end_idx:]
path.write_text(text)
PY
  then
    success "Headroom ajustado para Codex via bridge WS->SSE"
  else
    warn "Patch do Headroom para Codex não pôde ser aplicado automaticamente"
  fi
}

# ─── Constantes — TUDO dentro de ~/.dwyt ─────────────────────────────────────
DWYT_HOME="${HOME}/.dwyt"
DWYT_BIN="${DWYT_HOME}/bin"
DWYT_DATA="${DWYT_HOME}/data"          # banco SQLite e dados persistentes
CBMCP_DIR="${DWYT_HOME}/codebase-memory-mcp"
RTK_DIR="${DWYT_HOME}/rtk"
HEADROOM_VENV="${DWYT_HOME}/headroom-venv"
MEMSTACK_DIR="${DWYT_HOME}/memstack"
DWYT_ENV_FILE="${DWYT_HOME}/env.sh"   # exportado pelo shell rc
SHELL_RC=""
SHELL_LOGIN_RC=""
OS=""
TOOLS=""
CLIENTS=""
CHOSEN_REPO=""
DWYT_MODE="install"
DIRECT_REPO_PATH=""

# ─── Argumento --reinstall ────────────────────────────────────────────────────
handle_args() {
  case "${1:-}" in
    --repo)
      DWYT_MODE="repo"
      DIRECT_REPO_PATH="${2:-$PWD}"
      ;;
    --reinstall)
      warn "--reinstall: apagando ~/.dwyt e reinstalando tudo..."
      rm -rf "$DWYT_HOME"
      success "~/.dwyt removido. Iniciando instalação limpa."
      ;;
    --uninstall)
      uninstall
      ;;
    --help|-h)
      echo "Uso: ./dwyt.sh [opção]"
      echo ""
      echo "  (sem opção)    instalação interativa com checklist"
      echo "  --repo [path]  integra e indexa um repositório sem reinstalar tudo"
      echo "  --reinstall    apaga ~/.dwyt e reinstala tudo do zero"
      echo "  --uninstall    remove todas as ferramentas instaladas"
      echo "  --help         mostra esta mensagem"
      exit 0
      ;;
  esac
}

write_headroom_wrapper() {
  local wrapper_path="$1"

  cat > "$wrapper_path" <<'WEOF'
#!/usr/bin/env bash
set -euo pipefail

resolve_venv_exec() {
  local base_dir="VENV_PLACEHOLDER"
  local name="$1"
  local candidates=(
    "${base_dir}/Scripts/${name}.exe"
    "${base_dir}/Scripts/${name}"
    "${base_dir}/bin/${name}"
  )

  if [[ "$name" == "python" ]]; then
    candidates=(
      "${base_dir}/Scripts/python.exe"
      "${base_dir}/Scripts/python"
      "${base_dir}/bin/python3"
      "${base_dir}/bin/python"
    )
  fi

  local candidate=""
  for candidate in "${candidates[@]}"; do
    if [[ -x "$candidate" || -f "$candidate" ]]; then
      printf '%s\n' "$candidate"
      return 0
    fi
  done

  echo "Executável do virtualenv não encontrado para: ${name}" >&2
  exit 1
}

extract_headroom_port() {
  local port="8787"
  local args=("$@")
  local i=0

  while [[ $i -lt ${#args[@]} ]]; do
    case "${args[$i]}" in
      -p|--port)
        if [[ $((i + 1)) -lt ${#args[@]} ]]; then
          port="${args[$((i + 1))]}"
        fi
        i=$((i + 2))
        continue
        ;;
      --port=*)
        port="${args[$i]#--port=}"
        ;;
    esac
    i=$((i + 1))
  done

  printf '%s\n' "$port"
}

sync_codex_config_for_headroom() {
  local port="$1"

  if ! command -v codex >/dev/null 2>&1; then
    return 0
  fi

  local status_output=""
  status_output="$(codex login status 2>&1 || true)"

  local action="unset"
  if printf '%s\n' "$status_output" | grep -q "Logged in using API key"; then
    action="set"
  fi

  local config_path="${HOME}/.codex/config.toml"
  mkdir -p "$(dirname "$config_path")"
  [[ -f "$config_path" ]] || touch "$config_path"

  "$(resolve_venv_exec python)" - "$config_path" "$action" "http://127.0.0.1:${port}/v1" <<'PYCODBASE'
import sys
from pathlib import Path

config_path = Path(sys.argv[1])
action = sys.argv[2]
base_url = sys.argv[3]
text = config_path.read_text() if config_path.exists() else ""


def split_top_level(src: str):
    lines = src.splitlines()
    insert_at = len(lines)
    for i, line in enumerate(lines):
        if line.lstrip().startswith("["):
            insert_at = i
            break
    return lines[:insert_at], lines[insert_at:]


def set_top_level_key(src: str, key: str, value: str) -> str:
    top_level, rest = split_top_level(src)
    line = f'{key} = "{value}"'

    for idx, existing in enumerate(top_level):
        if existing.startswith(f"{key} "):
            top_level[idx] = line
            break
    else:
        if top_level and top_level[-1] != "":
            top_level.append("")
        top_level.append(line)
        top_level.append("")

    out = top_level + rest
    return "\n".join(out).rstrip() + "\n"


def unset_top_level_key(src: str, key: str) -> str:
    top_level, rest = split_top_level(src)
    filtered = [line for line in top_level if not line.startswith(f"{key} ")]

    while filtered and filtered[-1] == "":
        filtered.pop()

    out = filtered + ([""] if filtered and rest else []) + rest
    return "\n".join(out).rstrip() + ("\n" if out else "")


if action == "set":
    text = set_top_level_key(text, "openai_base_url", base_url)
else:
    text = unset_top_level_key(text, "openai_base_url")

config_path.write_text(text)
PYCODBASE
}

if [[ "${1:-}" == "wrap" && "${2:-}" == "codex" ]]; then
  sync_codex_config_for_headroom "$(extract_headroom_port "${@:3}")"
fi

exec "$(resolve_venv_exec headroom)" "$@"
WEOF

  sed -i.bak "s|VENV_PLACEHOLDER|${HEADROOM_VENV}|g" "$wrapper_path" && rm -f "${wrapper_path}.bak"
  chmod +x "$wrapper_path"
}

quick_integrate_repo() {
  local repo_input="${1:-$PWD}"
  local repo_path=""
  local system_python=""
  repo_path="$(cd "$repo_input" 2>/dev/null && pwd)" || {
    error "Caminho inválido: $repo_input"
    exit 1
  }

  system_python="$(resolve_system_python)" || {
    error "Python 3 não encontrado."
    error "Instale Python 3 e rode novamente: ./dwyt.sh --repo \"$repo_input\""
    exit 1
  }

  if [[ ! -d "$repo_path" ]]; then
    error "Repositório não encontrado: $repo_path"
    exit 1
  fi

  local codebase_bin="${DWYT_BIN}/codebase-memory-mcp"
  local codex_home="${HOME}/.codex"
  local codex_config="${codex_home}/config.toml"
  local gitignore_file="${repo_path}/.gitignore"
  local mcp_file="${repo_path}/.mcp.json"
  local agents_file="${repo_path}/AGENTS.md"

  if [[ ! -x "$codebase_bin" ]]; then
    error "codebase-memory-mcp não encontrado em $codebase_bin"
    error "Rode ./dwyt.sh ao menos uma vez para instalar as ferramentas base."
    exit 1
  fi

  mkdir -p "$codex_home"
  [[ -f "$codex_config" ]] || touch "$codex_config"

  if [[ -f "$gitignore_file" ]]; then
    grep -qxF "# dwyt" "$gitignore_file" || printf '\n# dwyt\n' >> "$gitignore_file"
  else
    printf '# dwyt\n' > "$gitignore_file"
  fi
  grep -qxF ".codex" "$gitignore_file" || printf '.codex\n' >> "$gitignore_file"
  grep -qxF ".mcp.json" "$gitignore_file" || printf '.mcp.json\n' >> "$gitignore_file"
  grep -qxF "AGENTS.md" "$gitignore_file" || printf 'AGENTS.md\n' >> "$gitignore_file"

  cat > "$mcp_file" <<'EOF'
{
  "mcpServers": {
    "codebase-memory-mcp": {
      "type": "stdio",
      "command": "codebase-memory-mcp"
    }
  }
}
EOF

  local dwyt_agents_section
  dwyt_agents_section=$(cat <<'EOF'
# DWYT — Don't Waste Your Tokens

Este repositório usa integrações locais opcionais do DWYT.

- Se o MCP em `.mcp.json` estiver conectado e respondendo, prefira `codebase-memory-mcp` antes de explorar arquivos manualmente
- Se o MCP não estiver disponível, faça fallback silencioso para busca manual
- Se o projeto estiver registrado no `~/.codex/config.toml`, trate-o como trusted no Codex
- Use `rtk <comando>` quando o binário existir e isso ajudar a reduzir output
- Use Headroom apenas quando a sessão tiver sido aberta com wrapper/proxy ativo
EOF
)

  if [[ -f "$agents_file" ]]; then
    if ! grep -qF "# DWYT — Don't Waste Your Tokens" "$agents_file"; then
      printf '\n---\n%s\n' "$dwyt_agents_section" >> "$agents_file"
    fi
  else
    printf '%s\n' "$dwyt_agents_section" > "$agents_file"
  fi

  "$system_python" - "$codex_config" "$codebase_bin" "$repo_path" << 'PYREPO'
import re
import sys
from pathlib import Path

config_path = Path(sys.argv[1])
mcp_command = sys.argv[2]
repo_path = sys.argv[3]
text = config_path.read_text() if config_path.exists() else ""


def upsert_section_value(src: str, section: str, key: str, value: str) -> str:
    section_header = f"[{section}]"
    line = f'{key} = "{value}"'
    pattern = re.compile(rf"(?ms)^\[{re.escape(section)}\]\n(.*?)(?=^\[|\Z)")
    match = pattern.search(src)
    if match:
        body = match.group(1)
        key_pattern = re.compile(rf"(?m)^{re.escape(key)}\s*=.*$")
        if key_pattern.search(body):
            new_body = key_pattern.sub(line, body, count=1)
        else:
            if body and not body.endswith("\n"):
                body += "\n"
            new_body = body + line + "\n"
        return src[:match.start(1)] + new_body + src[match.end(1):]

    if src and not src.endswith("\n"):
        src += "\n"
    if src and not src.endswith("\n\n"):
        src += "\n"
    return src + section_header + "\n" + line + "\n"


text = upsert_section_value(
    text,
    "mcp_servers.codebase-memory-mcp",
    "command",
    mcp_command,
)
text = upsert_section_value(
    text,
    f'projects."{repo_path}"',
    "trust_level",
    "trusted",
)

config_path.write_text(text)
PYREPO

  info "Indexando repositório..."
  printf '%s\n%s\n' \
    '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"dwyt-repo","version":"2.0"}}}' \
    "{\"jsonrpc\":\"2.0\",\"id\":2,\"method\":\"tools/call\",\"params\":{\"name\":\"index_repository\",\"arguments\":{\"repo_path\":\"${repo_path}\"}}}" \
    | run_with_timeout 120 "$codebase_bin" 2>&1 | grep -v "^$" | tail -3 || true

  success "Repositório preparado: $repo_path"
  success ".mcp.json criado/atualizado"
  success "AGENTS.md criado/atualizado"
  success "~/.codex/config.toml atualizado"
  success "index_repository disparado"
  exit 0
}

# ─── Uninstall ───────────────────────────────────────────────────────────────
uninstall() {
  clear
  echo -e "${BOLD}${RED}"
  echo "  ╔══════════════════════════════════════════════════════════╗"
  echo "  ║   🗑️  DWYT — Desinstalação                              ║"
  echo "  ╚══════════════════════════════════════════════════════════╝"
  echo -e "${NC}"

  detect_env

  if can_use_dialog; then
    dialog     --backtitle "dwyt — Don't Waste Your Tokens"     --title "Confirmar desinstalação"     --yesno "Isso irá remover:

  • ~/.dwyt/  (binários, venvs, memstack)
  • Linhas do dwyt em $SHELL_RC
  • Hook RTK global (~/.claude/hooks/rtk-rewrite.sh)
  • Banco do codebase-memory-mcp (~/.cache/codebase-memory-mcp/)

Não remove arquivos dos seus projetos (.mcp.json, AGENTS.md, CLAUDE.md, .claude/, .cursor/, .kiro/, .github/).

Deseja continuar?"     18 65 || { clear; info "Desinstalação cancelada."; exit 0; }
  else
    confirm_yes_no "Remover ~/.dwyt e os hooks globais do DWYT?" n || { info "Desinstalação cancelada."; exit 0; }
  fi
  clear

  # ── Remove ~/.dwyt ────────────────────────────────────────────────────────
  if [[ -d "${HOME}/.dwyt" ]]; then
    rm -rf "${HOME}/.dwyt"
    success "~/.dwyt removido"
  else
    warn "~/.dwyt não encontrado — nada a remover"
  fi

  # ── Remove banco do codebase-memory-mcp ──────────────────────────────────
  if [[ -d "${HOME}/.cache/codebase-memory-mcp" ]]; then
    if can_use_dialog; then
      dialog       --backtitle "dwyt — Don't Waste Your Tokens"       --title "Banco de dados do grafo"       --yesno "Remover também o banco SQLite do codebase-memory-mcp?
(~/.cache/codebase-memory-mcp/)

Contém todos os índices dos seus projetos."       10 60 && rm -rf "${HOME}/.cache/codebase-memory-mcp" && success "Cache do codebase-memory-mcp removido"
    elif confirm_yes_no "Remover também o cache ~/.cache/codebase-memory-mcp?" n; then
      rm -rf "${HOME}/.cache/codebase-memory-mcp"
      success "Cache do codebase-memory-mcp removido"
    fi
    clear
  fi

  # ── Remove hook RTK global ────────────────────────────────────────────────
  if [[ -f "${HOME}/.claude/hooks/rtk-rewrite.sh" ]]; then
    rm -f "${HOME}/.claude/hooks/rtk-rewrite.sh"
    success "Hook RTK global removido"
  fi

  # ── Remove linhas do dwyt no shell rc ────────────────────────────────────
  if [[ -f "$SHELL_RC" ]] && grep -q "dwyt" "$SHELL_RC" 2>/dev/null; then
    # Remove bloco entre "# dwyt:source" e a linha do source
    local TMP
    TMP=$(mktemp)
    grep -v "dwyt" "$SHELL_RC" > "$TMP" && mv "$TMP" "$SHELL_RC"
    success "Entradas dwyt removidas de $SHELL_RC"
  fi

  # ── Remove RTK global (~/.local/bin/rtk) — pergunta ─────────────────────
  if command -v rtk &>/dev/null; then
    local RTK_PATH
    RTK_PATH=$(command -v rtk)
    if can_use_dialog; then
      dialog       --backtitle "dwyt — Don't Waste Your Tokens"       --title "RTK global"       --yesno "Remover o binário RTK em:
$RTK_PATH

(instalado pelo install.sh do RTK)"       10 60 && rm -f "$RTK_PATH" && success "RTK removido de $RTK_PATH"
    elif confirm_yes_no "Remover também o binário global do RTK em $RTK_PATH?" n; then
      rm -f "$RTK_PATH"
      success "RTK removido de $RTK_PATH"
    fi
    clear
  fi

  echo -e "${BOLD}${GREEN}"
  echo "  ✓  Desinstalação concluída."
  echo "  Recarregue o shell: source ${SHELL_RC}"
  echo -e "${NC}"
  exit 0
}

# ─── Detectar OS & Shell ──────────────────────────────────────────────────────
detect_env() {
  if [[ "$OSTYPE" == "darwin"* ]]; then
    OS="macos"
  elif [[ "$OSTYPE" == msys* ]] || [[ "$OSTYPE" == cygwin* ]] || [[ "$OSTYPE" == win32* ]] || [[ "$OSTYPE" == mingw* ]] || [[ -n "${MSYSTEM:-}" ]]; then
    OS="windows"
  elif [[ -f /etc/debian_version ]]; then
    OS="debian"
  elif [[ -f /etc/fedora-release ]] || [[ -f /etc/redhat-release ]]; then
    OS="fedora"
  else
    error "Sistema não suportado (Linux Debian/Ubuntu, Fedora, macOS ou Windows via Git Bash)."
    exit 1
  fi

  if [[ "$SHELL" == */zsh ]] || [[ -n "${ZSH_VERSION:-}" ]]; then
    SHELL_RC="${HOME}/.zshrc"
    SHELL_LOGIN_RC="${HOME}/.zprofile"
  else
    SHELL_RC="${HOME}/.bashrc"
    if [[ -f "${HOME}/.bash_profile" ]]; then
      SHELL_LOGIN_RC="${HOME}/.bash_profile"
    else
      SHELL_LOGIN_RC="${HOME}/.profile"
    fi
  fi
}

# ─── Registra PATH/exports centralizados em ~/.dwyt/env.sh ───────────────────
init_env_file() {
  mkdir -p "$DWYT_HOME" "$DWYT_BIN" "$DWYT_DATA"

  # Cria/recria o env.sh central
  cat > "$DWYT_ENV_FILE" << ENVEOF
# ── DWYT — Don't Waste Your Tokens ──────────────────────────────────────────
# Gerado automaticamente por dwyt.sh — não edite manualmente

# Redireciona codebase-memory-mcp para salvar dados em ~/.dwyt/data/
# (padrão seria ~/.cache/codebase-memory-mcp/)
export XDG_CACHE_HOME="${DWYT_DATA}"
ENVEOF

  # Injeta source no shell rc uma única vez
  local marker="# dwyt:source"
  if ! grep -qF "$marker" "$SHELL_RC" 2>/dev/null; then
    cat >> "$SHELL_RC" << EOF

$marker
[[ -f "${DWYT_ENV_FILE}" ]] && source "${DWYT_ENV_FILE}"
EOF
    info "Source do dwyt/env.sh adicionado a $SHELL_RC"
  fi

  if [[ -n "$SHELL_LOGIN_RC" ]] && ! grep -qF "$marker" "$SHELL_LOGIN_RC" 2>/dev/null; then
    cat >> "$SHELL_LOGIN_RC" << EOF

$marker
[[ -f "${DWYT_ENV_FILE}" ]] && source "${DWYT_ENV_FILE}"
EOF
    info "Source do dwyt/env.sh adicionado a $SHELL_LOGIN_RC"
  fi
}

append_env() {
  # append_env "export VAR=value" "comentário"
  local line="$1"
  local comment="${2:-}"
  [[ -n "$comment" ]] && echo "# $comment" >> "$DWYT_ENV_FILE"
  echo "$line" >> "$DWYT_ENV_FILE"
  # Aplica na sessão atual também
  eval "$line" 2>/dev/null || true
}

configure_codex_cli() {
  [[ "$CLIENTS" != *codex* ]] && return

  local codex_home="${HOME}/.codex"
  local codex_config="${codex_home}/config.toml"

  mkdir -p "$codex_home"
  [[ -f "$codex_config" ]] || touch "$codex_config"

  "$(resolve_system_python)" - "$codex_config" "${DWYT_BIN}/codebase-memory-mcp" << 'PYCODEX'
import re
import sys
from pathlib import Path

config_path = Path(sys.argv[1])
mcp_command = sys.argv[2]
text = config_path.read_text() if config_path.exists() else ""


def upsert_section_value(src: str, section: str, key: str, value: str) -> str:
    section_header = f"[{section}]"
    line = f'{key} = "{value}"'
    pattern = re.compile(rf"(?ms)^\[{re.escape(section)}\]\n(.*?)(?=^\[|\Z)")
    match = pattern.search(src)
    if match:
      body = match.group(1)
      key_pattern = re.compile(rf"(?m)^{re.escape(key)}\s*=.*$")
      if key_pattern.search(body):
          new_body = key_pattern.sub(line, body, count=1)
      else:
          if body and not body.endswith("\n"):
              body += "\n"
          new_body = body + line + "\n"
      return src[:match.start(1)] + new_body + src[match.end(1):]

    if src and not src.endswith("\n"):
        src += "\n"
    if src and not src.endswith("\n\n"):
        src += "\n"
    return src + section_header + "\n" + line + "\n"


def remove_key_everywhere(src: str, key: str) -> str:
    pattern = re.compile(rf"(?m)^{re.escape(key)}\s*=.*\n?")
    cleaned = pattern.sub("", src)
    cleaned = re.sub(r"\n{3,}", "\n\n", cleaned)
    return cleaned.lstrip("\n")


text = upsert_section_value(
    text,
    "mcp_servers.codebase-memory-mcp",
    "command",
    mcp_command,
)
text = remove_key_everywhere(text, "openai_base_url")

config_path.write_text(text)
PYCODEX

  success "Codex CLI configurado em $codex_config"
}

# ─── Dependências base ────────────────────────────────────────────────────────
check_deps() {
  header "Verificando dependências base"
  local missing=()
  local base_cmds=(curl git)

  for cmd in "${base_cmds[@]}"; do
    if ! command -v "$cmd" &>/dev/null; then
      missing+=("$cmd")
    else
      success "$cmd ok"
    fi
  done

  if resolve_system_python &>/dev/null; then
    success "python ok ($("$(resolve_system_python)" --version 2>&1))"
  else
    missing+=("python3")
  fi

  if [[ ${#missing[@]} -gt 0 ]]; then
    warn "Instalando: ${missing[*]}"
    case "$OS" in
      macos)
        local macos_missing=()
        local dep=""
        require_brew
        for dep in "${missing[@]}"; do
          if [[ "$dep" == "python3" ]]; then
            macos_missing+=("python")
          else
            macos_missing+=("$dep")
          fi
        done
        brew install "${macos_missing[@]}" ;;
      debian) sudo apt-get update -qq && sudo apt-get install -y "${missing[@]}" ;;
      fedora) sudo dnf install -y "${missing[@]}" ;;
      windows)
        local dep=""
        for dep in "${missing[@]}"; do
          if install_windows_dependency "$dep"; then
            success "$dep instalado via gerenciador do Windows"
          else
            error "Dependência ausente no Windows: $dep"
            error "Use Git Bash com curl/git e instale Python 3 + Node.js via winget/choco/scoop."
            exit 1
          fi
        done
        hash -r
        ;;
    esac
  fi

  if ! resolve_system_python &>/dev/null; then
    error "Python 3 não foi encontrado após a instalação."
    error "Abra um novo terminal e rode novamente: ./dwyt.sh"
    exit 1
  fi

  # Node.js
  if ! command -v node &>/dev/null; then
    warn "Node.js não encontrado. Instalando..."
    case "$OS" in
      macos)
        require_brew
        brew install node ;;
      debian)
        curl -fsSL https://deb.nodesource.com/setup_lts.x | sudo -E bash -
        sudo apt-get install -y nodejs ;;
      fedora) sudo dnf install -y nodejs ;;
      windows)
        install_windows_dependency node || {
          error "Não foi possível instalar Node.js automaticamente."
          error "Instale Node.js LTS e rode novamente: ./dwyt.sh"
          exit 1
        }
        hash -r
        ;;
    esac
    success "Node.js instalado"
  else
    success "node ok ($(node --version))"
  fi

  if ! command -v node &>/dev/null; then
    error "Node.js não apareceu no PATH após a instalação."
    error "Abra um novo terminal e rode novamente: ./dwyt.sh"
    exit 1
  fi

  # python3-venv — instalado incondicionalmente no Debian
  if [[ "$OS" == "debian" ]]; then
    local PY_VER
    PY_VER=$("$(resolve_system_python)" -c "import sys; print(f'{sys.version_info.major}.{sys.version_info.minor}')")
    info "Garantindo python3-venv para Python $PY_VER..."
    sudo apt-get install -y python3-venv python3-pip "python${PY_VER}-venv" 2>/dev/null \
      || sudo apt-get install -y python3-venv python3-pip
    success "python3-venv ok"
  fi

  if ! can_use_dialog; then
    warn "dialog não disponível — usando prompts em texto."
  fi
}

# ─── Dialog: selecionar ferramentas ──────────────────────────────────────────
select_tools() {
  if can_use_dialog; then
    TOOLS=$(dialog \
      --backtitle "dwyt — Don't Waste Your Tokens" \
      --title "Selecione as ferramentas para instalar" \
      --checklist "ESPAÇO = marcar/desmarcar | ENTER = confirmar" 18 65 4 \
      "cbmcp"    "codebase-memory-mcp  (grafo + UI visual)"    ON \
      "rtk"      "RTK                  (comprime output CLI)"   ON \
      "headroom" "Headroom             (comprime chamadas API)" ON \
      "memstack" "MemStack             (memória entre sessões)" ON \
      3>&1 1>&2 2>&3) || {
        clear; error "Nenhuma ferramenta selecionada. Abortando."; exit 1
      }
  else
    TOOLS=""
    confirm_yes_no "Instalar codebase-memory-mcp (grafo + UI visual)?" y && TOOLS+=" cbmcp"
    confirm_yes_no "Instalar RTK (compressão de output CLI)?" y && TOOLS+=" rtk"
    confirm_yes_no "Instalar Headroom (compressão de chamadas API)?" y && TOOLS+=" headroom"
    confirm_yes_no "Instalar MemStack (memória entre sessões)?" y && TOOLS+=" memstack"
    TOOLS="${TOOLS# }"
    [[ -n "$TOOLS" ]] || { error "Nenhuma ferramenta selecionada. Abortando."; exit 1; }
  fi
  clear
}

# ─── Dialog: selecionar clientes LLM ─────────────────────────────────────────
select_clients() {
  if can_use_dialog; then
    CLIENTS=$(dialog \
      --backtitle "dwyt — Don't Waste Your Tokens" \
      --title "Selecione os clientes LLM para integrar" \
      --checklist "ESPAÇO = marcar/desmarcar | ENTER = confirmar" 20 72 6 \
      "claude"  "Claude Code        (.claude/CLAUDE.md, hooks)"         ON \
      "codex"   "Codex              (AGENTS.md + .codex/)"            ON \
      "copilot" "GitHub Copilot     (.github/copilot-instructions.md)" ON \
      "kiro"    "Kiro               (.kiro/steering + AGENTS.md)"      ON \
      "cursor"  "Cursor             (.cursor/rules + AGENTS.md)"       ON \
      3>&1 1>&2 2>&3) || {
        clear; error "Nenhum cliente selecionado. Abortando."; exit 1
      }
  else
    CLIENTS=""
    confirm_yes_no "Integrar Claude Code?" y && CLIENTS+=" claude"
    confirm_yes_no "Integrar Codex?" y && CLIENTS+=" codex"
    confirm_yes_no "Integrar GitHub Copilot?" y && CLIENTS+=" copilot"
    confirm_yes_no "Integrar Kiro?" y && CLIENTS+=" kiro"
    confirm_yes_no "Integrar Cursor?" y && CLIENTS+=" cursor"
    CLIENTS="${CLIENTS# }"
    [[ -n "$CLIENTS" ]] || { error "Nenhum cliente selecionado. Abortando."; exit 1; }
  fi
  clear
}

# ─── Dialog: navegador de diretórios interativo ──────────────────────────────
select_repo() {
  if ! can_use_dialog; then
    local repo_input=""
    while true; do
      echo
      info "Digite o caminho do projeto para integrar."
      info "Enter vazio usa o diretório atual (${PWD}); digite 'skip' para pular."
      read -r -p "> " repo_input || repo_input="skip"

      case "${repo_input:-}" in
        skip)
          warn "Integração de projeto pulada."
          CHOSEN_REPO=""
          return
          ;;
        "")
          repo_input="$PWD"
          ;;
      esac

      if CHOSEN_REPO="$(cd "$repo_input" 2>/dev/null && pwd)"; then
        success "Projeto selecionado: $CHOSEN_REPO"
        return
      fi

      warn "Caminho inválido: $repo_input"
    done
  fi

  local current_dir="$HOME"
  CHOSEN_REPO=""

  while true; do
    # Lista subdiretórios visíveis no diretório atual
    local subdirs=()
    while IFS= read -r d; do
      subdirs+=("$d")
    done < <(find "$current_dir" -mindepth 1 -maxdepth 1 -type d \
      ! -name ".*" \
      ! -name "node_modules" \
      ! -name "__pycache__" \
      ! -name "vendor" \
      ! -name ".dwyt" \
      2>/dev/null | sort)

    # Monta itens do menu dialog
    local items=()
    items+=("." "[ ✓  SELECIONAR ESTE DIRETÓRIO ]")
    if [[ "$current_dir" != "/" ]]; then
      items+=(".." "[ ←  VOLTAR ]  → $(dirname "$current_dir")")
    fi

    for d in "${subdirs[@]}"; do
      local name
      name="$(basename "$d")"
      local n
      n=$(find "$d" -mindepth 1 -maxdepth 1 -type d ! -name ".*" 2>/dev/null | wc -l | tr -d " \t")
      if [[ "$n" -gt 0 ]]; then
        items+=("$name" "${name}/   ▶  ($n subdir)")
      else
        items+=("$name" "${name}/")
      fi
    done

    # Linha de título mostrando caminho atual
    local title_line
    title_line="$(printf '📂  %s' "$current_dir")"

    local choice
    choice=$(dialog \
      --backtitle "dwyt — Don't Waste Your Tokens" \
      --title " Navegador de Projetos " \
      --extra-button --extra-label "Ir para /" \
      --ok-label "Confirmar" \
      --cancel-label "Cancelar" \
      --menu "$title_line\n\nSetas = navegar  |  Selecione [✓] para usar este diretório" \
      28 78 18 \
      "${items[@]}" \
      3>&1 1>&2 2>&3)
    local code=$?
    clear

    # Botão extra: ir para raiz
    if [[ $code -eq 3 ]]; then
      current_dir="/"
      continue
    fi

    # Cancelar
    if [[ $code -ne 0 ]]; then
      warn "Seleção cancelada — pulando integração de projeto."
      CHOSEN_REPO=""
      return
    fi

    case "$choice" in
      ".")
        # Seleciona o diretório atual
        CHOSEN_REPO="$current_dir"
        success "Projeto selecionado: $CHOSEN_REPO"
        return
        ;;
      "..")
        # Sobe um nível
        current_dir="$(dirname "$current_dir")"
        ;;
      *)
        # Entra no subdiretório
        local next="${current_dir%/}/${choice}"
        [[ -d "$next" ]] && current_dir="$next"
        ;;
    esac
  done
}


# ═════════════════════════════════════════════════════════════════════════════
# [1] codebase-memory-mcp  (binário + UI)
# ═════════════════════════════════════════════════════════════════════════════
install_cbmcp() {
  step "1/4" "codebase-memory-mcp — grafo do código + UI visual"

  local BIN="${DWYT_BIN}/codebase-memory-mcp"
  local UI_BIN="${DWYT_BIN}/codebase-memory-mcp-ui"
  local WIN_BIN="${DWYT_BIN}/codebase-memory-mcp.exe"
  local WIN_UI_BIN="${DWYT_BIN}/codebase-memory-mcp-ui.exe"

  if [[ -x "$BIN" ]] && "$BIN" --version &>/dev/null 2>&1; then
    success "codebase-memory-mcp já instalado em $BIN"
  else
    if [[ "$OS" == "windows" ]]; then
      info "Baixando codebase-memory-mcp para Windows direto do release oficial..."
      local arch
      arch="$(uname -m)"
      case "$arch" in
        x86_64|amd64) ;;
        *)
          error "codebase-memory-mcp para Windows está disponível apenas para x86_64/amd64 no release atual."
          return 1
          ;;
      esac

      local tmp_dir archive_path extract_dir
      tmp_dir="$(mktemp -d)"
      archive_path="${tmp_dir}/codebase-memory-mcp-win.zip"
      extract_dir="${tmp_dir}/extract"
      mkdir -p "$extract_dir"

      curl -fsSL \
        "https://github.com/DeusData/codebase-memory-mcp/releases/latest/download/codebase-memory-mcp-windows-amd64.zip" \
        -o "$archive_path" || {
          rm -rf "$tmp_dir"
          error "Falha ao baixar o codebase-memory-mcp para Windows."
          return 1
        }

      extract_zip_archive "$archive_path" "$extract_dir" || {
        rm -rf "$tmp_dir"
        error "Falha ao extrair o codebase-memory-mcp para Windows."
        error "É necessário ter 'unzip' ou 'powershell.exe' disponível no PATH."
        return 1
      }

      if [[ ! -f "${extract_dir}/codebase-memory-mcp.exe" ]]; then
        rm -rf "$tmp_dir"
        error "Arquivo codebase-memory-mcp.exe não encontrado após a extração."
        return 1
      fi

      cp "${extract_dir}/codebase-memory-mcp.exe" "$WIN_BIN"
      chmod +x "$WIN_BIN"
      cat > "$BIN" <<EOF
#!/usr/bin/env bash
exec "${WIN_BIN}" "\$@"
EOF
      chmod +x "$BIN"
      rm -rf "$tmp_dir"
      success "codebase-memory-mcp Windows instalado em $WIN_BIN"
    else
      info "Instalando binário padrão direto em ${DWYT_BIN}..."
      curl -fsSL \
        "https://raw.githubusercontent.com/DeusData/codebase-memory-mcp/main/install.sh" \
        | bash -s -- --dir="${DWYT_BIN}" --skip-config
    fi
  fi

  # Instala variante UI direto em ~/.dwyt/bin/ usando tmp dir para renomear
  if [[ ! -x "$UI_BIN" ]] || ! "$UI_BIN" --help &>/dev/null 2>&1; then
    if [[ "$OS" == "windows" ]]; then
      info "Baixando variante UI para Windows em ${DWYT_BIN}..."
      local tmp_dir archive_path extract_dir ui_download_ok=true
      tmp_dir="$(mktemp -d)"
      archive_path="${tmp_dir}/codebase-memory-mcp-ui-win.zip"
      extract_dir="${tmp_dir}/extract"
      mkdir -p "$extract_dir"

      curl -fsSL \
        "https://github.com/DeusData/codebase-memory-mcp/releases/latest/download/codebase-memory-mcp-ui-windows-amd64.zip" \
        -o "$archive_path" || {
          rm -rf "$tmp_dir"
          warn "UI não disponível nesta versão — somente binário padrão"
          ui_download_ok=false
        }

      if [[ "$ui_download_ok" == true ]]; then
        if extract_zip_archive "$archive_path" "$extract_dir" && [[ -f "${extract_dir}/codebase-memory-mcp.exe" ]]; then
          cp "${extract_dir}/codebase-memory-mcp.exe" "$WIN_UI_BIN"
          chmod +x "$WIN_UI_BIN"
          cat > "$UI_BIN" <<EOF
#!/usr/bin/env bash
exec "${WIN_UI_BIN}" "\$@"
EOF
          chmod +x "$UI_BIN"
          success "UI instalada em $WIN_UI_BIN"
        else
          warn "UI não disponível nesta versão — somente binário padrão"
        fi
      fi
      rm -rf "$tmp_dir"
    else
      info "Instalando variante com UI visual em ${DWYT_BIN}..."
      local TMP_UI="${DWYT_HOME}/.tmp-ui"
      mkdir -p "$TMP_UI"
      curl -fsSL \
        "https://raw.githubusercontent.com/DeusData/codebase-memory-mcp/main/install.sh" \
        | bash -s -- --ui --dir="$TMP_UI" --skip-config 2>/dev/null && {
          if [[ -x "${TMP_UI}/codebase-memory-mcp" ]]; then
            mv "${TMP_UI}/codebase-memory-mcp" "$UI_BIN"
            chmod +x "$UI_BIN"
            success "UI instalada em $UI_BIN"
          fi
        } || warn "UI não disponível nesta versão — somente binário padrão"
      rm -rf "$TMP_UI"
    fi
  fi

  append_env "export PATH=\"${DWYT_BIN}:\$PATH\"" "codebase-memory-mcp"
  export PATH="${DWYT_BIN}:$PATH"

  [[ -x "$BIN" ]] && success "codebase-memory-mcp pronto em $BIN" \
    || warn "Binário não encontrado — verifique $DWYT_BIN"
}

# ═════════════════════════════════════════════════════════════════════════════
# [2] RTK — Rust Token Killer
# ═════════════════════════════════════════════════════════════════════════════
install_rtk() {
  step "2/4" "RTK — Rust Token Killer"

  local BIN="${DWYT_BIN}/rtk"
  local WIN_BIN="${DWYT_BIN}/rtk.exe"

  if [[ -x "$BIN" ]] && "$BIN" gain &>/dev/null 2>&1; then
    success "RTK já instalado"
  else
    if [[ "$OS" == "windows" ]]; then
      info "Baixando RTK para Windows direto do release oficial..."
      local arch
      arch="$(uname -m)"
      case "$arch" in
        x86_64|amd64) ;;
        *)
          error "RTK para Windows está disponível apenas para x86_64/amd64 no release atual."
          return 1
          ;;
      esac

      local tmp_dir archive_url archive_path extract_dir
      tmp_dir="$(mktemp -d)"
      archive_url="https://github.com/rtk-ai/rtk/releases/latest/download/rtk-x86_64-pc-windows-msvc.zip"
      archive_path="${tmp_dir}/rtk-win.zip"
      extract_dir="${tmp_dir}/extract"
      mkdir -p "$extract_dir"

      curl -fsSL "$archive_url" -o "$archive_path" || {
        rm -rf "$tmp_dir"
        error "Falha ao baixar o RTK para Windows."
        return 1
      }

      extract_zip_archive "$archive_path" "$extract_dir" || {
        rm -rf "$tmp_dir"
        error "Falha ao extrair o RTK para Windows."
        error "É necessário ter 'unzip' ou 'powershell.exe' disponível no PATH."
        return 1
      }

      if [[ ! -f "${extract_dir}/rtk.exe" ]]; then
        rm -rf "$tmp_dir"
        error "Arquivo rtk.exe não encontrado após a extração."
        return 1
      fi

      cp "${extract_dir}/rtk.exe" "$WIN_BIN"
      chmod +x "$WIN_BIN"
      cat > "$BIN" <<EOF
#!/usr/bin/env bash
exec "${WIN_BIN}" "\$@"
EOF
      chmod +x "$BIN"
      rm -rf "$tmp_dir"
      success "RTK Windows instalado em $WIN_BIN"
    else
      info "Baixando RTK via install.sh oficial..."
      # O install.sh do RTK vai para ~/.local/bin — depois copiamos
      curl -fsSL \
        "https://raw.githubusercontent.com/rtk-ai/rtk/refs/heads/master/install.sh" \
        | sh

      for candidate in "$HOME/.local/bin/rtk" "/usr/local/bin/rtk"; do
        if [[ -x "$candidate" ]]; then
          cp "$candidate" "$BIN"
          success "RTK copiado para $BIN"
          break
        fi
      done
    fi
  fi

  append_env "export PATH=\"${DWYT_BIN}:\$PATH\"" "rtk"
  export PATH="${DWYT_BIN}:$PATH"

  if [[ -x "$BIN" ]]; then
    success "RTK pronto: $("$BIN" --version 2>/dev/null || echo 'ok')"
    info "Configurando hook global para Claude Code (não-interativo)..."
    # --yes evita prompts interativos; timeout garante que não trava
    run_with_timeout 15 "$BIN" init -g --yes 2>/dev/null \
      || run_with_timeout 15 "$BIN" init --global --yes 2>/dev/null \
      || run_with_timeout 15 "$BIN" init -g 2>/dev/null < /dev/null \
      || warn "rtk init -g pulado — rode manualmente: rtk init -g"
  else
    warn "RTK não encontrado em $BIN"
  fi
}

# ═════════════════════════════════════════════════════════════════════════════
# [3] Headroom — proxy de compressão de API
# ═════════════════════════════════════════════════════════════════════════════
install_headroom() {
  step "3/4" "Headroom — proxy de compressão de API"

  local WRAPPER="${DWYT_BIN}/headroom"

  if [[ -x "$WRAPPER" ]] && "$WRAPPER" --help &>/dev/null 2>&1; then
    success "Headroom já instalado"
    write_headroom_wrapper "$WRAPPER"
    patch_headroom_codex_ws
    append_env "export PATH=\"${DWYT_BIN}:\$PATH\"" "headroom"
    export PATH="${DWYT_BIN}:$PATH"
    return
  fi

  # headroom-ai exige Python >= 3.10
  local PYTHON_BIN=""
  local PY_MINOR
  PYTHON_BIN="$(resolve_system_python)"
  PY_MINOR=$("$PYTHON_BIN" -c "import sys; print(sys.version_info.minor)")
  local PY_VER
  PY_VER=$("$PYTHON_BIN" -c "import sys; print(f'{sys.version_info.major}.{sys.version_info.minor}')")

  if [[ "$PY_MINOR" -lt 10 ]]; then
    warn "Python $PY_VER detectado — headroom-ai requer >= 3.10. Procurando versão compatível..."
    local found=""
    for v in 3.13 3.12 3.11 3.10; do
      if command -v "python$v" &>/dev/null; then
        PYTHON_BIN="python$v"
        PY_VER="$v"
        found="$v"
        success "Encontrado: Python $v"
        break
      fi
    done

    if [[ -z "$found" ]]; then
      if [[ "$OS" == "macos" ]] && command -v brew &>/dev/null; then
        info "Instalando Python 3.12 via Homebrew..."
        brew install python@3.12
        PYTHON_BIN="$(brew --prefix python@3.12)/bin/python3.12"
        PY_VER="3.12"
      elif [[ "$OS" == "fedora" ]]; then
        info "Instalando Python compatível via dnf..."
        sudo dnf install -y python3.12 python3-pip 2>/dev/null \
          || sudo dnf install -y python3.11 python3-pip 2>/dev/null
        if command -v python3.12 &>/dev/null; then
          PYTHON_BIN="python3.12"
          PY_VER="3.12"
          found="$PY_VER"
        elif command -v python3.11 &>/dev/null; then
          PYTHON_BIN="python3.11"
          PY_VER="3.11"
          found="$PY_VER"
        elif "$PYTHON_BIN" -c "import sys; raise SystemExit(0 if sys.version_info >= (3,10) else 1)" 2>/dev/null; then
          PY_VER="$("$PYTHON_BIN" -c "import sys; print(f'{sys.version_info.major}.{sys.version_info.minor}')")"
          found="$PY_VER"
        fi
      elif [[ "$OS" == "debian" ]]; then
        info "Instalando Python 3.12 via apt..."
        sudo apt-get install -y python3.12 python3.12-venv 2>/dev/null \
          || sudo apt-get install -y python3.11 python3.11-venv 2>/dev/null
        command -v python3.12 &>/dev/null && PYTHON_BIN="python3.12" && PY_VER="3.12" \
          || { command -v python3.11 &>/dev/null && PYTHON_BIN="python3.11" && PY_VER="3.11"; }
      elif [[ "$OS" == "windows" ]]; then
        info "Tentando instalar Python compatível via gerenciador do Windows..."
        install_windows_dependency python3 || {
          error "Python >= 3.10 não encontrado."
          error "Instale Python 3.10+ e abra um novo terminal Git Bash."
          return 1
        }
        hash -r
        for candidate in python3.13 python3.12 python3.11 python3.10 python3 python py py.exe; do
          if command -v "$candidate" &>/dev/null; then
            local resolved
            resolved="$(command -v "$candidate")"
            if "$resolved" -c "import sys; raise SystemExit(0 if sys.version_info >= (3,10) else 1)" 2>/dev/null; then
              PYTHON_BIN="$resolved"
              PY_VER="$("$resolved" -c "import sys; print(f'{sys.version_info.major}.{sys.version_info.minor}')")"
              found="$PY_VER"
              break
            fi
          fi
        done
      else
        error "Python >= 3.10 não encontrado."
        error "No macOS instale com: brew install python@3.12"
        error "No Fedora instale com: sudo dnf install python3.12 python3-pip"
        error "No Windows, instale Python 3.10+ e abra um novo terminal Git Bash."
        error "Depois rode: ./dwyt.sh"
        return 1
      fi
    fi
  fi

  if ! "$PYTHON_BIN" -c "import sys; raise SystemExit(0 if sys.version_info >= (3,10) else 1)" 2>/dev/null; then
    error "Headroom requer Python >= 3.10."
    error "Abra um novo terminal com Python 3.10+ disponível e rode novamente: ./dwyt.sh"
    return 1
  fi

  info "Usando Python $PY_VER para o Headroom"

  # Limpa venv corrompido de tentativa anterior
  [[ -d "$HEADROOM_VENV" ]] && rm -rf "$HEADROOM_VENV"

  info "Criando virtualenv em $HEADROOM_VENV ..."
  "$PYTHON_BIN" -m venv "$HEADROOM_VENV" || {
    error "Falha ao criar venv com $PYTHON_BIN"
    return 1
  }

  info "Instalando headroom-ai[proxy]..."
  local VENV_PIP=""
  VENV_PIP="$(venv_pip_path "$HEADROOM_VENV")" || {
    error "pip não encontrado dentro do virtualenv do Headroom"
    return 1
  }
  "$VENV_PIP" install --quiet --upgrade pip
  "$VENV_PIP" install --quiet "headroom-ai[proxy]" || {
    error "Falha ao instalar headroom-ai."
    return 1
  }

  write_headroom_wrapper "$WRAPPER"
  success "Headroom instalado com Python $PY_VER — wrapper em $WRAPPER"

  patch_headroom_codex_ws

  append_env "export PATH=\"${DWYT_BIN}:\$PATH\"" "headroom"
  export PATH="${DWYT_BIN}:$PATH"
}

# ═════════════════════════════════════════════════════════════════════════════
# [4] MemStack — memória persistente entre sessões
# ═════════════════════════════════════════════════════════════════════════════
install_memstack() {
  step "4/4" "MemStack — memória persistente entre sessões"

  if [[ -d "$MEMSTACK_DIR/.git" ]]; then
    success "MemStack já existe — atualizando..."
    git -C "$MEMSTACK_DIR" pull --quiet 2>/dev/null || true
  else
    info "Clonando MemStack em $MEMSTACK_DIR ..."
    git clone --depth=1 \
      "https://github.com/cwinvestments/memstack.git" \
      "$MEMSTACK_DIR"
  fi

  # Dependências Python opcionais (busca semântica)
  local VENV_PIP=""
  if VENV_PIP="$(venv_pip_path "$HEADROOM_VENV" 2>/dev/null)"; then
    info "Instalando dependências opcionais (lancedb, sentence-transformers)..."
    "$VENV_PIP" install --quiet lancedb sentence-transformers 2>/dev/null \
      || warn "Deps opcionais não instaladas — busca semântica indisponível"
  fi

  cat > "${DWYT_BIN}/memstack" << 'EOF'
#!/usr/bin/env bash
set -euo pipefail

MEMSTACK_HOME="${HOME}/.dwyt/memstack"
MEMSTACK_DB="${MEMSTACK_HOME}/db/memstack-db.py"
HEADROOM_PORT="${HEADROOM_PORT:-8787}"
HEADROOM_HEALTH_URL="http://127.0.0.1:${HEADROOM_PORT}/health"
HEADROOM_PID_FILE="${HOME}/.dwyt/.memstack-headroom.pid"

resolve_python() {
  command -v python3 >/dev/null 2>&1 && { command -v python3; return 0; }
  command -v python >/dev/null 2>&1 && { command -v python; return 0; }
  command -v py >/dev/null 2>&1 && { command -v py; return 0; }
  echo "Python 3 não encontrado" >&2
  exit 1
}

show_help() {
  cat <<'HELP'
Uso: memstack <comando> [args]

Controle:
  memstack start                    inicia o proxy Headroom do MemStack
  memstack stop                     para o proxy Headroom iniciado pelo wrapper
  memstack help                     mostra esta ajuda

Memória:
  memstack stats
  memstack search "<query>"
  memstack get-sessions <project> --limit 5
  memstack get-insights <project>
  memstack get-context <project>
  memstack get-plan <project>
  memstack export-md <project>

Sessões salvas:
  memstack save-session <name> <project>   salva snapshot da memória do projeto
  memstack use-session [<name>]            carrega sessão salva (sem nome: lista todas)
HELP
}

is_headroom_healthy() {
  curl -fsS "$HEADROOM_HEALTH_URL" >/dev/null 2>&1
}

start_headroom() {
  if is_headroom_healthy; then
    echo "Headroom já está rodando em ${HEADROOM_HEALTH_URL}"
    exit 0
  fi

  nohup headroom proxy --port "$HEADROOM_PORT" --llmlingua-device cpu >/dev/null 2>&1 &
  echo $! > "$HEADROOM_PID_FILE"
  sleep 2

  if is_headroom_healthy; then
    echo "Headroom iniciado em ${HEADROOM_HEALTH_URL}"
  else
    echo "Falha ao iniciar o Headroom" >&2
    exit 1
  fi
}

stop_headroom() {
  local stopped=0

  if [[ -f "$HEADROOM_PID_FILE" ]]; then
    local pid
    pid="$(cat "$HEADROOM_PID_FILE" 2>/dev/null || true)"
    if [[ -n "${pid:-}" ]] && kill -0 "$pid" 2>/dev/null; then
      kill "$pid" 2>/dev/null || true
      stopped=1
    fi
    rm -f "$HEADROOM_PID_FILE"
  fi

  if [[ "$stopped" -eq 0 ]] && command -v pgrep >/dev/null 2>&1; then
    local pids
    pids="$(pgrep -f "headroom proxy --port ${HEADROOM_PORT}" || true)"
    if [[ -n "${pids:-}" ]]; then
      kill $pids 2>/dev/null || true
      stopped=1
    fi
  fi

  if is_headroom_healthy; then
    echo "Headroom ainda responde em ${HEADROOM_HEALTH_URL}" >&2
    exit 1
  fi

  if [[ "$stopped" -eq 1 ]]; then
    echo "Headroom parado"
  else
    echo "Headroom já estava parado"
  fi
}

save_session() {
  local name="${1:-}"
  local project="${2:-}"
  if [[ -z "$name" ]] || [[ -z "$project" ]]; then
    echo "Uso: memstack save-session <name> <project>" >&2
    exit 1
  fi
  local save_dir="${MEMSTACK_HOME}/saved"
  mkdir -p "$save_dir"
  local save_file="${save_dir}/${name}.md"
  "$(resolve_python)" "$MEMSTACK_DB" export-md "$project" > "$save_file"
  echo "Sessão '${name}' salva em: ${save_file}"
}

use_session() {
  local name="${1:-}"
  local save_dir="${MEMSTACK_HOME}/saved"
  if [[ -z "$name" ]]; then
    if [[ -d "$save_dir" ]] && ls "$save_dir"/*.md &>/dev/null 2>&1; then
      echo "Sessões salvas:"
      for f in "$save_dir"/*.md; do
        echo "  - $(basename "$f" .md)"
      done
    else
      echo "Nenhuma sessão salva encontrada."
    fi
    exit 0
  fi
  local save_file="${save_dir}/${name}.md"
  if [[ ! -f "$save_file" ]]; then
    echo "Sessão '${name}' não encontrada." >&2
    exit 1
  fi
  cat "$save_file"
}

case "${1:-help}" in
  start)
    shift
    start_headroom "$@"
    ;;
  stop)
    shift
    stop_headroom "$@"
    ;;
  save-session)
    shift
    save_session "$@"
    ;;
  use-session)
    shift
    use_session "$@"
    ;;
  help|-h|--help)
    show_help
    ;;
  *)
    exec "$(resolve_python)" "$MEMSTACK_DB" "$@"
    ;;
esac
EOF
  chmod +x "${DWYT_BIN}/memstack"
  append_env "export PATH=\"${DWYT_BIN}:\$PATH\"" "memstack"
  export PATH="${DWYT_BIN}:$PATH"

  success "MemStack pronto em $MEMSTACK_DIR"
  success "Comando curto disponível: memstack"
}

# ═════════════════════════════════════════════════════════════════════════════
# INTEGRAÇÃO no projeto escolhido
# (todos os arquivos de config vão para o projeto, apontando para ~/.dwyt)
# ═════════════════════════════════════════════════════════════════════════════
integrate_project() {
  [[ -z "$CHOSEN_REPO" ]] && return

  header "Integrando em: $CHOSEN_REPO"

  local claude_dir="${CHOSEN_REPO}/.claude"
  local hooks_dir="${claude_dir}/hooks"
  local rules_dir="${claude_dir}/rules"
  local settings_file="${claude_dir}/settings.json"
  local claude_memory_dir="${claude_dir}/memory"
  local mcp_file="${CHOSEN_REPO}/.mcp.json"
  local claude_md="${CHOSEN_REPO}/.claude/CLAUDE.md"
  local agents_md="${CHOSEN_REPO}/AGENTS.md"
  local codex_dir="${CHOSEN_REPO}/.codex"
  local codex_readme="${codex_dir}/README.md"
  local copilot_dir="${CHOSEN_REPO}/.github"
  local copilot_md="${copilot_dir}/copilot-instructions.md"
  local cursor_rules_dir="${CHOSEN_REPO}/.cursor/rules"
  local cursor_rule_file="${cursor_rules_dir}/dwyt.mdc"
  local kiro_steering_dir="${CHOSEN_REPO}/.kiro/steering"
  local kiro_steering_file="${kiro_steering_dir}/dwyt.md"

  [[ "$CLIENTS" == *claude*  ]] && mkdir -p "$hooks_dir" "$rules_dir" "$claude_memory_dir"
  [[ "$CLIENTS" == *codex*   ]] && mkdir -p "$codex_dir"
  [[ "$CLIENTS" == *copilot* ]] && mkdir -p "$copilot_dir"
  [[ "$CLIENTS" == *cursor*  ]] && mkdir -p "$cursor_rules_dir"
  [[ "$CLIENTS" == *kiro*    ]] && mkdir -p "$kiro_steering_dir"

  # ── .gitignore — ignora artefatos locais e diretórios gerados ─────────────
  local gitignore="${CHOSEN_REPO}/.gitignore"
  if [[ -f "$gitignore" ]]; then
    grep -qxF "# dwyt" "$gitignore" || printf '\n# dwyt\n' >> "$gitignore"
  else
    printf '# dwyt\n' > "$gitignore"
  fi

  if [[ "$CLIENTS" == *claude* ]]; then
    grep -qxF ".claude/" "$gitignore" || printf '.claude/\n' >> "$gitignore"
    success ".gitignore → diretório .claude/ marcado como local"
  fi

  if [[ "$CLIENTS" == *codex* ]]; then
    grep -qxF ".codex" "$gitignore" || printf '.codex\n' >> "$gitignore"
    grep -qxF "AGENTS.md" "$gitignore" || printf 'AGENTS.md\n' >> "$gitignore"
    success ".gitignore → integração local do Codex marcada"
  fi

  if [[ "$CLIENTS" == *copilot* ]]; then
    grep -qxF ".github/copilot-instructions.md" "$gitignore" || printf '.github/copilot-instructions.md\n' >> "$gitignore"
    success ".gitignore → instrução local do Copilot marcada"
  fi

  if [[ "$CLIENTS" == *cursor* ]]; then
    grep -qxF ".cursor/" "$gitignore" || printf '.cursor/\n' >> "$gitignore"
    success ".gitignore → diretório .cursor/ marcado como local"
  fi

  if [[ "$CLIENTS" == *kiro* ]]; then
    grep -qxF ".kiro/" "$gitignore" || printf '.kiro/\n' >> "$gitignore"
    success ".gitignore → diretório .kiro/ marcado como local"
  fi

  # ── .mcp.json ──────────────────────────────────────────────────────────────
  if [[ "$TOOLS" == *cbmcp* ]]; then
    grep -qxF ".mcp.json" "$gitignore" || printf '.mcp.json\n' >> "$gitignore"
    success ".gitignore → .mcp.json marcado como local"
    cat > "$mcp_file" << 'EOF'
{
  "mcpServers": {
    "codebase-memory-mcp": {
      "type": "stdio",
      "command": "codebase-memory-mcp"
    }
  }
}
EOF
    success ".mcp.json → usa 'codebase-memory-mcp' via PATH"
  fi

  # ── RTK hook ───────────────────────────────────────────────────────────────
  if [[ "$TOOLS" == *rtk* ]] && [[ "$CLIENTS" == *claude* ]]; then
    local RTK_HOOK="${hooks_dir}/rtk-rewrite.sh"

    # Copia hook oficial se existir, senão cria um básico
    if [[ -f "${HOME}/.claude/hooks/rtk-rewrite.sh" ]]; then
      cp "${HOME}/.claude/hooks/rtk-rewrite.sh" "$RTK_HOOK"
    else
      cat > "$RTK_HOOK" << RTKHOOK
#!/usr/bin/env bash
# RTK PreToolUse hook — reescreve comandos verbose automaticamente
INPUT=\$(cat)
PYTHON_BIN=\$(command -v python3 2>/dev/null || command -v python 2>/dev/null || command -v py 2>/dev/null || true)
[[ -z "\$PYTHON_BIN" ]] && { echo "\$INPUT"; exit 0; }
TOOL=\$(echo "\$INPUT" | "\$PYTHON_BIN" -c "import sys,json; d=json.load(sys.stdin); print(d.get('tool_name',''))" 2>/dev/null || echo "")
[[ "\$TOOL" != "Bash" ]] && { echo "\$INPUT"; exit 0; }
CMD=\$(echo "\$INPUT" | "\$PYTHON_BIN" -c "import sys,json; d=json.load(sys.stdin); print(d.get('tool_input',{}).get('command',''))" 2>/dev/null || echo "")
RTK="${DWYT_BIN}/rtk"
[[ ! -x "\$RTK" ]] && { echo "\$INPUT"; exit 0; }
FIRST=\$(echo "\$CMD" | awk '{print \$1}')
for c in git cargo npm pnpm yarn docker kubectl pip python pytest ruff mypy tsc; do
  if [[ "\$FIRST" == "\$c" ]]; then
    NEW="\$RTK \$CMD"
    echo "\$INPUT" | "\$PYTHON_BIN" -c "
import sys,json
d=json.load(sys.stdin)
d['tool_input']['command']='\$NEW'
print(json.dumps(d))"
    exit 0
  fi
done
echo "\$INPUT"
RTKHOOK
    fi
    chmod +x "$RTK_HOOK"

    # settings.json — merge ou criação
    if [[ -f "$settings_file" ]]; then
      "$(resolve_system_python)" - "$settings_file" "$RTK_HOOK" << 'PYMERGE'
import sys, json
f, hook = sys.argv[1], sys.argv[2]
with open(f) as fp: data = json.load(fp)
pre = data.setdefault("hooks", {}).setdefault("PreToolUse", [])
if not any("rtk" in str(h) for h in pre):
    pre.append({"matcher": "Bash", "hooks": [{"type": "command", "command": hook}]})
data.setdefault("permissions", {}).setdefault("allow", [])
if "Bash(rtk:*)" not in data["permissions"]["allow"]:
    data["permissions"]["allow"].append("Bash(rtk:*)")
with open(f, "w") as fp: json.dump(data, fp, indent=2)
PYMERGE
    else
      cat > "$settings_file" << EOF
{
  "hooks": {
    "PreToolUse": [
      { "matcher": "Bash", "hooks": [{ "type": "command", "command": "${RTK_HOOK}" }] }
    ]
  },
  "permissions": { "allow": ["Bash(rtk:*)"] }
}
EOF
    fi
    success "RTK hook → $RTK_HOOK"
  fi

  # ── MemStack: rules + skills symlink ──────────────────────────────────────
  if [[ "$TOOLS" == *memstack* ]] && [[ "$CLIENTS" == *claude* ]] && [[ -d "$MEMSTACK_DIR" ]]; then
    for f in "$MEMSTACK_DIR"/.claude/rules/*.md; do
      if [[ -f "$f" ]]; then
        local dest="${rules_dir}/$(basename "$f")"
        cp "$f" "$dest"
        "$(resolve_system_python)" - "$dest" << 'PYRULES'
import sys
from pathlib import Path

path = Path(sys.argv[1])
text = path.read_text()
replacements = [
    ("python C:/Projects/memstack", "memstack"),
    ("python C:\\Projects\\memstack", "memstack"),
    ("C:/Projects/memstack/memory/sessions/", "~/.dwyt/memstack/memory/sessions/"),
    ("C:\\Projects\\memstack\\memory\\sessions\\", "~/.dwyt/memstack/memory/sessions/"),
    ("C:/Projects/memstack", "~/.dwyt/memstack"),
    ("C:\\Projects\\memstack", "~/.dwyt/memstack"),
]
for old, new in replacements:
    text = text.replace(old, new)
path.write_text(text)
PYRULES
        success "Rule: $(basename "$f")"
      fi
    done
    local skills_link="${claude_dir}/skills"
    if [[ ! -e "$skills_link" ]] && [[ ! -L "$skills_link" ]]; then
      link_or_copy_dir "${MEMSTACK_DIR}/skills" "$skills_link" "Skills MemStack"
    fi
  fi

  # ── Instruções universais para LLMs ───────────────────────────────────────
  local universal_sections=""
  local claude_sections=""

  if [[ "$TOOLS" == *cbmcp* ]]; then
    universal_sections+="
### codebase-memory-mcp — Grafo do código
Se o MCP do codebase-memory-mcp estiver conectado e respondendo, prefira o grafo antes de explorar arquivos manualmente.
Se o MCP não estiver disponível, faça fallback para busca manual sem bloquear o trabalho.
- **Indexar projeto**: chame \`index_repository\` com o caminho do repositório
- **Quem chama função X?**: \`trace_call_path(function_name=\"X\", direction=\"inbound\")\`
- **O que X chama?**: \`trace_call_path(function_name=\"X\", direction=\"outbound\")\`
- **Buscar por nome**: \`search_graph(label=\"Function\", name_pattern=\".*Padrão.*\")\`
- **Código sem uso**: \`search_graph(label=\"Function\", relationship=\"CALLS\", direction=\"inbound\", max_degree=0, exclude_entry_points=true)\`
- **Rotas REST**: \`search_graph(label=\"Route\")\`
- **Chamadas HTTP entre serviços**: \`search_graph(relationship=\"HTTP_CALLS\")\`
- **Query customizada**: \`query_graph(query=\"MATCH (f:Function)-[:CALLS]->(g) RETURN g.name LIMIT 20\")\`
- **Ler código fonte**: \`get_code_snippet(qualified_name=\"pacote.Função\")\`
"
    claude_sections+="$universal_sections"
  fi

  if [[ "$TOOLS" == *rtk* ]]; then
    universal_sections+="
### RTK — Compressão de output de terminal
Se o comando \`rtk\` existir e estiver funcionando, use \`rtk <comando>\` quando fizer sentido.
Se não estiver disponível, execute o comando normal sem bloquear o fluxo.
Para ver quanto foi economizado: \`rtk gain\`
Para ver oportunidades de economia: \`rtk discover\`
"
    if [[ "$CLIENTS" == *claude* ]]; then
      claude_sections+="
### RTK — Compressão de output de terminal
Se o hook do RTK estiver instalado e funcionando, o Claude Code reescreve comandos Bash suportados automaticamente.
Se o hook não estiver disponível, siga normalmente com Bash sem exigir RTK.
Comandos comprimidos: \`git\`, \`cargo\`, \`npm\`, \`pnpm\`, \`docker\`, \`kubectl\`, \`pip\`, \`pytest\`
Para ver quanto foi economizado: \`rtk gain\`
Para ver oportunidades de economia: \`rtk discover\`
"
    fi
  fi

  if [[ "$TOOLS" == *headroom* ]]; then
    universal_sections+="
### Headroom — Compressão de chamadas à API
Se a sessão atual tiver sido iniciada com wrapper do Headroom, use Headroom.
Se não tiver wrapper ativo ou o proxy não estiver rodando, não use Headroom e siga com a API normal.
Use \`dwyt-codex\` para abrir o Codex com Headroom sem depender de \`OPENAI_BASE_URL\`.
- Compatibilidade adicional depende do cliente aceitar proxy/base URL custom
- Iniciar proxy: \`headroom proxy --port 8787\`
- Iniciar proxy + Codex: \`dwyt-codex\`
- Ver economia em tempo real: \`curl http://localhost:8787/stats\`
"
    if [[ "$CLIENTS" == *claude* ]]; then
      claude_sections+="
### Headroom — Compressão de chamadas à API
No Claude Code, use Headroom apenas quando a sessão for aberta com \`headroom wrap claude\`.
Não configure \`ANTHROPIC_BASE_URL\` fixo no projeto.
- Se abriu com wrapper, use o proxy
- Se não abriu com wrapper, use a API normal
- Iniciar proxy: \`headroom proxy --port 8787\`
- Iniciar proxy + Claude Code: \`headroom wrap claude\`
- Ver economia em tempo real: \`curl http://localhost:8787/stats\`
- Salvar aprendizados no CLAUDE.md: \`headroom learn --apply\`
"
    fi
  fi

  if [[ "$TOOLS" == *memstack* ]]; then
    universal_sections+="
### MemStack — Memória persistente entre sessões
Se o MemStack estiver instalado e disponível no cliente atual, use-o.
Se não estiver disponível, continue sem memória persistente.
Integração automática disponível hoje apenas no Claude Code.
Comandos de ajuda no terminal:
- memstack help
- memstack start
- memstack stop
- memstack stats
- memstack search \"<query>\"
- memstack get-sessions <project> --limit 5
- memstack get-insights <project>
- memstack get-context <project>
- memstack get-plan <project>
- memstack export-md <project>
- memstack save-session <name> <project>
- memstack use-session [<name>]
"
    if [[ "$CLIENTS" == *claude* ]]; then
      claude_sections+="
### MemStack — Memória persistente entre sessões
Integração automática disponível no Claude Code quando a integração estiver presente.
- Se o MemStack estiver disponível, use os comandos e skills abaixo
- Se não estiver, continue normalmente
- Buscar memórias anteriores: \`/memstack-search <query>\` (no chat do LLM)
- Status do Headroom: \`/memstack-headroom\`
- Ajuda no terminal: memstack help
- Iniciar proxy no terminal: memstack start
- Parar proxy no terminal: memstack stop
- Ajuda no terminal: memstack stats
- Busca no terminal: memstack search \"<query>\"
- Sessões no terminal: memstack get-sessions <project> --limit 5
- Insights no terminal: memstack get-insights <project>
- Contexto no terminal: memstack get-context <project>
- Plano no terminal: memstack get-plan <project>
- Exportar memória: memstack export-md <project>
- Salvar sessão: memstack save-session <name> <project>
- Usar sessão salva: memstack use-session [<name>]
- Diário de sessão: skill \`Diary\` ativa automaticamente
- Planejamento de tarefas: skill \`Work\` ativa com gatilhos como \"plan\", \"task\", \"implement\"
"
    fi
  fi

  local client_list=""
  [[ "$CLIENTS" == *claude*  ]] && client_list+="- Claude Code
"
  [[ "$CLIENTS" == *codex*   ]] && client_list+="- Codex
"
  [[ "$CLIENTS" == *copilot* ]] && client_list+="- GitHub Copilot
"
  [[ "$CLIENTS" == *kiro*    ]] && client_list+="- Kiro
"
  [[ "$CLIENTS" == *cursor*  ]] && client_list+="- Cursor
"

  local universal_header="# DWYT — Don't Waste Your Tokens

Este projeto usa um stack de ferramentas para reduzir consumo de tokens.
Clientes integrados neste repositório:
${client_list}
Todas as integrações deste projeto são opcionais.
Regra geral:
- Se Headroom estiver ativo via wrapper, use Headroom; se não estiver, não use
- Se o MCP do codebase-memory-mcp estiver conectado e respondendo, use ele; se não estiver, faça fallback para busca manual
- Se RTK existir e estiver funcionando, use RTK; se não, rode os comandos normalmente
- Se MemStack estiver disponível no cliente atual, use ele; se não, siga sem memória persistente
Prefira estas integrações, quando suportadas pelo cliente:
- \`.mcp.json\` para expor ferramentas MCP, incluindo o codebase-memory-mcp
- \`AGENTS.md\` para agentes compatíveis como Codex, Cursor e Kiro
- \`.github/copilot-instructions.md\` para GitHub Copilot
- \`.cursor/rules/\` para regras de projeto do Cursor
- \`.kiro/steering/\` para steering files do Kiro
${universal_sections}"

  local claude_header="# DWYT — Don't Waste Your Tokens

Este projeto usa um stack de ferramentas para reduzir consumo de tokens.
Instruções específicas para Claude Code:
- O arquivo \`CLAUDE.md\` fica em \`.claude/CLAUDE.md\` (local, não commitado)
- Hooks e permissões ficam em \`.claude/settings.json\`
- Arquivos locais devem ir em \`.claude/settings.local.json\` e \`.claude/memory/\`
- Consulte também o \`AGENTS.md\` na raiz para instruções universais
- Regra geral: use integrações opcionais somente quando estiverem disponíveis e funcionando; caso contrário, faça fallback silencioso
${claude_sections}"

  if [[ -f "$agents_md" ]]; then
    warn "AGENTS.md existente — adicionando seção DWYT ao final"
    printf '\n---\n%s\n' "$universal_header" >> "$agents_md"
  else
    printf '%s\n' "$universal_header" > "$agents_md"
  fi
  success "AGENTS.md atualizado"

  if [[ "$CLIENTS" == *codex* ]]; then
    cat > "$codex_readme" << 'EOF'
# Codex Integration

O Codex lê instruções do arquivo `AGENTS.md` na raiz do repositório.

Esta pasta `.codex/` é apenas auxiliar para organização local do projeto DWYT.
EOF
    success ".codex/ criado como apoio para a integração do Codex"
  fi

  if [[ "$CLIENTS" == *claude* ]] && [[ -f "$claude_md" ]]; then
    warn "CLAUDE.md existente — adicionando seção DWYT ao final"
    printf '\n---\n%s\n' "$claude_header" >> "$claude_md"
  elif [[ "$CLIENTS" == *claude* ]]; then
    printf '%s\n' "$claude_header" > "$claude_md"
  fi
  [[ "$CLIENTS" == *claude* ]] && success "CLAUDE.md atualizado"

  if [[ "$CLIENTS" == *copilot* ]]; then
    cat > "$copilot_md" << EOF
# DWYT — GitHub Copilot

Siga as instruções compartilhadas do arquivo \`AGENTS.md\`.

Ao trabalhar neste repositório:
- Se o MCP descrito em \`.mcp.json\` estiver conectado e respondendo, prefira-o antes de busca manual por arquivos
- Se o MCP não estiver disponível, faça fallback para busca manual
- Se RTK existir e estiver funcionando, use output enxuto; se não, siga normalmente
- Se MemStack ou Headroom não estiverem disponíveis no cliente atual, não trate isso como erro
- Se precisar investigar por terminal, minimize output desnecessário
- Se existir \`CLAUDE.md\`, trate-o apenas como referência complementar do stack DWYT
EOF
    success "GitHub Copilot → $copilot_md"
  fi

  if [[ "$CLIENTS" == *cursor* ]]; then
    cat > "$cursor_rule_file" << 'EOF'
---
description: DWYT project guidance
alwaysApply: true
---

Siga as instruções compartilhadas em `AGENTS.md`.

Neste repositório:
- Se as ferramentas MCP configuradas em `.mcp.json` estiverem disponíveis, prefira elas antes de buscas manuais
- Se não estiverem disponíveis, faça fallback para busca manual
- Se Headroom, RTK ou MemStack não estiverem disponíveis na sessão atual, siga normalmente
- Use saída de terminal enxuta e só expanda quando necessário
- Considere `CLAUDE.md` apenas como referência específica do Claude Code
EOF
    success "Cursor rule → $cursor_rule_file"
  fi

  if [[ "$CLIENTS" == *kiro* ]]; then
    cat > "$kiro_steering_file" << 'EOF'
# DWYT Steering

Siga as instruções compartilhadas em `AGENTS.md`.

Preferências deste projeto:
- Se as ferramentas MCP configuradas em `.mcp.json` estiverem disponíveis, priorize elas
- Se não estiverem disponíveis, use exploração manual sem bloquear a tarefa
- Se Headroom, RTK ou MemStack não estiverem disponíveis na sessão atual, siga normalmente
- Use `CLAUDE.md` apenas como contexto específico do Claude Code
EOF
    success "Kiro steering → $kiro_steering_file"
  fi

  # ── Indexar projeto ────────────────────────────────────────────────────────
  if [[ "$TOOLS" == *cbmcp* ]] && [[ -x "${DWYT_BIN}/codebase-memory-mcp" ]]; then
    info "Indexando projeto..."
    printf '%s\n%s\n' \
      '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"dwyt","version":"2.0"}}}' \
      "{\"jsonrpc\":\"2.0\",\"id\":2,\"method\":\"tools/call\",\"params\":{\"name\":\"index_repository\",\"arguments\":{\"repo_path\":\"${CHOSEN_REPO}\"}}}" \
      | run_with_timeout 120 "${DWYT_BIN}/codebase-memory-mcp" 2>&1 | grep -v "^$" | tail -3 || true
    success "Indexação concluída"
  fi
}

# ─── Finaliza env.sh e recarrega shell ───────────────────────────────────────
finalize_env() {
  # Garante PATH no env.sh (linha única, sem duplicatas)
  local path_line="export PATH=\"${DWYT_BIN}:\$PATH\""
  if ! grep -qF "$DWYT_BIN" "$DWYT_ENV_FILE" 2>/dev/null; then
    append_env "$path_line" "dwyt bin dir"
  fi

  # Headroom: exemplo opcional de ativação global via ambiente
  if [[ "$TOOLS" == *headroom* ]]; then
    append_env "# Descomente para ativar Headroom globalmente em todos os LLMs:" ""
    append_env "# export ANTHROPIC_BASE_URL=http://localhost:8787" ""
  fi

  if [[ "$TOOLS" == *headroom* ]] && [[ "$CLIENTS" == *codex* ]]; then
    cat > "${DWYT_BIN}/dwyt-codex" << 'CODEXWRAPPER'
#!/usr/bin/env bash
set -euo pipefail

HEADROOM_PORT="${HEADROOM_PORT:-8787}"
HEADROOM_URL="http://127.0.0.1:${HEADROOM_PORT}"
CODEX_BASE_URL="${HEADROOM_URL}/v1"
HEADROOM_PID_FILE="${HOME}/.dwyt/.codex-headroom.pid"

is_headroom_healthy() {
  curl -fsS "${HEADROOM_URL}/health" >/dev/null 2>&1
}

start_headroom() {
  if is_headroom_healthy; then
    return 0
  fi

  nohup headroom proxy --port "${HEADROOM_PORT}" >/dev/null 2>&1 &
  echo $! > "${HEADROOM_PID_FILE}"

  for _ in {1..20}; do
    if is_headroom_healthy; then
      return 0
    fi
    sleep 1
  done

  echo "Falha ao iniciar o Headroom em ${HEADROOM_URL}" >&2
  exit 1
}

start_headroom
exec codex -c "openai_base_url=\"${CODEX_BASE_URL}\"" "$@"
CODEXWRAPPER
    chmod +x "${DWYT_BIN}/dwyt-codex"
    success "Launcher do Codex com Headroom criado → use: dwyt-codex"
  fi

  # Cria wrapper dwyt-ui para iniciar/parar a UI facilmente
  cat > "${DWYT_BIN}/dwyt-ui" << 'UIWRAPPER'
#!/usr/bin/env bash
DWYT_HOME="${HOME}/.dwyt"
DWYT_BIN="${DWYT_HOME}/bin"
UI_PORT=9749
UI_PID_FILE="${DWYT_HOME}/.ui.pid"

stop_ui() {
  if [[ -f "$UI_PID_FILE" ]]; then
    kill "$(cat $UI_PID_FILE)" 2>/dev/null && echo "UI parada." || echo "Processo já encerrado."
    rm -f "$UI_PID_FILE"
  else
    echo "UI não está rodando."
  fi
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
      else
        "$BIN" --port "$UI_PORT" &>/dev/null &
      fi
      echo $! > "$UI_PID_FILE"
      sleep 2
      if kill -0 "$(cat $UI_PID_FILE)" 2>/dev/null; then
        echo "✓ UI rodando: http://localhost:${UI_PORT}  (PID $(cat $UI_PID_FILE))"
      else
        rm -f "$UI_PID_FILE"
        echo "✗ UI não iniciou com $BIN — tentando próximo..."
        continue
      fi
      return 0
    fi
  done
  echo "Erro: nenhum binário funcionou. Verifique: ls ${DWYT_BIN}"
  exit 1
}

case "${1:-start}" in
  stop)  stop_ui ;;
  start) start_ui ;;
  *)     start_ui ;;
esac
UIWRAPPER
  chmod +x "${DWYT_BIN}/dwyt-ui"
  success "Comando dwyt-ui criado → use: dwyt-ui / dwyt-ui stop"

  success "~/.dwyt/env.sh atualizado"

  # Aplica PATH e exports na sessão atual sem dar source no rc inteiro
  # (source do rc inteiro pode encerrar o script em alguns shells)
  export PATH="${DWYT_BIN}:${PATH}"
  export XDG_CACHE_HOME="${DWYT_DATA}"
}

# ═════════════════════════════════════════════════════════════════════════════
# RESUMO FINAL
# ═════════════════════════════════════════════════════════════════════════════
show_summary() {
  local clients_display="${CLIENTS//\"/}"

  clear
  echo -e "${BOLD}${GREEN}"
  echo "╔═══════════════════════════════════════════════════════════════════╗"
  echo "║   ✓  DWYT — Don't Waste Your Tokens — Instalação Concluída!      ║"
  echo "╚═══════════════════════════════════════════════════════════════════╝"
  echo -e "${NC}"

  [[ -n "$CHOSEN_REPO" ]] && \
    echo -e "  ${CYAN}Projeto integrado:${NC} ${BOLD}$CHOSEN_REPO${NC}\n"

  echo -e "  ${CYAN}Clientes integrados:${NC} ${BOLD}${clients_display}${NC}\n"

  echo -e "${BOLD}${YELLOW}  COMO USAR — com suporte universal + integrações específicas${NC}\n"

  if [[ "$TOOLS" == *headroom* ]]; then
    echo -e "${BOLD}  PASSO 1 — Antes de abrir clientes compatíveis com Headroom:${NC}"
    echo -e "  ${CYAN}headroom proxy --port 8787${NC}    → inicia o proxy de compressão"
    [[ "$CLIENTS" == *claude* ]] && echo -e "  ${CYAN}headroom wrap claude${NC}          → proxy + Claude Code (atalho)"
    [[ "$CLIENTS" == *codex*  ]] && echo -e "  ${CYAN}dwyt-codex${NC}                    → proxy + Codex sem OPENAI_BASE_URL"
    [[ "$CLIENTS" == *cursor* ]] && echo -e "  ${CYAN}headroom wrap cursor${NC}          → proxy + Cursor (atalho oficial)"
    echo -e "  ${YELLOW}Copilot e Kiro só aproveitam isso se o cliente permitir customizar proxy/base URL.${NC}"
    echo ""
  fi

  if [[ "$TOOLS" == *cbmcp* ]]; then
    echo -e "${BOLD}  PASSO 2 — No chat do LLM, valide se o MCP está conectado e use os 3 comandos principais:${NC}"
    echo -e "  ${CYAN}/mcp${NC}                           → valida se o servidor MCP está conectado no cliente"
    echo -e "  ${CYAN}\"Index this project\"${NC}          → dispara a tool index_repository"
    echo -e "  ${CYAN}./dwyt.sh --repo /caminho/do/repo${NC} → integra e indexa um novo repositório sem reinstalar"
    echo -e "  ${CYAN}\"Quem chama a função X?\"${NC}      → usa trace_call_path para rastrear chamadores"
    echo -e "  ${CYAN}\"O que a função X chama?\"${NC}     → usa trace_call_path para rastrear dependências"
    echo -e "  ${CYAN}AGENTS.md${NC}                      → instruções universais para Codex, Cursor e Kiro"
    echo ""
    echo -e "${BOLD}  UI Visual do grafo (navegador):${NC}"
    if [[ -n "${DWYT_UI_URL:-}" ]]; then
      echo -e "  ${GREEN}✓ UI rodando${NC} → ${BOLD}${CYAN}${DWYT_UI_URL}${NC}"
      echo -e "  Gerenciar depois: ${CYAN}dwyt-ui${NC} / ${CYAN}dwyt-ui stop${NC}"
    else
      echo -e "  ${YELLOW}UI não disponível ou não iniciou${NC}"
    fi
    echo ""
  fi

  if [[ "$TOOLS" == *rtk* ]]; then
    echo -e "${BOLD}  RTK — automático no Claude Code, manual nos demais clientes:${NC}"
    echo -e "  ${CYAN}rtk gain${NC}                      → tokens economizados total"
    echo -e "  ${CYAN}rtk discover${NC}                  → oportunidades ainda não capturadas"
    echo -e "  ${CYAN}rtk git status${NC}                → uso manual com qualquer comando"
    echo ""
  fi

  if [[ "$TOOLS" == *memstack* ]] && [[ "$CLIENTS" == *claude* ]]; then
    echo -e "${BOLD}  MemStack — automático no Claude Code:${NC}"
    echo -e "  ${CYAN}/memstack-search <termo>${NC}  busca memórias no chat"
    echo -e "  ${CYAN}/memstack-headroom${NC}        status do proxy"
    echo -e "  ${CYAN}memstack help${NC}              lista comandos"
    echo -e "  ${CYAN}memstack start${NC}             inicia o proxy"
    echo -e "  ${CYAN}memstack stop${NC}              para o proxy"
    echo -e "  ${CYAN}memstack stats${NC}             estatísticas do banco"
    echo -e "  ${CYAN}memstack search \"<termo>\"${NC}  busca direta no banco"
    echo -e "  ${CYAN}memstack get-sessions <projeto> --limit 5${NC}"
    echo -e "                               últimas sessões"
    echo -e "  ${CYAN}memstack get-context <projeto>${NC}  contexto salvo"
    echo -e "  ${CYAN}memstack save-session <nome> <projeto>${NC}  salva snapshot"
    echo -e "  ${CYAN}memstack use-session [<nome>]${NC}    carrega sessão salva"
    echo ""
  fi

  if [[ "$TOOLS" == *headroom* ]]; then
    echo -e "${BOLD}  Ao final de cada sessão:${NC}"
    [[ "$CLIENTS" == *claude* ]] && echo -e "  ${CYAN}headroom learn --apply${NC}        → salva aprendizados no CLAUDE.md"
    echo -e "  ${CYAN}curl localhost:8787/stats${NC}     → relatório de compressão da sessão"
    echo ""
  fi

  echo -e "${BOLD}${BLUE}  LOCALIZAÇÃO DOS ARQUIVOS:${NC}"
  echo -e "  ${CYAN}~/.dwyt/${NC}                       → tudo instalado aqui"
  echo -e "  ${CYAN}~/.dwyt/bin/${NC}                   → binários (no PATH)"
  echo -e "  ${CYAN}~/.dwyt/env.sh${NC}                 → variáveis (carregado pelo shell)"
  echo -e "  ${CYAN}~/.dwyt/memstack/${NC}              → MemStack"
  echo -e "  ${CYAN}~/.dwyt/headroom-venv/${NC}         → Python virtualenv do Headroom"
  echo -e "  ${CYAN}~/.dwyt/data/${NC}                  → banco SQLite do grafo (codebase-memory-mcp)"
  echo ""
  echo -e "${BOLD}  Para reinstalar tudo do zero:${NC}"
  echo -e "  ${CYAN}./dwyt.sh --reinstall${NC}"
  echo ""
  if [[ -n "$SHELL_LOGIN_RC" ]]; then
    echo -e "  ${BOLD}${YELLOW}Recarregue o shell agora:${NC}  ${CYAN}source ${SHELL_RC}${NC}  ${NC}ou${CYAN} source ${SHELL_LOGIN_RC}${NC}"
  else
    echo -e "  ${BOLD}${YELLOW}Recarregue o shell agora:${NC}  ${CYAN}source ${SHELL_RC}${NC}"
  fi
  echo -e "${BOLD}${GREEN}
  🚀 Bom uso — Don't Waste Your Tokens!${NC}\n"
}

# ═════════════════════════════════════════════════════════════════════════════
# Inicia UI do codebase-memory-mcp em background
# ═════════════════════════════════════════════════════════════════════════════
start_ui() {
  [[ "$TOOLS" != *cbmcp* ]] && return

  local UI_PORT=9749
  local UI_PID_FILE="${DWYT_HOME}/.ui.pid"
  DWYT_UI_URL=""

  # Mata instância anterior se existir
  if [[ -f "$UI_PID_FILE" ]]; then
    kill "$(cat "$UI_PID_FILE")" 2>/dev/null || true
    rm -f "$UI_PID_FILE"
  fi

  # Tenta os dois binários em ordem
  for BIN in "${DWYT_BIN}/codebase-memory-mcp-ui" "${DWYT_BIN}/codebase-memory-mcp"; do
    [[ ! -x "$BIN" ]] && continue

    info "Subindo UI do codebase-memory-mcp na porta $UI_PORT..."

    # Ativa UI HTTP (flag persistida) e inicia o servidor
    if "$BIN" --help 2>&1 | grep -q "\-\-ui="; then
      "$BIN" --ui=true --port="$UI_PORT" &>/dev/null &
    elif "$BIN" --help 2>&1 | grep -qw "serve"; then
      "$BIN" serve --port "$UI_PORT" &>/dev/null &
    else
      "$BIN" --port "$UI_PORT" &>/dev/null &
    fi
    local PID=$!
    echo "$PID" > "$UI_PID_FILE"

    sleep 2

    if kill -0 "$PID" 2>/dev/null; then
      DWYT_UI_URL="http://localhost:${UI_PORT}"
      success "UI rodando em $DWYT_UI_URL (PID $PID)"
      return 0
    else
      rm -f "$UI_PID_FILE"
      warn "Binário $BIN não iniciou a UI — tentando próximo..."
    fi
  done

  warn "UI não disponível nesta versão do codebase-memory-mcp"
}

# ═════════════════════════════════════════════════════════════════════════════
# MAIN
# ═════════════════════════════════════════════════════════════════════════════
main() {
  handle_args "${@:-}"

  if [[ "$DWYT_MODE" == "repo" ]]; then
    quick_integrate_repo "$DIRECT_REPO_PATH"
  fi

  clear
  echo -e "${BOLD}${BLUE}"
  echo "  ╔══════════════════════════════════════════════════════════╗"
  echo "  ║   🚀  DWYT — Don't Waste Your Tokens  v2.0              ║"
  echo "  ║   codebase-memory-mcp + RTK + Headroom + MemStack       ║"
  echo "  ║   Linux + macOS + Windows (Git Bash)                   ║"
  echo "  ║                                                          ║"
  echo "  ║   Uso: ./dwyt.sh [--repo path|--reinstall|--uninstall] ║"
  echo "  ╚══════════════════════════════════════════════════════════╝"
  echo -e "${NC}"

  detect_env
  info "Sistema: ${BOLD}$OS${NC}  |  Shell RC: ${BOLD}$SHELL_RC${NC}"
  sleep 1

  check_deps
  init_env_file
  select_tools
  select_clients
  select_repo

  [[ "$TOOLS" == *cbmcp*    ]] && install_cbmcp
  [[ "$TOOLS" == *rtk*      ]] && install_rtk
  [[ "$TOOLS" == *headroom* ]] && install_headroom
  [[ "$TOOLS" == *memstack* ]] && install_memstack

  integrate_project
  finalize_env
  configure_codex_cli
  start_ui        # sobe UI do codebase-memory-mcp em background
  show_summary
}

main "${@:-}"
