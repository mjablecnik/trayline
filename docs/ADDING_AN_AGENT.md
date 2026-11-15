# Adding an AI agent to Trayline

Use this guide when adding an agent or extending an existing agent to another Trayline surface. It is the maintained integration checklist for this repository. Update it in the same change whenever integration points move or a new surface is introduced.

The Codex integration exposed an important gap: installing a CLI and adding an executor branch does not make that agent available in YAML pipelines, the API, or chat. Trayline currently has several independent agent lists and protocol branches. There is no single registry that enables every surface.

Paths below are relative to the repository root. `newagent` is a placeholder; replace it with the chosen stable lowercase agent ID. The examples describe a future integration, not an agent already shipped with Trayline.

## 1. Define the integration contract

Before editing, record these decisions in the change description:

| Decision | What to establish |
|----------|-------------------|
| Identity | Stable agent ID, executable/package name, pinned sandbox version |
| Authentication | Host login command, credential files, refresh behavior, supported authentication modes |
| Execution | One-shot and interactive commands; working directory; stdin, stdout, stderr and exit-code behavior |
| Model and effort | Agent-specific mapping for `-m` / YAML `model` and `-t` / YAML `effort`, including unsupported levels and defaults |
| Instructions | How native API `system` and OpenAI system/developer messages reach the agent |
| Isolation | Required mounts and environment; whether the CLI uses its own sandbox or relies on Docker |
| Chat | Persistent process/protocol or exact-session resume; initialization; turn completion; interruption; reconnect |
| Streaming | Text/JSONL framing, assistant message boundaries, errors, tool/reasoning filtering |

Inspect the actual pinned CLI's help and protocol before implementing commands. Do not copy another provider's flags, credentials, or transport assumptions. Provider-specific features, such as Claude's external remote-control service, are separate from Trayline's own remote API.

Track support for all surfaces: local one-shot, local interactive, YAML, local queue, server workflows, native tasks, global chat, project chat, assistant chat, buffered OpenAI completions, OpenAI SSE, and dashboard selection. If one cannot be supported, record the reason and intended behavior explicitly. Do not advertise full support while any remains unimplemented or unverified.

## 2. Map every code integration point

Review every row. Some generic paths need verification rather than a code change; record why an unchanged row is already covered.

### Runtime, installation, and pipelines

| File | Required work or verification |
|------|-------------------------------|
| `runtime/sandbox/Dockerfile` | Install a pinned CLI with a version build argument. Prepare writable agent-owned state directories. Copy any protocol adapter into the image and verify runtime dependencies and permissions for the `agent` user. |
| `runtime/trayline-agent` | Add the accepted ID, help/examples, credential mounts, model/effort translation, one-shot and `-i` commands. Preserve argument boundaries and handle stdin/TTY appropriately. |
| `runtime/trayline` | Update the wrapper's help/examples and verify delegation to `trayline-agent`. |
| `setup/completions/_trayline` | Add the agent to shell completion. Keep LF line endings and no UTF-8 BOM. |
| `setup/install.sh` | Verify the image build context includes new files and that required binaries/scripts, configuration templates, and completions are installed. Existing environment files are preserved. |
| `setup/install-pipelines.sh` | Verify the fast update path installs any changed runtime tools. It does not rebuild Docker images. |
| `setup/uninstall.sh` | Review if the integration adds separately installed artifacts or completion files requiring cleanup. |
| `orchestrator/core/pipeline.go` | Add the ID to `validateStep` and its validation message. An executor branch alone is insufficient. |
| `orchestrator/engine/executor.go` | Verify `RunAgent` via the Docker wrapper and implement `runDirectAgent` for execution inside an existing workflow container (`TRAYLINE_DIRECT_AGENT=1`). Map model and effort in both paths. |
| `pipelines/` | Verify agent steps work with conditions, loops, variables, checkpoints, and composed workflows. Built-in pipelines retain their explicitly configured agents unless deliberately changed. |
| `tools/taskline/` | Verify queued `trayline run` execution. Taskline queues commands and normally needs no provider-specific branch. |

Model and effort behavior must match across the wrapper and direct executor. For example, Codex maps Trayline's `max` effort to `xhigh`; that mapping is specific to Codex. Do not silently discard a requested effort level in one path.

### Remote server and credentials

