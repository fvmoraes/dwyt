package cli

import (
	"github.com/fvmoraes/dwyt/cmd/dwyt/cli/root"
)

var version = "dev"

func Execute() error {
	root.SetVersion(version)
	return root.Cmd.Execute()
}

func SetVersion(v string) {
	version = v
}
