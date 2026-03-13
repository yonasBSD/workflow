# wf logs

View task output logs for a workflow run.

## Synopsis

```
wf logs <run-id> [task-id] [flags]
```

- `<run-id>` is required
- `[task-id]` is optional — if omitted, logs from all tasks are shown

## Flags

| Flag | Type | Default | Description |
|---|---|---|---|
| `--follow`, `-f` | bool | `false` | Stream new log lines as they are written (like `tail -f`) |
| `--tail`, `-n` | int | `100` | Show last N lines. `0` means show the entire file. |

## Log Storage

Each task writes its combined stdout+stderr to a dedicated log file:

```
~/.cache/workflow/logs/<run-id>/<task-id>.log
```

Log files are created with mode `0600` (owner read/write only).

## Examples

```bash
# Show logs for all tasks in a run
wf logs 2Xk7p9QrVnYoJ1mT3s

# Show logs for a specific task
wf logs 2Xk7p9QrVnYoJ1mT3s test-integration

# Show the full log (no truncation)
wf logs 2Xk7p9QrVnYoJ1mT3s build --tail 0

# Stream logs while a task is running
wf logs 2Xk7p9QrVnYoJ1mT3s deploy --follow

# Show only the last 50 lines
wf logs 2Xk7p9QrVnYoJ1mT3s transform --tail 50

# Matrix task logs (use the expanded node ID)
wf logs 2Xk7p9QrVnYoJ1mT3s "backup[db=postgres]"
```

## Finding Task IDs

Task IDs come from the TOML section headers (`[tasks.<id>]`). For matrix-expanded tasks, the ID includes the parameter:

```
backup[db=postgres]
backup[db=mysql]
build[os=linux,arch=amd64]
```

Use `wf inspect <run-id>` to see all task IDs for a run.

## Output Format

```
=== lint ===
[14:22:01] Running: golangci-lint run ./...
[14:22:04] ok

=== test-unit ===
[14:22:04] Running: go test -race ./...
[14:22:08] ok   github.com/silocorp/workflow/internal/dag   3.241s
[14:22:09] ok   github.com/silocorp/workflow/internal/executor   4.812s
```

## See Also

- [wf inspect](inspect.md) — structured run details
- [wf status](status.md) — live run status
