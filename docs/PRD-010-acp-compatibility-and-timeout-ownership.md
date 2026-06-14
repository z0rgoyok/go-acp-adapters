# PRD-010: ACP compatibility and timeout ownership

## Кратко

`claude-acp-adapter` должен быть совместим с текущим Cogerentor ACP client и
доверять timeout-ами вызывающей стороне.

Этот PRD закрывает два contract-level дефекта:

1. `session/new.configOptions` может сломать client-side decode, если adapter
   отдаёт option variant, которого нет в используемом ACP SDK.
2. ACP prompt turn может быть скрыто ограничен adapter default timeout-ом,
   хотя stage timeout уже задан Cogerentor profile.

Оба дефекта проявляются до полноценной работы агента:

- первый ломает `session/new`;
- второй режет рабочий turn раньше stage timeout-а.

## Цель

Сделать adapter предсказуемым production transport для Cogerentor:

- `session/new` возвращает только SDK-safe configuration payload;
- новые настройки adapter-а проходят compatibility gate перед публикацией в
  `configOptions`;
- ACP mode использует lifecycle context вызывающей стороны как источник правды;
- ручной `query` mode сохраняет локальный timeout для smoke usage.

Минимальный успешный сценарий:

```text
Cogerentor stage timeout 8m
-> adapter ACP session
-> Claude prompt turn
-> turn runs until Claude completion or caller context cancellation
```

## Почему это нужно

### Config option compatibility

Cogerentor сейчас использует `github.com/coder/acp-go-sdk v0.13.0`.

Этот SDK стабильно декодирует `SessionConfigOption` variant:

```json
{
  "type": "select",
  "id": "model",
  "currentValue": "claude-opus-4-8",
  "options": [
    {"name": "claude-opus-4-8", "value": "claude-opus-4-8"}
  ]
}
```

Если adapter отдаёт:

```json
{
  "type": "number",
  "id": "toolInputMaxBytes",
  "currentValue": 32768
}
```

SDK падает на decode с ошибкой:

```text
invalid variant payload
```

Для pipeline это выглядит как failure reviewer-а на `session/new`, хотя Claude
даже не стартовал как reviewer turn.

### Timeout ownership

Cogerentor profile уже задаёт stage timeout:

```yaml
timeoutMin: 8
```

Этот timeout является внешним контрактом stage. Adapter default `90s` внутри ACP
mode создаёт скрытый второй deadline:

```text
stage timeout = 8 минут
adapter prompt timeout = 90 секунд
```

Смыслово это конфликт слоёв. Cogerentor отвечает за lifecycle stage-а, adapter
отвечает за доставку prompt-а в Claude и наблюдение transcript completion.

## Источники

- ACP service options:
  `internal/app/types.go`
- Claude client timeout:
  `internal/claude/client.go`
- ACP entrypoint:
  `cmd/claude-acp-adapter/main.go`
- Session config response:
  `internal/app/session_config.go`
- ACP wire types:
  `internal/acp/types.go`
- Tool visibility scope:
  `docs/PRD-009-tool-event-observability.md`

## Scope

В scope входят четыре изменения поведения:

1. Ввести compatibility rule для `session/new.configOptions`.
2. Убрать unsupported variants из advertised `configOptions`.
3. Сделать ACP mode caller-timeout-owned.
4. Сохранить локальный timeout для ручного `query` mode.

В scope входят проверки, которые защищают это поведение от регрессии:

- unit tests на `session/new` payload;
- process/stdout JSON-RPC test;
- timeout behavior tests для ACP service и Claude client;
- documentation update для operator contract.

## Compatibility contract

### Rule 1: advertised configOptions are SDK-safe

Adapter обязан отдавать в `session/new.configOptions` только variants, которые
поддерживает текущий production client.

Для Cogerentor + `acp-go-sdk v0.13.0` SDK-safe surface:

- `type = "select"`;
- `currentValue` соответствует типу select value;
- `options` заполнены списком `{name, value}`;
- неизвестные variants отсутствуют в `session/new`.

