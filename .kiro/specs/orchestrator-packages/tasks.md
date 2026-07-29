# Implementation Plan: Orchestrator Package Restructure

## Overview

Refactoring the orchestrator Go module from a flat `package main` into 4 internal packages (`cmd/`, `core/`, `engine/`, `llm/`). The module path stays `module orchestrator`. Work is sequenced to build leaf packages first (no internal deps), then dependent packages, then wire everything in cmd/.

## Tasks

- [ ] 1. Create core/ package with config, pipeline types, and variables
  - Create `orchestrator/core/` directory
  - Move `config.go` → `core/config.go`, change `package main` to `package core`
  - Move `pipeline.go` → `core/pipeline.go`, change `package main` to `package core`
  - Move `variables.go` → `core/variables.go`, change `package main` to `package core`
  - Verify all exported symbols remain uppercase (Config, Pipeline, PipelineElement, Step, Loop, Condition, LoadConfig, ParsePipelineRaw, ParsePipeline, ValidatePipeline, ParseCLIVars, MergeVariables, SubstituteVariables, FindPlaceholders, ResolveString)
  - Move corresponding test files: `config_test.go` → `core/config_test.go`, `pipeline_test.go` → `core/pipeline_test.go`, `variables_test.go` → `core/variables_test.go`
  - Update test file package declarations to `package core`
  - Verify: `cd orchestrator && go build ./core`
  - **Requirements:** 2.1, 2.2, 2.3, 4.6, 5.1

- [ ] 2. Create llm/ package with client and logger
  - Create `orchestrator/llm/` directory
  - Move `llm.go` → `llm/client.go`, change `package main` to `package llm`
  - Move `llm_logger.go` → `llm/logger.go`, change `package main` to `package llm`
  - Ensure `ConditionEvaluator` interface is exported in `llm/client.go`
  - Ensure `LLMClient`, `NewLLMClient`, `LLMLogger`, `NewLLMLogger` are exported
  - Ensure `LLMLogger.Log`, `LLMLogger.LogSection`, `LLMLogger.LogError`, `LLMLogger.Close` are exported
  - Move test files: `llm_test.go` → `llm/client_test.go`, `llm_logger_test.go` → `llm/logger_test.go`
  - Update test file package declarations to `package llm`
  - Verify: `cd orchestrator && go build ./llm`
  - **Requirements:** 2.4, 4.8, 4.9, 5.2, 9.2

- [ ] 3. Create engine/ package with executor, checkpoint, and flow
  - Create `orchestrator/engine/` directory
  - Move `executor.go` → `engine/executor.go`, change `package main` to `package engine`
  - Move `checkpoint.go` → `engine/checkpoint.go`, change `package main` to `package engine`
  - Move `flow.go` → `engine/flow.go`, change `package main` to `package engine`
  - Add import statements: `"orchestrator/core"` and `"orchestrator/llm"`
  - Replace all bare type references with qualified names: `*Config` → `*core.Config`, `*Pipeline` → `*core.Pipeline`, `ConditionEvaluator` → `llm.ConditionEvaluator`, `*LLMLogger` → `*llm.LLMLogger`, etc.
  - Rename `runFlow` → `RunFlow` (export for cmd/ to call)
  - Update `Executor` struct fields to use `core.Config`, `core.Pipeline`, `llm.ConditionEvaluator`
  - Update `executeSinglePipeline` to use `core.ParsePipelineRaw`, `core.MergeVariables`, `core.SubstituteVariables`, `core.ValidatePipeline`, `llm.NewLLMClient`, `llm.NewLLMLogger`
  - Export `CommandRunner`, `OSCommandRunner` interfaces/structs
  - Export `Checkpoint`, `FlowCheckpoint`, `FlowSegment`, `FlowSegmentState` types
  - Export all checkpoint functions: `SaveCheckpoint`, `LoadCheckpoint`, `ClearCheckpoint`, `ClearAllCheckpoints`, `LoadAllCheckpoints`, `IsRateLimitError`, `ParseResetTime`, `SaveFlowCheckpoint`, `LoadFlowCheckpoint`, `ClearFlowCheckpoint`
  - Move test files: `executor_test.go` → `engine/executor_test.go`, `checkpoint_test.go` → `engine/checkpoint_test.go`, `checkpoint_ratelimit_test.go` → `engine/checkpoint_ratelimit_test.go`, `flow_test.go` → `engine/flow_test.go`
  - Update test file package declarations to `package engine`
  - Fix test imports to use `core.` and `llm.` qualified types
  - Verify: `cd orchestrator && go build ./engine`
  - **Requirements:** 2.3, 3.1, 3.2, 3.3, 3.4, 3.5, 4.10, 4.11, 5.3, 9.4

