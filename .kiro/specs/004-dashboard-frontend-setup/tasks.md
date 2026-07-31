# Tasks: 004 — Dashboard Frontend Setup

## Task 1: Initialize SvelteKit project
- [x] Create SvelteKit project in `dashboard/` with TypeScript
- [x] Configure `adapter-static` with `fallback: 'index.html'` (SPA mode)
- [x] Configure `svelte.config.js` and `vite.config.ts`
- [x] Configure `tsconfig.json` with strict mode
- [x] Pin all dependency versions in `package.json`

## Task 2: Configure TailwindCSS
- [x] Install TailwindCSS v4
- [x] Create `app.css` with Tailwind directives
- [x] Define breakpoints and spacing scale in config
- [x] Add base styles (font, selection color, reduced-motion)

## Task 3: Implement i18n
- [x] Create `src/lib/i18n/index.ts` with locale store and `t` derived store
- [x] Create `src/lib/i18n/cs.ts` with Czech translations (all UI strings)
- [x] Create `src/lib/i18n/en.ts` with English translations
- [x] Implement browser language detection (`navigator.language`)
- [x] Persist language choice in localStorage
- [x] Update `<html lang="...">` on locale change
- [x] Create language switcher component (CS/EN toggle)

## Task 4: Implement authentication module
- [x] Create `src/lib/auth.ts` — getToken, setToken, clearToken (localStorage)
- [x] Create `src/lib/stores/auth.ts` — reactive auth state store
- [x] Create TokenEntry component (input + connect button)
- [x] On 401 from API → clear token, redirect to token entry
- [x] Logout button in header clears token

## Task 5: Implement API client
- [x] Create `src/lib/api.ts` with typed request helper
- [x] Base URL from `PUBLIC_API_URL` env var
- [x] Auto-inject Bearer token from localStorage
- [x] Parse error responses into typed ApiError
- [x] Handle network errors (throw ConnectionError)
- [x] Export typed functions for all dashboard endpoints

## Task 6: Create layout shell
- [x] Create root `+layout.svelte` with sticky header
- [x] Header contains: app name "Trayline", language switcher, logout button
- [x] Mobile: hamburger menu (for future nav items)
- [x] Desktop: horizontal header layout
- [x] Main content area with proper spacing below header
- [x] Add error boundary wrapping slot content

## Task 7: Set up routing structure
- [x] Create route files (empty placeholders) for all pages:
  - `/` (project list / token entry)
  - `/[project]/` (project detail layout)
  - `/[project]/tree/[...path]` (files)
  - `/[project]/commits` (commit list)
  - `/[project]/commits/[hash]` (commit detail)
  - `/[project]/changes` (uncommitted)
  - `/[project]/env` (environment)
- [x] Create project detail `+layout.svelte` (tab shell placeholder)

## Task 8: Implement error handling
- [x] Create ConnectionError page component (shown when API unreachable)
- [x] Create ErrorFallback component (for unhandled errors)
- [x] Error boundary in root layout catches and displays fallback
- [x] Both error components localized (cs/en)

## Task 9: Create deployment infrastructure
- [x] Create `dashboard/Dockerfile` (multi-stage: node build → nginx serve)
- [x] Create `nginx.conf` for SPA routing (all paths → index.html)
- [x] Create `dashboard/fly.toml` with app config
- [x] Create `dashboard/.env.example` with `PUBLIC_API_URL`
- [x] Create `dashboard/.dockerignore`

## Task 10: Create scripts
- [x] Create `dashboard/scripts/build.sh` — runs npm build
- [x] Create `dashboard/scripts/start-docker.sh` — builds image, runs container
- [x] Create `dashboard/scripts/stop-docker.sh` — stops and removes container
- [x] Create `dashboard/scripts/deploy.sh` — deploys to Fly.io
- [x] All scripts follow project portability pattern (SCRIPT_DIR/PROJECT_DIR)
