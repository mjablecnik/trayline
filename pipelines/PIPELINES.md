# Pipelines

Overview of all available pipelines, what they do, and when to use them.

## 1-design-to-code

Converts visual designs into code. Three phases:
1. **Design analysis** — Kiro reads all files in the `.design/` folder, creates `PAGES.md` (detailed page descriptions with responsive breakpoints) and `TASKS.md` (one task per page)
2. **Page implementation** (loop, up to 50 iterations) — Claude implements pages with pixel-perfect accuracy, matching the designs exactly, including responsive layouts for mobile+desktop variants
3. **AI-LOG update** — Kiro logs what was done, cleans up temporary files

Handles responsive designs automatically — if mobile and desktop variants of the same page exist, they are implemented as a single responsive page with media queries.

**Variables:** `path` (default: `this`), `number` (default: `10`)

## 2-data-refactor

Extracts hardcoded strings into i18n and a repository data layer. Three phases:
1. **Analyze data** — Kiro identifies all hardcoded strings, classifies them (UI text vs API data), proposes i18n setup (en + cs) and repository layer, creates `DATA_SPEC.md` + `TASKS.md`
2. **Implement** (loop, up to 50 iterations) — Claude sets up i18n, creates repositories, extracts strings, adds language switcher
3. **AI-LOG update** — Kiro logs what was done, cleans up temporary files

Does not change visual appearance — only moves data out of components. Use when the project has hardcoded text that needs i18n or when mock data should be in a repository layer.

**Variables:** `path` (default: `this`), `number` (default: `10`)

## 3-ui-refactor

Decomposes monolithic UI pages into a clean component hierarchy with a centralized theme. Three phases:
1. **Analyze UI structure** — Kiro maps every page into sections → components, extracts design tokens (colors, fonts, spacing), proposes a folder structure with shared/page-specific layers, detects Storybook/Ladle/Histoire, creates `REFACTORING.md` + `TASKS.md`
2. **Refactor** (loop, up to 50 iterations) — Claude implements the refactoring in phases: theme tokens → shared elements → shared components → page-specific components → page composition → stories (if catalog tool detected)
3. **AI-LOG update** — Kiro logs what was done, cleans up temporary files

Does not change visual appearance — only restructures code. Use when pages are large monolithic files with duplicated UI code.

**Variables:** `path` (default: `this`), `number` (default: `10`)

## 4-create-code

Full development pipeline from a Kiro spec. Five phases:
1. **Code implementation** (loop, up to 50 iterations) — Claude implements tasks from `.kiro/specs/{specs-name}`, uses MEMORY.md for known issues
2. **Build verification** — Claude checks that the build and Docker build work
3. **Test creation** (loop, up to 50 iterations) — Claude creates all optional tests from the spec
4. **Code review** — Runs the `code-review` pipeline as a sub-pipeline
5. **AI-LOG update** — Kiro logs what was done, cleans up temporary files

Use for complete end-to-end development from a spec with quality assurance.

**Variables:** `specs-name`, `path` (default: `this`), `number` (default: `10`)

## 5-from-brief

Starts from a short project brief (a markdown file) instead of a full spec. Three phases:
1. **Spec creation** — Kiro reads the brief and generates `SPEC.md` + `TASKS.md`
2. **Implementation** (loop, up to 50 iterations) — Claude implements tasks from the generated spec, uses MEMORY.md for known issues
3. **AI-LOG update** — Kiro logs what was done, cleans up temporary files

Use when you have a rough idea written in a markdown file and want the agent to figure out the spec and build it.

**Variables:** `brief` (default: `BRIEF.md`), `path` (default: `this`), `number` (default: `10`)

## 6-ui-tests

Generates and maintains UI test coverage (E2E + component stories). Five phases:
1. **Analyze tests** — Kiro detects testing tools (Playwright/Cypress + Storybook/Ladle/Histoire), maps UI structure, identifies missing/orphaned/failing tests, creates `COMPONENT-TESTS.md`, `E2E-TESTS.md`, `COMPONENT-TASKS.md`, `E2E-TASKS.md`
2. **Generate component tests** (loop, up to 50 iterations) — Claude creates/fixes/removes stories
3. **Generate E2E tests** (loop, up to 50 iterations) — Claude creates/fixes/removes E2E tests with mocked APIs
4. **Summarize tests** — Kiro runs all tests, verifies coverage, creates `TEST-REPORT.md` inside the project
5. **AI-LOG update** — Kiro logs what was done, cleans up temporary files

Aborts early if neither an E2E framework nor a component catalog is installed.

**Variables:** `path` (default: `this`), `number` (default: `10`), `implement_features` (default: `false` — when `true`, implements missing features instead of deleting orphaned tests)

## check-build

Single-step pipeline. Claude detects the project type, runs the build, linter, and tests, and fixes everything needed to make the project build and run cleanly. Fixes all errors, fixes small warnings (1-2 line changes), updates build/run documentation if needed, and uses MEMORY.md to avoid repeating past mistakes.

Use as a quick health check to verify a project builds and runs without issues.

**Variables:** `path` (default: `this`)

## code-review

Reviews existing code against a spec and fixes issues. Runs up to 3 iterations of:
1. **Review** — Kiro analyzes the codebase, compares it to the spec, creates `CODE_REVIEW.md` + `TASKS.md` with issues sorted by severity
2. **Fix critical/high** — Claude fixes all CRITICAL and HIGH issues
3. **Fix medium** — Claude fixes all MEDIUM issues

Loop exits when no unchecked tasks remain. Uses MEMORY.md for context and records new insights.

**Variables:** `specs-name`, `path` (default: `this`), `number` (default: `10`)

---

## Common Patterns

- All pipelines use `MEMORY.md` inside the project to persist lessons learned across runs.
- All pipelines end with an `AI-LOG.md` update step that logs what was done and cleans up temporary files (TASKS.md, SPEC.md, etc.).
- Git commits are made with author `Martin Jablečník <martin.jablecnik@email.cz>`.
- The `number` variable controls how many tasks are completed per loop iteration.
- The `path` variable points to the project directory (defaults to `this` = current project).