- [ ] 4. Create cmd/ package with CLI entry point and lifecycle
  - Create `orchestrator/cmd/` directory
  - Move `main.go` → `cmd/main.go`, keep `package main`
  - Add import statements: `"orchestrator/core"`, `"orchestrator/engine"`, `"orchestrator/llm"`
  - Replace all bare type references with qualified names: `LoadConfig` → `core.LoadConfig`, `ParsePipelineRaw` → `core.ParsePipelineRaw`, `Executor` → `engine.Executor`, `OSCommandRunner` → `engine.OSCommandRunner`, `NewLLMClient` → `llm.NewLLMClient`, `runFlow` → `engine.RunFlow`, etc.
  - Update `runWithLifecycle` to use qualified types (`core.Step`, `engine.OSCommandRunner`, `engine.Executor`, `engine.ParseResetTime`)
  - Keep `varFlags`, `programName`, `usageText`, `version`, `findLifecycleFile` in cmd/ (CLI-specific)
  - Move `main_test.go` → `cmd/main_test.go`
  - Update test imports to use qualified package names
  - Verify: `cd orchestrator && go build ./cmd`
  - **Requirements:** 2.8, 3.1, 3.2, 3.3, 5.4, 7.1, 7.2

- [ ] 5. Remove old root-level Go files and update go.mod
  - Delete all `.go` files from `orchestrator/` root (they've been moved to subdirs)
  - Keep `go.mod`, `go.sum`, `.env.example`, `README.md`, `test-pipeline.yaml` at root
  - Remove stale build artifacts (`orchestrator/orchestrator`, `orchestrator/trayline-run`) if present
  - Run `go mod tidy` from orchestrator root to clean up dependencies
  - Verify no `.go` files remain in root: only `cmd/`, `core/`, `engine/`, `llm/` contain Go source
  - **Requirements:** 10.1, 10.2, 10.3

- [ ] 6. Update build command in setup/install.sh
  - Change orchestrator build command from `go build -o "$TRAYLINE_HOME/trayline-run" .` to `go build -o "$TRAYLINE_HOME/trayline-run" ./cmd`
  - Verify install.sh still builds the binary correctly
  - **Requirements:** 7.1, 7.3

- [ ] 7. Update README.md with new project structure
  - Update the "Project Structure" section in `orchestrator/README.md` to show the new package layout
  - Update the "Build" section: `go build -o trayline-run ./cmd`
  - Update the "Testing" section: `go test ./...` (unchanged, just verify it's documented)
  - **Requirements:** 7.2

- [ ] 8. Final verification — all builds and tests pass
  - Run `cd orchestrator && go build ./...` — must exit 0
  - Run `cd orchestrator && go test ./...` — must exit 0
  - Run `cd orchestrator && go build -o /tmp/trayline-run-test ./cmd` — must produce working binary
  - Test: `/tmp/trayline-run-test --version` outputs version
  - Test: `/tmp/trayline-run-test --help` outputs usage
  - Test: `/tmp/trayline-run-test --dry-run test-pipeline.yaml` works
  - Verify no circular dependencies: `go vet ./...` passes
  - Verify root has no .go files: `ls orchestrator/*.go` returns nothing
  - **Requirements:** 1.1, 1.2, 1.3, 6.1, 6.2, 6.3, 6.4, 6.5, 6.6, 6.7, 7.1, 7.4, 8.4, 9.1

## Task Dependency Graph

```json
{
  "waves": [
    {"tasks": [1, 2]},
    {"tasks": [3]},
    {"tasks": [4]},
    {"tasks": [5]},
    {"tasks": [6, 7]},
    {"tasks": [8]}
  ]
}
```

- **Wave 1**: Tasks 1 and 2 can run in parallel — core/ and llm/ are leaf packages with no internal dependencies
- **Wave 2**: Task 3 depends on 1+2 (engine/ imports core/ and llm/)
- **Wave 3**: Task 4 depends on 1+2+3 (cmd/ imports all three)
- **Wave 4**: Task 5 depends on 4 (safe to delete root files only after cmd/ is working)
- **Wave 5**: Tasks 6+7 can run in parallel (install.sh update and README update)
- **Wave 6**: Task 8 is the final verification gate

## Notes

- All files are currently `package main` — changing to `package core`/`engine`/`llm` means every function/type used from another package must be uppercase (exported). Most already are, since they were designed with clear naming. The exception is `runFlow` → `RunFlow`.
- Test files that use unexported helpers: if a test calls a now-unexported function from another package, either export it, inline it in the test file, or use the `export_test.go` pattern.
- The `test-pipeline.yaml` fixture stays at the module root for `cmd/main_test.go` integration tests.
- The `varFlags` type is CLI-specific and stays in `cmd/` — it's only used by flag parsing.
- ANSI color constants (`colorRed`, etc.) are used only in `engine/` — they stay unexported there.
- The build command changes from `go build -o trayline-run .` to `go build -o trayline-run ./cmd`. This affects `setup/install.sh` and must be updated there.
