# Requirements: 002 — Dashboard API Git Module

## Overview

Add git history and uncommitted changes endpoints to the `remote/` server. Extends the `git/` package created in spec 001.

Source of truth: `dashboard/SPEC.md` (FR-4, FR-5)

Dependencies: 001-dashboard-api-projects (git package, router, project validation)

## Requirements

### REQ-1: GET /projects/{name}/commits — Commit History
- [ ] Return paginated commit log for a given ref (branch, tag, or hash)
- [ ] Query params: `ref` (default: current branch), `limit` (default: 50, max: 100), `offset` (default: 0)
- [ ] Each commit includes: hash, short_hash, message, author, date (ISO 8601)
- [ ] Response includes `total` count and `has_more` boolean
- [ ] Return 404 if project not found, 400 if ref is invalid

### REQ-2: GET /projects/{name}/commits/{hash} — Single Commit Detail
- [ ] Return full commit metadata: hash, short_hash, message, author, date
- [ ] Include stats: files_changed, insertions, deletions
- [ ] Include full unified diff as string
- [ ] Return 404 if commit hash not found in the project

### REQ-3: GET /projects/{name}/status — Uncommitted Changes
- [ ] Return `clean: true/false` flag
- [ ] List all changed files with: path, status (modified/added/deleted/untracked)
- [ ] Each file includes insertions, deletions counts
- [ ] Each modified/deleted file includes per-file diff string
- [ ] Untracked files have `diff: null`
- [ ] Include summary: total files_changed, insertions, deletions
- [ ] Return 404 if project not found

### REQ-4: Git Package Extensions
- [ ] Add `Log(repoPath, ref, limit, offset)` function returning commits + total count
- [ ] Add `Show(repoPath, hash)` function returning commit detail with diff
- [ ] Add `Status(repoPath)` function returning working tree changes with per-file diffs
- [ ] All functions respect the 5-second timeout from spec 001
- [ ] Handle large diffs gracefully (truncate individual file diffs > 500 KB)
