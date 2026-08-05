# Verification Setup

## Services Running
| Service | Path | Method | Port | Start Command | Stop Command |
|---|---|---|---|---|---|
| dashboard (frontend) | dashboard | vite dev (background process) | 5173 | `PUBLIC_API_URL=http://trayline-server:9000 npm run dev -- --host 0.0.0.0 --port 5173` (run from `/workspace/dashboard`) | `pkill -f "vite dev --host 0.0.0.0 --port 5173"` |
| trayline-server (backend agent API) | remote/ | docker (already running, pre-existing container) | 9000 (container hostname `trayline-server`, host publish 9000 not reachable from sandbox) | `remote/scripts/start-docker.sh` | `docker stop trayline-server` |
| trayline-proxy (docker socket proxy, spawns sandbox agent containers) | tools/tunnel or remote-managed | docker (already running, pre-existing) | n/a (internal, `trayline-net`) | not managed by this setup | not managed by this setup |

## Rebuild Commands
- dashboard: kill the background `vite dev` process, then re-run `PUBLIC_API_URL=http://trayline-server:9000 npm run dev -- --host 0.0.0.0 --port 5173` from `/workspace/dashboard`.
- trayline-server / trayline-proxy: already running in Docker from before this session started; not restarted. If needed: `remote/scripts/start-docker.sh` (loads `~/.trayline/env/server.env`).

## Base URL
- Frontend: http://localhost:5173
- Backend (from sandbox / browser under test): http://trayline-server:9000 (container hostname on `trayline-net`; host-published `http://localhost:9000` is NOT reachable from this sandbox)

## Playwright Config
- Path: `/workspace/dashboard/playwright.config.ts`
- `testDir: './e2e'` (directory not yet created — to be added when tests are written)
- Chromium-only project, 30s timeout, `baseURL: http://localhost:5173`
- `launchOptions.args: ['--disable-web-security']` to bypass the `DASHBOARD_ORIGIN` CORS restriction on REST calls (WebSocket chat is unaffected by CORS)
- No `webServer` block — the dev server is managed separately (see above)

## Test Images
- `/workspace/.agents/tmp/ocr-test.png` — placehold.co image, text reads exactly `TRAYLINE OCR 7492`. Verified visually (valid PNG).
- `/workspace/.agents/tmp/photo-test.jpg` — valid JPEG (98KB→35KB after retries), subject: **a fox/jackal-like canine walking on dirt** (real-world photo, not a rendered/placeholder image). Verified visually. Assert agent descriptions mention "fox" or a canine-like animal.

## Notes
- `.env.local` in `dashboard/` points at a stale/exited `trayline-server-test:8081` — intentionally overridden via the `PUBLIC_API_URL` env var on the `npm run dev` command line (confirmed the correct value `http://trayline-server:9000` is embedded in the served HTML).
- Image download gotchas encountered and solved:
  - Wikimedia Commons thumbnail URLs (`/thumb/.../640px-*.jpg`) returned HTTP 400 "Use thumbnail sizes listed on ..." (non-whitelisted thumbnail width), and the non-thumb "original" path returned a Swift object-storage "File not found" error — both dead ends for this file. Switched to `https://httpbin.org/image/jpeg`, a stable fixed test-image endpoint, which returned a real photo of a fox/jackal on dirt ground. Recorded to `.agents/MEMORY.md`.
- `trayline-server` and `trayline-proxy` containers were already running when this setup began (confirmed via `docker ps` and `curl http://trayline-server:9000/health` → `{"status":"ok"}`); no action was needed to start them.
- No service start-order issues encountered — backend was already up before the dashboard dev server was started.
- @playwright/test was not previously in `dashboard/package.json`; installed via `npm install -D @playwright/test`. Did NOT run `playwright install` — Chromium is pre-installed at `/opt/playwright-browsers` per the `sandbox-playwright-browsers` skill.
