# Development model

This document captures a general development model that can be reused across
projects. The model fits adapters, local runtime projects, monorepositories, and
product tools where the contract, observability, and verifiable results matter.

## 1. Main principle

Work moves from meaning to contract, then to implementation:

1. Formulate the problem and expected result.
2. Describe the direction in a `PLAN`.
3. Lock the exact contract in a `PRD`.
4. Implement a small vertical slice.
5. Verify the code, documentation, and user scenario.
6. Record evidence: which commands were run, what result was received, and which
   area remained outside the run.

Documents are a working synchronization tool. They explain why the system changes
in this specific way, which contract must remain true, and how to understand that
the task is actually closed.

## 2. Document types

| Type | Purpose | When to write |
|---|---|---|
| `PLAN-NNN` | Direction and stages | There is a large initiative, several options, and open decisions |
| `PRD-NNN` | Exact contract | The direction is clear and requirements, API, states, and acceptance need to be fixed |
| `NOTES-NNN` | Fast synchronization | A decision, question, or tradeoff needs to be passed to another person or agent |
| `WRITEUP-*` | Explanation or runbook | A working model, launch method, diagnostics flow, or team rules need to be described |

Normal flow:

```text
PLAN -> direction alignment -> PRD -> implementation -> verification -> PR/MR
```

For a small task, a `PRD` can appear without a separate `PLAN`, but the document
status must explicitly say that it is a draft or PRD candidate.

## 3. PLAN

A `PLAN` answers the question: how to proceed.

Structure:

- context;
- current system;
- gap;
- solution options;
- selected approach;
- delivery stages;
- risks;
- checks;
- what remains outside the scope.

A good `PLAN` does not try to lock every signature. It helps make the road visible
and align the contentious points before the detailed contract.

## 4. PRD

A `PRD` answers the question: what exactly must be true after delivery.

Structure:

- goal;
- scope and out of scope;
- current point;
- product requirements;
- domain entities and states;
- protocol/API contract;
- lifecycle;
- failure semantics;
- observability;
- acceptance criteria;
- verification commands;
- open decisions.

A PRD uses the language of results. After reading a PRD, it should be clear how to
verify the feature without reading the whole implementation.

## 5. NOTES

`NOTES` is a short async document.

Structure:

- context;
- what was discovered or proposed;
- options;
- recommendation;
- what response is needed.

`NOTES` is useful when several people or agents work in parallel and need shared
meaning without reverse engineering the diff.

## 6. Architectural style

Recommended invariants:

- contract-first for APIs, protocols, and cross-package boundaries;
- explicit separation of transport, protocol, application/orchestration,
  domain/core, storage, and UI;
- dependency direction flows from top to bottom;
- core expands through adapters instead of importing infrastructure;
- provider-specific types are mapped into internal domain/runtime types at the
  boundary;
- one file has one main reason to change;
- shared code moves to the project level when several places genuinely need it;
- compatibility is preserved only when it is an explicit requirement.

## 7. Implementation style

Working rules:

- read the existing code and documents first;
- build a minimal vertical slice;
- keep the change near the owner of the behavior;
- choose the standard library when it solves the task cleanly;
- add interfaces only when there is a real caller/test boundary;
- write comments for non-obvious invariants;
- record future work in a PLAN/PRD/issue, not in random TODOs;
- update documentation together with contract changes.

## 8. Evidence and checks

Every meaningful change ends with checks.

Minimum report:

```text
Checks:
- command 1 — result
- command 2 — result

Outside the run:
- area that remained manual, expensive, or dependent on external auth
```

Typical checks:

| Change | Checks |
|---|---|
| Markdown docs | `git diff --check`, markdown lint, diagram lint when diagrams are present |
| Go code | `gofmt`, `go test ./...`, `go test -race ./...`, `go vet ./...` |
| Protocol/API | schema validation, contract tests, generated client update |
| Runtime/agent | isolated probe, real smoke, cleanup check |
| UI | typecheck, lint, browser smoke, screenshots for important screens |

Evidence must be reproducible. Commands and their results matter more than the
general phrase "verified."

## 9. Review model

Review looks at risks first:

- contract regression;
- layer violation;
- race/cancellation/resource leak;
- weak verifiability;
- observability loss;
- unclear failure state;
- UI or CLI hiding the real system state;
- documentation promising more than what is implemented.

A good review starts with findings and file/line links. Questions, residual
risks, and a short summary come after that.

## 10. Reusable checklist

For a new project, the short form can be reused:

1. `PLAN` selects the direction.
2. `PRD` locks the contract.
3. Implementation proceeds through a small vertical slice.
4. Layers preserve strict boundaries.
5. The final result contains verification evidence and the area that remained
   outside the run.

This model scales because the main unit of management is a verifiable contract,
not code size.
