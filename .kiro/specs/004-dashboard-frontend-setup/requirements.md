# Requirements: 004 — Dashboard Frontend Setup

## Overview

Scaffold the SvelteKit frontend application for the Trayline Dashboard. Static SPA with TypeScript, TailwindCSS, internationalization, authentication, and deployment infrastructure.

Source of truth: `dashboard/SPEC.md` (Architecture Frontend, Authentication, NFR-1, NFR-4, NFR-5, Configuration)

Dependencies: None (can be developed in parallel with backend specs, needs backend for integration testing)

## Requirements

### REQ-1: Project Scaffold
- [x] Create SvelteKit project in `dashboard/` with `adapter-static`
- [x] TypeScript configured (strict mode)
- [x] TailwindCSS installed and configured
- [x] ESLint + Prettier configured
- [x] `package.json` with all dependencies pinned to exact versions

### REQ-2: Authentication
- [x] Token entry page shown when no token in localStorage
- [x] Single input field for Bearer token + "Connect" button
- [x] Token stored in localStorage on submit
- [x] All API requests include `Authorization: Bearer <token>` header
- [x] On 401 response from API, clear stored token and redirect to token entry
- [x] "Logout" button in header that clears token and redirects to token entry

### REQ-3: API Client
- [x] Typed API client module (`src/lib/api.ts`) with functions for all dashboard endpoints
- [x] Base URL configurable via environment variable (`PUBLIC_API_URL`)
- [x] Automatic Bearer token injection from localStorage
- [x] Consistent error handling: parse JSON error responses, throw typed errors
- [x] Handle network errors gracefully (show connection error page)

### REQ-4: Routing
- [ ] `/` — Project list (or token entry if not authenticated)
- [ ] `/{project}` — Project detail with tabs
- [ ] `/{project}/tree/{...path}` — File browser / file viewer
- [ ] `/{project}/commits/{hash}` — Commit detail

### REQ-5: Layout Shell
- [x] Sticky header with: app name ("Trayline"), language switcher, logout button
- [x] Responsive: mobile hamburger menu, tablet/desktop horizontal nav
- [x] Main content area below header
- [x] Connection error page (displayed when API unreachable)

### REQ-6: Internationalization (i18n)
- [x] Support Czech (cs) and English (en)
- [x] Detect browser language on first visit (`navigator.language`)
- [x] Fall back to English if detected language not supported
- [x] Persist manual language choice in localStorage
- [x] Language switch without page reload
- [x] Update `<html lang="...">` attribute on locale change
- [x] Language switcher visible in header on every page

### REQ-7: Responsive Design Foundation
- [x] Mobile-first CSS approach
- [x] Breakpoints defined as Tailwind theme extensions
- [ ] Mobile: single-column layout
- [ ] Tablet: optional sidebar + content
- [ ] Desktop: full sidebar + wide content area

### REQ-8: Deployment Infrastructure
- [x] `Dockerfile` — multi-stage build (Node build → nginx/static serve)
- [x] `fly.toml` — Fly.io configuration for static site
- [x] `scripts/build.sh` — builds the SvelteKit project
- [x] `scripts/start-docker.sh` — builds image and runs container locally
- [x] `scripts/stop-docker.sh` — stops and removes container
- [x] `scripts/deploy.sh` — deploys to Fly.io (follows backend service standards)
- [x] `.env.example` with `PUBLIC_API_URL` placeholder

### REQ-9: Error Handling
- [x] Global error boundary that catches unhandled errors
- [x] User-friendly fallback UI (localized)
- [x] Connection error page when API is unreachable
- [ ] Inline error messages for API call failures (not full-page errors)
