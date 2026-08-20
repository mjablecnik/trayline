# Test Plan: OpenAI-Compatible API Layer

**Spec:** `.kiro/specs/openai-compatible-api/` (requirements.md, design.md, tasks.md)
**Target module:** `remote/` (Go module `remote`)
**Written:** 2026-08-20, after a full read of the spec + existing `remote/` code, before implementation tasks 8–12 landed.

This document is a self-contained work order. An agent picking it up cold should be able to execute
it end to end without re-deriving anything from the spec.

---

## 0. Precondition gate

Do not start until **all** of these hold:

```bash
cd /workspace
grep -c '^- \[ \]' .kiro/specs/openai-compatible-api/tasks.md   # must print 0
git status --porcelain                                          # must print nothing
cd remote && go build ./... && go test ./...                    # must pass
```

Note: `git stash list` has two entries from 2026-05-28 and 2026-06-13 (`WIP on master`, pipeline
YAML renames). They are unrelated to this feature — ignore them, do not pop them.

---

## 1. Why this plan exists

The spec is EARS-style with 12 requirements / ~70 acceptance criteria. Task 11 of `tasks.md`
prescribes 17 unit tests, which cover roughly the validation surface and nothing else. That is not
enough to call the feature verified:

- No test proves the wire format is actually consumable by a real OpenAI SDK — which is the entire
  point of the feature.
- No test covers the streaming failure modes (mid-stream container crash, client disconnect).
- No test covers slot accounting, which is where the design has a concrete defect (see §2 A).

This plan adds four layers on top of task 11 and defines the acceptance gate.

---

## 2. Known risks found during spec review

These were identified by reading the spec against the existing code. **Each must end up with a
dedicated test.** If the implementation already fixed one, keep the test as a regression guard.

### A. Double slot acquisition → capacity halving or deadlock  *(highest severity)*

`design.md` step 6 and `tasks.md` task 6 both say the handler acquires a slot and then calls
`RunOneShot`. But `docker/container.go` — `RunOneShot` **already acquires a slot itself**
(`m.acquireSlot(timeoutCtx, createdAt)` right at the top, `defer m.releaseSlot()`).

- `acquireSlot` is unexported and **blocking** (FIFO queue), not fail-fast. Requirement 7.2 demands
  an immediate 429 when the pool is empty — the current primitive cannot express that. A
  `TryAcquireTaskSlot() bool` is needed.
- If the handler does acquire separately, one `/v1/chat/completions` request consumes **two** task
  slots. With `MAX_CONCURRENT_TASKS=2` effective concurrency drops to 1. With `=1` the handler takes
  the only slot and `RunOneShot` then blocks on itself until `TASK_TIMEOUT` (default 10 min) → 500.

**Tests required:**
- `TestChatCompletions_SlotAccounting` — with `MaxConcurrentTasks=1`, one in-flight request must not
  self-deadlock; assert it completes in well under the task timeout.
- `TestChatCompletions_CapacityReturns429Fast` — saturate the pool, assert the next request returns
  429 + `Retry-After: 30` **within ~1s** (not after a timeout).
- `TestChatCompletions_SlotReleasedOnAllPaths` — success, agent error, validation error, client
  disconnect mid-stream. Assert `cm.AvailableSlots()` returns to its initial value each time.

### B. `ContainerRunner` interface too narrow to test against

`api/task_handler.go` defines:

```go
type ContainerRunner interface {
    RunOneShot(ctx, agent, prompt, model, system string, createdAt time.Time, onStart func(string)) (*docker.ContainerResult, error)
    StopAndRemoveContainer(ctx context.Context, containerID string) error
}
```

The OpenAI handler additionally needs `RunOneShotStreaming` and slot control. If the implementation
bound the handler to the concrete `*docker.ContainerManager`, unit tests are impossible without
Docker. **Check this first** — if it happened, the fix is to widen the interface (or add a second,
narrower one), not to skip the tests. Do not change `TaskHandler`'s use of the existing interface.

### C. `content` accepted only as `string`

