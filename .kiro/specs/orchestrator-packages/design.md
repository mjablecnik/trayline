# Design Document: Orchestrator Package Restructure

## Overview

Refactoring the orchestrator Go module from a flat `package main` (12 source files in root) into 4 internal packages: `cmd/`, `core/`, `engine/`, `llm/`. The module path stays `module orchestrator`. All behavior remains identical — only package boundaries, symbol visibility, and import paths change.

### Target Directory Layout

```
orchestrator/
├── cmd/
│   └── main.go              # CLI entry point, flag parsing, lifecycle wrapper
├── core/
│   ├── config.go            # Config struct, LoadConfig
│   ├── pipeline.go          # Pipeline/Step/Loop/Condition types, YAML parsing, validation
│   └── variables.go         # Variable resolution, substitution
├── engine/
│   ├── executor.go          # Executor struct, step execution, loops, conditions, dry-run
│   ├── checkpoint.go        # Checkpoint save/load/clear, rate limit detection
│   └── flow.go              # Flow subcommand, segment parsing, multi-pipeline execution
├── llm/
│   ├── client.go            # ConditionEvaluator interface, LLMClient, OpenRouter API
│   └── logger.go            # LLMLogger debug wrapper
├── go.mod                   # module orchestrator (unchanged)
├── go.sum
├── .env.example
└── README.md
```

## Architecture

### Package Dependency Graph

```
cmd/ → core/, engine/, llm/
engine/ → core/, llm/
core/ → (external: yaml.v3, godotenv)
llm/ → (standard library only)
```

No circular dependencies. The hierarchy is strictly layered:
- `llm/` is a leaf package (no internal deps)
- `core/` is a leaf package (no internal deps)
- `engine/` depends on both `core/` and `llm/`
- `cmd/` depends on all three

### ConditionEvaluator Interface Placement

The `ConditionEvaluator` interface lives in `llm/` because:
- It's defined alongside its implementations (`LLMClient`, `LLMLogger`)
- `engine/` imports `llm/` to reference the interface type
- This avoids circular dependencies (if it were in `core/`, `llm/` would need to import `core/`)

## Components and Interfaces

### core/ — Types, Config, and Pipeline Logic

Contains all data types and pure parsing/validation logic.

**Exported symbols:**

```go
package core

// Types
type Config struct { ... }
type Pipeline struct { ... }
type PipelineElement struct { ... }
type Step struct { ... }
type Loop struct { ... }
type Condition struct { ... }

// Config
func LoadConfig() *Config

// Pipeline parsing
func ParsePipelineRaw(path string) (*Pipeline, map[string]string, error)
func ParsePipeline(path string) (*Pipeline, error)
func ValidatePipeline(p *Pipeline) error

// Variables
func ParseCLIVars(flags []string) (map[string]string, error)
func MergeVariables(yamlVars, cliVars map[string]string) map[string]string
func SubstituteVariables(p *Pipeline, vars map[string]string) error
func FindPlaceholders(s string) []string
func ResolveString(s string, vars map[string]string) string

// Pipeline methods
func (p *Pipeline) TopLevelStepNames() []string
func (p *Pipeline) FlattenStepNames() []string
func (p *Pipeline) NeedsLLM() bool
```

**Internal (unexported) helpers that stay within core/:**
- `validateStep`, `validateCondition`, `validateLoop`
- `flattenElements`, `elementsNeedLLM`
- `placeholderRegex`, `validKeyRegex`
- `rawPipeline` (YAML marshaling helper)
- `defaultModel` constant

### llm/ — LLM Client and Logger

Contains the AI condition evaluation logic. Zero internal dependencies.

**Exported symbols:**

```go
package llm

// Interface
type ConditionEvaluator interface {
    Evaluate(content string, conditionPrompt string) (bool, error)
}

// Client
type LLMClient struct { ... }
func NewLLMClient(apiKey, model string) *LLMClient
func (c *LLMClient) Evaluate(content, conditionPrompt string) (bool, error)

// Logger wrapper
type LLMLogger struct { ... }
func NewLLMLogger(inner ConditionEvaluator) (*LLMLogger, error)
func (l *LLMLogger) Evaluate(content, conditionPrompt string) (bool, error)
func (l *LLMLogger) Close()
func (l *LLMLogger) Log(format string, args ...interface{})
func (l *LLMLogger) LogSection(title string)
func (l *LLMLogger) LogError(context string, err error)
```

**Internal (unexported):**
- `parseLLMDecision`, `doEvaluate`
- `systemPrompt`, `openRouterBaseURL`, `llmTimeout`, `llmLogFile` constants
- `LLMRequest`, `LLMMessage`, `LLMResponse` (API types)

### engine/ — Execution Engine

Contains all runtime execution logic: running steps, checkpointing, flow orchestration.

