# Tasks: 001 — Dashboard API Projects Module

## Task 1: Add CORS middleware
- [x] Create `remote/api/cors.go` with `CORSMiddleware` function
- [x] Handle OPTIONS preflight → 204 with headers
- [x] Add CORS headers to all responses (Access-Control-Allow-Origin, Methods, Headers, Max-Age)
- [x] If `DASHBOARD_ORIGIN` is empty, middleware is a no-op
- [x] Wire into middleware chain in `router.go` (before auth)

## Task 2: Add configuration for PROJECTS_DIR and DASHBOARD_ORIGIN
- [x] Add `ProjectsDir` and `DashboardOrigin` fields to config struct in `core/config.go`
- [x] Read from environment variables
- [x] Validate `PROJECTS_DIR` exists and is a directory at startup
- [x] Fail with clear error message if `PROJECTS_DIR` missing or invalid
- [x] Update `.env.example` with both new variables

## Task 3: Create git package foundation
- [x] Create `remote/git/git.go` with `Runner` struct (timeout field, default 5s)
- [x] Implement `Run(repoPath string, args ...string) (string, error)` method
- [x] Use `exec.CommandContext` with timeout, set working dir to repoPath
- [x] Always prepend `--no-pager` to git args
- [x] Return structured error with stderr content on non-zero exit

## Task 4: Implement git branch and repo operations
- [x] `IsRepo(path string) bool` — check for `.git/` directory
- [x] `CurrentBranch(repoPath string) (string, error)` — `git rev-parse --abbrev-ref HEAD`
- [x] `Branches(repoPath string) ([]string, error)` — `git branch --format='%(refname:short)'`
- [x] `RemoteURL(repoPath string) (string, error)` — `git remote get-url origin`
- [x] `LastCommit(repoPath string) (*Commit, error)` — `git log -1 --format=...`
- [x] `HasUncommittedChanges(repoPath string) (bool, error)` — `git status --porcelain`

## Task 5: Implement git tree and blob operations
- [x] `Tree(repoPath, ref, path string) ([]TreeEntry, error)` — `git ls-tree <ref> <path>/`
- [x] Parse ls-tree output: mode, type, hash, size, name
- [x] Sort: directories first, then files, alphabetically
- [x] `Blob(repoPath, ref, path string) ([]byte, error)` — `git show <ref>:<path>`
- [x] Handle non-existent paths (return specific error type for 404)

## Task 6: Implement path security helpers
- [x] `resolveProjectPath(name string) (string, error)` — validate name against directory listing
- [x] `validateSubPath(projectPath, subPath string) (string, error)` — reject `..`, resolve symlinks, verify prefix
- [x] Return specific error types for 400 (invalid path) vs 404 (not found)

## Task 7: Create project handler and response types
- [x] Create `remote/api/project_handler.go` with `ProjectHandler` struct
- [x] Create `remote/api/project_types.go` with response structs (ProjectSummary, ProjectDetail, TreeResponse, BlobResponse, Commit, TreeEntry)
- [x] Implement binary detection (null byte in first 8KB)
- [x] Implement language detection from file extension (map of ext → language name)

## Task 8: Implement GET /projects endpoint
- [x] Scan PROJECTS_DIR for directories with `.git/`
- [x] For each: get current branch, last commit, has_uncommitted_changes
- [x] Sort by last commit date descending
- [x] Return JSON array (empty array if none found)
- [x] Handle git errors per-project gracefully (skip broken repos, log warning)

## Task 9: Implement GET /projects/{name} endpoint
- [x] Validate project name via resolveProjectPath
- [x] Fetch: name, current branch, all branches, remote URL, last commit
- [x] Return 404 if project not found

## Task 10: Implement GET /projects/{name}/tree/{ref}/{path...} endpoint
- [x] Validate project name and path
- [x] Call git.Tree with ref and path
- [x] Return TreeResponse with entries
- [x] Handle empty path (root directory)
- [x] Return 404 for non-existent path/ref

## Task 11: Implement GET /projects/{name}/blob/{ref}/{path...} endpoint
- [x] Validate project name and path
- [x] Call git.Blob to get file content
- [x] Check size: > 1MB → truncated response
- [x] Check binary → binary response
- [x] Detect language from extension
- [x] Return BlobResponse

## Task 12: Register routes and integration test
- [x] Add all project routes to router.go
- [x] Wire ProjectHandler with dependencies (projectsDir, git.Runner, logger)
- [x] Verify all endpoints work with auth middleware
- [x] Verify CORS headers present on responses
