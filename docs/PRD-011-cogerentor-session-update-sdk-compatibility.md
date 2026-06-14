# PRD-011: Cogerentor session/update SDK compatibility

## Кратко

`claude-acp-adapter` должен отправлять `session/update` notifications в форме,
которую стабильно декодирует текущий Cogerentor ACP client.

Текущий Cogerentor использует:

```text
github.com/coder/acp-go-sdk v0.13.0
```

После добавления tool observability adapter начал отправлять tool updates в
самодельной JSON-форме. Эта форма частично похожа на ACP, но расходится с
реальным SDK union contract. В результате Cogerentor получает notification,
пытается декодировать `sdk.SessionUpdate` и падает:

```text
failed to handle notification method=session/update
invalid variant payload
```

Это ломает reviewer stage уже во время первого tool call, хотя `initialize`,
`session/new`, `session/set_config_option` и старт `session/prompt` проходят.

## Цель

Сделать `session/update` wire payload SDK-safe для Cogerentor:

- `agent_message_chunk` остается валидным ACP text update;
- `tool_call` соответствует `sdk.SessionUpdateToolCall`;
- `tool_call_update` соответствует `sdk.SessionToolCallUpdate`;
- нестандартная diagnostic metadata не ломает SDK decode;
- regression tests проверяют payload через настоящий `github.com/coder/acp-go-sdk`
  или эквивалентный contract fixture.

Минимальный успешный сценарий:

```text
Claude transcript tool_use
-> internal claude event
-> app mapping
-> ACP session/update tool_call
-> Cogerentor sdk.SessionUpdate decode
-> Cogerentor frame/log line
-> prompt continues
```

## Почему это нужно

Cogerentor является production client для этого adapter-а. Совместимость
определяется не локальной структурой `internal/acp.SessionUpdate`, а тем, что
реально принимает `acp-go-sdk v0.13.0`.

Сейчас наши unit tests зеленые, потому что они закрепляют собственную форму:

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

Но Cogerentor SDK ожидает форму вроде:

```json
{
  "sessionUpdate": "tool_call",
  "toolCallId": "tool-1",
  "title": "Read /tmp/a.txt",
  "kind": "read",
  "status": "pending",
  "rawInput": {
    "file_path": "/tmp/a.txt"
  }
}
```

Для `tool_call_update` текущий разрыв еще сильнее: adapter отправляет `content`
как строку или произвольный JSON, а SDK ожидает массив `ToolCallContent`.

## Источники истины

### Adapter

- `internal/acp/types.go`
- `internal/app/updates.go`
- `internal/app/updates_test.go`
- `internal/app/service_test.go`
- `docs/PRD-009-tool-event-observability.md`
- `docs/PRD-010-acp-compatibility-and-timeout-ownership.md`

### Cogerentor

- `/Users/neiro/dev/cogerentor/apps/engine/engine/go.mod`
- `/Users/neiro/dev/cogerentor/apps/engine/engine/internal/agent/acp/runner.go`
- `/Users/neiro/dev/cogerentor/apps/engine/engine/internal/agent/acp/updates.go`
- `/Users/neiro/dev/cogerentor/apps/engine/engine/internal/worker/agent_adapter.go`
- `/Users/neiro/dev/cogerentor/.cogerentor-state/runs/run-82/events.jsonl`
- `/Users/neiro/dev/cogerentor/.cogerentor-state/runs/run-82/agent-log.jsonl`

### SDK

- `github.com/coder/acp-go-sdk v0.13.0`
- `types_gen.go`: `SessionUpdate`, `SessionUpdateToolCall`,
  `SessionToolCallUpdate`, `ToolCallStatus`, `ToolCallContent`.
- `testdata/json_golden/session_update_tool_call*.json`

## Фактические расхождения

### 1. `tool_call.status`

Сейчас adapter отправляет:

```json
"status": "started"
```

SDK enum:

```text
pending
in_progress
completed
failed
```

Требуемое изменение:

- initial `tool_call` использует `pending`;
- progress update может использовать `in_progress`;
- final result update использует `completed` или `failed`.

### 2. `tool_call.kind`

Сейчас adapter отправляет Claude tool name:

```json
"kind": "Bash"
```

SDK ожидает `ToolKind`, то есть категорию инструмента, а не имя Claude tool.

Требуемое изменение:

- `Read` -> `read`;
- `Edit`, `MultiEdit`, `Write` -> `edit`;
- `Bash` -> `execute`;
- search/list tools -> `search`, если такой kind поддержан SDK;
- unknown tools не получают `kind`, а имя остается в `title`.

Перед реализацией нужно открыть enum `ToolKind` в `acp-go-sdk v0.13.0` и
использовать только значения из него.

