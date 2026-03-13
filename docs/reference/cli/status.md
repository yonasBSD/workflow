# wf status

Live-poll the status of a running workflow run.

## Synopsis

```
wf status <run-id> [flags]
```

## Flags

| Flag | Type | Default | Description |
|---|---|---|---|
| `--json`, `-j` | bool | `false` | Output one JSON object per poll interval |
| `--interval` | int | `1` | Poll interval in seconds |

## Description

`wf status` polls the database at the configured interval and prints the current state of all tasks. It exits automatically when the run reaches a terminal state (`success`, `failed`, `cancelled`).

This is useful for monitoring a run started in another terminal or by a CI system.

## Output

```
Run: 2Xk7p9QrVnYoJ1mT3s   cicd-pipeline   RUNNING   elapsed: 23s

  ✓ lint               success    3.2s
  ✓ test-unit          success    8.1s
  ⠸ test-integration   running   11.4s
  ○ build              pending
  ○ deploy-staging     pending
  ○ deploy-prod        pending
```

Symbols:

| Symbol | State |
|---|---|
| `✓` | success |
| `✗` | failed |
| `⠸` | running (spinner) |
| `○` | pending |
| `→` | ready |
| `↷` | skipped |
| `✕` | cancelled |

## Examples

```bash
# Monitor in real time
wf status 2Xk7p9QrVnYoJ1mT3s

# Poll every 5 seconds
wf status 2Xk7p9QrVnYoJ1mT3s --interval 5

# Machine-readable stream
wf status 2Xk7p9QrVnYoJ1mT3s --json | jq '.status'

# Block until done (use in scripts)
wf status 2Xk7p9QrVnYoJ1mT3s --json | tail -1 | jq '.status'
```

## See Also

- [wf logs](logs.md) — stream task output
- [wf inspect](inspect.md) — full run details after completion
