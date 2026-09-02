package env

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

const defaultHeadroomPort = 8787

func Init(dwytHome, dwytBin, dwytData, shellRC, loginRC string) {
	os.MkdirAll(dwytHome, 0755)
	os.MkdirAll(dwytBin, 0755)
	os.MkdirAll(dwytData, 0755)

	if runtime.GOOS == "windows" {
		initWindows(dwytHome, dwytBin, dwytData)
	} else {
		initUnix(dwytHome, dwytBin, dwytData, shellRC, loginRC)
	}

	// Symlink/copy the binary so `dwyt` is immediately available
	installBinaryOnPath(dwytBin)

	fmt.Printf("  ✓ Ambiente configurado\n")
}

// ── Unix (Linux + macOS) ──────────────────────────────────────────────────────

func initUnix(dwytHome, dwytBin, dwytData, shellRC, loginRC string) {
	envFile := filepath.Join(dwytHome, "env.sh")
	os.WriteFile(envFile, []byte(unixEnvContent(dwytHome, dwytBin, dwytData, defaultHeadroomPort)), 0644)

	injectUnixRC(envFile, shellRC)
	if loginRC != "" {
		injectUnixRC(envFile, loginRC)
	}
}

func unixEnvContent(dwytHome, dwytBin, dwytData string, headroomPort int) string {
	return fmt.Sprintf(
		"export XDG_CACHE_HOME=%s\nexport DWYT_HOME=%s\nexport PATH=%s:$PATH\n"+
			"# Headroom proxy — automatic compression of AI API calls\n"+
			"export HEADROOM_PORT=%d\n"+
			"export OPENAI_BASE_URL=%s\n"+
			"export ANTHROPIC_BASE_URL=%s\n"+
			"# Codebase — store indexes in ~/.dwyt/codebase\n"+
			"export CBM_CACHE_DIR=%s\n",
		posixShellLiteral(dwytData),
		posixShellLiteral(dwytHome),
		posixShellLiteral(dwytBin),
		headroomPort,
		posixShellLiteral(fmt.Sprintf("http://127.0.0.1:%d/v1", headroomPort)),
		posixShellLiteral(fmt.Sprintf("http://127.0.0.1:%d", headroomPort)),
		posixShellLiteral(filepath.Join(dwytHome, "codebase")),
	)
}

