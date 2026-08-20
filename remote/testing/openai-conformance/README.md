# OpenAI API conformance suite

Verifies that `/v1/chat/completions`, `/v1/models` and `/v1/models/{id}` behave
the way the OpenAI SDK expects — using the **real OpenAI Python SDK as the
oracle**. If a response deserialises into the SDK's own `ChatCompletion` /
`ChatCompletionChunk` models and errors raise the right exception type, the
wire contract is right by construction rather than by hand-written assertion.

## Running

```bash
./run.sh                      # everything, against the built-in fake server
./run.sh -k streaming         # extra args go straight to pytest
./run.sh -v --tb=short        # ditto
```

The first run creates `.venv/` and installs `openai`, `pytest` and `httpx`.
Go must be on PATH — the suite builds `cmd/fake-openai-server` itself.

Against a real deployment with real agents:

```bash
./run.sh --live --base-url https://your-host --token "$API_TOKEN"
```

For a zero-dependency check (production probes, post-deploy hooks) use
`../openai-smoke.sh` instead — curl and jq only.

## The two modes

| | fake (default) | live (`--base-url`) |
|---|---|---|
| Agent | scripted, in-process | real kiro / claude containers |
| Speed | ~50 s | minutes |
| Cost | none | agent credits |
| Determinism | total | none — assertions are structural |
| Tests run | everything except `@pytest.mark.live` | only `@pytest.mark.live` |

`cmd/fake-openai-server` runs the **production** router, middleware chain,
handler, registry, composer and SSE writer. Only the container execution layer
is replaced (`internal/faketest`). So a bug anywhere above the agent boundary is
caught in fake mode; live mode exists to prove the agent boundary itself works.

## Driving the fake agent

Tests provoke server states a real agent cannot be asked for reliably by putting
a marker in the user message:

| Marker | Fake agent behaviour |
|---|---|
| `__echo__` | returns the composed `(system, prompt)` as JSON — the only way to verify Req 11 as a black box |
| `__empty__` | produces no output |
| `__fail__` | exits non-zero |
| `__error__` | fails before the container starts |
| `__hang__` | blocks until the context is cancelled (timeout / capacity tests) |
| `__crash__` | streams two chunks, then dies mid-stream |
| `__slow__` | streams five chunks, honouring `CHUNK_DELAY` |
| `__ansi__` | emits ANSI escape sequences |
| `__big__` | emits ~1 MB of output |
| `__utf8__` | emits multi-byte text |

Tests using these carry `@pytest.mark.fake_only` and are skipped in live mode.

## Fixtures

- `client` — SDK client for the shared server, **`max_retries=0`**. Never remove
  that: the SDK retries 429 and 5xx by default, which would make "rejected
  immediately" and "queued for ten minutes" look identical.
- `raw` — httpx client for assertions the SDK normalises away (SSE framing,
  headers, unknown request fields).
- `spawn_server(**env)` — starts an isolated fake server with a custom
  configuration (`MAX_CONCURRENT_TASKS`, `TASK_TIMEOUT`, `RATE_LIMIT`,
  `OPENAI_MODELS`, `CHUNK_DELAY`).

## Files

| File | Covers |
|---|---|
| `test_models.py` | Req 3, 4.4 — listing, retrieval, custom and empty registries |
| `test_chat_nonstream.py` | Req 1, 8, 10 — response shape, usage, ignored params |
| `test_streaming.py` | Req 2 — chunk shape, ordering, incremental flush, crash recovery |
| `test_auth_validation.py` | Req 5, 6, 9 — auth, error format, the validation matrix |
| `test_multiturn.py` | Req 11 — prompt composition, via `__echo__` |
| `test_concurrency.py` | Req 7, 6.3 — capacity, slot release, rate limiting |
| `test_client_compat.py` | Content-parts arrays and the `developer` role — what real clients actually send |
| `test_compat_quirks.py` | Remaining behaviours the spec does not define |
| `test_live.py` | Smoke tests needing a real agent |

`test_compat_quirks.py` pins behaviours the specification leaves undefined. Its
remaining `xfail` marks an accepted limitation (unrouted `/v1/` paths return a
plain-text 404); an xfail that starts passing shows up as XPASS, so a change
there is a visible event rather than silent drift. See `TRACEABILITY.md`.

## Layers below this one

This suite is the outermost layer. Underneath, in `remote/api/`:

- `openai_composer_test.go`, `openai_registry_test.go`, `openai_sse_test.go`,
  `openai_types_test.go` — unit tests
- `openai_integration_test.go` — the same request paths through the real router
  with a Go test double, no subprocess
- `openai_capacity_test.go` — slot accounting against the real `ContainerManager`
- `openai_fuzz_test.go` — `go test ./api/ -fuzz FuzzHandleChatCompletions`

Run those with `cd remote && go test ./... -race`.
