# Requirements Document

## Introduction

Refactoring of the orchestrator Go module from a flat `package main` structure into properly separated internal packages (`cmd/`, `core/`, `engine/`, `llm/`). The goal is better organization, testability, and consistency with the `remote/` module which already uses packages. No functional changes — behavior must be identical before and after.

## Glossary

- **Orchestrator**: The Go module at `orchestrator/` that runs sequential AI agent pipelines.
- **Package**: A Go package — a directory containing `.go` files sharing the same `package` declaration.
- **Module_Path**: The Go module identifier declared in `go.mod` (`module orchestrator`).
- **Core_Package**: The `orchestrator/core` package containing configuration, pipeline types, and variable resolution.
- **Engine_Package**: The `orchestrator/engine` package containing step execution, checkpoint, and flow logic.
- **LLM_Package**: The `orchestrator/llm` package containing the OpenRouter API client and debug logger.
- **Cmd_Package**: The `orchestrator/cmd` package containing the CLI entry point and flag parsing.
- **Exported_Symbol**: A Go identifier starting with an uppercase letter, accessible from other packages.
- **Internal_Import**: An import path referencing a package within the same module (e.g., `orchestrator/core`).

## Requirements

### Requirement 1: Module Path Preservation

**User Story:** As a developer, I want the Go module path to remain unchanged, so that downstream tooling and build commands continue working.

#### Acceptance Criteria

1. THE Orchestrator SHALL declare `module orchestrator` in `go.mod` after the refactoring
2. THE Orchestrator SHALL preserve the same Go version declaration in `go.mod` after the refactoring
3. THE Orchestrator SHALL preserve the same dependency list in `go.mod` after the refactoring

### Requirement 2: Package Directory Structure

**User Story:** As a developer, I want source files organized into `cmd/`, `core/`, `engine/`, and `llm/` packages, so that responsibilities are clearly separated.

#### Acceptance Criteria

1. THE Orchestrator SHALL contain a `cmd/` directory with a `main.go` file declaring `package main`
2. THE Orchestrator SHALL contain a `core/` directory with files declaring `package core`
3. THE Orchestrator SHALL contain an `engine/` directory with files declaring `package engine`
4. THE Orchestrator SHALL contain an `llm/` directory with files declaring `package llm`
5. THE Orchestrator SHALL place `config.go`, `pipeline.go`, and `variables.go` source logic in the Core_Package
6. THE Orchestrator SHALL place `executor.go`, `checkpoint.go`, and `flow.go` source logic in the Engine_Package
7. THE Orchestrator SHALL place `client.go` (LLM API client) and `logger.go` (LLM debug logging) source logic in the LLM_Package
8. THE Orchestrator SHALL place the CLI entry point, flag parsing, and lifecycle handling in the Cmd_Package

### Requirement 3: Internal Import Paths

**User Story:** As a developer, I want packages to import each other using module-relative paths, so that Go tooling resolves dependencies correctly.

#### Acceptance Criteria

1. THE Cmd_Package SHALL import Core_Package using the path `orchestrator/core`
2. THE Cmd_Package SHALL import Engine_Package using the path `orchestrator/engine`
3. THE Cmd_Package SHALL import LLM_Package using the path `orchestrator/llm`
4. THE Engine_Package SHALL import Core_Package using the path `orchestrator/core`
5. THE Engine_Package SHALL import LLM_Package using the path `orchestrator/llm`
6. THE LLM_Package SHALL NOT import Core_Package or Engine_Package

### Requirement 4: Symbol Export for Cross-Package Access

**User Story:** As a developer, I want types and functions used across packages to be exported, so that Go compilation succeeds with the new package boundaries.

#### Acceptance Criteria

1. WHEN a type is referenced by another package, THE Core_Package SHALL export that type with an uppercase initial letter
2. WHEN a function is referenced by another package, THE Core_Package SHALL export that function with an uppercase initial letter
3. WHEN a type is referenced by another package, THE Engine_Package SHALL export that type with an uppercase initial letter
4. WHEN a function is referenced by another package, THE Engine_Package SHALL export that function with an uppercase initial letter
5. WHEN the ConditionEvaluator interface is used by Engine_Package, THE LLM_Package SHALL export the ConditionEvaluator interface
6. THE Core_Package SHALL export the following types: Config, Pipeline, PipelineElement, Step, Loop, Condition
7. THE Core_Package SHALL export the following functions: LoadConfig, ParsePipelineRaw, ParsePipeline, ValidatePipeline, ParseCLIVars, MergeVariables, SubstituteVariables, FindPlaceholders, ResolveString
8. THE LLM_Package SHALL export the following types: ConditionEvaluator, LLMClient, LLMLogger
9. THE LLM_Package SHALL export the following functions: NewLLMClient, NewLLMLogger
10. THE Engine_Package SHALL export the following types: Executor, CommandRunner, OSCommandRunner, Checkpoint, FlowSegment, FlowCheckpoint
11. THE Engine_Package SHALL export the following functions: SaveCheckpoint, LoadCheckpoint, ClearCheckpoint, ClearAllCheckpoints, LoadAllCheckpoints, IsRateLimitError, ParseResetTime, SaveFlowCheckpoint, LoadFlowCheckpoint, ClearFlowCheckpoint

