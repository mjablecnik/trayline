# Workflow Verification Plan

## Project Info
- Path: /workspace/dashboard (SvelteKit frontend for the `remote/` agent API server)
- Framework: SvelteKit 2 + Svelte 5 (runes), Tailwind 4, Vite 6, adapter-static
- Port: 5173 (Vite dev server) — see Setup Notes for how to serve
- Base URL: http://localhost:5173
- Auth required: yes (Bearer token entered on the landing page)
- Login credentials: API token = `66b8eb77f5f183cd0078aa5b229d1234c48290338bd3c28d10eb2b9e8d9feb2a`
  (stored by the app in localStorage under key `trayline.token`)

## What is being verified (from BRIEF.md)
The brief (Czech) asks two things:
1. **Run the dashboard and verify every page renders correctly on mobile** according to sound UX/UI
   principles — no horizontal overflow / content cut off, readable text, adequately sized & spaced
   touch targets, header/nav usable, single-column layouts where appropriate.
2. **Verify the mobile (hamburger) menu closes again** after the user clicks a nav link or the page
   changes.

### Relevant implementation facts (read before testing)
- Breakpoints (from `src/app.css`): `tablet = 768px`, `desktop = 1024px`, `wide = 1280px`. The
  primary responsive switch is `tablet` (768px): **below 768px the hamburger button shows**; at
  768px and up the inline nav shows and the hamburger is hidden (`tablet:hidden` / `hidden tablet:flex`).
- Mobile menu lives in `src/lib/components/Header.svelte`. The hamburger button has
  `aria-label="Menu"` and `aria-expanded={mobileMenuOpen}`. When open, a panel renders three links
  (Projects / Sessions / Assistant), the LanguageSwitcher, and (if authed) LogoutButton.
- **Suspected defect to confirm/deny:** the mobile menu links have **no** `onclick` that closes the
  panel and there is **no** `$effect` resetting `mobileMenuOpen` on navigation. So after tapping a
  link the panel is expected to STAY OPEN (aria-expanded stays true / panel still visible). Workflow 3
  is designed to surface this precisely — report it as a UX bug if reproduced.
- Routes: `/` (projects grid), `/sessions`, `/assistant`, and per-project pages under `/[project]/`:
  `tree/[...path]`, `commits`, `commits/[hash]`, `changes`, `env`, `workflows`, `agent`. Project pages
  share a layout with a horizontally-scrollable `TabBar` (Files/Commits/Changes/Env/Workflows/Agent).
- A real project named **`trayline`** exists on the server. Use it for per-project pages.

## Setup Notes
### Backend is already running (do NOT mock anything)
- Full stack is live in Docker on the `trayline-net` network. Agent API server = container
  `trayline-server`, reachable at **`http://trayline-server:9000`** from this sandbox (the host-published
  `0.0.0.0:9000` is NOT reachable — use the container hostname). Verify first:
  `curl -s http://trayline-server:9000/health` → `{"status":"ok"}` (confirmed working).

### Serving the dashboard for Playwright
- From `/workspace/dashboard` (node_modules already installed):
  `PUBLIC_API_URL=http://trayline-server:9000 npm run dev -- --host 0.0.0.0 --port 5173`
  (The committed `.env.local` points at the wrong `trayline-server-test:8081`; override on the CLI as shown.)
- **CORS caveat:** the server sets `DASHBOARD_ORIGIN=https://trayline-dashboard.fly.dev`, so REST calls
  (project list, sessions) from `http://localhost:5173` are blocked by CORS unless the browser runs with
  web security disabled. Launch Chromium with
  `args: ['--disable-web-security', '--user-data-dir=/tmp/pw-profile']`. (WebSocket chat is unaffected.)
- Chromium is pre-installed at `/opt/playwright-browsers` — do NOT run `playwright install`
  (see `sandbox-playwright-browsers` skill).

### Viewport convention for this run
- **Mobile = 375×812** (primary; iPhone-class). Tablet = **768×1024**. A couple of desktop checks at
  **1280×800** to confirm the responsive switch. Tag each task with its viewport, e.g. `[mobile]`.