**Exported symbols:**

```go
package engine

// Executor
type Executor struct {
    Config        *core.Config
    Pipeline      *core.Pipeline
    LLM           llm.ConditionEvaluator
    DryRun        bool
    Verbose       bool
    Runner        CommandRunner
    ResolvedVars  map[string]string
    LogTask       string
    PipelineName  string
    Restart       bool
    RateLimitOutput string
}
func (e *Executor) Run() int

// Command runner interface
type CommandRunner interface {
    RunAgent(agent, prompt, model, projectDir string, env []string, verbose bool, stdout, stderr io.Writer) (string, int, error)
    RunCommand(command, projectDir string, env []string, verbose bool, stdout, stderr io.Writer) (string, int, error)
}
type OSCommandRunner struct{}

// Checkpoint
type Checkpoint struct { ... }
type FlowCheckpoint struct { ... }
type FlowSegmentState struct { ... }
func SaveCheckpoint(pipelineName string, variables map[string]string, completedSteps []string, nextStep string, rateLimited bool) error
func LoadCheckpoint(pipelineName string, variables map[string]string) *Checkpoint
func ClearCheckpoint(pipelineName string)
func ClearAllCheckpoints()
func LoadAllCheckpoints() []*Checkpoint
func IsRateLimitError(output string) bool
func ParseResetTime(output string) time.Time
func SaveFlowCheckpoint(segments []*FlowSegment, completedSegments int) error
func LoadFlowCheckpoint(segments []*FlowSegment) *FlowCheckpoint
func ClearFlowCheckpoint()
func (cp *Checkpoint) IsStepCompleted(stepName string) bool

// Flow
type FlowSegment struct {
    PipelinePath string
    Vars         map[string]string
}
func RunFlow(args []string) int
```

**Internal (unexported):**
- `runSubprocess`, `resolveAgentBinary`, `getDepth`, `indent`
- `checkpointPath`, `checkpointDir`, `flowCheckpointFile`
- `rateLimitPatterns`, `ansiRegex`, `stripANSI`
- `colorReset`, `colorRed`, etc. (ANSI constants)
- `executeFlow`, `executeSinglePipeline`, `syncBetweenPipelines`
- `parseFlowArgs`, `parseSegment`, `flowUsageText`
- `runFlowWithLifecycle`, `parseLogTask`
- `countStepsInElements`, `printCondition`, `printElements`
- All executor methods except `Run()`

### cmd/ — CLI Entry Point

Thin entry point that wires everything together.

**Contents:**

```go
package main

import (
    "orchestrator/core"
    "orchestrator/engine"
    "orchestrator/llm"
)

var version = "2.4.0"

func main() { ... }
func run(args []string) int { ... }
func findLifecycleFile() string { ... }
func runWithLifecycle(executor *engine.Executor, lifecyclePath string, pipelineName string, vars map[string]string, verbose bool) int { ... }
```

**Key decisions for cmd/:**
- `runWithLifecycle` stays in `cmd/` because it's CLI-specific lifecycle logic (reads lifecycle.yaml, runs before/after steps, handles retry). It uses `engine.Executor`, `engine.OSCommandRunner`, `core.Step` types.
- `findLifecycleFile` stays in `cmd/` because it's deployment-path specific.
- `varFlags` type stays in `cmd/` (flag parsing helper).
- The `flow` subcommand dispatch (`runFlow`) is delegated to `engine.RunFlow`.

## Data Models

No new data models. All existing types (`Pipeline`, `Step`, `Loop`, `Condition`, `Config`, `Checkpoint`, `FlowCheckpoint`, `FlowSegment`, `Executor`, `CommandRunner`, `LLMClient`, `LLMLogger`) keep their fields unchanged. Only their package location and visibility change.

### Symbol Renaming Summary

