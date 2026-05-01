package server

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"os/exec"
	"sync"
	"time"

	"github.com/DeusData/dwyt-orchestrator/internal/status"
	"github.com/gin-gonic/gin"
)

//go:embed dashboard/*
var dashboardFS embed.FS

type DashboardServer struct {
	Port     int
	DwytBin  string
	DwytHome string
	sseClients map[chan string]bool
	sseMu      sync.Mutex
}

func New(port int, dwytBin, dwytHome string) *DashboardServer {
	return &DashboardServer{
		Port:       port,
		DwytBin:    dwytBin,
		DwytHome:   dwytHome,
		sseClients: make(map[chan string]bool),
	}
}

func (ds *DashboardServer) Start() error {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())

	sub, _ := fs.Sub(dashboardFS, "dashboard")
	r.StaticFS("/static", http.FS(sub))
	r.GET("/", func(c *gin.Context) {
		c.FileFromFS("/", http.FS(sub))
	})

	api := r.Group("/api")
	{
		api.GET("/status", ds.apiStatus)
		api.GET("/metrics", ds.apiMetrics)
		api.GET("/events", ds.apiSSE)
		api.POST("/headroom/start", ds.apiHeadroomStart)
		api.POST("/headroom/stop", ds.apiHeadroomStop)
		api.GET("/rtk/gain", ds.apiRTKGain)
		api.POST("/codebase/index", ds.apiCodebaseIndex)
		api.POST("/memstack/search", ds.apiMemstackSearch)
	}

	go ds.broadcastLoop()

	addr := fmt.Sprintf("127.0.0.1:%d", ds.Port)
	fmt.Printf("   Dashboard → http://%s\n", addr)
	return r.Run(addr)
}

func (ds *DashboardServer) apiStatus(c *gin.Context) {
	c.JSON(200, status.PollAll(ds.DwytBin))
}

func (ds *DashboardServer) apiMetrics(c *gin.Context) {
	c.JSON(200, gin.H{
		"rtk":      status.GetRTKMetrics(ds.DwytBin),
		"headroom": status.GetHeadroomMetrics(),
	})
}

func (ds *DashboardServer) apiSSE(c *gin.Context) {
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("Access-Control-Allow-Origin", "*")

	ch := make(chan string, 10)
	ds.sseMu.Lock()
	ds.sseClients[ch] = true
	ds.sseMu.Unlock()

	defer func() {
		ds.sseMu.Lock()
		delete(ds.sseClients, ch)
		ds.sseMu.Unlock()
	}()

	for {
		select {
		case msg := <-ch:
			fmt.Fprintf(c.Writer, "event: status\ndata: %s\n\n", msg)
			c.Writer.Flush()
		case <-c.Request.Context().Done():
			return
		}
	}
}

func (ds *DashboardServer) broadcastLoop() {
	ticker := time.NewTicker(3 * time.Second)
	go func() {
		for range ticker.C {
			s := status.PollAll(ds.DwytBin)
			data, _ := json.Marshal(s)
			ds.sseMu.Lock()
			for ch := range ds.sseClients {
				select {
				case ch <- string(data):
				default:
				}
			}
			ds.sseMu.Unlock()
		}
	}()
}

func (ds *DashboardServer) apiHeadroomStart(c *gin.Context) {
	cmd := exec.Command(ds.DwytBin+"/headroom", "proxy", "--port", "8787")
	if err := cmd.Start(); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	go cmd.Wait()
	time.Sleep(1 * time.Second)
	c.JSON(200, gin.H{"status": "started", "port": "8787"})
}

func (ds *DashboardServer) apiHeadroomStop(c *gin.Context) {
	out, _ := exec.Command("pkill", "-f", "headroom proxy --port 8787").CombinedOutput()
	c.JSON(200, gin.H{"status": "stopped", "output": string(out)})
}

func (ds *DashboardServer) apiRTKGain(c *gin.Context) {
	c.JSON(200, status.GetRTKMetrics(ds.DwytBin))
}

func (ds *DashboardServer) apiCodebaseIndex(c *gin.Context) {
	var body struct {
		Path string `json:"path"`
	}
	if err := c.BindJSON(&body); err != nil || body.Path == "" {
		c.JSON(400, gin.H{"error": "path is required"})
		return
	}

	cmd := exec.Command(ds.DwytBin+"/codebase-memory-mcp", "cli", "index_repository",
		fmt.Sprintf(`{"repo_path":"%s"}`, body.Path))
	out, err := cmd.CombinedOutput()
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error(), "output": string(out)})
		return
	}
	c.JSON(200, gin.H{"status": "indexed", "path": body.Path})
}

func (ds *DashboardServer) apiMemstackSearch(c *gin.Context) {
	var body struct {
		Query string `json:"query"`
	}
	if err := c.BindJSON(&body); err != nil || body.Query == "" {
		c.JSON(400, gin.H{"error": "query is required"})
		return
	}

	cmd := exec.Command(ds.DwytBin+"/memstack", "search", body.Query)
	out, _ := cmd.CombinedOutput()
	c.JSON(200, gin.H{"results": string(out)})
}
