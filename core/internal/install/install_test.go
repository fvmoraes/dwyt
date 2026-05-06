package install

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestObsidianMCPCreatesAliasFromCurrentExecutable(t *testing.T) {
	dwytBin := t.TempDir()
	if err := ObsidianMCP(dwytBin); err != nil {
		t.Fatal(err)
	}

	name := "dwyt-obsidian-mcp"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	info, err := os.Stat(filepath.Join(dwytBin, name))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&0111 == 0 {
		t.Fatalf("expected executable mode, got %s", info.Mode())
	}
}
