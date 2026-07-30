# Tasks: 001 — Dashboard API Projects Module

## Task 1: Add CORS middleware
- [ ] Create `remote/api/cors.go` with `CORSMiddleware` function
- [ ] Handle OPTIONS preflight → 204 with headers
- [ ] Add CORS headers to all responses (Access-Control-Allow-Origin, Methods, Headers, Max-Age)
- [ ] If `DASHBOARD_ORIGIN` is empty, middleware is a no-op
- [ ] Wire into middleware chain in `router.go` (before auth)

## Task 2: Add configuration for PROJECTS_DIR and DASHBOARD_ORIGIN
- [ ] Add `ProjectsDir` and `DashboardOrigin` fields to config struct in `core/config.go`
- [ ] Read from environment variables
- [ ] Validate `PROJECTS_DIR` exists and is a directory at startup
- [ ] Fail with clear error message if `PROJECTS_DIR` missing or invalid
- [ ] Update `.env.example` with both new variables

## Task 3: Create git package foundation
- [ ] Create `remote/git/git.go` with `Runner` struct (timeout field, default 5s)
- [ ] Implement `Run(repoPath string, args ...string) (string, error)` method
- [ ] Use `exec.CommandContext` with timeout, set working dir to repoPath
- [ ] Always prepend `--no-pager` to git args
- [ ] Return structured error with stderr content on non-zero exit

## Task 4: Implement git branch and repo operations
- [ ] `IsRepo(path string) bool` — check for `.git/` directory
- [ ] `CurrentBranch(repoPath string) (string, error)` — `git rev-parse --abbrev-ref HEAD`
- [ ] `Branches(repoPath string) ([]string, error)` — `git branch --format='%(refname:short)'`
- [ ] `RemoteURL(repoPath string) (string, error)` — `git remote get-url origin`
- [ ] `LastCommit(repoPath string) (*Commit, error)` — `git log -1 --format=...`
- [ ] `HasUncommittedChanges(repoPath string) (bool, error)` — `git status --porcelain`

## Task 5: Implement git tree and blob operations
- [ ] `Tree(repoPath, ref, path string) ([]TreeEntry, error)` — `git ls-tree <ref> <path>/`
- [ ] Parse ls-tree output: mode, type, hash, size, name
- [ ] Sort: directories first, then files, alphabetically
- [ ] `Blob(repoPath, ref, path string) ([]byte, error)` — `git show <ref>:<path>`
- [ ] Handle non-existent paths (return specific error type for 404)

## Task 6: Implement path security helpers
- [ ] `resolveProjectPath(name string) (string, error)` — validate name against directory listing
- [ ] `validateSubPath(projectPath, subPath string) (string, error)` — reject `..`, resolve symlinks, verify prefix
- [ ] Return specific error types for 400 (invalid path) vs 404 (not found)

## Task 7: Create project handler and response types
- [ ] Create `remote/api/project_handler.go` with `ProjectHandler` struct
- [ ] Create `remote/api/project_types.go` with response structs (ProjectSummary, ProjectDetail, TreeResponse, BlobResponse, Commit, TreeEntry)
- [ ] Implement binary detection (null byte in first 8KB)
- [ ] Implement language detection from file extension (map of ext → language name)

## Task 8: Implement GET /projects endpoint
- [ ] Scan PROJECTS_DIR for directories with `.git/`
- [ ] For each: get current branch, last commit, has_uncommitted_changes
- [ ] Sort by last commit date descending
- [ ] Return JSON array (empty array if none found)
- [ ] Handle git errors per-project gracefully (skip broken repos, log warning)

## Task 9: Implement GET /projects/{name} endpoint
- [ ] Validate project name via resolveProjectPath
- [ ] Fetch: name, current branch, all branches, remote URL, last commit
- [ ] Return 404 if project not found

## Task 10: Implement GET /projects/{name}/tree/{ref}/{path...} endpoint
- [ ] Validate project name and path
- [ ] Call git.Tree with ref and path
- [ ] Return TreeResponse with entries
- [ ] Handle empty path (root directory)
- [ ] Return 404 for non-existent path/ref

## Task 11: Implement GET /projects/{name}/blob/{ref}/{path...} endpoint
- [ ] Validate project name and path
- [ ] Call git.Blob to get file content
- [ ] Check size: > 1MB → truncated response
- [ ] Check binary → binary response
- [ ] Detect language from extension
- [ ] Return BlobResponse

## Task 12: Register routes and integration test
- [ ] Add all project routes to router.go
- [ ] Wire ProjectHandler with dependencies (projectsDir, git.Runner, logger)
- [ ] Verify all endpoints work with auth middleware
- [ ] Verify CORS headers present on responses
