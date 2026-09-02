// Package toolsource models who owns each Hub tool installation. Keeping this
// separate from installers is deliberate: an external/local executable can be
// used by DWYT, but it must never become an installer target.
package toolsource

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/fvmoraes/dwyt/internal/brain"
	"github.com/fvmoraes/dwyt/internal/platform"
)

const (
	ToolCodebase = "cbmcp"
	ToolObsidian = "obsidian"
	ToolHeadroom = "headroom"
	ToolRTK      = "rtk"

	ModeDWYT     = "dwyt"
	ModeExternal = "external"
)

// Selection is persisted with setup and runtime state. Path is always an
// absolute, validated executable path when Mode is external and setup has
// been saved successfully.
type Selection struct {
	Mode string `json:"mode"`
	Path string `json:"path,omitempty"`
}

// Tools returns the canonical Hub tool IDs used by the API and setup wizard.
func Tools() []string {
	return []string{ToolCodebase, ToolObsidian, ToolHeadroom, ToolRTK}
}

func IsKnown(tool string) bool {
	switch tool {
	case ToolCodebase, ToolObsidian, ToolHeadroom, ToolRTK:
		return true
	default:
		return false
	}
}

// Normalize converts legacy/empty selections to DWYT-managed mode. It never
// attempts discovery or touches the filesystem, so it is safe while loading
// old configuration records.
func Normalize(selection Selection) Selection {
	selection.Mode = strings.ToLower(strings.TrimSpace(selection.Mode))
	selection.Path = strings.TrimSpace(selection.Path)
	if selection.Mode == "" {
		selection.Mode = ModeDWYT
	}
	if selection.Mode != ModeExternal {
		selection.Mode = ModeDWYT
		selection.Path = ""
	}
	return selection
}

// NormalizeAll guarantees a canonical entry for every Hub tool while dropping
// unknown keys. Old setup records had no source information, which naturally
// maps to the original DWYT-managed behavior.
func NormalizeAll(selections map[string]Selection) map[string]Selection {
	result := make(map[string]Selection, len(Tools()))
	for _, tool := range Tools() {
		result[tool] = Normalize(selections[tool])
	}
	return result
}

func IsExternal(selection Selection) bool {
	return Normalize(selection).Mode == ModeExternal
}

// ManagedPath deliberately returns only the DWYT-owned launcher. It must not
// consult PATH: choosing a local executable is an explicit external-mode
// action, not an implicit preference that can change after an update.
func ManagedPath(dwytBin, tool string) string {
	switch tool {
	case ToolCodebase:
		return platform.DWYTLauncherPath(dwytBin, "codebase-memory-mcp")
	case ToolHeadroom:
		return platform.DWYTLauncherPath(dwytBin, "headroom")
	case ToolRTK:
		return platform.DWYTLauncherPath(dwytBin, "rtk")
	default:
		return ""
	}
}

// Resolve returns the executable selected for a tool. External mode resolves
// an explicit path, or discovers the command on PATH when its path is empty.
// It never falls back to a DWYT-managed executable after external resolution
// fails; doing so would violate the user's ownership selection.
func Resolve(dwytBin, tool string, selection Selection) (string, error) {
	if !IsKnown(tool) {
		return "", fmt.Errorf("unknown tool %q", tool)
	}
	selection = Normalize(selection)
	if selection.Mode == ModeExternal {
		return Detect(tool, selection.Path)
	}
	path := ManagedPath(dwytBin, tool)
	if path == "" {
		return "", nil // Obsidian's MCP is embedded in DWYT, not a launcher.
	}
	return path, nil
}

// Detect validates an explicit local executable path or locates the tool on
// PATH. For Obsidian this refers to the desktop app; the DWYT Obsidian MCP
// remains embedded in the main DWYT binary regardless of this selection.
func Detect(tool, requestedPath string) (string, error) {
	if !IsKnown(tool) {
		return "", fmt.Errorf("unknown tool %q", tool)
	}
	requestedPath = strings.TrimSpace(requestedPath)
	if requestedPath == "" {
		return detectDefault(tool)
	}

	path := requestedPath
	if isCommandName(path) {
		resolved, err := exec.LookPath(path)
		if err != nil {
			return "", fmt.Errorf("%s was not found on PATH", tool)
		}
		path = resolved
	}
	return validateExecutable(tool, path)
}

func detectDefault(tool string) (string, error) {
	if tool == ToolObsidian {
		if path, ok := brain.FindObsidianBinary(); ok {
			return validateExecutable(tool, path)
		}
		return "", fmt.Errorf("obsidian desktop app was not found; choose its local executable")
	}

	command := map[string]string{
		ToolCodebase: "codebase-memory-mcp",
		ToolHeadroom: "headroom",
		ToolRTK:      "rtk",
	}[tool]
	path, err := exec.LookPath(command)
	if err != nil {
		return "", fmt.Errorf("%s was not found on PATH; choose its local executable", command)
	}
	return validateExecutable(tool, path)
}

func isCommandName(path string) bool {
	return !filepath.IsAbs(path) &&
		!strings.ContainsRune(path, filepath.Separator) &&
		!strings.Contains(path, "/") &&
		!strings.Contains(path, `\`)
}

func validateExecutable(tool, path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve %s path: %w", tool, err)
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		abs = resolved
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", fmt.Errorf("%s path %q is unavailable: %w", tool, abs, err)
	}
	if info.IsDir() {
		return "", fmt.Errorf("%s path %q is a directory, not an executable", tool, abs)
	}
	// Windows executable permissions are not represented by POSIX mode bits;
	// extensions and CreateProcess decide whether it is runnable there.
	if runtime.GOOS != "windows" && info.Mode().Perm()&0111 == 0 {
		return "", fmt.Errorf("%s path %q is not executable", tool, abs)
	}
	return abs, nil
}
