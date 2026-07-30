# Requirements: 008 — Dashboard Frontend Environment Editor

## Overview

Implement the Environment tab in project detail: a UI for viewing and editing `.env` files with key-value pairs, validation, and masked sensitive values.

Source of truth: `dashboard/SPEC.md` (FR-6)

Dependencies: 004-dashboard-frontend-setup, 005-dashboard-frontend-projects (project detail shell, tab integration)

## Requirements

### REQ-1: Environment File Tabs
- [ ] Fetch env files from `GET /projects/{name}/env`
- [ ] Display tab for each .env file found (e.g., `.env`, `.env.example`, `.env.prod`)
- [ ] Active file tab highlighted
- [ ] Switching tabs shows that file's variables
- [ ] Loading state: skeleton table
- [ ] Empty state: "No .env files found in this project" message

### REQ-2: Key-Value Table
- [ ] Display variables as a table/list: Key column, Value column, action column
- [ ] Value fields are editable text inputs
- [ ] Keys are editable text inputs (for new rows) or read-only labels (for existing)
- [ ] Delete button (trash icon) per row to remove a variable
- [ ] "Add variable" button below the table to add a new empty row

### REQ-3: Sensitive Value Masking
- [ ] Values for keys containing KEY, SECRET, TOKEN, PASSWORD, or PRIVATE are masked by default (show dots)
- [ ] Eye/reveal toggle button per masked field to show actual value
- [ ] Masking is client-side only (API always returns real values)
- [ ] Newly added rows are not masked by default

### REQ-4: Validation
- [ ] Key must not be empty — show inline error
- [ ] Key must match `^[A-Za-z_][A-Za-z0-9_]*$` — show "Invalid variable name" error
- [ ] Duplicate keys highlighted with error message
- [ ] Validation runs on input change (real-time feedback)
- [ ] Save button disabled when any validation error exists

### REQ-5: Save Functionality
- [ ] "Save" button per file tab
- [ ] On click, send `PUT /projects/{name}/env` with current file's variables
- [ ] Show success feedback (brief toast or inline message)
- [ ] Show error feedback if save fails (inline error message from API)
- [ ] Unsaved changes indicator (dot on tab or button highlight) when modified
- [ ] Confirm dialog if navigating away with unsaved changes

### REQ-6: Reference View
- [ ] When editing `.env`, show `.env.example` values as reference (if it exists)
- [ ] Reference displayed as muted text below each field or in a side panel
- [ ] Helps user know what keys should exist and what placeholder values look like

### REQ-7: Responsive Behavior
- [ ] Mobile: key and value stack vertically (not side by side)
- [ ] Desktop: key and value in same row (table layout)
- [ ] Action buttons (delete, reveal) remain accessible at all breakpoints
