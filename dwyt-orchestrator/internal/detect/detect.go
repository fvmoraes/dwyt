package detect

import (
	"os"
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
	ShellZsh  Shell = "zsh"
	ShellBash Shell = "bash"
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
	e.HomeDir, _ = os.UserHomeDir()
	if e.HomeDir == "" {
		e.HomeDir = os.Getenv("HOME")
	}
	e.DwytHome = e.HomeDir + "/.dwyt"
	e.DwytBin = e.DwytHome + "/bin"
	e.DwytData = e.DwytHome + "/data"

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

	if strings.Contains(os.Getenv("SHELL"), "zsh") || os.Getenv("ZSH_VERSION") != "" {
		e.Shell = ShellZsh
		e.ShellRC = e.HomeDir + "/.zshrc"
		e.LoginRC = e.HomeDir + "/.zprofile"
	} else {
		e.Shell = ShellBash
		e.ShellRC = e.HomeDir + "/.bashrc"
		e.LoginRC = e.HomeDir + "/.profile"
		if fileExists(e.HomeDir + "/.bash_profile") {
			e.LoginRC = e.HomeDir + "/.bash_profile"
		}
	}

	return e
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
