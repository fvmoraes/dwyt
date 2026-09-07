// Package rawstore implements the DWYT v5 Raw Object Store (spec §29).
//
// High-volume evidence — build logs, test output, stack traces, tool dumps —
// must stay *recoverable* without ever occupying the default context. The store
// is content-addressed under ~/.dwyt/objects/ and referenced from notes and
// tool summaries by a `dwyt://objects/<id>` URI.
//
// Two properties matter more than performance here:
//
//   - Writing is idempotent. The same bytes always produce the same reference,
//     so re-running a failing test does not multiply objects on disk.
//   - Reading never fails destructively. A missing object returns a clear
//     "expired" error rather than an empty string, because silently returning
//     nothing would make a summary look like it had no evidence behind it.
package rawstore

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// URIScheme is the prefix of a raw object reference.
const URIScheme = "dwyt://objects/"

// ErrNotFound is returned when an object is absent: either it never existed or
// it was pruned after its TTL expired.
var ErrNotFound = errors.New("rawstore: object not found or expired")

// Meta is the sidecar record kept next to each object. It carries what the
// housekeeper needs to decide retention, and what a consumer needs to display
// the object without reading it.
type Meta struct {
	ID        string    `json:"id"`
	Bytes     int64     `json:"bytes"`
	Tokens    int       `json:"tokens_est"`
	Kind      string    `json:"kind,omitempty"`
	Label     string    `json:"label,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	// ExpiresAt is the TTL deadline. Zero means "no automatic expiry".
	ExpiresAt time.Time `json:"expires_at,omitempty"`
	// AccessCount and LastAccessed support the housekeeper's usage-aware
	// retention (spec §25).
	AccessCount  int       `json:"access_count,omitempty"`
	LastAccessed time.Time `json:"last_accessed,omitempty"`
}

// Ref is the reference handed back to callers.
func (m Meta) Ref() string { return URIScheme + m.ID }

// Expired reports whether the object is past its TTL as of now.
func (m Meta) Expired(now time.Time) bool {
	return !m.ExpiresAt.IsZero() && now.After(m.ExpiresAt)
}

// Store is a content-addressed object store rooted at a directory.
type Store struct {
	dir string
}

// New opens (and creates if needed) a store under dwytHome/objects.
func New(dwytHome string) (*Store, error) {
	if strings.TrimSpace(dwytHome) == "" {
		return nil, fmt.Errorf("rawstore: empty dwyt home")
	}
	dir := filepath.Join(dwytHome, "objects")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("rawstore: create %s: %w", dir, err)
	}
	return &Store{dir: dir}, nil
}

// Dir is the on-disk root of the store.
func (s *Store) Dir() string { return s.dir }

// ID derives the object identifier for a payload. It is the first 24 hex
// characters of the SHA-256 digest: short enough to paste into a note, long
// enough that a collision is not a practical concern for a local store.
func ID(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])[:24]
}

// ParseRef extracts the object ID from a reference. It accepts the full
// `dwyt://objects/<id>` form and a bare ID, because agents paste both.
func ParseRef(ref string) (string, error) {
	// Strip markdown decoration before the scheme, because agents routinely
	// paste the reference inside backticks or angle brackets.
	id := strings.Trim(strings.TrimSpace(ref), "`<>")
	id = strings.TrimPrefix(id, URIScheme)
	id = strings.Trim(id, "`<>")
	if id == "" {
		return "", fmt.Errorf("rawstore: empty object reference")
	}
	for _, r := range id {
		isHex := (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')
		if !isHex {
			return "", fmt.Errorf("rawstore: invalid object reference %q", ref)
		}
	}
	return strings.ToLower(id), nil
}

func (s *Store) objectPath(id string) string { return filepath.Join(s.dir, id) }
func (s *Store) metaPath(id string) string   { return filepath.Join(s.dir, id+".json") }

// PutOptions tunes a write.
type PutOptions struct {
	// Kind labels the object for retention ("tool_output", "build", "test",
	// "debug_dump"). The housekeeper maps it to a TTL class.
	Kind string
	// Label is a short human description shown in the dashboard.
	Label string
	// TTL, when non-zero, sets the expiry deadline.
	TTL time.Duration
}

// Put stores content and returns its metadata. Storing the same bytes twice is
// a no-op for the payload; only the metadata TTL is refreshed, so re-observing
// an object extends its life instead of orphaning a second copy.
func (s *Store) Put(content string, opts PutOptions) (Meta, error) {
	if content == "" {
		return Meta{}, fmt.Errorf("rawstore: refusing to store empty content")
	}
	id := ID(content)
	now := time.Now()

	meta := Meta{
		ID:        id,
		Bytes:     int64(len(content)),
		Tokens:    estimateTokens(content),
		Kind:      opts.Kind,
		Label:     opts.Label,
		CreatedAt: now,
	}
	if opts.TTL > 0 {
		meta.ExpiresAt = now.Add(opts.TTL)
	}

	// Preserve the original creation time and access stats when the object is
	// already present: it is the same evidence, observed again.
	if existing, err := s.readMeta(id); err == nil {
		meta.CreatedAt = existing.CreatedAt
		meta.AccessCount = existing.AccessCount
		meta.LastAccessed = existing.LastAccessed
		if meta.Label == "" {
			meta.Label = existing.Label
		}
		if meta.Kind == "" {
			meta.Kind = existing.Kind
		}
	}

	if _, err := os.Stat(s.objectPath(id)); err != nil {
		if err := writeFileAtomic(s.objectPath(id), []byte(content), 0644); err != nil {
			return Meta{}, fmt.Errorf("rawstore: write object: %w", err)
		}
	}
	if err := s.writeMeta(meta); err != nil {
		return Meta{}, err
	}
	return meta, nil
}

// Get reads an object by reference, bumping its access counters. An expired
// object is reported as not found even if the file is still on disk (the
// housekeeper prunes lazily), so behaviour matches the stated TTL.
func (s *Store) Get(ref string) (string, Meta, error) {
	id, err := ParseRef(ref)
	if err != nil {
		return "", Meta{}, err
	}
	meta, err := s.readMeta(id)
	if err != nil {
		// An object without metadata is still readable: older writes and
		// manual copies should not become unreadable. Synthesize the metadata.
		data, readErr := os.ReadFile(s.objectPath(id))
		if readErr != nil {
			return "", Meta{}, ErrNotFound
		}
		return string(data), Meta{ID: id, Bytes: int64(len(data)), Tokens: estimateTokens(string(data))}, nil
	}
	if meta.Expired(time.Now()) {
		return "", meta, ErrNotFound
	}
	data, err := os.ReadFile(s.objectPath(id))
	if err != nil {
		return "", meta, ErrNotFound
	}
	meta.AccessCount++
	meta.LastAccessed = time.Now()
	// A failed stats update must not fail the read: the caller wants the
	// evidence, not the bookkeeping.
	_ = s.writeMeta(meta)
	return string(data), meta, nil
}

// Stat returns metadata without reading the payload and without counting an
// access. Used by the dashboard and the housekeeper.
func (s *Store) Stat(ref string) (Meta, error) {
	id, err := ParseRef(ref)
	if err != nil {
		return Meta{}, err
	}
	meta, err := s.readMeta(id)
	if err != nil {
		return Meta{}, ErrNotFound
	}
	return meta, nil
}

// List returns metadata for every object, newest first.
func (s *Store) List() ([]Meta, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []Meta
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		id := strings.TrimSuffix(e.Name(), ".json")
		meta, err := s.readMeta(id)
		if err != nil {
			continue
		}
		out = append(out, meta)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out, nil
}

// Delete removes an object and its metadata. A missing object is not an error:
// deletion is idempotent so the housekeeper can retry safely.
func (s *Store) Delete(ref string) error {
	id, err := ParseRef(ref)
	if err != nil {
		return err
	}
	if err := os.Remove(s.objectPath(id)); err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := os.Remove(s.metaPath(id)); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// PruneResult reports what a prune pass did.
type PruneResult struct {
	Expired    int   `json:"expired"`
	Orphans    int   `json:"orphans"`
	BytesFreed int64 `json:"bytes_freed"`
	Kept       int   `json:"kept"`
	KeptBytes  int64 `json:"kept_bytes"`
}

// Prune removes expired objects and orphans. An "orphan" is a payload with no
// metadata *and* whose ID is not referenced by the supplied referenced set —
// the housekeeper passes the references it found in the vault so an object
// still cited by a note is never removed for lack of a sidecar (spec §29:
// "do not delete before validating the reference").
func (s *Store) Prune(referenced map[string]bool, now time.Time) (PruneResult, error) {
	res := PruneResult{}
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return res, nil
		}
		return res, err
	}

	payloads := map[string]int64{}
	metas := map[string]Meta{}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if strings.HasSuffix(e.Name(), ".json") {
			id := strings.TrimSuffix(e.Name(), ".json")
			if meta, err := s.readMeta(id); err == nil {
				metas[id] = meta
			}
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		payloads[e.Name()] = info.Size()
	}

	for id, size := range payloads {
		meta, hasMeta := metas[id]
		switch {
		case hasMeta && meta.Expired(now) && !referenced[id]:
			if err := s.Delete(id); err == nil {
				res.Expired++
				res.BytesFreed += size
				continue
			}
		case !hasMeta && !referenced[id]:
			if err := s.Delete(id); err == nil {
				res.Orphans++
				res.BytesFreed += size
				continue
			}
		}
		res.Kept++
		res.KeptBytes += size
	}

	// Drop metadata whose payload is gone: it can only mislead.
	for id := range metas {
		if _, ok := payloads[id]; !ok {
			_ = os.Remove(s.metaPath(id))
		}
	}
	return res, nil
}

// Usage reports the store footprint for the dashboard.
func (s *Store) Usage() (count int, bytes int64, err error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, 0, nil
		}
		return 0, 0, err
	}
	for _, e := range entries {
		if e.IsDir() || strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		count++
		bytes += info.Size()
	}
	return count, bytes, nil
}

