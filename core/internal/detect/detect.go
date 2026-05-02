package detect

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

type OS string

const (
	OSMacOS   OS = "macos"
	OSDebian  OS = "debian"
	OSFedora  OS = "fedora"
	OSWindows OS = "windows"
)

type Shell string

const (
	ShellZsh        Shell = "zsh"
	ShellBash       Shell = "bash"
	ShellPowerShell Shell = "powershell"
)

type Env struct {
	OS       OS
	Arch     string
	Shell    Shell
	ShellRC  string
	LoginRC  string
	HomeDir  string
	DwytHome string
	DwytBin  string
	DwytData string
}

func Detect() *Env {
	e := &Env{Arch: runtime.GOARCH}

	// os.UserHomeDir() works on all platforms:
	//   Linux/macOS → /home/user  or  /Users/user
	//   Windows     → C:\Users\user
	e.HomeDir, _ = os.UserHomeDir()
	if e.HomeDir == "" {
		// fallback: USERPROFILE on Windows, HOME on Unix
		if runtime.GOOS == "windows" {
			e.HomeDir = os.Getenv("USERPROFILE")
		} else {
			e.HomeDir = os.Getenv("HOME")
		}
	}

	// DwytHome:
	//   Windows → %APPDATA%\dwyt  (C:\Users\user\AppData\Roaming\dwyt)
	//             This is the standard location for per-user app data on Windows.
	//   Unix    → ~/.dwyt
	if runtime.GOOS == "windows" {
		appData := os.Getenv("APPDATA")
		if appData == "" {
			appData = filepath.Join(e.HomeDir, "AppData", "Roaming")
		}
		e.DwytHome = filepath.Join(appData, "dwyt")
	} else {
		e.DwytHome = filepath.Join(e.HomeDir, ".dwyt")
	}

	e.DwytBin  = filepath.Join(e.DwytHome, "bin")
	e.DwytData = filepath.Join(e.DwytHome, "data")

	// OS detection
	switch {
	case runtime.GOOS == "darwin":
		e.OS = OSMacOS
	case runtime.GOOS == "windows":
		e.OS = OSWindows
	case fileExists("/etc/debian_version"):
		e.OS = OSDebian
	case fileExists("/etc/fedora-release"), fileExists("/etc/redhat-release"):
		e.OS = OSFedora
	default:
		e.OS = OSDebian
	}

	// Shell detection
	if runtime.GOOS == "windows" {
		e.Shell   = ShellPowerShell
		// PowerShell profile — created if missing
		e.ShellRC = filepath.Join(e.HomeDir, "Documents", "PowerShell", "Microsoft.PowerShell_profile.ps1")
		e.LoginRC = ""
	} else if strings.Contains(os.Getenv("SHELL"), "zsh") || os.Getenv("ZSH_VERSION") != "" {
		e.Shell   = ShellZsh
		e.ShellRC = filepath.Join(e.HomeDir, ".zshrc")
		e.LoginRC = filepath.Join(e.HomeDir, ".zprofile")
	} else {
		e.Shell   = ShellBash
		e.ShellRC = filepath.Join(e.HomeDir, ".bashrc")
		e.LoginRC = filepath.Join(e.HomeDir, ".profile")
		if fileExists(filepath.Join(e.HomeDir, ".bash_profile")) {
			e.LoginRC = filepath.Join(e.HomeDir, ".bash_profile")
		}
	}

	return e
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
