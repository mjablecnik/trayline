# Design: 007 — Dashboard Frontend Git History & Changes

## Overview

Commits tab (paginated list + detail with diff), and Changes tab (uncommitted changes with per-file expandable diffs).

## Component Structure

```
src/routes/[project]/
├── commits/
│   ├── +page.svelte              # Commit list
│   └── [hash]/+page.svelte       # Commit detail with diff
├── changes/
│   └── +page.svelte              # Uncommitted changes

src/lib/components/
├── CommitList.svelte             # List of commit rows
├── CommitRow.svelte              # Single commit summary row
├── DiffViewer.svelte             # Unified diff renderer
├── DiffFileSection.svelte        # Single file within a diff
├── FileStatusBadge.svelte        # Colored badge (modified/added/deleted)
└── LoadMoreButton.svelte         # Pagination trigger
```

## Commits Tab (`commits/+page.svelte`)

### Data Flow
```typescript
let commits = [];
let hasMore = true;
let offset = 0;
const LIMIT = 50;

async function loadMore() {
  const res = await api.getCommits(project, ref, LIMIT, offset);
  commits = [...commits, ...res.commits];
  hasMore = res.has_more;
  offset += LIMIT;
}
```

### Commit Row Layout
```
abc1234  fix: resolve login redirect       Martin  2h ago
```
- Short hash (monospace, muted color)
- Message (truncated at ~80 chars with ellipsis)
- Author name
- Relative date (using Intl.RelativeTimeFormat)
- Click → navigate to `/{project}/commits/{hash}`

## Commit Detail Page (`commits/[hash]/+page.svelte`)

### Layout
```
┌─────────────────────────────────────────────────┐
│ ← Back to commits                               │
├─────────────────────────────────────────────────┤
│ fix: resolve login redirect                     │
│ Martin • 2 hours ago • 3 files (+12 −5)         │
├─────────────────────────────────────────────────┤
│ 📄 src/auth/login.go                    +8 −3   │
│ ┌───────────────────────────────────────────┐   │
│ │  15 │   func handleLogin(...) {           │   │
│ │  16 │-      http.Redirect(w, r, "/", 302) │   │
│ │  16 │+      redirectURL := r.URL.Query()  │   │
│ │  17 │+      if redirectURL == "" {        │   │
│ │  ...│                                     │   │
│ └───────────────────────────────────────────┘   │
│                                                 │
│ 📄 src/auth/login_test.go               +4 −2   │
│ ┌───────────────────────────────────────────┐   │
│ │  ...                                      │   │
│ └───────────────────────────────────────────┘   │
└─────────────────────────────────────────────────┘
```

## DiffViewer Component

Parses unified diff string into visual representation:

```typescript
interface DiffFile {
  path: string;
  insertions: number;
  deletions: number;
  hunks: DiffHunk[];
}

interface DiffHunk {
  header: string; // @@ -15,7 +15,12 @@
  lines: DiffLine[];
}

interface DiffLine {
  type: 'add' | 'del' | 'context';
  oldLineNo?: number;
  newLineNo?: number;
  content: string;
}
```

### Diff Parsing

Parse unified diff format:
1. Split on `diff --git` boundaries → file sections
2. Extract filename from `--- a/` and `+++ b/` lines
3. Split on `@@` → hunks
4. Classify lines: `+` = add, `-` = del, ` ` = context

### Styling
- Added lines: green background (`bg-green-900/20`), green text
- Deleted lines: red background (`bg-red-900/20`), red text
- Context lines: neutral background
- Line numbers: muted grey, monospace
- File headers: bold path, +/- stats as colored badges

## Changes Tab (`changes/+page.svelte`)

### Layout
```
┌─────────────────────────────────────────────────┐
│ 3 files changed, 33 insertions, 45 deletions    │
├─────────────────────────────────────────────────┤
│ 🟡 modified   src/handlers/user.go       [▾]    │
│ ┌───────────────────────────────────────────┐   │
│ │  (inline diff, expanded)                  │   │
│ └───────────────────────────────────────────┘   │
│ 🟢 added      src/handlers/avatar.go     [▸]    │
│ 🔴 deleted    src/old_handler.go         [▸]    │
└─────────────────────────────────────────────────┘
```

### Behavior
- Summary line at top
- Each file row is expandable (click to toggle diff)
- First file expanded by default, rest collapsed
- Status badges: colored circles + text
- If `clean: true` → show "Working tree clean" message

### File Status Colors
| Status | Color | Badge |
|--------|-------|-------|
| modified | yellow/amber | 🟡 |
| added/untracked | green | 🟢 |
| deleted | red | 🔴 |

## Responsive Notes

- Mobile: diff scrolls horizontally, line numbers hidden < 400px width
- File paths truncated with ellipsis on narrow screens
- Commit message in list truncated more aggressively on mobile
