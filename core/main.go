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
	// The MCP servers are served by this same binary through dedicated
	// subcommands, so an install never needs renamed copies of dwyt on disk.
	// The dispatch happens before Cobra so the stdio protocol owns stdout
	// exclusively — a Cobra usage banner on stdout would corrupt the JSON-RPC
	// stream and the client would drop the connection.
	if isOptimizerMCPInvocation() {
		runOptimizerMCP()
		return
	}
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

// isOptimizerMCPInvocation matches the DWYT Optimizer MCP, one of the three
// official MCPs alongside dwyt_obsidian and dwyt_codebase.
func isOptimizerMCPInvocation() bool {
	name := strings.TrimSuffix(filepath.Base(os.Args[0]), ".exe")
	return name == "dwyt-optimizer-mcp" || (len(os.Args) > 1 && os.Args[1] == "optimizer-mcp")
}

func runObsidianMCP() {
	if apiURL := os.Getenv("DWYT_API_URL"); apiURL != "" {
		mcp.SetAPIBase(apiURL)
	}

	server := mcp.NewServer("dwyt_obsidian", "1.0.0")
	mcp.RegisterObsidianTools(server)

	if err := server.Run(); err != nil {
		os.Exit(1)
	}
}

func runOptimizerMCP() {
	if apiURL := os.Getenv("DWYT_API_URL"); apiURL != "" {
		mcp.SetAPIBase(apiURL)
	}

	server := mcp.NewServer("dwyt_optimizer", "5.0.0")
	mcp.RegisterOptimizerTools(server)

	if err := server.Run(); err != nil {
		os.Exit(1)
	}
}
