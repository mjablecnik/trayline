# Pipelines

Overview of all available pipelines, what they do, and when to use them.

## quick

The simplest pipeline. Claude reads a spec from `.kiro/specs/` and implements tasks in a loop, up to 10 per iteration. No build verification, no tests, no code review. Use when you just want code written fast from an existing spec.

**Variables:** `specs-name`, `path`, `number`

## default

The full development pipeline. Four phases:
1. **Code implementation** — Claude implements tasks from the spec in a loop
2. **Build verification** — Claude checks that the build and Docker build work
3. **Test creation** — Claude creates all optional tests from the spec in a loop
4. **Code review** — Kiro reviews the code against the spec, then Claude fixes found issues

Use for complete end-to-end development from a spec with quality assurance.

**Variables:** `specs-name`, `path`, `number`

## create-code

Similar to `default` but with higher iteration limits (50 instead of 20) and runs the `code-review` pipeline as a sub-pipeline at the end. Four phases: code implementation → build verification → test creation → code review.

Use for larger projects where you need more iterations to complete all tasks.

**Variables:** `specs-name`, `path`, `number`

## from-brief

Starts from a short project brief (a markdown file) instead of a full spec. Two phases:
1. **Spec creation** — Kiro reads the brief and generates `SPEC.md` + `TASKS.md`
2. **Implementation** — Claude implements tasks from the generated spec in a loop

Use when you have a rough idea written in a markdown file and want the agent to figure out the spec and build it.

**Variables:** `brief` (default: `BRIEF.md`), `path`, `number`

## design-to-code

Converts visual designs into code. Two phases:
1. **Design analysis** — Kiro reads all files in the `.design/` folder, creates `PAGES.md` (detailed page descriptions) and `TASKS.md` (one task per page)
2. **Page implementation** — Claude implements pages with pixel-perfect accuracy, matching the designs exactly

Use when you have design files (screenshots, mockups) in a `.design/` folder and want static UI pages created from them.

**Variables:** `path`, `number`

## code-review

Reviews existing code against a spec and fixes issues. Runs up to 5 iterations of:
1. **Review** — Kiro analyzes the codebase, compares it to the spec, creates `CODE_REVIEW.md` + `TASKS.md` with issues sorted by severity
2. **Fix critical/high** — Claude fixes all CRITICAL and HIGH issues
3. **Fix medium** — Claude fixes all MEDIUM issues

Loop exits when no unchecked tasks remain. Use after implementation to catch bugs, missing features, and spec deviations.

**Variables:** `specs-name`, `path`, `number`

## improvements

Finds and applies code improvements. Runs up to 5 iterations of:
1. **Find improvements** — Kiro analyzes the codebase focusing on validation, developer experience, and test quality
2. **Apply critical/high** — Claude applies CRITICAL and HIGH improvements
3. **Apply medium** — Claude applies MEDIUM improvements

Does not add new features — only improves existing code. Use to harden and polish a project after initial development.

**Variables:** `specs-name`, `path`, `number`

## test-generation

Generates missing tests. Runs up to 3 outer iterations of:
1. **Analyze & create test spec** — Claude analyzes the codebase, identifies missing tests, creates `TEST_SPEC.md` + `TASKS.md`
2. **Implement tests** (inner loop, up to 30 iterations) — Claude implements 10 tests per iteration until all are done

Each outer iteration re-analyzes the codebase to find tests missed in previous rounds. Use to add comprehensive test coverage to an existing project.

**Variables:** `specs-name`, `path`, `number`

## check-build

Single-step pipeline. Claude detects the project type, runs the build, linter, and tests, and fixes everything needed to make the project build and run cleanly. Fixes all errors, fixes small warnings (1-2 line changes), and updates build/run documentation if needed.

Use as a quick health check to verify a project builds and runs without issues.

**Variables:** `path`

## ui-refactor

Decomposes monolithic UI pages into a clean component hierarchy. Two phases:
1. **Analyze UI structure** — Kiro maps every page into sections → components, extracts design tokens (colors, fonts, spacing), proposes a folder structure with shared/page-specific layers, creates `REFACTORING.md` + `TASKS.md`
2. **Refactor** — Claude implements the refactoring in phases: theme tokens → shared elements → shared components → page-specific components → page composition

Does not change visual appearance — only restructures code. Use when pages are large monolithic files with duplicated UI code.

**Variables:** `path`, `number`

## data-refactor

Extracts hardcoded strings into i18n and a repository data layer. Two phases:
1. **Analyze data** — Kiro identifies all hardcoded strings, classifies them (UI text vs API data), proposes i18n setup (en + cs) and repository layer, creates `DATA_SPEC.md` + `TASKS.md`
2. **Implement** — Claude sets up i18n, creates repositories, extracts strings, adds language switcher

Does not change visual appearance — only moves data out of components. Use when the project has hardcoded text that needs i18n or when mock data should be in a repository layer.

**Variables:** `path`, `number`
