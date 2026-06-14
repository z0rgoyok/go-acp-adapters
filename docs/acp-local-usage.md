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

## Developer transport smoke

Use `query` to debug Claude/tmux/transcript behavior without an ACP client:

```bash
go run ./cmd/claude-acp-adapter query -cwd /tmp -timeout 45s -prompt "Reply with exactly one word: OK"
```

If Claude Code is rate-limited, the command can still return the rate-limit text
from the transcript. That is a valid transport-level result.

## Manual smoke checks

- Start an ACP client subprocess and send `initialize`.
- Create a session with an absolute `cwd`, then send a text `session/prompt`.
- Send `session/cancel` while a long prompt is running and verify the pending
  prompt returns `cancelled`.
- Send `session/close` and verify the session is no longer addressable.
- Stop the process and verify no adapter-owned tmux sessions or FIFO files remain.
