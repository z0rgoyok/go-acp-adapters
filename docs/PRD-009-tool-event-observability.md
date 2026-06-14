# PRD-009: Tool event observability

## Кратко

`claude-acp-adapter` должен отдавать наружу не только текст ассистента, но и
структурные события tool calls.

Сейчас Claude transcript уже содержит:

- `assistant.content[].type = "tool_use"`;
- `user.content[].type = "tool_result"`.

Adapter уже парсит эти записи во внутренние события:

- `claude.AssistantToolUseEvent`;
- `claude.ToolResultEvent`.

Но `internal/app/updates.go` отправляет в ACP client только
`AssistantTextEvent`. Из-за этого Cogerentor log показывает ход рассуждения и
итоговый текст, но теряет самую полезную операционную часть: какой tool был
вызван, с какими аргументами, что вернулось и была ли ошибка.

Этот PRD фиксирует минимально полезный продуктовый контракт для tool visibility
без полной rich UI интеграции.

## Цель

Сделать так, чтобы Cogerentor и другие ACP clients могли видеть tool activity
как структурный поток событий:

```text
assistant text
tool_call Read {"file_path":"..."}
tool_result tool-1 "..."
assistant text
PromptResponse
```

Минимальный успешный сценарий:

```text
Claude transcript tool_use
-> internal claude event
-> app mapping
-> ACP session/update
-> Cogerentor frame/log line
```

## Почему это нужно

Для pipeline runner tool activity является evidence.

Когда stage зависает, падает или выглядит слишком тихо, оператору важно видеть:

- агент реально работает или только пишет текст;
- какие файлы он читает;
- какие команды запускает;
- какой tool вернул ошибку;
- где был последний полезный прогресс перед cancellation/timeout.

Текстовые chunks отвечают на вопрос "что агент сказал". Tool events отвечают на
вопрос "что агент сделал". Для диагностики agent loop второе часто важнее
первого.

## Источники

- Текущий ACP update mapping:
  `internal/app/updates.go`
- Внутренние Claude transcript events:
  `internal/claude/types.go`
- Transcript parser:
  `internal/claude/transcript_events.go`
- Fixture с interleaving:
  `internal/claude/testdata/transcripts/text_tool_interleaving.jsonl`
- Отложенный compatibility scope:
  `docs/PLAN-006-transcript-driven-acp-updates.md`

## Scope

В scope входят пять изменений поведения:

1. Отправлять `AssistantToolUseEvent` как структурный ACP session update.
2. Отправлять `ToolResultEvent` как структурный ACP session update.
3. Добавить настройки детализации tool output.
4. Обеспечить стабильное отображение в Cogerentor log/frame stream.
5. Покрыть mapping unit tests и transcript interleaving tests.

В scope не входят:

- in-process tool hosting;
- выполнение tools внутри adapter-а;
- изменение Claude prompt text;
- замена transcript source;
- полноценный rich UI для diff/image/binary outputs;
- retry policy для failed tools;
- semantic interpretation конкретных tools.

## Требуемое поведение

### Tool call update

Когда transcript содержит assistant block:

```json
{
  "type": "tool_use",
  "id": "tool-1",
  "name": "Read",
  "input": {
    "file_path": "/tmp/a.txt"
  }
}
```

Adapter отправляет session update со структурой, достаточной для клиента:

```json
{
  "sessionUpdate": "tool_call",
  "toolCallId": "tool-1",
  "title": "Read /tmp/a.txt",
  "kind": "Read",
  "status": "started",
  "input": {
    "file_path": "/tmp/a.txt"
  }
}
```

Обязательные поля:

- `sessionUpdate = "tool_call"`;
- `toolCallId`;
- `kind`;
- `status = "started"`;
- `input`.

`title` является display hint. Клиент может использовать его для короткого
человеческого лога.

Минимальное правило title:

- для известных tools title строится из tool name и главного аргумента;
- для неизвестных tools title равен tool name;
- пустой tool name отображается как `tool`.

### Tool result update

Когда transcript содержит user block:

```json
{
  "type": "tool_result",
  "tool_use_id": "tool-1",
  "content": "file contents",
  "is_error": false
}
```

Adapter отправляет session update:

```json
{
  "sessionUpdate": "tool_call_update",
  "toolCallId": "tool-1",
  "status": "completed",
  "isError": false,
  "content": "file contents"
}
```

Если `is_error = true`, status становится `failed`.

Обязательные поля:

