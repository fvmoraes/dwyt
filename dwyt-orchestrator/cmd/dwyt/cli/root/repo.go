package root

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/DeusData/dwyt-orchestrator/internal/detect"
	"github.com/DeusData/dwyt-orchestrator/internal/install"
	"github.com/DeusData/dwyt-orchestrator/internal/state"

	"github.com/spf13/cobra"
)

var repoCmd = &cobra.Command{
	Use:   "repo [path]",
	Short: "Integrate and index a repository",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		e := detect.Detect()
		rp := "."
		if len(args) > 0 {
			rp = args[0]
		}
		absPath, err := filepath.Abs(rp)
		if err != nil {
			return fmt.Errorf("invalid path: %w", err)
		}

		state.SetPath(e.DwytHome)
		s, _ := state.Load()

		install.Integrate(e, absPath, "cxpko", "crhm")
		s.AddProject(absPath, 0, 0)
		state.Save(s)

		fmt.Printf("  ✓ opencode.json, .mcp.json, AGENTS.md criados em %s\n", absPath)
		return nil
	},
}

var reinstallCmd = &cobra.Command{
	Use:   "reinstall",
	Short: "Reinstall all tools from scratch",
	RunE: func(cmd *cobra.Command, args []string) error {
		e := detect.Detect()
		fmt.Printf("  Apagando %s...\n", e.DwytHome)
		if err := os.RemoveAll(e.DwytHome); err != nil && !os.IsNotExist(err) {
			return err
		}
		fmt.Printf("  ✓ Removido. Execute 'dwyt install' para reinstalar.\n")
		return nil
	},
}

var uninstallCmd = &cobra.Command{
	Use:   "uninstall",
	Short: "Remove all DWYT tools and config",
	RunE: func(cmd *cobra.Command, args []string) error {
		e := detect.Detect()
		os.RemoveAll(e.DwytHome)
		fmt.Println("  ✓ Desinstalação concluída.")
		return nil
	},
}
