# PRD-008: Runtime turn lifecycle

## Кратко

`claude-acp-adapter` должен гарантировать, что один ACP `session/prompt`
соответствует одному полностью завершенному Claude turn.

Сейчас adapter использует цепочку:

```text
ACP client -> adapter process -> detached tmux -> Claude CLI -> JSONL transcript
```

В этой цепочке есть три runtime-риска:

- Claude в `tmux` может получить старое окружение вместо окружения текущего
  adapter process;
- adapter может завершить `session/prompt` до фактического конца Claude turn;
- detached `tmux`/Claude session может остаться жить после завершения adapter
  process.

Этот PRD фиксирует продуктовый контракт для надежного runtime lifecycle. Scope
не связан с расширением ACP protocol surface и не добавляет новых пользовательских
возможностей.

## Цель

Сделать runtime adapter детерминированным:

- Claude CLI запускается с окружением текущей ACP session;
- ответ на `session/prompt` возвращается только после завершения именно этого
  prompt turn;
- Stop hook от старого turn не завершает новый turn;
- shutdown adapter чистит все запущенные им `tmux`/Claude sessions;
- аварийные leftovers можно безопасно найти и удалить по ownership marker.

## Почему это нужно

ACP client принимает финальный response `session/prompt` как границу готовности.
После этого client имеет право проверять stage outputs, закрывать subprocess,
запускать следующий этап или агрегировать результат.

Если adapter возвращает response раньше фактического конца Claude turn, система
получает рассинхрон:

```text
adapter response уже ушел
client проверил output files
Claude продолжил выполнять tool call
output file появился позже
client уже зафиксировал ошибку
```

Если Claude получает старое окружение, он может читать и писать не те рабочие
директории, даже когда `cwd` у `tmux` правильный.

Если detached sessions не чистятся, старые Claude processes продолжают держать
ресурсы, писать transcript и принимать lifecycle signals вне контроля ACP
process.

## Scope

В scope входят пять изменений поведения:

1. Явно пробрасывать runtime environment в `tmux new-session`.
2. Ввести turn identity для Stop hook и transcript wait.
3. Завершать prompt только по terminal assistant stop reason текущего turn.
4. Сделать cleanup активных `tmux`/Claude sessions обязательной частью stdin EOF,
   context cancellation и signal shutdown.
5. Добавить ownership marker для аварийной уборки stale sessions.

В scope не входят:

- изменение ACP request/response schemas;
- изменение Claude prompt text;
- новая модельная конфигурация;
- новая интеграция с конкретным ACP client;
- замена `tmux` на другой transport.

## Требуемое поведение

### Environment forwarding

Adapter обязан запускать Claude CLI внутри `tmux` с окружением текущего adapter
process.

Минимально обязательные группы переменных:

- переменные stage/workspace contract, если они присутствуют в adapter process;
- переменные tool/runtime policy, если они присутствуют в adapter process;
- `CLAUDE_CONFIG_DIR`, если он задан adapter options.

Правило: если переменная влияет на то, где agent читает inputs или пишет
outputs, она передается в `tmux new-session` через `-e KEY=VALUE`.

`tmux` global environment не является источником истины для Claude child process.

### Turn identity

Каждый `session/prompt` получает внутренний `turnID`.

`turnID` не обязан быть частью ACP protocol. Он нужен adapter для runtime
корреляции:

- Stop hook payload содержит `turnID`;
- transcript wait принимает только Stop hook payload текущего `turnID`;
- stale Stop hook payload drain не может закрыть следующий prompt;
- diagnostics связывает session id, Claude session id и turn id.

Если Claude hook не может получить explicit `turnID`, adapter должен использовать
эквивалентный monotonic marker: например, transcript byte offset на момент
отправки prompt плюс timestamp/nonce в hook settings.

### Prompt completion

Adapter возвращает `PromptResponse` только после одного из terminal outcomes:

- в transcript текущего turn найден assistant message с terminal stop reason;
- prompt отменен через ACP cancellation;
- transport timeout истек;
- Claude process завершился с ошибкой.

Stop hook сам по себе не является достаточным terminal condition, если последний
assistant event имеет `stop_reason: "tool_use"` или terminal stop reason еще не
прочитан из transcript.

Stop hook можно использовать как сигнал:

- ускорить flush transcript tail;
- определить transcript path;
- начать финальное ожидание terminal assistant record.

