package cli

import (
	"os"

	"github.com/DeusData/dwyt-orchestrator/cmd/dwyt/cli/root"
)

func Execute() error {
	return root.Cmd.Execute()
}

func getHome() string {
	h, _ := os.UserHomeDir()
	if h == "" {
		h = os.Getenv("HOME")
	}
	return h
}
