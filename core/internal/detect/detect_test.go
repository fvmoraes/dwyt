package detect

import (
	"path/filepath"
	"testing"
)

func TestDetectHonorsDWYTHomeOverride(t *testing.T) {
	override := filepath.Join(t.TempDir(), "dwyt-state")
	t.Setenv("DWYT_HOME", override)

	env := Detect()
	if env.DwytHome != override {
		t.Fatalf("DwytHome = %q, want override %q", env.DwytHome, override)
	}
	if want := filepath.Join(override, "bin"); env.DwytBin != want {
		t.Fatalf("DwytBin = %q, want %q", env.DwytBin, want)
	}
	if want := filepath.Join(override, "data"); env.DwytData != want {
		t.Fatalf("DwytData = %q, want %q", env.DwytData, want)
	}
}
