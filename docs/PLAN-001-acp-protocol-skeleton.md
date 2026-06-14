# PLAN-001: ACP protocol skeleton

## Связанный PRD

Источник: `docs/acp-prd.md`.

Этап поставки: `Этап 1: Protocol Skeleton`.

## Scope

Этот план покрывает первый минимальный ACP protocol skeleton, который делает
`claude-acp-adapter` узнаваемым ACP v1 agent поверх stdio.

В границах этапа:

- dependency spike для ACP Go SDK candidate;
- решение между `github.com/coder/acp-go-sdk` и narrow protocol package из
  official ACP schema;
- новый protocol boundary для `internal/acp`;
- JSON-RPC loop поверх stdin/stdout;
- `initialize` request/response;
- capability negotiation для первого среза;
- гарантия, что stdout в ACP mode содержит только JSON-RPC messages;
- fake Claude transport boundary для тестов protocol behavior без real Claude
  CLI.

## Current State

Сейчас бинарь работает как developer smoke command: принимает flags `-cwd`,
`-timeout`, `-prompt`, запускает `internal/claude.Client`, печатает assistant
text в stdout и diagnostics в stderr.

В проекте уже есть рабочий Claude transport slice:

- `internal/claude.Client` управляет connect/query/disconnect;
- `internal/claude.TmuxSession` запускает интерактивный Claude CLI через tmux;
- transcript parsing возвращает assistant text и stop reason из JSONL;
- cleanup умеет закрывать tmux session и FIFO resources.

Пока отсутствуют:

- ACP protocol package;
- JSON-RPC dispatcher;
- ACP wire types;
- separation между protocol, app orchestration и Claude transport;
- режим запуска `claude-acp-adapter` как ACP stdio server.

## Dependency Decision

Перед реализацией этапа нужен короткий spike по `github.com/coder/acp-go-sdk`.
Цель spike не в том, чтобы написать adapter целиком, а в том, чтобы снизить риск
неправильного protocol contract до начала downstream work.

Критерии выбора SDK:

- пакет покрывает ACP v1 schema, включая `initialize`, `session/new`,
  `session/prompt`, `session/cancel` и `session/update`;
- есть понятный stdio JSON-RPC connection или достаточно узкие primitives для
  его сборки без лишнего runtime surface;
- notification semantics не заставляют отвечать на `session/cancel`;
- license совместима с проектом;
- dependency не тянет тяжелые server/runtime зависимости, которые не нужны для
  локального subprocess adapter.

Если SDK не проходит эти критерии, fallback — narrow `internal/acp` package из
official ACP JSON schema. В fallback запрещены ad hoc `map[string]any` как
основной contract: wire types должны быть явными, тестируемыми и привязанными к
schema.

Результат spike должен быть зафиксирован в реализации этапа через минимальный
кодовый выбор: либо dependency в `go.mod`, либо локальные generated/handwritten
types в `internal/acp`.

## Design

Protocol skeleton должен быть самостоятельным слоем над будущей application
orchestration.

Граница ответственности:

- `cmd/claude-acp-adapter` выбирает режим процесса и соединяет stdin/stdout со
  server loop;
- `internal/acp` читает JSON-RPC, валидирует shape, dispatches ACP methods и
  пишет JSON-RPC responses/notifications;
- `internal/app` пока представлен минимальным fake boundary для тестов, чтобы
  protocol layer не импортировал `internal/claude` напрямую;
- `internal/claude` не импортирует ACP types.

Минимальный `initialize` response:

- принимает ACP v1 client protocol version;
- отвергает unsupported protocol version стабильной JSON-RPC error response;
- возвращает `agentInfo` с именем adapter и версией;
- возвращает capabilities только для первого среза;
- возвращает пустой `authMethods`;
- сохраняет или pass-through `_meta` только там, где ACP contract этого требует.

stdout/stderr правило является частью protocol design. Любая human-readable
diagnostic line в ACP mode должна идти в stderr, иначе ACP client увидит
поврежденный JSON-RPC stream.

## Implementation Steps

1. Провести dependency spike по `github.com/coder/acp-go-sdk` и применить
   dependency decision из этого плана.
2. Добавить `internal/acp` с минимальными JSON-RPC request, response, error и
   notification primitives.
3. Добавить method dispatcher для `initialize` и стабильную ошибку для
   unknown/unsupported methods.
4. Добавить server loop, который читает newline-delimited JSON-RPC messages из
   stdin и пишет responses в stdout.
5. Обновить `cmd/claude-acp-adapter`, чтобы запуск без `query` становился ACP
   stdio mode, а текущий direct prompt smoke позже мог быть сохранен как
   подкоманда `query`.
6. Ввести узкий application-facing interface для protocol tests, backed by fake
   implementation.
7. Добавить logging/diagnostics writer, явно направленный в stderr.
8. Покрыть `initialize`, unsupported protocol version, invalid JSON, unknown
   method и stdout/stderr separation тестами.

## Verification

Документальная проверка для этого planning change:

- `git diff --check -- docs`
- `mlint` только если в измененных markdown есть Mermaid diagrams.

Проверка будущей реализации этапа:

- `go test ./...`
- `go test -race ./...`
- `go vet ./...`
- unit tests для JSON-RPC request dispatch и `initialize` capability negotiation;
- integration test с in-memory stdin/stdout, который подтверждает, что stdout
  содержит только JSON-RPC messages;
- negative tests для unsupported protocol version и malformed request.

## Risks

- ACP SDK может оказаться несовместимым со schema или notification semantics;
  тогда fallback должен быть принят сразу, не после частичной интеграции.
- Раннее связывание ACP handlers с `internal/claude` усложнит следующие этапы;
  protocol skeleton должен зависеть от app boundary, а не от tmux/transcript.
- Перенос текущего CLI поведения может случайно оставить plain assistant text в
  stdout ACP mode; это критичный protocol break.
- Слишком широкие advertised capabilities создадут ожидания, которые первый
  срез еще не выполняет.

## Non-goals

- Не реализовывать `session/new`, `session/prompt`, `session/cancel` или
  `session/close` в этом этапе.
- Не запускать real Claude CLI из ACP protocol tests.
- Не добавлять rich streaming, tools, images, audio, binary resources или auth
  flows.
- Не менять внутреннюю механику tmux/transcript ради protocol skeleton.
