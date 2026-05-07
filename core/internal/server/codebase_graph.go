package server

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/fvmoraes/dwyt/internal/db"
)

func (ds *DashboardServer) codebaseGraphStats(projectPath string) (nodes, edges int) {
	if projectPath == "" {
		return 0, 0
	}
	if ds.Store != nil {
		if pj, err := ds.Store.GetProjectByPath(projectPath); err == nil && pj.IndexedAt != nil && (pj.Nodes > 0 || pj.Edges > 0) {
			return pj.Nodes, pj.Edges
		}
	}

	nodes, edges = countCodebaseGraph(ds.DwytHome, projectPath)
	if nodes <= 0 && edges <= 0 {
		return 0, 0
	}
	if ds.Store != nil {
		if err := ds.Store.TouchProject(projectPath); err == nil {
			_ = ds.Store.MarkIndexed(projectPath, nodes, edges)
		}
	}
	return nodes, edges
}

func countCodebaseGraph(dwytHome, projectPath string) (nodes, edges int) {
	if nodes, edges := countCodebaseGraphSQLite(dwytHome, projectPath); nodes > 0 || edges > 0 {
		return nodes, edges
	}
	return countCodebaseGraphJSON(dwytHome, projectPath)
}

func countCodebaseGraphSQLite(dwytHome, projectPath string) (nodes, edges int) {
	for _, path := range codebaseGraphDBCandidates(dwytHome, projectPath) {
		if nodes, edges, ok := countCodebaseGraphSQLiteFile(path, projectPath); ok {
			return nodes, edges
		}
	}
	return 0, 0
}

func codebaseGraphDBCandidates(dwytHome, projectPath string) []string {
	codebaseDir := filepath.Join(dwytHome, "codebase")
	seen := map[string]bool{}
	var candidates []string
	add := func(path string) {
		if path == "" || seen[path] {
			return
		}
		seen[path] = true
		candidates = append(candidates, path)
	}

	add(filepath.Join(codebaseDir, codebaseProjectName(projectPath)+".db"))
	add(filepath.Join(codebaseDir, db.HashPath(projectPath)+".db"))

	entries, err := os.ReadDir(codebaseDir)
	if err != nil {
		return candidates
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".db") || strings.HasPrefix(entry.Name(), "_") {
			continue
		}
		add(filepath.Join(codebaseDir, entry.Name()))
	}
	return candidates
}

func codebaseProjectName(projectPath string) string {
	abs, err := filepath.Abs(projectPath)
	if err != nil {
		abs = projectPath
	}
	abs = filepath.Clean(abs)
	name := strings.NewReplacer("/", "-", "\\", "-", ":", "").Replace(abs)
	return strings.Trim(name, "-")
}

func countCodebaseGraphSQLiteFile(path, projectPath string) (nodes, edges int, ok bool) {
	if _, err := os.Stat(path); err != nil {
		return 0, 0, false
	}
	conn, err := sql.Open("sqlite", path+"?_busy_timeout=1000")
	if err != nil {
		return 0, 0, false
	}
	defer conn.Close()

	project, ok := codebaseProjectInSQLite(conn, projectPath)
	if !ok {
		return 0, 0, false
	}
	if err := conn.QueryRow(`SELECT COUNT(*) FROM nodes WHERE project = ?`, project).Scan(&nodes); err != nil {
		return 0, 0, false
	}
	if err := conn.QueryRow(`SELECT COUNT(*) FROM edges WHERE project = ?`, project).Scan(&edges); err != nil {
		return 0, 0, false
	}
	return nodes, edges, true
}

func codebaseProjectInSQLite(conn *sql.DB, projectPath string) (string, bool) {
	rows, err := conn.Query(`SELECT name, root_path FROM projects`)
	if err != nil {
		return "", false
	}
	defer rows.Close()

	for rows.Next() {
		var name, rootPath string
		if err := rows.Scan(&name, &rootPath); err != nil {
			continue
		}
		if sameCleanPath(rootPath, projectPath) || name == codebaseProjectName(projectPath) {
			return name, true
		}
	}
	return "", false
}

func sameCleanPath(a, b string) bool {
	aa, err := filepath.Abs(a)
	if err != nil {
		aa = a
	}
	bb, err := filepath.Abs(b)
	if err != nil {
		bb = b
	}
	return filepath.Clean(aa) == filepath.Clean(bb)
}

func countCodebaseGraphJSON(dwytHome, projectPath string) (nodes, edges int) {
	hash := db.HashPath(projectPath)
	cacheDir := filepath.Join(dwytHome, "codebase", hash)
	if _, err := os.Stat(cacheDir); err != nil {
		return 0, 0
	}

	filepath.Walk(cacheDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".json") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		var doc map[string]interface{}
		if json.Unmarshal(data, &doc) == nil {
			if n, ok := doc["nodes"]; ok {
				switch v := n.(type) {
				case float64:
					nodes += int(v)
				case []interface{}:
					nodes += len(v)
				}
			}
			if e, ok := doc["edges"]; ok {
				switch v := e.(type) {
				case float64:
					edges += int(v)
				case []interface{}:
					edges += len(v)
				}
			}
		}
		return nil
	})
	return nodes, edges
}
