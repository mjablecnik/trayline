# Workflow Verification Plan

## Project Info
- Path: /workspace/dashboard (SvelteKit frontend for the `remote/` agent API server)
- Framework: SvelteKit 2 + Svelte 5 (runes), Tailwind 4, Vite 6, adapter-static
- Port: 5173 (Vite dev server) — see Setup Notes for how to serve
- Base URL: http://localhost:5173
- Auth required: yes (Bearer token entered on the landing page)
- Login credentials: API token = `66b8eb77f5f183cd0078aa5b229d1234c48290338bd3c28d10eb2b9e8d9feb2a`
  (read from the running `trayline-server` container's `API_TOKEN` env var; stored by the app in localStorage under key `trayline.token`)

## What is being verified (from BRIEF.md)
In the dashboard chat — for BOTH the **project agent** and the **main/assistant agent** — a user must be able to attach a file/image three ways:
1. via the attachment icon (📎 → file picker)
2. via drag-and-drop into the browser
3. via copy-paste (Ctrl/Cmd+V of an image on the clipboard)

The AI agent on the other side must then **recognize and describe** the image, **including reading any text that appears in the image** (OCR). Test images are downloaded from the internet (see Setup Notes).

## Setup Notes

### Backend is already running (do NOT mock anything)
- The full stack is live in Docker on the `trayline-net` network:
  - `trayline-server` (agent API) — container `trayline-server`, listening on **`http://trayline-server:9000`** (host publish `0.0.0.0:9000` is NOT reachable from this sandbox; use the container hostname).
  - `trayline-sandbox` agent containers are spawned per chat session and run the real `claude` CLI (vision-capable). Default agent = `claude`, default model = `sonnet`.
- This sandbox is itself a container attached to `trayline-net`, so `curl http://trayline-server:9000/health` works from the sandbox shell and from a browser launched here. Verify before testing:
  `curl -s http://trayline-server:9000/health` → `{"status":"ok"}`
- If the server ever needs (re)starting, its start script is `remote/scripts/start-docker.sh` (loads `~/.trayline/env/server.env`). See the `sandbox-docker-net` skill if a published Docker port is unreachable.

### Serving the dashboard for Playwright
- Run the Vite dev server from `/workspace/dashboard` with the API URL pointed at the live server:
  `PUBLIC_API_URL=http://trayline-server:9000 npm run dev -- --host 0.0.0.0 --port 5173`
  (`node_modules` is already installed. The committed `.env.local` points at the wrong/exited `trayline-server-test:8081`, so override `PUBLIC_API_URL` on the command line as shown.)
- **CORS caveat (important):** the running server has `DASHBOARD_ORIGIN=https://trayline-dashboard.fly.dev`, so REST responses only carry `Access-Control-Allow-Origin` for that origin. A browser loading the app from `http://localhost:5173` will have its REST calls (project list, sessions) blocked by CORS. WebSocket chat is NOT affected (the server's `CheckOrigin` returns true). To make the REST calls work in the E2E browser, launch Chromium with web security disabled, e.g. Playwright:
  `chromium.launch({ args: ['--disable-web-security', '--user-data-dir=/tmp/pw-profile'] })`.
  (Alternative: restart `trayline-server` with `DASHBOARD_ORIGIN=http://localhost:5173`, but the browser-flag approach is non-invasive and preferred.)
- Chromium is pre-installed at `/opt/playwright-browsers` — do NOT run `playwright install` (see `sandbox-playwright-browsers` skill).

### Test images (download from the internet — internet access confirmed working)
Prepare these files on disk before the run so Playwright can set them on the file input / synthesize drag/paste events. Use deterministic content so assertions are exact:
- **Text/OCR image** `/workspace/.agents/tmp/ocr-test.png` — an image whose only content is a known unique phrase:
  `curl -s -o /workspace/.agents/tmp/ocr-test.png "https://placehold.co/600x200/png?text=TRAYLINE+OCR+7492"`
  Expected recognizable text: `TRAYLINE OCR 7492` (assert the agent's reply contains `7492` and `TRAYLINE OCR`).
- **Photo image** `/workspace/.agents/tmp/photo-test.jpg` — a clearly recognizable real-world subject. Download a stable public-domain image and note its subject, e.g. a red London bus / a banana / the Google logo. Assert the agent's description mentions the known subject keyword. (If a download is flaky, fall back to a second `placehold.co` image with distinct text, and treat it as a second OCR check.)

### Project & routes used
- A real project named **`trayline`** exists on the server (confirmed via `GET /projects`). Use it for the project-agent workflows.
- Project agent chat: `http://localhost:5173/trayline/agent`
- Main/assistant agent chat: `http://localhost:5173/assistant`

### Behavior notes that shape the steps
- Attaching a file only **stages** it (shows a thumbnail/📄 chip above the textarea). The file is uploaded and the agent is prompted only when you press **Send** with a non-empty text message. **You must type a prompt** (e.g. "Describe this image and read any text in it") together with the attachment — an attachment with empty text uploads the file but never triggers an agent response.
- On upload the server emits a `file_uploaded` message → the UI shows a system bubble `📁 <filename> uploaded`.
- Agent replies stream in from a real LLM inside a container, so they can take a while. Use a generous timeout (up to ~120s) when waiting for the agent's description text.
- Starting a chat requires the agent selector: agent defaults to `claude`, model defaults to `sonnet`; click **Start Agent** to open the session.

## Workflows

### Workflow 1: Login with API token
- [x] 1. Navigate to http://localhost:5173. Expected: the token entry screen is visible — heading and a password input with a "Connect"/"Attach" button (component `TokenEntry`).
- [x] 2. Type the API token into the password field and submit. Expected: the token screen disappears and the projects grid loads, showing at least one project card including one named `trayline`.

### Workflow 2: Project agent — attach image via 📎 icon, agent describes it
- [x] 3. Navigate to http://localhost:5173/trayline/agent. Expected: the agent selector is shown with agent `claude` and model `sonnet` preselected and a "Start Agent" button.
- [x] 4. Click "Start Agent" and wait for the session to open. Expected: the selector is replaced by the chat view — a message log area, a textarea with placeholder "Message the agent...", the 📎 attach button, and a "Send" button.
- [x] 5. Set the hidden file input (the 📎 button's `input[type=file]`) to `/workspace/.agents/tmp/photo-test.jpg`. Expected: a pending-attachment chip appears above the textarea showing an image thumbnail and the filename `photo-test.jpg`, with a ✕ remove button.
- [x] 6. Type `Describe this image in one sentence.` in the textarea and click "Send". Expected: a system bubble `📁 photo-test.jpg uploaded` appears, followed by the user's message bubble; the pending chip is cleared.
- [x] 7. Wait for the agent reply (up to ~120s). Expected: an agent message bubble streams in with a coherent description that mentions the known subject of `photo-test.jpg` (e.g. the object/scene it depicts).

### Workflow 3: Project agent — attach image via drag-and-drop
- [x] 8. In the same project-agent session, dispatch a drag-and-drop of `/workspace/.agents/tmp/ocr-test.png` onto the message log area (the element with `role="log"`, which has `ondrop`). Expected: a pending-attachment chip for `ocr-test.png` appears above the textarea.
- [x] 9. Type `What text is written in this image?` and click "Send". Expected: `📁 ocr-test.png uploaded` system bubble, then the user message bubble.
- [x] 10. Wait for the agent reply. Expected: the agent's message contains the exact text from the image — includes `7492` and mentions `TRAYLINE OCR` — proving OCR of image text works over the drag-and-drop path.

### Workflow 4: Project agent — attach image via copy-paste
- [x] 11. Focus the textarea and dispatch a `paste` ClipboardEvent carrying `ocr-test.png` as an `image/*` clipboard file item. Expected: a pending-attachment chip appears (filename like `clipboard-<n>.png`) and no image markup is pasted as text into the textarea.
- [x] 12. Type `Read the text in the pasted image.` and click "Send", then wait for the reply. Expected: `📁 …uploaded` system bubble, then an agent reply that again contains `7492` / `TRAYLINE OCR`, proving the paste path uploads and is recognized.

### Workflow 5: Main/assistant agent — attach image via 📎 icon, agent reads text (OCR)
- [x] 13. Navigate to http://localhost:5173/assistant. Expected: the assistant page loads with a Chat/Files tab bar (Chat active) and an agent selector (agent `claude`, model `sonnet`) with a Start button.
- [x] 14. Start the assistant session. Expected: the chat view appears with the message log, textarea (placeholder "Message the agent..."), the 📎 `FileUploadButton`, and a "Send" button.
- [x] 15. Use the 📎 file input to select `/workspace/.agents/tmp/ocr-test.png`. Expected: a pending-attachment chip with the image thumbnail and filename `ocr-test.png` appears above the textarea.
- [x] 16. Type `Read and quote the text shown in this image.` and click "Send". Expected: `📁 ocr-test.png uploaded` system bubble, then the user message bubble.
- [x] 17. Wait for the agent reply (up to ~120s). Expected: the assistant's reply contains the image's text — includes `7492` and `TRAYLINE OCR` — proving the main agent recognizes text in the image.

### Workflow 6: Main/assistant agent — drag-and-drop image, agent describes it
- [x] 18. In the same assistant session, drag-and-drop `/workspace/.agents/tmp/photo-test.jpg` onto the assistant message log area (element with `role="log"` / `ondrop`). Expected: a pending-attachment chip for `photo-test.jpg` appears.
- [x] 19. Type `Describe what is in this image.` and click "Send". Expected: `📁 photo-test.jpg uploaded` system bubble, then the user message bubble.
- [x] 20. Wait for the agent reply. Expected: an agent message bubble with a coherent description mentioning the known subject of `photo-test.jpg`, confirming the main agent recognizes and describes dropped images.

## Environment
- Everything runs locally — the agent API server, the spawned agent containers, and the LLM calls all execute against real services reachable on the `trayline-net` Docker network (`http://trayline-server:9000`).
- Do NOT mock any API calls or agent responses. These are real end-to-end tests: real WebSocket chat, real file upload into the agent container, real vision model describing/reading the image.
- Services that must be running: `trayline-server` (verify `http://trayline-server:9000/health` → `{"status":"ok"}`) and the Docker daemon proxy (`trayline-proxy`) so the server can spawn `trayline-sandbox` chat containers. Both are already up.
- The dashboard itself must be served locally with `PUBLIC_API_URL=http://trayline-server:9000` and the browser launched with `--disable-web-security` (see Setup Notes → CORS caveat).
