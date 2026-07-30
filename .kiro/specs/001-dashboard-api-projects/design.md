# Design: 001 — Dashboard API Projects Module

## Overview

Adds project browsing capabilities to the `remote/` Go server. Introduces CORS middleware, a `git/` package for command execution, and project handler endpoints.

## File Structure

```
remote/
├── api/
│   ├── cors.go                 # NEW — CORS middleware
│   ├── project_handler.go      # NEW — /projects endpoints
│   ├── project_types.go        # NEW — request/response types
│   ├── router.go               # MODIFIED — register new routes
│   └── ... (existing files unchanged)
├── core/
│   ├── config.go               # MODIFIED — add PROJECTS_DIR, DASHBOARD_ORIGIN
│   └── ...
├── git/
│   ├── git.go                  # NEW — git command runner with timeout
│   ├── branch.go               # NEW — branch operations
│   ├── tree.go                 # NEW — tree/blob operations
│   └── commit.go               # NEW — commit info (last commit, is-repo)
└── ...
```

## Component Design

### CORS Middleware (`api/cors.go`)

```go
func CORSMiddleware(allowedOrigin string) func(http.Handler) http.Handler
```

- Wraps the entire handler chain (applied before auth)
- Handles preflight OPTIONS → 204 with headers
- Adds CORS headers to all responses
- If `allowedOrigin` is empty, CORS middleware is a no-op (disabled)

### Git Package (`git/`)

Central abstraction for all git CLI interactions:

```go
// git/git.go
type Runner struct {
    Timeout time.Duration // default 5s
}

func (r *Runner) Run(repoPath string, args ...string) (string, error)
```

- Executes `git --no-pager <args>` in the given repo directory
- Returns stdout as string, or error with stderr content
- Uses `context.WithTimeout` for the configured timeout
- All other git functions use `Runner.Run` internally

```go
// git/branch.go
func (r *Runner) CurrentBranch(repoPath string) (string, error)
func (r *Runner) Branches(repoPath string) ([]string, error)
func (r *Runner) IsRepo(path string) bool

// git/tree.go
func (r *Runner) Tree(repoPath, ref, path string) ([]TreeEntry, error)
func (r *Runner) Blob(repoPath, ref, path string) ([]byte, error)

// git/commit.go
func (r *Runner) LastCommit(repoPath string) (*Commit, error)
func (r *Runner) HasUncommittedChanges(repoPath string) (bool, error)
```

### Project Handler (`api/project_handler.go`)

```go
type ProjectHandler struct {
    projectsDir string
    git         *git.Runner
    logger      *core.Logger
}

func (h *ProjectHandler) HandleListProjects(w http.ResponseWriter, r *http.Request)
func (h *ProjectHandler) HandleGetProject(w http.ResponseWriter, r *http.Request)
func (h *ProjectHandler) HandleGetTree(w http.ResponseWriter, r *http.Request)
func (h *ProjectHandler) HandleGetBlob(w http.ResponseWriter, r *http.Request)
```

### Path Security

Implemented as a helper in `project_handler.go`:

```go
func (h *ProjectHandler) resolveProjectPath(name string) (string, error)
func (h *ProjectHandler) validateSubPath(projectPath, subPath string) (string, error)
```

- `resolveProjectPath`: lists `PROJECTS_DIR`, checks name exists in listing, returns absolute path
- `validateSubPath`: rejects `..`, resolves symlinks, verifies prefix match

### Binary Detection

For blob endpoint, detect binary by checking first 8KB for null bytes:

```go
func isBinary(data []byte) bool {
    check := data
    if len(check) > 8192 {
        check = check[:8192]
    }
    return bytes.Contains(check, []byte{0})
}
```

### Language Detection

Map file extension to language name for syntax highlighting hint:

```go
var langMap = map[string]string{
    ".go": "go", ".ts": "typescript", ".js": "javascript",
    ".py": "python", ".rs": "rust", ".yaml": "yaml",
    ".yml": "yaml", ".json": "json", ".md": "markdown",
    ".sh": "bash", ".html": "html", ".css": "css",
    ".svelte": "svelte", ".sql": "sql", ".toml": "toml",
    // ... extend as needed
}
```

## Route Registration

In `router.go`, add after existing session routes:

```go
// Project endpoints (dashboard)
mux.HandleFunc("GET /projects", projectH.HandleListProjects)
mux.HandleFunc("GET /projects/{name}", projectH.HandleGetProject)
mux.HandleFunc("GET /projects/{name}/tree/{ref}/{path...}", projectH.HandleGetTree)
mux.HandleFunc("GET /projects/{name}/blob/{ref}/{path...}", projectH.HandleGetBlob)
```

Middleware chain becomes: recovery → CORS → rate limiter → auth → requestID → mux

## Config Changes

Add to `core/config.go`:

```go
type Config struct {
    // ... existing fields
    ProjectsDir     string // PROJECTS_DIR
    DashboardOrigin string // DASHBOARD_ORIGIN (optional, empty = CORS disabled)
}
```

## Response Types (`api/project_types.go`)

```go
type ProjectSummary struct {
    Name                  string  `json:"name"`
    Path                  string  `json:"path"`
    Branch                string  `json:"branch"`
    LastCommit            *Commit `json:"last_commit"`
    HasUncommittedChanges bool    `json:"has_uncommitted_changes"`
}

type ProjectDetail struct {
    Name      string   `json:"name"`
    Branch    string   `json:"branch"`
    Branches  []string `json:"branches"`
    RemoteURL string   `json:"remote_url"`
    LastCommit *Commit `json:"last_commit"`
}

type TreeResponse struct {
    Type    string      `json:"type"` // "directory"
    Path    string      `json:"path"`
    Entries []TreeEntry `json:"entries"`
}

type TreeEntry struct {
    Name string `json:"name"`
    Type string `json:"type"` // "file" or "directory"
    Size int64  `json:"size,omitempty"`
}

type BlobResponse struct {
    Type      string  `json:"type"` // "file"
    Path      string  `json:"path"`
    Size      int64   `json:"size"`
    Content   *string `json:"content"`   // null if binary/truncated
    Language  string  `json:"language"`
    Binary    bool    `json:"binary,omitempty"`
    Truncated bool    `json:"truncated,omitempty"`
}

type Commit struct {
    Hash    string `json:"hash"`
    Message string `json:"message"`
    Author  string `json:"author"`
    Date    string `json:"date"` // ISO 8601
}
```