### Requirement 5: Dependency Isolation

**User Story:** As a developer, I want each package to depend only on what it needs, so that the dependency graph is clear and testable.

#### Acceptance Criteria

1. THE Core_Package SHALL depend only on `github.com/joho/godotenv` and `gopkg.in/yaml.v3` from external modules
2. THE LLM_Package SHALL depend only on the Go standard library (net/http, encoding/json)
3. THE Engine_Package SHALL depend on Core_Package, LLM_Package, and the Go standard library
4. THE Cmd_Package SHALL depend on Core_Package, Engine_Package, LLM_Package, and `gopkg.in/yaml.v3`

### Requirement 6: Behavioral Equivalence

**User Story:** As a developer, I want the refactored orchestrator to behave identically to the original, so that no regressions are introduced.

#### Acceptance Criteria

1. THE Orchestrator SHALL produce identical CLI output for the same pipeline inputs before and after the refactoring
2. THE Orchestrator SHALL produce the same exit codes for the same pipeline inputs before and after the refactoring
3. THE Orchestrator SHALL execute pipeline steps in the same order before and after the refactoring
4. THE Orchestrator SHALL evaluate conditions with the same logic before and after the refactoring
5. THE Orchestrator SHALL handle checkpoints with the same file format and paths before and after the refactoring
6. THE Orchestrator SHALL handle rate limit detection with the same patterns before and after the refactoring
7. THE Orchestrator SHALL handle variable substitution with the same behavior before and after the refactoring

### Requirement 7: Build and Binary Compatibility

**User Story:** As a developer, I want the build command and resulting binary to remain compatible, so that existing scripts and CI pipelines continue working.

#### Acceptance Criteria

1. WHEN `go build -o trayline-run ./cmd` is executed from the orchestrator root, THE Orchestrator SHALL produce a working binary
2. THE Orchestrator SHALL produce a binary with the same CLI interface (flags, subcommands, usage text)
3. THE Orchestrator SHALL produce a binary named `trayline-run` that accepts the same arguments as before
4. THE Orchestrator SHALL support the `flow` subcommand with the same syntax and behavior

### Requirement 8: Test Migration

**User Story:** As a developer, I want all existing tests to move with their source files and continue passing, so that test coverage is preserved.

#### Acceptance Criteria

1. THE Core_Package SHALL contain test files for config, pipeline, and variables logic
2. THE Engine_Package SHALL contain test files for executor, checkpoint, and flow logic
3. THE LLM_Package SHALL contain test files for client and logger logic
4. WHEN `go test ./...` is executed from the orchestrator root, THE Orchestrator SHALL run all tests successfully
5. THE Orchestrator SHALL preserve property-based tests using `pgregory.net/rapid` in their respective packages

### Requirement 9: No Circular Dependencies

**User Story:** As a developer, I want packages to have a clean dependency hierarchy, so that Go compilation succeeds and the architecture is maintainable.

#### Acceptance Criteria

1. THE Orchestrator SHALL have no circular import dependencies between packages
2. THE LLM_Package SHALL NOT depend on Core_Package or Engine_Package
3. THE Core_Package SHALL NOT depend on Engine_Package or LLM_Package
4. THE Engine_Package SHALL NOT depend on Cmd_Package

### Requirement 10: Root Directory Cleanup

**User Story:** As a developer, I want the orchestrator root directory to contain only module files and package directories, so that the flat structure is fully eliminated.

#### Acceptance Criteria

1. WHEN the refactoring is complete, THE Orchestrator root directory SHALL NOT contain any `.go` source files with `package main` declarations (except within `cmd/`)
2. WHEN the refactoring is complete, THE Orchestrator root directory SHALL retain `go.mod` and `go.sum` at the module root
3. WHEN the refactoring is complete, THE Orchestrator root directory SHALL contain only `cmd/`, `core/`, `engine/`, `llm/` as Go source directories
