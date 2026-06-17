# DWYT Token Savings, Agent Laws, and Kiro Power

## Objective

Implement improvements in DWYT to:

- display `Tokens Saved` for Codebase in the same way it is already displayed for Obsidian;
- include Codebase and Obsidian in the global token savings summary;
- reinforce the two main agent laws: **Codebase Law** and **Obsidian Law**;
- correctly configure AI instructions without overwriting user content;
- fix and validate the Kiro Power integration;
- keep the CLI, UI, installer, status, and documentation consistent with each other;
- avoid regressions, duplications, and conflicts between RTK, Codebase, Obsidian, and Headroom.

---

## 0. Tool priority rule

DWYT must guide agents to use the tools in this priority order, **when applicable to the type of task**:

1. **RTK** — priority for shell commands, terminal automation, and repetitive commands.
2. **Codebase MCP** — priority for understanding the real code structure, dependencies, symbols, flows, and the impact of changes.
3. **Obsidian MCP** — priority for retrieving and saving persistent project memory, decisions, history, tasks, and future context.
4. **Headroom** — priority only as a proxy/cache optimization when compatible with the AI client.

This order must not create conflict between the tools:

- RTK does not replace Codebase or Obsidian; it only optimizes shell commands.
- Codebase is the primary source for the current code structure.
- Obsidian is the primary source for project memory, history, and decisions.
- Headroom is traffic optimization, not a source of truth.
- Codex authenticated via ChatGPT/OAuth **must not use Headroom**.

---

## 1. Mandatory assumptions

- The user will commit and push manually.
- DWYT must not fully overwrite existing AI instruction or configuration files.
- Blocks controlled by DWYT must be identifiable, idempotent, and updatable without duplication.
- Content outside the blocks controlled by DWYT must be preserved.
- The UI must continue to work with the current RTK and Headroom data.
- Obsidian data must reside under `~/.dwyt` and have absolute persistence.
- No install, uninstall, reinstall, clean, repair, or reset flow may delete Obsidian vaults, projects, notes, or history.
- States shown by the CLI and UI must use the same semantics.
- Tools installed on demand must not appear as a failure when they are merely inactive.
- The Codebase and Obsidian laws are mandatory for agents, documentation, generated templates, Kiro Power, and new vaults.

---

## 2. Codebase Law — mandatory code map

When working on any project managed by DWYT, the agent must use the **Codebase MCP** whenever it needs to understand, validate, diagnose, or change the real code structure.

### 2.1 Main rule

Before proposing changes, refactors, fixes, or technical diagnostics:

- validate whether the project is indexed;
- query the current code state via Codebase MCP;
- avoid assumptions based solely on memory, prior context, or apparent file names.

The code indexed by Codebase is the primary source for:

- files;
- relationships;
- dependencies;
- symbols;
- calls;
- paths;
- the impact of changes.

### 2.2 Preferred tools

Always prefer:

- `search_graph` to locate files, modules, symbols, services, handlers, components, and relationships;
- `trace_path` to understand flows, calls, dependencies, and impact;
- `get_code_snippet` to read real snippets before suggesting or applying changes.

### 2.3 Restrictions

When the Codebase MCP is available:

- avoid `grep`, `glob`, `find`, and massive manual reading as a first strategy;
- do not change critical files without consulting the graph;
- do not create duplicate code without checking whether an equivalent implementation already exists;
- do not remove, rename, or move files without tracing the impact.

### 2.4 Recommended flow

1. Validate or update the project index.
2. Use `search_graph` to locate the affected area.
3. Use `trace_path` to understand dependencies and impact.
4. Use `get_code_snippet` before suggesting or editing code.
5. Apply the change carefully.
6. Validate the build, tests, and behavior.
7. Record the relevant context in Obsidian.

---

## 3. Obsidian Law — mandatory persistent memory

Obsidian is the official project memory within DWYT.

The agent must use the **Obsidian MCP** to retrieve context before relevant tasks and to save useful information during or at the end of execution.

### 3.1 Before acting

Before starting diagnostic, refactoring, planning, documentation, or other relevant change tasks:

- search for existing notes in the project vault;
- read or rebuild the current summary;
- retrieve decisions, known bugs, open tasks, and relevant history.

### 3.2 During the action

During execution, save information whenever there is a relevant change:

- technical decisions;
- task status;
- problems found;
- hypotheses confirmed or discarded;
- important commands;
- impacted files.

Suggested types:

- `type: "decision"` for ADRs and technical choices;
- `type: "task"` for tasks, progress, and status;
- `type: "debug"` for errors, investigation, and root cause;
- `type: "context"` for a summary useful to future agents.

### 3.3 At the end of the task

At the end of every relevant task, save a complete context with:

- `summary`;
- `user_request`;
- `files`;
- `decisions`;
- `actions`;
- `commands`;
- `errors`;
- `outcome`;
- `next_steps`;
- `context`.

If the Obsidian MCP is unavailable, the agent must:

- not block the task;
- record the failure clearly;
- guide the user on how to re-run the save later;
- never delete or recreate vaults as an automatic correction attempt.

### 3.4 Vault structure

New vaults must be created organized with:

- `instructions/`;
- `templates/`;
- `maps/`;
- `decisions/`;
- `tasks/`;
- `debug/`;
- `context/`.

They must also include:

- internal links;
- basic templates;
- a navigation map;
- usage instructions for agents;
- a reference to the Obsidian Law and the Codebase Law.
- ALL Obsidian files in each vault must be created with interlinking and `[[]]` tags, no loose files; a structured map must be in place.
---

## 4. Combined use: RTK, Codebase, Obsidian, and Headroom

The tools must be used in a complementary way.

### 4.1 RTK

Use RTK for:

- frequent shell commands;
- compression of long commands;
- standardized execution of repetitive tasks;
- token savings in terminal interactions.

The RTK savings metric continues to come from `rtk gain`.

### 4.2 Codebase

Use Codebase for:

- structural analysis;
- navigation through the project graph;
- locating symbols and dependencies;
- preventing massive manual reading of the repository.

### 4.3 Obsidian

Use Obsidian for:

- persistent context;
- decisions;
- tasks;
- history;
- incremental documentation;
- memory for future agents.

### 4.4 Headroom

Use Headroom only when compatible.

Rules:

- keep real data coming from `/stats`;
- do not use it with Codex authenticated via ChatGPT/OAuth;
- do not present Headroom as mandatory when the client does not support proxy/base URL;
- a Headroom failure must not break Codebase, RTK, or Obsidian.

---

## 5. Tokens Saved — Codebase

Add a cheap, consistent, realistic, and transparent estimate for `tokens_saved` in the `codebase-memory-mcp` detail.

### 5.1 Proposed calculation

When the project is indexed:

- use index metadata already known, such as `nodes` and `edges`;
- estimate the cost of manually reading the repository based on this metadata;
- estimate the cost of a structural query via MCP as a small, fixed, or proportional value;
- calculate:

```txt
tokens_saved = max(manual_tokens - mcp_tokens, 0)
```

Also expose the fields used in the global summary:

- `without_dwyt_tokens`;
- `with_dwyt_tokens`;
- `tokens_saved`;
- `estimation_source`.

### 5.2 Display rules

- The Codebase card must show `Tokens Saved` using the same visual pattern as the Obsidian card.
- The global summary must include the Codebase savings.
- Projects without an index must not display an error.
- Projects without an index may display `—`, `0`, or a neutral state, as long as the UI is consistent with Obsidian.
- The estimate must make it clear that it is local until native MCP or harness telemetry exists.

### 5.3 Validation

Validate that:

- the Codebase card shows `Tokens Saved` when there is an index;
- the value enters the global summary;
- there are no artificially high numbers in small or newly indexed projects;
- there is no regression in the current Codebase status;
- the UI does not break when the index is absent, empty, or corrupted.

---

## 6. Tokens Saved — Obsidian

Add or revise `tokens_saved` in the Obsidian card to maintain parity with Codebase.

### 6.1 Proposed calculation

For the project vault:

- measure the number of Markdown files;
- measure the total bytes of the relevant vault;
- estimate manual context tokens as `total_bytes / 4`;
- estimate the search/save overhead via MCP as a small proportional cost;
- calculate:

```txt
tokens_saved = max(manual_tokens - mcp_tokens, 0)
```

### 6.2 UI update

Update the data after relevant actions:

- saving context;
- searching notes;
- summarizing the vault;
- opening the vault;
- creating the vault;
- reindexing or recalculating the project status.

### 6.3 Validation

Validate that:

- the Obsidian card shows `Tokens Saved`;
- the global summary includes the Obsidian savings;
- an empty or newly created vault does not generate artificially high savings;
- vault read failures do not break the dashboard;
- no process deletes vault data.

---

## 7. Real data vs local estimates

Keep real data when it already exists:

- RTK continues to come from `rtk gain`;
- Headroom continues to come from `/stats`;
- Codebase uses a local estimate until there is native telemetry;
- Obsidian uses a local estimate until there is native telemetry.

The estimates must be implemented with clear helpers, for example:

- `estimateCodebaseTokenSavings`;
- `estimateObsidianTokenSavings`;
- `calculateGlobalTokenSavings`.

Avoid long comments in the code. The formula documentation must reside in docs or a technical README.

---

## 8. AI instruction files — safe append-only

Update the generation of the files:

- `AGENTS.md`;
- `CLAUDE.md`;
- `.cursor/rules/dwyt.mdc`;
- `.kiro/steering/dwyt.md`;
- `.github/copilot-instructions.md`;
- equivalent files for OpenCode, Codex, and other supported clients.

### 8.1 Mandatory rules

- If the file does not exist, create it with the DWYT block.
- If the file exists, preserve the original content.
- If the DWYT block is absent, add the block.
- If the DWYT block already exists, update only the block.
- Do not duplicate blocks.
- Do not remove content outside the block controlled by DWYT.
- Do not change the user's manual settings outside the DWYT section.

### 8.2 Official markers

Use unique and stable markers:

```md
<!-- DWYT:START -->
# DWYT
...
<!-- DWYT:END -->
```

### 8.3 DWYT block content

The block must be very complete and instruct the AIs to:

- use RTK for shell commands when applicable;
- use Codebase MCP before analyzing or changing the real code structure;
- use Obsidian MCP to retrieve and save persistent memory;
- use Headroom only when compatible;
- never use Headroom with Codex authenticated via ChatGPT/OAuth;
- save context in Obsidian at the end of relevant tasks;
- respect the priority order: RTK, Codebase, Obsidian, and Headroom;
- avoid overwriting the user's configuration files;
- validate changes before finishing.
- Have the Codebase and Obsidian laws clearly stated and complete.

### 8.4 Minimum context payload

The payload saved in Obsidian must contain:

- the user's request;
- a summary;
- changed files;
- decisions;
- actions;
- commands;
- errors;
- result;
- next steps;
- context for future agents.

EVERYTHING interlinked with `[[]]`

---

## 9. Kiro Power and MCP configuration

Update the Kiro integration with validation against the current official documentation before the final implementation.

### 9.1 Expected structure

- Every Power must have a `POWER.md` with frontmatter.
- The local DWYT Power must reside in:

```txt
~/.dwyt/powers/dwyt-power
```

- DWYT must attempt to register or link the Power in:

```txt
~/.kiro/powers/dwyt-power
```

- When automatic installation cannot be guaranteed, the UI/status must show a manual activation instruction using the local path of the Power.

### 9.2 Kiro MCP configuration

- The per-workspace MCP configuration must be written in:

```txt
.kiro/settings/mcp.json
```

- `.kiro/mcp.json` may be updated for legacy compatibility, but must not be the primary source.
- Existing JSONs must receive a safe merge.
- The user's MCPs must be preserved.
- DWYT entries must be idempotent.

### 9.3 Minimum `POWER.md` frontmatter

```yaml
---
name: dwyt-power
displayName: DWYT Project Context
description: DWYT integration for Codebase MCP, Obsidian memory, RTK command compression and compatible Headroom usage.
keywords:
  - dwyt
  - codebase
  - obsidian
  - mcp
  - memory
  - project memory
  - token savings
  - repo analysis
  - arquitetura
  - refatoracao
  - debugging
  - documentacao
  - contexto do projeto
author: DWYT
---
```

### 9.4 Power content

The Power must reinforce:

- the Codebase Law;
- the Obsidian Law;
- the priority RTK, Codebase, Obsidian, and Headroom;
- the Codex ChatGPT/OAuth exception with no Headroom;
- the mandatory use of MCPs when available;
- saving context at the end of the task.

---

## 10. Security and non-regression

Validate that:

- existing JSONs are merged safely;
- the user's MCPs are preserved;
- DWYT sections do not duplicate;
- old Kiro configs continue to be accepted when they exist;
- Codex ChatGPT/OAuth continues without Headroom;
- Kiro, Claude, Cursor, OpenCode, Copilot, and Codex receive appropriate instructions;
- the UI compiles and displays the new values;
- Go tests pass;
- the frontend build/lint runs when available;
- no process removes vaults or persistent Obsidian data;
- a failure in one tool does not bring down the status of the others;
- the `installed`, `inactive`, `launch on demand`, and equivalent states are not treated as errors.

---

## 11. Installation, status, and new versions

Ensure that the public installation and update flow is predictable.

### 11.1 Installer

The official command:

```bash
curl -fsSL https://raw.githubusercontent.com/fvmoraes/dwyt/main/install.sh | bash
```

Must:

- download the latest published release;
- safely overwrite the old binary at `~/.local/bin/dwyt`;
- not accidentally reuse a local binary from the current directory;
- preserve persistent data in `~/.dwyt`;
- never delete Obsidian vaults.

### 11.2 CLI and UI status

`dwyt status` and the dashboard must use the same semantics for:

- Codebase;
- RTK;
- Headroom;
- Obsidian.

Rules:

- `installed (launch on demand)` must appear as healthy or inactive, not as an error;
- `inactive` must not be treated as a critical failure;
- the absence of an index must be a neutral state, not a crash;
- a network failure when querying the release must not break the dashboard.

### 11.3 New version notice

The UI must:

- query the latest published release;
- show a discreet notice when there is a new version;
- not show a notice in `dev` builds;
- not show a notice when the local version is already current;
- display the official update command via `curl`.

---

## 12. Documentation to update

Update or create:

- `docs/OBSIDIAN-LAW.md`;
- `docs/CODEBASE-LAW.md`;
- `Tokens Saved` documentation;
- Kiro Power documentation;
- the main README;
- agent templates;
- the initial vault seed;
- installation and update instructions.

The documentation must make clear:

- what is real data;
- what is a local estimate;
- when each tool should be used;
- how to avoid conflicts between tools;
- where the data is persisted;
- which data can never be deleted automatically.

---

## 13. Mandatory final validation

Run thorough validation before finishing.

### 13.1 Backend

Validate:

- Go tests;
- status handlers;
- the `tokens_saved` calculation;
- serialization of the new fields;
- path security in `~/.dwyt`;
- vault preservation;
- safe JSON merging.

### 13.2 Frontend

Validate:

- the build;
- lint when available;
- the Codebase and Obsidian cards;
- the global summary;
- the error, empty, inactive, and installed-on-demand states;
- the new version notice;
- the visual consistency of the cards.

### 13.3 Integrations

Validate:

- Kiro Power;
- `.kiro/settings/mcp.json`;
- legacy `.kiro/mcp.json`;
- `AGENTS.md`;
- `CLAUDE.md`;
- Cursor;
- Copilot;
- OpenCode;
- Codex;
- the Codex ChatGPT/OAuth exception with no Headroom.

### 13.4 Regression

Confirm that:

- nothing deleted Obsidian data;
- DWYT blocks did not duplicate;
- the user's manual content was preserved;
- the CLI status matches the UI status;
- on-demand tools do not appear as a failure;
- the installer remains functional;
- the README does not contradict the implementation.

---

## 14. Expected result

At the end of execution:

- Codebase shows `Tokens Saved` with the same pattern as Obsidian;
- Obsidian keeps `Tokens Saved` consistent and without artificial values;
- the global summary includes RTK, Headroom, Codebase, and Obsidian;
- the Codebase Law is documented and applied in the templates;
- the Obsidian Law is documented and applied in the templates;
- the order RTK, Codebase, Obsidian, and Headroom is clear and without conflict;
- the AI instruction files are updated in safe append-only mode;
- Kiro Power follows the expected structure and preserves existing configurations;
- installation, update, CLI status, and dashboard use the same semantics;
- the documentation is consistent with the real behavior;
- automated validation was run;
- commit and push are left to the user.

---

## 15. Final principle

> In DWYT, no agent should work in the dark.  
> For commands, use RTK.  
> To understand code, consult the Codebase.  
> To remember context, use Obsidian.  
> To optimize compatible calls, use Headroom.  
> Before touching the code, consult the map.  
> Before losing context, save it to the project memory.
