package root

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/fvmoraes/dwyt/internal/dwytconfig"
	"github.com/spf13/cobra"
)

var configForce bool

// configCmd groups the DWYT v5 configuration commands (spec §57).
var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Inspect or scaffold the DWYT configuration",
}

// configShowCmd prints the effective configuration.
//
// "Effective" rather than "the file": defaults are merged in, so what is printed
// is what DWYT will actually use. Printing the raw file would hide the defaults
// filling every unset field.
var configShowCmd = &cobra.Command{
	Use:   "show",
	Short: "Print the effective configuration (defaults merged with the file)",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, loadErr := dwytconfig.Load(DwytHome)
		data, err := json.MarshalIndent(&cfg, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(data))
		fmt.Fprintf(os.Stderr, "\nsource: %s\n", cfg.Source())
		if loadErr != nil {
			// The config was unusable and defaults are in force. Exit non-zero so
			// a script notices; the user still gets to see what will be used.
			return loadErr
		}
		return nil
	},
}

// configInitCmd writes a complete configuration file.
//
// It writes every field, not a minimal stub: the point is to give the user
// something to edit rather than something to reconstruct from the docs.
var configInitCmd = &cobra.Command{
	Use:   "init",
	Short: "Write a complete configuration file with the recommended defaults",
	RunE: func(cmd *cobra.Command, args []string) error {
		path := dwytconfig.Path(DwytHome)
		if _, err := os.Stat(path); err == nil && !configForce {
			// Never overwrite a user's tuning by accident.
			return fmt.Errorf("%s already exists; pass --force to overwrite", path)
		}
		if err := dwytconfig.Save(DwytHome, dwytconfig.Default()); err != nil {
			return err
		}
		fmt.Printf("wrote %s\n", path)
		return nil
	},
}

// configValidateCmd checks the file without applying it.
var configValidateCmd = &cobra.Command{
	Use:   "validate",
	Short: "Validate the configuration file",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := dwytconfig.Load(DwytHome)
		if err != nil {
			return err
		}
		if cfg.Source() == "default" {
			fmt.Printf("no configuration file at %s; the recommended defaults are in use\n",
				dwytconfig.Path(DwytHome))
			return nil
		}
		fmt.Printf("%s is valid (schema %s)\n", cfg.Source(), cfg.Version)
		return nil
	},
}

func init() {
	configInitCmd.Flags().BoolVar(&configForce, "force", false, "overwrite an existing configuration file")
	configCmd.AddCommand(configShowCmd, configInitCmd, configValidateCmd)
	Cmd.AddCommand(configCmd)
}
