package install

import (
	"runtime"
	"strings"
	"testing"
)

func TestRTKTargetTriple(t *testing.T) {
	target, err := rtkTargetTriple()
	switch runtime.GOOS {
	case "linux", "darwin":
		if err != nil {
			t.Fatalf("expected a target triple on %s/%s, got error: %v", runtime.GOOS, runtime.GOARCH, err)
		}
		if !strings.Contains(target, "linux") && !strings.Contains(target, "apple-darwin") {
			t.Fatalf("unexpected target triple %q", target)
		}
	case "windows":
		// rtk now publishes an official Windows build
		// (rtk-x86_64-pc-windows-msvc.zip) — see rtk.go.
		if err != nil {
			t.Fatalf("expected a target triple on windows, got error: %v", err)
		}
		if !strings.Contains(target, "windows") {
			t.Fatalf("unexpected windows target triple %q", target)
		}
	default:
		if err == nil {
			t.Fatalf("expected unsupported-platform error on %s, got %q", runtime.GOOS, target)
		}
	}
}

func TestRTKBinaryName(t *testing.T) {
	name := rtkBinaryName()
	if runtime.GOOS == "windows" && name != "rtk.exe" {
		t.Fatalf("windows binary name = %q, want rtk.exe", name)
	}
	if runtime.GOOS != "windows" && name != "rtk" {
		t.Fatalf("unix binary name = %q, want rtk", name)
	}
}
