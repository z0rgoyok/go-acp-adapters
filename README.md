# claude-acp-adapter

Go adapter for driving Claude Code through its interactive CLI mode and exposing
it through ACP.

The adapter runs as an ACP stdio server by default. It maps ACP sessions to
interactive Claude Code sessions running inside `tmux`, sends prompts to Claude,
reads Claude's JSONL transcript, and streams assistant text back as ACP
`session/update` notifications.

## Requirements

- Go 1.26+
- `tmux`
- Claude Code CLI available as `claude`

Check the local environment:

```bash
go version
tmux -V
claude --version
```

## Build

Build the binary into `bin/`:

```bash
make build
```

The equivalent raw Go command is:

```bash
go build -o bin/claude-acp-adapter ./cmd/claude-acp-adapter
```

Run the built binary:

```bash
./bin/claude-acp-adapter
```

## Run as an ACP server

From the repository root:

```bash
make run
```

Or run the built binary:

```bash
./bin/claude-acp-adapter
```

The server reads newline-delimited ACP JSON-RPC messages from stdin and writes
responses and notifications to stdout. Diagnostics go to stderr, so stdout stays
reserved for protocol messages.

ACP mode uses the caller's request context for prompt deadlines and
cancellation. The adapter does not install its own prompt-turn timeout in server
mode.

## Direct Claude transport smoke

Use the `query` subcommand to exercise the Claude transport without an ACP
client:

```bash
make query
```

The equivalent raw command is:

```bash
go run ./cmd/claude-acp-adapter query -cwd /tmp -prompt "Reply with exactly one word: OK"
```

The prompt can also come from stdin:

```bash
echo "Reply with exactly one word: OK" | go run ./cmd/claude-acp-adapter query
```

`query` flags:

```text
-cwd                    working directory for Claude
-model                  Claude model
-prompt                 prompt text; stdin is used when empty
-timeout                query timeout, default 90s
-tool-events            tool event update mode: off, compact, full
-tool-input-max-bytes   max bytes kept from tool input in ACP updates
-tool-result-max-bytes  max bytes kept from tool result in ACP updates
```

The `query` subcommand keeps its own default timeout because it is a standalone
developer smoke tool.

## Architecture

The runtime is split into three layers:

- `cmd/claude-acp-adapter` contains the CLI entry point.
- `internal/acp` owns ACP JSON-RPC types, request dispatch, and notifications.
- `internal/app` maps ACP sessions and prompt turns to Claude transport calls.
- `internal/claude` owns the Claude Code interactive transport through `tmux`,
  Stop hook FIFO handling, transcript discovery, transcript parsing, and
  cancellation.

Claude is launched with:

- a generated `--session-id`;
- `--permission-mode bypassPermissions` by default;
- a `Stop` hook that writes to a local FIFO;
- the requested working directory.

The transcript remains the source of truth for returned text. This matters
because some Claude states, such as subscription/session-limit messages, are
written to the transcript even when the Stop hook does not fire.

## ACP surface

The current implementation provides:

- ACP stdio server mode by default;
- `initialize`, `session/new`, `session/prompt`, `session/cancel`, and
  `session/close`;
- caller-owned ACP prompt timeout and cancellation through request context;
- text and resource-link prompt blocks;
- stdio MCP server forwarding through `--mcp-config`;
- direct transport smoke through the `query` subcommand;
- interactive Claude Code launch through `tmux`;
- prompt turn updates through transcript events;
- assistant text extraction from transcript JSONL;
- SDK-safe `session/new.configOptions` using advertised `select` options for
  `model`, `effort`, `mode`, and `toolEvents`;
- unit tests for ACP mapping, session orchestration, transcript parsing, FIFO
  setup, quoting, and settings JSON.

Numeric tool payload limits are process-level settings. They are available as
environment variables and CLI flags, and are intentionally not advertised in
`session/new.configOptions`:

```text
CLAUDE_ACP_TOOL_EVENTS             --tool-events
CLAUDE_ACP_TOOL_INPUT_MAX_BYTES    --tool-input-max-bytes
CLAUDE_ACP_TOOL_RESULT_MAX_BYTES   --tool-result-max-bytes
```

## Development commands

```bash
make help
make build
make check
make smoke
make clean
```

`make check` runs the repository verification loop:

```bash
gofmt -w cmd internal
go test ./...
go test -race ./...
go vet ./...
```

`make smoke` runs:

```bash
go run ./cmd/claude-acp-adapter query -cwd /tmp -timeout 45s -prompt "Reply with exactly one word: OK"
```

If Claude Code is rate-limited, the smoke command still verifies the transport
when it returns the rate-limit text from the transcript.
