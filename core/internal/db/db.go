package db

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
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
	_, err := s.db.Exec(`
		CREATE TABLE IF NOT EXISTS projects (
			id         TEXT PRIMARY KEY,
			path       TEXT NOT NULL UNIQUE,
			name       TEXT NOT NULL,
			created_at TEXT NOT NULL,
			last_open  TEXT NOT NULL,
			indexed_at TEXT,
			nodes      INTEGER DEFAULT 0,
			edges      INTEGER DEFAULT 0
		);
		CREATE TABLE IF NOT EXISTS config (
			key   TEXT PRIMARY KEY,
			value TEXT NOT NULL
		);
	`)
	return err
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
		ON CONFLICT(path) DO UPDATE SET last_open = ?, name = ?
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

	// If project exists, update last_open and name; otherwise insert
	_, err := s.db.Exec(`
		INSERT INTO projects (id, path, name, created_at, last_open)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(path) DO UPDATE SET last_open = ?, name = ?
	`, id, path, name, now, now, now, name)
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
		`SELECT id, path, name, created_at, last_open, indexed_at, nodes, edges FROM projects ORDER BY last_open DESC`,
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