- On EVERY mobile page, in addition to page-specific assertions, verify there is **no horizontal
  scroll/overflow**: `document.documentElement.scrollWidth <= window.innerWidth + 1` (allow 1px rounding).
  A failure here means content overflows the viewport — a mobile UX bug to report.

## Workflows

### Workflow 1: Login on mobile (375px)
- [x] 1. `[mobile]` Set viewport 375×812, navigate to http://localhost:5173. Expected: the TokenEntry
  screen is visible — an `<h1>` (auth title) and a `type=password` input with a "Connect" submit button;
  the input and button are full-width / comfortably tappable and fit within the 375px viewport (no
  horizontal overflow). — VERIFIED: e2e/verification/workflow-mobile-login.spec.ts, passed.
- [x] 2. `[mobile]` Type the API token into the password field and submit. Expected: the token screen
  disappears and the projects view loads (see Workflow 2). The header now shows the app name on the left
  and a single hamburger button on the right (no inline nav links visible at 375px). — VERIFIED: same
  test file as task 1, passed.

### Workflow 2: Projects page renders correctly on mobile (375px)
- [x] 3. `[mobile]` On http://localhost:5173/ verify the projects grid is a **single column** at 375px
  (cards stack vertically, each card full-width) and at least one card labelled `trayline` is visible.
  Expected: no horizontal overflow; card text/badges are not clipped. — VERIFIED:
  e2e/verification/workflow-mobile-projects-page.spec.ts, passed.
- [x] 4. `[mobile]` Verify the sticky header stays at the top: it has `sticky top-0`; scroll the page and
  confirm the app-name link and hamburger remain visible and aligned within the 375px width. — VERIFIED:
  same test file as task 3, passed.

### Workflow 3: Mobile menu open / close behaviour (375px) — CORE of the brief
- [x] 5. `[mobile]` On any page (start at `/`), click the hamburger button (`aria-label="Menu"`).
  Expected: `aria-expanded` becomes `true` and a menu panel appears listing links **Projects**,
  **Sessions**, **Assistant** plus the language switcher and Logout; the icon switches to an ✕ (close).
  — VERIFIED: e2e/verification/workflow-mobile-menu-open.spec.ts, passed.
- [x] 6. `[mobile]` With the menu open, click the hamburger again. Expected: `aria-expanded` becomes
  `false` and the panel is removed — confirms the toggle closes it. — VERIFIED:
  e2e/verification/workflow-mobile-menu-close-toggle.spec.ts, passed.
- [x] 7. `[mobile]` Open the menu again, then click the **Sessions** link. Expected (per brief): the app
  navigates to `/sessions` AND the mobile menu closes automatically — the panel is gone and
  `aria-expanded` is `false` on the new page. **If the panel is still open / `aria-expanded` stays
  `true` after navigation, record this as a UX defect** (menu does not close on link click). —
  REPRODUCED THE DEFECT, then FIXED: `dashboard/src/lib/components/Header.svelte` mobile nav links had
  no `onclick` closing the panel. Added `onclick={() => (mobileMenuOpen = false)}` to each mobile link.
  VERIFIED: e2e/verification/workflow-mobile-menu-close-on-nav.spec.ts, passed after fix.
- [x] 8. `[mobile]` From `/sessions`, open the menu and click **Assistant**. Expected: navigates to
  `/assistant` and, again, the menu should be closed afterwards. Note the observed behaviour (closed vs.
  still-open) to corroborate task 7. — VERIFIED: same test file as task 7 (covers both navigations),
  passed after the Header.svelte fix.

### Workflow 4: Sessions page renders correctly on mobile (375px)
- [x] 9. `[mobile]` Navigate to http://localhost:5173/sessions. Expected: a page heading (Sessions) is
  visible; either a session list or an empty-state message renders; content is single-column, text is not
  clipped, and there is no horizontal overflow at 375px. — VERIFIED:
  e2e/verification/workflow-mobile-sessions-page.spec.ts, passed.

