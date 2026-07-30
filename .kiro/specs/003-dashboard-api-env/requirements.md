# Requirements: 003 — Dashboard API Environment Variables Module

## Overview

Add endpoints for reading and writing `.env` files within projects. The only write operation in V1 of the dashboard.

Source of truth: `dashboard/SPEC.md` (FR-6)

Dependencies: 001-dashboard-api-projects (router, project validation, path security)

## Requirements

### REQ-1: GET /projects/{name}/env — Read Environment Files
- [ ] Scan project root for files matching `.env*` pattern (`.env`, `.env.example`, `.env.prod`, etc.)
- [ ] For each file, parse key-value pairs (skip empty lines and comments starting with `#`)
- [ ] Return list of files, each with filename and array of {key, value} objects
- [ ] Preserve original key order from the file
- [ ] Return 404 if project not found
- [ ] Return empty `files: []` if no .env files exist (not an error)

### REQ-2: PUT /projects/{name}/env — Write Environment File
- [ ] Accept JSON body with `filename` and `variables` array of {key, value}
- [ ] Write the file atomically (write to temp, then rename)
- [ ] Preserve comment lines from the original file if it exists (re-insert comments at top)
- [ ] Return 200 with updated file content on success
- [ ] Return 400 on validation error with descriptive message

### REQ-3: Input Validation
- [ ] Key must not be empty
- [ ] Key must match `^[A-Za-z_][A-Za-z0-9_]*$` (valid shell variable name)
- [ ] Duplicate keys within the same request are rejected (400)
- [ ] Value can be empty string (valid)
- [ ] Filename must match `^\.env(\..+)?$` regex
- [ ] Reject filenames that would write outside project root

### REQ-4: Security
- [ ] Verify the resolved file path is within the project directory
- [ ] Only allow writing to files that match the `.env*` pattern
- [ ] Do not allow creating .env files in subdirectories (project root only)
- [ ] Log all write operations (filename, project name, number of variables)
