package root

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/DeusData/dwyt-orchestrator/internal/deps"
	"github.com/DeusData/dwyt-orchestrator/internal/detect"
	"github.com/DeusData/dwyt-orchestrator/internal/env"
	"github.com/DeusData/dwyt-orchestrator/internal/install"
	"github.com/DeusData/dwyt-orchestrator/internal/state"

	"github.com/spf13/cobra"
)

var installCmd = &cobra.Command{
	Use:   "install",
	Short: "Install DWYT tools interactively",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runInstall()
	},
}

func init() {
	installCmd.Flags().StringVarP(&toolFlag, "tool", "t", "", "Tools (c=codebase, r=rtk, h=headroom, m=memstack)")
	installCmd.Flags().StringVarP(&clientFlag, "client", "l", "", "Clients (c=claude, x=codex, p=copilot, k=kiro, r=cursor, o=opencode)")
	installCmd.Flags().StringVarP(&repoFlag, "repo", "R", "", "Repository path")
}

func runInstall() error {
	e := detect.Detect()
	fmt.Printf("\n  ╔══════════════════════════════════════════╗\n")
	fmt.Printf("  ║  DWYT — Don't Waste Your Tokens  v3.0  ║\n")
	fmt.Printf("  ╚══════════════════════════════════════════╝\n\n")
	fmt.Printf("  OS: %-8s  Shell: %-5s  Arch: %s\n\n", e.OS, e.Shell, e.Arch)

	state.SetPath(e.DwytHome)
	s, _ := state.Load()

	env.Init(e)

	if err := deps.Ensure(e); err != nil {
		return err
	}

	if toolFlag == "" {
		fmt.Print("Ferramentas: [c]bmcp [r]tk [h]eadroom [m]emstack (ex: crhm): ")
		fmt.Scanln(&toolFlag)
	}
	if clientFlag == "" {
		fmt.Print("Clientes: [c]laude code[x] co[p]ilot [k]iro cu[r]sor [o]pencode (ex: cxo): ")
		fmt.Scanln(&clientFlag)
	}
	if repoFlag == "" {
		cwd, _ := os.Getwd()
		fmt.Printf("Repositório [%s]: ", cwd)
		fmt.Scanln(&repoFlag)
		if repoFlag == "" {
			repoFlag = cwd
		}
	}

	doInstall(e, s)

	if repoFlag != "" {
		absRepo, _ := filepath.Abs(repoFlag)
		install.Integrate(e, absRepo, clientFlag, toolFlag)
		s.AddProject(absRepo, 0, 0)
	}

	env.Finalize(e, s)
	state.Save(s)

	fmt.Printf("\n  ✓ Instalação concluída.\n")
	fmt.Printf("  PATH: export PATH=\"%s:$PATH\"\n", e.DwytBin)
	fmt.Printf("  Recarregue: source %s\n", e.ShellRC)
	return nil
}

func doInstall(e *detect.Env, s *state.State) {
	for _, ch := range toolFlag {
		switch ch {
		case 'c':
			fmt.Println("  → codebase-memory-mcp...")
			if err := install.CBMCP(e); err != nil {
				fmt.Printf("    WARN: %v\n", err)
			}
			s.SetTool("cbmcp", state.ToolState{Installed: true})
		case 'r':
			fmt.Println("  → RTK...")
			if err := install.RTK(e); err != nil {
				fmt.Printf("    WARN: %v\n", err)
			}
			s.SetTool("rtk", state.ToolState{Installed: true})
		case 'h':
			fmt.Println("  → Headroom...")
			if err := install.Headroom(e); err != nil {
				fmt.Printf("    WARN: %v\n", err)
			}
			s.SetTool("headroom", state.ToolState{Installed: true, ProxyPort: 8787})
		case 'm':
			fmt.Println("  → MemStack...")
			if err := install.MemStack(e); err != nil {
				fmt.Printf("    WARN: %v\n", err)
			}
			s.SetTool("memstack", state.ToolState{Installed: true})
		}
	}

	clientMap := map[byte]string{'c': "claude", 'x': "codex", 'p': "copilot", 'k': "kiro", 'r': "cursor", 'o': "opencode"}
	for _, ch := range clientFlag {
		if name, ok := clientMap[byte(ch)]; ok {
			s.AddClient(name)
		}
	}
}
