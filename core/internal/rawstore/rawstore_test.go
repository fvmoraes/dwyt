package rawstore

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func newStore(t *testing.T) *Store {
	t.Helper()
	s, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestPutIsContentAddressedAndIdempotent(t *testing.T) {
	s := newStore(t)
	first, err := s.Put("build failed at line 42", PutOptions{Kind: "build"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.Put("build failed at line 42", PutOptions{Kind: "build"})
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != second.ID {
		t.Fatalf("same bytes must produce the same id: %s vs %s", first.ID, second.ID)
	}
	if !first.CreatedAt.Equal(second.CreatedAt) {
		t.Fatal("re-observing an object must preserve its creation time")
	}

	entries, err := os.ReadDir(s.Dir())
	if err != nil {
		t.Fatal(err)
	}
	// Exactly one payload plus one sidecar; a second copy would be a leak.
	if len(entries) != 2 {
		t.Fatalf("expected 1 payload + 1 sidecar, got %d entries", len(entries))
	}
}

func TestPutRejectsEmptyContent(t *testing.T) {
	if _, err := newStore(t).Put("", PutOptions{}); err == nil {
		t.Fatal("storing empty content should be refused, not silently succeed")
	}
}

func TestGetRoundTripsAndCountsAccess(t *testing.T) {
	s := newStore(t)
	meta, _ := s.Put("hello evidence", PutOptions{Label: "go test"})

	content, got, err := s.Get(meta.Ref())
	if err != nil {
		t.Fatal(err)
	}
	if content != "hello evidence" {
		t.Fatalf("round trip corrupted content: %q", content)
	}
	if got.AccessCount != 1 {
		t.Fatalf("expected access count 1, got %d", got.AccessCount)
	}
	if _, again, _ := s.Get(meta.Ref()); again.AccessCount != 2 {
		t.Fatalf("access count should accumulate, got %d", again.AccessCount)
	}
}

func TestParseRefAcceptsBothForms(t *testing.T) {
	id, err := ParseRef("dwyt://objects/abc123")
	if err != nil || id != "abc123" {
		t.Fatalf("full form failed: %q %v", id, err)
	}
	if id, err := ParseRef("ABC123"); err != nil || id != "abc123" {
		t.Fatalf("bare form failed: %q %v", id, err)
	}
	if id, err := ParseRef("`dwyt://objects/abc123`"); err != nil || id != "abc123" {
		t.Fatalf("markdown-wrapped form failed: %q %v", id, err)
	}
	if _, err := ParseRef("not-hex!"); err == nil {
		t.Fatal("a non-hex reference must be rejected")
	}
	if _, err := ParseRef(""); err == nil {
		t.Fatal("an empty reference must be rejected")
	}
}

func TestExpiredObjectReadsAsNotFound(t *testing.T) {
	s := newStore(t)
	meta, _ := s.Put("stale log", PutOptions{TTL: time.Nanosecond})
	time.Sleep(2 * time.Millisecond)

	if _, _, err := s.Get(meta.Ref()); err != ErrNotFound {
		t.Fatalf("an expired object must report as not found, got %v", err)
	}
}

func TestGetSurvivesMissingSidecar(t *testing.T) {
	s := newStore(t)
	meta, _ := s.Put("orphan payload", PutOptions{})
	if err := os.Remove(filepath.Join(s.Dir(), meta.ID+".json")); err != nil {
		t.Fatal(err)
	}
	// Losing the sidecar must not make the evidence unreadable.
	content, _, err := s.Get(meta.Ref())
	if err != nil {
		t.Fatalf("payload should still be readable: %v", err)
	}
	if content != "orphan payload" {
		t.Fatalf("unexpected content: %q", content)
	}
}

func TestDeleteIsIdempotent(t *testing.T) {
	s := newStore(t)
	meta, _ := s.Put("gone soon", PutOptions{})
	if err := s.Delete(meta.Ref()); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(meta.Ref()); err != nil {
		t.Fatalf("deleting a missing object must be a no-op, got %v", err)
	}
}

func TestPruneRemovesExpiredButKeepsReferenced(t *testing.T) {
	s := newStore(t)
	expired, _ := s.Put("expired evidence", PutOptions{TTL: time.Nanosecond})
	cited, _ := s.Put("cited evidence", PutOptions{TTL: time.Nanosecond})
	live, _ := s.Put("live evidence", PutOptions{TTL: time.Hour})
	time.Sleep(2 * time.Millisecond)

	res, err := s.Prune(map[string]bool{cited.ID: true}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if res.Expired != 1 {
		t.Fatalf("expected exactly one expiry, got %+v", res)
	}
	if _, _, err := s.Get(expired.Ref()); err != ErrNotFound {
		t.Fatal("expired unreferenced object should be gone")
	}
	// An expired object still cited by a note must survive: the reference
	// would otherwise dangle.
	if _, err := s.Stat(cited.Ref()); err != nil {
		t.Fatal("a referenced object must not be pruned")
	}
	if _, err := s.Stat(live.Ref()); err != nil {
		t.Fatal("an unexpired object must not be pruned")
	}
}

func TestPruneRemovesOrphansAndDanglingMetadata(t *testing.T) {
	s := newStore(t)
	meta, _ := s.Put("payload without sidecar", PutOptions{})
	if err := os.Remove(filepath.Join(s.Dir(), meta.ID+".json")); err != nil {
		t.Fatal(err)
	}
	// Dangling sidecar with no payload.
	if err := os.WriteFile(filepath.Join(s.Dir(), "deadbeef.json"), []byte(`{"id":"deadbeef"}`), 0644); err != nil {
		t.Fatal(err)
	}

	res, err := s.Prune(nil, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if res.Orphans != 1 {
		t.Fatalf("expected the orphan payload to be pruned, got %+v", res)
	}
	if _, err := os.Stat(filepath.Join(s.Dir(), "deadbeef.json")); err == nil {
		t.Fatal("dangling metadata should be removed")
	}
}

func TestUsageCountsPayloadsOnly(t *testing.T) {
	s := newStore(t)
	s.Put("one", PutOptions{})
	s.Put("two", PutOptions{})

	count, bytes, err := s.Usage()
	if err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("expected 2 objects (sidecars excluded), got %d", count)
	}
	if bytes != int64(len("one")+len("two")) {
		t.Fatalf("unexpected byte total %d", bytes)
	}
}

func TestNewRejectsEmptyHome(t *testing.T) {
	if _, err := New(""); err == nil {
		t.Fatal("an empty home must be rejected rather than writing to the cwd")
	}
}
