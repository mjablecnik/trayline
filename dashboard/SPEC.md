# Trayline Dashboard — Master Specification

## Overview

A web-based dashboard for browsing and managing projects synced to the trayline remote server. Provides read-only file browsing with syntax highlighting, git history visualization, uncommitted change tracking, and .env variable editing.

The dashboard consists of two parts:
- **Backend** — new "projects" module added to the existing `remote/` Go server
- **Frontend** — SvelteKit SPA deployed as a static site

## Architecture

### Backend (Go — inside `remote/`)

The existing `remote/` server gains a new logical module alongside the agent module:

```
remote/
├── api/
│   ├── agent/          # existing: /run, /chat, /sessions (renamed from current flat structure)
│   ├── projects/       # NEW: /projects, /projects/{name}/...
│   ├── router.go       # registers both modules
│   ├── auth.go         # shared auth middleware
│   ├── ratelimit.go    # shared rate limiter
│   └── response.go     # shared response helpers
├── core/               # config, logger, types
├── docker/             # agent container management
├── git/                # NEW: git operations (log, diff, status, show, tree)
├── store/              # sessions, tasks (existing)
└── cmd/server/main.go  # single entry point
```

### Frontend (SvelteKit — `dashboard/`)

```
dashboard/
├── src/
│   ├── lib/            # API client, stores, utilities
│   ├── routes/         # SvelteKit pages
│   └── app.html
├── static/
├── scripts/            # build.sh, start-docker.sh, stop-docker.sh, deploy.sh
├── Dockerfile
├── fly.toml
├── package.json
├── svelte.config.js
└── vite.config.ts
```

Deployed as static SPA (SvelteKit `adapter-static`). Communicates with `remote/` server via REST API.

---

## Authentication

Bearer token in `Authorization` header — same mechanism as the existing agent API. The frontend stores the token in localStorage after the user enters it on first visit (simple token input screen, not a login form).

No user accounts, no sessions, no cookies. Single shared token for all dashboard access.

---

## CORS

The `remote/` server must handle CORS for dashboard requests. CORS middleware is applied **before** auth (browsers send preflight OPTIONS without auth headers).

- Allowed origin: configured via `DASHBOARD_ORIGIN` env var
- Allowed methods: `GET, PUT, OPTIONS`
- Allowed headers: `Authorization, Content-Type`
- Max preflight cache: 3600s

In development, `DASHBOARD_ORIGIN` can be set to `http://localhost:5173` (SvelteKit dev server).

---

## Functional Requirements

### FR-1: Project List

**Endpoint:** `GET /projects`

Scans the configured `PROJECTS_DIR` directory on the server. Each subdirectory containing a `.git/` folder is considered a project.

**Response:**
```json
[
  {
    "name": "my-app",
    "path": "my-app",
    "last_commit": {
      "hash": "abc1234",
      "message": "fix: resolve login bug",
      "author": "Martin",
      "date": "2026-07-28T14:30:00Z"
    },
    "branch": "main",
    "has_uncommitted_changes": true
  }
]
```

**Sorting:** Projects are sorted by last commit date (newest first).

**UI:** Card grid or list showing project name, current branch, last commit message, and an indicator if there are uncommitted changes.

---

### FR-2: Project Detail

**Endpoint:** `GET /projects/{name}`

Returns metadata about a single project: branches, current branch, remote URL, file count.

**Response:**
```json
{
  "name": "my-app",
  "branch": "main",
  "branches": ["main", "feature/auth", "fix/typo"],
  "remote_url": "git@server:/repos/my-app.git",
  "last_commit": {
    "hash": "abc1234",
    "message": "fix: resolve login bug",
    "author": "Martin",
    "date": "2026-07-28T14:30:00Z"
  }
}
```

**UI:** Project header with branch selector dropdown, then tabbed content below (Files, Commits, Changes, Environment).

---

### FR-3: File Browser (Read-Only)

**Endpoint:** `GET /projects/{name}/tree/{ref}/{path...}`

Lists directory contents at a given path and git ref (branch name or commit hash). Returns files and subdirectories with metadata.

**Important:** The `{ref}` parameter refers to a git ref (branch name, tag, or commit hash). The tree and blob endpoints show **committed state only** — they read from the git object store, not the working tree. Untracked or modified-but-uncommitted files are NOT visible here; those appear exclusively in the Changes tab (`GET /status`).

**Response (directory):**
```json
{
  "type": "directory",
  "path": "src",
  "entries": [
    {"name": "main.go", "type": "file", "size": 2048},
    {"name": "handlers/", "type": "directory"},
    {"name": "README.md", "type": "file", "size": 512}
  ]
}
```

**Endpoint:** `GET /projects/{name}/blob/{ref}/{path...}`

Returns file content for a single file.