### Rule 2: numeric settings use process config by default

Numeric adapter settings задаются через process-level configuration:

```bash
CLAUDE_ACP_TOOL_INPUT_MAX_BYTES=4096
CLAUDE_ACP_TOOL_RESULT_MAX_BYTES=8192
```

или через CLI flags:

```bash
claude-acp-adapter \
  --tool-input-max-bytes 4096 \
  --tool-result-max-bytes 8192
```

Эти settings применяются как session defaults.

### Rule 3: session config may accept more than it advertises

Adapter может принимать `session/set_config_option` для numeric settings, если
client явно отправил такой request:

```json
{
  "configId": "toolInputMaxBytes",
  "value": 4096
}
```

Но adapter не рекламирует numeric option в `session/new.configOptions`, пока
production client SDK поддерживает только `select`.

Это разделяет два контракта:

- advertised UX surface;
- accepted API surface.

### Rule 4: select presets are the safe advertised alternative

Если numeric setting нужно сделать видимым в `session/new`, adapter использует
select presets вместо number variant:

```json
{
  "type": "select",
  "id": "toolPayloadProfile",
  "currentValue": "compact",
  "options": [
    {"name": "Compact", "value": "compact"},
    {"name": "Verbose", "value": "verbose"},
    {"name": "Full", "value": "full"}
  ]
}
```

В минимальной поставке достаточно рекламировать только:

- `toolEvents: off | compact | full`.

Byte limits остаются process defaults.

## Timeout ownership contract

### ACP mode

В ACP mode adapter не задаёт собственный prompt turn timeout.

Поведение:

- `app.NewService` сохраняет `Timeout = 0` как valid production value;
- `NewClaudeTransport` передаёт `Timeout = 0` в `claude.NewClient`;
- `claude.Client` трактует `Timeout = 0` как отсутствие adapter-owned turn
  deadline;
- `runTurn` использует incoming context напрямую;
- cancellation/timeout приходят через `ctx.Done()`.

Источник timeout-а:

```text
Cogerentor stage context
-> ACP server request context
-> app PromptTurn context
-> claude runTurn context
```

### Manual query mode

`query` command остаётся standalone smoke tool.

Для него локальный timeout полезен, потому что caller может быть обычным shell:

```bash
claude-acp-adapter query --timeout 90s --prompt "hello"
```

Default `90s` остаётся только в query mode.

### Internal waits

Отсутствие adapter-owned prompt timeout не означает бесконечное игнорирование
caller cancellation.

Все внутренние waits обязаны принимать context:

- `WaitReady`;
- transcript discovery;
- transcript tail;
- Stop hook wait;
- tmux command execution where applicable.

Если caller context отменён, adapter завершает prompt с cancellation/transport
error в текущем protocol mapping.

## Required behavior

### `session/new`

Adapter возвращает Cogerentor-safe config options:

- `model` как `select`;
- `effort` как `select`;
- `mode` как `select`;
- `toolEvents` как `select`, если `PRD-009` реализован.

Adapter не возвращает:

- `type = "number"`;
- `type = "boolean"`;
- custom variant types;
- object-valued `currentValue` в advertised config option.

### Tool byte limits

Tool byte limits применяются из:

1. CLI flags;
2. environment variables;
3. defaults.

Если session-level update поддержан, он применяется через
`session/set_config_option`, но не рекламируется как number option.

### ACP prompt lifecycle

Adapter завершает prompt при одном из событий:

- Claude turn завершился terminal assistant stop reason;
- caller context cancelled;
- Claude process/transport failed;
- explicit ACP cancellation.

Adapter не завершает ACP prompt по собственному fixed-duration deadline.

## Architecture

### Protocol layer

`internal/acp` отвечает за JSON wire types и dispatch.

Contract:

- `SessionConfigOption` остаётся совместимым с production SDK;
- новые variants добавляются только после compatibility verification;
- process test защищает `session/new` response shape.