func posixShellLiteral(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func injectUnixRC(envFile, rcFile string) {
	if rcFile == "" {
		return
	}
	marker := "# dwyt:source"
	sourceLine := unixSourceLine(envFile)

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

func unixSourceLine(envFile string) string {
	return fmt.Sprintf("[[ -f %s ]] && source %s", posixShellLiteral(envFile), posixShellLiteral(envFile))
}

// ── Windows ───────────────────────────────────────────────────────────────────

func initWindows(dwytHome, dwytBin, dwytData string) {
	// 1. Write a PowerShell env file
	envFile := filepath.Join(dwytHome, "env.ps1")
	os.WriteFile(envFile, []byte(windowsEnvContent(dwytHome, dwytBin, dwytData, defaultHeadroomPort)), 0644)

	// 2. Inject into both supported PowerShell profile locations: Windows
	// PowerShell 5.1 uses Documents/WindowsPowerShell while PowerShell 7+
	// uses Documents/PowerShell.
	for _, profile := range getPowerShellProfiles() {
		os.MkdirAll(filepath.Dir(profile), 0755)
		injectPowerShellProfileAt(profile, envFile)
	}

	// 3. Add dwytBin to the user PATH via registry (best practice on Windows)
	addToWindowsUserPath(dwytBin)
}

// windowsEnvContent is intentionally rendered with PowerShell single-quoted
// literals. Go's %q escapes Windows backslashes (C:\\Users...), but PowerShell
// does not interpret backslash escapes and would retain both slashes.
func windowsEnvContent(dwytHome, dwytBin, dwytData string, headroomPort int) string {
	return fmt.Sprintf(
		"$env:XDG_CACHE_HOME = %s\r\n"+
			"$env:DWYT_HOME = %s\r\n"+
			"$env:PATH = %s + ';' + $env:PATH\r\n"+
			"# Headroom proxy — automatic compression of AI API calls\r\n"+
			"$env:HEADROOM_PORT = %s\r\n"+
			"$env:OPENAI_BASE_URL = %s\r\n"+
			"$env:ANTHROPIC_BASE_URL = %s\r\n"+
			"# Codebase — store indexes in ~/.dwyt/codebase\r\n"+
			"$env:CBM_CACHE_DIR = %s\r\n",
		powerShellLiteral(dwytData),
		powerShellLiteral(dwytHome),
		powerShellLiteral(dwytBin),
		powerShellLiteral(strconv.Itoa(headroomPort)),
		powerShellLiteral(fmt.Sprintf("http://127.0.0.1:%d/v1", headroomPort)),
		powerShellLiteral(fmt.Sprintf("http://127.0.0.1:%d", headroomPort)),
		powerShellLiteral(windowsPathJoin(dwytHome, "codebase")),
	)
}

func windowsPathJoin(base, name string) string {
	return strings.TrimRight(base, `\\/`) + `\` + name
}

func powerShellLiteral(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

// SetHeadroomPort publishes the actual port selected at runtime. The parent
// shell cannot be mutated by a child daemon, but updating its environment and
// the DWYT-managed env file keeps wrappers, new terminals, and subsequent
// client launches consistent after a fallback from 8787.
func SetHeadroomPort(dwytHome string, port int) error {
	if port < 1 || port > 65535 {
		return fmt.Errorf("invalid Headroom port %d", port)
	}
	portText := strconv.Itoa(port)
	for key, value := range map[string]string{
		"HEADROOM_PORT":      portText,
		"OPENAI_BASE_URL":    fmt.Sprintf("http://127.0.0.1:%d/v1", port),
		"ANTHROPIC_BASE_URL": fmt.Sprintf("http://127.0.0.1:%d", port),
	} {
		if err := os.Setenv(key, value); err != nil {
			return err
		}
	}
	if dwytHome == "" {
		return nil
	}

	name := "env.sh"
	windows := runtime.GOOS == "windows"
	if windows {
		name = "env.ps1"
	}
	envFile := filepath.Join(dwytHome, name)
	content, err := os.ReadFile(envFile)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	updated := updateHeadroomEnvContent(string(content), port, windows)
	return os.WriteFile(envFile, []byte(updated), 0644)
}

func updateHeadroomEnvContent(content string, port int, windows bool) string {
	assignments := []struct {
		name  string
		value string
	}{
		{"HEADROOM_PORT", strconv.Itoa(port)},
		{"OPENAI_BASE_URL", fmt.Sprintf("http://127.0.0.1:%d/v1", port)},
		{"ANTHROPIC_BASE_URL", fmt.Sprintf("http://127.0.0.1:%d", port)},
	}
	for _, assignment := range assignments {
		line := "export " + assignment.name + "=" + shellEnvValue(assignment.value)
		prefix := "export " + assignment.name + "="
		if windows {
			line = "$env:" + assignment.name + " = " + powerShellLiteral(assignment.value)
			prefix = "$env:" + assignment.name
		}
		content = replaceEnvLine(content, prefix, line)
	}
	return content
}

func shellEnvValue(value string) string {
	if strings.ContainsAny(value, " \t\"'$`\\") {
		return posixShellLiteral(value)
	}
	return value
}

func replaceEnvLine(content, prefix, replacement string) string {
	lineEnding := "\n"
	if strings.Contains(content, "\r\n") {
		lineEnding = "\r\n"
	}
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(strings.ToUpper(trimmed), strings.ToUpper(prefix)) {
			ending := ""
			if strings.HasSuffix(line, "\r") {
				ending = "\r"
			}
			lines[i] = replacement + ending
			return strings.Join(lines, "\n")
		}
	}
	if content != "" && !strings.HasSuffix(content, "\n") {
		content += lineEnding
	}
	return content + replacement + lineEnding
}

func getPowerShellProfile() string {
	return getPowerShellProfiles()[0]
}

func getPowerShellProfiles() []string {
	home, _ := os.UserHomeDir()
	return powerShellProfilesForHome(home)
}

func powerShellProfilesForHome(home string) []string {
	return []string{
		filepath.Join(home, "Documents", "PowerShell", "Microsoft.PowerShell_profile.ps1"),
		filepath.Join(home, "Documents", "WindowsPowerShell", "Microsoft.PowerShell_profile.ps1"),
	}
}

func injectPowerShellProfile(envFile string) {
	injectPowerShellProfileAt(getPowerShellProfile(), envFile)
}

func injectPowerShellProfileAt(profile, envFile string) {
	marker := "# dwyt:source"
	line := powerShellProfileSourceLine(envFile)

	data, _ := os.ReadFile(profile)
	if strings.Contains(string(data), marker) {
		return
	}
	f, _ := os.OpenFile(profile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if f != nil {
		defer f.Close()
		fmt.Fprintf(f, "\r\n%s\r\n%s\r\n", marker, line)
	}
}

func powerShellProfileSourceLine(envFile string) string {
	return ". " + powerShellLiteral(envFile)
}

// addToWindowsUserPath adds dir to HKCU\Environment\PATH via reg.exe.
// This is the standard Windows way — no admin required, persists across sessions.
func addToWindowsUserPath(dir string) {
	// Read current user PATH from registry
	out, err := runCmd("reg", "query", `HKCU\Environment`, "/v", "PATH")
	currentPath := ""
	if err == nil {
		currentPath = parseWindowsUserPath(string(out))
	}

	// Already in PATH? Compare semicolon-delimited entries. A substring check
	// incorrectly treats C:\\dwyt\\binary as if C:\\dwyt\\bin were present.
	if windowsPathContains(currentPath, dir) {
		return
	}

	newPath := dir
	if currentPath != "" {
		newPath = dir + ";" + currentPath
	}

	runCmd("reg", "add", `HKCU\Environment`, "/v", "PATH", "/t", "REG_EXPAND_SZ", "/d", newPath, "/f")
}

func windowsPathContains(pathValue, dir string) bool {
	for _, entry := range strings.Split(pathValue, ";") {
		if strings.EqualFold(strings.TrimSpace(entry), strings.TrimSpace(dir)) {
			return true
		}
	}
	return false
}

// parseWindowsUserPath accepts both the spaces and tabs emitted by reg.exe,
// while preserving spaces in actual path entries (for example Program Files).
func parseWindowsUserPath(output string) string {
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 || !strings.EqualFold(fields[0], "PATH") {
			continue
		}
		typeIndex := strings.Index(line, fields[1])
		if typeIndex < 0 {
			continue
		}
		return strings.TrimSpace(line[typeIndex+len(fields[1]):])
	}
	return ""
}

