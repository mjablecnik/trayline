# Trayline Orchestrator

A Go CLI that reads a YAML pipeline definition and sequentially executes `trayline-agent` commands and shell commands. Supports LLM-based loops for iterative refinement and step-level conditions with optional goto jumps.

Compiles into a single static binary with no runtime dependencies beyond the Go standard library and the `trayline-agent` script on PATH.

## Build

```bash
cd orchestrator
go build -o trayline-run ./cmd
```

## Usage

```
trayline-run <pipeline> [--dry-run] [--verbose] [--log-llm] [--no-lifecycle] [--restart] [--var key=value ...]
trayline-run flow <pipeline> [--then <pipeline> ...] [--dry-run] [--verbose] [--no-lifecycle]
trayline-run stop
trayline-run --version
trayline-run --help
```

Flags:

- `--var key=value` — Set or override a pipeline variable (repeatable)
- `--dry-run` — Print pipeline steps without executing
- `--verbose` — Stream trayline-agent output to stdout in real time
- `--log-llm` — Log all LLM requests and responses to llm-debug.log
- `--no-lifecycle` — Skip lifecycle.yaml before/after steps
- `--restart` — Ignore checkpoint and start pipeline from the beginning
- `--version` — Print version and exit
- `--help, -h` — Show help message

Subcommands:

- `flow` — Run multiple pipelines sequentially separated by `--then`
- `stop` — Signal a running pipeline to stop gracefully after its current step

## Configuration

Copy `.env.example` to `.env` and fill in your values:

```bash
cp .env.example .env
```

Environment variables:

| Variable | Required | Default | Description |
|---|---|---|---|
| `OPENROUTER_API_KEY` | Yes (if pipeline uses conditions) | — | API key for OpenRouter LLM requests |
| `OPENROUTER_MODEL` | No | `openai/gpt-4.1-mini` | LLM model for condition evaluation |

The orchestrator tries `.env` in the current working directory first (development override), then falls back to `~/.trayline/env/orchestrator.env` (the file installed by `setup/install.sh`). If neither exists, it continues with existing environment variables.

## Pipeline YAML Format

A pipeline is a YAML file with a top-level `steps` key containing an ordered list of steps and loops.

### Variables

Define a flat `variables` map at the top of the pipeline file and reference values with `{{variable-name}}` placeholders in templatable fields (`prompt`, `command`, `project_dir`, `condition.prompt`, `condition.file`):

```yaml
variables:
  project-path: "/home/user/myproject"
  spec-name: "agent-orchestrator"

steps:
  - name: "create-code"
    agent: "claude"
    prompt: "Read specs from .kiro/specs/{{spec-name}} and implement in {{project-path}}"
    project_dir: "{{project-path}}"

  - name: "run-tests"
    command: "cd {{project-path}} && go test ./..."
```

Override or add variables at runtime with `--var`:

```bash
trayline-run --pipeline workflow.yaml --var project-path=/tmp/proj --var spec-name=my-spec
```

Rules:
- Variable keys must contain lowercase letters, digits, and hyphens only
- CLI `--var` values override YAML-defined values; last occurrence wins for duplicate keys
- Placeholders referencing undefined variables cause an immediate error before execution
- Use `--dry-run` to preview resolved variable values before running

### Agent Step

Runs `trayline-agent` with the specified agent type and prompt:

```yaml
steps:
  - name: "create-code"
    agent: "claude"          # "claude", "kiro", or "cline"
    model: "sonnet"          # optional, overrides default model
    effort: "high"           # optional, thinking/effort level (low, medium, high, xhigh, max)
    prompt: "Read the spec and create the code"
    project_dir: "/path/to/project"  # optional, defaults to cwd
```

The `effort` field controls how much reasoning the model applies. Maps to:
- Kiro CLI: `--effort <level>`
- Claude Code: `--effort <level>`
- Cline CLI: `--thinking <level>`

If omitted, each agent uses its own default (Kiro: persistent preference, Claude: adaptive, Cline: medium).

### Command Step

Runs a shell command via `sh -c`:

```yaml
steps:
  - name: "run-tests"
    command: "go test ./..."
    project_dir: "/path/to/project"  # optional, defaults to cwd
```

### Common Step Fields

Both agent and command steps also accept:

```yaml
steps:
  - name: "optional-step"
    command: "echo hi"
    skip: "true"       # optional, "true"/"false" (or a {{variable}}) — skips this step when true
    log: true           # optional, runs the lifecycle log-task (default tasks/update-ai-log) after this step succeeds
```

### Step with Condition (gate)

When no `goto` is specified, `true` means continue to the next step, `false` stops the pipeline:

```yaml
steps:
  - name: "test"
    agent: "kiro"
    prompt: "Run all tests"
    condition:
      prompt: "Do all tests pass based on the output?"
```

### Step with Condition (goto)

When `goto` is specified, `true` jumps to the target step, `false` continues normally:

