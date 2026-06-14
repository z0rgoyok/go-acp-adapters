# PLAN-005: ACP compatibility smoke and local usage

## Связанный PRD

Источник: `docs/acp-prd.md`.

Этап поставки: `Этап 5: Compatibility Smoke`.

Зависит от `PLAN-004`, потому что compatibility smoke должен покрывать полный
baseline lifecycle: `initialize`, `session/new`, `session/prompt`,
`session/cancel`, `session/close` и shutdown cleanup.

## Scope

Этот план покрывает финальную приемку первого ACP-среза как продукта, а не
только набора unit tests.

В границах этапа:

- real ACP client handshake через stdio;
- real Claude prompt через ACP stdio;
- cancellation smoke against long-running prompt;
- signal shutdown smoke;
- проверка stdout-only JSON-RPC и stderr diagnostics;
- сохранение developer smoke path через `query` subcommand;
- документация local usage.

## Current State

Сейчас local smoke path уже существует как основной CLI режим:

```text
claude-acp-adapter -cwd /tmp -timeout 45s -prompt "Reply with OK"
```

PRD требует изменить целевую shape:

```text
claude-acp-adapter
```

Этот запуск должен быть ACP stdio server. Direct prompt smoke должен остаться,
но перейти в явную подкоманду:

```text
claude-acp-adapter query -cwd /tmp -timeout 45s -prompt "Reply with OK"
```

После `PLAN-001`...`PLAN-004` ожидается, что adapter уже умеет baseline ACP
lifecycle. Этот этап не добавляет новый protocol behavior, а проверяет, что
поведение совместимо с реальным клиентским запуском и понятно локальному
разработчику.

## Compatibility Matrix

Минимальная matrix для первого среза:

- process mode: default launch starts ACP stdio server;
- legacy developer check: `query` subcommand executes direct Claude transport
  prompt;
- protocol: ACP v1 `initialize` succeeds with valid capabilities;
- session: absolute `cwd` accepted, relative `cwd` rejected;
- MCP: stdio `mcpServers` forwarded through `--mcp-config`;
- MCP unsupported: `http`/`sse` fail fast unless explicitly advertised later;
- prompt: text prompt returns final `session/update` and valid
  `PromptResponse.stopReason`;
- cancellation: `session/cancel` interrupts active prompt and sends no response
  to the notification itself;
- close: `session/close` removes session and disconnects transport;
- output discipline: ACP mode stdout contains only JSON-RPC messages;
- diagnostics: setup, transport and cleanup diagnostics go to stderr.

## Local Usage

Documentation should explain two operator stories.

ACP client story:

- install/build the binary;
- configure editor/agent UI to launch `claude-acp-adapter` as subprocess;
- ensure `claude`, `tmux` and local Claude auth are available;
- expect stdout to be reserved for ACP JSON-RPC;
- read diagnostics from stderr/log capture.

Developer transport smoke story:

- run `claude-acp-adapter query -cwd /tmp -timeout 45s -prompt "Reply with OK"`;
- understand that rate-limit text from transcript can be a valid
  transport-level result;
- use this path for debugging Claude/tmux/transcript independent of ACP client
  integration.

Documentation must not ask users to inspect internal files for normal usage.
Internal paths are useful only for debugging notes.

## Smoke Scenarios

Automated smoke candidates:

- in-process ACP client starts binary, sends `initialize`, asserts ACP v1
  response;
- fake transport end-to-end run covers `initialize -> session/new ->
  session/prompt -> session/close` without real Claude auth;
- blocked fake transport receives `session/cancel` and pending prompt returns
  `cancelled`;
- stdout capture validates every line is JSON-RPC in ACP mode.

Manual smoke candidates:

- real Claude prompt through ACP stdio;
- real cancellation against a long-running prompt;
- signal shutdown leaves no tmux sessions or FIFO files;
- `query` subcommand still runs direct transport smoke.

Manual smoke must be documented separately from mandatory automated checks,
because it depends on local Claude Code availability, auth and subscription
state.

## Design

Compatibility smoke should sit at process boundary. It should test the adapter
as a subprocess where possible, because stdout/stderr discipline and signal
handling are properties of the whole process, not just Go functions.

The test suite should use fake Claude transport for deterministic CI-like
coverage. Real Claude smoke remains manual or opt-in because it depends on
local environment and may return rate-limit or subscription messages that are
valid transport-level outcomes.

Documentation belongs in `docs/` and should reference PRD/PLAN language without
claiming broader ACP capabilities than the first slice supports.

## Implementation Steps

1. Preserve default `claude-acp-adapter` as ACP stdio server mode.
2. Move direct prompt smoke to `query` subcommand while keeping flags familiar.
3. Add process-boundary compatibility tests with fake transport where the code
   structure allows dependency injection.
4. Add stdout/stderr capture tests for ACP mode.
5. Add opt-in or documented manual smoke for real Claude prompt through ACP.
6. Add cancellation and signal shutdown smoke instructions.
7. Document local usage for ACP clients and developers.
8. Re-run full verification suite and capture manual smoke notes when local
   environment supports it.

## Verification

Документальная проверка для этого planning change:

- `git diff --check -- docs`
- `mlint` только если в измененных markdown есть Mermaid diagrams.

Проверка будущей реализации этапа:

- `go test ./...`
- `go test -race ./...`
- `go vet ./...`
- subprocess test for ACP stdio `initialize`;
- end-to-end fake transport test for baseline lifecycle;
- stdout-only JSON-RPC assertion for ACP mode;
- manual `query` smoke with real Claude transport;
- manual real ACP prompt smoke when Claude auth/subscription permits;
- manual signal shutdown check for no leaked tmux sessions or FIFO files.

## Risks

- Real Claude smoke can fail because of auth, subscription or rate limits even
  when adapter behavior is correct; acceptance evidence must distinguish
  environment failure from adapter failure.
- If `query` compatibility is not preserved, developers lose the fastest way to
  debug transport regressions.
- Process-boundary tests may be flaky if they depend on real timing instead of
  fake transport synchronization.
- Documentation can overpromise if it lists future ACP capabilities next to
  first-slice capabilities without clear labels.

## Non-goals

- Не добавлять новые ACP capabilities вне первого среза.
- Не превращать real Claude smoke в mandatory unit test.
- Не реализовывать persistent sessions, auth flows, rich tool visualization или
  embedded binary resources.
- Не менять Claude transport behavior ради smoke, если проблема находится в
  protocol/process wiring.
