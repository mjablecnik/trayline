# Requirements Document

## Introduction

This document specifies the requirements for restructuring the trayline monorepo from its current flat layout into an organized architecture with 7 top-level directories, each with a distinct responsibility. The restructuring is purely structural — no functional changes to any component. The goal is to improve developer orientation, reduce cognitive overhead, and make the dependency graph between subsystems explicit in the directory layout.

## Glossary

- **Monorepo**: The single `trayline/` git repository containing all project components
- **Runtime**: The set of scripts and container definitions used during normal trayline usage (CLI wrapper, agent runner, sync script, sandbox Dockerfile)
- **Orchestrator**: The Go binary (`trayline-run`) that reads YAML pipeline definitions and executes agent/shell steps sequentially
- **Remote**: The combined server and client components providing HTTP/WebSocket API access to agents — merged into a single Go module
- **Tools**: Independent utilities (taskline, tunnel) that function without trayline core
- **Setup**: Installation, configuration, and shell completion artifacts
- **Pipeline_Definitions**: YAML files (tasks, processes, workflows, lifecycle) consumed by the Orchestrator
- **Go_Module**: A directory containing a `go.mod` file that defines a Go module boundary
- **Sandbox_Image**: The Docker image (`trayline-sandbox`) built from the Dockerfile, providing the agent execution environment
- **Installer**: The `install.sh` script that builds, copies, and configures all trayline components on the host machine

## Requirements

### Requirement 1: Create runtime/ Directory

**User Story:** As a developer, I want all runtime execution artifacts grouped in one place, so that I can quickly find what runs during normal trayline usage.

#### Acceptance Criteria

1. THE Monorepo SHALL contain a top-level `runtime/` directory with the following contents: `sandbox/Dockerfile`, `trayline` (CLI wrapper script), `trayline-agent` (agent launcher script), and `sync.sh` (sync script)
2. WHEN the restructuring is complete, THE `runtime/sandbox/Dockerfile` SHALL be byte-for-byte identical to the content of the current root-level `Dockerfile`
3. WHEN the restructuring is complete, THE `runtime/trayline` SHALL be byte-for-byte identical to the content of the current `scripts/trayline`
4. WHEN the restructuring is complete, THE `runtime/trayline-agent` SHALL be byte-for-byte identical to the content of the current `scripts/trayline-agent`
5. WHEN the restructuring is complete, THE `runtime/sync.sh` SHALL be byte-for-byte identical to the content of the current `scripts/sync.sh`
6. WHEN the restructuring is complete, THE root-level `Dockerfile` and the files `scripts/trayline`, `scripts/trayline-agent`, and `scripts/sync.sh` SHALL no longer exist at their original locations
7. WHEN the restructuring is complete, THE `scripts/` directory SHALL no longer exist
8. WHEN the restructuring is complete, THE files `runtime/trayline`, `runtime/trayline-agent`, and `runtime/sync.sh` SHALL retain their executable permission (chmod +x)

### Requirement 2: Preserve orchestrator/ Directory

**User Story:** As a developer, I want the orchestrator Go module to remain at its current location, so that its internal structure and build process stay unchanged.

#### Acceptance Criteria

1. THE Monorepo SHALL retain the top-level `orchestrator/` directory with all existing files and subdirectories, including Go source files, `go.mod`, `go.sum`, and test files, with identical content and relative paths
2. WHEN the restructuring is complete, THE Orchestrator `go.mod` SHALL declare the same module path (`module orchestrator`) as before
3. WHEN the restructuring is complete, THE Orchestrator SHALL compile successfully using `go build ./...` from the `orchestrator/` directory with exit code 0 and no source code modifications
4. WHEN the restructuring is complete, THE Orchestrator SHALL pass all tests using `go test ./...` from the `orchestrator/` directory with exit code 0 and no source code modifications

### Requirement 3: Elevate pipelines/ Directory

**User Story:** As a developer, I want pipeline YAML definitions separated from code, so that it is clear they are data/configuration consumed by the orchestrator rather than executable code.

#### Acceptance Criteria

