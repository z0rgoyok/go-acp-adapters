# PLAN-004: Cancellation, session close, and shutdown cleanup

## Связанный PRD

Источник: `docs/acp-prd.md`.

Этап поставки: `Этап 4: Cancellation And Cleanup`.

Зависит от `PLAN-003`, потому что cancellation и close должны завершать
описанный active prompt lifecycle.

## Scope

Этот план покрывает управляемое завершение активной работы и ресурсов adapter.

В границах этапа:

- exported cancellation boundary в `internal/claude`;
- notification-only `session/cancel`;
- idempotent cancellation state;
- завершение pending `session/prompt` со stop reason `cancelled`;
- advertised `session/close`;
- cleanup Claude transport, tmux session, FIFO resources, MCP config files и
  session registry;
- signal-aware ACP server shutdown.

`session/close` считается baseline scope этого этапа, потому что критерии
приемки PRD требуют, чтобы закрытая session больше не была addressable и
underlying transport был отключен.

## Current State

`internal/claude.TmuxSession.Interrupt` уже умеет отправлять `C-c`. Сейчас это
используется внутри `Client.Query` только на timeout/error path через приватный
`interruptTurn`.

`internal/claude.Client.Disconnect` уже чистит:

- tmux session;
- active session registry внутри transport cleanup;
- Stop reader;
- FIFO path.

Сейчас отсутствуют:

- public cancellation method на Claude transport boundary;
- ACP `session/cancel` notification handling;
- idempotent app-level cancellation state;
- `session/close` handler;
- shutdown cleanup всех ACP sessions;
- связь close/shutdown с временными MCP config files из `PLAN-002`.

## Cancellation Boundary

Application layer не должен знать про tmux. Поэтому `internal/claude` должен
предоставить узкий exported method, например `Cancel` или `Interrupt`, который
означает business action: попросить активный Claude turn остановиться.

Boundary expectations:

- method принимает `context.Context`;
- method не раскрывает tmux session name;
- method возвращает error, если interrupt не удалось отправить;
- method безопасен для повторного вызова;
- method может быть вызван while `Query` is running.

Если текущий `Client.Query` удерживает mutex на весь turn и блокирует внешний
cancel call, реализация должна сузить lock scope или добавить отдельную
concurrency-safe cancellation path. Это ключевой технический риск этапа.

## Session Close

`session/close` должен быть advertised только после реализации handler и cleanup
semantics.

Close behavior:

- unknown session -> stable JSON-RPC error;
- active turn exists -> cancel active turn first;
- pending `session/prompt` завершается со stop reason `cancelled`;
- Claude transport disconnects;
- FIFO, tmux session и временные MCP config files удаляются;
- session удаляется из registry;
- повторный close для той же session возвращает unknown session или стабильный
  already-closed behavior, выбранный в protocol mapping.

Close является request/response method, в отличие от `session/cancel`, который
является notification.

## Shutdown Cleanup

ACP stdio server должен обрабатывать process context cancellation и OS signals.

Shutdown behavior:

- прекращает прием новых requests;
- отменяет active turns;
- disconnects all Claude transport clients;
- удаляет FIFO и temporary MCP config files;
- завершает server loop без записи non-JSON diagnostics в stdout;
- пишет shutdown diagnostics в stderr.

Cleanup должен быть best-effort, но observable: ошибки cleanup логируются в
stderr с ACP session ID и Claude session ID, если они известны.

## Design

Cancellation state принадлежит session record, а не protocol handler.

Для `session/cancel`:

- protocol layer декодирует notification и не создает JSON-RPC response;
- app layer ищет session и active turn;
- если active turn отсутствует, operation остается idempotent no-op;
- если active turn есть, app layer marks it cancelling, вызывает transport
  cancellation boundary и cancel function;
- исходный `session/prompt` завершает свой response path.

Для `session/close` и shutdown нужен общий cleanup use case, чтобы ручной close
и process shutdown не расходились по ресурсным гарантиям.

## Implementation Steps

1. Добавить exported cancellation method в `internal/claude.Client` и/или
   transport interface.
2. Проверить mutex behavior вокруг `Query` и cancellation; убрать блокировку,
   которая мешает external cancel, если она есть.
3. Расширить app-level active turn state cancellation flags и idempotent no-op
   behavior.
4. Добавить ACP notification handler для `session/cancel` без JSON-RPC response.
5. Связать cancellation с pending `session/prompt` stop reason `cancelled`.
6. Реализовать `session/close` handler и advertised capability.
7. Добавить общий cleanup use case для close and shutdown.
8. Подключить signal-aware shutdown в ACP server mode.
9. Добавить tests на cancel notification semantics, idempotency, close cleanup и
   shutdown cleanup with fake transport.

## Verification

Документальная проверка для этого planning change:

- `git diff --check -- docs`
- `mlint` только если в измененных markdown есть Mermaid diagrams.

Проверка будущей реализации этапа:

- `go test ./...`
- `go test -race ./...`
- `go vet ./...`
- unit tests, что `session/cancel` не пишет JSON-RPC response;
- unit tests для повторного cancel как idempotent operation;
- integration test, где fake transport blocks, cancel arrives, pending prompt
  returns `cancelled`;
- tests для `session/close`: active turn cancellation, registry removal и
  transport disconnect;
- shutdown test, который проверяет cleanup всех active sessions;
- manual smoke для signal shutdown без leaked tmux sessions/FIFO files.

## Risks

- Текущий transport mutex может сделать external cancellation невозможной без
  refactor lock scope.
- `session/cancel` как notification легко случайно реализовать как request с
  response; это нарушит ACP contract.
- Если close и shutdown получат разные cleanup paths, один из них начнет течь
  ресурсами.
- Best-effort cleanup может скрыть реальные проблемы, если diagnostics не будут
  включать session IDs.

## Non-goals

- Не раскрывать tmux details в ACP handlers или app use cases.
- Не реализовывать persistent session storage.
- Не добавлять authentication flows.
- Не расширять prompt streaming beyond final update from `PLAN-003`.
