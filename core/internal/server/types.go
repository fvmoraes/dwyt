package server

import (
	"context"
	"sync"

	"github.com/fvmoraes/dwyt/internal/brain"
	"github.com/fvmoraes/dwyt/internal/db"
	"github.com/fvmoraes/dwyt/internal/procman"
	"github.com/fvmoraes/dwyt/internal/state"
)

type Config struct {
	Configured  bool     `json:"configured"`
	Tools       []string `json:"tools"`
	Clients     []string `json:"clients"`
	Ias         []string `json:"ias"`
	Providers   []string `json:"providers"`
	ProjectPath string   `json:"project_path"`
	LastSetup   string   `json:"last_setup"`
}

type FsNode struct {
	Name     string   `json:"name"`
	Path     string   `json:"path"`
	IsDir    bool     `json:"is_dir"`
	Children []FsNode `json:"children,omitempty"`
}

type ToolDetail struct {
	TokensSaved    int64    `json:"tokens_saved"`
	UptimeSecs     int64    `json:"uptime_secs"`
	UptimeLabel    string   `json:"uptime_label"`
	Repos          []string `json:"repos"`
	Requests       int64    `json:"requests,omitempty"`
	CompressionPct float64  `json:"compression_pct,omitempty"`
	ProxyPort      int      `json:"proxy_port,omitempty"`
	TotalCommands  int64    `json:"total_commands,omitempty"`
	PctSaved       float64  `json:"pct_saved,omitempty"`
	IndexedNodes   int64    `json:"indexed_nodes,omitempty"`
	MemoryCount    int      `json:"memory_count,omitempty"`
	LastUpdated    string   `json:"last_updated,omitempty"`
}

type DashboardServer struct {
	Port           int
	DwytBin        string
	DwytHome       string
	StartCwd       string
	DefaultProject string
	Store          *db.Store
	ProjectObsidian   *brain.ProjectObsidian
	ProcMan        *procman.ProcessManager
	RuntimeState   *state.RuntimeState
	HeadroomPort   int
	projectMu      sync.RWMutex
	sseClients     map[chan string]bool
	sseMu          sync.Mutex
	installMu      sync.Mutex
	installStatus  map[string]string
	installing     bool
	indexProject   string
	codebaseProgress struct {
		mu       sync.Mutex
		indexing bool
		progress string
		error    string
	}
	codebaseIndexCancel context.CancelFunc
	headroomStartMu     sync.Mutex
}
