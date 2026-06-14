# PLAN-003: Prompt turn and assistant updates

## Связанный PRD

Источник: `docs/acp-prd.md`.

Этап поставки: `Этап 3: Prompt Turn`.

Зависит от `PLAN-002`, потому что использует session registry, Claude transport
client и сохраненные session capabilities.

## Scope

Этот план покрывает основную пользовательскую ценность первого ACP-среза:
клиент отправляет prompt в существующую ACP session и получает assistant answer
через ACP.

В границах этапа:

- lookup ACP session по `sessionId`;
- conversion поддержанных ACP `ContentBlock` values в Claude prompt string;
- rejection неподдержанных content blocks до старта Claude turn;
- one-active-turn-per-session enforcement;
- вызов Claude transport `Query`;
- final `session/update` notification с assistant text;
- `PromptResponse.stopReason` mapping;
- error mapping для prompt-level failures.

## Current State

`internal/claude.Client.Query` уже выполняет один prompt turn внутри connected
Claude session:

- ждет готовность интерактивного CLI;
- вставляет prompt через tmux buffer;
- ждет transcript path через Stop hook или transcript discovery;
- читает assistant messages;
- возвращает `Response.Text`, `Messages`, `Duration` и `TranscriptPath`.

Сейчас нет ACP prompt contract:

- нет `session/prompt` handler;
- нет conversion ACP content blocks;
- нет `session/update` notifications;
- нет app-level active turn state;
- stop reason остается transport detail, а не ACP `PromptResponse.stopReason`;
- cancellation пока доступна только как timeout-side interrupt внутри transport.

## Prompt Conversion

Первый срез поддерживает только content, который можно надежно превратить в
plain prompt для Claude Code.

Поддерживаемые inputs:

- text block превращается в plain text без дополнительной разметки;
- resource link превращается в readable reference с URI, title и mime type, если
  они есть;
- несколько blocks объединяются в стабильном порядке request payload.

Неподдержанные inputs:

- image/audio/binary embedded resources;
- tool results без стабильного mapping;
- content kinds, отсутствующие в выбранных ACP types;
- resource payloads, требующие загрузки bytes самим adapter.

Неподдержанный block должен дать protocol error до `ClaudeTransport.Query`,
чтобы не запускать дорогой turn, который заведомо не соответствует user input.

## Turn Orchestration

Application layer управляет active turn state внутри session record.

Turn lifecycle:

- handler валидирует `sessionId` и content;
- app layer атомарно резервирует active turn slot;
- если slot занят, возвращается stable `prompt already running` error;
- app layer создает cancellable context для future `session/cancel`;
- вызывается Claude transport `Query`;
- после ответа отправляется final assistant `session/update` notification;
- исходный `session/prompt` получает `PromptResponse` со stop reason;
- active turn slot освобождается при success, error или cancellation.

Первый срез не обязан отправлять chunks. Он должен отправить минимум один final
`session/update` после готового transport response. Это сохраняет ACP-visible
assistant answer и оставляет rich streaming отдельным улучшением.

## Stop Reason Mapping

Mapping должен жить в application layer, потому что это перевод transport result
в ACP contract.

Базовые правила:

- Claude `end_turn` -> ACP `end_turn`;
- Claude `max_tokens` -> ACP `max_tokens`;
- Claude `stop_sequence` -> ACP `end_turn`;
- context cancellation -> ACP `cancelled`;
- transport timeout -> ACP `cancelled`, если interrupt был отправлен в живую
  Claude session;
- transport timeout -> protocol/transport error, если interrupt не был отправлен
  или живое состояние session не подтверждено;
- unknown stop reason -> ACP `end_turn` только при наличии завершенного
  assistant text, иначе transport error.

Если transport response содержит несколько assistant messages, mapping должен
брать terminal stop reason из последнего завершенного assistant message.

## Design

`session/prompt` должен разделять три типа результата:

- ACP notification `session/update`, где клиент видит assistant content;
- JSON-RPC response на исходный request, где клиент видит final stop reason;
- JSON-RPC error, если turn не был корректно выполнен.

Protocol package не должен знать, как Claude читает transcript. Claude transport
не должен знать, что его response будет превращен в ACP notification. Это
позволяет тестировать prompt mapping через fake transport и fake notifier.

## Implementation Steps

1. Добавить prompt conversion functions в app/protocol mapping boundary.
2. Добавить active turn state в session record: request ID, cancel function,
   start time и статус.
3. Реализовать one-active-turn-per-session guard с освобождением через defer.
4. Добавить ACP handler для `session/prompt`.
5. Подключить Claude transport `Query` через app-level transport interface.
6. Реализовать final assistant `session/update` notification writer.
7. Реализовать stop reason mapping и error mapping по правилам PRD.
8. Добавить tests на success, unsupported content, unknown session, prompt already
   running и transport failure.

## Verification

Документальная проверка для этого planning change:

- `git diff --check -- docs`
- `mlint` только если в измененных markdown есть Mermaid diagrams.

Проверка будущей реализации этапа:

- `go test ./...`
- `go test -race ./...`
- `go vet ./...`
- unit tests для prompt content conversion;
- unit tests для unsupported content errors до fake transport call;
- unit tests для one-active-turn guard;
- integration test `initialize -> session/new -> session/prompt` with fake
  Claude transport;
- tests, которые проверяют порядок: сначала `session/update`, затем
  `PromptResponse` для успешного turn;
- tests для stop reason mapping.

## Risks

- Если final `session/update` и `PromptResponse` будут отправляться в
  неправильном порядке, некоторые ACP clients могут считать turn пустым или
  зависшим.
- Если unsupported content будет частично игнорироваться, пользователь получит
  неполный prompt без явной ошибки.
- Если active turn slot не освобождается на error path, session станет
  навсегда занятой.
- Если stop reason mapping будет жить в transport, `internal/claude` начнет
  зависеть от ACP protocol semantics.

## Non-goals

- Не реализовывать rich streaming chunks в этом этапе.
- Не реализовывать tool updates, image/audio/binary resources или in-process
  tool hosting.
- Не реализовывать `session/cancel` как ACP notification; active turn state
  только подготавливает boundary для `PLAN-004`.
- Не менять transcript parser ради ACP-specific output shape.
