# wf resume

Resume a failed or interrupted workflow run from the point of failure.

## Synopsis

```
wf resume <run-id> [flags]
```

## Description

`wf resume` re-executes a run, skipping tasks that already succeeded and re-running tasks that failed, were cancelled, or never started. Registered variables from the original run are restored from database snapshots before any task executes.

## Flags

| Flag | Type | Default | Description |
|---|---|---|---|
| `--parallel` | bool | `false` | Resume with level-based parallel execution |
| `--work-stealing` | bool | `false` | Resume with work-stealing scheduler |
| `--max-parallel` | int | `4` | Maximum concurrent tasks during resume |
| `--var` | string | — | Inject additional runtime variables (`KEY=VALUE`). Repeatable. |
| `--print-output` | bool | `false` | Print task output to terminal after completion |

If neither `--parallel` nor `--work-stealing` is specified, the execution mode defaults to the one used in the original run.

## Finding a Run to Resume

```bash
# List failed runs
wf runs --status failed

# List recent runs for a specific workflow
wf runs --workflow my-pipeline --limit 10
```

## Examples

```bash
# Resume using original execution mode
wf resume 2Xk7p9QrVnYoJ1mT3sWdBfHuAeC

# Resume with a different execution mode
wf resume 2Xk7p9QrVnYoJ1mT3sWdBfHuAeC --parallel --max-parallel 4

# Resume and re-inject a runtime variable
wf resume 2Xk7p9QrVnYoJ1mT3sWdBfHuAeC --var TARGET_ENV=production

# Watch progress while resuming
wf resume 2Xk7p9QrVnYoJ1mT3sWdBfHuAeC --print-output
```

## Behaviour Details

- The workflow TOML is reloaded from disk — if the file changed, the new version is used
- Tasks that reached `success` in the original run are **always skipped**, regardless of any file changes
- Variable snapshots are restored in the order they were captured; each variable gets its most recent value
- The original run record is updated in place (status transitions `failed` → `resuming` → `success`/`failed`)
- New `task_execution` records are created for re-run tasks (attempt counter increments)

## See Also

- [Resume concept](../../concepts/resume.md)
- [wf inspect](inspect.md)
- [wf runs](runs.md)
