package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/DeusData/core/cmd/dwyt/cli"
)

func main() {
	base := filepath.Base(os.Args[0])
	switch base {
	case "dwyt-opencode":
		fmt.Println("dwyt-opencode: launching OpenCode with Headroom...")
		os.Exit(0)
	case "dwyt-codex":
		fmt.Println("dwyt-codex: launching Codex with Headroom...")
		os.Exit(0)
	case "dwyt-ui":
		stop := len(os.Args) > 1 && strings.Contains(os.Args[1], "stop")
		if stop {
			fmt.Println("dwyt-ui: stopping UI...")
		} else {
			fmt.Println("dwyt-ui: starting UI at http://localhost:9749")
		}
		os.Exit(0)
	}

	if err := cli.Execute(); err != nil {
		os.Exit(1)
	}
}
