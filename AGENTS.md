# Agent Instructions

## Project

This repository is a Go implementation of a Claude Code interactive transport
with an ACP adapter layer.

Work in English for code, comments, and default repository files. Product and
planning documents can be Russian when requested.

## Current Implementation

The implemented slice is:

```text
ACP request -> app orchestration -> tmux -> interactive claude CLI -> JSONL transcript -> ACP updates
```

Use this as context for the existing code shape. New features should be added
through clear package boundaries that match their domain responsibility.

## Development Rules

- Keep Go/source files under 300 lines.
- Documentation files can be longer when the topic needs it.
- Prefer small files with one clear reason to change.
- Keep transport, protocol, and orchestration as separate concerns.
- Put new protocol concepts in packages named after the protocol/domain they
  implement.
- Make dependencies earn their place: prefer the standard library for simple
  needs, and use small established packages for well-known infrastructure.
- Prefer standard library APIs whenever they fit cleanly.
- Keep process, filesystem, and transcript parsing code explicit, observable,
  and testable.
- Add interfaces when they are backed by a concrete caller and make the code
  easier to test or extend.

## Important Files

- `Makefile` — build, run, smoke, and verification entry points.
- `cmd/claude-acp-adapter/main.go` — CLI entry point.
- `internal/acp` — ACP JSON-RPC types, dispatch, responses, and notifications.
- `internal/app` — ACP session orchestration and mapping to Claude transport.
- `internal/claude/client.go` — session orchestration.
- `internal/claude/tmux.go` — `tmux` lifecycle and prompt paste.
- `internal/claude/transcript.go` — JSONL transcript parsing.
- `internal/claude/fifo.go` — Stop hook FIFO handling.
- `internal/claude/session.go` — transcript discovery.
- `internal/claude/types.go` — public transport types.

## Development Commands

Run these before finishing meaningful code changes:

```bash
make build
make check
```

Manual smoke:

```bash
make smoke
```

The smoke is still useful during Claude session limits. A rate-limit message
returned from the transcript is a valid transport-level result.

## Design Guidance

ACP sits above the Claude transport:

```text
ACP adapter -> Claude transport -> tmux/claude/transcript
```

When adding a new capability, first identify the layer it belongs to:

- Claude transport: launching Claude, sending prompts, reading transcript state.
- ACP protocol: sessions, requests, responses, cancellation, and protocol errors.
- Orchestration: mapping protocol requests to Claude transport operations.

## Testing Guidance

Add unit tests for:

- transcript JSON parsing;
- timeout and completion behavior;
- FIFO and Stop hook settings;
- process argument construction;
- ACP message mapping.

Keep real Claude CLI tests as smoke tests because they depend on local auth,
subscription state, and Claude Code availability.
