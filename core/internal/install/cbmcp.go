package install

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

const cbmcpReleaseBase = "https://github.com/DeusData/codebase-memory-mcp/releases/latest/download"

// CBMCP installs the codebase-memory-mcp UI binary into dwytBin by downloading
// the prebuilt release archive directly in Go (no bash/curl/tar needed), with
// SHA-256 checksum verification. This works natively on Linux, macOS, and
// Windows. The UI variant exposes the visualization HTTP server on :9749; the
// standard build is stdio-only. DWYT manages agent config itself, so the
// upstream "install -y" step is intentionally not run.
func CBMCP(dwytBin string) error {
	binName := "codebase-memory-mcp"
	if runtime.GOOS == "windows" {
		binName += ".exe"
	}
	binPath := filepath.Join(dwytBin, binName)
	os.MkdirAll(dwytBin, 0755)

	archive, isZip := cbmcpArchiveName()
	rb := releaseBinary{
		archiveURL:  cbmcpReleaseBase + "/" + archive,
		checksumURL: cbmcpReleaseBase + "/checksums.txt",
		archiveName: archive,
		destPath:    binPath,
		innerNames:  []string{"codebase-memory-mcp", "codebase-memory-mcp.exe"},
		isZip:       isZip,
	}
	if err := installReleaseBinary(rb); err != nil {
		return fmt.Errorf("cbmcp: %w", err)
	}

	// Verify the installed binary runs.
	if out, err := exec.Command(binPath, "--version").CombinedOutput(); err != nil {
		return fmt.Errorf("cbmcp: binário instalado não executa: %w\n%s", err, string(out))
	}
	return nil
}

// cbmcpArchiveName builds the release archive name for the current platform,
// mirroring the upstream install.sh naming scheme. Returns the name and whether
// it is a zip (Windows) vs tar.gz.
func cbmcpArchiveName() (string, bool) {
	goos := runtime.GOOS // linux | darwin | windows
	arch := runtime.GOARCH
	if arch != "arm64" {
		arch = "amd64"
	}
	ext := "tar.gz"
	isZip := false
	if goos == "windows" {
		ext = "zip"
		isZip = true
	}
	// Linux ships a fully-static "-portable" build for old-glibc compatibility.
	portable := ""
	if goos == "linux" {
		portable = "-portable"
	}
	return fmt.Sprintf("codebase-memory-mcp-ui-%s-%s%s.%s", goos, arch, portable, ext), isZip
}
