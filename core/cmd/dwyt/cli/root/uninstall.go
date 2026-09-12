package root

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/fvmoraes/dwyt/internal/detect"
	"github.com/fvmoraes/dwyt/internal/platform"
	"github.com/fvmoraes/dwyt/internal/security"
)

func cleanDWYTHome(e *detect.Env) {
	fmt.Printf("  → Cleaning DWYT home: %s\n", e.DwytHome)
	if !security.IsSafeHome(e.DwytHome) {
		fmt.Printf("  ✗ Unsafe DWYT home path: %s — refusing to clean\n", e.DwytHome)
		return
	}
	security.CleanHome(e.DwytHome)
	fmt.Println("  ✓ DWYT home cleaned (Obsidian vaults preserved)")
}

// sandboxUninstall is the deliberately narrow lifecycle used by hermetic
// installer tests. It only touches paths proved to be below sandboxRoot:
// DWYT-managed state (while retaining protected vaults) and, when supplied,
// the test launcher. It never terminates processes, edits profiles/PATH, runs
// package managers, or reaches globally installed tools.
func sandboxUninstall(e *detect.Env, sandboxRoot, installDir string) error {
	if e == nil || e.DwytHome == "" {
		return fmt.Errorf("sandbox uninstall requires a DWYT home")
	}
	root, err := filepath.Abs(sandboxRoot)
	if err != nil || sandboxRoot == "" {
		return fmt.Errorf("sandbox uninstall requires an absolute-safe root")
	}
	home, err := filepath.Abs(e.DwytHome)
	if err != nil {
		return fmt.Errorf("resolve DWYT home: %w", err)
	}
	if !pathWithin(root, home) {
		return fmt.Errorf("DWYT home is outside sandbox root: %s", e.DwytHome)
	}
	if installDir != "" {
		bin, err := filepath.Abs(installDir)
		if err != nil {
			return fmt.Errorf("resolve sandbox install dir: %w", err)
		}
		if !pathWithin(root, bin) {
			return fmt.Errorf("install directory is outside sandbox root: %s", installDir)
		}
	}
	if !security.IsSafeHome(e.DwytHome) {
		return fmt.Errorf("unsafe DWYT home path: %s", e.DwytHome)
	}

	cleanDWYTHome(e)
	if installDir == "" {
		return nil
	}
	launcher := platform.DWYTLauncherPath(installDir, "dwyt")
	if err := os.Remove(launcher); err != nil && !os.IsNotExist(err) {
		// Windows cannot remove the executable currently running this command.
		// The protected-data contract still holds because CleanHome ran inside
		// the explicit sandbox; the CI scenario asserts that managed state is
		// gone and the user-owned vault/config remain intact.
		if runtime.GOOS != "windows" {
			return fmt.Errorf("remove sandbox launcher: %w", err)
		}
	}
	return nil
}

func pathWithin(root, target string) bool {
	rel, err := filepath.Rel(root, target)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return false
	}
	return !filepath.IsAbs(rel)
}

func removeSymlinks(home string) {
	if runtime.GOOS == "windows" {
		return
	}
	localBin := filepath.Join(home, ".local", "bin")
	for _, name := range []string{"dwyt", "rtk", "headroom", "codebase-memory-mcp"} {
		link := filepath.Join(localBin, name)
		if _, err := os.Lstat(link); err == nil {
			if err := os.Remove(link); err != nil {
				fmt.Printf("  ⚠ Failed to remove symlink %s: %v\n", link, err)
				continue
			}
			fmt.Printf("  ✓ Removed symlink: %s\n", link)
		}
	}
}

func removeRTKData(home string) {
	fmt.Println("  → Removing RTK data...")
	dirs := []string{
		filepath.Join(home, ".rtk"),
		filepath.Join(home, ".config", "rtk"),
		filepath.Join(home, ".local", "share", "rtk"),
	}
	for _, d := range dirs {
		if _, err := os.Stat(d); err == nil {
			if err := os.RemoveAll(d); err != nil {
				fmt.Printf("  ⚠ Failed to remove %s: %v\n", d, err)
				continue
			}
			fmt.Printf("  ✓ Removed: %s\n", d)
		}
	}
	bins := []string{
		filepath.Join(home, ".local", "bin", "rtk"),
		"/usr/local/bin/rtk",
	}
	for _, b := range bins {
		if _, err := os.Lstat(b); err == nil {
			if err := os.Remove(b); err != nil {
				fmt.Printf("  ⚠ Failed to remove %s: %v\n", b, err)
				continue
			}
			fmt.Printf("  ✓ Removed: %s\n", b)
		}
	}
}