func runCmd(name string, args ...string) ([]byte, error) {
	cmd := fmt.Sprintf("%s %s", name, strings.Join(args, " "))
	_ = cmd
	// Use os/exec indirectly to avoid import cycle — call via shell
	// We use a simple approach: write a temp script and run it
	// Actually just use exec directly here
	return execRun(name, args...)
}

// ── PATH symlink (Unix) / copy (Windows) ─────────────────────────────────────

func installBinaryOnPath(dwytBin string) {
	exe, err := os.Executable()
	if err != nil {
		return
	}

	// Resolve the real path — critical on macOS where os.Executable()
	// may return the symlink itself, causing "too many levels of symbolic links"
	realExe, err := filepath.EvalSymlinks(exe)
	if err != nil {
		realExe = exe // fallback to original if resolution fails
	}

	if runtime.GOOS == "windows" {
		dst := filepath.Join(dwytBin, "dwyt.exe")
		if err := copyFile(realExe, dst); err != nil {
			fmt.Printf("  ⚠ Não foi possível atualizar %s: %v\n", dst, err)
		}
		return
	}

	// Unix: symlink into ~/.local/bin (usually already in PATH on modern distros)
	home, _ := os.UserHomeDir()
	localBin := filepath.Join(home, ".local", "bin")
	os.MkdirAll(localBin, 0755)

	for _, link := range []string{
		filepath.Join(localBin, "dwyt"),
		filepath.Join(dwytBin, "dwyt"),
	} {
		// Skip if the link would point to the same on-disk file as the binary.
		// On macOS, /tmp is a symlink to /private/tmp, so a string compare alone
		// misses this — we must resolve the link's parent directory too.
		// Without this, we'd remove the binary and replace it with a symlink to
		// itself, producing "too many levels of symbolic links".
		if sameFile(link, realExe) {
			continue
		}
		// Skip if an existing symlink already points to the right place
		if existing, err := os.Readlink(link); err == nil && existing == realExe {
			continue
		}
		os.Remove(link)
		os.Symlink(realExe, link)
	}
}

// sameFile reports whether two paths resolve to the same on-disk file. It
// handles hard links and symlinks when both paths exist, then falls back to a
// normalized parent-path comparison for a destination that has not been
// created yet (including macOS's /tmp -> /private/tmp indirection).
func sameFile(path, target string) bool {
	if path == target {
		return true
	}
	pathInfo, pathErr := os.Stat(path)
	targetInfo, targetErr := os.Stat(target)
	if pathErr == nil && targetErr == nil && os.SameFile(pathInfo, targetInfo) {
		return true
	}
	return normalizedFilePath(path) == normalizedFilePath(target)
}

func normalizedFilePath(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return filepath.Clean(path)
	}
	if parent, err := filepath.EvalSymlinks(filepath.Dir(abs)); err == nil {
		return filepath.Join(parent, filepath.Base(abs))
	}
	return filepath.Clean(abs)
}

// copyFile copies through a sibling temporary file and only replaces dst after
// a complete successful write. This is safe for running executables on Unix
// and avoids truncating the destination before a Windows replacement can be
// attempted. On Windows, a locked destination may reject the direct rename;
// in that case we move it aside, install the replacement, and restore it if
// the second rename fails.
func copyFile(src, dst string) error {
	// This is essential when `dwyt` was launched from dwytBin already. Opening
	// dst with os.Create would truncate the currently running executable (and
	// its source handle) before any bytes could be copied.
	if sameFile(src, dst) {
		return nil
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(dst), ".dwyt-"+filepath.Base(dst)+"-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if _, err := io.Copy(tmp, in); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}

	// The normal path is atomic because the temporary lives alongside dst.
	if err := os.Rename(tmpName, dst); err == nil {
		return nil
	} else if _, statErr := os.Stat(dst); statErr != nil {
		return err
	}

	// Windows cannot replace a locked executable in one rename. Never delete
	// the old binary first: move it aside and restore it on any failure.
	backup := dst + ".old"
	_ = os.Remove(backup)
	if err := os.Rename(dst, backup); err != nil {
		return fmt.Errorf("move existing executable aside: %w", err)
	}
	if err := os.Rename(tmpName, dst); err != nil {
		_ = os.Rename(backup, dst)
		return fmt.Errorf("install replacement executable: %w", err)
	}
	_ = os.Remove(backup) // may still be locked; next update retries cleanup
	return nil
}
