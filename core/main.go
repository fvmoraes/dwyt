package main

import (
	"os"

	"github.com/fvmoraes/dwyt/cmd/dwyt/cli"
)

func main() {
	if err := cli.Execute(); err != nil {
		os.Exit(1)
	}
}
