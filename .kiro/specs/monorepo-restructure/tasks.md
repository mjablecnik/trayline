# Implementation Plan: Monorepo Restructure

## Overview

This plan restructures the trayline monorepo from a flat layout into 7 top-level directories (runtime/, orchestrator/, pipelines/, remote/, tools/, setup/, .agents/). The work is sequenced to maximize git history preservation: pure moves first (git mv), then the remote/ module merge (most complex), then content updates, and finally verification.

## Tasks

- [x] 1. Create runtime/ directory and move scripts + Dockerfile
  - Create `runtime/` and `runtime/sandbox/` directories
  - `git mv Dockerfile runtime/sandbox/Dockerfile`
  - `git mv scripts/trayline runtime/trayline`
  - `git mv scripts/trayline-agent runtime/trayline-agent`
  - `git mv scripts/sync.sh runtime/sync.sh`
  - Remove the now-empty `scripts/` directory
  - Verify moved files retain executable permissions
  - **Requirements:** 1.1, 1.2, 1.3, 1.4, 1.5, 1.6, 1.7, 1.8, 9.1

- [x] 2. Move tools (taskline + tunnel) into tools/ directory
  - Create `tools/` directory
  - `git mv taskline tools/taskline`
  - `git mv tunnel tools/tunnel`
  - Verify taskline server compiles: `cd tools/taskline/server && go build ./...`
  - Verify taskline cli compiles: `cd tools/taskline/cli && go build ./...`
  - Verify taskline tests pass: `go test ./...` in both modules
  - **Requirements:** 5.1, 5.2, 5.3, 5.4, 5.5, 9.1

- [x] 3. Move setup artifacts into setup/ directory
  - Create `setup/` and `setup/completions/` directories
  - `git mv install.sh setup/install.sh`
  - `git mv config.example setup/config.example`
  - `git mv .rsyncignore setup/.rsyncignore`
  - `git mv completions/_trayline setup/completions/_trayline`
  - Remove the now-empty `completions/` directory
  - **Requirements:** 6.1, 6.3, 6.4, 6.5, 6.6, 9.1

- [x] 4. Move server files into remote/ structure
  - Create `remote/cmd/server/` directories
  - `git mv server/main.go remote/cmd/server/main.go`
  - `git mv server/api remote/api`
  - `git mv server/core remote/core`
  - `git mv server/docker remote/docker`
  - `git mv server/store remote/store`
  - `git mv server/scripts remote/scripts`
  - Move remaining server root Go files and test files to `remote/`
  - Move server `.env`, `.env.example`, `API.md`, `README.md` to `remote/`
  - **Requirements:** 4.2, 4.3, 4.4, 4.7, 9.1

- [x] 5. Move client files into remote/ structure
  - Create `remote/cmd/client/` directory
  - `git mv client/main.go remote/cmd/client/main.go`
  - Move all other client `.go` files to `remote/cmd/client/`
  - Move client test files to `remote/cmd/client/`
  - Move client scripts to `remote/scripts/`
  - Remove the now-empty `server/` and `client/` directories
  - **Requirements:** 4.2, 4.7, 9.1

- [x] 6. Create unified remote/go.mod and update import paths
  - Create `remote/go.mod` declaring `module remote` with `go 1.23`
  - Merge all dependencies from former server and client go.mod files
  - Replace all `import "server/..."` with `import "remote/..."` in remote/
  - Update any `trayline-client` references if present
  - Run `go mod tidy` in `remote/`
  - Verify: `cd remote && go build ./... && go test ./...`
  - **Requirements:** 4.1, 4.5, 4.6, 12.1

- [x] 7. Update setup/install.sh path references
  - Change `SCRIPT_DIR` logic to derive `REPO_ROOT="$(dirname "$SCRIPT_DIR")"` since install.sh is now in `setup/`
  - Update sandbox Docker build path to `$REPO_ROOT/runtime/sandbox`
  - Update runtime script copy sources to `$REPO_ROOT/runtime/`
  - Update orchestrator build path to `$REPO_ROOT/orchestrator`
  - Update pipelines copy source to `$REPO_ROOT/pipelines`
  - Keep setup-relative paths for config.example, .rsyncignore, and completions
  - Verify install.sh runs without errors from any working directory
  - **Requirements:** 6.2, 6.7, 8.1, 8.2, 8.3, 8.4, 8.5, 8.6, 8.7, 8.8, 8.9, 8.10, 8.11

