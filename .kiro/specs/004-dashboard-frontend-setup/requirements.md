# Requirements: 004 — Dashboard Frontend Setup

## Overview

Scaffold the SvelteKit frontend application for the Trayline Dashboard. Static SPA with TypeScript, TailwindCSS, internationalization, authentication, and deployment infrastructure.

Source of truth: `dashboard/SPEC.md` (Architecture Frontend, Authentication, NFR-1, NFR-4, NFR-5, Configuration)

Dependencies: None (can be developed in parallel with backend specs, needs backend for integration testing)

## Requirements

### REQ-1: Project Scaffold
- [ ] Create SvelteKit project in `dashboard/` with `adapter-static`
- [ ] TypeScript configured (strict mode)
- [ ] TailwindCSS installed and configured
- [ ] ESLint + Prettier configured
- [ ] `package.json` with all dependencies pinned to exact versions

### REQ-2: Authentication
- [ ] Token entry page shown when no token in localStorage
- [ ] Single input field for Bearer token + "Connect" button
- [ ] Token stored in localStorage on submit
- [ ] All API requests include `Authorization: Bearer <token>` header
- [ ] On 401 response from API, clear stored token and redirect to token entry
- [ ] "Logout" button in header that clears token and redirects to token entry

### REQ-3: API Client
- [ ] Typed API client module (`src/lib/api.ts`) with functions for all dashboard endpoints
- [ ] Base URL configurable via environment variable (`PUBLIC_API_URL`)
- [ ] Automatic Bearer token injection from localStorage
- [ ] Consistent error handling: parse JSON error responses, throw typed errors
- [ ] Handle network errors gracefully (show connection error page)

### REQ-4: Routing
- [ ] `/` — Project list (or token entry if not authenticated)
- [ ] `/{project}` — Project detail with tabs
- [ ] `/{project}/tree/{...path}` — File browser / file viewer
- [ ] `/{project}/commits/{hash}` — Commit detail

### REQ-5: Layout Shell
- [ ] Sticky header with: app name ("Trayline"), language switcher, logout button
- [ ] Responsive: mobile hamburger menu, tablet/desktop horizontal nav
- [ ] Main content area below header
- [ ] Connection error page (displayed when API unreachable)

### REQ-6: Internationalization (i18n)
- [ ] Support Czech (cs) and English (en)
- [ ] Detect browser language on first visit (`navigator.language`)
- [ ] Fall back to English if detected language not supported
- [ ] Persist manual language choice in localStorage
- [ ] Language switch without page reload
- [ ] Update `<html lang="...">` attribute on locale change
- [ ] Language switcher visible in header on every page

### REQ-7: Responsive Design Foundation
- [ ] Mobile-first CSS approach
- [ ] Breakpoints defined as Tailwind theme extensions
- [ ] Mobile: single-column layout
- [ ] Tablet: optional sidebar + content
- [ ] Desktop: full sidebar + wide content area

### REQ-8: Deployment Infrastructure
- [ ] `Dockerfile` — multi-stage build (Node build → nginx/static serve)
- [ ] `fly.toml` — Fly.io configuration for static site
- [ ] `scripts/build.sh` — builds the SvelteKit project
- [ ] `scripts/start-docker.sh` — builds image and runs container locally
- [ ] `scripts/stop-docker.sh` — stops and removes container
- [ ] `scripts/deploy.sh` — deploys to Fly.io (follows backend service standards)
- [ ] `.env.example` with `PUBLIC_API_URL` placeholder

### REQ-9: Error Handling
- [ ] Global error boundary that catches unhandled errors
- [ ] User-friendly fallback UI (localized)
- [ ] Connection error page when API is unreachable
- [ ] Inline error messages for API call failures (not full-page errors)
