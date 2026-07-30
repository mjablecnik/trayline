# Requirements: 006 — Dashboard Frontend File Browser

## Overview

Implement the file browser tab in project detail: directory listing with navigation, and file viewer with syntax highlighting. Read-only.

Source of truth: `dashboard/SPEC.md` (FR-3, UI Pages)

Dependencies: 004-dashboard-frontend-setup, 005-dashboard-frontend-projects (project detail shell, tab integration)

## Requirements

### REQ-1: Directory Listing
- [ ] Fetch directory contents from `GET /projects/{name}/tree/{ref}/{path}`
- [ ] Display entries as a list with icons: folder icon for directories, file icon for files
- [ ] Show file size (human-readable: B, KB, MB) next to file entries
- [ ] Directories sorted first, then files (matching API response order)
- [ ] Click directory → navigates into it (updates URL path)
- [ ] Click file → opens file viewer
- [ ] Loading state: skeleton rows while fetching

### REQ-2: Breadcrumb Navigation
- [ ] Show path breadcrumbs: project root → each directory level → current
- [ ] Each breadcrumb segment is clickable (navigates to that level)
- [ ] Root level shows project name or "/" icon
- [ ] Breadcrumbs wrap on mobile (no horizontal overflow)

### REQ-3: File Viewer
- [ ] Fetch file content from `GET /projects/{name}/blob/{ref}/{path}`
- [ ] Display with syntax highlighting using Shiki
- [ ] Language detected from file extension (shown in header)
- [ ] Line numbers displayed alongside content
- [ ] Monospace font, horizontal scroll for long lines
- [ ] "Raw" button that opens/downloads the raw file content

### REQ-4: Edge Cases
- [ ] Binary files: show message "Binary file (X KB) — cannot display" with download link
- [ ] Truncated files (> 1 MB): show message "File too large to display" with download link
- [ ] Empty files: show "Empty file" message
- [ ] Empty directories: show "This directory is empty" message

### REQ-5: URL Structure
- [ ] Directory listing: `/{project}/tree/{path}` (path can be multi-level like `src/handlers`)
- [ ] File viewer: same URL pattern — behavior determined by API response type
- [ ] Navigating updates browser URL (back/forward works)
- [ ] Direct URL access works (deep-linking to a specific file)

### REQ-6: Responsive Behavior
- [ ] Mobile: full-width list, breadcrumbs wrap, file viewer scrolls horizontally
- [ ] Desktop: comfortable width with readable line lengths
- [ ] Code viewer maintains readability at all breakpoints
