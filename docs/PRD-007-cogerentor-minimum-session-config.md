# PRD-007: Cogerentor minimum session config

## Кратко

`claude-acp-adapter` должен закрыть минимальный достаточный ACP-контур для
работы в Cogerentor как agent `claude-code`.

Текущий блокер проявился на stage `plan_review`:

```text
set session model "claude-opus-4-8": {"code":-32601,"message":"method not found"}
```

Cogerentor уже умеет вести ACP session lifecycle и ожидает, что agent:

- объявляет session config options в `session/new`;
- принимает `session/set_model`;
- принимает `session/set_config_option`;
- поддерживает config ids `model`, `effort`, `mode`.

Этот PRD покрывает только минимальный набор, достаточный для текущего
Cogerentor runner и текущих профилей. Полная ACP v1 совместимость является
следующим продуктовым scope.

## Цель

Сделать так, чтобы Cogerentor мог выполнить stage с `agent: claude-code` и
`agentConfig` без падения на session configuration.

Минимальный успешный сценарий:

```text
initialize
session/new
session/set_model
session/set_config_option model|effort|mode
session/prompt
session/update
PromptResponse
```

## Почему это нужно

Cogerentor берёт настройки агента из профиля:

```yaml
agent: claude-code
agentConfig:
  model: claude-opus-4-8
  reasoning: high
```

Дальше Cogerentor настраивает ACP session до prompt:

- `model` сначала отправляется через `session/set_model`;
- если `session/set_model` вернул ошибку, `model` отправляется через
  `session/set_config_option`;
- `reasoning` отправляется как `session/set_config_option` с config id
  `effort`;
- permission mode для `claude-code` получает default `auto` и отправляется как
  `session/set_config_option` с config id `mode`.

Значит adapter должен поддерживать эти методы и объявить эти options в
`session/new`, чтобы клиент видел понятный session config contract.

## Источники

- Cogerentor session configuration:
  `/Users/neiro/dev/cogerentor/apps/engine/engine/internal/agent/acp/runner.go`
- Cogerentor `claude-code` permission default:
  `/Users/neiro/dev/cogerentor/apps/engine/engine/internal/worker/agent_adapter.go`
- Наш текущий ACP dispatcher:
  `internal/acp/server.go`
- Наш текущий `NewSessionResponse` уже содержит поля `configOptions` и `modes`:
  `internal/acp/types.go`

## Scope

В scope входят только четыре изменения поведения:

1. Заполнять `configOptions` в `session/new`.
2. Заполнять `modes` в `session/new`.
3. Реализовать `session/set_model`.
4. Реализовать `session/set_config_option` для `model`, `effort`, `mode`.

## Требуемое поведение

### `session/new`

`session/new` возвращает обычный `sessionId` и дополнительно объявляет session
configuration surface.

Минимальные `configOptions`:

- `model` с category `model`;
- `effort` с category `thought_level`;
- `mode` с category `mode`.

Минимальные `modes`:

- `auto`.

Если ACP SDK ожидает option values для select options, adapter отдаёт значения,
которые Cogerentor использует в текущих профилях:

- model values:
  - `claude-opus-4-8`;
  - `claude-sonnet-4-6`;
- effort values:
  - `low`;
  - `medium`;
  - `high`;
- mode values:
  - `auto`.

Adapter может принимать значения вне этого списка, если Claude Code CLI умеет
их принять. Список в response нужен как contract hint для клиента, а не как
жёсткая продуктовая модельная матрица.

### `session/set_model`

Request:

```json
{
  "method": "session/set_model",
  "params": {
    "sessionId": "...",
    "modelId": "claude-opus-4-8"
  }
}
```

Поведение:

- session существует;
- active prompt для session отсутствует;
- `modelId` сохраняется в session config;
- следующий Claude transport prompt запускается с этой моделью;
- response: `{}`.

### `session/set_config_option`

Request:

```json
{
  "method": "session/set_config_option",
  "params": {
    "sessionId": "...",
    "configId": "effort",
    "value": "high"
  }
}
```

Поддерживаемые config ids:

- `model`;
- `effort`;
- `mode`.

Поведение по config id:

- `model`: эквивалентно `session/set_model`;
- `effort`: сохраняется в session config как reasoning effort metadata;
- `mode`: сохраняется в session config как permission mode metadata.

Для текущего Claude transport:

- `model` влияет на запуск Claude CLI через `--model`;
- `effort` сохраняется для совместимости с Cogerentor и будущего mapping;
- `mode=auto` совместим с текущим default permission behavior адаптера.

Response: `{}`.

## Ошибки

Adapter возвращает protocol error в этих случаях:

- unknown session;
- config mutation во время active prompt turn;
- пустой `modelId`;
- пустой `configId`;
- пустой `value`;
- unsupported `configId`.

Ошибки должны быть стабильными и читаться в Cogerentor log без дополнительного
контекста.

## Архитектурное решение

Session config принадлежит orchestration layer.

Ожидаемая раскладка:

- `internal/acp` добавляет wire types и dispatch для двух методов;
- `internal/app` хранит session config в `Session`;
- `internal/app` валидирует, что config меняется до prompt;
- `internal/claude` получает уже resolved `claude.Options`;
- `internal/claude` не импортирует ACP types.

Ключевой инвариант: успешный `session/set_model` означает, что следующий prompt
пойдёт в Claude Code с этой моделью.

## Acceptance Criteria

- `session/new` возвращает `configOptions` для `model`, `effort`, `mode`.
- `session/new` возвращает `modes` с `auto`.
- `session/set_model` успешно применяет `claude-opus-4-8`.
- `session/set_config_option` успешно применяет `model`.
- `session/set_config_option` успешно принимает `effort=high`.
- `session/set_config_option` успешно принимает `mode=auto`.
- Cogerentor run доходит дальше session configuration на stage
  `plan_review`.
- Ошибка `method not found` для `session/set_model` исчезает.
- Unit tests покрывают dispatcher и app service behavior.
- Process/stdin-stdout test покрывает последовательность:
  `initialize -> session/new -> session/set_model`.

## Out of Scope

- Полная ACP v1 compatibility matrix.
- `session/load`, `session/resume`, `session/list`, `session/delete`.
- Terminal methods.
- Client filesystem callbacks.
- Permission request callbacks.
- Usage accounting.
- Rich tool/diff updates.
- Runtime смена модели во время active prompt.

## Verification

Документальная проверка:

```bash
git diff --check -- docs/PRD-007-cogerentor-minimum-session-config.md
```

Проверки будущей реализации:

```bash
make build
make check
```

Cogerentor smoke:

```bash
/Users/neiro/dev/cogerentor/scripts/cogerentor-dev agents \
  --agents 'claude-code=~/.local/bin/claude-code-cli-acp'
```

End-to-end proof берётся из профиля, где `claude-code` имеет
`agentConfig.model`, например `feature-custom-models` stage `plan_review`.
