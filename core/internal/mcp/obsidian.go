package mcp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
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
	resp, err := ot.client.Get(fmt.Sprintf("%s/obsidian/search?q=%s", dwytAPI, query))
	if err != nil {
		return "", fmt.Errorf("obsidian search failed: %w", err)
	}
	defer resp.Body.Close()
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
	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)
	if status, ok := result["status"].(string); ok {
		return fmt.Sprintf("Entry saved: %s", status), nil
	}
	return "Entry saved", nil
}

func (ot *ObsidianTools) Status(args map[string]interface{}) (string, error) {
	resp, err := ot.client.Get(fmt.Sprintf("%s/obsidian/status", dwytAPI))
	if err != nil {
		return "", fmt.Errorf("status check failed: %w", err)
	}
	defer resp.Body.Close()
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
	return "Obsidian vault opened", nil
}

func RegisterObsidianTools(s *Server) {
	ot := NewObsidianTools()

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
