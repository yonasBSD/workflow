# wf inspect

Show detailed information about a workflow run: task outcomes, registered variables, forensic logs, and DAG structure.

## Synopsis

```
wf inspect <run-id> [flags]
```

## Flags

| Flag | Type | Default | Description |
|---|---|---|---|
| `--json`, `-j` | bool | `false` | Output as JSON |

## Output Sections

`wf inspect` displays:

1. **Run metadata** — ID, workflow name, status, start time, duration, execution mode, timeout
2. **Task executions** — per-task: state, exit code, duration, attempt number, log file path
3. **Context variables** — all registered variables and their values at run end, including which task registered each
4. **Forensic logs** — output from forensic trap tasks, crash dumps, timeout records
5. **DAG summary** — total tasks, succeeded, failed, skipped, cancelled

## Examples

```bash
# Full inspection
wf inspect 2Xk7p9QrVnYoJ1mT3s

# JSON for scripting
wf inspect 2Xk7p9QrVnYoJ1mT3s --json

# Extract specific variable values
wf inspect 2Xk7p9QrVnYoJ1mT3s --json | jq '.variables.version'

# Check which tasks failed
wf inspect 2Xk7p9QrVnYoJ1mT3s --json | jq '.tasks[] | select(.state == "failed")'
```

## Example Output

```
Run: 2Xk7p9QrVnYoJ1mT3s
  Workflow  : cicd-pipeline
  Status    : success
  Mode      : work-stealing
  Started   : 2026-03-09 14:22:01
  Duration  : 1m 12s
  Tasks     : 12 total  ✓ 12 success  ✗ 0 failed  ○ 0 skipped

── Tasks ──────────────────────────────────────────────────
  lint               ✓ success   3.2s    attempt 1
  test-unit          ✓ success   8.1s    attempt 1
  test-integration   ✓ success  11.4s    attempt 1
  build-linux        ✓ success  15.2s    attempt 1
  build-darwin       ✓ success  14.8s    attempt 1
  build-windows      ✓ success  16.1s    attempt 1
  push-image         ✓ success   4.3s    attempt 1
  deploy-staging     ✓ success   6.7s    attempt 2   (retried 1x)
  smoke-staging      ✓ success   2.1s    attempt 1
  deploy-prod        ✓ success   7.2s    attempt 1
  smoke-prod         ✓ success   2.3s    attempt 1
  notify             ✓ success   0.4s    attempt 1

── Variables ──────────────────────────────────────────────
  version       = "1.4.2"            (registered by: get-version)
  staging_url   = "https://staging.example.com"   (registered by: deploy-staging)
  git_sha       = "a3f2b91"          (registered by: init)

── Forensic Logs ──────────────────────────────────────────
  (none)
```

## See Also

- [wf audit](audit.md) — chronological event trail
- [wf logs](logs.md) — raw task output
- [wf diff](diff.md) — compare two runs
