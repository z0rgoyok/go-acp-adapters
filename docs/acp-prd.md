# PRD: ACP-адаптер

## Кратко

`claude-acp-adapter` должен открывать Claude Code в интерактивном CLI-режиме
как ACP-агент.

Ценность продукта: ACP-клиент запускает этот бинарь через stdio, создаёт
рабочую сессию, отправляет промпты, получает обновления assistant, отменяет
активную работу и закрывает сессии. Claude Code при этом продолжает работать
через уже реализованный интерактивный транспорт.

## Источники

- ACP v1 overview: https://agentclientprotocol.com/protocol/v1/overview
- ACP v1 schema: https://agentclientprotocol.com/protocol/v1/schema
- Референсный ACP-репозиторий: https://github.com/agentclientprotocol/agent-client-protocol
- Контекст текущей реализации: https://github.com/z0rgoyok/claude-acp-adapter

Текущий репозиторий является контекстом реализации. Требования идут из
ожидаемого поведения adapter и контракта ACP v1.

## Цели

- Запускаться как локальный subprocess ACP-агента поверх stdin/stdout.
- Реализовать базовый жизненный цикл ACP:
  - `initialize`;
  - `session/new`;
  - `session/prompt`;
  - `session/cancel`.
- Отправлять текст assistant клиенту через `session/update`.
- Возвращать `PromptResponse.stopReason` после завершения turn.
- Мапить ACP sessions на изолированные сессии интерактивного Claude transport.
- Держать transport, protocol и orchestration в отдельных пакетах.
- Сохранить `internal/claude` как источник истины для жизненного цикла Claude
  CLI, доставки prompt, cancellation, чтения transcript и cleanup.
- Оставить developer smoke path для прямой проверки transport.

## Первый срез

Первый ACP-срез закрывает базовый контракт агента:

- запуск процесса как ACP stdio server;
- инициализация и согласование capabilities;
- создание session с absolute `cwd`;
- выполнение text prompt через Claude Code;
- доставка финального assistant update;
- отмена turn;
- очистка session.

Более богатые capabilities добавляются после end-to-end базового контракта:

- загрузка истории session;
- подробная визуализация file edits;
- эмуляция terminal tools;
- embedded image, audio и binary resources;
- in-process tool hosting;
- authentication flows.

## Пользователи

### ACP-клиент

Editor, desktop app или agent UI запускает бинарь как subprocess и говорит с
ним по ACP поверх stdio.

### Локальный разработчик

Разработчик запускает adapter локально, проверяет поведение protocol и дебажит
Claude transport через логи и smoke commands.

## Поведение продукта

### Режим процесса

Adapter binary стартует ACP JSON-RPC server поверх stdin/stdout.

stdout зарезервирован для protocol messages. stderr несёт diagnostics.

### Инициализация

На `initialize` adapter возвращает:

- `protocolVersion`: ACP v1, если client его поддерживает;
- `agentInfo`: имя и версия adapter;
- `agentCapabilities`: text prompts, resource links, cancellation, close после
  реализации close;
- `authMethods`: пустой список для первого среза.

Adapter принимает `_meta` и сохраняет его там, где protocol требует pass-through
behavior.

### Создание Session

На `session/new` adapter:

- валидирует `cwd` как absolute path;
- создаёт Claude transport session с корнем в `cwd`;
- сохраняет ACP session record in memory;
- стартует Claude Code через `internal/claude`;
- возвращает ACP `sessionId`.

`additionalDirectories` сохраняются в session record. Поддержка их передачи в
Claude Code реализуется через Claude CLI flags, когда mapping явный.

`mcpServers` являются обязательной частью `session/new`. Первый compliant slice
поддерживает stdio MCP servers и передаёт их в Claude Code через явный
`--mcp-config` mapping. `http` и `sse` принимаются только после advertising
соответствующих `mcpCapabilities`; до этого такие requests fail fast с
protocol error.

### Prompt Turn