**Response (file):**
```json
{
  "type": "file",
  "path": "src/main.go",
  "size": 2048,
  "content": "package main\n\nimport ...",
  "language": "go"
}
```

**Size limits:**
- Files ≤ 1 MB: content returned inline
- Files > 1 MB: `"content": null` with `"truncated": true` — frontend shows a message and offers a "Download raw" link
- Binary files (detected by content): `"content": null` with `"binary": true`

**Directory entry limits:**
- Directories with > 500 entries are returned in full (no pagination) — the frontend handles rendering with virtual scrolling if needed. This keeps the API simple since most project directories are well under this limit.

**UI:**
- Directory listing with icons (folder/file), click to navigate
- Breadcrumb path navigation
- File view with syntax highlighting (using Shiki or Prism)
- Line numbers
- "Raw" button to view/download raw content

---

### FR-4: Git Commit History

**Endpoint:** `GET /projects/{name}/commits?ref={branch}&limit=50&offset=0`

Returns commit log for the given ref.

**Response:**
```json
{
  "commits": [
    {
      "hash": "abc1234",
      "short_hash": "abc1234",
      "message": "fix: resolve login bug",
      "author": "Martin",
      "date": "2026-07-28T14:30:00Z"
    }
  ],
  "total": 142,
  "has_more": true
}
```

**Endpoint:** `GET /projects/{name}/commits/{hash}`

Returns a single commit with its diff.

**Response:**
```json
{
  "hash": "abc1234def5678...",
  "short_hash": "abc1234",
  "message": "fix: resolve login bug",
  "author": "Martin",
  "date": "2026-07-28T14:30:00Z",
  "files_changed": 3,
  "insertions": 12,
  "deletions": 5,
  "diff": "diff --git a/src/main.go b/src/main.go\n..."
}
```

**UI:**
- Commit list with hash, message, author, relative date
- Click commit → shows diff with syntax highlighting (added lines green, removed red)
- Pagination (load more)

---

### FR-5: Uncommitted Changes

**Endpoint:** `GET /projects/{name}/status`

Returns uncommitted changes (working tree + staged).

**Response:**
```json
{
  "clean": false,
  "files": [
    {
      "path": "src/main.go",
      "status": "modified",
      "insertions": 8,
      "deletions": 3,
      "diff": "diff --git a/src/main.go b/src/main.go\n..."
    },
    {
      "path": "src/new_file.go",
      "status": "untracked",
      "insertions": 25,
      "deletions": 0,
      "diff": null
    },
    {
      "path": "old_file.go",
      "status": "deleted",
      "insertions": 0,
      "deletions": 42,
      "diff": "diff --git a/old_file.go..."
    }
  ],
  "summary": {
    "files_changed": 3,
    "insertions": 33,
    "deletions": 45
  }
}
```

Diff is provided per file so the frontend can show/hide individual file diffs on click without re-fetching. Untracked files have `"diff": null` (their full content is visible via the blob endpoint).

**UI:**
- List of changed files with status badges (modified, added, deleted, untracked)
- Click file → expands inline diff for that file
- Summary line: "3 files changed, 33 insertions, 45 deletions"

---

### FR-6: Environment Variable Editor

**Endpoint:** `GET /projects/{name}/env`

Reads all `.env*` files in the project root (`.env`, `.env.example`, `.env.prod`, etc.).

**Response:**
```json
{
  "files": [
    {
      "filename": ".env",
      "variables": [
        {"key": "DATABASE_URL", "value": "postgres://localhost:5432/myapp"},
        {"key": "API_KEY", "value": "sk-abc123"}
      ]
    },
    {
      "filename": ".env.example",
      "variables": [
        {"key": "DATABASE_URL", "value": "your-database-url-here"},
        {"key": "API_KEY", "value": "your-api-key-here"}
      ]
    }
  ]
}
```

**Endpoint:** `PUT /projects/{name}/env`

Writes a specific .env file.

**Request:**
```json
{
  "filename": ".env",
  "variables": [
    {"key": "DATABASE_URL", "value": "postgres://newhost:5432/myapp"},
    {"key": "API_KEY", "value": "sk-new-key"}
  ]
}
```

**Response:** `200 OK` with the updated file content, or `400` on validation error.

**Validation rules:**
- Key must not be empty
- Key must match `^[A-Za-z_][A-Za-z0-9_]*$` (valid shell variable name)
- Duplicate keys within the same file are rejected
- Value can be empty (valid — represents unset variable)
- Filename must match `^\.env(\..+)?$` (only .env files allowed)
- Writing to files outside project root is rejected

**UI:**
- Tab selector for each .env file found
- Key-value table with editable value fields
- Add/remove variable buttons
- Save button (per file)
- Inline validation errors (empty key, invalid characters, duplicates) shown before submit
- Values masked by default (click to reveal) for keys containing KEY, SECRET, TOKEN, PASSWORD, or PRIVATE
- Side-by-side with .env.example for reference

