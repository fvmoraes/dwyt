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
	Phase     string `json:"phase,omitempty"`

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

	EstimatedCostUSD *float64 `json:"estimated_cost_usd,omitempty"`
	ActualCostUSD    *float64 `json:"actual_cost_usd,omitempty"`

	CacheKeyHash string `json:"cache_key_hash,omitempty"`
	PrefixHash   string `json:"prefix_hash,omitempty"`

	LatencyMS *int `json:"latency_ms,omitempty"`

	// Observed is true only when the provider reported the numbers.
	Observed  bool      `json:"observed"`
	Timestamp time.Time `json:"ts"`
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
	_, err := s.db.Exec(`
		CREATE TABLE IF NOT EXISTS llm_request_events (
			id                    TEXT PRIMARY KEY,
			project_id            TEXT NOT NULL,
			task_id               TEXT,
			provider              TEXT,
			model                 TEXT,
			phase                 TEXT,
			input_tokens          INTEGER,
			uncached_input_tokens INTEGER,
			cached_input_tokens   INTEGER,
			cache_write_tokens    INTEGER,
			output_tokens         INTEGER,
			reasoning_tokens      INTEGER,
			tool_tokens           INTEGER,
			context_before        INTEGER,
			context_after         INTEGER,
			estimated_cost_usd    REAL,
			actual_cost_usd       REAL,
			cache_key_hash        TEXT,
			prefix_hash           TEXT,
			latency_ms            INTEGER,
			observed              INTEGER NOT NULL DEFAULT 0,
			ts                    INTEGER NOT NULL
		);
		CREATE INDEX IF NOT EXISTS idx_llm_events_project_ts
			ON llm_request_events(project_id, ts);
		CREATE INDEX IF NOT EXISTS idx_llm_events_task
			ON llm_request_events(task_id);

		CREATE TABLE IF NOT EXISTS task_outcomes (
			task_id      TEXT PRIMARY KEY,
			project_id   TEXT NOT NULL,
			success      INTEGER NOT NULL DEFAULT 0,
			tests_pass   INTEGER NOT NULL DEFAULT 0,
			attempts     INTEGER NOT NULL DEFAULT 0,
			total_cost_usd REAL NOT NULL DEFAULT 0,
			total_tokens INTEGER NOT NULL DEFAULT 0,
			started_at   INTEGER NOT NULL,
			completed_at INTEGER
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
	`)
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

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	_, err = tx.Exec(`
		INSERT OR REPLACE INTO llm_request_events (
			id, project_id, task_id, provider, model, phase,
			input_tokens, uncached_input_tokens, cached_input_tokens,
			cache_write_tokens, output_tokens, reasoning_tokens, tool_tokens,
			context_before, context_after,
			estimated_cost_usd, actual_cost_usd,
			cache_key_hash, prefix_hash, latency_ms, observed, ts
		) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		e.ID, e.ProjectID, nullString(e.TaskID), nullString(e.Provider),
		nullString(e.Model), nullString(e.Phase),
		nullInt(e.InputTokens), nullInt(e.UncachedInputTokens), nullInt(e.CachedInputTokens),
		nullInt(e.CacheWriteTokens), nullInt(e.OutputTokens), nullInt(e.ReasoningTokens),
		nullInt(e.ToolTokens), nullInt(e.ContextBefore), nullInt(e.ContextAfter),
		nullFloat(e.EstimatedCostUSD), nullFloat(e.ActualCostUSD),
		nullString(e.CacheKeyHash), nullString(e.PrefixHash), nullInt(e.LatencyMS),
		boolToInt(e.Observed), e.Timestamp.Unix(),
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

	InputTokens         int `json:"input_tokens"`
	CachedInputTokens   int `json:"cached_input_tokens"`
	UncachedInputTokens int `json:"uncached_input_tokens"`
	CacheWriteTokens    int `json:"cache_write_tokens"`
	OutputTokens        int `json:"output_tokens"`
	ReasoningTokens     int `json:"reasoning_tokens"`

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

	// Coverage tells the reader how much of this is measured rather than guessed.
	Coverage Coverage `json:"coverage"`
}

// Coverage describes how complete the data behind a Summary is.
type Coverage struct {
	CacheReported   int `json:"cache_reported_requests"`
	ContextReported int `json:"context_reported_requests"`
	CostReported    int `json:"cost_reported_requests"`
}

// Summarize aggregates a time window for a project.
func (s *Store) Summarize(projectID string, since time.Time, window string) (Summary, error) {
	sum := Summary{Window: window}
	rows, err := s.db.Query(`
		SELECT input_tokens, uncached_input_tokens, cached_input_tokens,
		       cache_write_tokens, output_tokens, reasoning_tokens,
		       context_before, context_after,
		       estimated_cost_usd, actual_cost_usd, observed
		FROM llm_request_events
		WHERE project_id = ? AND ts >= ?`, projectID, since.Unix())
	if err != nil {
		return sum, err
	}
	defer rows.Close()

	for rows.Next() {
		var input, uncached, cached, cacheWrite, output, reasoning sql.NullInt64
		var before, after sql.NullInt64
		var estimated, actual sql.NullFloat64
		var observed int
		if err := rows.Scan(&input, &uncached, &cached, &cacheWrite, &output,
			&reasoning, &before, &after, &estimated, &actual, &observed); err != nil {
			return sum, err
		}
		sum.Requests++
		if observed == 1 {
			sum.ObservedRequests++
		}
		addNullInt(&sum.InputTokens, input)
		addNullInt(&sum.UncachedInputTokens, uncached)
		addNullInt(&sum.CacheWriteTokens, cacheWrite)
		addNullInt(&sum.OutputTokens, output)
		addNullInt(&sum.ReasoningTokens, reasoning)
		if cached.Valid {
			sum.CachedInputTokens += int(cached.Int64)
			sum.Coverage.CacheReported++
		}
		if before.Valid && after.Valid {
			sum.ContextBefore += int(before.Int64)
			sum.ContextAfter += int(after.Int64)
			sum.Coverage.ContextReported++
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

	// Ratios are only reported when there is data behind them.
	if sum.Coverage.CacheReported > 0 && sum.InputTokens > 0 {
		pct := float64(sum.CachedInputTokens) / float64(sum.InputTokens) * 100
		sum.CacheHitPct = &pct
	}
	if sum.Coverage.ContextReported > 0 && sum.ContextBefore > 0 {
		sum.AvoidedTokens = sum.ContextBefore - sum.ContextAfter
		if sum.AvoidedTokens < 0 {
			sum.AvoidedTokens = 0
		}
		pct := float64(sum.AvoidedTokens) / float64(sum.ContextBefore) * 100
		sum.ContextReductionPct = &pct
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
	defer rows.Close()

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
		SELECT id, task_id, provider, model, phase,
		       input_tokens, cached_input_tokens, output_tokens,
		       estimated_cost_usd, actual_cost_usd, prefix_hash, observed, ts
		FROM llm_request_events
		WHERE project_id = ?
		ORDER BY ts DESC LIMIT ?`, projectID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []RequestEvent
	for rows.Next() {
		var e RequestEvent
		var taskID, provider, model, phase, prefixHash sql.NullString
		var input, cached, output sql.NullInt64
		var estimated, actual sql.NullFloat64
		var observed int
		var ts int64
		if err := rows.Scan(&e.ID, &taskID, &provider, &model, &phase,
			&input, &cached, &output, &estimated, &actual, &prefixHash, &observed, &ts); err != nil {
			return nil, err
		}
		e.ProjectID = projectID
		e.TaskID = taskID.String
		e.Provider = provider.String
		e.Model = model.String
		e.Phase = phase.String
		e.PrefixHash = prefixHash.String
		e.InputTokens = intPtrFromNull(input)
		e.CachedInputTokens = intPtrFromNull(cached)
		e.OutputTokens = intPtrFromNull(output)
		e.EstimatedCostUSD = floatPtrFromNull(estimated)
		e.ActualCostUSD = floatPtrFromNull(actual)
		e.Observed = observed == 1
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
