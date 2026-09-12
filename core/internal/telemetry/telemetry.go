// Package telemetry records what a request and a task actually cost (spec §52,
// §53, §55, §56).
//
// The whole point is to make the savings claim falsifiable. Two rules follow
// from that:
//
//  1. **Observed and estimated never mix.** Every stored figure carries whether
//     the provider reported it or DWYT computed it, and a value the provider did
//     not report stays NULL rather than becoming 0. Storing an unknown cached
//     token count as zero would understate cache effectiveness and overstate
//     cost, which is exactly the misleading metric spec §52 forbids.
//
//  2. **Cost per *successfully completed* task is the headline.** Tokens per
//     request is easy to improve by failing faster, so the task ledger records
//     success, attempts and validation alongside the spend.
package telemetry

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

// RequestEvent is one LLM request (spec §53 llm_request_events).
//
// Pointer fields are "not reported". Keep them pointers all the way to the
// database so the NULL/0 distinction survives.
type RequestEvent struct {
	ID        string `json:"id"`
	ProjectID string `json:"project_id"`
	TaskID    string `json:"task_id,omitempty"`
	Provider  string `json:"provider,omitempty"`
	Model     string `json:"model,omitempty"`
	// Variant/Effort carry the model variant and reasoning-effort level the
	// agent reported via dwyt_report_usage. Empty means not reported.
	Variant string `json:"variant,omitempty"`
	Effort  string `json:"effort,omitempty"`
	Phase   string `json:"phase,omitempty"`

	InputTokens         *int `json:"input_tokens,omitempty"`
	UncachedInputTokens *int `json:"uncached_input_tokens,omitempty"`
	CachedInputTokens   *int `json:"cached_input_tokens,omitempty"`
	CacheWriteTokens    *int `json:"cache_write_tokens,omitempty"`
	OutputTokens        *int `json:"output_tokens,omitempty"`
	ReasoningTokens     *int `json:"reasoning_tokens,omitempty"`
	ToolTokens          *int `json:"tool_tokens,omitempty"`

	// ContextBefore/After measure what DWYT avoided sending.
	ContextBefore *int `json:"context_before_dwyt,omitempty"`
	ContextAfter  *int `json:"context_after_dwyt,omitempty"`
	// CompressionMetadataTokens is expected recovery overhead not included in
	// ContextAfter. Nil means it was not measured, while an explicit zero means
	// the request incurred no separate recovery cost.
	CompressionMetadataTokens *int `json:"compression_metadata_tokens,omitempty"`

	EstimatedCostUSD *float64 `json:"estimated_cost_usd,omitempty"`
	ActualCostUSD    *float64 `json:"actual_cost_usd,omitempty"`

	CacheKeyHash string `json:"cache_key_hash,omitempty"`
	PrefixHash   string `json:"prefix_hash,omitempty"`

	LatencyMS *int `json:"latency_ms,omitempty"`

	// Observed is retained for backward compatibility. Provenance is the
	// authoritative per-metric source label and is normalized on persistence.
	Observed   bool             `json:"observed"`
	Provenance MetricProvenance `json:"provenance,omitempty"`
	Timestamp  time.Time        `json:"ts"`
}