### 3. `tool_call.input`

Сейчас adapter отправляет:

```json
"input": {...}
```

SDK ожидает:

```json
"rawInput": {...}
```

Требуемое изменение:

- заменить exported field `Input` на `RawInput`;
- сохранить truncation behavior, но отдавать результат в `rawInput`;
- metadata о truncation перенести в `_meta`, если она нужна клиенту.

### 4. `tool_call_update.content`

Сейчас compact mode отправляет:

```json
"content": "file contents"
```

Full mode может отправлять произвольный объект или массив Claude content blocks.

SDK ожидает:

```json
"content": [
  {
    "type": "content",
    "content": {
      "type": "text",
      "text": "file contents"
    }
  }
]
```

Требуемое изменение:

- compact result превращать в один text `ToolCallContent`;
- full result либо класть в `rawOutput`, либо преобразовывать поддержанные
  content blocks в SDK-compatible `ToolCallContent`;
- произвольный JSON result безопасно показывать как text preview и/или
  отдавать как `rawOutput`.

### 5. `isError`, `truncated`, `originalBytes`

Сейчас adapter кладет эти поля на верхний уровень update.

SDK может терпеть extra fields, но contract-level безопаснее держать
нестандартные детали в `_meta`.

Требуемое изменение:

```json
"_meta": {
  "isError": true,
  "truncated": true,
  "originalBytes": 12345
}
```

`status` остается основным SDK-visible сигналом:

- `completed` для успешного результата;
- `failed` для tool error.

### 6. Тесты проверяют не тот контракт

Сейчас тесты в `internal/app/updates_test.go` утверждают текущий локальный
payload, включая `status = "started"` и поле `input`.

Требуемое изменение:

- обновить unit tests под SDK-safe payload;
- добавить contract test, который маршалит наш `SessionUpdate`, затем
  декодирует его в `github.com/coder/acp-go-sdk.SessionUpdate`;
- добавить negative fixture на старый payload, чтобы было видно, почему он
  ломал Cogerentor;
- добавить process-level smoke с text -> tool_call -> tool_call_update -> text,
  где stdout notifications проходят SDK decode.

## Требуемое поведение

### Assistant text

Text update остается:

```json
{
  "sessionUpdate": "agent_message_chunk",
  "messageId": "msg-1",
  "content": {
    "type": "text",
    "text": "hello"
  }
}
```

`messageId` сохраняется, если он есть в transcript event.

### Tool start

Для Claude `Read`:

```json
{
  "sessionUpdate": "tool_call",
  "toolCallId": "tool-1",
  "title": "Read /tmp/a.txt",
  "kind": "read",
  "status": "pending",
  "rawInput": {
    "file_path": "/tmp/a.txt"
  }
}
```

Для Claude `Bash`:

```json
{
  "sessionUpdate": "tool_call",
  "toolCallId": "tool-2",
  "title": "Bash go test ./...",
  "kind": "execute",
  "status": "pending",
  "rawInput": {
    "command": "go test ./..."
  }
}
```

Если `kind` не удается безопасно сопоставить с SDK enum, поле `kind`
опускается.

### Tool result success

Compact mode:

```json
{
  "sessionUpdate": "tool_call_update",
  "toolCallId": "tool-1",
  "status": "completed",
  "content": [
    {
      "type": "content",
      "content": {
        "type": "text",
        "text": "file contents"
      }
    }
  ]
}
```

Full mode:

```json
{
  "sessionUpdate": "tool_call_update",
  "toolCallId": "tool-1",
  "status": "completed",
  "rawOutput": {
    "result": "success"
  },
  "content": [
    {
      "type": "content",
      "content": {
        "type": "text",
        "text": "{\"result\":\"success\"}"
      }
    }
  ]
}
```

### Tool result error

```json
{
  "sessionUpdate": "tool_call_update",
  "toolCallId": "tool-1",
  "status": "failed",
  "_meta": {
    "isError": true
  },
  "content": [
    {
      "type": "content",
      "content": {
        "type": "text",
        "text": "error message"
      }
    }
  ]
}
```

## Архитектурное решение

Правильная граница фикса: `internal/acp` и `internal/app`.

`internal/claude` уже отдает доменные transcript events:

- `AssistantTextEvent`;
- `AssistantToolUseEvent`;
- `ToolResultEvent`.

Эти события остаются Claude transport model. Они не должны импортировать
Cogerentor или ACP SDK.

`internal/app/updates.go` отвечает за перевод доменных transport events в ACP
wire updates. Именно там должен появиться SDK-compatible mapping.

`internal/acp/types.go` должен описывать wire shape, близкий к ACP SDK:

