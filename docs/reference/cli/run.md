# wf run

Execute a workflow.

## Synopsis

```
wf run <workflow> [flags]
```

`<workflow>` is the workflow name (the `.toml` filename without extension).

## Flags

| Flag | Type | Default | Description |
|---|---|---|---|
| `--parallel` | bool | `false` | Enable level-based parallel execution |
| `--work-stealing` | bool | `false` | Enable work-stealing scheduler (dependency-driven) |
| `--max-parallel` | int | `4` | Maximum number of concurrently running tasks |
| `--timeout` | duration | none | Maximum wall-clock time for the entire run |
| `--var` | string | — | Set a runtime variable (`KEY=VALUE`). Repeatable. |
| `--print-output` | bool | `false` | Print each task's stdout/stderr after it completes |
| `--clean-env` | bool | `false` | Start all tasks with an empty environment (global override) |
| `--dry-run` | bool | `false` | Print the execution plan without running any tasks |
| `--json`, `-j` | bool | `false` | Output the execution plan as JSON (requires `--dry-run`) |

## Execution Modes

Only one of `--parallel` and `--work-stealing` can be specified at a time.

=== "Sequential (default)"

    ```bash
    wf run deploy
    ```

    Tasks run one at a time, level by level.

=== "Parallel"

    ```bash
    wf run deploy --parallel
    wf run deploy --parallel --max-parallel 8
    ```

    Tasks within the same topological level run concurrently.

=== "Work-Stealing"

    ```bash
    wf run deploy --work-stealing
    wf run deploy --work-stealing --max-parallel 16
    ```

    Tasks start as soon as their specific dependencies complete.

## Runtime Variables

```bash
wf run deploy --var TARGET_ENV=production --var VERSION=1.4.2
```

Variables are available as `{{.TARGET_ENV}}` and `{{.VERSION}}` in task commands and `if` conditions.

## Timeout

```bash
wf run etl --timeout 2h
wf run quick-check --timeout 30s
```

Duration syntax: `30s`, `5m`, `1h30m`, `24h`. When the timeout fires, all running tasks are killed and the run is marked `cancelled`.

## Print Output

```bash
wf run my-workflow --print-output
```

By default, task output is written to log files only. With `--print-output`, each task's stdout and stderr are buffered and printed atomically to the terminal after the task completes. Output is capped at 64 KiB per task on the terminal; the full output is always in the log file.

## Dry Run

```bash
wf run my-workflow --dry-run
wf run my-workflow --dry-run --json | jq .
```

Prints the resolved execution plan (task order, dependencies, levels) without executing any task.

## Clean Environment

```bash
wf run my-workflow --clean-env
```

Overrides all tasks in the workflow to start with an empty environment. Individual tasks can also set `clean_env = true` in the TOML.

## Examples

```bash
# Basic run
wf run cicd-pipeline

# Full production run
wf run cicd-pipeline \
  --work-stealing \
  --max-parallel 8 \
  --timeout 30m \
  --print-output \
  --var REGISTRY=ghcr.io/myorg \
  --var TAG=$(git rev-parse --short HEAD)

# Check what would run
wf run cicd-pipeline --dry-run

# Debug mode
wf --log-level debug run my-workflow
```

## Exit Codes

| Code | Meaning |
|---|---|
| `0` | Workflow completed successfully |
| `1` | One or more tasks failed |
| `2` | Workflow definition is invalid |
| `130` | Interrupted (Ctrl+C) |

## See Also

- [Execution Modes](../../concepts/execution-modes.md)
- [Variables & Interpolation](../../concepts/variables.md)
- [wf resume](resume.md)