### App layer

`internal/app` отвечает за:

- session config defaults;
- advertised config option list;
- accepted `session/set_config_option` values;
- timeout propagation в transport options.

Contract:

- `Options.Timeout = 0` означает caller-owned timeout;
- app layer не подставляет production default timeout для ACP mode.

### Claude transport layer

`internal/claude` отвечает за:

- Claude/tmux lifecycle;
- prompt delivery;
- transcript completion;
- context-aware waits.

Contract:

- `Options.Timeout = 0` означает use caller context directly;
- positive `Options.Timeout` создаёт adapter-owned bounded turn context;
- `query` mode может передавать positive timeout.

### CLI layer

`cmd/claude-acp-adapter` отвечает за:

- ACP mode;
- manual query mode;
- process-level options.

Contract:

- ACP mode не задаёт prompt timeout default;
- query mode задаёт `--timeout` default.

## Acceptance Criteria

- `session/new.configOptions` содержит только `type = "select"` для advertised
  options.
- `session/new.configOptions` не содержит `type = "number"`.
- `toolInputMaxBytes` и `toolResultMaxBytes` не рекламируются как number
  options.
- `toolEvents` может рекламироваться как select option.
- Numeric tool settings читаются из CLI flags.
- Numeric tool settings читаются из environment variables.
- Adapter может принимать numeric `session/set_config_option`, если client явно
  отправляет request.
- Process test проверяет, что `session/new` response декодируется SDK-safe
  shape.
- Unit test проверяет отсутствие unsupported variants в `session/new`.
- ACP service default timeout равен caller-owned mode.
- `app.NewService(app.Options{})` не создаёт 90s timeout.
- `claude.NewClient(claude.Options{Timeout: 0})` не создаёт 90s prompt timeout.
- `runTurn` при `Timeout = 0` использует caller context.
- `query` command сохраняет `--timeout` и default `90s`.
- Cogerentor `reviewer-claude` проходит `session/new`.
- Cogerentor stage duration контролируется stage timeout, а не adapter default.

## Testing

Обязательные проверки:

```bash
go test ./...
go test -race ./...
go vet ./...
make build
make check
```

Focused checks:

```bash
go test ./internal/app -run 'TestNewSession.*Config|Test.*Timeout' -count=1
go test ./internal/claude -run 'Test.*Timeout|Test.*Context' -count=1
go test ./cmd/claude-acp-adapter -run 'Test.*Timeout|Test.*SessionNew' -count=1
```

Manual Cogerentor smoke:

```bash
cogerentor run resume --run-id <run-id> \
  --agents 'claude-code=/Users/deniszabozhanov/.local/bin/claude-code-cli-acp'
```

Expected smoke result:

- `reviewer-claude` проходит `session/new`;
- log не содержит `invalid variant payload`;
- long review turn живёт дольше 90 секунд при живом stage context.

## Rollout notes

Сначала фиксируется compatibility surface:

1. Убрать number options из advertised `session/new`.
2. Добавить regression test на response shape.
3. Пересобрать adapter binary.
4. Прогнать Cogerentor reviewer stage.

Затем фиксируется timeout ownership:

1. Разделить ACP mode и query mode timeout defaults.
2. Сделать `Timeout = 0` caller-owned value в app и claude layers.
3. Добавить tests.
4. Прогнать long-running Cogerentor stage.

## Risks

- Некоторые future ACP clients смогут поддерживать richer config variants.
  Adapter должен развивать capability-aware negotiation, а не расширять
  advertised surface вслепую.
- Caller-owned timeout требует строгой context propagation. Любой internal wait
  без context снова создаст зависание.
- Full tool output mode может создавать большие JSON-RPC payloads. Byte limits
  остаются process-level safety defaults.

## Out of scope

- Обновление Cogerentor ACP SDK.
- Capability negotiation для всех future config variants.
- Rich UI rendering для tool output.
- Retry policy для reviewers.
- Stage timeout model внутри Cogerentor.
