package install

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"runtime"
	"strings"
	"testing"
)

func TestExpectedSHA256(t *testing.T) {
	sums := "abc123  codebase-memory-mcp-linux-amd64-portable.tar.gz\n" +
		"def456 *codebase-memory-mcp-windows-amd64.zip\n"
	if got := expectedSHA256([]byte(sums), "codebase-memory-mcp-windows-amd64.zip"); got != "def456" {
		t.Fatalf("zip sum = %q, want def456", got)
	}
	if got := expectedSHA256([]byte(sums), "codebase-memory-mcp-linux-amd64-portable.tar.gz"); got != "abc123" {
		t.Fatalf("tar sum = %q, want abc123", got)
	}
	if got := expectedSHA256([]byte(sums), "missing.zip"); got != "" {
		t.Fatalf("missing asset should yield empty, got %q", got)
	}
}

func TestExtractBinaryFromTarGz(t *testing.T) {
	want := []byte("#!/bin/sh\necho hi\n")
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	// a decoy file plus the real binary
	writeTar(t, tw, "README.md", []byte("docs"))
	writeTar(t, tw, "codebase-memory-mcp", want)
	tw.Close()
	gz.Close()

	got, err := extractBinaryFromTarGz(buf.Bytes(), []string{"codebase-memory-mcp", "codebase-memory-mcp.exe"})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("extracted %q, want %q", got, want)
	}

	if _, err := extractBinaryFromTarGz(buf.Bytes(), []string{"nonexistent"}); err == nil {
		t.Fatal("expected error when binary not present")
	}
}

func TestExtractBinaryFromZip(t *testing.T) {
	want := []byte("MZ-fake-windows-binary")
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, _ := zw.Create("codebase-memory-mcp.exe")
	w.Write(want)
	zw.Close()

	got, err := extractBinaryFromZip(buf.Bytes(), []string{"codebase-memory-mcp", "codebase-memory-mcp.exe"})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("extracted %q, want %q", got, want)
	}
}

func TestCBMCPArchiveName(t *testing.T) {
	name, isZip := cbmcpArchiveName()
	if !strings.HasPrefix(name, "codebase-memory-mcp-ui-") {
		t.Fatalf("archive name should be the UI variant, got %q", name)
	}
	switch runtime.GOOS {
	case "windows":
		if !isZip || !strings.HasSuffix(name, ".zip") {
			t.Fatalf("windows should be a .zip, got %q (isZip=%v)", name, isZip)
		}
	case "linux":
		if isZip || !strings.Contains(name, "-portable.") || !strings.HasSuffix(name, ".tar.gz") {
			t.Fatalf("linux should be portable tar.gz, got %q", name)
		}
	default:
		if isZip || !strings.HasSuffix(name, ".tar.gz") {
			t.Fatalf("unix should be tar.gz, got %q", name)
		}
	}
}

func writeTar(t *testing.T, tw *tar.Writer, name string, data []byte) {
	t.Helper()
	if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0755, Size: int64(len(data)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(data); err != nil {
		t.Fatal(err)
	}
}
