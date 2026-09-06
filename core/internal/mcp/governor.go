package mcp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// DWYT MCP — the Governor (spec §3.1).
//
// This is the third and final official MCP alongside Obsidian (Brain) and
// Codebase (Code Intelligence). It is a thin stdio shim over the daemon's HTTP
// API so every AI client shares one governor state.
//
// Tool responses are deliberately terse. The governor exists to reduce token
// spend; a chatty governor would defeat its own purpose. Nothing here echoes
// the policy text back to the caller.

// GovernorTools is the DWYT MCP tool implementation.
type GovernorTools struct {
	client *http.Client
}

// NewGovernorTools builds the tool set. The timeout is generous enough for a
// cold daemon start but short enough that a hung daemon does not stall the
// agent's turn.
func NewGovernorTools() *GovernorTools {
	return &GovernorTools{client: &http.Client{Timeout: 15 * time.Second}}
}

// postJSON sends args as a JSON body and returns the pretty-printed response.
// Errors carry the endpoint so a misconfigured DWYT_API_URL is obvious.
func (gt *GovernorTools) postJSON(path string, payload interface{}) (string, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("encode request: %w", err)
	}
	resp, err := gt.client.Post(dwytAPI+path, "application/json", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("dwyt governor %s failed: %w", path, err)
	}
	defer resp.Body.Close()
	return decodeGovernorResponse(resp, path)
}

func (gt *GovernorTools) getJSON(path string, query url.Values) (string, error) {
	target := dwytAPI + path
	if len(query) > 0 {
		target += "?" + query.Encode()
	}
	resp, err := gt.client.Get(target)
	if err != nil {
		return "", fmt.Errorf("dwyt governor %s failed: %w", path, err)
	}
	defer resp.Body.Close()
	return decodeGovernorResponse(resp, path)
}

