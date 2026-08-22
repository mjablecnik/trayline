# Phase 5 — Docs

**Target:** whole `trayline` repository (root). No `.agents/PIPELINE_STATE.json` existed
at the start of this run — this was not a formal spec-pipeline run (no `impl`/`build`/
`cases`/`verify` phases preceded it in this repo's `.agents/`), but a direct, explicit
documentation-review request following an earlier ad-hoc repo-hygiene cleanup session
(4 local commits on top of `ee6c401`, not pushed). Proceeded on the strength of that
explicit brief rather than blocking on a missing predecessor; a `PIPELINE_STATE.json`
is created below for record-keeping, with all phases before `docs` marked `done` at the
pre-existing `HEAD` since no tracked spec/run governs this work.

## Result
OK WITH ISSUES

## Headline
Verified every documented build/test/lint command across all seven service directories
(orchestrator, remote, tools/taskline/server, tools/taskline/cli, tools/tunnel, dashboard,
setup) by actually running them or reading the scripts in full. Fixed real drift in six
files: a completely undocumented `trayline schedule` command and `claude remote` mode in
the root README, a wrong default model and four undocumented condition/step features in
orchestrator's pipeline YAML docs, two undocumented env vars and ~38 undocumented API
routes in remote/, three inaccurate claims in tools/tunnel/README.md (a documented env var
that doesn't exist, an unsupported "skip" claim, and a health-check script that isn't
wired up), and three inaccurate installer claims in root README.md/CLAUDE.md (`--force`
flag that doesn't exist, "remove everything" that leaves Docker images behind, wrong
install-location claim). Confirmed the repo-hygiene cleanup from the prior session
(removed BRIEF.md, test-pipeline.yaml, orphaned checkpoint, checkpoint-migration shim)
left no stale doc references anywhere. `tools/taskline/` and `dashboard/` docs were
already fully accurate — no changes needed there.

## Needs attention
- Two code-level findings were logged to `.agents/ISSUES.md` (LOW severity, out of this
  phase's scope to fix): the trayline version string is hardcoded in two places
  (`orchestrator/cmd/main.go` and `runtime/trayline`) that must be kept in sync by hand;
  and `tools/tunnel/relay/health.sh` is built into the relay image but never invoked by
  anything (`entrypoint.sh` doesn't call it, `fly.toml`'s health check is a plain TCP
  probe) — dead code that looks load-bearing.
- No `.agents/PIPELINE_STATE.json` existed before this run. If this repo is meant to use
  the formal dev-pipeline going forward, someone should decide whether to backfill state
  for the untracked work already in `.agents/AI_LOG.md`/`MEMORY.md` or start clean.
- Root `README.md`/CLAUDE.md/per-service READMEs do not carry the standard's Author /
  Show your support / License footer sections, even though every other reviewed project
  in this user's workspace does (confirmed by reading several). This repo's own
  `pipelines/tasks/sync-docs.yaml` prompt actually mandates those sections. Left
  unchanged this run: forcing them onto five already-mature, differently-styled
  technical READMEs (root + 4 per-service) felt like a bigger call than a docs-accuracy
  sweep should make unilaterally, especially since there's no LICENSE file at the root
  to point to and this is an internal dev tool, not a distributed product. Flagging for
  an explicit decision rather than guessing.
- `dashboard/` has no dedicated README.md/DOCS.md of its own (root README's "Dashboard"
  section covers it, and it checked out accurate) — inconsistent with orchestrator/,
  remote/, taskline/, tunnel/ each having one, but not fixed this run since it wasn't
  flagged as inaccurate, just less thorough.

## What changed
- `README.md` — added the `schedule` command (row in Commands table + full `### schedule`
  section) and `claude remote` remote-control mode (options + example + clarifying note
  that it's unrelated to `remote/`) to the `agent` section; fixed the install-locations
  description (was: everything to `~/.trayline/`; actually: split across `~/bin/`,
  `~/.trayline/`, and `~/.local/bin/`) and the Docker image count (two images, not one).
- `CLAUDE.md` — fixed the `setup/*.sh` one-liners: `reinstall.sh` doesn't pass a
  `--force` flag (it doesn't exist), `uninstall.sh` does not remove Docker images; added
  an install-layout note under "Other Key Paths" for the same `~/bin` / `~/.trayline` /
  `~/.local/bin` split.
- `orchestrator/README.md` — fixed `OPENROUTER_MODEL`'s documented default
  (`openai/gpt-4.1-nano` → the actual compiled-in `openai/gpt-4.1-mini`); documented the
  `~/.trayline/env/orchestrator.env` fallback when no local `.env` exists; added a
  "Common Step Fields" section for the previously-undocumented `skip`/`log` step fields;
  added a "Condition Modes" section for the previously-undocumented `contains`/
  `not_contains`/`matches`/`not_matches` condition modes (docs only showed `prompt`);
  corrected the Validation Rules list (condition validation, loop condition requirement
  nuance, and removed the stale "conditions inside loop steps not supported" line — they
  are supported; only `goto` inside a loop step's condition is rejected).
- `remote/README.md` — added `REPOS_DIR` and `ASSISTANT_DATA_DIR` to the environment
  variables table (both real, read by `core/config.go`, previously undocumented).
- `remote/API.md` — added a new "Dashboard-Internal Endpoints" section listing all ~38
  routes used by `dashboard/` (projects, git, env, project-agent chat, pipelines, specs,
  workflows, assistant) that were registered in `api/router.go` but entirely absent from
  the API reference; scoped explicitly as a reference table (method + path + purpose)
  rather than full request/response documentation, since writing that at the same depth
  as the existing public-API sections was out of proportion for this pass.
- `tools/tunnel/README.md` — rewrote the "Check Health" section (previously claimed
  `health.sh` backs `fly.toml`'s health check; it doesn't — see Needs attention);
  corrected the deploy script's behavior (removed a documented `DPLOY_ENV_FILE`
  environment variable that doesn't exist anywhere in the code; replaced the false
  "skips keys already in fly.toml [env]" claim with the actual centralized-env-file
  resolution order, which was previously undocumented); replaced a troubleshooting entry
  that described symptoms of a health check that isn't actually reachable.
- `.agents/ISSUES.md` — created, with the two LOW findings above.
- `.agents/PIPELINE_STATE.json` — created (see note in Target above).

## Decisions made
- Treated the missing `PIPELINE_STATE.json` as a documentation-run-without-a-pipeline
  situation rather than a blocking missing-predecessor condition, given the explicit,
  detailed task brief this run was launched with. Recorded the reasoning here and in the
  state file rather than silently proceeding or silently blocking.
- Did not restructure README.md's "Project Structure" / "Default Pipelines" content into
  a new root `DOCS.md`, even though the project's complexity would technically warrant
  one per the standard. The existing split (README for usage, CLAUDE.md for agent-facing
  structure/paths/conventions) is coherent and was explicitly called out by the launching
  task to preserve; fragmenting it further for template-purity risked degrading a
  currently-working document for unclear benefit. Flagged the Author/License footer
  question in "Needs attention" instead of guessing at a license for a private tool.
- Reverted an incidental `dashboard/package-lock.json` change produced by a verification
  subagent's `npm install` (removed a stale optional `yaml` entry) — out of scope for a
  docs-only phase and not part of the deliverable.
- Scoped the new API.md "Dashboard-Internal Endpoints" section to a method+path+purpose
  table rather than full per-endpoint documentation, to keep the addition proportionate;
  flagged this choice above rather than silently under-documenting.
