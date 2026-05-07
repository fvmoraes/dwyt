package install

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// ObsidianMCP coloca o binário dwyt-obsidian-mcp em dwytBin. O MCP é
// embutido no próprio binário do DWYT (mesmo executável, comando
// alternativo) — então copiamos o executável atual ou um sibling com nome
// canônico, ao invés de baixar.
func ObsidianMCP(dwytBin string) error {
	binName := obsidianMCPBinaryName()
	binPath := filepath.Join(dwytBin, binName)
	os.MkdirAll(dwytBin, 0755)

	candidates, err := obsidianMCPSourceCandidates(binName)
	if err != nil {
		return fmt.Errorf("obsidian-mcp: %w", err)
	}

	for _, src := range candidates {
		if src == "" {
			continue
		}
		if sameFile(src, binPath) {
			return nil
		}
		if _, err := os.Stat(src); err != nil {
			continue
		}
		if err := copyExecutable(src, binPath); err != nil {
			return fmt.Errorf("obsidian-mcp: copy %s to %s: %w", src, binPath, err)
		}
		return nil
	}
	return fmt.Errorf("dwyt-obsidian-mcp source binary not found")
}

func obsidianMCPBinaryName() string {
	if runtime.GOOS == "windows" {
		return "dwyt-obsidian-mcp.exe"
	}
	return "dwyt-obsidian-mcp"
}

// obsidianMCPSourceCandidates lista os paths onde o binário pode ser
// encontrado: um sibling com nome canônico, o próprio executável atual,
// e os equivalentes após resolver symlinks.
func obsidianMCPSourceCandidates(binName string) ([]string, error) {
	exe, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("cannot locate DWYT binary: %w", err)
	}
	candidates := []string{
		filepath.Join(filepath.Dir(exe), binName),
		exe,
	}
	if realExe, err := filepath.EvalSymlinks(exe); err == nil {
		candidates = append(
			[]string{filepath.Join(filepath.Dir(realExe), binName), realExe},
			candidates...,
		)
	}
	return candidates, nil
}
