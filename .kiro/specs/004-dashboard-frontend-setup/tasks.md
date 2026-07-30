# Tasks: 004 — Dashboard Frontend Setup

## Task 1: Initialize SvelteKit project
- [ ] Create SvelteKit project in `dashboard/` with TypeScript
- [ ] Configure `adapter-static` with `fallback: 'index.html'` (SPA mode)
- [ ] Configure `svelte.config.js` and `vite.config.ts`
- [ ] Configure `tsconfig.json` with strict mode
- [ ] Pin all dependency versions in `package.json`

## Task 2: Configure TailwindCSS
- [ ] Install TailwindCSS v4
- [ ] Create `app.css` with Tailwind directives
- [ ] Define breakpoints and spacing scale in config
- [ ] Add base styles (font, selection color, reduced-motion)

## Task 3: Implement i18n
- [ ] Create `src/lib/i18n/index.ts` with locale store and `t` derived store
- [ ] Create `src/lib/i18n/cs.ts` with Czech translations (all UI strings)
- [ ] Create `src/lib/i18n/en.ts` with English translations
- [ ] Implement browser language detection (`navigator.language`)
- [ ] Persist language choice in localStorage
- [ ] Update `<html lang="...">` on locale change
- [ ] Create language switcher component (CS/EN toggle)

## Task 4: Implement authentication module
- [ ] Create `src/lib/auth.ts` — getToken, setToken, clearToken (localStorage)
- [ ] Create `src/lib/stores/auth.ts` — reactive auth state store
- [ ] Create TokenEntry component (input + connect button)
- [ ] On 401 from API → clear token, redirect to token entry
- [ ] Logout button in header clears token

## Task 5: Implement API client
- [ ] Create `src/lib/api.ts` with typed request helper
- [ ] Base URL from `PUBLIC_API_URL` env var
- [ ] Auto-inject Bearer token from localStorage
- [ ] Parse error responses into typed ApiError
- [ ] Handle network errors (throw ConnectionError)
- [ ] Export typed functions for all dashboard endpoints

## Task 6: Create layout shell
- [ ] Create root `+layout.svelte` with sticky header
- [ ] Header contains: app name "Trayline", language switcher, logout button
- [ ] Mobile: hamburger menu (for future nav items)
- [ ] Desktop: horizontal header layout
- [ ] Main content area with proper spacing below header
- [ ] Add error boundary wrapping slot content

## Task 7: Set up routing structure
- [ ] Create route files (empty placeholders) for all pages:
  - `/` (project list / token entry)
  - `/[project]/` (project detail layout)
  - `/[project]/tree/[...path]` (files)
  - `/[project]/commits` (commit list)
  - `/[project]/commits/[hash]` (commit detail)
  - `/[project]/changes` (uncommitted)
  - `/[project]/env` (environment)
- [ ] Create project detail `+layout.svelte` (tab shell placeholder)

## Task 8: Implement error handling
- [ ] Create ConnectionError page component (shown when API unreachable)
- [ ] Create ErrorFallback component (for unhandled errors)
- [ ] Error boundary in root layout catches and displays fallback
- [ ] Both error components localized (cs/en)

## Task 9: Create deployment infrastructure
- [ ] Create `dashboard/Dockerfile` (multi-stage: node build → nginx serve)
- [ ] Create `nginx.conf` for SPA routing (all paths → index.html)
- [ ] Create `dashboard/fly.toml` with app config
- [ ] Create `dashboard/.env.example` with `PUBLIC_API_URL`
- [ ] Create `dashboard/.dockerignore`

## Task 10: Create scripts
- [ ] Create `dashboard/scripts/build.sh` — runs npm build
- [ ] Create `dashboard/scripts/start-docker.sh` — builds image, runs container
- [ ] Create `dashboard/scripts/stop-docker.sh` — stops and removes container
- [ ] Create `dashboard/scripts/deploy.sh` — deploys to Fly.io
- [ ] All scripts follow project portability pattern (SCRIPT_DIR/PROJECT_DIR)
