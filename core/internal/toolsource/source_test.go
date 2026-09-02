package toolsource

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func writeExternalExecutable(t *testing.T, name string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte("fixture"), 0755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestNormalizeAllDefaultsLegacyConfigToDWYTManaged(t *testing.T) {
	sources := NormalizeAll(nil)
	for _, tool := range Tools() {
		if got := sources[tool]; got.Mode != ModeDWYT || got.Path != "" {
			t.Fatalf("%s = %#v, want DWYT-managed empty path", tool, got)
		}
	}
}

func TestResolveExternalUsesExplicitExecutableWithoutDWYTFallback(t *testing.T) {
	external := writeExternalExecutable(t, "headroom-local")
	got, err := Resolve(t.TempDir(), ToolHeadroom, Selection{Mode: ModeExternal, Path: external})
	if err != nil {
		t.Fatal(err)
	}
	if got != external {
		t.Fatalf("Resolve external = %q, want %q", got, external)
	}
}

func TestResolveExternalRejectsMissingPathInsteadOfUsingManagedBinary(t *testing.T) {
	if _, err := Resolve(t.TempDir(), ToolRTK, Selection{Mode: ModeExternal, Path: filepath.Join(t.TempDir(), "missing")}); err == nil {
		t.Fatal("external mode must reject a missing path instead of falling back to DWYT")
	}
}

func TestDetectExternalPathRequiresExecutableOnUnix(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows does not expose POSIX executable mode bits")
	}
	path := filepath.Join(t.TempDir(), "rtk")
	if err := os.WriteFile(path, []byte("fixture"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := Detect(ToolRTK, path); err == nil {
		t.Fatal("expected non-executable external path to be rejected")
	}
}
