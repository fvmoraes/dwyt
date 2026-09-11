package kiropow

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"time"

	"github.com/fvmoraes/dwyt/internal/mcpregistry"
)

func TestEnsurePower_FirstRun(t *testing.T) {
	dwytHome, dwytBin := tempPowerEnv(t)
	status, err := EnsurePower(dwytHome, dwytBin, "/tmp/project")
	if err != nil {
		t.Fatalf("EnsurePower returned error: %v", err)
	}
	if !status.Installed {
		t.Fatal("expected installed power")
	}
	if status.ActivationStatus != "linked" {
		t.Fatalf("expected linked activation status, got %s", status.ActivationStatus)
	}
	for _, rel := range []string{"POWER.md", "mcp.json", "steering/dwyt-context.md"} {
		if _, err := os.Stat(filepath.Join(status.PowerDir, rel)); err != nil {
			t.Fatalf("missing %s: %v", rel, err)
		}
	}
	powerMD, _ := os.ReadFile(filepath.Join(status.PowerDir, "POWER.md"))
	if !strings.HasPrefix(string(powerMD), "---\nname: dwyt-power") {
		t.Fatalf("expected Kiro Power frontmatter, got:\n%s", string(powerMD))
	}
	if !strings.Contains(string(powerMD), "displayName: DWYT Project Context") {
		t.Fatalf("expected DWYT Project Context display name, got:\n%s", string(powerMD))
	}
	// v5 entry contract: the Power names the three MCPs and points at the
	// Optimizer instead of restating the whole policy (spec §4, §60.3).
	for _, want := range []string{
		"**dwyt_optimizer** — the Context Optimizer",
		"**dwyt_obsidian** — the Brain",
		"**dwyt_codebase** — Code Intelligence",
		"`dwyt_context_plan`",
		"RTK compresses terminal output. It is a CLI tool, not a fourth MCP.",
	} {
		if !strings.Contains(string(powerMD), want) {
			t.Fatalf("expected POWER.md to contain %q, got:\n%s", want, string(powerMD))
		}
	}
	for _, forbidden := range []string{"## Codebase Law", "## Obsidian Law", "Required completion payload"} {
		if strings.Contains(string(powerMD), forbidden) {
			t.Fatalf("POWER.md still duplicates optimizer policy (%q):\n%s", forbidden, string(powerMD))
		}
	}
}

func TestEnsurePower_Idempotent(t *testing.T) {
	dwytHome, dwytBin := tempPowerEnv(t)
	status, err := EnsurePower(dwytHome, dwytBin, "/tmp/project")
	if err != nil {
		t.Fatal(err)
	}
	powerMD := filepath.Join(status.PowerDir, "POWER.md")
	first, _ := os.Stat(powerMD)
	time.Sleep(10 * time.Millisecond)
	if _, err := EnsurePower(dwytHome, dwytBin, "/tmp/project"); err != nil {
		t.Fatal(err)
	}
	second, _ := os.Stat(powerMD)
	if !first.ModTime().Equal(second.ModTime()) {
		t.Fatal("POWER.md was rewritten despite identical content")
	}
}

func TestEnsurePower_KiroEnabled(t *testing.T) {
	cfg := map[string]interface{}{"ias": []interface{}{"codex", "kiro"}}
	if !IsKiroEnabled(cfg) {
		t.Fatal("expected Kiro enabled")
	}
}

func TestEnsurePower_KiroDisabled(t *testing.T) {
	cfg := map[string]interface{}{"ias": []interface{}{"codex"}}
	if IsKiroEnabled(cfg) {
		t.Fatal("expected Kiro disabled")
	}
}

func TestEnsurePower_MissingMCP(t *testing.T) {
	dwytHome := t.TempDir()
	dwytBin := filepath.Join(t.TempDir(), "bin")
	t.Setenv("HOME", t.TempDir())
	status, err := EnsurePower(dwytHome, dwytBin, "/tmp/project")
	if err != nil {
		t.Fatal(err)
	}
	if status.MCPs["codebase"] || status.MCPs["obsidian"] {
		t.Fatal("expected missing MCP binaries to be false")
	}
	data, _ := os.ReadFile(filepath.Join(status.PowerDir, "mcp.json"))
	if strings.Contains(string(data), "codebase-memory-mcp") {
		t.Fatal("missing codebase binary should not be included in mcp.json")
	}
}

func TestEnsurePower_NewProject(t *testing.T) {
	dwytHome, dwytBin := tempPowerEnv(t)
	project := filepath.Join(t.TempDir(), "new-project")
	status, err := EnsurePower(dwytHome, dwytBin, project)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(status.PowerDir, "steering", "obsidian.md"))
	if !strings.Contains(string(data), project) {
		t.Fatal("expected project path in obsidian steering")
	}
}

