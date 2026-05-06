package main

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/fvmoraes/dwyt/cmd/dwyt/cli"
	"github.com/fvmoraes/dwyt/internal/mcp"
)

var version = "dev"

func main() {
	if isObsidianMCPInvocation() {
		runObsidianMCP()
		return
	}

	cli.SetVersion(version)
	if err := cli.Execute(); err != nil {
		os.Exit(1)
	}
}

func isObsidianMCPInvocation() bool {
	name := strings.TrimSuffix(filepath.Base(os.Args[0]), ".exe")
	return name == "dwyt-obsidian-mcp" || (len(os.Args) > 1 && os.Args[1] == "obsidian-mcp")
}

func runObsidianMCP() {
	if apiURL := os.Getenv("DWYT_API_URL"); apiURL != "" {
		mcp.SetAPIBase(apiURL)
	}

	server := mcp.NewServer("dwyt-obsidian", "1.0.0")
	mcp.RegisterObsidianTools(server)

	if err := server.Run(); err != nil {
		os.Exit(1)
	}
}
