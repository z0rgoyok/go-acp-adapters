# ACP local usage

## ACP client mode

Build or run the adapter as a subprocess command:

```bash
go run ./cmd/claude-acp-adapter
```

The default process mode is ACP over stdin/stdout. stdout contains only JSON-RPC
messages, one JSON object per line. Capture stderr for setup and cleanup
diagnostics.

The first slice supports:

- `initialize` with ACP protocol version `1`;
- `session/new` with an absolute `cwd`;
- stdio `mcpServers` forwarded through Claude Code `--mcp-config`;
- `session/prompt` with text and resource-link content blocks;
- final assistant `session/update` notifications;
- `session/cancel` as a notification;
- `session/close` cleanup.

`http` and `sse` MCP servers are rejected until those transports are advertised.

## Timeout ownership

ACP mode delegates timeout control to the caller. The adapter does not impose
its own prompt-turn deadline. Cancellation or timeout arrives through the
caller's context chain:

```
Cogerentor stage context -> ACP request context -> turn context
```

When the caller context is cancelled, the adapter terminates the prompt turn
and returns a transport cancellation error.

Internal waits (`WaitReady`, transcript discovery, transcript tail) all respect
the context. No adapter-owned fixed deadlines are injected for ACP turns.

## Config options compatibility

`session/new.configOptions` advertises only `type: "select"` variants that are
safe for the current production ACP SDK. Numeric settings
(`toolInputMaxBytes`, `toolResultMaxBytes`) are configured through environment
variables or CLI flags and are not advertised in `configOptions`.

Supported advertised options:

| ID | Type | Values |
|---|---|---|
| `model` | select | `claude-opus-4-8`, `claude-sonnet-4-6` |
| `effort` | select | `low`, `medium`, `high` |
| `mode` | select | `auto` |
| `toolEvents` | select | `off`, `compact`, `full` |

Process-level numeric settings:

| Environment variable | CLI flag | Default |
|---|---|---|
| `CLAUDE_ACP_TOOL_INPUT_MAX_BYTES` | `--tool-input-max-bytes` | 4096 |
| `CLAUDE_ACP_TOOL_RESULT_MAX_BYTES` | `--tool-result-max-bytes` | 8192 |
| `CLAUDE_ACP_TOOL_EVENTS` | `--tool-events` | compact |

## Developer transport smoke

Use `query` to debug Claude/tmux/transcript behavior without an ACP client:

```bash
go run ./cmd/claude-acp-adapter query -cwd /tmp -timeout 45s -prompt "Reply with exactly one word: OK"
```

`query` mode keeps a local `--timeout` (default `90s`) so it works as a
standalone smoke tool. This is the only mode with an adapter-owned prompt
deadline.

If Claude Code is rate-limited, the command can still return the rate-limit text
from the transcript. That is a valid transport-level result.

## Manual smoke checks

- Start an ACP client subprocess and send `initialize`.
- Create a session with an absolute `cwd`, then send a text `session/prompt`.
- Send `session/cancel` while a long prompt is running and verify the pending
  prompt returns `cancelled`.
- Send `session/close` and verify the session is no longer addressable.
- Stop the process and verify no adapter-owned tmux sessions or FIFO files remain.