---

## Non-Functional Requirements

### NFR-1: Responsive Design

- Mobile-first layout
- Mobile: single-column, hamburger menu, file tree as slide-in panel
- Tablet: sidebar + content
- Desktop: full sidebar + wide content area

### NFR-2: Performance

- File tree lazy-loaded (only fetch contents of opened directories)
- Commit list paginated (50 per page)
- Diff rendering on demand (not pre-loaded for all commits)
- API responses cached client-side where appropriate (file content by ref+path is immutable for committed content)

### NFR-3: Security

- Bearer token required for all endpoints
- CORS configured with explicit allowed origin (no wildcard in production)
- `PROJECTS_DIR` path is validated — API cannot escape outside it (path traversal protection: reject `..`, resolve symlinks, verify resolved path starts with `PROJECTS_DIR`)
- Project name validated against directory listing (no arbitrary path construction)
- .env file write restricted to files within project root matching `.env*` pattern
- Binary file content never sent inline (only metadata)
- No server-side rendering (static SPA — no secrets in HTML)
- Git commands executed with `--no-pager` and timeouts (5s default) to prevent hangs on large repos

### NFR-4: Internationalization

- Czech (primary) + English
- Language detection from browser, manual switch persisted

### NFR-5: Error Handling

- Consistent JSON error responses (same format as existing agent API)
- Frontend: inline error messages, connection error page when server unreachable
- Never show raw stack traces

---

## API Endpoint Summary

| Method | Path | Description |
|--------|------|-------------|
| GET | `/projects` | List all projects |
| GET | `/projects/{name}` | Project metadata |
| GET | `/projects/{name}/tree/{ref}/{path...}` | Directory listing |
| GET | `/projects/{name}/blob/{ref}/{path...}` | File content |
| GET | `/projects/{name}/commits` | Commit history |
| GET | `/projects/{name}/commits/{hash}` | Single commit + diff |
| GET | `/projects/{name}/status` | Uncommitted changes |
| GET | `/projects/{name}/env` | Read .env files |
| PUT | `/projects/{name}/env` | Write .env file |

All endpoints (except `/health`) require `Authorization: Bearer <token>`.

---

## UI Pages

| Page | Route | Description |
|------|-------|-------------|
| Token Entry | `/` (no token) | Input field for API token, stored in localStorage |
| Project List | `/` | Grid/list of all projects |
| Project Detail | `/{project}` | Tabbed view: Files, Commits, Changes, Environment |
| File Viewer | `/{project}/tree/{path}` | Directory listing or file content |
| Commit Detail | `/{project}/commits/{hash}` | Single commit with diff |

---

## Configuration

New environment variables added to `remote/` server:

| Variable | Description | Example |
|----------|-------------|---------|
| `PROJECTS_DIR` | Absolute path to directory containing synced projects | `/home/user/projects` |
| `DASHBOARD_ORIGIN` | Allowed CORS origin for dashboard frontend | `https://dashboard.example.com` |

---

## Technology Stack

### Backend (additions to `remote/`)
- Go (same module)
- `os/exec` for git commands
- Standard library `net/http` (same router pattern)

### Frontend (`dashboard/`)
- SvelteKit with `adapter-static`
- TypeScript
- Shiki (syntax highlighting)
- TailwindCSS (styling)

---

## Future Modules (Not in V1)

These are documented here for architectural awareness. The API structure supports adding them later without breaking changes.

### Workflow Runner
- `POST /projects/{name}/workflows/run` — start a trayline pipeline
- `GET /projects/{name}/workflows/{id}` — status + streaming output (WebSocket)
- UI: workflow selector, parameter input, live output terminal

### File Editor
- `PUT /projects/{name}/files/{path}` — write file content to disk
- UI: CodeMirror/Monaco editor, save button, appears in uncommitted changes after save

---

## Suggested Kiro Spec Breakdown

When implementing, split this master spec into these smaller Kiro specs:

1. **`dashboard-api-projects`** — Backend: project list, detail, file tree, blob endpoints + git package
2. **`dashboard-api-git`** — Backend: commits, status, diff endpoints
3. **`dashboard-api-env`** — Backend: .env read/write endpoints
4. **`dashboard-frontend-setup`** — Frontend: SvelteKit project scaffold, auth, routing, i18n
5. **`dashboard-frontend-projects`** — Frontend: project list page, project detail shell
6. **`dashboard-frontend-files`** — Frontend: file browser, syntax highlighting
7. **`dashboard-frontend-git`** — Frontend: commit list, commit detail, diff viewer, uncommitted changes
8. **`dashboard-frontend-env`** — Frontend: .env editor UI

Each spec is independently implementable and testable.
