# Pipelines

Documentation for the trayline pipeline system — tasks, processes, and workflows that orchestrate AI agent actions.

## Structure Overview

```
pipelines/
├── lifecycle.yaml          ← Wraps every run: sync-pull → [run] → update-ai-log → sync-push
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

The `lifecycle.yaml` wraps every pipeline run with synchronization steps:

```
before: sync-pull
  ↓
[pipeline runs]
  ↓
after: update-ai-log → sync-push
```

- **before**: Pulls latest changes from the bare repo before the pipeline starts.
- **after**: Updates the AI log and pushes results back to the bare repo.

Use `--no-lifecycle` flag to disable lifecycle wrapping (useful for local-only tasks or debugging).

## Usage Examples

```bash
trayline run processes/4-create-code --var specs-name=my-feature
trayline run workflows/feature-implementation --var specs-name=my-feature
trayline run workflows/feature-implementation --var specs-name=my-feature --var skip-code-review=true
trayline run tasks/check-build --no-lifecycle
trayline run tasks/update-ai-log
```

## Common Patterns

- All processes use `.agents/MEMORY.md` for persistent knowledge.
- `.agents/tmp/` is used for temporary task files (cleaned up by update-ai-log).
- `.agents/AI_LOG.md` tracks all pipeline activity.
- Git commits are made with author Martin Jablečník.
- The `number` variable controls tasks per loop iteration.
- The `path` variable points to the project directory.
- The `skip` field on steps accepts "true"/"false" for conditional execution.
