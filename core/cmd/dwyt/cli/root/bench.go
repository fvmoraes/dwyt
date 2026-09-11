package root

import (
	"encoding/json"
	"fmt"

	"github.com/fvmoraes/dwyt/internal/benchmark"
	"github.com/spf13/cobra"
)

var benchJSON bool

// benchCmd runs the mandatory v5 benchmark (spec §69).
//
// It is a local, deterministic harness: no provider is called and no LLM is
// involved, so the same command always prints the same numbers. That is the
// point — a benchmark whose result moves between runs cannot tell you whether
// the optimizer regressed.
//
// What it will not do is hand you a savings claim. The report ends with
// claim_allowed: false and lists the KPIs it cannot observe, because spec §69
// requires evidence that the completion rate did not drop before any percentage
// is published, and a fixture cannot produce that evidence.
var benchCmd = &cobra.Command{
	Use:   "bench",
	Short: "Run the deterministic context/token benchmark (spec §69)",
	Long: "Measures input context, vault retrieval, tool output and relative cost across\n" +
		"four arms — baseline, DWYT v4, the v5 Optimizer, and the v5 Optimizer with\n" +
		"provider cache intelligence — over eleven deterministic scenarios, including\n" +
		"the mandatory compression-passthrough case.\n\n" +
		"Completion rate, real cost, cache hit rate and latency are NOT measured here.\n" +
		"They need a live agent loop and a real provider, so the report declares them\n" +
		"unmeasured instead of estimating them.",
	RunE: func(cmd *cobra.Command, args []string) error {
		report := benchmark.Run()
		if benchJSON {
			data, err := json.MarshalIndent(report, "", "  ")
			if err != nil {
				return err
			}
			fmt.Println(string(data))
			return nil
		}
		fmt.Print(report.Render())
		return nil
	},
}

func init() {
	benchCmd.Flags().BoolVar(&benchJSON, "json", false, "emit the full report as JSON")
	Cmd.AddCommand(benchCmd)
}
