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
	if isGovernorMCPInvocation() {
		runGovernorMCP()
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

// isGovernorMCPInvocation matches the DWYT MCP (the Context Governor), the
// third official MCP alongside Obsidian and Codebase.
func isGovernorMCPInvocation() bool {
	name := strings.TrimSuffix(filepath.Base(os.Args[0]), ".exe")
	return name == "dwyt-governor-mcp" || (len(os.Args) > 1 && os.Args[1] == "governor-mcp")
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

func runGovernorMCP() {
	if apiURL := os.Getenv("DWYT_API_URL"); apiURL != "" {
		mcp.SetAPIBase(apiURL)
	}

	server := mcp.NewServer("dwyt", "5.0.0")
	mcp.RegisterGovernorTools(server)

	if err := server.Run(); err != nil {
		os.Exit(1)
	}
}