```yaml
steps:
  - name: "review"
    agent: "kiro"
    prompt: "Review the code and write CODE_REVIEW.md"
    condition:
      prompt: "Are there critical issues that need fixing?"
      file: "CODE_REVIEW.md"       # optional, reads file instead of step output
      goto: "fix-code"             # jump target (must be a top-level step name)
```

### Condition Modes

A `condition` must specify exactly one of `prompt` (LLM-evaluated, shown above), `contains`, `not_contains`, `matches`, or `not_matches`. The latter four are plain string/regex checks against the step output (or `file`, if given) — no LLM call:

```yaml
steps:
  - name: "run-tests"
    command: "go test ./... > test-results.txt 2>&1 || true"
    condition:
      not_contains: "FAIL"      # true if the output does NOT contain "FAIL"
      file: "test-results.txt"  # optional, reads file instead of step output
```

Same gate/goto semantics as the LLM-based condition above: without `goto`, `true` continues and `false` stops the pipeline; with `goto`, `true` jumps to the target step.

- `contains: "text"` — true if the input contains the substring
- `not_contains: "text"` — true if the input does not contain the substring
- `matches: "regex"` — true if the input matches the regex (at least one occurrence)
- `not_matches: "regex"` — true if the input does not match the regex

### Loop

Repeats a group of steps with an exit condition (LLM prompt or string/regex match, see above). The loop runs until the condition returns `false` or `max_iterations` is reached:

```yaml
steps:
  - loop:
      max_iterations: 3
      condition:
        prompt: "Are there still failing tests that need fixing?"
        file: "test-results.txt"   # optional
      steps:
        - name: "run-tests"
          command: "go test ./... > test-results.txt 2>&1"
        - name: "fix-tests"
          agent: "claude"
          prompt: "Fix the failing tests"
```

### Full Example

```yaml
steps:
  - name: "create-code"
    agent: "claude"
    prompt: |
      Read the spec in SPEC.md and implement the solution.
      Write clean, tested code.
    project_dir: "/home/user/myproject"

  - name: "run-tests"
    command: "go test ./..."
    project_dir: "/home/user/myproject"

  - loop:
      max_iterations: 3
      condition:
        prompt: "Are there still failing tests?"
        file: "test-results.txt"
      steps:
        - name: "test-loop"
          command: "go test ./... 2>&1 | tee test-results.txt"
        - name: "fix-loop"
          agent: "claude"
          prompt: "Fix the failing tests based on test-results.txt"

  - name: "review"
    agent: "kiro"
    prompt: "Review the code and write CODE_REVIEW.md"
    condition:
      prompt: "Are there critical issues that need fixing?"
      file: "CODE_REVIEW.md"
      goto: "fix-review"

  - name: "fix-review"
    agent: "claude"
    prompt: "Fix the issues described in CODE_REVIEW.md"
```

## Validation Rules

The orchestrator validates the pipeline at parse time and fails fast on:

- Missing or invalid YAML
- Steps missing `name`
- Steps with both `agent` and `command`, or neither
- Invalid agent type (must be `kiro`, `claude`, or `cline`)
- Duplicate step names (across entire pipeline including loops)
- Condition not specifying exactly one of `prompt`, `contains`, `not_contains`, `matches`, or `not_matches`
- Invalid regex in `matches`/`not_matches`
- `goto` referencing a non-existent top-level step
- `goto` inside a loop step's condition (not supported — loop step conditions are gate-only)
- Loop missing `max_iterations` or `steps`
- Loop missing a loop-level `condition`, unless at least one step inside it already has its own `condition`
- `max_iterations` ≤ 0

## Testing

```bash
cd orchestrator
go test ./... -v
```

Tests include both unit tests and property-based tests using [pgregory.net/rapid](https://github.com/flyingmutant/rapid).

## Project Structure

```
orchestrator/
├── cmd/
│   ├── main.go              # CLI entry point, flag parsing, lifecycle wrapper
│   └── main_test.go         # CLI flags, dry-run, integration tests
├── core/
│   ├── config.go            # Environment loading, configuration
│   ├── config_test.go
│   ├── pipeline.go          # YAML parsing, validation, pipeline types
│   ├── pipeline_test.go     # Parsing, validation, round-trip property tests
│   ├── variables.go         # Variable resolution, substitution
│   └── variables_test.go
├── engine/
│   ├── executor.go          # Step execution, loop handling, condition routing
│   ├── executor_test.go     # Execution, condition routing, loop property tests
│   ├── checkpoint.go        # Checkpoint save/load/clear, rate limit detection
│   ├── checkpoint_test.go
│   ├── checkpoint_ratelimit_test.go
│   ├── flow.go              # Flow subcommand, segment parsing, multi-pipeline execution
│   └── flow_test.go
├── llm/
│   ├── client.go            # OpenRouter API client, LLM decision parsing
│   ├── client_test.go       # LLM client, retry, response parsing tests
│   ├── logger.go            # LLMLogger debug wrapper
│   └── logger_test.go
├── .env.example      # Environment variable template
├── go.mod
└── go.sum
```
