# Tasks: 005 — Dashboard Frontend Projects

## Task 1: Implement ProjectCard component
- [ ] Create `src/lib/components/ProjectCard.svelte`
- [ ] Display: project name, branch badge, last commit message (truncated), relative date
- [ ] Uncommitted changes indicator (colored dot when has_uncommitted_changes)
- [ ] Click handler → navigate to `/{project.name}`
- [ ] Hover state with subtle elevation/border change
- [ ] Cursor pointer

## Task 2: Implement project list page
- [ ] Update `src/routes/+page.svelte`
- [ ] If no token → show TokenEntry component
- [ ] If token → fetch `GET /projects` on mount
- [ ] Loading state: skeleton card grid (3-6 grey placeholder cards)
- [ ] Empty state: centered message + icon
- [ ] Error state: inline banner with retry button
- [ ] Data state: responsive card grid

## Task 3: Implement responsive card grid
- [ ] CSS grid: 1 col mobile, 2 cols tablet (768px+), 3 cols desktop (1024px+)
- [ ] Consistent gap between cards
- [ ] Cards stretch to fill grid cell width

## Task 4: Implement project detail layout
- [ ] Create `src/routes/[project]/+layout.svelte`
- [ ] Fetch project metadata on mount (`GET /projects/{name}`)
- [ ] Display ProjectHeader (project name + breadcrumb back to list)
- [ ] Display BranchSelector dropdown
- [ ] Display TabBar below header
- [ ] Slot for tab content below

## Task 5: Implement BranchSelector component
- [ ] Create `src/lib/components/BranchSelector.svelte`
- [ ] Dropdown/select with all branches from project metadata
- [ ] Current branch pre-selected
- [ ] On change: update URL/store, child routes re-fetch with new ref
- [ ] Styled as chip/badge with dropdown arrow

## Task 6: Implement TabBar component
- [ ] Create `src/lib/components/TabBar.svelte`
- [ ] Tabs: Files, Commits, Changes, Environment
- [ ] Each tab links to corresponding route
- [ ] Active tab determined by current URL path
- [ ] Horizontal scroll on mobile (overflow-x: auto)
- [ ] Visual indicator on active tab (underline or background)

## Task 7: Implement relative date formatting
- [ ] Create `src/lib/utils/date.ts`
- [ ] Use `Intl.RelativeTimeFormat` for "2h ago", "3d ago", etc.
- [ ] Support both cs and en locales
- [ ] Handle edge cases (just now, future dates)
