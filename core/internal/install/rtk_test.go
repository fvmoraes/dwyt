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