1. THE Monorepo SHALL retain the top-level `pipelines/` directory containing `lifecycle.yaml`, `tasks/`, `processes/`, and `workflows/` subdirectories
2. WHEN the restructuring is complete, THE Pipeline_Definitions SHALL have the same file names, directory hierarchy, and YAML content as before the restructuring, except for path reference updates required by criterion 3
3. IF any Pipeline_Definitions contain relative path references in `command` fields (e.g., `trayline run processes/...`, `trayline run tasks/...`) or in the `log-task` field that resolve differently from the new monorepo structure, THEN THE Pipeline_Definitions SHALL be updated so that each such reference resolves to the same target file as before the restructuring
4. WHEN the restructuring is complete, THE Orchestrator SHALL be able to parse and resolve every pipeline definition in `pipelines/` without file-not-found or path-resolution errors

### Requirement 4: Create remote/ Directory by Merging Server and Client

**User Story:** As a developer, I want the API server and CLI client in a single Go module, so that shared types and API contracts are co-located and maintained together.

#### Acceptance Criteria

1. THE Monorepo SHALL contain a top-level `remote/` directory with a single `go.mod` file declaring one Go module using Go version 1.23
2. WHEN the restructuring is complete, THE `remote/` directory SHALL contain `cmd/server/main.go` and `cmd/client/main.go` as the two entry points, with each command's supporting source files co-located in its respective `cmd/` subdirectory or in shared packages
3. WHEN the restructuring is complete, THE `remote/` directory SHALL contain the existing server packages: `api/`, `docker/`, `store/`, `core/`
4. WHEN the restructuring is complete, THE `remote/` directory SHALL contain a `scripts/` subdirectory with `build.sh`, `start-docker.sh`, and `stop-docker.sh`
5. THE Remote Go module SHALL compile both `cmd/server/main.go` and `cmd/client/main.go` successfully with all internal import paths updated to reference the module path declared in `remote/go.mod`
6. THE Remote Go module SHALL pass all existing tests from both the former server and client modules when run via `go test ./...` from the `remote/` directory without functional changes to test logic
7. WHEN the restructuring is complete, THE original `server/` and `client/` directories SHALL no longer exist

### Requirement 5: Create tools/ Directory

**User Story:** As a developer, I want independent tools grouped under a shared parent directory, so that it is clear they are self-contained utilities usable outside the trayline ecosystem.

#### Acceptance Criteria

1. THE Monorepo SHALL contain a top-level `tools/` directory with `taskline/` and `tunnel/` as immediate subdirectories
2. WHEN the restructuring is complete, THE `tools/taskline/` directory SHALL contain an identical file and directory tree to the current `taskline/` directory, preserving all file contents byte-for-byte
3. WHEN the restructuring is complete, THE `tools/tunnel/` directory SHALL contain an identical file and directory tree to the current `tunnel/` directory, preserving all file contents byte-for-byte
4. WHEN the restructuring is complete, THE `tools/taskline/server/` and `tools/taskline/cli/` Go modules SHALL compile with exit code 0 and pass all existing tests with exit code 0, with no changes to application logic (module path updates in `go.mod` and import statements are permitted)
5. WHEN the restructuring is complete, THE original top-level `taskline/` and `tunnel/` directories SHALL no longer exist in the repository
6. WHEN the restructuring is complete, THE Monorepo SHALL contain no remaining references to the old top-level `taskline/` or `tunnel/` paths in scripts, configuration files, or documentation outside the `tools/` directory

### Requirement 6: Create setup/ Directory

**User Story:** As a developer, I want all installation and configuration artifacts consolidated in one place, so that setting up trayline on a new machine is straightforward.

#### Acceptance Criteria

1. THE Monorepo SHALL contain a top-level `setup/` directory with the following contents: `install.sh`, `config.example`, `.rsyncignore`, and `completions/_trayline`
2. WHEN the restructuring is complete, THE `setup/install.sh` SHALL resolve the monorepo root as the parent of its own directory and reference all sibling artifacts using that root path (runtime scripts from `runtime/`, orchestrator from `orchestrator/`, pipelines from `pipelines/`, completions from `setup/completions/`)
3. WHEN the restructuring is complete, THE `setup/config.example` SHALL be byte-for-byte identical to the current root-level `config.example`
4. WHEN the restructuring is complete, THE `setup/.rsyncignore` SHALL be byte-for-byte identical to the current root-level `.rsyncignore`
5. WHEN the restructuring is complete, THE `setup/completions/_trayline` SHALL be byte-for-byte identical to the current `completions/_trayline`
6. WHEN the restructuring is complete, THE original root-level `config.example`, `.rsyncignore`, `install.sh`, and `completions/` directory SHALL no longer exist
7. WHEN `setup/install.sh` is executed from any working directory, THE install script SHALL complete successfully, installing all artifacts to their target locations without path resolution errors