- `RawInput`;
- `RawOutput`;
- `Content []ToolCallContent`;
- `Meta map[string]any`;
- typed или validated status/kind values.

Импортировать `github.com/coder/acp-go-sdk` в production code adapter-а не
обязательно. Для contract tests импорт допустим и полезен, потому что Cogerentor
реально использует этот SDK.

## План реализации

1. Открыть `acp-go-sdk v0.13.0` и зафиксировать поддержанные значения
   `ToolKind`, `ToolCallStatus`, `ToolCallContent`.
2. Обновить `internal/acp.SessionUpdate` и добавить минимальные supporting
   structs для SDK-compatible tool content.
3. Обновить `internal/app/updates.go`:
   - `input` -> `rawInput`;
   - `started` -> `pending`;
   - Claude tool name -> SDK `kind`;
   - result content -> `[]ToolCallContent`;
   - нестандартные fields -> `_meta`.
4. Обновить truncation:
   - truncation применяется к preview/raw payload;
   - `truncated/originalBytes` уходят в `_meta`;
   - payload остается валидным JSON после обрезки.
5. Обновить unit tests в `internal/app/updates_test.go`.
6. Добавить SDK contract tests:
   - `agent_message_chunk` decode;
   - `tool_call` decode;
   - `tool_call_update` decode;
   - interleaving stream decode.
7. Обновить `docs/PRD-009-tool-event-observability.md`, потому что текущий PRD
   закрепляет старую форму `input/status=started/content=string`.
8. Обновить README/operator docs, если там описывается tool event payload.
9. Запустить полный verification contour.
10. Повторить Cogerentor scenario:
    `cogerentor run resume --run-id run-82` или новый короткий
    `multi_agent_review` smoke.

## Проверки

Обязательные локальные проверки в `claude-acp-adapter`:

```bash
go test ./...
make build
make check
```

Contract checks:

```bash
go test ./internal/app -run 'SDK|SessionUpdate|Tool'
go test ./internal/acp -run 'SessionUpdate|SDK'
```

Cogerentor integration check:

```bash
cogerentor "внимательное ревью PRD-016-project-registry-task-graph-execution.md пожалуйста" \
  -p multi_agent_review \
  --agents 'claude-code=/Users/deniszabozhanov/.local/bin/claude-code-cli-acp'
```

Успешный результат:

- log не содержит `invalid variant payload`;
- reviewer `claude-reviewer` проходит первые tool calls;
- Cogerentor frame stream показывает tool activity;
- `session/prompt` завершается обычным `stopReason` или caller-owned
  cancellation;
- infra error по `session/update` исчезает.

## Acceptance criteria

- `tool_call` notification декодируется в `sdk.SessionUpdate.ToolCall`.
- `tool_call_update` notification декодируется в
  `sdk.SessionUpdate.ToolCallUpdate`.
- `agent_message_chunk` продолжает декодироваться и отображаться как текст.
- `toolEvents=off` подавляет tool updates.
- `toolEvents=compact` отправляет краткий SDK-valid preview.
- `toolEvents=full` отправляет SDK-valid payload и сохраняет raw output в
  безопасном поле.
- Старый payload со `status=started`, `input`, string `content` больше не
  генерируется.
- Тесты падают, если future change снова вернет SDK-incompatible shape.
- Cogerentor run проходит дальше первых Claude tool calls без
  `invalid variant payload`.

## Риски и ограничения

Главный риск: ACP SDK может менять tool schema между версиями. Поэтому tests
должны быть привязаны к версии SDK, которую реально использует Cogerentor.

Второй риск: богатые Claude tool outputs не всегда естественно ложатся в
`ToolCallContent`. Для production log достаточно text preview + `rawOutput`.
Rich rendering можно расширять отдельной задачей после восстановления
совместимости.

Третий риск: `kind` не является Claude tool name. Ошибочное сопоставление
хуже, чем отсутствие kind. Для неизвестных tools лучше опустить `kind` и
оставить человекочитаемый `title`.

## Out of scope

- Обновление версии `github.com/coder/acp-go-sdk` в Cogerentor.
- Изменение Cogerentor error policy для reviewer infra errors.
- Изменение Claude transcript parser.
- Rich UI для diff/terminal/image tool outputs.
- Новая capability negotiation для tool update variants.

## Связанные follow-ups

- Переписать `docs/PRD-009-tool-event-observability.md` под SDK-safe payload.
- Добавить в adapter отдельный compatibility test package с fixtures из
  `acp-go-sdk/testdata/json_golden`.
- Зафиксировать в README, что Cogerentor compatibility проверяется через
  `acp-go-sdk v0.13.0`.
