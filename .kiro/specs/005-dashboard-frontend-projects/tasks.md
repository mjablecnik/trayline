# Tasks: 005 — Dashboard Frontend Projects

## Task 1: Implement ProjectCard component
- [x] Create `src/lib/components/ProjectCard.svelte`
- [x] Display: project name, branch badge, last commit message (truncated), relative date
- [x] Uncommitted changes indicator (colored dot when has_uncommitted_changes)
- [x] Click handler → navigate to `/{project.name}`
- [x] Hover state with subtle elevation/border change
- [x] Cursor pointer

## Task 2: Implement project list page
- [x] Update `src/routes/+page.svelte`
- [x] If no token → show TokenEntry component
- [x] If token → fetch `GET /projects` on mount
- [x] Loading state: skeleton card grid (3-6 grey placeholder cards)
- [x] Empty state: centered message + icon
- [x] Error state: inline banner with retry button
- [x] Data state: responsive card grid

## Task 3: Implement responsive card grid
- [x] CSS grid: 1 col mobile, 2 cols tablet (768px+), 3 cols desktop (1024px+)
- [x] Consistent gap between cards
- [x] Cards stretch to fill grid cell width

## Task 4: Implement project detail layout
- [x] Create `src/routes/[project]/+layout.svelte`
- [x] Fetch project metadata on mount (`GET /projects/{name}`)
- [x] Display ProjectHeader (project name + breadcrumb back to list)
- [x] Display BranchSelector dropdown
- [x] Display TabBar below header
- [x] Slot for tab content below

## Task 5: Implement BranchSelector component
- [x] Create `src/lib/components/BranchSelector.svelte`
- [x] Dropdown/select with all branches from project metadata
- [x] Current branch pre-selected
- [x] On change: update URL/store, child routes re-fetch with new ref
- [x] Styled as chip/badge with dropdown arrow

## Task 6: Implement TabBar component
- [x] Create `src/lib/components/TabBar.svelte`
- [x] Tabs: Files, Commits, Changes, Environment
- [x] Each tab links to corresponding route
- [x] Active tab determined by current URL path
- [x] Horizontal scroll on mobile (overflow-x: auto)
- [x] Visual indicator on active tab (underline or background)

## Task 7: Implement relative date formatting
- [x] Create `src/lib/utils/date.ts`
- [x] Use `Intl.RelativeTimeFormat` for "2h ago", "3d ago", etc.
- [x] Support both cs and en locales
- [x] Handle edge cases (just now, future dates)