| File / functions | Required work or verification |
|------------------|-------------------------------|
| `remote/core/config.go` | Add credential path/environment configuration, defaults and local auto-detection if required. |
| `remote/.env.example` | Describe the new configuration and authentication contract; keep the model registry default accurate. |
| `remote/scripts/start-docker.sh` | Resolve host credential paths and forward configuration into the server container. Auto-detection inside a container sees that container's home, not the host user's home. |
| `remote/docker/container.go`: `buildOneShotCmd`, `buildChatCmd` | Construct correct commands and pass model/system instructions. A one-shot CLI reading stdin is not automatically a persistent chat process. |
| `remote/docker/container.go`: `RunOneShotStreaming` | Request the correct structured streaming format and set TTY/demultiplexing behavior. Keep stderr diagnostics out of assistant output. |
| `remote/docker/container.go`: `buildContainerBinds` | Mount credentials for global tasks/chat. |
| `remote/docker/container.go`: `BuildProjectContainerBinds` | Mount credentials for project chat. |
| `remote/docker/container.go`: `BuildAssistantContainerBinds` | Mount credentials for personal assistant chat. |
| `remote/docker/container.go`: `buildWorkflowContainerBinds` | Mount credentials for scheduled workflows, including pipelines with multiple agent types. |
| `remote/docker/container.go`: `StartChatContainer`, `StartProjectChatContainer`, `StartAssistantChatContainer`, `createContainer` | Verify working directory, TTY, open stdin, attach-before-start, and container lifetime. |

Mount the minimum credential material. Establish whether the provider needs writable credentials for refresh; use a private writable copy if needed rather than sharing mutable state between concurrent containers. Avoid importing host temporary symlinks, machine-specific config, caches, or session databases. Codex mounts only `auth.json` read-only; Claude seeds private writable credentials from read-only snapshots. Neither approach is universally correct for every provider.

Verify that mount sources are host paths understood by the Docker daemon. An environment variable in the server container is not itself a credential mount. Keep credentials and prompt content out of command logs and test fixtures.

### Native API and chat protocol

| File | Required work or verification |
|------|-------------------------------|
| `remote/api/task_handler.go` | Allow the ID for native one-shot tasks, including JSON and multipart requests. Update validation messages. |
| `remote/api/session_handler.go` | Allow global chat and route both input and output through the appropriate protocol. |
| `remote/api/project_agent_handler.go` | Update `validateAgent`, structured/plain input dispatch, output parsing, history accumulation, and completion state. |
| `remote/api/assistant_chat_handler.go` | Update `validateAssistantAgent`, input dispatch and output parsing independently of project chat. |
| `remote/store/state.go` and `remote/store/session.go` | Verify session metadata persistence, recovery/reattachment, and any newly introduced per-session state. Do not assume a new protocol needs no recovery changes. |
| `remote/api/workflow_queue.go`, `remote/api/workflow_validation.go`, `remote/api/workflow_handler.go` | Verify scheduling, pipeline validation, log streaming, cancellation and retry reach the updated executor/image. These paths currently schedule pipelines rather than selecting an agent directly. |

For chat, verify the entire turn lifecycle:

1. Authenticate the client and initialize the agent protocol.
2. Send the user message, including uploaded-file metadata and system instructions.
3. Forward assistant-visible output without leaking reasoning, raw tool logs or lifecycle JSON.
4. Emit completion exactly once per turn, including failed turns, so the UI leaves its busy state.
5. Preserve conversation context for the next turn using the exact session/thread ID.
6. Interrupt active work while preserving the chat bridge where supported. Stop child processes on termination.
7. Reconnect to the live session and verify server restart recovery under the existing session contract. Document limitations if a container has been removed; reconnect is not the same as recreating lost provider state.

Choose the transport deliberately. Current paths include Kiro plain TTY text, Claude structured NDJSON, and the Codex adapter in `runtime/sandbox/codex-chat.py`. Codex resumes the exact thread between `exec` turns and translates events into the existing structured chat protocol. Another agent may need a different adapter. If adding one, install it in the sandbox and test it as an actual subprocess.

### OpenAI-compatible API, client, and dashboard

