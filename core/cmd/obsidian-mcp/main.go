package main

import (
	"os"

	"github.com/fvmoraes/dwyt/internal/mcp"
)

func main() {
	if apiURL := os.Getenv("DWYT_API_URL"); apiURL != "" {
		mcp.SetAPIBase(apiURL)
	}

	server := mcp.NewServer("dwyt-obsidian", "1.0.0")
	mcp.RegisterObsidianTools(server)

	if err := server.Run(); err != nil {
		os.Exit(1)
	}
}
