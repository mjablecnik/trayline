# Tasks: 007 — Dashboard Frontend Git History & Changes

## Task 1: Implement CommitRow component
- [x] Create `src/lib/components/CommitRow.svelte`
- [x] Display: short hash (monospace), message (truncated ~80 chars), author, relative date
- [x] Click → navigate to `/{project}/commits/{hash}`
- [x] Hover state, cursor pointer

## Task 2: Implement commit list page
- [x] Update `src/routes/[project]/commits/+page.svelte`
- [x] Fetch commits from API with limit=50, offset=0
- [x] Render list of CommitRow components
- [x] "Load more" button when has_more is true
- [x] Load more appends to existing list (increments offset)
- [x] Loading state: skeleton rows
- [x] Empty state: "No commits found"

## Task 3: Implement DiffViewer component
- [x] Create `src/lib/components/DiffViewer.svelte`
- [x] Accept raw unified diff string
- [x] Parse into file sections (split on `diff --git` boundaries)
- [x] For each file: extract path, parse hunks, classify lines (add/del/context)
- [x] Render with color coding: green background additions, red deletions

## Task 4: Implement DiffFileSection component
- [x] Create `src/lib/components/DiffFileSection.svelte`
- [x] File header: path + stats (+N −M) as colored badges
- [x] Collapsible (click header to expand/collapse)
- [x] Line numbers (old + new) in muted columns
- [x] Monospace font for diff content
- [x] Handle "diff too large" placeholder

## Task 5: Implement commit detail page
- [x] Update `src/routes/[project]/commits/[hash]/+page.svelte`
- [x] Fetch commit detail from API
- [x] Header: full message, author, date, stats summary
- [x] Render full diff using DiffViewer
- [x] Back button → return to commit list
- [x] Loading state while fetching

## Task 6: Implement FileStatusBadge component
- [ ] Create `src/lib/components/FileStatusBadge.svelte`
- [ ] Colored circle + status text
- [ ] Colors: modified=yellow/amber, added/untracked=green, deleted=red
- [ ] Compact design (inline with file path)

## Task 7: Implement changes page
- [ ] Update `src/routes/[project]/changes/+page.svelte`
- [ ] Fetch from `GET /projects/{name}/status`
- [ ] If clean → show "Working tree clean" centered message
- [ ] If dirty → show summary line + file list
- [ ] Each file row: status badge + path + expand/collapse toggle
- [ ] First file expanded by default, rest collapsed
- [ ] Expanded shows inline diff (reuse DiffFileSection component)
- [ ] Loading state: skeleton

## Task 8: Implement diff parser utility
- [x] Create `src/lib/utils/diff.ts`
- [x] Parse unified diff string into structured `DiffFile[]` array
- [x] Handle multi-file diffs (split on `diff --git` lines)
- [x] Parse hunk headers (`@@ -n,m +n,m @@`)
- [x] Classify lines: `+`=add, `-`=del, ` `=context
- [x] Calculate line numbers for both sides