func (s *Store) readMeta(id string) (Meta, error) {
	data, err := os.ReadFile(s.metaPath(id))
	if err != nil {
		return Meta{}, err
	}
	var meta Meta
	if err := json.Unmarshal(data, &meta); err != nil {
		return Meta{}, err
	}
	if meta.ID == "" {
		meta.ID = id
	}
	return meta, nil
}

func (s *Store) writeMeta(meta Meta) error {
	data, err := json.MarshalIndent(&meta, "", "  ")
	if err != nil {
		return fmt.Errorf("rawstore: encode metadata: %w", err)
	}
	return writeFileAtomic(s.metaPath(meta.ID), append(data, '\n'), 0644)
}

// writeFileAtomic writes via a temp file + rename so a concurrent reader never
// observes a partial object.
func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".rawstore-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() {
		if _, statErr := os.Stat(tmpName); statErr == nil {
			os.Remove(tmpName)
		}
	}()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, perm); err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		// Cross-device fallback; the temp file is always in the same dir so
		// this is purely defensive.
		return os.WriteFile(path, data, perm)
	}
	return nil
}

// estimateTokens mirrors contextopt.EstimateTokens. It is duplicated (four
// lines) rather than imported to keep rawstore free of a dependency on the
// optimizer: the store is also used by the housekeeper and the installer.
func estimateTokens(content string) int {
	if content == "" {
		return 0
	}
	byChars := (len(content) + 3) / 4
	byWords := len(strings.Fields(content))
	if byWords > byChars {
		return byWords
	}
	return byChars
}
