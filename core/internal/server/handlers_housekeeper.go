package server

import (
	"net/http"
	"strings"

	"github.com/fvmoraes/dwyt/internal/brain"
	"github.com/fvmoraes/dwyt/internal/housekeeper"
	"github.com/fvmoraes/dwyt/internal/telemetry"
	"github.com/gin-gonic/gin"
)

// HTTP surface for the Housekeeper and canonical memory (spec §14, §20–§24).

// apiHousekeeperStatus reports Brain lifecycle health.
func (ds *DashboardServer) apiHousekeeperStatus(c *gin.Context) {
	if ds.Housekeeper == nil {
		c.JSON(http.StatusOK, gin.H{"enabled": false, "reason": "housekeeper not initialized"})
		return
	}
	c.JSON(http.StatusOK, ds.Housekeeper.HousekeeperStatus())
}

// apiHousekeeperRun triggers a pass.
//
// `depth=light|deep` selects the pass; `dry_run=true` computes the outcome and
// changes nothing, which is how the dashboard previews a cleanup before the user
// commits to it. Deletion is irreversible, so a preview is not a nicety.
func (ds *DashboardServer) apiHousekeeperRun(c *gin.Context) {
	if ds.Housekeeper == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "housekeeper not initialized"})
		return
	}
	depth := housekeeper.Deep
	if strings.EqualFold(c.Query("depth"), "light") {
		depth = housekeeper.Light
	}

	dryRun := c.Query("dry_run") == "true"

	var report housekeeper.Report
	run := func() error {
		if dryRun {
			report = ds.Housekeeper.RunDry(depth)
			return nil
		}
		report = ds.Housekeeper.Run(depth)
		return nil
	}
	if ds.Governor != nil {
		// Housekeeping is a pipeline phase (spec §54), so it belongs in a trace
		// alongside the retrieval and LLM spans.
		_ = ds.Governor.TraceSpan(telemetry.SpanHousekeeper,
			map[string]interface{}{"depth": string(depth), "dry_run": dryRun}, run)
	} else {
		_ = run()
	}

	// Persisting the report gives the dashboard a housekeeping history rather
	// than only the most recent in-memory pass.
	if ds.Telemetry != nil && !dryRun {
		_ = ds.Telemetry.RecordHousekeeperRun(ds.currentProjectID(), string(depth), report)
	}
	c.JSON(http.StatusOK, report)
}

// apiCanonicalList returns the canonical memory notes, HOT first (spec §17).
//
// The response carries token estimates so a caller can decide what to load
// without fetching bodies it will then discard.
func (ds *DashboardServer) apiCanonicalList(c *gin.Context) {
	pb := ds.projectObsidian()
	if pb == nil {
		c.JSON(http.StatusOK, gin.H{"notes": []interface{}{}, "note": "no Obsidian vault"})
		return
	}
	includeBody := c.Query("include_body") == "true"

	notes := pb.CanonicalMemory(brain.ParseTemperature(c.Query("temperature")))
	if !includeBody {
		for i := range notes {
			notes[i].Body = ""
		}
	}
	tokens := 0
	for _, n := range notes {
		tokens += n.TokensEst
	}
	c.JSON(http.StatusOK, gin.H{"notes": notes, "count": len(notes), "tokens_est": tokens})
}

// apiCanonicalUpsert writes or updates one canonical note.
func (ds *DashboardServer) apiCanonicalUpsert(c *gin.Context) {
	pb := ds.projectObsidian()
	if pb == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "no Obsidian vault loaded"})
		return
	}
	var body struct {
		Key        string `json:"key"`
		Title      string `json:"title"`
		Body       string `json:"body"`
		SourceFile string `json:"source_file"`
		// Bullet appends a single deduplicated list entry instead of replacing
		// the note. It is how the compiler and agents add lessons.
		Bullet string `json:"bullet"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if strings.TrimSpace(body.Key) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "key is required"})
		return
	}

	if strings.TrimSpace(body.Bullet) != "" {
		added, err := pb.AppendCanonicalBullet(body.Key, body.Bullet)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"key": body.Key, "added": added,
			"note": map[string]bool{"deduplicated": !added}})
		return
	}
	if strings.TrimSpace(body.Body) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "body or bullet is required"})
		return
	}

	source := brain.SourceRef{}
	if body.SourceFile != "" {
		source = brain.SourceOf(body.SourceFile)
	}
	note, err := pb.UpsertCanonical(body.Key, body.Title, body.Body, source)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"key": note.Key, "path": note.Path, "tokens_est": note.TokensEst})
}

// apiMemoryCompile promotes the reusable knowledge in a compact snapshot into
// canonical memory without persisting the session itself. Agents call it when a
// phase ends but the task continues.
func (ds *DashboardServer) apiMemoryCompile(c *gin.Context) {
	pb := ds.projectObsidian()
	if pb == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "no Obsidian vault loaded"})
		return
	}
	var snapshot brain.CompactSnapshot
	if err := c.ShouldBindJSON(&snapshot); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if snapshot.Empty() {
		c.JSON(http.StatusBadRequest, gin.H{"error": "nothing to compile"})
		return
	}

	var result brain.PromotionResult
	compile := func() error {
		result = pb.Compile(snapshot)
		return nil
	}
	if ds.Governor != nil {
		_ = ds.Governor.TraceSpan(telemetry.SpanMemoryCompile, nil, compile)
	} else {
		_ = compile()
	}
	c.JSON(http.StatusOK, result)
}