| Current (package main) | New Location | Visibility Change |
|------------------------|--------------|-------------------|
| `Pipeline` | `core.Pipeline` | Already uppercase ✓ |
| `PipelineElement` | `core.PipelineElement` | Already uppercase ✓ |
| `Step` | `core.Step` | Already uppercase ✓ |
| `Loop` | `core.Loop` | Already uppercase ✓ |
| `Condition` | `core.Condition` | Already uppercase ✓ |
| `Config` | `core.Config` | Already uppercase ✓ |
| `LoadConfig` | `core.LoadConfig` | Already uppercase ✓ |
| `ParsePipelineRaw` | `core.ParsePipelineRaw` | Already uppercase ✓ |
| `ValidatePipeline` | `core.ValidatePipeline` | Already uppercase ✓ |
| `ParseCLIVars` | `core.ParseCLIVars` | Already uppercase ✓ |
| `MergeVariables` | `core.MergeVariables` | Already uppercase ✓ |
| `SubstituteVariables` | `core.SubstituteVariables` | Already uppercase ✓ |
| `ConditionEvaluator` | `llm.ConditionEvaluator` | Already uppercase ✓ |
| `LLMClient` | `llm.LLMClient` | Already uppercase ✓ |
| `NewLLMClient` | `llm.NewLLMClient` | Already uppercase ✓ |
| `LLMLogger` | `llm.LLMLogger` | Already uppercase ✓ |
| `NewLLMLogger` | `llm.NewLLMLogger` | Already uppercase ✓ |
| `Executor` | `engine.Executor` | Already uppercase ✓ |
| `CommandRunner` | `engine.CommandRunner` | Already uppercase ✓ |
| `OSCommandRunner` | `engine.OSCommandRunner` | Already uppercase ✓ |
| `Checkpoint` | `engine.Checkpoint` | Already uppercase ✓ |
| `FlowSegment` | `engine.FlowSegment` | Already uppercase ✓ |
| `SaveCheckpoint` | `engine.SaveCheckpoint` | Already uppercase ✓ |
| `LoadCheckpoint` | `engine.LoadCheckpoint` | Already uppercase ✓ |
| `ClearCheckpoint` | `engine.ClearCheckpoint` | Already uppercase ✓ |
| `IsRateLimitError` | `engine.IsRateLimitError` | Already uppercase ✓ |
| `ParseResetTime` | `engine.ParseResetTime` | Already uppercase ✓ |
| `runFlow` | `engine.RunFlow` | **Needs export** (lowercase → uppercase) |

Most symbols are already exported (uppercase). Only `runFlow` needs renaming to `RunFlow`.

## Error Handling

No changes to error handling logic. All error types, error messages, and exit codes remain identical. The refactoring only changes where error-producing functions live, not how they produce or propagate errors.

## Correctness Properties

### Property 1: Compilation Success

All four packages compile independently and together via `go build ./...` from the module root.

**Validates: Requirements 1.1, 7.1, 7.2, 7.3**

### Property 2: Test Equivalence

All existing tests pass via `go test ./...` from the module root. Tests move to their respective package directories and maintain the same assertions.

**Validates: Requirements 8.1, 8.2, 8.3, 8.4, 8.5**

### Property 3: No Circular Imports

The import graph is a DAG: `cmd → {core, engine, llm}`, `engine → {core, llm}`, with `core` and `llm` as leaves.

**Validates: Requirements 9.1, 9.2, 9.3, 9.4**

### Property 4: Behavioral Equivalence

The binary produced by `go build ./cmd` exhibits identical behavior to the current binary for all inputs (same output, same exit codes, same checkpoint files, same LLM interactions).

**Validates: Requirements 6.1, 6.2, 6.3, 6.4, 6.5, 6.6, 6.7**

### Property 5: Dependency Isolation

Each package imports only what it declares: `core` depends on godotenv+yaml.v3, `llm` depends on stdlib only, `engine` depends on core+llm+stdlib, `cmd` depends on all.

**Validates: Requirements 5.1, 5.2, 5.3, 5.4**

### Property 6: Clean Root

No `.go` files with `package main` exist in the module root directory after refactoring.

**Validates: Requirements 10.1, 10.2, 10.3**

## Testing Strategy

### Build Verification

```bash
cd orchestrator && go build ./...
```

This single command verifies all packages compile and all import paths are correct.

### Test Migration

Tests move with their source files:
- `config_test.go` → `core/config_test.go`
- `pipeline_test.go` → `core/pipeline_test.go`
- `variables_test.go` → `core/variables_test.go`
- `executor_test.go` → `engine/executor_test.go`
- `checkpoint_test.go`, `checkpoint_ratelimit_test.go` → `engine/checkpoint_test.go`, `engine/checkpoint_ratelimit_test.go`
- `flow_test.go` → `engine/flow_test.go`
- `llm_test.go` → `llm/client_test.go`
- `llm_logger_test.go` → `llm/logger_test.go`
- `main_test.go` → `cmd/main_test.go`

Tests that reference now-unexported functions need one of:
1. Move the helper to an `_test.go` file in the same package (for test-only utilities)
2. Export the function if it's genuinely needed from outside
3. Use `export_test.go` pattern to expose internals for testing

### Full Test Run

```bash
cd orchestrator && go test ./...
```

Must exit 0. Property-based tests (rapid) continue working as-is since they test exported functions.

### Binary Equivalence Test

```bash
# Build from new structure
cd orchestrator && go build -o /tmp/trayline-run-new ./cmd

# Compare help output
/tmp/trayline-run-new --help
/tmp/trayline-run-new --version
/tmp/trayline-run-new --dry-run test-pipeline.yaml
```

Output should be identical to the current binary.
