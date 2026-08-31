package install

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// ObsidianMCP garante que o binário principal do DWYT está disponível em
// dwytBin para servir o MCP do Obsidian via o subcomando `obsidian-mcp`.
// Como o servidor é embutido no próprio binário do DWYT (mesmo executável,
// comando alternativo), nenhuma cópia com nome alternativo é necessária —
// basta o DWYT canônico existir em dwytBin.
//
// A função é idempotente e auto-curável: se `dwytBin/dwyt` ainda não existe
// (ex.: primeiro `dwyt install` num build rodando de fora do bin dir, antes
// de qualquer `dwyt .` — que é o fluxo que cria o symlink via env.Init), o
// próprio executável em execução é copiado para lá. Isso preserva o
// comportamento de auto-instalação que a cópia legada proporcionava, sem o
// nome alternativo e sem o modo de falha do Windows (o destino do rename
// era o arquivo novo `dwyt-obsidian-mcp.exe`, que não existe mais).
//
// Isso resolve o bug original do Windows no qual o instalador falhava ao
// copiar `%APPDATA%\dwyt\bin\dwyt.exe` para
// `%APPDATA%\dwyt\bin\dwyt-obsidian-mcp.exe` (arquivo bloqueado, antivírus,
// permissão, etc.) — o MCP do Obsidian passa a funcionar invocando
// `dwyt.exe obsidian-mcp`.
//
// Por compatibilidade, qualquer cópia legada `dwyt-obsidian-mcp` ainda
// presente em dwytBin é removida (best-effort) assim que o binário
// principal é confirmado. Falhas ao remover o legado não são fatais: ele
// coexiste com o canônico e o registry o ignora.
func ObsidianMCP(dwytBin string) error {
	binName := "dwyt"
	if runtime.GOOS == "windows" {
		binName += ".exe"
	}
	dwytPath := filepath.Join(dwytBin, binName)
	if _, err := os.Stat(dwytPath); err != nil {
		// Self-heal: seed the canonical binary location from the currently
		// running executable. Only makes sense when the running binary IS
		// a dwyt binary (it always is — this code only runs inside dwyt),
		// and only when the canonical path is genuinely absent (a missing
		// file, not a locked one — we never overwrite).
		exe, exeErr := os.Executable()
		if exeErr != nil {
			return fmt.Errorf("obsidian-mcp: dwyt binary not found at %s and cannot locate the running binary: %w", dwytPath, exeErr)
		}
		if sameFile(exe, dwytPath) {
			return fmt.Errorf("obsidian-mcp: dwyt binary not found at %s", dwytPath)
		}
		if err := copyExecutable(exe, dwytPath); err != nil {
			return fmt.Errorf("obsidian-mcp: dwyt binary not found at %s and self-install failed: %w", dwytPath, err)
		}
	}
	removeLegacyObsidianMPCopy(dwytBin)
	return nil
}

// removeLegacyObsidianMPCopy best-effort removes an obsolete
// `dwyt-obsidian-mcp` binary (or `.exe`) sitting next to the real DWYT
// binary. It must never block setup: errors are swallowed, the file may be
// in use by a running process, owned by another user, locked by an AV, etc.
// Leaving it on disk is harmless — the registry no longer writes it into
// client configs and ignores its presence.
func removeLegacyObsidianMPCopy(dwytBin string) {
	legacy := "dwyt-obsidian-mcp"
	if runtime.GOOS == "windows" {
		legacy += ".exe"
	}
	os.Remove(filepath.Join(dwytBin, legacy))
}
