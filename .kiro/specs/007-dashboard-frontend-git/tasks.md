# Tasks: 007 — Dashboard Frontend Git History & Changes

## Task 1: Implement CommitRow component
- [ ] Create `src/lib/components/CommitRow.svelte`
- [ ] Display: short hash (monospace), message (truncated ~80 chars), author, relative date
- [ ] Click → navigate to `/{project}/commits/{hash}`
- [ ] Hover state, cursor pointer

## Task 2: Implement commit list page
- [ ] Update `src/routes/[project]/commits/+page.svelte`
- [ ] Fetch commits from API with limit=50, offset=0
- [ ] Render list of CommitRow components
- [ ] "Load more" button when has_more is true
- [ ] Load more appends to existing list (increments offset)
- [ ] Loading state: skeleton rows
- [ ] Empty state: "No commits found"

## Task 3: Implement DiffViewer component
- [ ] Create `src/lib/components/DiffViewer.svelte`
- [ ] Accept raw unified diff string
- [ ] Parse into file sections (split on `diff --git` boundaries)
- [ ] For each file: extract path, parse hunks, classify lines (add/del/context)
- [ ] Render with color coding: green background additions, red deletions

## Task 4: Implement DiffFileSection component
- [ ] Create `src/lib/components/DiffFileSection.svelte`
- [ ] File header: path + stats (+N −M) as colored badges
- [ ] Collapsible (click header to expand/collapse)
- [ ] Line numbers (old + new) in muted columns
- [ ] Monospace font for diff content
- [ ] Handle "diff too large" placeholder

## Task 5: Implement commit detail page
- [ ] Update `src/routes/[project]/commits/[hash]/+page.svelte`
- [ ] Fetch commit detail from API
- [ ] Header: full message, author, date, stats summary
- [ ] Render full diff using DiffViewer
- [ ] Back button → return to commit list
- [ ] Loading state while fetching

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
- [ ] Create `src/lib/utils/diff.ts`
- [ ] Parse unified diff string into structured `DiffFile[]` array
- [ ] Handle multi-file diffs (split on `diff --git` lines)
- [ ] Parse hunk headers (`@@ -n,m +n,m @@`)
- [ ] Classify lines: `+`=add, `-`=del, ` `=context
- [ ] Calculate line numbers for both sides