| File | Required work or verification |
|------|-------------------------------|
| `remote/api/openai_registry.go` | Add default public model mapping if the agent should be advertised by default; update related comments. Custom entries are separate from protocol support. |
| `remote/api/openai_handler.go` | Normalize buffered output and implement/select the correct SSE decoder. Do not route every non-Kiro agent to the Claude decoder. Report provider failure, nonzero exit and truncated streams correctly. |
| `remote/api/openai_composer.go` | Verify full conversation composition and system/developer instruction mapping. `/v1/` requests are independent one-shot runs carrying their own message history. |
| `remote/cmd/client/commands.go` | Update client help and supported-agent examples. |
| `remote/cmd/client/completions/_trayline-client` | Update agent completion for both run and chat. |
| `remote/cmd/client/run.go`, `remote/cmd/client/chat.go` | Verify model/system forwarding, output, uploads, interruption, and reconnect. These currently forward agent strings without their own provider allowlist. |
| `dashboard/src/lib/components/AgentSelector.svelte` | Add project-chat selection. |
| `dashboard/src/routes/assistant/+page.svelte` | Add assistant-chat selection independently. |
| `dashboard/src/lib/stores/agent.ts`, `dashboard/src/lib/stores/assistant.ts` | Update provider-specific defaults/types. Clear incompatible model IDs on provider change and preserve edits when reselecting the same provider. |
| `dashboard/src/lib/api.ts`, session views and stores | Review shared types, display labels, restored-session behavior and any new protocol fields. |

A registry entry makes a model discoverable; it does not implement output parsing. Every advertised model must accept both buffered and streamed requests. Explain message-level versus token-level streaming rather than promising a transport the CLI does not provide.

## 3. Add regression coverage

Test observable behavior and provider boundaries. Do not stop at checking that an agent name appears in a list.

| Coverage | Existing locations / examples |
|----------|------------------------------|
| YAML parsing and validation | `orchestrator/core/pipeline_test.go`; `TestParsePipelineCodex` |
| Direct executor, model/effort and safe prompt arguments | `orchestrator/engine/executor_test.go`; `TestDirectCodexPassesModelEffortAndSafePrompt` |
| Docker wrapper invocation | Exercise `runtime/trayline-agent` with a fake `docker` executable; verify both modes, credential mounts and argument boundaries. |
| Commands, TTY and all credential mount paths | `remote/docker/container_test.go`; `TestCodexContainerCommands`, `TestCodexCredentialsMountedAsReadOnlyAuthFile` |
| Native tasks and all chat validators | `remote/api/task_handler_test.go`, `session_handler_test.go`, `project_agent_handler_test.go`, `assistant_chat_handler_test.go`, `codex_test.go` |
| Buffered and SSE OpenAI behavior | `remote/api/openai_integration_test.go`, `openai_registry_test.go`, `openai_handler_test.go`, `codex_test.go` |
| Fake streamed provider framing | Update both `remote/internal/faketest/runner.go` and the independent `scriptedRunner` in `remote/api/openai_integration_test.go` if exercising the new agent through those harnesses. |
| Real subprocess chat contract | `runtime/sandbox/test_codex_chat.py`: initialization, quoted/multiline prompts, exact-thread resume, private-output filtering, failed-turn recovery and interruption |
| Dashboard provider/model changes | `dashboard/src/lib/stores/agent-selection.test.ts` and existing store tests |
| SDK-facing behavior | `remote/testing/openai-conformance/`; keep its README and traceability map current when adding coverage. |

When adding a supported ID, update property-test generators that exclude valid agents from invalid-input tests. Update default-model count assertions and mappings deliberately; do not weaken them just to make tests pass.

Run the relevant checks from the repository root:

```bash
(cd orchestrator && go test ./...)
(cd remote && go test ./...)
(cd dashboard && npm run check && npm run test && npm run build)
```

Run formatting/lint for changed frontend files (or `npm run lint` from `dashboard/`). Validate changed shell scripts with `bash -n`. Run adapter-specific tests; the current Codex example is:

```bash
python -B runtime/sandbox/test_codex_chat.py
```

Use a fake executable or fake runner for deterministic protocol/error tests. These do not prove the real CLI's login, installed package, daemon behavior, sandbox flags or image permissions work. Keep a separate live smoke test and record its result or the specific reason it could not run.

## 4. Verify the installed system

Create a disposable project and a local `newagent-smoke.yaml`:

```yaml
steps:
  - name: smoke
    agent: newagent
    effort: high
    prompt: Read this project and report its name. Do not modify files.
```

First preview parsing, then run it with lifecycle hooks disabled so the default sync hooks cannot modify the disposable project or use other configured agents:

```bash
trayline run ./newagent-smoke.yaml --dry-run --no-lifecycle
trayline run ./newagent-smoke.yaml --no-lifecycle --verbose
```

