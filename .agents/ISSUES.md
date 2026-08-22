# Issues

## [LOW] Version string duplicated in two hardcoded places
- Run: 2026-08-22-docs-adhoc
- Phase: docs
- Status: OPEN
- What: `orchestrator/cmd/main.go:28` defines `var version = "2.4.0"`, and separately `runtime/trayline:259` hardcodes `echo "trayline wrapper v2.4.0"`. Per `code-versioning.md`, a project's version must have exactly one source of truth; here there are two literals that must be updated together by hand.
- Impact: Low risk today (both currently read "2.4.0"), but the next version bump only has to update one of the two literals for them to silently diverge — `trayline version` would then print two different version numbers.
- Tried: N/A — this is a code fix, out of scope for the docs phase.
- Next: Have `runtime/trayline`'s `version` case read the version from `trayline-run --version` only (drop the separately hardcoded "wrapper v2.4.0" string), or otherwise establish one literal as the single source.

## [LOW] tools/tunnel/relay/health.sh is built into the image but never invoked
- Run: 2026-08-22-docs-adhoc
- Phase: docs
- Status: OPEN
- What: `relay/Dockerfile` copies `health.sh` into the image and `chmod +x`'s it, but `relay/entrypoint.sh` never calls it, and `relay/fly.toml`'s `[checks.health]` is a plain TCP check against port 8080 (the port chisel binds directly) rather than an HTTP call to this script. Found while verifying `tools/tunnel/README.md`'s "Check Health" section, which previously (incorrectly) documented `health.sh` as the mechanism behind `fly.toml`'s health check — the doc has been corrected to describe this as currently unused.
- Impact: No functional impact today (the TCP check works fine on its own), but the script is dead code that looks load-bearing, and a future change to `fly.toml`'s health check assuming `health.sh` is wired up would be surprised to find it isn't.
- Tried: N/A — this is a code fix (wire it in or remove it), out of scope for the docs phase.
- Next: Either wire `health.sh` into `entrypoint.sh` (e.g. run it as a tiny HTTP listener alongside chisel) and switch `fly.toml`'s check to HTTP, or delete the script if the TCP check is considered sufficient.
