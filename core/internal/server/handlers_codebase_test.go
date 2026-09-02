package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/fvmoraes/dwyt/internal/procman"
	"github.com/gin-gonic/gin"
)

func TestStopCodebaseForOpenUIReportsStopError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	home := t.TempDir()
	ds := &DashboardServer{
		ProcMan: procman.New(home),
	}

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/codebase/open-ui", nil)
	if ds.stopCodebaseForOpenUI(c) {
		t.Fatal("stopCodebaseForOpenUI returned success after Stop failed")
	}

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("open UI status = %d, want 500; body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Status string `json:"status"`
		Error  string `json:"error"`
		URL    string `json:"url"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Status != "error" || body.Error == "" || body.URL != "" {
		t.Fatalf("open UI response = %+v, want explicit stop error", body)
	}
}
