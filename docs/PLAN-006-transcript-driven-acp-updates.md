# PLAN-006: Transcript-driven ACP updates

## Связанный PRD

Источник: `docs/acp-prd.md`.

Этот план уточняет будущий streaming после первого ACP-среза. Главная цель:
выдача adapter должна выглядеть как обычный ACP output. Клиентам достаточно
поддерживать ACP v1 schema; специальных правил под Claude transcript им
добавлять не требуется.

## Контекст

Текущая реализация работает так:

```text
ACP client -> adapter -> tmux -> interactive Claude CLI -> JSONL transcript -> final session/update
```

`tmux` остается control plane: запустить Claude, вставить prompt, отправить
interrupt, завершить session. Данные для ответа берутся из JSONL transcript.
Чтение `tmux` pane не входит в архитектуру.

Сейчас `internal/claude.TranscriptReader` читает только завершенные JSONL lines,
держит byte offset и извлекает только `assistant.message.content[].text`.
`internal/app` после завершения turn отправляет один `session/update` с
`sessionUpdate=agent_message_chunk`.

## Что показал transcript spike

Локальная проверка последних Claude JSONL transcript-файлов показала устойчивую
форму событий:

- top-level `type`: `assistant`, `user`, `system`, `attachment`, `ai-title`,
  `last-prompt`, `mode`, `permission-mode`, `file-history-snapshot`,
  `queue-operation`;
- assistant output лежит в `type=assistant`, `message.role=assistant`;
- assistant text лежит в `message.content[]` blocks с `type=text`;
- Claude tool start лежит в `assistant/message.content[]` blocks
  `type=tool_use`, обычно с `id`, `name`, `input`;
- tool result возвращается как `type=user`, `message.content[]` block
  `type=tool_result`, обычно с `tool_use_id`, `content`, `is_error`;
- assistant stop reason лежит в `message.stop_reason`;
- порядок событий задается порядком строк в JSONL file и byte offset;
- одна assistant turn может состоять из нескольких assistant JSONL records:
  text, tool use, tool result, следующий text, финальный `end_turn`.

Из этого следует важный продуктовый вывод: настоящий runtime stream здесь
равен скорости появления завершенных JSONL records. Adapter может отдавать
обновления раньше финала, когда Claude пишет промежуточные assistant text
records. Если Claude записал только финальный text record, ACP output будет
одним chunk.

## ACP Compatibility Contract

Базовый compatibility profile для первой реализации streaming:

- adapter отправляет наружу только валидные ACP `session/update`
  notifications;
- assistant text из transcript становится ACP `agent_message_chunk`;
- каждый chunk содержит `content.type=text` и исходный text без adapter-specific
  разметки;
- `messageId` стабилен внутри одного Claude assistant message: берем
  `message.id`, при отсутствии берем outer `uuid`;
- порядок `session/update` совпадает с порядком JSONL byte offsets;
- каждый завершенный transcript line обрабатывается один раз;
- финальный response на `session/prompt` содержит ACP `stopReason`;
- stdout остается чистым JSON-RPC stream, diagnostics уходят в stderr.

Инвариант для клиентов: клиент видит обычный ACP v1 output. Он не знает про
Claude JSONL, `tmux`, Stop hook, byte offsets и внутренние Claude tool records.

## Reduced Output Profile

Первый streaming slice сознательно урезан:

- наружу идут `agent_message_chunk` только для assistant text;
- `tool_use` и `tool_result` парсятся во внутренние events и покрываются tests;
- tool events используются для наблюдаемости, stop detection и будущего
  mapping;
- наружный `tool_call`/`tool_call_update` включается отдельным этапом после
  подтверждения полного ACP shape;
- `agent_thought_chunk` не используется, потому что Claude transcript не дает
  стабильный публичный reasoning stream;
- `user_message_chunk` не используется, потому что prompt уже принадлежит
  client side и повторная трансляция user text создаст лишний шум.

Такой профиль дает предсказуемое поведение для ACP clients: обычный chat stream
из text chunks плюс финальный stop reason. Клиенты получают меньше информации,
зато получают protocol-native информацию.

## Future Tool Mapping

После text streaming добавляется второй compatibility profile для tools:

- `assistant/tool_use` -> ACP `tool_call`;
- `user/tool_result` -> ACP `tool_call_update`;
- `toolCallId` берется из Claude `tool_use.id`;
- `title` строится из known tool name и короткой операции;
- `status` строится из пары start/result;
- `rawInput` получает Claude tool input;
- `rawOutput` получает structured result, когда он есть;
- `content` получает user-facing text summary, когда результат безопасен для UI.

Tool mapping добавляется только через ACP union types. Adapter-specific поля
могут жить в `_meta`, если они реально нужны для diagnostics, но UI clients не
должны зависеть от `_meta`.

## Design

### Transcript tailer

`internal/claude` получает отдельную ответственность: tail JSONL transcript как
event stream.

Свойства tailer:

- читает только завершенные newline-delimited JSON records;
- сохраняет byte offset после каждой завершенной строки;
- оставляет partial line для следующего read;
- обрабатывает file growth polling с маленьким interval и backoff;
- возвращает typed events, а не ACP types;
- завершает turn по terminal assistant stop reason, Stop hook signal или
  cancellation context.

### Transcript event model

Внутренние события Claude transport:

- `AssistantText`: text, timestamp, Claude message id, outer uuid, stop reason;
- `AssistantToolUse`: tool use id, name, input, timestamp;
- `ToolResult`: tool use id, content, is error, timestamp;
- `SessionTitle`: title from `ai-title`;
- `Usage`: token usage from assistant message usage;
- `Unknown`: type, keys, offset for diagnostics.

