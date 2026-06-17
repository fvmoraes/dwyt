package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestLocalOriginGuard(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	api := r.Group("/api")
	api.Use(localOriginGuard(2737))
	api.POST("/x", func(c *gin.Context) { c.JSON(200, gin.H{"ok": true}) })

	cases := []struct {
		name   string
		host   string
		origin string
		want   int
	}{
		{"loopback host, no origin (CLI/MCP)", "127.0.0.1:2737", "", 200},
		{"localhost host, same origin (SPA)", "localhost:2737", "http://localhost:2737", 200},
		{"rebinding host blocked", "evil.example.com", "", 403},
		{"cross-origin blocked", "127.0.0.1:2737", "https://evil.example.com", 403},
		{"wrong port host blocked", "127.0.0.1:9999", "", 403},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/x", nil)
			req.Host = tc.host
			if tc.origin != "" {
				req.Header.Set("Origin", tc.origin)
			}
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)
			if rec.Code != tc.want {
				t.Fatalf("%s: got %d, want %d", tc.name, rec.Code, tc.want)
			}
		})
	}
}
