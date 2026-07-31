# Tasks: 002 — Dashboard API Git Module

## Task 1: Implement git log with pagination
- [x] Add `Log(repoPath, ref string, limit, offset int) (*LogResult, error)` to git package
- [x] Use `git log --format=%H|%h|%s|%an|%aI --skip=<offset> -n <limit> <ref>`
- [x] Parse output into `[]CommitEntry` structs
- [x] Get total count via `git rev-list --count <ref>`
- [x] Handle invalid ref gracefully (return specific error)

## Task 2: Implement git show (commit detail)
- [x] Add `Show(repoPath, hash string) (*CommitDetail, error)` to git package
- [x] Use `git show --stat --format=%H|%h|%s|%an|%aI <hash>` for metadata
- [x] Use `git diff-tree -p <hash>` for unified diff
- [x] Parse `--stat` summary line for files_changed, insertions, deletions
- [x] Extended timeout (10s) for large diffs
- [x] Return 404-type error for invalid hash

## Task 3: Implement git status (uncommitted changes)
- [x] Add `Status(repoPath string) (*StatusResult, error)` to git package
- [x] Use `git status --porcelain=v1` for file list
- [x] Map status codes: M→modified, A/??→added/untracked, D→deleted
- [x] Use `git diff --numstat` for per-file insertion/deletion counts
- [x] Use `git diff -- <file>` for per-file diff content
- [x] Truncate individual file diffs > 500 KB (replace with placeholder message)
- [x] Build summary (total files, insertions, deletions)

## Task 4: Create git handler and response types
- [x] Create `remote/api/git_handler.go` with `GitHandler` struct
- [x] Create `remote/api/git_types.go` with response structs
- [x] Implement query param parsing helper (ref, limit with bounds, offset)
- [x] Wire GitHandler with git.Runner, projectsDir, logger

## Task 5: Implement GET /projects/{name}/commits endpoint
- [x] Validate project name
- [x] Parse query params: ref (default current branch), limit (1-100, default 50), offset (default 0)
- [x] Call git.Log, compute has_more from total vs offset+limit
- [x] Return CommitsResponse JSON

## Task 6: Implement GET /projects/{name}/commits/{hash} endpoint
- [x] Validate project name and hash format (hexadecimal, 7-40 chars)
- [x] Call git.Show
- [x] Return CommitDetailResponse JSON
- [x] Return 404 if hash not found

## Task 7: Implement GET /projects/{name}/status endpoint
- [x] Validate project name
- [x] Call git.Status
- [x] Return StatusResponse JSON with per-file diffs and summary

## Task 8: Register routes
- [x] Add commit and status routes to router.go
- [x] Verify integration with auth + rate limiting
