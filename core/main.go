package main

import (
	"os"

	"github.com/fvmoraes/dwyt/cmd/dwyt/cli"
)

var version = "dev"

func main() {
	cli.SetVersion(version)
	if err := cli.Execute(); err != nil {
		os.Exit(1)
	}
}