- [x] 8. Update .gitignore for new structure
  - Remove old paths that no longer exist (server/bin/, client/bin/, client/trayline-client)
  - Add new paths for remote/ and tools/ build artifacts
  - Ensure .env patterns cover new locations under tools/
  - Verify no patterns reference removed directories
  - **Requirements:** 11.3

- [x] 9. Create/update README.md with project structure map
  - Write root README.md with one-line project description
  - Add "Project Structure" section with directory tree (all 7 top-level dirs)
  - Include one-line description per directory (max 80 chars)
  - Add dependency direction overview
  - Ensure all referenced paths exist in the actual filesystem
  - **Requirements:** 11.1, 11.4

- [x] 10. Create CLAUDE.md with agent instructions
  - Create CLAUDE.md at repo root
  - List repo-relative paths to each service directory
  - Document build commands for each Go module
  - Document dependency direction rules
  - List paths to pipelines, specs, and config files
  - **Requirements:** 11.2, 11.4

- [x] 11. Verify orchestrator and pipelines are unchanged
  - Confirm orchestrator/go.mod still declares `module orchestrator`
  - Run `cd orchestrator && go build ./... && go test ./...` — must exit 0
  - Confirm all YAML files in pipelines/ have identical content to before
  - Verify lifecycle.yaml log-task field still resolves correctly
  - **Requirements:** 2.1, 2.2, 2.3, 2.4, 3.1, 3.2, 3.3, 3.4, 12.4

- [x] 12. Verify .agents/ and .kiro/ are untouched
  - Confirm .agents/ exists at root with MEMORY.md, AI_LOG.md, tmp/, checkpoints/
  - Confirm .kiro/ exists at root with all specs intact
  - Confirm no files were moved, renamed, or modified within these directories
  - **Requirements:** 7.1, 7.2, 7.3, 7.4

- [x] 13. Final verification — all builds, tests, and install pass
  - `cd orchestrator && go build ./... && go test ./...`
  - `cd remote && go build ./... && go test ./...`
  - `cd tools/taskline/server && go build ./... && go test ./...`
  - `cd tools/taskline/cli && go build ./... && go test ./...`
  - Run `setup/install.sh --skip-docker-build`
  - Run dependency direction audit (grep for disallowed imports)
  - Verify `git log --follow -- runtime/sync.sh` shows full history
  - **Requirements:** 12.6, 12.7, 12.8, 13.1, 13.2, 13.4, 13.5

- [x] 14. Clean up stale references
  - Search entire repo for references to old paths (scripts/trayline, server/, client/ as directories)
  - Update any remaining references in documentation or specs
  - Remove any leftover empty directories or stale files
  - **Requirements:** 5.6, 11.4

## Task Dependency Graph

```json
{
  "waves": [
    {"tasks": [1, 2, 3]},
    {"tasks": [4]},
    {"tasks": [5]},
    {"tasks": [6]},
    {"tasks": [7, 8]},
    {"tasks": [9, 10, 11, 12]},
    {"tasks": [13]},
    {"tasks": [14]}
  ]
}
```

- **Wave 1**: Tasks 1-3 can run in parallel (independent git mv moves to runtime/, tools/, setup/)
- **Wave 2-3**: Tasks 4-5 are sequential (both target remote/ — server first, then client)
- **Wave 4**: Task 6 depends on 4+5 (create unified go.mod and update imports after all files are in place)
- **Wave 5**: Tasks 7-8 depend on all moves and module merge being done
- **Wave 6**: Tasks 9-12 are documentation and verification (can run in parallel)
- **Wave 7**: Task 13 is the final gate — all builds, tests, and install must pass
- **Wave 8**: Task 14 is cleanup after everything passes

## Notes

- All `git mv` operations in tasks 1-5 should be committed together as one "pure moves" commit to maximize rename detection
- Task 6 (import path changes + go.mod creation) should be a separate commit from the moves
- Task 7 (install.sh rewrite) can be in the same commit as task 6 since both are content changes
- The restructuring does NOT run on Windows — all git mv and build commands must be executed in WSL/Linux (the agent machine)
- If any Go build fails in task 6, the fix is mechanical: grep for remaining old import paths and update them
