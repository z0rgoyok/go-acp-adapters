# PLAN-002: Session lifecycle and MCP forwarding

## Связанный PRD

Источник: `docs/acp-prd.md`.

Этап поставки: `Этап 2: Session Lifecycle`.

Зависит от `PLAN-001`, потому что использует выбранные ACP wire types,
JSON-RPC dispatch и error envelope.

## Scope

Этот план покрывает создание управляемой ACP session и подготовку Claude
transport context для будущих prompt turns.

В границах этапа:

- `session/new` request handling;
- validation, что `cwd` является absolute path;
- in-memory session registry;
- хранение client/session capabilities и `additionalDirectories`;
- mapping одной ACP session на один `internal/claude.Client`;
- старт Claude Code через существующий Claude transport;
- forwarding stdio `mcpServers` в Claude CLI через `--mcp-config`;
- fail-fast behavior для unsupported `http` и `sse` MCP transports до
  advertising соответствующих capabilities.

## Current State

`internal/claude.NewClient` уже принимает `WorkingDir`, `Model`, `Timeout`,
`ConfigDir`, `PermissionMode`, `TmuxName` и `ExtraArgs`. `Connect` создает Claude
session, FIFO Stop hook и tmux process. `Disconnect` чистит tmux session, Stop
reader и FIFO path.

Сейчас нет ACP-level session model:

- нет ACP `sessionId`;
- нет registry активных sessions;
- нет хранения client capabilities;
- нет проверки `cwd` по ACP contract;
- нет передачи `mcpServers` в Claude CLI;
- lifecycle завязан на один CLI invocation, а не на несколько protocol requests.

## Session Model

Application layer должен владеть session lifecycle. Protocol layer только
принимает request, вызывает use case и мапит результат в ACP response.

Session record должен содержать:

- ACP `sessionId`;
- absolute `cwd`;
- relevant client capabilities;
- `additionalDirectories` как данные ACP session;
- normalized `mcpServers` для диагностики и future reuse;
- Claude transport client;
- Claude session ID после successful connect;
- active turn state placeholder для `PLAN-003`;
- created/updated timestamps.

Registry behavior:

- создает новую запись только после successful validation;
- не оставляет session record, если Claude transport startup failed;
- возвращает stable unknown-session error для missing `sessionId`;
- поддерживает lookup и delete для следующих этапов;
- serializes mutations внутри registry, чтобы parallel requests не ломали
  lifecycle state.

## MCP Forwarding

ACP `mcpServers` являются частью `session/new`, поэтому этап должен явно решить
mapping в Claude CLI.

Первый compliant slice поддерживает только stdio MCP servers:

- command и args переносятся в Claude MCP config;
- env передается только из явных ACP server settings;
- server name сохраняется стабильным;
- generated config записывается во временный файл, принадлежащий session;
- путь к config передается Claude CLI через `--mcp-config` или подтвержденный
  эквивалентный flag.

Validation spike по Claude CLI должен подтвердить точный формат `--mcp-config`.
Если текущая версия Claude CLI не поддерживает ожидаемый формат, реализация
этапа должна fail fast с понятным setup error, а не молча игнорировать MCP.

`http` и `sse` MCP transports в первом срезе не поддерживаются. До advertising
соответствующих `mcpCapabilities` такие requests должны возвращать protocol
error до запуска Claude transport.

## Design

`session/new` проходит через три слоя:

1. `internal/acp` декодирует request и возвращает JSON-RPC response/error.
2. `internal/app` валидирует бизнес-смысл session, создает registry record и
   управляет Claude transport.
3. `internal/claude` запускает interactive Claude CLI, не зная про ACP types.

MCP config generation принадлежит application/orchestration boundary, потому что
это mapping между ACP session request и Claude CLI arguments. `internal/claude`
может получить готовые `ExtraArgs` или более узкую typed option, но не должен
импортировать ACP protocol types.

Temporary MCP config files должны чиститься вместе с session close/shutdown.
Если close еще не реализован на этом этапе, registry должен иметь внутренний
cleanup method, который будет использован в `PLAN-004`.

## Implementation Steps

1. Добавить application session registry и session record в `internal/app`.
2. Добавить use case `CreateSession` с validation absolute `cwd`, capabilities,
   `additionalDirectories` и `mcpServers`.
3. Добавить ACP handler для `session/new`, который вызывает `CreateSession` и
   возвращает ACP `sessionId`.
4. Добавить narrow Claude transport interface в app layer, backed by
   `internal/claude.Client`, чтобы unit tests использовали fake transport.
5. Реализовать stdio MCP server normalization и generated MCP config file.
6. Передать generated config в Claude CLI через confirmed `--mcp-config`
   mapping.
7. Добавить fail-fast errors для relative `cwd`, unsupported MCP transport,
   invalid MCP server shape и Claude startup failure.
8. Добавить cleanup path при partial failure: если config создан, но Claude
   startup failed, временные файлы и transport resources удаляются.

## Verification

Документальная проверка для этого planning change:

- `git diff --check -- docs`
- `mlint` только если в измененных markdown есть Mermaid diagrams.

Проверка будущей реализации этапа:

- `go test ./...`
- `go test -race ./...`
- `go vet ./...`
- unit tests для absolute `cwd` validation;
- unit tests для registry create/lookup/delete behavior;
- unit tests для unsupported `http` и `sse` MCP transports;
- unit tests для generated stdio MCP config;
- integration test `initialize -> session/new` with fake Claude transport;
- failure test, который подтверждает отсутствие leaked registry record после
  Claude startup error.

## Risks

- Точное поведение Claude CLI `--mcp-config` может отличаться от предположений;
  этап должен включить validation spike до финального mapping.
- Если registry начнет владеть protocol details, дальнейший prompt/cancel flow
  станет сложнее тестировать.
- Если временные MCP config files не будут связаны с session cleanup, close и
  shutdown оставят мусор на диске.
- Сохранение `additionalDirectories` без применения к Claude CLI может быть
  воспринято как полноценная поддержка; документация capability должна быть
  честной.

## Non-goals

- Не реализовывать `session/prompt` и assistant updates в этом этапе.
- Не реализовывать `session/cancel` или `session/close` как public ACP methods.
- Не поддерживать `http` и `sse` MCP transports без отдельного capability-gated
  решения.
- Не переносить tmux/FIFO details в ACP handlers.