На `session/prompt` adapter:

- находит целевую session;
- конвертирует поддержанные ACP `ContentBlock` values в Claude prompt string;
- стартует один cancellable turn для этой session;
- вызывает Claude transport `Query`;
- отправляет минимум один `session/update` notification с assistant text;
- возвращает `PromptResponse` со stop reason.

Базовая конвертация prompt:

- text blocks становятся plain text;
- resource links становятся readable references с URI, title и mime type при
  наличии;
- неподдержанные content kinds дают protocol error до старта Claude turn.

### Streaming Updates

Первая реализация может отправлять assistant text как final `session/update`
после готового transport response.

Следующее улучшение streaming:

- assistant text chunks становятся ACP message chunks;
- tool-like transcript events становятся ACP tool updates, когда есть стабильный
  mapping;
- progress updates сохраняют порядок внутри active turn.

### Cancellation

На `session/cancel` adapter:

- находит active turn для session;
- отменяет его context;
- отправляет `C-c` через Claude transport;
- даёт исходному `session/prompt` request завершиться со stop reason
  `cancelled`.

`session/cancel` является notification. Adapter не отправляет JSON-RPC response
на сам `session/cancel`; результат cancellation виден только через pending
`session/update` notifications и финальный response исходного `session/prompt`.

Cancellation идемпотентна внутри adapter. Повторная cancellation для той же
session сохраняет то же effective state и также остаётся notification без
response.

### Session Close

Когда `session/close` реализован и advertised, adapter:

- отменяет active prompt turn;
- отключает Claude transport;
- удаляет временные FIFO resources;
- удаляет session из in-memory registry.

### Ошибки

Adapter мапит errors в JSON-RPC responses со стабильными messages:

- invalid request shape;
- unsupported protocol version;
- unknown session;
- prompt already running for the session;
- unsupported content block;
- Claude transport startup failure;
- Claude transport timeout;
- Claude transcript failure.

Transport failures содержат достаточно context для local setup debugging и
сохраняют stdout валидным JSON-RPC stream.

## Архитектура

### Пакеты

- `internal/acp`: ACP JSON-RPC handling, protocol types, method dispatch и
  stdio connection.
- `internal/app`: orchestration sessions и use cases.
- `internal/claude`: существующий Claude interactive transport.
- `cmd/claude-acp-adapter`: process entry point и mode wiring.

### Стратегия зависимостей

Используем established ACP Go package для wire types и connection plumbing,
когда его API, license и schema version совпадают с ACP v1. Текущий candidate:
`github.com/coder/acp-go-sdk`.

Перед выбором dependency нужен spike: подтвердить ACP v1 schema coverage,
license compatibility и наличие типов/connection primitives для stdio JSON-RPC.
Fallback включается, если пакет расходится со schema, тянет лишний runtime
surface или не покрывает notification semantics. В таком случае генерируем или
поддерживаем narrow protocol package из official ACP JSON schema вместо ad hoc
JSON maps.

### Session Model

Application layer владеет:

- `SessionID`;
- `cwd`;
- relevant ACP client capabilities;
- Claude transport client;
- active turn cancel function;
- prompt serialization lock;
- created и updated timestamps.

Каждая ACP session мапится на один Claude transport client.

### Turn Model

Turn содержит:

- `sessionId`;
- `requestId`;
- input content blocks;
- cancellable context;
- start time;
- final stop reason;
- transcript path, если известен.

Application layer гарантирует один active turn на session.

## Stop Reason Mapping

- Claude `end_turn` мапится в ACP `end_turn`.
- Claude `max_tokens` мапится в ACP `max_tokens`.
- Claude `stop_sequence` мапится в ACP `end_turn`, потому что ACP v1 не имеет
  `stop_sequence`, а turn завершён штатно.
- Context cancellation мапится в ACP `cancelled`.
- Transport timeout мапится в ACP `cancelled`, когда transport cancellation
  method успешно отправил interrupt в живую Claude session.
