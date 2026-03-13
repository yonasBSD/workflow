# wf audit

Show the chronological audit trail of significant events for a workflow run.

## Synopsis

```
wf audit <run-id> [flags]
```

## Flags

| Flag | Type | Default | Description |
|---|---|---|---|
| `--json`, `-j` | bool | `false` | Output as JSON |

## Description

Every state transition in a workflow run is recorded as an immutable audit event. `wf audit` presents these events in chronological order, showing exactly what happened and when.

The audit trail is append-only — records cannot be modified or deleted after creation.

## Event Types

| Event | Description |
|---|---|
| `run_started` | Workflow execution began |
| `task_started` | A task transitioned to `running` |
| `task_success` | A task completed with exit code 0 |
| `task_failed` | A task exited with non-zero code |
| `task_skipped` | A task's `if` condition evaluated to false |
| `task_cancelled` | A task was cancelled because a dependency failed |
| `task_retrying` | A task is about to be retried |
| `variable_registered` | A task registered a variable via `register` |
| `forensic_triggered` | A forensic handler was invoked |
| `run_success` | All tasks completed successfully |
| `run_failed` | The run ended in failure |
| `run_cancelled` | The run was cancelled (Ctrl+C or timeout) |
| `run_resumed` | A resume operation began |

## Examples

```bash
wf audit 2Xk7p9QrVnYoJ1mT3s

# JSON output
wf audit 2Xk7p9QrVnYoJ1mT3s --json

# Show only failure-related events
wf audit 2Xk7p9QrVnYoJ1mT3s --json | jq '.[] | select(.event | startswith("task_fail"))'

# Show variable registration events
wf audit 2Xk7p9QrVnYoJ1mT3s --json | jq '.[] | select(.event == "variable_registered")'
```

## Example Output

```
2026-03-09 14:22:01.003   run_started          cicd-pipeline
2026-03-09 14:22:01.021   task_started         lint
2026-03-09 14:22:04.302   task_success         lint               (3.28s)
2026-03-09 14:22:04.312   task_started         test-unit
2026-03-09 14:22:04.318   task_started         test-integration
2026-03-09 14:22:08.402   variable_registered  version = "1.4.2"  (by: init)
2026-03-09 14:22:12.819   task_success         test-unit          (8.51s)
2026-03-09 14:22:15.730   task_success         test-integration   (11.41s)
2026-03-09 14:22:15.741   task_started         build-linux
2026-03-09 14:22:15.744   task_started         build-darwin
2026-03-09 14:22:15.748   task_started         build-windows
2026-03-09 14:22:30.901   task_success         build-darwin       (15.16s)
2026-03-09 14:22:31.043   task_success         build-linux        (15.30s)
2026-03-09 14:22:31.872   task_success         build-windows      (16.12s)
...
2026-03-09 14:23:13.402   run_success          cicd-pipeline      (1m12.4s)
```

## See Also

- [wf inspect](inspect.md) — structured run details
- [wf diff](diff.md) — compare two runs
- [Security Model](../../security/model.md) — audit trail as a security control