func decodeGovernorResponse(resp *http.Response, path string) (string, error) {
	var payload interface{}
	decodeErr := json.NewDecoder(resp.Body).Decode(&payload)
	if resp.StatusCode >= 400 {
		if m, ok := payload.(map[string]interface{}); ok {
			if msg, ok := m["error"].(string); ok {
				return "", fmt.Errorf("dwyt governor %s: %s", path, msg)
			}
		}
		return "", fmt.Errorf("dwyt governor %s returned HTTP %d", path, resp.StatusCode)
	}
	if decodeErr != nil {
		return "", fmt.Errorf("parse %s response: %w", path, decodeErr)
	}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// ContextPlan implements dwyt_context_plan.
func (gt *GovernorTools) ContextPlan(args map[string]interface{}) (string, error) {
	task, _ := args["task"].(string)
	if strings.TrimSpace(task) == "" {
		return "", fmt.Errorf("task is required")
	}
	payload := map[string]interface{}{"task": task}
	copyString(payload, args, "task_id", "phase", "complexity", "achieved_level")
	copyStringSlice(payload, args, "hints", "missing_context")
	copyNumber(payload, args, "confidence", "model_context_window", "long_context_threshold")
	copyBool(payload, args, "allow_long_context")
	return gt.postJSON("/governor/plan", payload)
}

// ContextStatus implements dwyt_context_status.
func (gt *GovernorTools) ContextStatus(args map[string]interface{}) (string, error) {
	query := url.Values{}
	if v, ok := args["task_id"].(string); ok && v != "" {
		query.Set("task_id", v)
	}
	return gt.getJSON("/governor/status", query)
}

// RegisterContext implements dwyt_register_context.
func (gt *GovernorTools) RegisterContext(args map[string]interface{}) (string, error) {
	items, ok := args["items"].([]interface{})
	if !ok || len(items) == 0 {
		return "", fmt.Errorf("items is required and must be a non-empty array")
	}
	payload := map[string]interface{}{"items": items}
	copyString(payload, args, "task_id")
	return gt.postJSON("/governor/register", payload)
}

// OutputProfile implements dwyt_output_profile.
func (gt *GovernorTools) OutputProfile(args map[string]interface{}) (string, error) {
	query := url.Values{}
	if v, ok := args["task_type"].(string); ok && v != "" {
		query.Set("task_type", v)
	}
	if v, ok := args["phase"].(string); ok && v != "" {
		query.Set("phase", v)
	}
	return gt.getJSON("/governor/output-profile", query)
}

// CacheGuidance implements dwyt_cache_guidance.
func (gt *GovernorTools) CacheGuidance(args map[string]interface{}) (string, error) {
	query := url.Values{}
	for _, key := range []string{"provider", "model"} {
		if v, ok := args[key].(string); ok && v != "" {
			query.Set(key, v)
		}
	}
	return gt.getJSON("/governor/cache-guidance", query)
}

// CompactToolOutput implements dwyt_compact_tool_output.
func (gt *GovernorTools) CompactToolOutput(args map[string]interface{}) (string, error) {
	content, _ := args["content"].(string)
	rawRef, _ := args["raw_ref"].(string)
	if strings.TrimSpace(content) == "" && strings.TrimSpace(rawRef) == "" {
		return "", fmt.Errorf("content or raw_ref is required")
	}
	payload := map[string]interface{}{}
	copyString(payload, args, "task_id", "content", "raw_ref", "kind", "label")
	copyBool(payload, args, "already_compact")
	return gt.postJSON("/governor/compact", payload)
}

// GetRaw implements dwyt_get_raw.
func (gt *GovernorTools) GetRaw(args map[string]interface{}) (string, error) {
	ref, _ := args["ref"].(string)
	if strings.TrimSpace(ref) == "" {
		return "", fmt.Errorf("ref is required")
	}
	return gt.getJSON("/governor/raw", url.Values{"ref": {ref}})
}

// ReportUsage implements dwyt_report_usage.
func (gt *GovernorTools) ReportUsage(args map[string]interface{}) (string, error) {
	payload := map[string]interface{}{}
	copyString(payload, args, "task_id", "provider", "model", "phase", "cache_key_hash", "prefix_hash")
	copyNumber(payload, args,
		"input_tokens", "uncached_input_tokens", "cached_input_tokens",
		"cache_write_tokens", "output_tokens", "reasoning_tokens", "tool_tokens",
		"context_before_dwyt", "context_after_dwyt",
		"estimated_cost_usd", "actual_cost_usd", "latency_ms", "tool_iterations")
	copyBool(payload, args, "observed", "build_failed")
	copyStringSlice(payload, args, "cached_hashes")
	if len(payload) == 0 {
		return "", fmt.Errorf("at least one usage field is required")
	}
	return gt.postJSON("/governor/usage", payload)
}

// Route implements dwyt_route.
func (gt *GovernorTools) Route(args map[string]interface{}) (string, error) {
	payload := map[string]interface{}{}
	copyString(payload, args, "task_id", "task", "phase")
	copyNumber(payload, args, "files", "modules", "languages", "diff_lines",
		"prior_failures", "confidence")
	copyBool(payload, args, "touches_architecture", "touches_security",
		"touches_infrastructure", "destructive", "has_tests")
	return gt.postJSON("/governor/route", payload)
}

// HousekeeperStatus implements dwyt_housekeeper_status.
func (gt *GovernorTools) HousekeeperStatus(args map[string]interface{}) (string, error) {
	return gt.getJSON("/governor/housekeeper", nil)
}

// HousekeeperRun implements dwyt_housekeeper_run.
//
// It defaults to a dry run. A housekeeping pass deletes notes, and an agent
// should have to opt in explicitly before that happens rather than discovering
// it after the fact.
func (gt *GovernorTools) HousekeeperRun(args map[string]interface{}) (string, error) {
	query := url.Values{}
	if v, ok := args["depth"].(string); ok && v != "" {
		query.Set("depth", v)
	}
	apply, _ := args["apply"].(bool)
	if !apply {
		query.Set("dry_run", "true")
	}
	resp, err := gt.client.Post(dwytAPI+"/housekeeper/run?"+query.Encode(), "application/json", nil)
	if err != nil {
		return "", fmt.Errorf("housekeeper run failed: %w", err)
	}
	defer resp.Body.Close()
	return decodeGovernorResponse(resp, "/housekeeper/run")
}

// MemoryHealth implements dwyt_memory_health.
func (gt *GovernorTools) MemoryHealth(args map[string]interface{}) (string, error) {
	return gt.getJSON("/governor/memory-health", nil)
}

// RegisterGovernorTools registers the DWYT MCP tool surface.
func RegisterGovernorTools(s *Server) {
	gt := NewGovernorTools()

	s.RegisterTool("dwyt_context_plan",
		"Get the DWYT context plan before any broad repository or memory retrieval. "+
			"Returns the token budget, which canonical memory keys and code targets to load, "+
			"what to exclude, the authorized retrieval level (project map → module → symbol → "+
			"range → full file), cache ordering and the output budget. Call this first.",
		map[string]Property{
			"task":                   {Type: "string", Description: "What you are about to do, in one sentence"},
			"task_id":                {Type: "string", Description: "Stable id for this task; reuse it across turns so context is not re-retrieved"},
			"phase":                  {Type: "string", Description: "classify, retrieve, tool_loop, fix, review, plan or artifact"},
			"complexity":             {Type: "string", Description: "trivial, simple, medium, complex or critical"},
			"hints":                  {Type: "array", Description: "Known symbols, files or modules to target"},
			"confidence":             {Type: "number", Description: "0..1 confidence that you can act with the context you already have"},
			"missing_context":        {Type: "array", Description: "Exactly what you still lack; the plan targets only these"},
			"achieved_level":         {Type: "string", Description: "Highest retrieval level already completed, so the plan advances one rung"},
			"model_context_window":   {Type: "number", Description: "Context window of the model in use, when known"},
			"long_context_threshold": {Type: "number", Description: "Provider long-context pricing threshold, when known"},
			"allow_long_context":     {Type: "boolean", Description: "Permit planning above the long-context threshold"},
		},
		[]string{"task"},
		gt.ContextPlan,
	)

	s.RegisterTool("dwyt_context_status",
		"Report the compact state of the current task: objective, affected files, validation, "+
			"active errors, blockers, current budget and which already-retrieved context can be "+
			"reused instead of fetched again.",
		map[string]Property{
			"task_id": {Type: "string", Description: "Task id used with dwyt_context_plan"},
		},
		nil,
		gt.ContextStatus,
	)

	s.RegisterTool("dwyt_register_context",
		"Register the context you actually obtained so the governor can rank it by token ROI, "+
			"fit it into the budget and tell you what to keep, drop or reuse. Send metadata "+
			"(kind, tokens, path, symbol, content_hash) rather than full content when possible.",
		map[string]Property{
			"task_id": {Type: "string", Description: "Task id used with dwyt_context_plan"},
			"items":   {Type: "array", Description: "Context blocks: {id, kind, title, tokens, path, symbol, content_hash, state, cache_class, source, pinned}"},
		},
		[]string{"items"},
		gt.RegisterContext,
	)

	s.RegisterTool("dwyt_output_profile",
		"Get the output budget for the current phase: target visible tokens, whether to answer "+
			"with a structured operational payload, and whether the artifact exception applies "+
			"(a requested document must never be truncated).",
		map[string]Property{
			"task_type": {Type: "string", Description: "fix, review, plan, document, classify, retrieve or tool"},
			"phase":     {Type: "string", Description: "Explicit phase, overriding task_type"},
		},
		nil,
		gt.OutputProfile,
	)

	s.RegisterTool("dwyt_cache_guidance",
		"Get prompt-assembly guidance for provider prefix caching: the immutable → long_lived → "+
			"session → volatile ordering, what must never precede the stable prefix, and the "+
			"detected capability state for the provider/model.",
		map[string]Property{
			"provider": {Type: "string", Description: "Provider name, e.g. openai, anthropic, gemini"},
			"model":    {Type: "string", Description: "Model identifier"},
		},
		nil,
		gt.CacheGuidance,
	)

	s.RegisterTool("dwyt_compact_tool_output",
		"Compact large tool output (tests, builds, linters, logs) into status, deduplicated "+
			"diagnostics and counts, archiving the full bytes in the raw object store and "+
			"returning a raw_ref you can resolve later with dwyt_get_raw.",
		map[string]Property{
			"task_id":         {Type: "string", Description: "Task id, so the raw reference is recorded in the task state"},
			"content":         {Type: "string", Description: "Raw tool output to compact"},
			"raw_ref":         {Type: "string", Description: "Existing raw reference to compact instead of inline content"},
			"kind":            {Type: "string", Description: "tool_output, build, test or debug_dump; drives retention"},
			"label":           {Type: "string", Description: "Short human description, e.g. 'go test ./...'"},
			"already_compact": {Type: "boolean", Description: "True when RTK already reduced this output"},
		},
		nil,
		gt.CompactToolOutput,
	)

	s.RegisterTool("dwyt_get_raw",
		"Resolve a dwyt://objects/<id> reference back to the full raw output. Use it only when "+
			"the compact summary is insufficient.",
		map[string]Property{
			"ref": {Type: "string", Description: "Raw reference, e.g. dwyt://objects/a8293f..."},
		},
		[]string{"ref"},
		gt.GetRaw,
	)

	s.RegisterTool("dwyt_report_usage",
		"Report per-request token/cost usage so DWYT can measure real savings and enforce stop "+
			"conditions. Set observed=true only for numbers the provider actually returned; omit "+
			"fields you do not know rather than sending zero.",
		map[string]Property{
			"task_id":               {Type: "string", Description: "Task id used with dwyt_context_plan"},
			"provider":              {Type: "string", Description: "Provider name"},
			"model":                 {Type: "string", Description: "Model identifier"},
			"phase":                 {Type: "string", Description: "Phase this request belonged to"},
			"input_tokens":          {Type: "number", Description: "Total input tokens"},
			"uncached_input_tokens": {Type: "number", Description: "Input tokens billed as uncached"},
			"cached_input_tokens":   {Type: "number", Description: "Input tokens served from cache"},
			"cache_write_tokens":    {Type: "number", Description: "Tokens written to cache"},
			"output_tokens":         {Type: "number", Description: "Output tokens"},
			"reasoning_tokens":      {Type: "number", Description: "Reasoning/thinking tokens, when billed"},
			"tool_tokens":           {Type: "number", Description: "Tokens spent on tool payloads"},
			"context_before_dwyt":   {Type: "number", Description: "Context size before DWYT governance"},
			"context_after_dwyt":    {Type: "number", Description: "Context size actually sent"},
			"estimated_cost_usd":    {Type: "number", Description: "DWYT cost estimate"},
			"actual_cost_usd":       {Type: "number", Description: "Provider-reported cost, when available"},
			"latency_ms":            {Type: "number", Description: "Request latency in milliseconds"},
			"tool_iterations":       {Type: "number", Description: "Tool iterations consumed by this request"},
			"observed":              {Type: "boolean", Description: "True only when the provider reported these numbers"},
			"build_failed":          {Type: "boolean", Description: "True when this turn produced a failed build"},
			"cache_key_hash":        {Type: "string", Description: "Cache key hash used for the request"},
			"prefix_hash":           {Type: "string", Description: "Stable prefix hash, for cache miss diagnostics"},
			"cached_hashes":         {Type: "array", Description: "Prefix hashes the provider confirmed as cache reads"},
		},
		nil,
		gt.ReportUsage,
	)

	s.RegisterTool("dwyt_housekeeper_status",
		"Report Brain lifecycle health: retained sessions against the 100-session limit, items "+
			"nearing expiry, stale notes, raw object footprint and the last housekeeping run.",
		map[string]Property{},
		nil,
		gt.HousekeeperStatus,
	)

	s.RegisterTool("dwyt_route",
		"Classify a task by complexity and risk and get the recommended model tier "+
			"(local, cheap, mid, frontier, premium), the matching context budget, output "+
			"profile and cost limits. Deterministic: no auxiliary model is used. This is a "+
			"recommendation, not an instruction, and tiers are not mapped to model names.",
		map[string]Property{
			"task_id":                {Type: "string", Description: "Task id, so observed failure history is taken into account"},
			"task":                   {Type: "string", Description: "One-sentence description of the task"},
			"phase":                  {Type: "string", Description: "classify, retrieve, tool_loop, fix, review, plan or artifact"},
			"files":                  {Type: "number", Description: "Number of files the task touches"},
			"modules":                {Type: "number", Description: "Number of modules the task touches"},
			"languages":              {Type: "number", Description: "Number of languages involved"},
			"diff_lines":             {Type: "number", Description: "Size of the change in lines, when known"},
			"touches_architecture":   {Type: "boolean", Description: "The change alters structure, not only behaviour"},
			"touches_security":       {Type: "boolean", Description: "Auth, crypto, secrets or access control"},
			"touches_infrastructure": {Type: "boolean", Description: "Deployment, migrations or infrastructure code"},
			"destructive":            {Type: "boolean", Description: "The change deletes or overwrites data"},
			"has_tests":              {Type: "boolean", Description: "The affected area is covered by tests, which lowers risk"},
			"prior_failures":         {Type: "number", Description: "How many times this task already failed"},
			"confidence":             {Type: "number", Description: "0..1 confidence that you can complete the task"},
		},
		nil,
		gt.Route,
	)

	s.RegisterTool("dwyt_housekeeper_run",
		"Run a Brain retention pass. Defaults to a dry run that reports what would be removed and "+
			"changes nothing; pass apply=true to commit. Reusable knowledge is always promoted to "+
			"canonical memory before anything is deleted.",
		map[string]Property{
			"depth": {Type: "string", Description: "light (bookkeeping only) or deep (session limit, TTLs, dedup, raw pruning)"},
			"apply": {Type: "boolean", Description: "Commit the pass. Omit or set false to preview only"},
		},
		nil,
		gt.HousekeeperRun,
	)

	s.RegisterTool("dwyt_memory_health",
		"Report the Obsidian Brain's composition: note counts per area (project, architecture, "+
			"decisions, modules, knowledge, state, sessions) and vault size.",
		map[string]Property{},
		nil,
		gt.MemoryHealth,
	)
}

// copyString copies present, non-empty string args into the payload.
func copyString(dst, src map[string]interface{}, keys ...string) {
	for _, key := range keys {
		if v, ok := src[key].(string); ok && strings.TrimSpace(v) != "" {
			dst[key] = v
		}
	}
}

// copyNumber copies present numeric args. JSON decoding gives float64; a string
// that looks numeric is also accepted because some clients stringify numbers in
// tool arguments.
func copyNumber(dst, src map[string]interface{}, keys ...string) {
	for _, key := range keys {
		switch v := src[key].(type) {
		case float64:
			dst[key] = v
		case int:
			dst[key] = v
		case json.Number:
			if f, err := v.Float64(); err == nil {
				dst[key] = f
			}
		}
	}
}

func copyBool(dst, src map[string]interface{}, keys ...string) {
	for _, key := range keys {
		if v, ok := src[key].(bool); ok {
			dst[key] = v
		}
	}
}

// copyStringSlice copies array args, keeping only string elements. A single
// string is promoted to a one-element array: agents frequently pass a bare
// string where an array is expected, and rejecting it would be pure friction.
func copyStringSlice(dst, src map[string]interface{}, keys ...string) {
	for _, key := range keys {
		switch v := src[key].(type) {
		case []interface{}:
			out := make([]string, 0, len(v))
			for _, item := range v {
				if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
					out = append(out, s)
				}
			}
			if len(out) > 0 {
				dst[key] = out
			}
		case []string:
			if len(v) > 0 {
				dst[key] = v
			}
		case string:
			if strings.TrimSpace(v) != "" {
				dst[key] = []string{v}
			}
		}
	}
}
