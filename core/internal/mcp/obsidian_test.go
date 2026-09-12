package mcp

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestObsidianSearchForwardsOptionalBudgetParameters(t *testing.T) {
	var gotPath string
	var gotQuery string
	var gotTaskID string
	var gotMaxTokens string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.Query().Get("q")
		gotTaskID = r.URL.Query().Get("task_id")
		gotMaxTokens = r.URL.Query().Get("max_tokens")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results":[{"title":"match"}],"count":1}`))
	}))
	defer server.Close()

	previous := dwytAPI
	SetAPIBase(server.URL)
	t.Cleanup(func() { SetAPIBase(previous) })

	result, err := NewObsidianTools().Search(map[string]interface{}{
		"query":      "prompt cache",
		"task_id":    "task-42",
		"max_tokens": 321,
	})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if gotPath != "/obsidian/search" {
		t.Fatalf("path = %q, want /obsidian/search", gotPath)
	}
	if gotQuery != "prompt cache" || gotTaskID != "task-42" || gotMaxTokens != "321" {
		t.Fatalf("forwarded query = (%q, %q, %q)", gotQuery, gotTaskID, gotMaxTokens)
	}
	if !strings.Contains(result, "match") {
		t.Fatalf("unexpected search result: %s", result)
	}
}

func TestObsidianSearchReturnsHTTPFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusConflict)
	}))
	defer server.Close()

	previous := dwytAPI
	SetAPIBase(server.URL)
	t.Cleanup(func() { SetAPIBase(previous) })

	if _, err := NewObsidianTools().Search(map[string]interface{}{"query": "prompt cache"}); err == nil || !strings.Contains(err.Error(), "HTTP 409") {
		t.Fatalf("Search() error = %v, want HTTP status error", err)
	}
}