func TestEnsurePower_ExistingProject(t *testing.T) {
	dwytHome, dwytBin := tempPowerEnv(t)
	project := filepath.Join(t.TempDir(), "existing-project")
	if _, err := EnsurePower(dwytHome, dwytBin, project); err != nil {
		t.Fatal(err)
	}
	if _, err := EnsurePower(dwytHome, dwytBin, project); err != nil {
		t.Fatalf("second EnsurePower failed: %v", err)
	}
}

func TestEnsurePower_VaultProtection(t *testing.T) {
	dwytHome, dwytBin := tempPowerEnv(t)
	vaultFile := filepath.Join(dwytHome, "projects", "abc", "obsidian", "keep.md")
	if err := os.MkdirAll(filepath.Dir(vaultFile), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(vaultFile, []byte("keep"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := EnsurePower(dwytHome, dwytBin, "/tmp/project"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(vaultFile); err != nil {
		t.Fatalf("vault file was removed: %v", err)
	}
}

func TestRegisterWithKiro_CreatesSymlink(t *testing.T) {
	home := t.TempDir()
	// os.UserHomeDir reads USERPROFILE on Windows (HOME is ignored there),
	// so set both to route kiroLinkPath into the temp dir.
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	powerDir := filepath.Join(t.TempDir(), "dwyt-power")
	if err := os.MkdirAll(powerDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := RegisterWithKiro(powerDir); err != nil {
		t.Fatal(err)
	}
	target, err := os.Readlink(filepath.Join(home, ".kiro", "powers", "dwyt-power"))
	if err != nil {
		t.Fatal(err)
	}
	if target != powerDir {
		t.Fatalf("symlink target = %s, want %s", target, powerDir)
	}
}

func TestRegisterWithKiro_Idempotent(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	powerDir := filepath.Join(t.TempDir(), "dwyt-power")
	if err := os.MkdirAll(powerDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := RegisterWithKiro(powerDir); err != nil {
		t.Fatal(err)
	}
	if err := RegisterWithKiro(powerDir); err != nil {
		t.Fatal(err)
	}
}

func TestNeedsUpdate_BinaryChanged(t *testing.T) {
	dwytHome := t.TempDir()
	dwytBin := filepath.Join(t.TempDir(), "bin")
	t.Setenv("HOME", t.TempDir())
	status, err := EnsurePower(dwytHome, dwytBin, "/tmp/project")
	if err != nil {
		t.Fatal(err)
	}
	touchBin(t, dwytBin, executableName("codebase-memory-mcp"))
	if !NeedsUpdate(status.PowerDir, dwytBin) {
		t.Fatal("expected update after MCP binary appeared")
	}
}

func TestNeedsUpdate_MissingFile(t *testing.T) {
	dwytHome, dwytBin := tempPowerEnv(t)
	status, err := EnsurePower(dwytHome, dwytBin, "/tmp/project")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(status.PowerDir, "steering", "rtk.md")); err != nil {
		t.Fatal(err)
	}
	if !NeedsUpdate(status.PowerDir, dwytBin) {
		t.Fatal("expected update after steering file removal")
	}
}

func TestGenerateMCPJSON_OnlyExistingBinaries(t *testing.T) {
	dwytBin := "/tmp/bin"
	data, err := GenerateMCPJSON(dwytBin, map[string]bool{
		mcpregistry.ServerCodebase: true,
		mcpregistry.ServerObsidian: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(data, "codebase-memory-mcp") {
		t.Fatal("expected codebase MCP")
	}
	if strings.Contains(data, "\"obsidian-mcp\"") {
		t.Fatal("did not expect obsidian MCP")
	}
}

func TestGenerateMCPJSON_ObsidianUsesCanonicalCommand(t *testing.T) {
	dwytBin := "/tmp/bin"
	data, err := GenerateMCPJSON(dwytBin, map[string]bool{mcpregistry.ServerCodebase: true, mcpregistry.ServerObsidian: true})
	if err != nil {
		t.Fatal(err)
	}
	// Compare with the platform-correct joined path — on Windows
	// filepath.Join produces backslashes and the .exe suffix.
	wantCmd := filepath.Join(dwytBin, executableName("dwyt"))
	if !strings.Contains(data, `"command": `+strconv.Quote(wantCmd)) {
		t.Fatalf("expected canonical command %s, got: %s", wantCmd, data)
	}
	if !strings.Contains(data, `"obsidian-mcp"`) {
		t.Fatalf("expected obsidian-mcp subcommand arg, got: %s", data)
	}
	if !strings.Contains(data, `"DWYT_API_URL"`) {
		t.Fatalf("expected DWYT_API_URL env, got: %s", data)
	}
	if strings.Contains(data, "dwyt-obsidian-mcp") {
		t.Fatalf("legacy binary name must not appear: %s", data)
	}
}

// v5 moves the per-tool detail to manual inclusion: only the small, stable
// context contract is loaded on every turn (spec §4). Everything else is
// fetched on demand from the Optimizer or by explicitly referencing the file.
func TestSteeringUsesStageScopedGuidance(t *testing.T) {
	context := steeringContext()
	if !strings.Contains(context, "inclusion: always") {
		t.Fatalf("context steering must stay always-included, got:\n%s", context)
	}
	for _, want := range []string{
		"## Stage-Scoped Collaboration",
		"There is no universal order of tools.",
		"`dwyt_context_plan`",
		"canonical project knowledge",
		"current code-structure questions",
		"When a shell operation is needed, prefix it with `rtk`.",
		"`dwyt_compact_tool_output`",
		"Headroom only as a compatible transport proxy",
	} {
		if !strings.Contains(context, want) {
			t.Errorf("context steering is missing stage-scoped guidance %q", want)
		}
	}
	if strings.Contains(context, "## Order of Operations") {
		t.Fatalf("context steering still presents a global tool order:\n%s", context)
	}
	for name, content := range map[string]string{
		"obsidian": steeringObsidian("/tmp/project"),
		"codebase": steeringCodebase(),
		"rtk":      steeringRTK(),
		"headroom": steeringHeadroom(),
	} {
		if !strings.Contains(content, "inclusion: manual") {
			t.Fatalf("%s steering should be manual in v5, got:\n%s", name, content)
		}
	}
	// Size guard on the always-included file: it is injected into every turn.
	const maxAlwaysBytes = 1600
	if got := len(context); got > maxAlwaysBytes {
		t.Fatalf("always-included steering grew to %d bytes (max %d); "+
			"detailed policy belongs in the DWYT MCP", got, maxAlwaysBytes)
	}
}

func TestValidateMCPBinaries(t *testing.T) {
	_, dwytBin := tempPowerEnv(t)
	mcps := ValidateMCPBinaries(dwytBin)
	if !mcps[mcpregistry.ServerCodebase] || !mcps[mcpregistry.ServerObsidian] || !mcps[mcpregistry.ServerOptimizer] {
		t.Fatalf("expected all three MCPs present: %#v", mcps)
	}
}

// The Kiro Power must configure the Optimizer as a first-class MCP.
func TestGenerateMCPJSON_IncludesOptimizer(t *testing.T) {
	dwytBin := "/tmp/bin"
	data, err := GenerateMCPJSON(dwytBin, map[string]bool{mcpregistry.ServerOptimizer: true, mcpregistry.ServerCodebase: true, mcpregistry.ServerObsidian: true})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(data, `"optimizer-mcp"`) {
		t.Fatalf("expected optimizer-mcp subcommand arg, got: %s", data)
	}
	if !strings.Contains(data, `"dwyt_optimizer": {`) {
		t.Fatalf("expected a dwyt server entry, got: %s", data)
	}
}

// A Power written before v5 has no Optimizer entry; NeedsUpdate must notice.
func TestNeedsUpdateWhenOptimizerMissing(t *testing.T) {
	dwytHome, dwytBin := tempPowerEnv(t)
	status, err := EnsurePower(dwytHome, dwytBin, "/tmp/project")
	if err != nil {
		t.Fatal(err)
	}
	if NeedsUpdate(status.PowerDir, dwytBin) {
		t.Fatal("a freshly generated Power should not need an update")
	}
	// Simulate the pre-v5 file: same content minus the optimizer server.
	legacy, err := GenerateMCPJSON(dwytBin, map[string]bool{mcpregistry.ServerCodebase: true, mcpregistry.ServerObsidian: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(status.PowerDir, "mcp.json"), []byte(legacy), 0644); err != nil {
		t.Fatal(err)
	}
	if !NeedsUpdate(status.PowerDir, dwytBin) {
		t.Fatal("expected NeedsUpdate to detect the missing optimizer entry")
	}
}

func tempPowerEnv(t *testing.T) (string, string) {
	t.Helper()
	dwytHome := t.TempDir()
	dwytBin := filepath.Join(t.TempDir(), "bin")
	t.Setenv("HOME", t.TempDir())
	touchBin(t, dwytBin, executableName("codebase-memory-mcp"))
	touchBin(t, dwytBin, executableName("dwyt"))
	return dwytHome, dwytBin
}

func touchBin(t *testing.T, dir, name string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"), 0755); err != nil {
		t.Fatal(err)
	}
}
