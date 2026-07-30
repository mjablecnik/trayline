# Design: 002 — Dashboard API Git Module

## Overview

Extends the `git/` package and adds handler endpoints for commit history, commit detail, and uncommitted changes.

## File Structure

```
remote/
├── api/
│   ├── git_handler.go          # NEW — /commits, /status endpoints
│   ├── git_types.go            # NEW — request/response types
│   └── router.go               # MODIFIED — register new routes
├── git/
│   ├── log.go                  # NEW — commit log with pagination
│   ├── show.go                 # NEW — single commit detail with diff
│   └── status.go               # NEW — working tree status with per-file diffs
└── ...
```

## Component Design

### Git Package Extensions

```go
// git/log.go
type LogResult struct {
    Commits []CommitEntry
    Total   int
}

type CommitEntry struct {
    Hash      string
    ShortHash string
    Message   string
    Author    string
    Date      time.Time
}

func (r *Runner) Log(repoPath, ref string, limit, offset int) (*LogResult, error)
```

Implementation:
- Uses `git log --format=<custom> --skip=<offset> -n <limit> <ref>`
- Format: `%H|%h|%s|%an|%aI` (hash, short hash, subject, author name, ISO date)
- Total count via separate `git rev-list --count <ref>`

```go
// git/show.go
type CommitDetail struct {
    Hash         string
    ShortHash    string
    Message      string
    Author       string
    Date         time.Time
    FilesChanged int
    Insertions   int
    Deletions    int
    Diff         string
}

func (r *Runner) Show(repoPath, hash string) (*CommitDetail, error)
```

Implementation:
- `git show --stat --format=<custom> <hash>` for metadata + stats
- `git diff-tree -p <hash>` for the full unified diff
- Parse `--stat` output for insertions/deletions counts
- Timeout extended to 10s for show (diffs can be large)

```go
// git/status.go
type StatusResult struct {
    Clean   bool
    Files   []FileChange
    Summary StatusSummary
}

type FileChange struct {
    Path       string
    Status     string // "modified", "added", "deleted", "untracked"
    Insertions int
    Deletions  int
    Diff       *string // nil for untracked
}

type StatusSummary struct {
    FilesChanged int
    Insertions   int
    Deletions    int
}

func (r *Runner) Status(repoPath string) (*StatusResult, error)
```

Implementation:
- `git status --porcelain=v1` for file list and statuses
- `git diff` for modified/deleted file diffs (working tree vs HEAD)
- `git diff --numstat` for per-file insertion/deletion counts
- Map porcelain status codes: `M` → modified, `A`/`??` → added/untracked, `D` → deleted
- Per-file diff truncation: if a single file diff > 500 KB, replace with `"(diff too large)"`

### Git Handler (`api/git_handler.go`)

```go
type GitHandler struct {
    projectsDir string
    git         *git.Runner
    logger      *core.Logger
}

func (h *GitHandler) HandleGetCommits(w http.ResponseWriter, r *http.Request)
func (h *GitHandler) HandleGetCommitDetail(w http.ResponseWriter, r *http.Request)
func (h *GitHandler) HandleGetStatus(w http.ResponseWriter, r *http.Request)
```

### Query Parameter Parsing (commits endpoint)

```go
ref := r.URL.Query().Get("ref")     // default: current branch
limit := parseIntParam(r, "limit", 50, 1, 100)
offset := parseIntParam(r, "offset", 0, 0, math.MaxInt)
```

## Route Registration

```go
mux.HandleFunc("GET /projects/{name}/commits", gitH.HandleGetCommits)
mux.HandleFunc("GET /projects/{name}/commits/{hash}", gitH.HandleGetCommitDetail)
mux.HandleFunc("GET /projects/{name}/status", gitH.HandleGetStatus)
```

## Response Types (`api/git_types.go`)

```go
type CommitsResponse struct {
    Commits []CommitSummary `json:"commits"`
    Total   int            `json:"total"`
    HasMore bool           `json:"has_more"`
}

type CommitSummary struct {
    Hash      string `json:"hash"`
    ShortHash string `json:"short_hash"`
    Message   string `json:"message"`
    Author    string `json:"author"`
    Date      string `json:"date"`
}

type CommitDetailResponse struct {
    Hash         string `json:"hash"`
    ShortHash    string `json:"short_hash"`
    Message      string `json:"message"`
    Author       string `json:"author"`
    Date         string `json:"date"`
    FilesChanged int    `json:"files_changed"`
    Insertions   int    `json:"insertions"`
    Deletions    int    `json:"deletions"`
    Diff         string `json:"diff"`
}

type StatusResponse struct {
    Clean   bool            `json:"clean"`
    Files   []FileStatus    `json:"files"`
    Summary StatusSummary   `json:"summary"`
}

type FileStatus struct {
    Path       string  `json:"path"`
    Status     string  `json:"status"`
    Insertions int     `json:"insertions"`
    Deletions  int     `json:"deletions"`
    Diff       *string `json:"diff"`
}

type StatusSummary struct {
    FilesChanged int `json:"files_changed"`
    Insertions   int `json:"insertions"`
    Deletions    int `json:"deletions"`
}
```

## Error Handling

- Invalid ref (branch/tag not found): 400 with `VALIDATION_ERROR`
- Invalid commit hash: 404 with `NOT_FOUND`
- Git command timeout: 500 with `INTERNAL_ERROR` + log warning
- Project not found: 404 (reuses validation from spec 001)
