# Tasks: 002 — Dashboard API Git Module

## Task 1: Implement git log with pagination
- [ ] Add `Log(repoPath, ref string, limit, offset int) (*LogResult, error)` to git package
- [ ] Use `git log --format=%H|%h|%s|%an|%aI --skip=<offset> -n <limit> <ref>`
- [ ] Parse output into `[]CommitEntry` structs
- [ ] Get total count via `git rev-list --count <ref>`
- [ ] Handle invalid ref gracefully (return specific error)

## Task 2: Implement git show (commit detail)
- [ ] Add `Show(repoPath, hash string) (*CommitDetail, error)` to git package
- [ ] Use `git show --stat --format=%H|%h|%s|%an|%aI <hash>` for metadata
- [ ] Use `git diff-tree -p <hash>` for unified diff
- [ ] Parse `--stat` summary line for files_changed, insertions, deletions
- [ ] Extended timeout (10s) for large diffs
- [ ] Return 404-type error for invalid hash

## Task 3: Implement git status (uncommitted changes)
- [ ] Add `Status(repoPath string) (*StatusResult, error)` to git package
- [ ] Use `git status --porcelain=v1` for file list
- [ ] Map status codes: M→modified, A/??→added/untracked, D→deleted
- [ ] Use `git diff --numstat` for per-file insertion/deletion counts
- [ ] Use `git diff -- <file>` for per-file diff content
- [ ] Truncate individual file diffs > 500 KB (replace with placeholder message)
- [ ] Build summary (total files, insertions, deletions)

## Task 4: Create git handler and response types
- [ ] Create `remote/api/git_handler.go` with `GitHandler` struct
- [ ] Create `remote/api/git_types.go` with response structs
- [ ] Implement query param parsing helper (ref, limit with bounds, offset)
- [ ] Wire GitHandler with git.Runner, projectsDir, logger

## Task 5: Implement GET /projects/{name}/commits endpoint
- [ ] Validate project name
- [ ] Parse query params: ref (default current branch), limit (1-100, default 50), offset (default 0)
- [ ] Call git.Log, compute has_more from total vs offset+limit
- [ ] Return CommitsResponse JSON

## Task 6: Implement GET /projects/{name}/commits/{hash} endpoint
- [ ] Validate project name and hash format (hexadecimal, 7-40 chars)
- [ ] Call git.Show
- [ ] Return CommitDetailResponse JSON
- [ ] Return 404 if hash not found

## Task 7: Implement GET /projects/{name}/status endpoint
- [ ] Validate project name
- [ ] Call git.Status
- [ ] Return StatusResponse JSON with per-file diffs and summary

## Task 8: Register routes
- [ ] Add commit and status routes to router.go
- [ ] Verify integration with auth + rate limiting
