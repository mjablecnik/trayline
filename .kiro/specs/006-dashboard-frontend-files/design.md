# Design: 006 — Dashboard Frontend File Browser

## Overview

File browser tab: directory listing with navigation and file viewer with syntax highlighting. Read-only.

## Component Structure

```
src/routes/[project]/tree/[...path]/
└── +page.svelte                # Handles both directory listing and file view

src/lib/components/
├── DirectoryListing.svelte     # Table of files/folders
├── FileViewer.svelte           # Code display with syntax highlighting
├── Breadcrumbs.svelte          # Path breadcrumb navigation
└── FileIcon.svelte             # SVG icon for file type
```

## Page Logic (`tree/[...path]/+page.svelte`)

The page fetches from the API and renders based on response type:

```typescript
const data = await api.getTree(project, ref, path);
// If response has entries[] → render DirectoryListing
// If response is a file → fetch blob and render FileViewer
```

Actually, tree endpoint always returns directory. File click navigates
to same route with file path — page then calls blob endpoint instead:

```typescript
// Determine if path is a file or directory:
// 1. Try tree endpoint first
// 2. If 404, try blob endpoint
// OR: check parent directory listing for type info
```

Simpler approach: always fetch tree. If path points to a file (detected
by parent listing or by tree returning 404), fetch blob instead.

## Directory Listing Component

```
┌──────────────────────────────────────────────┐
│ src / handlers /                   (breadcrumb)│
├──────────────────────────────────────────────┤
│ 📁 middleware/                                │
│ 📁 utils/                                    │
│ 📄 auth.go                          1.2 KB   │
│ 📄 router.go                        3.4 KB   │
│ 📄 types.go                         856 B    │
└──────────────────────────────────────────────┘
```

- Folders first, then files (API returns sorted)
- Click folder → navigate to `/{project}/tree/{ref}/{folder-path}`
- Click file → navigate to `/{project}/tree/{ref}/{file-path}` (triggers blob fetch)
- File size displayed in human-readable format

## Breadcrumbs Component

```typescript
// Given path "src/handlers/auth.go"
// Renders: project-root / src / handlers / auth.go
// Each segment clickable except the last (current)
```

## File Viewer Component

```
┌──────────────────────────────────────────────┐
│ auth.go                    Go • 1.2 KB [Raw] │
├──────────────────────────────────────────────┤
│  1 │ package handlers                        │
│  2 │                                         │
│  3 │ import (                                │
│  4 │     "net/http"                          │
│  5 │ )                                       │
│  6 │                                         │
│  7 │ func AuthMiddleware(next http.Handler)   │
└──────────────────────────────────────────────┘
```

### Syntax Highlighting

Using Shiki (runs at build time for SSG, or client-side for dynamic content):

```typescript
import { createHighlighter } from 'shiki';

const highlighter = await createHighlighter({
  themes: ['github-dark'],
  langs: ['go', 'typescript', 'javascript', 'python', 'yaml', 'json', ...]
});

const html = highlighter.codeToHtml(content, { lang: language, theme: 'github-dark' });
```

### Edge Cases

- **Binary file**: "Binary file (X KB) — cannot display" + download button
- **Truncated file**: "File too large (X MB) — cannot display" + download button
- **Empty file**: "Empty file" grey text
- **Unknown language**: render as plain text (no highlighting)

### Raw/Download

"Raw" button opens the file content in a new tab as plain text,
or triggers download for binary/large files.

## Responsive Behavior

- Mobile: full-width listing, code scrolls horizontally, breadcrumbs wrap
- Desktop: comfortable max-width, monospace font at readable size
- Line numbers always visible (left column)
