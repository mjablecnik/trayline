# Design: 005 — Dashboard Frontend Projects

## Overview

Project list page (main dashboard view) and project detail shell with tab navigation.

## Component Structure

```
src/routes/
├── +page.svelte                    # Project list (or TokenEntry if not authed)
├── [project]/
│   ├── +layout.svelte              # Project detail shell (header + tabs)
│   ├── +page.svelte                # Redirects to /tree (default tab)
│   ├── tree/[...path]/+page.svelte # Files tab (spec 006)
│   ├── commits/+page.svelte        # Commits tab (spec 007)
│   ├── commits/[hash]/+page.svelte # Commit detail (spec 007)
│   ├── changes/+page.svelte        # Changes tab (spec 007)
│   └── env/+page.svelte            # Environment tab (spec 008)

src/lib/components/
├── ProjectCard.svelte              # Single project card
├── ProjectHeader.svelte            # Project name + branch selector
├── BranchSelector.svelte           # Dropdown for branch switching
└── TabBar.svelte                   # Horizontal tab navigation
```

## Project List Page (`+page.svelte`)

### States
- **Loading**: Skeleton cards (grey rectangles mimicking card layout)
- **Empty**: Centered message + icon ("No projects synced yet")
- **Error**: Inline error banner with retry button
- **Data**: Card grid

### Card Grid Layout

```css
/* Responsive grid */
.project-grid {
  display: grid;
  grid-template-columns: 1fr;              /* mobile: 1 col */
  gap: 1rem;
}
@media (min-width: 768px) { /* tablet: 2 cols */ }
@media (min-width: 1024px) { /* desktop: 3 cols */ }
```

### ProjectCard Component

```
┌─────────────────────────────┐
│ 📁 project-name             │
│ main • 2h ago               │
│ "fix: login redirect"       │
│                    ● changes │
└─────────────────────────────┘
```

Props:
- `project: ProjectSummary`

Click → `goto(`/${project.name}`)`

## Project Detail Shell (`[project]/+layout.svelte`)

### Layout

```
┌─────────────────────────────────────────┐
│ ← Projects / my-app    Branch: [main ▾] │
├─────────────────────────────────────────┤
│ [Files] [Commits] [Changes] [Env]       │
├─────────────────────────────────────────┤
│ <slot /> (tab content from child route) │
└─────────────────────────────────────────┘
```

### Data Loading

On mount, fetch `GET /projects/{name}` for metadata (branches, current branch).
Store in a layout-level store so child routes can access branch info.

### Branch Selector

- Svelte `<select>` styled as dropdown
- On change: update store + re-navigate to current tab with new ref
- Current branch shown as default selected value

### Tab Bar

Tabs map to routes:
| Tab | Route |
|-----|-------|
| Files | `/{project}/tree` |
| Commits | `/{project}/commits` |
| Changes | `/{project}/changes` |
| Environment | `/{project}/env` |

Active tab determined by current URL path segment.
Horizontal scrollable on mobile (overflow-x: auto).

## Navigation

- Breadcrumb in project header: "Projects" is clickable → navigates back to list
- Browser back/forward works via SvelteKit routing
- Deep links work (direct URL to any tab/file/commit)