Финальный flush window допускается только после terminal assistant stop reason
или после подтвержденного отсутствия active tool use.

### Correction turns

Если ACP client отправляет второй prompt в той же session для исправления output
contract, adapter обязан считать это новым turn:

- старый Stop hook payload игнорируется;
- transcript offset продолжает читаться с актуальной позиции;
- completion второго prompt зависит только от событий второго prompt;
- response второго prompt возвращается после фактического завершения correction
  turn.

### Cleanup

Adapter process обязан закрывать все запущенные им Claude sessions при:

- `session/close`;
- stdin EOF;
- parent context cancellation;
- SIGTERM;
- SIGINT.

Cleanup sequence:

1. Остановить прием новых prompts.
2. Отменить active turns.
3. Отправить Claude `/exit` или equivalent graceful exit.
4. Подождать короткий bounded timeout.
5. Убить `tmux` session, если graceful exit не завершился.
6. Закрыть FIFO/temporary hook resources.
7. Удалить session из active registry.

Cleanup является best-effort, но observable: ошибки пишутся в stderr с
adapter session id, Claude session id, tmux session name и ownership marker.

### Ownership marker

Каждая detached `tmux` session получает marker, по которому adapter может
доказать ownership:

- session name prefix остается adapter-specific;
- environment содержит marker с adapter process id и generated instance id;
- временный marker file содержит tmux session name, Claude session id, cwd и
  createdAt.

На старте adapter может выполнять stale cleanup только для sessions, которые
принадлежат этому adapter по marker и явно старше safe threshold.

## Архитектурное решение

### Transport layer

`internal/claude` отвечает за:

- запуск `tmux`;
- передачу env в `tmux`;
- Stop hook payload;
- transcript tailing;
- completion detection;
- cleanup detached runtime resources.

`internal/claude` не импортирует ACP types.

### App layer

`internal/app` отвечает за:

- active prompt lifecycle;
- session close;
- cancellation state;
- mapping transport result в ACP `PromptResponse`;
- shutdown всех session transport.

### Protocol layer

`internal/acp` отвечает только за JSON-RPC dispatch и protocol errors.

Protocol layer не знает про `tmux`, FIFO, Stop hook и ownership markers.

## Acceptance Criteria

- Claude child process внутри `tmux` получает current process env для declared
  runtime variables.
- Unit test проверяет, что `TmuxSession.Launch` добавляет env через `tmux -e`.
- Unit test проверяет, что Stop hook payload без current `turnID` игнорируется.
- Unit test проверяет, что assistant `stop_reason=tool_use` не завершает prompt.
- Unit test проверяет, что terminal `stop_reason=end_turn` завершает prompt.
- Correction prompt после failed output check завершается только после terminal
  assistant event correction turn.
- `session/close` убирает `tmux` session, FIFO и registry entry.
- stdin EOF запускает cleanup активных sessions.
- SIGTERM запускает cleanup активных sessions.
- Adapter startup может найти stale owned sessions по marker.
- Stale cleanup не трогает чужие `tmux` sessions.
- Manual smoke: после hard-stop adapter не остается active owned `tmux` session
  старше safe threshold.

## Verification

Документальная проверка для этого PRD:

```bash
git diff --check -- docs
```

Проверка будущей реализации:

```bash
make build
make check
```

Дополнительные targeted tests:

- `go test ./internal/claude -run 'Env|Turn|Stop|Cleanup'`;
- `go test ./internal/app -run 'Close|Shutdown|Cancel'`;
- process test для stdin EOF cleanup;
- smoke test с двумя prompt turns в одной session;
- smoke test с искусственным stale Stop hook payload перед вторым prompt.

## Risks

- Claude CLI Stop hook format может измениться; turn completion должен
  опираться на transcript terminal assistant event как главный источник истины.
- `tmux` global environment может содержать stale variables; explicit `-e`
  должен иметь приоритет.
- Жесткое завершение process может прервать cleanup; ownership marker нужен как
  второй контур восстановления.
- Слишком агрессивный stale cleanup может убить ручную Claude session; поэтому
  cleanup должен требовать adapter marker, а не только name prefix.

## Non-goals

- Не реализовывать новую ACP capability.
- Не менять format user prompt.
- Не добавлять client-specific behavior.
- Не удалять `tmux` из архитектуры.
- Не делать cleanup по process name без ownership marker.