- `sessionUpdate = "tool_call_update"`;
- `toolCallId`;
- `status`;
- `isError`;
- `content`.

### Ordering

Adapter сохраняет transcript order.

Для одного turn клиент получает события в том порядке, в котором они появились
в JSONL:

```text
text chunk
tool_call
tool_call_update
text chunk
terminal PromptResponse
```

Tool events не завершают prompt сами по себе. Completion boundary остается из
`PRD-008`: terminal assistant stop reason текущего turn.

### Output detail settings

Adapter должен поддержать настройки детализации tool output. Настройки нужны,
чтобы один и тот же adapter можно было использовать в двух режимах:

- compact logs для обычного pipeline run;
- подробный evidence режим для отладки.

Минимальная конфигурационная модель:

```text
toolEvents: off | compact | full
toolResultMaxBytes: integer
toolInputMaxBytes: integer
```

Значения по умолчанию:

- `toolEvents = compact`;
- `toolInputMaxBytes = 4096`;
- `toolResultMaxBytes = 8192`.

Поведение:

- `off`: adapter не отправляет tool updates наружу;
- `compact`: adapter отправляет metadata, title и обрезанный input/result;
- `full`: adapter отправляет полный JSON input/result без intentional
  truncation.

Даже в `full` режиме adapter может ограничивать payload техническим hard limit,
если это нужно для стабильности stdio JSON-RPC. В таком случае update содержит
`truncated = true` и `originalBytes`.

### CLI arguments

Adapter binary должен принимать аргументы:

```bash
claude-acp-adapter \
  --tool-events compact \
  --tool-input-max-bytes 4096 \
  --tool-result-max-bytes 8192
```

Поддерживаемые значения:

- `--tool-events off`;
- `--tool-events compact`;
- `--tool-events full`.

Аргументы CLI имеют приоритет над environment variables.

### Environment variables

Для запуска через shell/runner без изменения command line adapter поддерживает:

```bash
CLAUDE_ACP_TOOL_EVENTS=compact
CLAUDE_ACP_TOOL_INPUT_MAX_BYTES=4096
CLAUDE_ACP_TOOL_RESULT_MAX_BYTES=8192
```

Environment variables имеют приоритет над defaults и ниже CLI arguments.

### Session config option

Для ACP clients, которые умеют менять session options до prompt, adapter
добавляет `session/set_config_option`:

```json
{
  "configId": "toolEvents",
  "value": "full"
}
```

Минимальные config ids:

- `toolEvents`;
- `toolInputMaxBytes`;
- `toolResultMaxBytes`.

Поведение:

- значения применяются только до active prompt;
- значения сохраняются в session config;
- следующий prompt использует обновленную детализацию;
- unsupported value возвращает protocol error.

Session config имеет приоритет над process defaults, но ниже explicit CLI
hard-disable, если будет введен отдельный operational lock. В минимальной версии
operational lock не нужен.

## Wire contract

### ACP types

`internal/acp` должен расширить `SessionUpdate` так, чтобы текущий text update
остался совместимым, а tool updates получили отдельные поля.

Минимальная shape:

```go
type SessionUpdate struct {
    SessionUpdate string          `json:"sessionUpdate"`
    MessageID     string          `json:"messageId,omitempty"`
    Content       ContentBlock    `json:"content,omitempty"`
    ToolCallID    string          `json:"toolCallId,omitempty"`
    Title         string          `json:"title,omitempty"`
    Kind          string          `json:"kind,omitempty"`
    Status        string          `json:"status,omitempty"`
    Input         json.RawMessage `json:"input,omitempty"`
    IsError       *bool           `json:"isError,omitempty"`
    Truncated     bool            `json:"truncated,omitempty"`
    OriginalBytes int             `json:"originalBytes,omitempty"`
}
```

Поле `Content` для tool result может остаться `ContentBlock`, если клиентский
контракт ACP требует content blocks. Если ACP допускает raw JSON content, лучше
использовать `json.RawMessage`, потому что Claude tool result бывает строкой,
массивом blocks или объектом.

### App mapping

`internal/app/updates.go` становится единственным местом, где transport events
превращаются в ACP updates:

- `AssistantTextEvent` -> `agent_message_chunk`;
- `AssistantToolUseEvent` -> `tool_call`;
- `ToolResultEvent` -> `tool_call_update`;
- остальные events остаются внутренними или diagnostic-only.

Transport layer не импортирует ACP types.

### Truncation

Truncation применяется после JSON normalization.

Правила:

- input/result меньше лимита передается как есть;
- input/result больше лимита обрезается по bytes;
- update получает `truncated = true`;
- update получает `originalBytes`;
- compact title всегда сохраняется, даже если payload обрезан.

## Ошибки

Adapter возвращает protocol error при неверной конфигурации:

- unknown `toolEvents` value;
- отрицательный byte limit;
- нечисловой byte limit;
- config mutation во время active prompt.

Ошибки tool execution не становятся protocol errors сами по себе. Они остаются
частью stream:

```json
{
  "sessionUpdate": "tool_call_update",
  "toolCallId": "tool-1",
  "status": "failed",
  "isError": true,
  "content": "..."
}
```

Prompt-level error возвращается только по transport failure, cancellation,
timeout или protocol violation.

## Архитектурное решение

### Protocol layer

`internal/acp` отвечает за wire types:

- новые поля `SessionUpdate`;
- session config option metadata для tool settings;
- JSON-RPC compatibility.

Protocol layer не знает про Claude transcript records.

### App layer

`internal/app` отвечает за:

- хранение tool observability config в `SessionConfig`;
- validation config values;
- mapping `claude.TranscriptEvent` в `acp.SessionUpdate`;
- truncation policy;
- display title construction.

### Claude transport layer

`internal/claude` отвечает за:

- чтение transcript;
- парсинг `tool_use` и `tool_result`;
- сохранение byte offset и raw payload.

`internal/claude` не решает, как tool event выглядит в ACP.

## Acceptance Criteria

- `tool_use` transcript event отправляется как `session/update` с
  `sessionUpdate = "tool_call"`.
- `tool_result` transcript event отправляется как `session/update` с
  `sessionUpdate = "tool_call_update"`.
- Tool call update содержит `toolCallId`, `kind`, `status`, `input`.
- Tool result update содержит `toolCallId`, `status`, `isError`, `content`.
- `is_error = true` мапится в `status = "failed"`.
- `is_error = false` мапится в `status = "completed"`.
- Text streaming продолжает работать как `agent_message_chunk`.
- События приходят в transcript order.
- Tool events не завершают prompt.
- Default mode `compact` включен без дополнительных аргументов.
- `--tool-events off` отключает наружные tool updates.
- `--tool-events full` отправляет полный input/result в пределах hard limit.
- `--tool-input-max-bytes` ограничивает input payload.
- `--tool-result-max-bytes` ограничивает result payload.
- Environment variables задают defaults для process.
- Session config option `toolEvents` меняет поведение следующего prompt.
- Config mutation во время active prompt возвращает protocol error.
- Unit tests покрывают `AssistantToolUseEvent -> tool_call`.
- Unit tests покрывают `ToolResultEvent -> tool_call_update`.
- Unit tests покрывают truncation metadata.
- Unit tests покрывают CLI/env config precedence.
- Process test покрывает JSON-RPC stream с text -> tool_call ->
  tool_call_update -> text.

## Тестирование

Обязательные проверки после реализации:

```bash
go test ./...
go test -race ./...
go vet ./...
make build
make check
```

Отдельные focused tests:

```bash
go test ./internal/claude -run TestParseTranscriptEventsFromFixture -count=1
go test ./internal/app -run 'Test.*Tool.*Update|Test.*Tool.*Config' -count=1
go test ./cmd/claude-acp-adapter -run Test.*Tool -count=1
```

Manual smoke:

```bash
cogerentor run resume --run-id <run-id> \
  --agents 'claude-code=/path/to/claude-acp-adapter'
```

Ожидаемый smoke result: Cogerentor log показывает хотя бы компактные tool lines
между assistant text chunks.

## Риски

- ACP clients могут иметь разные ожидания к names `tool_call` и
  `tool_call_update`. Перед implementation нужно сверить текущий Cogerentor
  parser и при необходимости зафиксировать adapter-specific compatibility.
- Claude tool result content может быть строкой, массивом или объектом.
  `ContentBlock` может оказаться слишком узким wire type.
- Full mode может раздуть JSON-RPC stream на больших file reads или command
  outputs. Поэтому compact default является обязательным.
- Слишком агрессивное truncation может убрать полезный evidence. Поэтому update
  должен сохранять `truncated` и `originalBytes`.

## Вне рамок

- Рендеринг diff previews.
- File artifact upload.
- Binary/image tool outputs.
- Перезапуск failed tools.
- Correlation с external spans/traces.
- Нормализация всех Claude tool names в отдельный registry.
