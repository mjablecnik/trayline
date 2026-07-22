# Test Results

All tests pass.

## Summary

| Module           | Total | Passing | Failing |
|------------------|-------|---------|---------|
| taskline/cli     | 78    | 78      | 0       |
| taskline/server  | 110   | 110     | 0       |
| **Total**        | 188   | 188     | 0       |

Counts include subtests (`t.Run` cases) as reported by `go test -v`.

## Test Runner

Go modules detected via `go.mod`:
- `taskline/cli` — `go test ./...`
- `taskline/server` — `go test ./...`

## Results

- `taskline/cli`: `ok  cli  0.055s` — all 78 tests passed.
- `taskline/server`: `ok  server  1.537s` — all 110 tests passed.

## Failures

None.
