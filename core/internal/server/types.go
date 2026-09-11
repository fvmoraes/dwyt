package server

import (
	"context"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/fvmoraes/dwyt/internal/brain"
	"github.com/fvmoraes/dwyt/internal/db"
	"github.com/fvmoraes/dwyt/internal/dwytconfig"
	"github.com/fvmoraes/dwyt/internal/housekeeper"
	"github.com/fvmoraes/dwyt/internal/optimizer"
	"github.com/fvmoraes/dwyt/internal/procman"
	"github.com/fvmoraes/dwyt/internal/state"
	"github.com/fvmoraes/dwyt/internal/telemetry"
	"github.com/fvmoraes/dwyt/internal/toolsource"
)

type Config struct {
	Configured  bool                            `json:"configured"`
	Tools       []string                        `json:"tools"`
	Clients     []string                        `json:"clients"`
	Ias         []string                        `json:"ias"`
	Providers   []string                        `json:"providers"`
	ToolSources map[string]toolsource.Selection `json:"tool_sources,omitempty"`
	ProjectPath string                          `json:"project_path"`
	LastSetup   string                          `json:"last_setup"`
}

type FsNode struct {
	Name     string   `json:"name"`
	Path     string   `json:"path"`
	IsDir    bool     `json:"is_dir"`
	Children []FsNode `json:"children,omitempty"`
}

type ToolDetail struct {
	TokensSaved       int64    `json:"tokens_saved"`
	TokensUsed        int64    `json:"tokens_used,omitempty"`
	WithoutDWYTTokens int64    `json:"without_dwyt_tokens,omitempty"`
	WithDWYTTokens    int64    `json:"with_dwyt_tokens,omitempty"`
	UptimeSecs        int64    `json:"uptime_secs"`
	UptimeLabel       string   `json:"uptime_label"`
	Repos             []string `json:"repos"`
	Requests          int64    `json:"requests,omitempty"`
	CompressionPct    float64  `json:"compression_pct,omitempty"`
	ProxyPort         int      `json:"proxy_port,omitempty"`
	TotalCommands     int64    `json:"total_commands,omitempty"`
	PctSaved          float64  `json:"pct_saved,omitempty"`
	IndexedNodes      int64    `json:"indexed_nodes,omitempty"`
	IndexedEdges      int64    `json:"indexed_edges,omitempty"`
	MemoryCount       int      `json:"memory_count,omitempty"`
	MemoryBytes       int64    `json:"memory_bytes,omitempty"`
	LastUpdated       string   `json:"last_updated,omitempty"`
	SavingsBasis      string   `json:"savings_basis,omitempty"`
	EstimationSource  string   `json:"estimation_source,omitempty"`
	Scope             string   `json:"scope,omitempty"` // "project" | "global"
}

type DashboardServer struct {
	Port            int
	DwytBin         string
	DwytHome        string
	ReleaseVersion  string
	StartCwd        string
	DefaultProject  string
	Store           *db.Store
	ProjectObsidian *brain.ProjectObsidian
	ProcMan         *procman.ProcessManager
	RuntimeState    *state.RuntimeState
	// Optimizer is the DWYT v5 Context Optimizer. It owns the efficiency policy
	// (budgets, retrieval ladder, output profiles, cache guidance, raw store)
	// so instruction files can stay small and stable.
	Optimizer *optimizer.Optimizer
	// Housekeeper enforces Brain retention: the 100-session limit, TTLs, stale
	// detection and raw pruning, always promoting reusable knowledge first.
	Housekeeper *housekeeper.Housekeeper
	// SvcCtl is the service reconciler: the single watchdog that adopts
	// healthy instances, publishes lifecycle states and recovers dead
	// auto-start services with bounded backoff.
	SvcCtl *ServiceReconciler `json:"-"`
	// Telemetry is the request and task ledger behind cost-per-completed-task.
	Telemetry *telemetry.Store
	// V5Config is the consolidated DWYT v5 configuration (spec §57).
	V5Config              dwytconfig.Config
	HeadroomPort          int // effective port used by health/status/client wrappers
	HeadroomRequestedPort int // configured port retained across transient fallbacks
	headroomMu            sync.RWMutex
	projectMu             sync.RWMutex
	sseClients            map[chan string]bool
	sseMu                 sync.Mutex
	installMu             sync.Mutex
	installStatus         map[string]string
	installing            bool
	indexProject          string
	codebaseProgress      struct {
		mu       sync.Mutex
		indexing bool
		progress string
		error    string
	}
	codebaseIndexCancel context.CancelFunc
	headroomStartMu     sync.Mutex
	savingsMu           sync.Mutex
	// Dashboard-first startup: non-critical boot work runs as ordered
	// background tasks only after http.Server has entered its accept loop.
	// startupTasksOverride replaces the real task list in tests; startupDone
	// closes when the loop finishes.
	startupTasksOverride []startupTask
	startupDone          <-chan struct{}
	startupCancel        context.CancelFunc
	startupStarted       time.Time

	// lifecycleMu serializes Start, post-bind service activation and Shutdown.
	// All WaitGroup additions happen while Start holds this lock and before the
	// server becomes externally observable, so Shutdown can wait safely.
	lifecycleMu       sync.Mutex
	lifecycleCtx      context.Context
	lifecycleCancel   context.CancelFunc
	httpServer        *http.Server
	lifecycleWG       sync.WaitGroup
	lifecycleDone     <-chan struct{}
	lifecycleStarted  bool
	lifecycleStopping bool
	svcCtlStarted     bool
	shutdownRequested bool
	shutdownStarted   bool
	shutdownDone      chan struct{}
	shutdownErr       error

	// vaultMigrationMu is a filesystem lease for structural vault changes.
	// HTTP/MCP vault operations hold a read lease; startup/manual migrations
	// hold the write lease. vaultMigrating enables fail-fast 503 responses.
	vaultMigrationMu sync.RWMutex
	vaultMigrating   atomic.Bool
	// hasSetupConfig/setupConfig mirror the persisted setup so background
	// tasks (MCP config sync) can act on it after New() returned.
	hasSetupConfig bool
	setupConfig    Config
}
