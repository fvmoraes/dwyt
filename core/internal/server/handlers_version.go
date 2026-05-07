package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

const (
	dwytLatestReleaseURL = "https://api.github.com/repos/fvmoraes/dwyt/releases/latest"
	dwytInstallCommand   = "curl -fsSL https://raw.githubusercontent.com/fvmoraes/dwyt/main/install.sh | bash"
)

type githubRelease struct {
	TagName string `json:"tag_name"`
	HTMLURL string `json:"html_url"`
}

func (ds *DashboardServer) apiVersionCheck(c *gin.Context) {
	current := ds.currentReleaseVersion()
	out := gin.H{
		"current":          current,
		"latest":           "",
		"update_available": false,
		"install_command":  dwytInstallCommand,
		"release_url":      "",
	}

	if isDevRelease(current) {
		c.JSON(200, out)
		return
	}

	req, err := http.NewRequestWithContext(c.Request.Context(), http.MethodGet, dwytLatestReleaseURL, nil)
	if err != nil {
		out["error"] = err.Error()
		c.JSON(200, out)
		return
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "dwyt-dashboard")

	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		out["error"] = err.Error()
		c.JSON(200, out)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		out["error"] = fmt.Sprintf("release check returned HTTP %d", resp.StatusCode)
		c.JSON(200, out)
		return
	}

	var release githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		out["error"] = err.Error()
		c.JSON(200, out)
		return
	}

	latest := normalizeReleaseVersion(release.TagName)
	out["latest"] = latest
	out["release_url"] = release.HTMLURL
	out["update_available"] = versionGreater(latest, current)
	c.JSON(200, out)
}

func (ds *DashboardServer) currentReleaseVersion() string {
	if ds.ReleaseVersion != "" {
		return normalizeReleaseVersion(ds.ReleaseVersion)
	}
	if ds.RuntimeState != nil {
		if v, ok := ds.RuntimeState.Snapshot()["version"].(string); ok && v != "" {
			return normalizeReleaseVersion(v)
		}
	}
	return "dev"
}

func normalizeReleaseVersion(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return "dev"
	}
	if isDevRelease(v) {
		return "dev"
	}
	if strings.HasPrefix(v, "v") || strings.HasPrefix(v, "V") {
		return "v" + strings.TrimPrefix(strings.TrimPrefix(v, "v"), "V")
	}
	return "v" + v
}

func isDevRelease(v string) bool {
	v = strings.TrimSpace(strings.ToLower(v))
	return v == "" || v == "dev" || v == "development"
}

func versionGreater(candidate, current string) bool {
	candidateParts, ok := parseReleaseVersion(candidate)
	if !ok {
		return false
	}
	currentParts, ok := parseReleaseVersion(current)
	if !ok {
		return false
	}
	for i := range candidateParts {
		if candidateParts[i] > currentParts[i] {
			return true
		}
		if candidateParts[i] < currentParts[i] {
			return false
		}
	}
	return false
}

func parseReleaseVersion(v string) ([3]int, bool) {
	var out [3]int
	v = strings.TrimSpace(strings.ToLower(v))
	v = strings.TrimPrefix(v, "v")
	if isDevRelease(v) {
		return out, false
	}

	parts := strings.Split(v, ".")
	for i := 0; i < len(out) && i < len(parts); i++ {
		part := parts[i]
		digits := 0
		for digits < len(part) && part[digits] >= '0' && part[digits] <= '9' {
			digits++
		}
		if digits == 0 {
			return out, false
		}
		n, err := strconv.Atoi(part[:digits])
		if err != nil {
			return out, false
		}
		out[i] = n
	}
	return out, true
}
