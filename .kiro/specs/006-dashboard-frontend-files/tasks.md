# Tasks: 006 — Dashboard Frontend File Browser

## Task 1: Implement Breadcrumbs component
- [x] Create `src/lib/components/Breadcrumbs.svelte`
- [x] Accept path string, split into segments
- [x] Each segment clickable (navigates to that directory level)
- [x] Last segment is current (non-clickable, bold)
- [x] Root shows project name or "/" icon
- [x] Wraps on mobile (no horizontal overflow)

## Task 2: Implement DirectoryListing component
- [x] Create `src/lib/components/DirectoryListing.svelte`
- [x] Accept entries array from API response
- [x] Display as list: icon + name + size (for files)
- [x] Folder icon (📁) for directories, file icon (📄) for files
- [x] Size formatted human-readable (B, KB, MB)
- [x] Click directory → navigate deeper
- [x] Click file → navigate to file view
- [x] Empty directory: "This directory is empty" message

## Task 3: Implement FileViewer component
- [x] Create `src/lib/components/FileViewer.svelte`
- [x] Accept file content, language, size
- [x] Display with Shiki syntax highlighting
- [x] Line numbers in left column (monospace, muted)
- [x] Horizontal scroll for long lines
- [x] Header: filename, language badge, file size, "Raw" button

## Task 4: Integrate Shiki
- [x] Install Shiki as dependency
- [x] Create highlighter instance with theme (github-dark or similar)
- [x] Load common languages: go, ts, js, python, yaml, json, md, bash, html, css, svelte, sql, toml, rust
- [x] Create `src/lib/highlight.ts` helper that returns highlighted HTML
- [x] Handle unknown languages: render as plain text

## Task 5: Implement tree/file page logic
- [x] Update `src/routes/[project]/tree/[...path]/+page.svelte`
- [x] Determine if path is directory or file (fetch tree first, if 404 try blob)
- [x] If directory → render Breadcrumbs + DirectoryListing
- [x] If file → render Breadcrumbs + FileViewer
- [x] Loading state: skeleton rows/lines
- [x] 404 state: "File or directory not found"

## Task 6: Handle edge cases
- [x] Binary file: show "Binary file (X KB)" message + download button
- [x] Truncated file (> 1 MB): show "File too large" message + download button
- [x] Empty file: show "Empty file" in muted text
- [x] "Raw" button: open content in new tab or trigger download

## Task 7: URL and navigation
- [x] Navigating directories updates browser URL
- [x] Back/forward button works correctly
- [x] Direct URL access to any path works (deep linking)
- [x] Root path (`/{project}/tree`) shows top-level directory
