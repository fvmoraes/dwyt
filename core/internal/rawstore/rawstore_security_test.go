package rawstore

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Raw objects hold tool output, which routinely embeds tokens and environment
// values. They must not be world-readable, and a single write must not be able
// to exhaust the disk.
func TestPutKeepsObjectsPrivate(t *testing.T) {
	s, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	meta, err := s.Put("build log with embedded credentials", PutOptions{})
	if err != nil {
		t.Fatal(err)
	}

	objInfo, err := os.Stat(filepath.Join(s.Dir(), meta.ID))
	if err != nil {
		t.Fatal(err)
	}
	if objInfo.Mode().Perm() != 0600 {
		t.Fatalf("object should be 0600, got %v", objInfo.Mode().Perm())
	}
	metaInfo, err := os.Stat(filepath.Join(s.Dir(), meta.ID+".json"))
	if err != nil {
		t.Fatal(err)
	}
	if metaInfo.Mode().Perm() != 0600 {
		t.Fatalf("meta should be 0600, got %v", metaInfo.Mode().Perm())
	}
	dirInfo, err := os.Stat(s.Dir())
	if err != nil {
		t.Fatal(err)
	}
	if dirInfo.Mode().Perm() != 0700 {
		t.Fatalf("store dir should be 0700, got %v", dirInfo.Mode().Perm())
	}
}

func TestPutRejectsOversizedContent(t *testing.T) {
	s, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	huge := strings.Repeat("x", MaxObjectBytes+1)
	if _, err := s.Put(huge, PutOptions{}); err == nil {
		t.Fatal("content over MaxObjectBytes must be refused")
	}
	// The refused write must not leave a partial object behind.
	entries, _ := os.ReadDir(s.Dir())
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		t.Fatalf("no object should exist after a refused write, found %q", e.Name())
	}
}

func TestPutAcceptsContentJustUnderTheLimit(t *testing.T) {
	s, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	big := strings.Repeat("x", MaxObjectBytes-1)
	if _, err := s.Put(big, PutOptions{}); err != nil {
		t.Fatalf("content under the limit should be stored: %v", err)
	}
}
