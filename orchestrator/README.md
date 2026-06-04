# Trayline Orchestrator

A Go CLI that reads a YAML pipeline definition and sequentially executes `trayline-agent` commands and shell commands. Supports LLM-based loops for iterative refinement and step-level conditions with optional goto jumps.

Compiles into a single static binary with no runtime dependencies beyond the Go standard library and the `trayline-agent` script on PATH.

## Build

```bash
cd orchestrator
go build -ldflags "-X main.version=1.0.0" -o trayline-run .
```

## Usage

```
trayline-run --pipeline <path> [--dry-run] [--verbose] [--var key=value ...]
trayline-run --version
trayline-run --help
```

Flags:

- `--pipeline` — Path to pipeline YAML file (required)
- `--var key=value` — Set or override a pipeline variable (repeatable)
- `--dry-run` — Print pipeline steps without executing
- `--verbose` — Stream trayline-agent output to stdout in real time
- `--version` — Print version and exit
- `--help, -h` — Show help message

## Configuration

Copy `.env.example` to `.env` and fill in your values:

```bash
cp .env.example .env
```

Environment variables:

| Variable | Required | Default | Description |
|---|---|---|---|
| `OPENROUTER_API_KEY` | Yes (if pipeline uses conditions) | — | API key for OpenRouter LLM requests |
| `OPENROUTER_MODEL` | No | `openai/gpt-4.1-nano` | LLM model for condition evaluation |

The orchestrator loads `.env` from the current working directory automatically. If the file doesn't exist, it continues with existing environment variables.

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
    agent: "claude"          # "claude" or "kiro"
    prompt: "Read the spec and create the code"
    project_dir: "/path/to/project"  # optional, defaults to cwd
```

### Command Step

Runs a shell command via `sh -c`:

```yaml
steps:
  - name: "run-tests"
    command: "go test ./..."
    project_dir: "/path/to/project"  # optional, defaults to cwd
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

### Loop

Repeats a group of steps with an LLM-based exit condition. The loop runs until the LLM returns `false` or `max_iterations` is reached:

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
- Invalid agent type (must be `kiro` or `claude`)
- Duplicate step names (across entire pipeline including loops)
- Condition missing `prompt`
- `goto` referencing a non-existent top-level step
- Loop missing `max_iterations`, `steps`, or `condition`
- `max_iterations` ≤ 0
- Conditions inside loop steps (not supported)

## Testing

```bash
cd orchestrator
go test ./... -v
```

Tests include both unit tests and property-based tests using [pgregory.net/rapid](https://github.com/flyingmutant/rapid).

## Project Structure

```
orchestrator/
├── main.go           # CLI entry point, flag parsing
├── config.go         # Environment loading, configuration
├── pipeline.go       # YAML parsing, validation, pipeline types
├── executor.go       # Step execution, loop handling, condition routing
├── llm.go            # OpenRouter API client, LLM decision parsing
├── config_test.go    # Config loading tests
├── pipeline_test.go  # Parsing, validation, round-trip property tests
├── executor_test.go  # Execution, condition routing, loop property tests
├── llm_test.go       # LLM client, retry, response parsing tests
├── main_test.go      # CLI flags, dry-run, integration tests
├── .env.example      # Environment variable template
├── go.mod
└── go.sum
```
