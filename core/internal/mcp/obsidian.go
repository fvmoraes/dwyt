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

var dwytAPI = "http://localhost:2737/api"

func SetAPIBase(url string) {
	dwytAPI = url
}

type ObsidianTools struct {
	client *http.Client
}

func NewObsidianTools() *ObsidianTools {
	return &ObsidianTools{
		client: &http.Client{Timeout: 10 * time.Second},
	}
}

func (ot *ObsidianTools) Search(args map[string]interface{}) (string, error) {
	query, _ := args["query"].(string)
	if query == "" {
		return "", fmt.Errorf("query is required")
	}
	resp, err := ot.client.Get(fmt.Sprintf("%s/obsidian/search?q=%s", dwytAPI, url.QueryEscape(query)))
	if err != nil {
		return "", fmt.Errorf("obsidian search failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("obsidian search returned HTTP %d", resp.StatusCode)
	}
	var result struct {
		Results []map[string]interface{} `json:"results"`
		Count   int                      `json:"count"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("parse error: %w", err)
	}
	if result.Count == 0 {
		return "No results found", nil
	}
	data, _ := json.MarshalIndent(result.Results, "", "  ")
	return string(data), nil
}

func (ot *ObsidianTools) Save(args map[string]interface{}) (string, error) {
	entryType, _ := args["type"].(string)
	if entryType == "" {
		entryType = "note"
	}
	content, _ := args["content"].(string)
	if content == "" {
		return "", fmt.Errorf("content is required")
	}
	body, _ := json.Marshal(map[string]string{
		"type":    entryType,
		"content": content,
	})
	resp, err := ot.client.Post(
		fmt.Sprintf("%s/obsidian/save", dwytAPI),
		"application/json",
		bytes.NewReader(body),
	)
	if err != nil {
		return "", fmt.Errorf("save failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("save returned HTTP %d", resp.StatusCode)
	}
	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)
	if status, ok := result["status"].(string); ok {
		return fmt.Sprintf("Entry saved: %s", status), nil
	}
	return "Entry saved", nil
}

func (ot *ObsidianTools) SaveContext(args map[string]interface{}) (string, error) {
	if _, ok := args["client"]; !ok {
		args["client"] = "mcp"
	}
	body, _ := json.Marshal(args)
	resp, err := ot.client.Post(
		fmt.Sprintf("%s/obsidian/context", dwytAPI),
		"application/json",
		bytes.NewReader(body),
	)
	if err != nil {
		return "", fmt.Errorf("context save failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("context save returned HTTP %d", resp.StatusCode)
	}
	var result struct {
		Status string `json:"status"`
		File   string `json:"file"`
	}
	json.NewDecoder(resp.Body).Decode(&result)
	if result.File != "" {
		return "Context saved: " + result.File, nil
	}
	return "Context saved", nil
}

func (ot *ObsidianTools) Status(args map[string]interface{}) (string, error) {
	resp, err := ot.client.Get(fmt.Sprintf("%s/obsidian/status", dwytAPI))
	if err != nil {
		return "", fmt.Errorf("status check failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("status returned HTTP %d", resp.StatusCode)
	}
	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)
	data, _ := json.MarshalIndent(result, "", "  ")
	return string(data), nil
}

func (ot *ObsidianTools) Summarize(args map[string]interface{}) (string, error) {
	resp, err := ot.client.Post(
		fmt.Sprintf("%s/obsidian/summarize", dwytAPI),
		"application/json",
		nil,
	)
	if err != nil {
		return "", fmt.Errorf("summarize failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("summarize returned HTTP %d", resp.StatusCode)
	}
	var result struct {
		Status  string `json:"status"`
		Summary string `json:"summary"`
	}
	json.NewDecoder(resp.Body).Decode(&result)
	if result.Summary != "" {
		return result.Summary, nil
	}
	return "Summary rebuilt", nil
}

func (ot *ObsidianTools) Open(args map[string]interface{}) (string, error) {
	resp, err := ot.client.Post(
		fmt.Sprintf("%s/obsidian/open", dwytAPI),
		"application/json",
		nil,
	)
	if err != nil {
		return "", fmt.Errorf("open vault failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("open vault returned HTTP %d", resp.StatusCode)
	}
	return "Obsidian vault opened", nil
}

// Canonical implements obsidian_canonical (spec §15, §17).
func (ot *ObsidianTools) Canonical(args map[string]interface{}) (string, error) {
	query := url.Values{}
	if v, ok := args["temperature"].(string); ok && v != "" {
		query.Set("temperature", v)
	}
	if v, ok := args["include_body"].(bool); ok && v {
		query.Set("include_body", "true")
	}
	target := fmt.Sprintf("%s/memory/canonical", dwytAPI)
	if len(query) > 0 {
		target += "?" + query.Encode()
	}
	resp, err := ot.client.Get(target)
	if err != nil {
		return "", fmt.Errorf("canonical memory read failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("canonical memory returned HTTP %d", resp.StatusCode)
	}
	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("parse error: %w", err)
	}
	data, _ := json.MarshalIndent(result, "", "  ")
	return string(data), nil
}

// UpsertCanonical implements obsidian_upsert_canonical.
func (ot *ObsidianTools) UpsertCanonical(args map[string]interface{}) (string, error) {
	key, _ := args["key"].(string)
	if strings.TrimSpace(key) == "" {
		return "", fmt.Errorf("key is required")
	}
	body, _ := args["body"].(string)
	bullet, _ := args["bullet"].(string)
	if strings.TrimSpace(body) == "" && strings.TrimSpace(bullet) == "" {
		return "", fmt.Errorf("body or bullet is required")
	}
	payload, _ := json.Marshal(args)
	resp, err := ot.client.Post(fmt.Sprintf("%s/memory/canonical", dwytAPI),
		"application/json", bytes.NewReader(payload))
	if err != nil {
		return "", fmt.Errorf("canonical memory write failed: %w", err)
	}
	defer resp.Body.Close()
	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)
	if resp.StatusCode >= 400 {
		if msg, ok := result["error"].(string); ok {
			return "", fmt.Errorf("canonical memory write: %s", msg)
		}
		return "", fmt.Errorf("canonical memory write returned HTTP %d", resp.StatusCode)
	}
	data, _ := json.MarshalIndent(result, "", "  ")
	return string(data), nil
}

// Compile implements obsidian_compile (spec §18).
func (ot *ObsidianTools) Compile(args map[string]interface{}) (string, error) {
	payload, _ := json.Marshal(args)
	resp, err := ot.client.Post(fmt.Sprintf("%s/memory/compile", dwytAPI),
		"application/json", bytes.NewReader(payload))
	if err != nil {
		return "", fmt.Errorf("memory compile failed: %w", err)
	}
	defer resp.Body.Close()
	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)
	if resp.StatusCode >= 400 {
		if msg, ok := result["error"].(string); ok {
			return "", fmt.Errorf("memory compile: %s", msg)
		}
		return "", fmt.Errorf("memory compile returned HTTP %d", resp.StatusCode)
	}
	data, _ := json.MarshalIndent(result, "", "  ")
	return string(data), nil
}

func RegisterObsidianTools(s *Server) {
	ot := NewObsidianTools()

	s.RegisterTool("obsidian_canonical",
		"Read the project's canonical memory: the single authoritative copy of project identity, "+
			"architecture, decisions, conventions, constraints and module summaries. Returns notes "+
			"ordered HOT then WARM then COLD with token estimates. Prefer this over searching old "+
			"session notes.",
		map[string]Property{
			"temperature":  {Type: "string", Description: "hot, warm or cold; omit for all"},
			"include_body": {Type: "boolean", Description: "Include note bodies; omit to get only keys, titles and token estimates"},
		},
		nil,
		ot.Canonical,
	)

	s.RegisterTool("obsidian_upsert_canonical",
		"Update canonical memory in place, or append one deduplicated bullet to a list note. "+
			"Use this instead of saving a second copy of a fact that changed. Keys: project, "+
			"active-constraints, architecture, frontend, backend, integrations, conventions, "+
			"known-issues, lessons, error-memory, current-task, active-errors, module:<name>.",
		map[string]Property{
			"key":         {Type: "string", Description: "Canonical key to write"},
			"title":       {Type: "string", Description: "Optional note title"},
			"body":        {Type: "string", Description: "Full replacement body for the note"},
			"bullet":      {Type: "string", Description: "Single list entry to append; skipped when an equivalent entry already exists"},
			"source_file": {Type: "string", Description: "Source file this note derives from, so it can be marked stale when that file changes"},
		},
		[]string{"key"},
		ot.UpsertCanonical,
	)

	s.RegisterTool("obsidian_compile",
		"Promote the reusable knowledge from a finished task into canonical memory: decisions, "+
			"constraints, reusable error patterns, lessons and known issues. Call it at the end of a "+
			"phase so the knowledge survives after the session note expires.",
		map[string]Property{
			"objective":      {Type: "string", Description: "What the task was about"},
			"decisions":      {Type: "array", Description: "Decisions made; constraint-shaped ones are routed to active-constraints"},
			"resolved":       {Type: "array", Description: "Resolved problems as 'symptom — resolution'; promoted as reusable error patterns"},
			"blockers":       {Type: "array", Description: "Unresolved blockers; promoted as known issues"},
			"next_steps":     {Type: "array", Description: "Follow-ups; lesson-shaped entries are promoted as lessons"},
			"affected_files": {Type: "array", Description: "Files the task touched"},
		},
		nil,
		ot.Compile,
	)

	s.RegisterTool("obsidian_search",
		"Search the Obsidian vault for notes matching a query. Returns matching entries with type, content, and creation date.",
		map[string]Property{
			"query": {Type: "string", Description: "Search query string to find matching notes in the vault"},
		},
		[]string{"query"},
		ot.Search,
	)

	s.RegisterTool("obsidian_save",
		"Save a new entry to the Obsidian vault. Supported types: note, decision, session, error.",
		map[string]Property{
			"type":    {Type: "string", Description: "Entry type: note, decision, session, or error. Default: note"},
			"content": {Type: "string", Description: "Markdown content to save in the vault"},
		},
		[]string{"content"},
		ot.Save,
	)

	s.RegisterTool("obsidian_save_context",
		"Save the current conversation context to the Obsidian vault. Call this at the end of each task with the user request, summary, changed files, decisions, actions, commands, errors, outcome, and next steps.",
		map[string]Property{
			"client":          {Type: "string", Description: "AI client name, for example codex, cursor, kiro, copilot, opencode, or claude"},
			"conversation_id": {Type: "string", Description: "Optional conversation or session identifier"},
			"user_request":    {Type: "string", Description: "The user's request for this conversation"},
			"summary":         {Type: "string", Description: "Short summary of what happened"},
			"context":         {Type: "string", Description: "Important context future agents should know"},
			"outcome":         {Type: "string", Description: "Final result or current status"},
			"files":           {Type: "array", Description: "Files read or changed"},
			"decisions":       {Type: "array", Description: "Important decisions made"},
			"actions":         {Type: "array", Description: "Actions completed"},
			"commands":        {Type: "array", Description: "Commands run"},
			"errors":          {Type: "array", Description: "Errors or blockers encountered"},
			"next_steps":      {Type: "array", Description: "Recommended next steps"},
		},
		[]string{"summary"},
		ot.SaveContext,
	)

	s.RegisterTool("obsidian_status",
		"Check the status of the Obsidian vault: number of files, types, last update time.",
		map[string]Property{},
		nil,
		ot.Status,
	)

	s.RegisterTool("obsidian_summarize",
		"Rebuild and retrieve the Obsidian vault summary showing recent activity and entry counts.",
		map[string]Property{},
		nil,
		ot.Summarize,
	)

	s.RegisterTool("obsidian_open",
		"Open the Obsidian vault in the Obsidian desktop application.",
		map[string]Property{},
		nil,
		ot.Open,
	)
}
