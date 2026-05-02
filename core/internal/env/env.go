package env

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

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
	content := fmt.Sprintf(
		"export XDG_CACHE_HOME=%q\nexport PATH=%s:$PATH\n",
		dwytData, dwytBin,
	)
	os.WriteFile(envFile, []byte(content), 0644)

	injectUnixRC(envFile, shellRC)
	if loginRC != "" {
		injectUnixRC(envFile, loginRC)
	}
}

func injectUnixRC(envFile, rcFile string) {
	if rcFile == "" {
		return
	}
	marker     := "# dwyt:source"
	sourceLine := fmt.Sprintf("[[ -f %q ]] && source %q", envFile, envFile)

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

// ── Windows ───────────────────────────────────────────────────────────────────

func initWindows(dwytHome, dwytBin, dwytData string) {
	// 1. Write a PowerShell env file
	envFile := filepath.Join(dwytHome, "env.ps1")
	content := fmt.Sprintf(
		"$env:XDG_CACHE_HOME = %q\n$env:PATH = %q + \";\" + $env:PATH\n",
		dwytData, dwytBin,
	)
	os.WriteFile(envFile, []byte(content), 0644)

	// 2. Inject into PowerShell profile
	profileDir := filepath.Dir(getPowerShellProfile())
	os.MkdirAll(profileDir, 0755)
	injectPowerShellProfile(envFile)

	// 3. Add dwytBin to the user PATH via registry (best practice on Windows)
	addToWindowsUserPath(dwytBin)
}

func getPowerShellProfile() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Documents", "PowerShell", "Microsoft.PowerShell_profile.ps1")
}

func injectPowerShellProfile(envFile string) {
	profile := getPowerShellProfile()
	marker  := "# dwyt:source"
	line    := fmt.Sprintf(". %q", envFile)

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

// addToWindowsUserPath adds dir to HKCU\Environment\PATH via reg.exe.
// This is the standard Windows way — no admin required, persists across sessions.
func addToWindowsUserPath(dir string) {
	// Read current user PATH from registry
	out, err := runCmd("reg", "query", `HKCU\Environment`, "/v", "PATH")
	currentPath := ""
	if err == nil {
		// parse: "    PATH    REG_SZ    <value>"
		for _, line := range strings.Split(string(out), "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(strings.ToUpper(line), "PATH") {
				parts := strings.SplitN(line, "    ", 3)
				if len(parts) == 3 {
					currentPath = strings.TrimSpace(parts[2])
				}
			}
		}
	}

	// Already in PATH?
	if strings.Contains(strings.ToLower(currentPath), strings.ToLower(dir)) {
		return
	}

	newPath := dir
	if currentPath != "" {
		newPath = dir + ";" + currentPath
	}

	runCmd("reg", "add", `HKCU\Environment`, "/v", "PATH", "/t", "REG_EXPAND_SZ", "/d", newPath, "/f")
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

	if runtime.GOOS == "windows" {
		// On Windows: copy the exe into dwytBin as dwyt.exe
		// dwytBin is already in PATH (added via registry above)
		dst := filepath.Join(dwytBin, "dwyt.exe")
		copyFile(exe, dst)
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
		os.Remove(link)
		os.Symlink(exe, link)
	}
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	os.MkdirAll(filepath.Dir(dst), 0755)
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
}