// TaskOutcome is the per-task ledger (spec §53 task_outcomes).
type TaskOutcome struct {
	TaskID      string     `json:"task_id"`
	ProjectID   string     `json:"project_id"`
	Success     bool       `json:"success"`
	TestsPass   bool       `json:"tests_pass"`
	Attempts    int        `json:"attempts"`
	TotalCost   float64    `json:"total_cost_usd"`
	TotalToken  int        `json:"total_tokens"`
	StartedAt   time.Time  `json:"started_at"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
}

// Store persists telemetry. It owns its own tables inside the existing DWYT
// SQLite database, so there is one file to back up and one migration path.
type Store struct {
	db *sql.DB
}

// New wraps an open database and ensures the schema exists.
func New(db *sql.DB) (*Store, error) {
	if db == nil {
		return nil, fmt.Errorf("telemetry: nil database")
	}
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) migrate() error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	// Keep an explicit migration ledger. CREATE TABLE IF NOT EXISTS alone does
	// not evolve databases created by a prior DWYT version, so version 2 below
	// adds its columns without dropping or rewriting historical events.
	if _, err := tx.Exec(`
		CREATE TABLE IF NOT EXISTS telemetry_schema_migrations (
			version    INTEGER PRIMARY KEY,
			name       TEXT NOT NULL,
			applied_at INTEGER NOT NULL
		);

		CREATE TABLE IF NOT EXISTS llm_request_events (
			id                          TEXT PRIMARY KEY,
			project_id                  TEXT NOT NULL,
			task_id                     TEXT,
			provider                    TEXT,
			model                       TEXT,
			phase                       TEXT,
			input_tokens                INTEGER,
			uncached_input_tokens       INTEGER,
			cached_input_tokens         INTEGER,
			cache_write_tokens          INTEGER,
			output_tokens               INTEGER,
			reasoning_tokens            INTEGER,
			tool_tokens                 INTEGER,
			context_before              INTEGER,
			context_after               INTEGER,
			compression_metadata_tokens INTEGER,
			estimated_cost_usd          REAL,
			actual_cost_usd             REAL,
			cache_key_hash              TEXT,
			prefix_hash                 TEXT,
			latency_ms                  INTEGER,
			observed                    INTEGER NOT NULL DEFAULT 0,
			metric_provenance           TEXT NOT NULL DEFAULT '{}',
			ts                          INTEGER NOT NULL
		);
		CREATE INDEX IF NOT EXISTS idx_llm_events_project_ts
			ON llm_request_events(project_id, ts);
		CREATE INDEX IF NOT EXISTS idx_llm_events_task
			ON llm_request_events(task_id);

		CREATE TABLE IF NOT EXISTS task_outcomes (
			task_id        TEXT PRIMARY KEY,
			project_id     TEXT NOT NULL,
			success        INTEGER NOT NULL DEFAULT 0,
			tests_pass     INTEGER NOT NULL DEFAULT 0,
			attempts       INTEGER NOT NULL DEFAULT 0,
			total_cost_usd REAL NOT NULL DEFAULT 0,
			total_tokens   INTEGER NOT NULL DEFAULT 0,
			started_at     INTEGER NOT NULL,
			completed_at   INTEGER
		);
		CREATE INDEX IF NOT EXISTS idx_task_outcomes_project
			ON task_outcomes(project_id, started_at);

		CREATE TABLE IF NOT EXISTS housekeeper_runs (
			id         INTEGER PRIMARY KEY AUTOINCREMENT,
			project_id TEXT NOT NULL,
			depth      TEXT NOT NULL,
			report     TEXT NOT NULL,
			ts         INTEGER NOT NULL
		);
		CREATE INDEX IF NOT EXISTS idx_housekeeper_runs
			ON housekeeper_runs(project_id, ts);
	`); err != nil {
		return err
	}

	now := time.Now().Unix()
	if _, err := tx.Exec(`INSERT OR IGNORE INTO telemetry_schema_migrations(version, name, applied_at) VALUES (1, 'initial request ledger', ?)`, now); err != nil {
		return err
	}
	if err := ensureRequestEventColumn(tx, "compression_metadata_tokens", "INTEGER"); err != nil {
		return err
	}
	if err := ensureRequestEventColumn(tx, "metric_provenance", "TEXT NOT NULL DEFAULT '{}'"); err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT OR IGNORE INTO telemetry_schema_migrations(version, name, applied_at) VALUES (2, 'metric provenance and compression metadata', ?)`, now); err != nil {
		return err
	}
	if err := ensureRequestEventColumn(tx, "variant", "TEXT"); err != nil {
		return err
	}
	if err := ensureRequestEventColumn(tx, "effort", "TEXT"); err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT OR IGNORE INTO telemetry_schema_migrations(version, name, applied_at) VALUES (3, 'model variant and effort', ?)`, now); err != nil {
		return err
	}
	return tx.Commit()
}

// ensureRequestEventColumn performs the additive half of a versioned migration
// for databases created before the column existed. Identifiers are constants
// controlled by this package, never caller input.
func ensureRequestEventColumn(tx *sql.Tx, column, definition string) error {
	rows, err := tx.Query(`PRAGMA table_info(llm_request_events)`)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var cid int
		var name, typ string
		var notNull, pk int
		var defaultValue interface{}
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &pk); err != nil {
			return err
		}
		if name == column {
			return nil
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	_, err = tx.Exec(`ALTER TABLE llm_request_events ADD COLUMN ` + column + ` ` + definition)
	return err
}

// RecordRequest stores one request event and folds its spend into the task
// ledger.
//
// The two writes are one transaction: a request counted without its cost landing
// in the task ledger would silently break cost-per-task, which is the metric the
// whole package exists to produce.
func (s *Store) RecordRequest(e RequestEvent) error {
	if e.ProjectID == "" {
		return fmt.Errorf("telemetry: project_id is required")
	}
	if e.Timestamp.IsZero() {
		e.Timestamp = time.Now()
	}
	if e.ID == "" {
		e.ID = eventID(e)
	}

	e.Provenance = normalizeRequestProvenance(e.Provenance, e.Observed, requestMetricPresence(e))
	provenanceJSON, err := json.Marshal(e.Provenance)
	if err != nil {
		return fmt.Errorf("telemetry: encode metric provenance: %w", err)
	}

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	_, err = tx.Exec(`
		INSERT OR REPLACE INTO llm_request_events (
			id, project_id, task_id, provider, model, variant, effort, phase,
			input_tokens, uncached_input_tokens, cached_input_tokens,
			cache_write_tokens, output_tokens, reasoning_tokens, tool_tokens,
			context_before, context_after, compression_metadata_tokens,
			estimated_cost_usd, actual_cost_usd,
			cache_key_hash, prefix_hash, latency_ms, observed, metric_provenance, ts
		) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		e.ID, e.ProjectID, nullString(e.TaskID), nullString(e.Provider),
		nullString(e.Model), nullString(e.Variant), nullString(e.Effort), nullString(e.Phase),
		nullInt(e.InputTokens), nullInt(e.UncachedInputTokens), nullInt(e.CachedInputTokens),
		nullInt(e.CacheWriteTokens), nullInt(e.OutputTokens), nullInt(e.ReasoningTokens),
		nullInt(e.ToolTokens), nullInt(e.ContextBefore), nullInt(e.ContextAfter),
		nullInt(e.CompressionMetadataTokens),
		nullFloat(e.EstimatedCostUSD), nullFloat(e.ActualCostUSD),
		nullString(e.CacheKeyHash), nullString(e.PrefixHash), nullInt(e.LatencyMS),
		boolToInt(e.Observed), string(provenanceJSON), e.Timestamp.Unix(),
	)
	if err != nil {
		return err
	}

	if e.TaskID != "" {
		cost := 0.0
		switch {
		case e.ActualCostUSD != nil:
			cost = *e.ActualCostUSD
		case e.EstimatedCostUSD != nil:
			cost = *e.EstimatedCostUSD
		}
		tokens := valueOr(e.InputTokens) + valueOr(e.OutputTokens)
		if _, err := tx.Exec(`
			INSERT INTO task_outcomes (task_id, project_id, attempts, total_cost_usd, total_tokens, started_at)
			VALUES (?, ?, 1, ?, ?, ?)
			ON CONFLICT(task_id) DO UPDATE SET
				attempts       = attempts + 1,
				total_cost_usd = total_cost_usd + ?,
				total_tokens   = total_tokens + ?`,
			e.TaskID, e.ProjectID, cost, tokens, e.Timestamp.Unix(), cost, tokens,
		); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// CompleteTask records the outcome of a task.
func (s *Store) CompleteTask(taskID, projectID string, success, testsPass bool, at time.Time) error {
	if taskID == "" || projectID == "" {
		return fmt.Errorf("telemetry: task_id and project_id are required")
	}
	if at.IsZero() {
		at = time.Now()
	}
	_, err := s.db.Exec(`
		INSERT INTO task_outcomes (task_id, project_id, success, tests_pass, attempts, started_at, completed_at)
		VALUES (?, ?, ?, ?, 0, ?, ?)
		ON CONFLICT(task_id) DO UPDATE SET
			success      = ?,
			tests_pass   = ?,
			completed_at = ?`,
		taskID, projectID, boolToInt(success), boolToInt(testsPass), at.Unix(), at.Unix(),
		boolToInt(success), boolToInt(testsPass), at.Unix(),
	)
	return err
}

// RecordHousekeeperRun stores a housekeeping report for the dashboard's health
// panel.
func (s *Store) RecordHousekeeperRun(projectID, depth string, report interface{}) error {
	data, err := json.Marshal(report)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(
		`INSERT INTO housekeeper_runs (project_id, depth, report, ts) VALUES (?, ?, ?, ?)`,
		projectID, depth, string(data), time.Now().Unix(),
	)
	return err
}

// Summary is the dashboard aggregate (spec §55, §56).
//
// Token and cost totals are split into observed and estimated because merging
// them would produce a number nobody could act on.
type Summary struct {
	Window string `json:"window"`

	Requests         int `json:"requests"`
	ObservedRequests int `json:"observed_requests"`

	InputTokens               int `json:"input_tokens"`
	CachedInputTokens         int `json:"cached_input_tokens"`
	UncachedInputTokens       int `json:"uncached_input_tokens"`
	CacheWriteTokens          int `json:"cache_write_tokens"`
	OutputTokens              int `json:"output_tokens"`
	ReasoningTokens           int `json:"reasoning_tokens"`
	ToolTokens                int `json:"tool_tokens"`
	LatencyMS                 int `json:"latency_ms"`
	CompressionMetadataTokens int `json:"compression_metadata_tokens"`

	// CacheHitPct is nil when no request reported cache numbers.
	CacheHitPct *float64 `json:"cache_hit_pct"`
	// ContextReductionPct is nil when no request reported before/after sizes.
	ContextReductionPct *float64 `json:"context_reduction_pct"`
	ContextBefore       int      `json:"context_before"`
	ContextAfter        int      `json:"context_after"`
	AvoidedTokens       int      `json:"avoided_tokens"`

	ObservedCostUSD  float64 `json:"observed_cost_usd"`
	EstimatedCostUSD float64 `json:"estimated_cost_usd"`

	Tasks          int `json:"tasks"`
	TasksSucceeded int `json:"tasks_succeeded"`
	// CompletionPct and CostPerCompletedTask are nil until at least one task has
	// finished; a rate over zero tasks is meaningless, not zero.
	CompletionPct          *float64 `json:"completion_pct"`
	CostPerCompletedTask   *float64 `json:"cost_per_completed_task"`
	TokensPerCompletedTask *float64 `json:"tokens_per_completed_task"`
	AvgAttempts            *float64 `json:"avg_attempts"`

	// Provenance labels every aggregate independently. A partial aggregate is
	// unsupported rather than a seemingly precise sum of only some requests.
	Provenance MetricProvenance `json:"provenance"`

	// Coverage tells the reader how much of this is measured rather than guessed.
	Coverage Coverage `json:"coverage"`
}

// Coverage describes how complete the data behind a Summary is.
type Coverage struct {
	InputReported               int `json:"input_reported_requests"`
	OutputReported              int `json:"output_reported_requests"`
	ToolReported                int `json:"tool_reported_requests"`
	LatencyReported             int `json:"latency_reported_requests"`
	CacheReported               int `json:"cache_reported_requests"`
	ContextReported             int `json:"context_reported_requests"`
	CompressionMetadataReported int `json:"compression_metadata_reported_requests"`
	CostReported                int `json:"cost_reported_requests"`
}

// Summarize aggregates a time window for a project.
func (s *Store) Summarize(projectID string, since time.Time, window string) (Summary, error) {
	sum := Summary{Window: window, Provenance: make(MetricProvenance)}
	rollups := newMetricRollups()
	rows, err := s.db.Query(`
		SELECT input_tokens, uncached_input_tokens, cached_input_tokens,
		       cache_write_tokens, output_tokens, reasoning_tokens, tool_tokens,
		       context_before, context_after, compression_metadata_tokens,
		       estimated_cost_usd, actual_cost_usd, latency_ms, observed, metric_provenance
		FROM llm_request_events
		WHERE project_id = ? AND ts >= ?`, projectID, since.Unix())
	if err != nil {
		return sum, err
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var input, uncached, cached, cacheWrite, output, reasoning, tool sql.NullInt64
		var before, after, compressionMetadata, latency sql.NullInt64
		var estimated, actual sql.NullFloat64
		var observed int
		var provenance sql.NullString
		if err := rows.Scan(&input, &uncached, &cached, &cacheWrite, &output,
			&reasoning, &tool, &before, &after, &compressionMetadata,
			&estimated, &actual, &latency, &observed, &provenance); err != nil {
			return sum, err
		}
		sum.Requests++
		if observed == 1 {
			sum.ObservedRequests++
		}
		rawProvenance := ""
		if provenance.Valid {
			rawProvenance = provenance.String
		}
		eventProvenance := decodeRequestProvenance(rawProvenance, observed == 1, map[string]bool{
			MetricInputTokens:               input.Valid,
			MetricUncachedInputTokens:       uncached.Valid,
			MetricCachedInputTokens:         cached.Valid,
			MetricCacheWriteTokens:          cacheWrite.Valid,
			MetricOutputTokens:              output.Valid,
			MetricReasoningTokens:           reasoning.Valid,
			MetricToolTokens:                tool.Valid,
			MetricContextBeforeDWYT:         before.Valid,
			MetricContextAfterDWYT:          after.Valid,
			MetricCompressionMetadataTokens: compressionMetadata.Valid,
			MetricEstimatedCostUSD:          estimated.Valid,
			MetricActualCostUSD:             actual.Valid,
			MetricLatencyMS:                 latency.Valid,
		})
		rollups.add(MetricInputTokens, input.Valid, eventProvenance.For(MetricInputTokens))
		rollups.add(MetricUncachedInputTokens, uncached.Valid, eventProvenance.For(MetricUncachedInputTokens))
		rollups.add(MetricCachedInputTokens, cached.Valid, eventProvenance.For(MetricCachedInputTokens))
		rollups.add(MetricCacheWriteTokens, cacheWrite.Valid, eventProvenance.For(MetricCacheWriteTokens))
		rollups.add(MetricOutputTokens, output.Valid, eventProvenance.For(MetricOutputTokens))
		rollups.add(MetricReasoningTokens, reasoning.Valid, eventProvenance.For(MetricReasoningTokens))
		rollups.add(MetricToolTokens, tool.Valid, eventProvenance.For(MetricToolTokens))
		rollups.add(MetricContextBeforeDWYT, before.Valid, eventProvenance.For(MetricContextBeforeDWYT))
		rollups.add(MetricContextAfterDWYT, after.Valid, eventProvenance.For(MetricContextAfterDWYT))
		rollups.add(MetricCompressionMetadataTokens, compressionMetadata.Valid, eventProvenance.For(MetricCompressionMetadataTokens))
		rollups.add(MetricEstimatedCostUSD, estimated.Valid, eventProvenance.For(MetricEstimatedCostUSD))
		rollups.add(MetricActualCostUSD, actual.Valid, eventProvenance.For(MetricActualCostUSD))
		rollups.add(MetricLatencyMS, latency.Valid, eventProvenance.For(MetricLatencyMS))

		addNullInt(&sum.InputTokens, input)
		addNullInt(&sum.UncachedInputTokens, uncached)
		addNullInt(&sum.CacheWriteTokens, cacheWrite)
		addNullInt(&sum.OutputTokens, output)
		addNullInt(&sum.ReasoningTokens, reasoning)
		addNullInt(&sum.ToolTokens, tool)
		addNullInt(&sum.LatencyMS, latency)
		addNullInt(&sum.CompressionMetadataTokens, compressionMetadata)
		if input.Valid {
			sum.Coverage.InputReported++
		}
		if output.Valid {
			sum.Coverage.OutputReported++
		}
		if tool.Valid {
			sum.Coverage.ToolReported++
		}
		if latency.Valid {
			sum.Coverage.LatencyReported++
		}
		if cached.Valid {
			sum.CachedInputTokens += int(cached.Int64)
			sum.Coverage.CacheReported++
		}
		if before.Valid && after.Valid {
			sum.ContextBefore += int(before.Int64)
			sum.ContextAfter += int(after.Int64)
			sum.Coverage.ContextReported++
		}
		if compressionMetadata.Valid {
			sum.Coverage.CompressionMetadataReported++
		}
		if actual.Valid {
			sum.ObservedCostUSD += actual.Float64
			sum.Coverage.CostReported++
		} else if estimated.Valid {
			sum.EstimatedCostUSD += estimated.Float64
		}
	}
	if err := rows.Err(); err != nil {
		return sum, err
	}

	sum.Provenance[MetricRequests] = ProvenanceObserved
	sum.Provenance[MetricObservedRequests] = ProvenanceObserved
	for _, metric := range requestMetricNames {
		sum.Provenance[metric] = rollups.value(metric, sum.Requests)
	}
	sum.Provenance[MetricContextBefore] = sum.Provenance[MetricContextBeforeDWYT]
	sum.Provenance[MetricContextAfter] = sum.Provenance[MetricContextAfterDWYT]
	sum.Provenance[MetricObservedCostUSD] = rollups.value(MetricActualCostUSD, sum.Requests)

	cacheKnown := rollups.allReported(sum.Requests, MetricInputTokens, MetricCachedInputTokens)
	if cacheKnown && sum.InputTokens > 0 {
		pct := float64(sum.CachedInputTokens) / float64(sum.InputTokens) * 100
		sum.CacheHitPct = &pct
		sum.Provenance[MetricCacheHitPct] = aggregateProvenance(
			sum.Provenance[MetricInputTokens], sum.Provenance[MetricCachedInputTokens],
		)
	} else {
		sum.Provenance[MetricCacheHitPct] = ProvenanceUnsupported
	}

	contextKnown := rollups.allReported(sum.Requests, MetricContextBeforeDWYT, MetricContextAfterDWYT)
	if contextKnown {
		sum.AvoidedTokens = sum.ContextBefore - sum.ContextAfter
		if sum.AvoidedTokens < 0 {
			sum.AvoidedTokens = 0
		}
		sum.Provenance[MetricAvoidedTokens] = aggregateProvenance(
			sum.Provenance[MetricContextBeforeDWYT], sum.Provenance[MetricContextAfterDWYT],
		)
		if sum.ContextBefore > 0 {
			pct := float64(sum.AvoidedTokens) / float64(sum.ContextBefore) * 100
			sum.ContextReductionPct = &pct
			sum.Provenance[MetricContextReductionPct] = sum.Provenance[MetricAvoidedTokens]
		} else {
			sum.Provenance[MetricContextReductionPct] = ProvenanceUnsupported
		}
	} else {
		sum.Provenance[MetricAvoidedTokens] = ProvenanceUnsupported
		sum.Provenance[MetricContextReductionPct] = ProvenanceUnsupported
	}

	if err := s.summarizeTasks(&sum, projectID, since); err != nil {
		return sum, err
	}
	return sum, nil
}

func (s *Store) summarizeTasks(sum *Summary, projectID string, since time.Time) error {
	rows, err := s.db.Query(`
		SELECT success, attempts, total_cost_usd, total_tokens, completed_at
		FROM task_outcomes
		WHERE project_id = ? AND started_at >= ?`, projectID, since.Unix())
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()

	succeededCost := 0.0
	succeededTokens := 0
	attemptsTotal := 0
	for rows.Next() {
		var success, attempts, tokens int
		var cost float64
		var completedAt sql.NullInt64
		if err := rows.Scan(&success, &attempts, &cost, &tokens, &completedAt); err != nil {
			return err
		}
		sum.Tasks++
		attemptsTotal += attempts
		if success == 1 {
			sum.TasksSucceeded++
			succeededCost += cost
			succeededTokens += tokens
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}

	if sum.Tasks > 0 {
		pct := float64(sum.TasksSucceeded) / float64(sum.Tasks) * 100
		sum.CompletionPct = &pct
		avg := float64(attemptsTotal) / float64(sum.Tasks)
		sum.AvgAttempts = &avg
	}
	if sum.TasksSucceeded > 0 {
		// The headline metric: cost per *successfully completed* task. Dividing
		// by all tasks would reward failing fast.
		cost := succeededCost / float64(sum.TasksSucceeded)
		sum.CostPerCompletedTask = &cost
		tokens := float64(succeededTokens) / float64(sum.TasksSucceeded)
		sum.TokensPerCompletedTask = &tokens
	}
	if sum.Provenance == nil {
		sum.Provenance = make(MetricProvenance)
	}
	// Task outcomes are locally recorded facts. Derived cost/token rates retain
	// the conservative estimated label because their constituent request costs
	// can mix provider observations and DWYT estimates.
	sum.Provenance[MetricTasks] = ProvenanceObserved
	sum.Provenance[MetricTasksSucceeded] = ProvenanceObserved
	if sum.CompletionPct != nil {
		sum.Provenance[MetricCompletionPct] = ProvenanceObserved
	} else {
		sum.Provenance[MetricCompletionPct] = ProvenanceUnsupported
	}
	if sum.AvgAttempts != nil {
		sum.Provenance[MetricAvgAttempts] = ProvenanceObserved
	} else {
		sum.Provenance[MetricAvgAttempts] = ProvenanceUnsupported
	}
	if sum.CostPerCompletedTask != nil {
		sum.Provenance[MetricCostPerCompletedTask] = ProvenanceEstimated
	} else {
		sum.Provenance[MetricCostPerCompletedTask] = ProvenanceUnsupported
	}
	if sum.TokensPerCompletedTask != nil {
		sum.Provenance[MetricTokensPerCompletedTask] = ProvenanceEstimated
	} else {
		sum.Provenance[MetricTokensPerCompletedTask] = ProvenanceUnsupported
	}
	return nil
}

// Prune deletes request events older than a cutoff. Task outcomes are kept:
// they are small and are the historical record of what DWYT achieved.
func (s *Store) Prune(olderThan time.Time) error {
	_, err := s.db.Exec(`DELETE FROM llm_request_events WHERE ts < ?`, olderThan.Unix())
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`DELETE FROM housekeeper_runs WHERE ts < ?`, olderThan.Unix())
	return err
}

// RecentRequests returns the newest events for a project, for a debugging view.
func (s *Store) RecentRequests(projectID string, limit int) ([]RequestEvent, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := s.db.Query(`
		SELECT id, task_id, provider, model, variant, effort, phase,
		       input_tokens, uncached_input_tokens, cached_input_tokens,
		       cache_write_tokens, output_tokens, reasoning_tokens, tool_tokens,
		       context_before, context_after, compression_metadata_tokens,
		       estimated_cost_usd, actual_cost_usd,
		       cache_key_hash, prefix_hash, latency_ms, observed, metric_provenance, ts
		FROM llm_request_events
		WHERE project_id = ?
		ORDER BY ts DESC LIMIT ?`, projectID, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var out []RequestEvent
	for rows.Next() {
		var e RequestEvent
		var taskID, provider, model, variant, effort, phase, cacheKeyHash, prefixHash, provenance sql.NullString
		var input, uncached, cached, cacheWrite, output, reasoning, tool sql.NullInt64
		var before, after, compressionMetadata, latency sql.NullInt64
		var estimated, actual sql.NullFloat64
		var observed int
		var ts int64
		if err := rows.Scan(&e.ID, &taskID, &provider, &model, &variant, &effort, &phase,
			&input, &uncached, &cached, &cacheWrite, &output, &reasoning, &tool,
			&before, &after, &compressionMetadata, &estimated, &actual,
			&cacheKeyHash, &prefixHash, &latency, &observed, &provenance, &ts); err != nil {
			return nil, err
		}
		e.ProjectID = projectID
		e.TaskID = taskID.String
		e.Provider = provider.String
		e.Model = model.String
		e.Variant = variant.String
		e.Effort = effort.String
		e.Phase = phase.String
		e.CacheKeyHash = cacheKeyHash.String
		e.PrefixHash = prefixHash.String
		e.InputTokens = intPtrFromNull(input)
		e.UncachedInputTokens = intPtrFromNull(uncached)
		e.CachedInputTokens = intPtrFromNull(cached)
		e.CacheWriteTokens = intPtrFromNull(cacheWrite)
		e.OutputTokens = intPtrFromNull(output)
		e.ReasoningTokens = intPtrFromNull(reasoning)
		e.ToolTokens = intPtrFromNull(tool)
		e.ContextBefore = intPtrFromNull(before)
		e.ContextAfter = intPtrFromNull(after)
		e.CompressionMetadataTokens = intPtrFromNull(compressionMetadata)
		e.EstimatedCostUSD = floatPtrFromNull(estimated)
		e.ActualCostUSD = floatPtrFromNull(actual)
		e.LatencyMS = intPtrFromNull(latency)
		e.Observed = observed == 1
		e.Provenance = decodeRequestProvenance(provenance.String, e.Observed, requestMetricPresence(e))
		e.Timestamp = time.Unix(ts, 0)
		out = append(out, e)
	}
	return out, rows.Err()
}

// eventID derives a stable id from the event's identity so a retried report
// replaces rather than duplicates its row.
func eventID(e RequestEvent) string {
	parts := []string{
		e.ProjectID, e.TaskID, e.Provider, e.Model, e.Phase,
		fmt.Sprint(e.Timestamp.UnixNano()),
	}
	sort.SliceStable(parts, func(i, j int) bool { return false }) // keep order explicit
	return strings.Join(parts, "|")
}

func nullString(v string) interface{} {
	if v == "" {
		return nil
	}
	return v
}

func nullInt(v *int) interface{} {
	if v == nil {
		return nil
	}
	return *v
}

func nullFloat(v *float64) interface{} {
	if v == nil {
		return nil
	}
	return *v
}

func valueOr(v *int) int {
	if v == nil {
		return 0
	}
	return *v
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func addNullInt(target *int, v sql.NullInt64) {
	if v.Valid {
		*target += int(v.Int64)
	}
}

func intPtrFromNull(v sql.NullInt64) *int {
	if !v.Valid {
		return nil
	}
	out := int(v.Int64)
	return &out
}

func floatPtrFromNull(v sql.NullFloat64) *float64 {
	if !v.Valid {
		return nil
	}
	out := v.Float64
	return &out
}

// ActivityTS returns the (distinct, ascending) timestamps of request events for
// a project, so session detection can union them with the other activity
// ledgers.
func (s *Store) ActivityTS(projectID string, since time.Time) ([]int64, error) {
	rows, err := s.db.Query(
		`SELECT DISTINCT ts FROM llm_request_events WHERE project_id = ? AND ts >= ? ORDER BY ts`,
		projectID, since.Unix(),
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []int64
	for rows.Next() {
		var ts int64
		if err := rows.Scan(&ts); err != nil {
			return nil, err
		}
		out = append(out, ts)
	}
	return out, rows.Err()
}

// ModelUsage is the per-model rollup of the requests inside a time span. Token
// sums skip events that did not report the field (NULL), so a model that never
// reported reasoning tokens does not drag an imaginary 0 into the aggregate.
type ModelUsage struct {
	Model string
	// Variant/Effort identify the exact model configuration the requests ran
	// with; the session view groups on the triple so different efforts stay
	// distinct rows instead of blurring into one aggregate.
	Variant string
	Effort  string

	Requests         int
	ObservedRequests int
	InputTokens      int
	CachedTokens     int
	OutputTokens     int
	ReasoningTokens  int
	EstimatedCostUSD float64
	ActualCostUSD    float64
	// FirstTS/LastTS bound the model's own activity inside the span (unix
	// seconds); they ground the tokens-per-second figure.
	FirstTS int64
	LastTS  int64
}

// TotalTokens is the conversational weight of the model usage: input + output
// + reasoning, the classes a user recognizes as "the conversation".
func (m ModelUsage) TotalTokens() int {
	return m.InputTokens + m.OutputTokens + m.ReasoningTokens
}

// SessionUsage rolls the request ledger up per model+variant+effort inside
// [start, end].
func (s *Store) SessionUsage(projectID string, start, end time.Time) ([]ModelUsage, error) {
	rows, err := s.db.Query(`
		SELECT COALESCE(model, ''),
		       COALESCE(variant, ''),
		       COALESCE(effort, ''),
		       COUNT(*),
		       COALESCE(SUM(observed), 0),
		       COALESCE(SUM(input_tokens), 0),
		       COALESCE(SUM(cached_input_tokens), 0),
		       COALESCE(SUM(output_tokens), 0),
		       COALESCE(SUM(reasoning_tokens), 0),
		       COALESCE(SUM(estimated_cost_usd), 0),
		       COALESCE(SUM(actual_cost_usd), 0),
		       MIN(ts), MAX(ts)
		FROM llm_request_events
		WHERE project_id = ? AND ts >= ? AND ts <= ?
		GROUP BY model, variant, effort
		ORDER BY MIN(ts)`,
		projectID, start.Unix(), end.Unix(),
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var out []ModelUsage
	for rows.Next() {
		var m ModelUsage
		var estimated, actual sql.NullFloat64
		if err := rows.Scan(
			&m.Model, &m.Variant, &m.Effort, &m.Requests, &m.ObservedRequests,
			&m.InputTokens, &m.CachedTokens, &m.OutputTokens, &m.ReasoningTokens,
			&estimated, &actual, &m.FirstTS, &m.LastTS,
		); err != nil {
			return nil, err
		}
		if estimated.Valid {
			m.EstimatedCostUSD = estimated.Float64
		}
		if actual.Valid {
			m.ActualCostUSD = actual.Float64
		}
		out = append(out, m)
	}
	return out, rows.Err()
}
