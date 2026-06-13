# Pipelines

Documentation for the trayline pipeline system — tasks, processes, and workflows that orchestrate AI agent actions.

## Structure Overview

```
pipelines/
├── lifecycle.yaml          ← Wraps every run: sync-pull → [run] → sync-push
├── tasks/                  ← Atomic operations (smallest runnable units)
├── processes/              ← Standalone processes with clear output
└── workflows/              ← Composed processes for full development cycles
```

## Tasks

Atomic operations that perform a single responsibility. These are the smallest runnable units.

| Task | Description |
|------|-------------|
| `check-build` | Verifies project builds, runs, lints. Fixes issues until clean. |
| `release` | Bumps version, updates CHANGELOG.md, creates git tag. |
| `sync-pull` | Pulls from bare repo with conflict resolution. |
| `sync-push` | Pushes to bare repo with conflict resolution. |
| `update-ai-log` | Updates .agents/AI_LOG.md from .agents/tmp/ or git history. |

## Processes

Standalone processes with clear output. Each process focuses on one development concern.

| Process | Description | Variables |
|---------|-------------|-----------|
| `1-design-to-code` | Converts .design/ files into pixel-perfect web pages. | `path`, `number` |
| `2-data-refactor` | Extracts hardcoded strings into i18n + repository layer. | `path`, `number` |
| `3-ui-refactor` | Decomposes pages into component hierarchy with theme tokens. | `path`, `number` |
| `4-create-code` | Implements code from a Kiro spec, verifies build, runs code review. | `specs-name`, `path`, `number` |
| `5-create-from-brief` | Generates spec from a brief file and implements it. | `brief`, `path`, `number` |
| `6-ui-tests` | Creates/maintains E2E tests and component stories. | `path`, `number`, `implement_features` |
| `7-create-tests` | Creates unit/integration tests for uncovered code. | `specs-name`, `path`, `number` |
| `8-code-review` | Reviews code against spec, fixes critical/high/medium issues. | `specs-name`, `path`, `number` |
| `9-improvements` | Finds and applies validation, DX, and test improvements. | `specs-name`, `path`, `number` |

## Workflows

Composed processes for full development cycles. Workflows chain multiple processes together and support skip flags to bypass individual steps.

| Workflow | Processes | Skip Flags |
|----------|-----------|------------|
| `design-implementation` | 1→2→3 | `skip-data-refactor`, `skip-ui-refactor` |
| `feature-implementation` | 4→8→7→6 | `skip-code-review`, `skip-create-tests`, `skip-ui-tests` |
| `bug-fixing` | 5→7→6 | `skip-create-tests`, `skip-ui-tests` |
| `tests-implementation` | 6→7 | `skip-ui-tests`, `skip-create-tests` |
| `refactoring` | 8→9 | `skip-improvements` |

## Lifecycle

The `lifecycle.yaml` wraps every pipeline run with synchronization and logging:

```
before: sync-pull
  ↓
[pipeline runs]
  ↓ (log:true steps trigger update-ai-log automatically)
  ↓
after: sync-push
```

- **before**: Pulls latest changes from the bare repo before the pipeline starts.
- **log-task**: Configured task that runs automatically after steps with `log: true`.
- **after**: Pushes results back to the bare repo.
- **retry**: Configures automatic retry on rate limit errors.

### log: true

Steps with `log: true` automatically trigger the configured `log-task` (default: `tasks/update-ai-log`) after successful completion. This ensures each process gets its own AI log entry in workflows.

```yaml
- name: "create-code"
  command: "trayline run processes/4-create-code ..."
  verbose: true
  log: true    # → runs tasks/update-ai-log after this step
```

### Retry on Rate Limit

When an agent hits a token/rate limit, the orchestrator detects it from the output, saves a checkpoint, waits the configured duration, and retries from where it left off.

```yaml
retry:
  on-rate-limit: true    # Enable automatic retry
  wait-minutes: 120      # Wait 2 hours between retries
  max-retries: 3         # Maximum retry attempts
```

Use `--no-lifecycle` flag to disable all lifecycle behavior (sync, logging, retry).

## Checkpoint & Resume

The orchestrator automatically saves progress after each completed step. If a pipeline fails or hits a rate limit, it can resume from where it left off.

**How it works:**
- After each successful top-level step, the step name is recorded in memory.
- On failure or rate limit, a checkpoint is saved to `.agents/tmp/.checkpoint`.
- On the next run of the same pipeline, completed steps are skipped automatically.
- On successful completion, the checkpoint is cleared.

**Flags:**
- `--restart` — Ignore any existing checkpoint and start from the beginning.

**Rate limit behavior:**
- Exit code 2 indicates a rate limit was hit.
- If retry is configured in lifecycle.yaml, the orchestrator waits and retries automatically.
- If retry is not configured, the pipeline exits and you can re-run it manually later.

## Usage Examples

```bash
# Run a single process:
trayline run processes/4-create-code --var specs-name=my-feature

# Run a full workflow:
trayline run workflows/feature-implementation --var specs-name=my-feature

# Skip specific steps in a workflow:
trayline run workflows/feature-implementation --var specs-name=my-feature --var skip-code-review=true

# Run a task without lifecycle (no sync):
trayline run tasks/check-build --no-lifecycle

# Force restart (ignore checkpoint):
trayline run workflows/feature-implementation --var specs-name=my-feature --restart

# Dry run (preview what would execute):
trayline run workflows/feature-implementation --var specs-name=my-feature --dry-run
```

## Common Patterns

- All processes use `.agents/MEMORY.md` for persistent knowledge.
- `.agents/tmp/` is used for temporary task files (cleaned up by update-ai-log).
- `.agents/AI_LOG.md` tracks all pipeline activity with project attribution.
- Git commits are made with author Martin Jablečník.
- The `number` variable controls tasks per loop iteration.
- The `path` variable points to the project directory.
- The `skip` field on steps accepts "true"/"false" for conditional execution.
- The `log` field on steps triggers automatic AI log updates after completion.
- The `model` field on agent steps overrides the default LLM model.
