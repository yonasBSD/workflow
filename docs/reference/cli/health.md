# wf health

Check the health of the `wf` system.

## Synopsis

```
wf health [flags]
```

## Flags

| Flag | Type | Default | Description |
|---|---|---|---|
| `--json`, `-j` | bool | `false` | Output as JSON |

## Checks Performed

| Check | Description |
|---|---|
| **Database** | Opens and pings the SQLite database |
| **Workflows directory** | Verifies the directory exists and is readable |
| **Workflow validity** | Validates all `.toml` files in the workflows directory |
| **Stale runs** | Reports runs that have been `running` for more than 1 hour (may indicate a crashed executor) |
| **Log disk usage** | Reports total size of the log directory |
| **7-day success rate** | Percentage of runs in the last 7 days that completed successfully |

## Examples

```bash
wf health
# ✓ Database       reachable  (~/.cache/workflow/workflow.db)
# ✓ Workflows dir  readable   (/home/alice/.config/workflow/workflows — 10 files)
# ✓ Validation     all 10 workflows valid
# ✓ Stale runs     none
# ✓ Log usage      142 MB
# ✓ Success rate   94.2%  (67/71 runs in last 7 days)

# JSON output
wf health --json | jq '.checks[] | select(.status != "ok")'
```

## Exit Codes

| Code | Meaning |
|---|---|
| `0` | All checks passed |
| `1` | One or more checks failed |

This makes `wf health` usable as a liveness/readiness probe in scripts and monitoring systems.

## See Also

- [wf init](init.md) — create missing directories and files
- [Configuration](../../getting-started/configuration.md)
