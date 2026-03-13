# DAG Execution Model

Every `wf` workflow is a **Directed Acyclic Graph** (DAG). Each task is a node; each `depends_on` entry is a directed edge from that task to its prerequisite.

## How the DAG is Built

1. **Parse** — the TOML file is read into a `WorkflowDefinition`
2. **Validate** — duplicate task IDs, self-references, and unknown dependency targets are rejected
3. **Cycle detection** — Kahn's algorithm detects cycles; a workflow with a cycle is rejected at parse time with a clear error message
4. **Topological sort** — tasks are assigned to **levels** (0, 1, 2, …) where level 0 has no dependencies and every higher level only depends on lower levels

## Topological Levels

```
Level 0:   [lint]
Level 1:   [test-unit]   [test-integration]   ← both depend only on lint
Level 2:   [build]
Level 3:   [deploy]
```

Tasks within the same level are **independent of each other** — they can always run in parallel. Tasks at a higher level only start after *all* tasks at the level they depend on have completed successfully.

## Task States

Every node in the DAG moves through a state machine:

```
pending → ready → running → success
                          → failed      (non-zero exit / dependency failed)
                          → skipped     (if condition evaluated false)
```

| State | Meaning |
|---|---|
| `pending` | Waiting for dependencies to complete |
| `ready` | All dependencies succeeded; eligible to run |
| `running` | Currently executing |
| `success` | Exited with code 0 (or `ignore_failure = true`) |
| `failed` | Exited with non-zero code, or a dependency failed |
| `skipped` | The `if` condition evaluated to false |

## Early Failure and Cancellation

When a task fails:

1. Its direct and indirect dependants are marked `cancelled` (they will not run)
2. Tasks at the *same level* that are already running continue to completion
3. If the workflow has a global `on_failure` handler, it fires after the run settles
4. Task-level `on_failure` handlers fire immediately when their specific task fails

`ignore_failure = true` changes this: the task is marked `success` regardless of exit code, and dependants continue normally.

## Cycle Detection

`wf validate` (and `wf run`) will reject a cycle with a detailed error:

```
Error: cycle detected: task-a → task-b → task-c → task-a
```

## Inspecting the DAG

```bash
# Print ASCII DAG to stdout (default)
wf graph my-workflow

# Write interactive HTML file (open in browser)
wf graph my-workflow --format html

# Export as Graphviz DOT
wf graph my-workflow --format dot | dot -Tsvg -o dag.svg

# Mermaid diagram
wf graph my-workflow --format mermaid

# JSON representation (machine-readable)
wf graph my-workflow --format json
```

## Node Identity and Matrix Expansion

When a task has a `matrix` field, the builder expands it into N nodes — one per parameter combination. Each expanded node has a unique ID of the form:

```
task-id[param1=value1,param2=value2]
```

For example, a task `backup` with `matrix = {db = ["postgres", "mysql", "redis"]}` produces:

```
backup[db=postgres]
backup[db=mysql]
backup[db=redis]
```

Each is an independent node in the DAG with its own state, logs, and output. See [Matrix Expansion](matrix.md) for full details.