Preview does not execute the CLI or authenticate it. Also verify the actual installed surfaces:

- Local one-shot and `trayline agent newagent -i`.
- YAML through the wrapper and directly inside a server workflow container.
- Local taskline queue and remote workflow queue, including logs and cancel/retry.
- Native `/run`, file upload, and global/project/assistant chat with two consecutive messages.
- Chat interrupt, reconnect, completion/busy state, and existing server recovery behavior.
- `/v1/models`, buffered `/v1/chat/completions`, SSE, a provider failure, and a custom model mapping.
- Dashboard project and assistant selection with model changes and a restored session.
- Two concurrent containers without credential corruption or cross-session conversation state.

Use an authenticated real provider for the live check and record the pinned CLI/image version used. Real requests can consume provider credits; use small prompts. Describe untested surfaces explicitly in the final change report.

## 5. Update documentation and deploy artifacts

Update the relevant current documentation in the same change:

- `README.md`: supported agents, host login, usage, sandbox version table, and upgrade instructions.
- `CLAUDE.md`: provider/effort behavior and execution/authentication guidance.
- `orchestrator/README.md` and `pipelines/PIPELINES.md`: accepted YAML agents and model/effort mappings.
- `remote/README.md`, `remote/API.md`, and `remote/.env.example`: native API, chat, credentials and public model mappings.
- `remote/testing/openai-conformance/README.md` and `TRACEABILITY.md` when test coverage changes.
- `.agents/AI_LOG.md` for the change record; `.agents/MEMORY.md` for non-obvious reusable findings.
- This guide if the integration introduces another entry point or a new kind of adapter.

Preserve historical specs and verification reports unless the task explicitly updates them; they describe earlier requirements/results. Put the current support contract in maintained documentation.

Run `./setup/install.sh` to rebuild sandbox/server images and install the updated tools, pipelines and completions. Restart the remote server using `bash remote/scripts/start-docker.sh` from the repository root. Rebuild/deploy the dashboard if it is hosted separately. `--skip-docker` and `install-pipelines.sh` cannot install a CLI or adapter into an old image.

The installer preserves existing `~/.trayline/env/` files. Document any new variables and append `newagent:newagent:` to an existing explicit `OPENAI_MODELS` list if the agent should be exposed. A changed default does not override an operator's custom list. Restart Zsh after completion updates. Commit the implementation, tests and documentation together, and distinguish a local commit from a push.

## Completion checklist

Copy this into the change description. Check a row only after verification, or annotate it with a concrete reason it does not apply and any remaining limitation.

- [ ] Stable ID, pinned CLI version, authentication and protocol contract recorded.
- [ ] Sandbox installation, adapter packaging, non-root permissions and working directory verified.
- [ ] CLI help/completions and both one-shot/interactive execution covered.
- [ ] YAML validator **and** wrapper/direct executors support the agent with matching model/effort behavior.
- [ ] Credentials covered for global, project, assistant **and workflow** containers; host paths and concurrent isolation verified.
- [ ] Native task, global chat, project chat and assistant chat validators and protocol dispatch updated.
- [ ] Multi-turn context, uploads, system instructions, errors, completion, interrupt, reconnect and recovery checked.
- [ ] OpenAI model registry, buffered output and SSE parsing covered; private events filtered and failures surfaced.
- [ ] Client help/completions, dashboard selectors and provider-specific model defaults updated.
- [ ] Property generators, default counts, both fake stream harnesses and relevant regression tests reviewed.
- [ ] Required tests, shell checks, frontend checks/build and formatting/lint passed.
- [ ] Live installed-system checks recorded separately from fake tests; limitations named.
- [ ] Maintained documentation and this integration map updated; upgrade steps include images, binaries, server, dashboard and existing environment files.
- [ ] Final diff reviewed and committed; commit/push status and any remaining unverified behavior reported accurately.

## Finding new integration points

This checklist is a map of the current layout, not a substitute for searching before each integration. From the repository root:

```bash
rg -n 'codex|claude|kiro|cline' runtime orchestrator remote dashboard/src setup pipelines
rg -n 'agent|Agent|model|Model' remote/api dashboard/src/lib/stores
rg --files --hidden -g AGENTS.md -g '*.md' -g '!node_modules' -g '!.git'
```

Review each match by purpose. Do not globally replace another agent's name: command flags, protocol decoders, credentials and provider defaults have different semantics. New integration points discovered during the change belong in the map above.