`OpenAIMessage.Content` is `string`. Real clients (openai-python ≥1.x with multimodal helpers,
Cline, Continue, LangChain, LibreChat, Open WebUI) routinely send:

```json
{"role": "user", "content": [{"type": "text", "text": "Hello"}]}
```

That fails `json.Unmarshal` into `string` → 400 on a legitimate request. Also the `developer` role
(OpenAI's replacement for `system`) is rejected by Requirement 9.4's allowlist.

**Test:** `test_compat_quirks.py::test_content_as_array`, `::test_developer_role`.
**Expected outcome is a decision, not automatically a pass** — if the server 400s, record it as a
known limitation in `API.md` and report it. Recommended fix: custom `UnmarshalJSON` on
`OpenAIMessage` that flattens text parts and maps `developer` → `system`.

### D. `stream_options: {"include_usage": true}` unhandled

SDKs send it whenever the caller wants token counts from a stream, and then expect one extra final
chunk carrying `usage` with an empty `choices` array. The spec does not mention it.
**Test:** `test_streaming.py::test_stream_options_include_usage` — at minimum the request must not
error; ideally the usage chunk is emitted.

### E. `estimateTokens` counts bytes, not characters

`estimateTokens` uses `len(text)` (bytes). Requirement 8.2 says "character length". For Czech text
the estimate is inflated roughly 1.5–2×, for emoji up to 4×.
**Test:** `TestEstimateTokens_Unicode` — assert the chosen semantics explicitly (`utf8.RuneCountInString`
recommended) so the behaviour is pinned either way.

### F. 429 is overloaded — rate limiter vs. slot exhaustion

Both return 429 with `type: "server_error"`. Worse: **the OpenAI SDK retries 429 automatically**
(default `max_retries=2` with backoff). Any test asserting a status code must construct the client
with `max_retries=0`, otherwise a passing test may be masking a failure, and concurrency tests will
measure the retry loop rather than the server.

`RATE_LIMIT` defaults to 60/min per IP — a concurrency test firing more than that trips the rate
limiter instead of the slot pool. Raise `RATE_LIMIT` in the test server config.

### G. Unmatched `/v1/` paths fall through to Trayline-format 404

`POST /v1/embeddings`, `GET /v1/completions`, or `GET /v1/models/` (trailing slash) hit the mux
default, which returns either an empty body or `core.ErrorResponse`. SDKs then fail to parse the
error. **Test:** `test_compat_quirks.py::test_unknown_v1_endpoint` — assert the body is
OpenAI-shaped (`{"error": {...}}`) or record as a known limitation.

### H. CORS restricted to `dashboardOrigin`

Browser-side use of the openai SDK will be blocked by preflight. Probably intentional (the token
would be exposed anyway) — just confirm and document. **Test:** `test_cors_preflight` (assert
current behaviour, whatever it is, so a future change is noticed).

---

## 3. Test layers

```
L1  Go unit          remote/api/openai_*_test.go              ~1 s    no Docker
L2  Go integration   httptest.Server + fake ContainerRunner   ~5 s    no Docker
L3  SDK conformance  Python openai SDK → fake server         ~20 s   no Docker, no credits
L4  Live smoke       same suite → real server + real agents  ~2 min  costs credits
L5  Interop & load   curl script, real client, fuzz, k6      manual
```

### L1 — Go unit tests

Extend (do not replace) whatever task 11 produced. Add in `remote/api/`:

| File | Tests |
|---|---|
| `openai_registry_test.go` | `TestModelRegistry_Resolve` (exact, `KIRO`, `Kiro`, mixed case), `_ResolveNotFound`, `_EmptyConfig` (falls back to defaults), `_MalformedConfig` (entry without colons, trailing comma, blank names — must not panic), `_List_Order`, `_Immutable` (List() result mutation does not affect registry) |
| `openai_composer_test.go` | Table test over Req 11.1–11.6: single user → prompt verbatim, no labels; system-only extraction and newline join of multiple systems; multi-turn label format exactly `User:\n{c}\n\nAssistant:\n{c}`; adjacent same-role each labelled; no system → empty system string; trailing whitespace behaviour |
| `openai_sse_test.go` | `TestSSEWriter_Headers`, `_FirstChunkHasRole`, `_SubsequentChunksNoRole` (Req 2.6), `_WireFormat` (byte-exact `data: {json}\n\n`), `_WriteDone_FinalChunkThenDONE`, `_WriteError_StillTerminates`, `_ExactlyOneDONE` (Property 5), `_FlushCalledPerChunk` (Req 2.1), `_NonFlusherWriter` (returns error, no panic) |
| `openai_types_test.go` | `TestEstimateTokens` table (incl. unicode — risk E), `TestGenerateCompletionID_Format` (prefix, length ≥24, alphanumeric) and `_Uniqueness` (10k IDs, no collision), `TestWriteOpenAIError_NullParamCode` (must serialise as JSON `null`, not omitted — Req 5.2) |

Invariants worth asserting as properties (cheap, catch regressions):
- `usage.total_tokens == prompt_tokens + completion_tokens` for any input (Property 7)
- every response, streaming or not, has exactly one choice with `index: 0` (Property 2)
- every started stream ends with exactly one `data: [DONE]\n\n` (Property 5)

`httptest.ResponseRecorder` implements `http.Flusher`, so `SSEWriter` needs no server.

### L2 — Go integration tests

`remote/api/openai_integration_test.go`. Build the **real** router via `NewRouter(...)` so the full
middleware chain (recovery → CORS → ratelimit → auth → requestID → mux) is exercised, and serve it
with `httptest.NewServer`. Inject a scriptable fake container runner.

```go
// scriptedRunner implements the container interface used by OpenAIHandler.
// Behaviour is selected by magic tokens in the prompt so both Go tests and the
// Python suite can drive it over the wire.
type scriptedRunner struct {
    slots      int32   // mirrors task slot pool for assertions
    chunkDelay time.Duration
}
```

Scripted behaviours (prompt token → behaviour):

| Token in prompt | Behaviour |
|---|---|
| `__echo__` | returns JSON `{"system": "...", "prompt": "..."}` — lets black-box tests verify `ComposeMessages` output (Req 11) |
| `__slow__` | emits 5 chunks, 50 ms apart |
| `__crash__` | emits 2 chunks then returns a non-zero exit / error mid-stream |
| `__empty__` | returns empty stdout (Req 8.3 — content must be `""`, not null) |
| `__big__` | 5 MB of output |
| `__ansi__` | output with ANSI escape sequences (kiro path — must be stripped) |
| `__ndjson__` | claude-style NDJSON stream-json frames |
| `__hang__` | blocks until ctx cancelled (timeout + disconnect tests) |
| anything else | returns `"Hello from fake agent"` |

Cases:

- Non-streaming happy path — full response shape, `id` prefix, `object`, `created` sanity, `model` echo, `usage`, `finish_reason: "stop"`
- Streaming happy path — read raw bytes off the wire, assert ≥2 `data:` frames, role only in first, terminating `[DONE]`, correct ordering (Property 3)
- Mid-stream crash → stop chunk + `[DONE]`, connection closed cleanly (Req 2.7)
- Client disconnect mid-stream → no panic, slot released (Req 7.4, risk A)
- Timeout → configure a 2s `TaskTimeout`, use `__hang__`, assert 500 `server_error` with timeout message
- Auth: missing header → 401 OpenAI format, `param: null`, `code: null`; wrong token → 401 `code: "invalid_api_key"` (Req 5.2/5.3)
- Auth on `GET /v1/models` too (Req 5.4)
- 429 on slot exhaustion with `Retry-After: 30` (Req 7.2)
- 429 from the rate limiter on `/v1/` → OpenAI format, `type: "server_error"` (task 9)
- Validation matrix (Req 9.1–9.4) — one subtest per case, asserting **both** status and `param`:
  `model` missing/empty → `"model"`; `messages` missing/empty → `"messages"`; message without role →
  `"messages[0]"`; message without content → `"messages[0]"`; bad role → `"messages[1].role"` with
  the correct index; malformed JSON body → 400
- Unknown model → 404 `code: "model_not_found"` (Req 1.8, 4.3)
- Ignored params (Req 10.1–10.4) — send all of `temperature`, `top_p`, `max_tokens`, `stop` as both
  string and array, `n: 3`, `presence_penalty`, `frequency_penalty`, `logit_bias`, `user`, plus a
  junk key `"totally_unknown": {...}`, plus **wrong types** (`"temperature": "hot"`) → all must be
  accepted, and `n: 3` must still yield exactly one choice (Req 10.2)
- `GET /v1/models` list shape, `GET /v1/models/{id}` hit and 404 (Req 3.1–3.4), empty registry → empty array (Req 3.5)
- **Backward compatibility (Req 12)**: `POST /run`, `GET /runs`, `GET /health` still behave
  identically — plus confirm the pre-existing suite passes unchanged.

### L3 — SDK conformance suite (the deliverable to "just run")

Layout:

```
remote/testing/openai-conformance/
├── README.md
├── run.sh                  # venv + pip install + pytest; --live to hit a real server
├── requirements.txt        # openai>=3.3, pytest, pytest-timeout
├── conftest.py             # fixtures: base_url, client (max_retries=0!), fake server lifecycle
├── test_models.py
├── test_chat_nonstream.py
├── test_streaming.py
├── test_auth_errors.py
├── test_validation.py
├── test_multiturn.py       # uses __echo__ to verify composed prompt
├── test_compat_quirks.py   # risks C, D, G, H
├── test_concurrency.py     # risk A, F
└── test_live.py            # @pytest.mark.live only
```

The point of this layer: **the SDK itself is the oracle.** If the response deserialises into
`ChatCompletion` / `ChatCompletionChunk` without the SDK raising, the wire format is right. Hand-rolled
`curl` assertions cannot prove that.

Verified available in this sandbox: `python3 -m venv` + `pip install openai pytest` works
(openai 3.3.1). Node 22 + npm are present if a JS parity smoke is wanted later.

Non-negotiable fixture details:

```python
client = OpenAI(base_url=BASE_URL, api_key=TOKEN, max_retries=0, timeout=30.0)
```

`max_retries=0` — see risk F. Without it, 429 and 5xx tests silently retry and lie.

Assertions to include beyond the obvious:
- `resp.id.startswith("chatcmpl-")` and `len(resp.id) >= 24`
- `resp.object == "chat.completion"`, chunk `object == "chat.completion.chunk"`
- `resp.usage.total_tokens == resp.usage.prompt_tokens + resp.usage.completion_tokens`
- streaming: reassembled `"".join(deltas)` equals the non-streaming answer for the same fixed prompt
- streaming: `chunks[0].choices[0].delta.role == "assistant"` and no later chunk sets `role`
- streaming: exactly one chunk with `finish_reason == "stop"`, and it is the last content-bearing one
- errors surface as the right SDK exception type: `AuthenticationError` (401), `NotFoundError` (404),
  `BadRequestError` (400), `RateLimitError` (429) — and `e.body["error"]["param"] / ["code"]` match
- unicode round-trip: Czech diacritics + emoji in, unmangled out

### L3b — Fake server binary

`remote/cmd/fake-openai-server/main.go` (or a `//go:build faketest` variant). It must construct the
**production** `NewRouter` and `NewOpenAIHandler` and substitute only the container layer with
`scriptedRunner` from L2. Flags/env: `PORT`, `API_TOKEN` (default `test-token`), `OPENAI_MODELS`,
`MAX_CONCURRENT_TASKS`, `TASK_TIMEOUT`, `RATE_LIMIT`.

This is what makes the same Python suite runnable in CI with no Docker, no agent credentials, and
deterministic output. Keep the scripted runner in non-test code only if necessary — preferred is a
small `internal/faketest` package so it never ships in the real server binary.

### L4 — Live smoke against real agents

Same suite, `./run.sh --live --base-url https://<host>/v1 --token $API_TOKEN`, runs only
`@pytest.mark.live`. Agent output is non-deterministic, so assert structure plus weak semantics:

1. `GET /v1/models` returns the configured registry
2. non-streaming `kiro`: prompt `"Reply with exactly the word PONG and nothing else."` → `"PONG" in content`
3. streaming `claude-sonnet`: ≥2 chunks arrive, reassembled text non-empty, stream terminates
4. wrong token → `AuthenticationError`
5. unknown model → `NotFoundError` with `code == "model_not_found"`
6. multi-turn: 3-message conversation where the answer requires turn 1 context

Keep it to ~6 tests. Each real agent run costs credits and takes tens of seconds. Mark with
`pytest.mark.timeout(300)`.

### L5 — Interop, fuzz, load

- `remote/testing/openai-smoke.sh` — pure `curl` + `jq`, zero dependencies, for production probes
  and post-deploy checks. Exits non-zero on the first failed assertion.
- Point a **real third-party client** at the server once: Cline or Continue with a custom OpenAI
  base URL, or `llm -m ... --options`. This is where risks C and D show up for real.
- `FuzzHandleChatCompletions` (Go native fuzzing) over the request body — target is *no panic and no
  5xx on malformed input*; the recovery middleware turning a panic into a 500 still counts as a bug.
- `k6` or `hey`: 20 concurrent requests against `MAX_CONCURRENT_TASKS=2`, `RATE_LIMIT` raised.
  Expect a clean mix of 200 and 429, no hangs, no leaked slots afterwards.

---

## 4. Traceability

Every test carries a comment naming the acceptance criteria it covers (`// Req 2.3`, `# Req 9.4`).
Produce `remote/testing/openai-conformance/TRACEABILITY.md` mapping all ~70 criteria → test names.
Any criterion without a test is either covered before finishing or listed explicitly as untested
with a reason.

---

## 5. Execution protocol

1. Verify the §0 gate.
2. Read the implementation that landed (tasks 2–12) before writing tests — test the code that
   exists, not the code the spec imagined. Where they diverge, the spec wins and it is a bug; note
   it, do not silently test the buggy behaviour as correct.
3. Build layers in order L1 → L2 → L3b → L3 → L4. Each layer green before starting the next.
4. **Iterate to green.** On failure: diagnose, decide whether the bug is in the implementation or in
   the test, fix, rerun. Repeat until the whole suite passes. Do not mark work complete with known
   failures.
5. Exceptions requiring a judgement call — risks C, D, G, H — are decisions, not automatic bugs. If
   the fix is out of scope, keep the test, mark it `xfail` with an explanatory reason, and report it
   in the summary. Never delete a test to make the suite green.
6. Full verification run before declaring done:

```bash
cd /workspace/remote
go build ./... && go vet ./... && go test ./... -race
cd testing/openai-conformance && ./run.sh
```

7. Report: what passes, what was fixed, what remains, and the traceability gaps.

---

## 6. Constraints

- **Do not modify** anything under `orchestrator/`, `tools/`, `runtime/`, `pipelines/`, `dashboard/`
  — this is `remote/` work only. Respect the dependency rules in `CLAUDE.md`.
- **Do not reformat or refactor** the other agent's implementation code. Fixes go in as minimal,
  targeted edits with a reason.
- **Do not weaken existing tests** to accommodate new code. Requirement 12 says the existing suite
  must pass unchanged; if it does not, that is the first bug to fix.
- Test artifacts live in `remote/testing/` and `remote/api/*_test.go`. Do not scatter scratch files
  into the repo root — use `.agents/tmp/`.
- The Python venv (`remote/testing/openai-conformance/.venv/`) must be gitignored.

---

## 7. Definition of done

- [ ] L1–L3 green, run from a clean checkout with a single command
- [ ] L4 green against a real server with real agents (at least once, manually)
- [ ] `go test ./... -race` green in `remote/`, including all pre-existing tests
- [ ] `TRACEABILITY.md` complete; every acceptance criterion mapped or explicitly waived
- [ ] Risks A–H each resolved, or documented as a known limitation with an `xfail` test
- [ ] `remote/API.md` accurate about what the implementation actually does (task 12 may have
      documented intent rather than reality — verify)

---

# Outcome — executed 2026-08-20

Gate opened at commit `8537b55` (12/12 tasks, clean tree). All layers built and run.

## Delivered

| Layer | Location | Result |
|---|---|---|
| L1 unit | `remote/api/openai_{composer,registry,sse,types}_test.go` | green |
| L2 integration | `remote/api/openai_integration_test.go` | green |
| L2 capacity | `remote/api/openai_capacity_test.go` (real `ContainerManager`) | green |
| Fuzz | `remote/api/openai_fuzz_test.go` | green, 52k execs, no crashes |
| L3b fake server | `remote/cmd/fake-openai-server/`, `remote/internal/faketest/` | working |
| L3 SDK conformance | `remote/testing/openai-conformance/` (openai 3.3.1) | 83 passed, 4 xfail, 7 live skipped |
| L5 smoke | `remote/testing/openai-smoke.sh` | 23/23 |
| Traceability | `remote/testing/openai-conformance/TRACEABILITY.md` | complete |

`go build ./... && go vet ./... && go test ./...` green, including all pre-existing tests.
`-race` could not run — no gcc in this sandbox; `-count=2` used instead.

## Bugs found and fixed

1. **Capacity request queued instead of rejecting** (Req 7.2) — at capacity a client waited
   the full task timeout (10 min in production) and then got a 500, not a 429. Added a
   non-blocking `slotReporter` capacity check in the handler.
2. **Panic in a goroutine crashed the whole server** — a nil attach reader made the SSE
   read loop panic outside the HTTP stack, where recovery middleware cannot catch it.
   Guarded in `RunOneShotStreaming` and added a recover in `scanLines`.
3. **Ignored params with the wrong type returned 400** (Req 10.4) — Go's decoder aborts the
   whole body on the first mismatch. All ignored fields are now `json.RawMessage`.
4. **Token estimate counted bytes, not characters** (Req 8.2) — Czech text inflated ~1.4x,
   emoji 4x. Now `utf8.RuneCountInString`.
5. **Req 1.2 size limits unenforced** — 128-message and 256-character caps added.
6. **`OneShotStream.Wait/Close` panicked without a manager** — blocked all streaming test
   doubles. Nil-guarded.

## Follow-up round — client compatibility (same day, at the user's request)

Two of the open decisions were taken up and implemented:

7. **Structured content parts** — `content` now accepts both the plain string and the
   `[{"type": "text", "text": "..."}]` array the OpenAI SDKs and Cline/Continue/LangChain/
   LibreChat emit. Text parts are newline-joined; non-text parts (`image_url`, `input_audio`,
   `file`) are rejected with a message naming the type rather than silently dropped.
8. **The `developer` role** — OpenAI's newer name for `system` is normalised to `system`, so
   those messages become the agent's system prompt instead of a 400.

Both are implemented in `OpenAIMessage.UnmarshalJSON`, so every call site (validation,
composition, streaming) sees a plain string and needed no changes.

A third bug surfaced while doing it: the handler dereferenced a nil `*ContainerResult` when a
runner returned `(nil, nil)`. Guarded.

Final state: Go suite green (`go build`, `go vet`, `go test ./...`), conformance suite
**93 passed / 2 xfail / 7 live-skipped**, smoke 23/23.

## Remaining open decisions — accepted as-is

See TRACEABILITY.md "Open items":

1. Unrouted `/v1/` paths (`/v1/embeddings`) return Go's plain-text mux 404 rather than an
   OpenAI error body. The SDK still raises `NotFoundError`; only the message is unhelpful.
2. `stream_options.include_usage` is accepted but emits no usage chunk.
3. CORS is limited to the dashboard origin, so browser-side SDK use is blocked.
