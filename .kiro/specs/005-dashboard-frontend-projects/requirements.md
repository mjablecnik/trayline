# Requirements: 005 — Dashboard Frontend Projects

## Overview

Implement the project list page and project detail shell with tab navigation. The main entry point of the dashboard after authentication.

Source of truth: `dashboard/SPEC.md` (FR-1, FR-2, UI Pages)

Dependencies: 004-dashboard-frontend-setup (layout, API client, routing, i18n)

## Requirements

### REQ-1: Project List Page
- [ ] Fetch and display all projects from `GET /projects`
- [ ] Card grid layout (responsive: 1 col mobile, 2 cols tablet, 3 cols desktop)
- [ ] Each card shows: project name, current branch, last commit message (truncated), relative date
- [ ] Uncommitted changes indicator (colored dot or badge) on cards with `has_uncommitted_changes: true`
- [ ] Click card → navigates to project detail page
- [ ] Loading state: skeleton cards while fetching
- [ ] Empty state: message when no projects found ("No projects synced yet")
- [ ] Error state: inline error message if API call fails

### REQ-2: Project Detail Shell
- [ ] Header showing project name and branch selector dropdown
- [ ] Branch selector fetches branches from `GET /projects/{name}` and allows switching
- [ ] Switching branch updates the Files and Commits tabs (re-fetches with new ref)
- [ ] Tab bar with: Files, Commits, Changes, Environment
- [ ] Active tab indicated visually
- [ ] Tab content area below (content from specs 006-008)
- [ ] Back navigation to project list

### REQ-3: Branch Selector
- [ ] Dropdown listing all branches from project metadata
- [ ] Current branch pre-selected
- [ ] Selecting a branch updates the URL and re-fetches tab content
- [ ] Branch name displayed in a styled badge/chip

### REQ-4: Responsive Behavior
- [ ] Mobile: cards stack vertically, tabs become horizontally scrollable strip
- [ ] Tablet/Desktop: cards in grid, tabs as standard horizontal bar
- [ ] Project header wraps gracefully on narrow screens

### REQ-5: Navigation
- [ ] Breadcrumb: "Projects / {project name}" on detail page
- [ ] Browser back/forward works correctly with tab state
- [ ] Tab selection preserved in URL (query param or hash) so direct links work