Эти события остаются в `internal/claude`. Они не импортируют ACP package.

### ACP mapper

`internal/app` получает mapper из Claude transport events в ACP updates.

MVP mapping:

- `AssistantText` -> `session/update.agent_message_chunk`;
- terminal `AssistantText.stop_reason` -> `PromptResponse.stopReason`;
- `SessionTitle` -> optional `session_info_update`, если ACP type уже
  реализован;
- `Usage` -> optional `usage_update`, если ACP type уже реализован;
- tool events -> diagnostics/internal state до этапа tool compatibility.

ACP mapper живет выше transport layer, потому что это перевод доменных событий
Claude adapter в protocol contract.

### Transport API

Существующий `Query(ctx, prompt) Response` остается полезным для финального
режима и `query` smoke.

Для streaming добавляется turn API уровня transport:

```text
StartTurn(ctx, prompt) -> event channel + final result
```

Финальный result нужен даже при streaming:

- чтобы вернуть `PromptResponse.stopReason`;
- чтобы сохранить transcript path и duration;
- чтобы закрыть turn корректно при cancellation;
- чтобы сделать final reconciliation по offset и не потерять последние lines.

### ACP type coverage

Текущие локальные ACP types покрывают только simplified
`agent_message_chunk`. Для полноценной совместимости нужно расширить
`internal/acp` до официального union shape:

- `agent_message_chunk` с optional `messageId`;
- `tool_call`;
- `tool_call_update`;
- `session_info_update`;
- `usage_update`;
- `StopReason` values: `end_turn`, `max_tokens`, `max_turn_requests`,
  `refusal`, `cancelled`.

Первый implementation step добавляет только те fields, которые реально
используются text streaming profile: `messageId` для message chunks и строгую
валидацию discriminator values.

## Implementation Steps

1. Добавить sanitized transcript fixtures с реальными shapes:
   `assistant/text`, `assistant/tool_use`, `user/tool_result`, `system`,
   `ai-title`, partial line.
2. Разделить transcript чтение на low-level JSONL tailer и parser typed events.
3. Покрыть tailer tests: offset, partial line, huge line, append после read,
   invalid JSON diagnostics.
4. Добавить `AssistantText` event с `messageId`, `text`, `stopReason`,
   `timestamp`, `offset`.
5. Добавить parsing для `AssistantToolUse` и `ToolResult` как internal events.
6. Расширить local ACP `SessionUpdate` для `messageId` в
   `agent_message_chunk`.
7. Добавить app-level mapper `AssistantText -> agent_message_chunk`.
8. Добавить streaming turn API в transport boundary без протаскивания ACP types
   в `internal/claude`.
9. Подключить `session/prompt` к streaming path: отправлять chunks по мере
   появления events и возвращать final `PromptResponse`.
10. Сохранить final-only fallback: если за turn пришел один text event, клиент
    получает один валидный chunk.
11. Добавить deterministic fake transcript writer tests для нескольких text
    records и tool interleaving.
12. Добавить opt-in/manual smoke через real Claude Code с model `sonnet`.
13. После text profile отдельно спланировать tool compatibility profile.

## Acceptance Criteria

- ACP client получает только валидные `session/update` notifications по ACP v1
  shape.
- Text streaming не требует client-side Claude-specific handling.
- Несколько assistant text records дают несколько `agent_message_chunk` updates
  с тем же `messageId`, если Claude message id совпадает.
- Tool records в transcript не ломают stream и не превращаются в произвольный
  text.
- Stop reason возвращается в response исходного `session/prompt`.
- Cancellation возвращает `cancelled` для pending prompt.
- stdout содержит только JSON-RPC messages.
- stderr diagnostics содержат transcript path, offset и event type без
  раскрытия полного message text.

## Verification

Документальная проверка для этого planning change:

- `git diff --check -- docs/PLAN-006-transcript-driven-acp-updates.md`
- проверить, что markdown fences сбалансированы.
- `mlint`, если в документ добавлены Mermaid diagrams.

Проверка будущей реализации:

- `gofmt -w internal/claude/*.go internal/app/*.go internal/acp/*.go`
- `go test ./...`
- `go test -race ./...`
- `go vet ./...`
- unit tests для transcript fixtures и typed event parsing;
- unit tests для ACP mapper;
- subprocess ACP test, который проверяет порядок:
  `session/update` chunks перед final `session/prompt` response;
- fake transcript writer test с interleaving:
  text -> tool_use -> tool_result -> text -> end_turn;
- manual smoke:
  `go run ./cmd/claude-acp-adapter query -cwd /tmp -model sonnet -timeout 45s -prompt "Reply with exactly one word: OK"`.

## Risks

- Claude transcript format является implementation detail Claude Code, поэтому
  parser должен быть tolerant к неизвестным top-level types и новым fields.
- Если tool records сразу отдать как text, UI clients получат шум вместо
  expected ACP tool UI.
- Если tool records сразу отдать как ACP tools без полного mapping, clients
  начнут зависеть от кривой семантики.
- Если tailer потеряет offset discipline, клиент увидит duplicate chunks.
- Если adapter будет ждать final transcript перед отправкой chunks, streaming
  снова станет final-only.

## Non-goals

- Чтение `tmux` pane как source of truth.
- Использование Claude `--print --output-format stream-json` как основной
  transport.
- Публикация Claude internal transcript schema как часть adapter public API.
- Генерация `agent_thought_chunk` из обычного assistant text.
- Полная визуализация tools в первом streaming slice.