### Requirement 7: Preserve .agents/ and .kiro/ at Root

**User Story:** As a developer, I want AI agent working files and spec files to remain at the repository root, so that AI agents and Kiro can find them at their expected paths.

#### Acceptance Criteria

1. THE Monorepo SHALL retain `.agents/` at the repository root with its existing structure (MEMORY.md, AI_LOG.md, tmp/, checkpoints/)
2. THE Monorepo SHALL retain `.kiro/` at the repository root with its existing structure (specs/ and all subdirectories within it)
3. WHEN the restructuring is complete, THE `.agents/` and `.kiro/` directories SHALL contain the same file tree and identical file contents as before the restructuring
4. IF any restructuring operation attempts to move, rename, or nest `.agents/` or `.kiro/` under a subdirectory, THEN THE Monorepo SHALL preserve them at the repository root path unchanged

### Requirement 8: Update install.sh Path References

**User Story:** As a developer, I want the installer to work correctly after restructuring, so that running install.sh sets up trayline from the new directory layout.

#### Acceptance Criteria

1. WHEN `setup/install.sh` is executed, THE Installer SHALL copy `runtime/trayline-agent` to `~/.trayline/trayline-agent` with executable permission preserved
2. WHEN `setup/install.sh` is executed, THE Installer SHALL copy `runtime/sync.sh` to `~/.trayline/sync.sh` with executable permission preserved
3. WHEN `setup/install.sh` is executed, THE Installer SHALL copy `runtime/trayline` to `~/bin/trayline` with executable permission preserved
4. WHEN `setup/install.sh` is executed, THE Installer SHALL build the Orchestrator from `orchestrator/` and install it to `~/.trayline/trayline-run`
5. WHEN `setup/install.sh` is executed, THE Installer SHALL copy Pipeline_Definitions from `pipelines/` to `~/.trayline/pipelines/`
6. WHEN `setup/install.sh` is executed, THE Installer SHALL build the Sandbox_Image from `runtime/sandbox/Dockerfile`
7. WHEN `setup/install.sh` is executed, THE Installer SHALL install zsh completions from `setup/completions/_trayline`
8. WHEN `setup/install.sh` is executed, THE Installer SHALL copy `setup/.rsyncignore` to `~/.trayline/.rsyncignore`
9. WHEN `setup/install.sh` is executed, THE Installer SHALL copy `setup/config.example` to `~/.trayline/config` only if no config file exists
10. WHEN `setup/install.sh` is executed and Go is not available, THE Installer SHALL copy the pre-built orchestrator binary from `orchestrator/bin/` to `~/.trayline/trayline-run`
11. WHEN `setup/install.sh` is executed, THE Installer SHALL strip CRLF line endings from all copied scripts for WSL compatibility

### Requirement 9: Preserve Git History

**User Story:** As a developer, I want git history preserved for moved files, so that blame and log remain useful for understanding past changes.

#### Acceptance Criteria

1. WHEN moving files during restructuring, THE Monorepo SHALL use `git mv` for all file relocations to preserve rename tracking in git history
2. IF a file must be both moved and modified (content change required such as import path updates), THEN THE Monorepo SHALL perform the move and content change in separate commits to maximize history preservation
3. IF a file is newly created as part of the merge (e.g., the new `remote/go.mod`), THEN the file is exempt from the `git mv` requirement since it has no prior history to preserve

### Requirement 10: Update trayline CLI Wrapper Paths

**User Story:** As a developer, I want the trayline CLI wrapper to continue working after restructuring, so that all subcommands resolve to the correct script locations.

#### Acceptance Criteria

1. THE `trayline` CLI wrapper SHALL resolve `TRAYLINE_HOME` to the value of the `TRAYLINE_HOME` environment variable if set, or default to `~/.trayline/` if unset
2. WHEN the `agent` subcommand is invoked, THE `trayline` wrapper SHALL execute `${TRAYLINE_HOME}/trayline-agent` with all remaining arguments passed through
3. WHEN the `sync` subcommand is invoked, THE `trayline` wrapper SHALL execute `${TRAYLINE_HOME}/sync.sh` with all remaining arguments passed through
4. WHEN the `run` subcommand is invoked, THE `trayline` wrapper SHALL execute `${TRAYLINE_HOME}/trayline-run` with the pipeline path resolved from `${TRAYLINE_HOME}/pipelines/` and all flags passed through
5. WHEN the `flow` subcommand is invoked, THE `trayline` wrapper SHALL execute `${TRAYLINE_HOME}/trayline-run flow` with each pipeline reference resolved from `${TRAYLINE_HOME}/pipelines/` and all flags passed through
6. WHEN the `version` subcommand is invoked, THE `trayline` wrapper SHALL print the wrapper version string and invoke `${TRAYLINE_HOME}/trayline-run --version`
7. WHEN the `help` subcommand is invoked or no arguments are provided, THE `trayline` wrapper SHALL print the usage message listing all available subcommands (agent, sync, run, flow, install, version, help) and exit with code 0

