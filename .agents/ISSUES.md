# Issues

## [LOW] Version string duplicated in two hardcoded places
- Run: 2026-08-22-docs-adhoc
- Phase: docs
- Status: FIXED
- What: `orchestrator/cmd/main.go:28` defines `var version = "2.4.0"`, and separately `runtime/trayline:259` hardcoded `echo "trayline wrapper v2.4.0"`. Per `code-versioning.md`, a project's version must have exactly one source of truth; here there were two literals that had to be updated together by hand.
- Impact: Low risk today (both currently read "2.4.0"), but the next version bump only had to update one of the two literals for them to silently diverge — `trayline version` would then print two different version numbers.
- Fix: `runtime/trayline`'s `version` case no longer prints its own hardcoded string — it now only delegates to `trayline-run --version`, which reads the single `version` literal in `orchestrator/cmd/main.go`.

## [LOW] tools/tunnel/relay/health.sh is built into the image but never invoked
- Run: 2026-08-22-docs-adhoc
- Phase: docs
- Status: FIXED
- What: `relay/Dockerfile` copied `health.sh` into the image and `chmod +x`'d it, but `relay/entrypoint.sh` never called it, and `relay/fly.toml`'s `[checks.health]` is a plain TCP check against port 8080 (the port chisel binds directly) rather than an HTTP call to this script. Found while verifying `tools/tunnel/README.md`'s "Check Health" section, which previously (incorrectly) documented `health.sh` as the mechanism behind `fly.toml`'s health check.
- Impact: No functional impact (the TCP check works fine on its own), but the script was dead code that looked load-bearing.
- Fix: Deleted `relay/health.sh` and its `COPY`/`chmod` lines in `relay/Dockerfile` (the TCP check was decided sufficient, per user). Updated `tools/tunnel/README.md`'s directory listing and "Check Health" section to describe the actual TCP-only mechanism.