- Transport timeout мапится в protocol/transport error, когда session уже
  недоступна, interrupt не был отправлен или transport не может подтвердить
  живое состояние session.
- Неподдержанный Claude stop reason мапится в `end_turn` только если transcript
  содержит завершённый assistant text; иначе adapter возвращает transport
  error, чтобы не производить недействительный ACP `StopReason`.

## CLI Shape

Целевое поведение процесса:

```text
claude-acp-adapter
```

Запускает ACP stdio server.

Текущая smoke-команда сохраняется через явную подкоманду:

```text
claude-acp-adapter query -cwd /tmp -timeout 45s -prompt "Reply with OK"
```

## Наблюдаемость

- stdout содержит только ACP JSON-RPC.
- stderr содержит structured diagnostics.
- Каждая session log line включает ACP session ID и Claude session ID.
- Каждая prompt turn log line включает request ID, duration, stop reason и
  transcript path, когда он известен.

## Требования к тестированию

Модульные тесты:

- JSON-RPC request dispatch;
- `initialize` response capability negotiation;
- `session/new` validation и session registry behavior;
- prompt content conversion;
- `session/prompt` success mapping;
- `session/cancel` notification handling без JSON-RPC response;
- transport error to JSON-RPC error mapping;
- stdout/stderr separation.

Интеграционные тесты:

- ACP stdio handshake against an in-process client;
- `initialize -> session/new -> session/prompt` using fake Claude transport;
- cancellation while fake transport blocks;
- session cleanup after close.

Smoke-тесты:

- real Claude CLI prompt through ACP stdio;
- real cancellation against a long-running prompt;
- signal shutdown leaves no tmux sessions or FIFO files.

## Критерии приёмки

- ACP client initializes adapter и получает valid ACP v1 capabilities.
- ACP client creates session with absolute `cwd`.
- ACP client sends text prompt и receives assistant text через
  `session/update`.
- Corresponding `session/prompt` response returns valid stop reason.
- `session/cancel` interrupts active Claude turn.
- `session/close` makes the session no longer addressable and disconnects the
  underlying transport.
- Non-empty stdio `mcpServers` are forwarded to Claude Code through
  `--mcp-config`; unsupported MCP transports fail fast unless advertised.
- stdout contains only JSON-RPC messages during ACP mode.
- `go test ./...`, `go test -race ./...`, `go vet ./...` pass.
- All Go/source files remain under 300 lines.

## План поставки

### Этап 1: Protocol Skeleton

- Провести dependency spike для ACP Go SDK candidate.
- Добавить ACP package и stdio JSON-RPC loop.
- Реализовать `initialize`.
- Добавить fake transport для tests.

### Этап 2: Session Lifecycle

- Реализовать `session/new`.
- Добавить in-memory session registry.
- Связать sessions с Claude transport clients.
- Реализовать stdio `mcpServers` forwarding в Claude CLI `--mcp-config`.

### Этап 3: Prompt Turn

- Реализовать prompt content conversion.
- Реализовать `session/prompt`.
- Отправлять final assistant text как `session/update`.
- Возвращать prompt stop reason.

### Этап 4: Cancellation And Cleanup

- Добавить exported cancellation boundary в `internal/claude`, например
  `Cancel` или `Interrupt`, чтобы application layer не знал про tmux.
- Реализовать `session/cancel`.
- Реализовать advertised `session/close`.
- Добавить signal-aware server shutdown.

### Этап 5: Compatibility Smoke

- Запустить real ACP client handshake.
- Запустить real Claude prompt smoke через ACP.
- Документировать local usage.

## Заметки по реализации

- Держать ACP request handlers свободными от tmux и transcript details.
- Держать Claude transport свободным от ACP protocol types.
- Размещать mapping code в application layer.
- Предпочитать capability-gated additions широким protocol claims.
- Добавлять package-level interfaces только на boundary between app orchestration and
  Claude transport.
