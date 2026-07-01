# Memory

## pgregory.net/rapid v1.x uses generic Draw() API
- Project: server
- Problem: Tests written with old rapid API used type assertions like `.Draw(t, "name").(string)`, which fail to compile in rapid v1.2.0 because `Draw` now returns the generic type directly (not `interface{}`).
- Solution: Remove all type assertions after `.Draw(t, "name")` calls. The value is already the correct type.
- Source: create-code, 2026-07-01

## github.com/docker/docker module path with go 1.23
- Project: server
- Problem: `github.com/docker/docker/client` has been moved to `github.com/moby/moby/client`. The latest `github.com/docker/docker` module (v28.x) redirects to the new path and fails to resolve with `go get`. Also, `golang.org/x/time` v0.15.0 requires go >= 1.25.0.
- Solution: Use `github.com/docker/docker@v24.0.9+incompatible` (pre-moby-rename version) and `golang.org/x/time@v0.9.0` (compatible with go 1.23).
- Source: create-code, 2026-07-01

## rapid StringMatching("[^0-9]+") generates null bytes, breaking os.Setenv
- Project: server
- Problem: `rapid.StringMatching("[^0-9]+")` can produce strings with null bytes (e.g. `"\x00\x01+"`). `os.Setenv` on Linux returns an error for such strings but the test didn't check it, so the env var kept its previous valid value and `LoadConfig()` returned nil instead of an error, failing the property test.
- Solution: Guard the `os.Setenv` call with `if err := os.Setenv(...); err != nil { t.Skip(...) }`, matching the pattern already used in the `invalid MAX_CONCURRENT_TASKS` subtest.
- Source: check-build, 2026-07-01
