package db

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

type Project struct {
	ID        string    `json:"id"`
	Path      string    `json:"path"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
	LastOpen  time.Time `json:"last_open"`
	IndexedAt *time.Time `json:"indexed_at,omitempty"`
	Nodes     int       `json:"nodes"`
	Edges     int       `json:"edges"`
}

type Store struct {
	db *sql.DB
}

func New(path string) (*Store, error) {
	dir := filepath.Dir(path)
	os.MkdirAll(dir, 0755)

	db, err := sql.Open("sqlite", path+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("sqlite open: %w", err)
	}

	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) migrate() error {
	if _, err := s.db.Exec(`
		CREATE TABLE IF NOT EXISTS projects (
			id         TEXT PRIMARY KEY,
			path       TEXT NOT NULL UNIQUE,
			name       TEXT NOT NULL,
			created_at TEXT NOT NULL,
			last_open  TEXT NOT NULL,
			indexed_at TEXT,
			nodes      INTEGER DEFAULT 0,
			edges      INTEGER DEFAULT 0,
			removed    INTEGER DEFAULT 0
		);
		CREATE TABLE IF NOT EXISTS config (
			key   TEXT PRIMARY KEY,
			value TEXT NOT NULL
		);
		CREATE TABLE IF NOT EXISTS headroom_savings (
			project_id TEXT PRIMARY KEY,
			tokens     INTEGER DEFAULT 0
		);
		CREATE TABLE IF NOT EXISTS metric_events (
			id         INTEGER PRIMARY KEY AUTOINCREMENT,
			project_id TEXT NOT NULL,
			tool       TEXT NOT NULL,
			metric     TEXT NOT NULL,
			delta      INTEGER NOT NULL,
			ts         INTEGER NOT NULL
		);
		CREATE INDEX IF NOT EXISTS idx_metric_events ON metric_events(project_id, ts);
		CREATE TABLE IF NOT EXISTS metric_cursor (
			project_id TEXT NOT NULL,
			tool       TEXT NOT NULL,
			metric     TEXT NOT NULL,
			cumulative INTEGER NOT NULL,
			PRIMARY KEY (project_id, tool, metric)
		);
	`); err != nil {
		return err
	}

	// Older databases predate the `removed` column. SQLite has no
	// ADD COLUMN IF NOT EXISTS, so add it and ignore the duplicate error.
	if _, err := s.db.Exec(`ALTER TABLE projects ADD COLUMN removed INTEGER DEFAULT 0`); err != nil &&
		!strings.Contains(err.Error(), "duplicate column") {
		return err
	}
	return nil
}

func HashPath(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	abs = filepath.Clean(abs)
	h := sha256.Sum256([]byte(abs))
	return hex.EncodeToString(h[:])[:12]
}

func (s *Store) UpsertProject(path string) (*Project, error) {
	id := HashPath(path)
	name := filepath.Base(path)
	now := time.Now().UTC().Format(time.RFC3339)

	_, err := s.db.Exec(`
		INSERT INTO projects (id, path, name, created_at, last_open)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(path) DO UPDATE SET last_open = ?, name = ?, removed = 0
	`, id, path, name, now, now, now, name)
	if err != nil {
		return nil, err
	}

	return s.GetProject(id)
}

func (s *Store) GetProject(id string) (*Project, error) {
	p := &Project{}
	var indexedAt sql.NullString
	var createdAt, lastOpen string
	err := s.db.QueryRow(
		`SELECT id, path, name, created_at, last_open, indexed_at, nodes, edges FROM projects WHERE id = ?`,
		id,
	).Scan(&p.ID, &p.Path, &p.Name, &createdAt, &lastOpen, &indexedAt, &p.Nodes, &p.Edges)
	if err != nil {
		return nil, err
	}
	p.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	p.LastOpen, _ = time.Parse(time.RFC3339, lastOpen)
	if indexedAt.Valid {
		t, _ := time.Parse(time.RFC3339, indexedAt.String)
		p.IndexedAt = &t
	}
	return p, nil
}

func (s *Store) GetProjectByPath(path string) (*Project, error) {
	id := HashPath(path)
	return s.GetProject(id)
}

func (s *Store) TouchProject(path string) error {
	id := HashPath(path)
	name := filepath.Base(path)
	now := time.Now().UTC().Format(time.RFC3339)

	// If project exists, update last_open and name; otherwise insert.
	// Re-touching a soft-removed project restores it (removed = 0).
	_, err := s.db.Exec(`
		INSERT INTO projects (id, path, name, created_at, last_open)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(path) DO UPDATE SET last_open = ?, name = ?, removed = 0
	`, id, path, name, now, now, now, name)
	return err
}

// RemoveProject performs a logical (soft) delete: the project disappears
// from the active list but its row and all ~/.dwyt data stay intact, so a
// later re-add restores the full history automatically.
func (s *Store) RemoveProject(path string) error {
	_, err := s.db.Exec(`UPDATE projects SET removed = 1 WHERE path = ?`, path)
	return err
}

// AddHeadroomSavings attributes a token-savings delta to a project. Headroom
// runs as a single shared proxy, so deltas are credited to whichever project
// is active when they are observed.
func (s *Store) AddHeadroomSavings(projectID string, delta int64) error {
	if delta <= 0 {
		return nil
	}
	_, err := s.db.Exec(`
		INSERT INTO headroom_savings (project_id, tokens) VALUES (?, ?)
		ON CONFLICT(project_id) DO UPDATE SET tokens = tokens + ?
	`, projectID, delta, delta)
	return err
}

// GetHeadroomSavings returns the tokens attributed to a single project.
func (s *Store) GetHeadroomSavings(projectID string) (int64, error) {
	var v int64
	err := s.db.QueryRow(`SELECT tokens FROM headroom_savings WHERE project_id = ?`, projectID).Scan(&v)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	return v, err
}

// RecordMetricDeltas tracks the growth of a tool's cumulative metrics for a
// project as timestamped events, enabling time-windowed queries (last hour,
// 24h, 7d, ...). Every metric (tokens saved, commands, requests, graph nodes,
// vault files, ...) is recorded the same way so the dashboard can scope the
// whole card to a window. It is idempotent: only positive growth since the
// last observation is stored, and a cumulative drop (reset/reindex) rebases
// the cursor without emitting a bogus event.
func (s *Store) RecordMetricDeltas(projectID, tool string, cumulative map[string]int64) error {
	now := time.Now().Unix()
	for metric, cur := range cumulative {
		var prev int64
		err := s.db.QueryRow(
			`SELECT cumulative FROM metric_cursor WHERE project_id = ? AND tool = ? AND metric = ?`,
			projectID, tool, metric,
		).Scan(&prev)
		if err == sql.ErrNoRows {
			if _, err := s.db.Exec(
				`INSERT INTO metric_cursor (project_id, tool, metric, cumulative) VALUES (?, ?, ?, ?)`,
				projectID, tool, metric, cur,
			); err != nil {
				return err
			}
			continue
		}
		if err != nil {
			return err
		}
		if cur == prev {
			continue
		}
		if cur < prev {
			// Counter dropped (reset/reindex) — rebase, attribute nothing.
			if _, err := s.db.Exec(
				`UPDATE metric_cursor SET cumulative = ? WHERE project_id = ? AND tool = ? AND metric = ?`,
				cur, projectID, tool, metric,
			); err != nil {
				return err
			}
			continue
		}
		if _, err := s.db.Exec(
			`INSERT INTO metric_events (project_id, tool, metric, delta, ts) VALUES (?, ?, ?, ?, ?)`,
			projectID, tool, metric, cur-prev, now,
		); err != nil {
			return err
		}
		if _, err := s.db.Exec(
			`UPDATE metric_cursor SET cumulative = ? WHERE project_id = ? AND tool = ? AND metric = ?`,
			cur, projectID, tool, metric,
		); err != nil {
			return err
		}
	}
	return nil
}

// SumMetricsByTool returns the metric growth for a project since the given unix
// timestamp, grouped as tool -> metric -> summed delta.
func (s *Store) SumMetricsByTool(projectID string, sinceUnix int64) (map[string]map[string]int64, error) {
	rows, err := s.db.Query(
		`SELECT tool, metric, COALESCE(SUM(delta), 0)
		 FROM metric_events WHERE project_id = ? AND ts >= ? GROUP BY tool, metric`,
		projectID, sinceUnix,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make(map[string]map[string]int64)
	for rows.Next() {
		var tool, metric string
		var sum int64
		if err := rows.Scan(&tool, &metric, &sum); err != nil {
			return nil, err
		}
		if out[tool] == nil {
			out[tool] = make(map[string]int64)
		}
		out[tool][metric] = sum
	}
	return out, nil
}

// PruneMetricEvents drops events older than the given unix timestamp. The
// dashboard never queries beyond the largest window (7 days), so old rows are
// pure dead weight.
func (s *Store) PruneMetricEvents(olderThanUnix int64) error {
	_, err := s.db.Exec(`DELETE FROM metric_events WHERE ts < ?`, olderThanUnix)
	return err
}

func (s *Store) MarkIndexed(path string, nodes, edges int) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.Exec(
		`UPDATE projects SET indexed_at = ?, nodes = ?, edges = ? WHERE path = ?`,
		now, nodes, edges, path,
	)
	return err
}

func (s *Store) ListProjects() ([]*Project, error) {
	rows, err := s.db.Query(
		`SELECT id, path, name, created_at, last_open, indexed_at, nodes, edges FROM projects WHERE removed = 0 ORDER BY last_open DESC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var projects []*Project
	for rows.Next() {
		p := &Project{}
		var indexedAt sql.NullString
		var createdAt, lastOpen string
		if err := rows.Scan(&p.ID, &p.Path, &p.Name, &createdAt, &lastOpen, &indexedAt, &p.Nodes, &p.Edges); err != nil {
			return nil, err
		}
		p.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		p.LastOpen, _ = time.Parse(time.RFC3339, lastOpen)
		if indexedAt.Valid {
			t, _ := time.Parse(time.RFC3339, indexedAt.String)
			p.IndexedAt = &t
		}
		projects = append(projects, p)
	}
	return projects, nil
}

func (s *Store) SetConfig(key, value string) error {
	_, err := s.db.Exec(
		`INSERT INTO config (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = ?`,
		key, value, value,
	)
	return err
}

func (s *Store) GetConfig(key string) (string, error) {
	var value string
	err := s.db.QueryRow(`SELECT value FROM config WHERE key = ?`, key).Scan(&value)
	return value, err
}

func (s *Store) Close() error {
	return s.db.Close()
}