### Workflow 5: Assistant page renders correctly on mobile (375px)
- [x] 10. `[mobile]` Navigate to http://localhost:5173/assistant. Expected: a Chat/Files tab bar is
  visible (Chat active) and the agent selector (agent/model dropdowns + Start button) fits within 375px
  with no horizontal overflow; tab targets are tappable. — VERIFIED:
  e2e/verification/workflow-mobile-assistant-page.spec.ts, passed.
- [x] 11. `[mobile]` Start the assistant session (select defaults, click Start). Expected: the chat view
  fits the mobile viewport — message log area, a full-width textarea (placeholder "Message the agent..."),
  the 📎 attach button and a Send button all visible and reachable without horizontal scrolling; the input
  row does not overflow the 375px width. — VERIFIED: e2e/verification/workflow-mobile-assistant-chat.spec.ts,
  passed.

### Workflow 6: Per-project pages render correctly on mobile (375px)
- [x] 12. `[mobile]` Navigate to http://localhost:5173/trayline/tree/ (Files tab). Expected: the project
  header + branch selector wrap onto their own line(s) without overflow, and the TabBar
  (Files/Commits/Changes/Env/Workflows/Agent) is **horizontally scrollable** rather than overflowing the
  page — the tab strip itself scrolls but the page body does not overflow at 375px. The active tab
  (Files) is underlined. — VERIFIED: e2e/verification/workflow-mobile-project-tabs.spec.ts, passed.
- [x] 13. `[mobile]` Tap the **Commits** tab. Expected: navigate to the commits view; commit rows render
  in a single column, hashes/messages are not clipped, no horizontal overflow at 375px. — VERIFIED: same
  test file as task 12, passed.
- [x] 14. `[mobile]` Tap the **Changes** tab. Expected: the changes/diff view renders; any diff content is
  contained (its own scroll region) so the page itself has no horizontal overflow at 375px. — VERIFIED:
  same test file as task 12, passed.
- [x] 15. `[mobile]` Tap the **Env** tab. Expected: env file list / editor rows stack readably in a single
  column; inputs fit within 375px; no horizontal overflow. — VERIFIED: same test file as task 12, passed.
- [x] 16. `[mobile]` Tap the **Workflows** tab. Expected: workflow list / empty-state renders in a single
  column; any action buttons are tappable and within the viewport; no horizontal overflow. — VERIFIED:
  e2e/verification/workflow-mobile-project-tabs-2.spec.ts, passed.
- [x] 17. `[mobile]` Tap the **Agent** tab. Expected: the agent chat view (selector then, after Start, the
  message log + textarea + Send) fits the mobile viewport with no horizontal overflow; input row usable.
  — VERIFIED: same test file as task 16, passed.

### Workflow 7: Responsive switch — tablet & desktop (768px / 1280px)
- [x] 18. `[tablet]` Set viewport 768×1024 and load http://localhost:5173/. Expected: the inline nav
  (Projects / Sessions / Assistant links + language switcher + Logout) is now visible in the header and
  the hamburger button is hidden — confirms the `tablet` (768px) breakpoint switches layouts. — VERIFIED:
  e2e/verification/workflow-tablet-desktop-responsive.spec.ts, passed.
- [x] 19. `[tablet]` At 768px, verify the projects grid shows **two columns** (`tablet:grid-cols-2`) and
  there is no horizontal overflow. — VERIFIED: same test file as task 18, passed.
- [x] 20. `[desktop]` Set viewport 1280×800 and load http://localhost:5173/. Expected: inline nav still
  shown, hamburger hidden, projects grid shows **three columns** (`desktop:grid-cols-3`); content is
  centered within the `max-w-6xl` container with no horizontal overflow. — VERIFIED: same test file as
  task 18, passed.

## Environment
- Everything runs locally — the agent API server, spawned agent containers, and LLM calls execute
  against real services on the `trayline-net` Docker network (`http://trayline-server:9000`).
- Do NOT mock any API calls or agent responses. Real WebSocket chat and REST calls only.
- Services that must be running: `trayline-server` (verify `http://trayline-server:9000/health` →
  `{"status":"ok"}`); already up.
- Serve the dashboard locally with `PUBLIC_API_URL=http://trayline-server:9000` and launch Chromium with
  `--disable-web-security` (see Setup Notes → CORS caveat).