### Requirement 11: Update Root-Level Files

**User Story:** As a developer, I want root-level documentation to reflect the new structure, so that new contributors can orient themselves quickly.

#### Acceptance Criteria

1. WHEN the restructuring is complete, THE Monorepo root SHALL contain an updated `README.md` with a "Project Structure" section that lists every top-level directory as a tree diagram, where each directory entry includes a one-line description of its purpose (maximum 80 characters per description)
2. WHEN the restructuring is complete, THE Monorepo root SHALL contain an updated `CLAUDE.md` that lists the repo-relative paths to each service directory, the pipelines directory, the specs directory, and any shared configuration files that AI agents need to reference
3. THE Monorepo root SHALL retain a `.gitignore` updated so that every path pattern references directories that exist in the new structure (no patterns referencing removed directories) and includes patterns for any new directories that contain build artifacts, binaries, or environment secret files
4. WHEN the restructuring is complete, THE Monorepo root documentation files (`README.md` and `CLAUDE.md`) SHALL reference only paths that exist in the repository's actual filesystem post-restructuring

### Requirement 12: No Functional Changes

**User Story:** As a developer, I want the restructuring to be purely organizational, so that I can merge it confidently without risking regressions.

#### Acceptance Criteria

1. THE restructuring SHALL NOT modify any Go source code logic — only import path strings within the merged Remote module and module path declarations in `go.mod` files are permitted to change
2. THE restructuring SHALL NOT modify the Sandbox_Image Dockerfile content (only its filesystem location changes)
3. THE restructuring SHALL NOT modify Pipeline_Definitions content unless relative path references require correction to resolve to the same target as before
4. THE restructuring SHALL NOT modify the Orchestrator source code or its module path
5. THE restructuring SHALL NOT modify the Tools source code or their module paths (unless module paths must change due to new directory location)
6. WHEN the restructuring is complete, THE Orchestrator SHALL compile and pass all existing tests via `go build ./...` and `go test ./...`
7. WHEN the restructuring is complete, THE Remote module SHALL compile and pass all existing tests via `go build ./...` and `go test ./...`
8. WHEN the restructuring is complete, THE Tools modules (taskline server, taskline cli) SHALL compile and pass all existing tests via `go build ./...` and `go test ./...`

### Requirement 13: Dependency Direction Enforcement

**User Story:** As a developer, I want the directory structure to reflect the actual dependency graph, so that circular dependencies are prevented by convention.

#### Acceptance Criteria

1. THE `runtime/` directory SHALL NOT contain Go import statements, shell source/include directives, or path references that resolve to files within `orchestrator/`, `remote/`, `pipelines/`, or `tools/`
2. THE `orchestrator/` directory SHALL NOT contain Go import statements referencing the `remote/` or `tools/` module paths, and SHALL depend on `runtime/` only by invoking the `trayline-agent` script at execution time (not as a compile-time Go import)
3. THE `orchestrator/` directory SHALL reference `pipelines/` only by reading Pipeline_Definitions at execution time (not as a compile-time dependency)
4. THE `remote/` directory SHALL NOT contain Go import statements referencing the `orchestrator/` or `tools/` module paths, and SHALL depend on `runtime/` only by building or running the Sandbox_Image defined in `runtime/sandbox/Dockerfile` (not as a compile-time Go import)
5. THE `tools/` directory SHALL NOT contain Go import statements, shell source/include directives, or path references that resolve to files within `runtime/`, `orchestrator/`, `remote/`, `pipelines/`, or `setup/`
6. THE `setup/` directory SHALL reference all other directories (runtime, orchestrator, pipelines, tools) via path references in installation scripts
7. No directory other than `setup/` SHALL contain path references or script invocations that resolve to files within the `setup/` directory
