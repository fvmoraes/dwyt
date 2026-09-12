package root

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/fvmoraes/dwyt/internal/detect"
	"github.com/fvmoraes/dwyt/internal/platform"
)

func TestSandboxUninstallPreservesVaultAndExternalConfig(t *testing.T) {
	sandboxRoot := t.TempDir()
	home := filepath.Join(sandboxRoot, "home")
	dwytHome := filepath.Join(home, ".dwyt")
	installDir := filepath.Join(home, ".local", "bin")
	vault := filepath.Join(dwytHome, "projects", "project", "vault.md")
	managed := filepath.Join(dwytHome, "cache", "managed.txt")
	launcher := platform.DWYTLauncherPath(installDir, "dwyt")
	externalConfig := filepath.Join(home, ".config", "keep.txt")

	for path, content := range map[string]string{
		vault:          "user vault",
		managed:        "managed data",
		launcher:       "launcher",
		externalConfig: "external config",
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	t.Setenv("HOME", home)
	t.Setenv("DWYT_HOME", dwytHome)
	env := &detect.Env{DwytHome: dwytHome, DwytBin: filepath.Join(dwytHome, "bin")}
	if err := sandboxUninstall(env, sandboxRoot, installDir); err != nil {
		t.Fatalf("sandboxUninstall() error = %v", err)
	}

	for _, path := range []string{vault, externalConfig} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("user-owned file %s was not preserved: %v", path, err)
		}
	}
	for _, path := range []string{managed, launcher} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("managed file %s still exists or could not be checked: %v", path, err)
		}
	}
}

func TestSandboxUninstallRejectsInstallDirOutsideSandbox(t *testing.T) {
	sandboxRoot := t.TempDir()
	home := filepath.Join(sandboxRoot, "home")
	dwytHome := filepath.Join(home, ".dwyt")
	if err := os.MkdirAll(dwytHome, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("DWYT_HOME", dwytHome)

	env := &detect.Env{DwytHome: dwytHome}
	outside := filepath.Join(filepath.Dir(sandboxRoot), "outside-bin")
	if err := sandboxUninstall(env, sandboxRoot, outside); err == nil {
		t.Fatal("sandboxUninstall accepted an install directory outside its sandbox")
	}
}