func removeHeadroomData(home string) {
	fmt.Println("  → Removing Headroom data...")
	dirs := []string{
		filepath.Join(home, ".headroom"),
		filepath.Join(home, ".config", "headroom"),
		filepath.Join(home, ".local", "share", "headroom"),
	}
	for _, d := range dirs {
		if _, err := os.Stat(d); err == nil {
			if err := os.RemoveAll(d); err != nil {
				fmt.Printf("  ⚠ Failed to remove %s: %v\n", d, err)
				continue
			}
			fmt.Printf("  ✓ Removed: %s\n", d)
		}
	}
	_ = exec.Command("pip", "uninstall", "-y", "headroom-ai").Run()
	_ = exec.Command("pip3", "uninstall", "-y", "headroom-ai").Run()
}

func removeCodebaseData(home string, e *detect.Env) {
	fmt.Println("  → Removing Codebase data...")
	dirs := []string{
		filepath.Join(e.DwytHome, "codebase"),
		filepath.Join(home, ".cache", "codebase-memory-mcp"),
		filepath.Join(home, ".codebase-memory-mcp"),
		filepath.Join(home, ".config", "codebase-memory-mcp"),
	}
	for _, d := range dirs {
		if _, err := os.Stat(d); err == nil {
			if err := os.RemoveAll(d); err != nil {
				fmt.Printf("  ⚠ Failed to remove %s: %v\n", d, err)
				continue
			}
			fmt.Printf("  ✓ Removed: %s\n", d)
		}
	}
	cbmcpBin := platform.DWYTLauncherPath(e.DwytBin, "codebase-memory-mcp")
	if _, err := os.Stat(cbmcpBin); err == nil {
		if err := exec.Command(cbmcpBin, "uninstall", "-y").Run(); err != nil {
			fmt.Printf("  ⚠ Failed to remove Codebase agent configs: %v\n", err)
		} else {
			fmt.Println("  ✓ Codebase agent configs removed")
		}
	}
	bins := []string{
		filepath.Join(home, ".local", "bin", "codebase-memory-mcp"),
		"/usr/local/bin/codebase-memory-mcp",
	}
	for _, b := range bins {
		if _, err := os.Lstat(b); err == nil {
			if err := os.Remove(b); err != nil {
				fmt.Printf("  ⚠ Failed to remove %s: %v\n", b, err)
				continue
			}
			fmt.Printf("  ✓ Removed: %s\n", b)
		}
	}
}

func cleanShellRC(e *detect.Env) {
	fmt.Println("  → Cleaning shell RC files...")
	for _, rc := range []string{e.ShellRC, e.LoginRC} {
		if rc == "" {
			continue
		}
		if cleaned := removeFromRC(rc); cleaned {
			fmt.Printf("  ✓ Cleaned: %s\n", rc)
		}
	}
}

func cleanPowerShellProfile(home string) {
	if runtime.GOOS != "windows" {
		return
	}
	psProfile := filepath.Join(home, "Documents", "PowerShell", "Microsoft.PowerShell_profile.ps1")
	if cleaned := removeFromRC(psProfile); cleaned {
		fmt.Printf("  ✓ Cleaned PowerShell profile: %s\n", psProfile)
	}
}

func removeFromRC(rcFile string) bool {
	data, err := os.ReadFile(rcFile)
	if err != nil {
		return false
	}
	original := string(data)
	lines := strings.Split(original, "\n")
	filtered := make([]string, 0, len(lines))
	skip := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "# dwyt:source" {
			skip = true
			continue
		}
		if skip {
			skip = false
			continue
		}
		filtered = append(filtered, line)
	}
	result := strings.Join(filtered, "\n")
	for strings.Contains(result, "\n\n\n") {
		result = strings.ReplaceAll(result, "\n\n\n", "\n\n")
	}
	if result == original {
		return false
	}
	if err := os.WriteFile(rcFile, []byte(result), 0644); err != nil {
		fmt.Printf("  ⚠ Failed to clean %s: %v\n", rcFile, err)
		return false
	}
	return true
}

func removeFromWindowsUserPath(dwytBin string) {
	out, err := exec.Command("reg", "query", `HKCU\Environment`, "/v", "PATH").Output()
	if err != nil {
		return
	}
	currentPath := ""
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(strings.ToUpper(line), "PATH") {
			parts := strings.SplitN(line, "    ", 3)
			if len(parts) == 3 {
				currentPath = strings.TrimSpace(parts[2])
			}
		}
	}
	if currentPath == "" {
		return
	}
	segments := strings.Split(currentPath, ";")
	filtered := make([]string, 0, len(segments))
	for _, s := range segments {
		if !strings.EqualFold(strings.TrimSpace(s), dwytBin) {
			filtered = append(filtered, s)
		}
	}
	newPath := strings.Join(filtered, ";")
	if err := exec.Command("reg", "add", `HKCU\Environment`, "/v", "PATH", "/t", "REG_EXPAND_SZ", "/d", newPath, "/f").Run(); err != nil {
		fmt.Printf("  ⚠ Failed to update Windows PATH: %v\n", err)
	}
}
