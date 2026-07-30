# Tasks: 006 — Dashboard Frontend File Browser

## Task 1: Implement Breadcrumbs component
- [ ] Create `src/lib/components/Breadcrumbs.svelte`
- [ ] Accept path string, split into segments
- [ ] Each segment clickable (navigates to that directory level)
- [ ] Last segment is current (non-clickable, bold)
- [ ] Root shows project name or "/" icon
- [ ] Wraps on mobile (no horizontal overflow)

## Task 2: Implement DirectoryListing component
- [ ] Create `src/lib/components/DirectoryListing.svelte`
- [ ] Accept entries array from API response
- [ ] Display as list: icon + name + size (for files)
- [ ] Folder icon (📁) for directories, file icon (📄) for files
- [ ] Size formatted human-readable (B, KB, MB)
- [ ] Click directory → navigate deeper
- [ ] Click file → navigate to file view
- [ ] Empty directory: "This directory is empty" message

## Task 3: Implement FileViewer component
- [ ] Create `src/lib/components/FileViewer.svelte`
- [ ] Accept file content, language, size
- [ ] Display with Shiki syntax highlighting
- [ ] Line numbers in left column (monospace, muted)
- [ ] Horizontal scroll for long lines
- [ ] Header: filename, language badge, file size, "Raw" button

## Task 4: Integrate Shiki
- [ ] Install Shiki as dependency
- [ ] Create highlighter instance with theme (github-dark or similar)
- [ ] Load common languages: go, ts, js, python, yaml, json, md, bash, html, css, svelte, sql, toml, rust
- [ ] Create `src/lib/highlight.ts` helper that returns highlighted HTML
- [ ] Handle unknown languages: render as plain text

## Task 5: Implement tree/file page logic
- [ ] Update `src/routes/[project]/tree/[...path]/+page.svelte`
- [ ] Determine if path is directory or file (fetch tree first, if 404 try blob)
- [ ] If directory → render Breadcrumbs + DirectoryListing
- [ ] If file → render Breadcrumbs + FileViewer
- [ ] Loading state: skeleton rows/lines
- [ ] 404 state: "File or directory not found"

## Task 6: Handle edge cases
- [ ] Binary file: show "Binary file (X KB)" message + download button
- [ ] Truncated file (> 1 MB): show "File too large" message + download button
- [ ] Empty file: show "Empty file" in muted text
- [ ] "Raw" button: open content in new tab or trigger download

## Task 7: URL and navigation
- [ ] Navigating directories updates browser URL
- [ ] Back/forward button works correctly
- [ ] Direct URL access to any path works (deep linking)
- [ ] Root path (`/{project}/tree`) shows top-level directory
