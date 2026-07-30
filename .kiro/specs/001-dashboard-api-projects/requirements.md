# Requirements: 001 — Dashboard API Projects Module

## Overview

Add a "projects" module to the existing `remote/` Go server that provides REST endpoints for project discovery, metadata, file browsing, and file content retrieval. This is the foundation for the Trayline Dashboard backend.

Source of truth: `dashboard/SPEC.md` (FR-1, FR-2, FR-3, Architecture, CORS, NFR-3)

## Requirements

### REQ-1: CORS Middleware
- [ ] Add CORS middleware to the `remote/` server, applied before auth middleware
- [ ] Allowed origin read from `DASHBOARD_ORIGIN` env var
- [ ] Allowed methods: GET, PUT, OPTIONS
- [ ] Allowed headers: Authorization, Content-Type
- [ ] Preflight cache: 3600s
- [ ] OPTIONS requests return 204 with CORS headers (no auth required for preflight)

### REQ-2: Project Discovery Configuration
- [ ] Add `PROJECTS_DIR` environment variable to server config
- [ ] Update `.env.example` with `PROJECTS_DIR` placeholder
- [ ] Validate `PROJECTS_DIR` exists and is a directory at server startup
- [ ] Log error and refuse to start if `PROJECTS_DIR` is not set or invalid

### REQ-3: GET /projects — List All Projects
- [ ] Scan `PROJECTS_DIR` for subdirectories containing a `.git/` folder
- [ ] For each project, return: name, path, current branch, last commit (hash, message, author, date), has_uncommitted_changes flag
- [ ] Sort by last commit date (newest first)
- [ ] Return empty array (not error) if no projects found
- [ ] Skip non-git directories silently

### REQ-4: GET /projects/{name} — Project Metadata
- [ ] Return project name, current branch, list of all branches, remote URL, last commit
- [ ] Return 404 with standard error response if project not found
- [ ] Validate project name against actual directory listing (no path construction from user input)

### REQ-5: GET /projects/{name}/tree/{ref}/{path...} — Directory Listing
- [ ] List directory contents at given git ref and path
- [ ] `{ref}` is a branch name, tag, or commit hash — reads from git object store (committed state only)
- [ ] Return entries with: name, type (file/directory), size (for files)
- [ ] Directories sorted first, then files, both alphabetically
- [ ] Return 404 if path does not exist at given ref
- [ ] Root path (empty or `/`) returns top-level directory contents

### REQ-6: GET /projects/{name}/blob/{ref}/{path...} — File Content
- [ ] Return file content, size, detected language (from extension)
- [ ] Files ≤ 1 MB: content returned inline as string
- [ ] Files > 1 MB: content is null, `truncated: true`
- [ ] Binary files (detected by content inspection): content is null, `binary: true`
- [ ] Return 404 if file does not exist at given ref

### REQ-7: Path Security
- [ ] Reject any path containing `..` segments
- [ ] Resolve symlinks and verify resolved path starts with `PROJECTS_DIR`
- [ ] Project name must match an existing directory in `PROJECTS_DIR` (validated against listing)
- [ ] Return 400 for invalid paths, 404 for non-existent paths

### REQ-8: Git Package Foundation
- [ ] Create `git/` package in `remote/` for git command execution
- [ ] All git commands use `--no-pager` flag
- [ ] All git commands have a 5-second timeout (configurable)
- [ ] Git errors (non-zero exit) return structured error responses
- [ ] Package provides functions for: current branch, branch list, last commit, tree listing, blob content, is-git-repo check

### REQ-9: Module Registration
- [ ] Register all new routes in the existing router (`api/router.go`)
- [ ] New endpoints share existing auth and rate limiting middleware
- [ ] Organize project handler code in `api/` directory consistent with existing code structure
