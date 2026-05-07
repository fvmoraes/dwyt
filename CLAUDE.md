# DWYT — Engineering Conventions for AI Agents

This file is the source of truth for agents (Claude Code, Cursor, Copilot etc.)
working in this repo. Project-domain rules live in `docs/Rules/Rules.md`;
this file covers **how code is written and organized**.

## File size

- **Hard ceiling: 300 lines per file.** No exceptions for new files; existing
  oversized files (e.g. legacy `internal/brain/brain.go`) must not grow further
  and should be split when touched.
- **Soft target: 250 lines.** If you cross 250, ask whether the file has more
  than one clear responsibility — that's almost always the signal to split.
- Counts apply to source files (`.go`, `.ts`, `.py`, `.sh`, `.tsx`, etc.).
  Generated files, vendored code, and fixtures are exempt.

## Decomposition

- **Functions over inline blocks.** If a block inside a function reads as a
  named step ("validate input", "render header", "spawn daemon"), extract it
  into its own function with that name. The parent function should read like
  an outline of the steps it orchestrates.
- **One concept per file.** A file should be answerable by a single sentence
  ("installs Headroom", "detects the Obsidian binary"). When a file accretes a
  second concept, split before adding the third.
- **Helpers next to consumers, then promoted.** A helper used by one file
  stays in that file. The moment a second file needs it, move it to a shared
  location (`helpers.go`, `internal/<topic>/`, etc.) — never duplicate.
- **Same-package multi-file split is the default in Go.** Don't introduce a
  new package just to split a long file: `package install` across
  `install.go`, `headroom.go`, `obsidian.go` is preferred over creating
  `internal/installhelpers`.
- **Parent orchestrates, children execute.** A "mother" function/class wires
  inputs through specific helpers and surfaces failures; helpers do one thing
  and return errors, not log + exit.

## Reuse

- **Search before writing.** Before adding a path-detection block, fetch
  helper, retry loop, or HTTP client wrapper, grep the repo. The right place
  for it almost always already exists.
- **Single source of truth for cross-cutting data.** Lists like "where could
  Obsidian be installed" or "compatible Python versions" live in exactly one
  function. If two callers need it, they import — they don't redefine.
- **Lift on the second use, not the first.** Don't pre-abstract. Three
  similar lines is fine; two functions with copy-pasted bodies is the trigger.

## Bash specifics

- Bash files have the same 300-line ceiling. Extract repeated logic into
  functions defined at the top of the script.
- For installer scripts that ship via `curl | bash`, prefer in-script
  functions over sourced helper files — sourcing is unreliable when piped.

## When refactoring an oversized file you have to touch

1. Identify the cohesive groups (by domain, by tool, by lifecycle phase).
2. Move each group into its own file in the same package.
3. Keep the original file as the "entry point": shared types, package doc,
   and small dispatcher functions. It should end up the smallest of the set.
4. Run the full build and tests for the package; don't ship a partial split.
