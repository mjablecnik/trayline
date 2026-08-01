# Requirements: 007 — Dashboard Frontend Git History & Changes

## Overview

Implement the Commits tab (paginated commit list + commit detail with diff) and Changes tab (uncommitted changes with per-file diffs).

Source of truth: `dashboard/SPEC.md` (FR-4, FR-5, UI Pages)

Dependencies: 004-dashboard-frontend-setup, 005-dashboard-frontend-projects (project detail shell, tab integration)

## Requirements

### REQ-1: Commit List (Commits Tab)
- [x] Fetch commits from `GET /projects/{name}/commits?ref={branch}&limit=50`
- [x] Display as a list: short hash, commit message, author, relative date
- [x] Commit message truncated if too long (single line, ellipsis)
- [x] Click commit → navigates to commit detail page
- [x] "Load more" button at bottom when `has_more: true`
- [x] Loading state: skeleton rows
- [x] Empty state: "No commits found" message

### REQ-2: Commit Detail Page
- [x] Route: `/{project}/commits/{hash}`
- [x] Header: full commit message, author, date, stats (files changed, +insertions, -deletions)
- [x] Diff rendered with syntax highlighting (added lines green background, removed lines red background)
- [x] File sections within the diff clearly separated with file path headers
- [x] Back navigation to commit list
- [x] Loading state while fetching commit detail

### REQ-3: Diff Rendering
- [x] Parse unified diff format into visual diff view
- [x] Line numbers for both old and new file (side or inline)
- [x] Color coding: green for additions, red for deletions, neutral for context lines
- [x] File headers with path and +/- stats per file
- [x] Collapsible file sections (all expanded by default)
- [x] Handle large diffs gracefully (show "Diff too large" if > 500 KB per file)

### REQ-4: Uncommitted Changes (Changes Tab)
- [x] Fetch from `GET /projects/{name}/status`
- [x] Show summary line: "X files changed, Y insertions, Z deletions"
- [x] List changed files with status badges: modified (yellow), added (green), deleted (red), untracked (grey)
- [x] Click file → expands/collapses inline diff for that file
- [x] Diffs rendered with same styling as commit detail
- [x] If `clean: true`, show "Working tree clean — no uncommitted changes" message
- [x] Loading state: skeleton

### REQ-5: Responsive Behavior
- [x] Mobile: commit list items stack, diff view scrolls horizontally
- [x] Diff line numbers hidden on very narrow screens (< 400px)
- [x] File path headers truncated with ellipsis on mobile
