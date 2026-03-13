# wf diff

Compare two workflow runs side-by-side.

## Synopsis

```
wf diff <run-id-a> <run-id-b> [flags]
```

## Flags

| Flag | Type | Default | Description |
|---|---|---|---|
| `--json`, `-j` | bool | `false` | Output diff as JSON |

## Description

`wf diff` compares two runs of any workflow (they don't need to be the same workflow, though comparing different workflows is usually less useful). It highlights differences in:

- Run-level: status, duration, execution mode, timeout
- Task-level: per-task state, exit code, duration, attempt count
- Context variables: added, removed, changed values

A common use case is comparing a successful run against a failed run to identify what changed.

## Examples

```bash
# Compare two runs
wf diff 2Xk7p9QrVnYoJ1mT3s 2Xk7p0AbCdEfGhIjKl

# JSON output for scripting
wf diff 2Xk7p9QrVnYoJ1mT3s 2Xk7p0AbCdEfGhIjKl --json

# Compare last two runs of a workflow
RUN_A=$(wf runs --workflow cicd-pipeline --limit 2 | awk 'NR==2{print $1}')
RUN_B=$(wf runs --workflow cicd-pipeline --limit 2 | awk 'NR==3{print $1}')
wf diff $RUN_A $RUN_B
```

## Example Output

```
── Run Comparison ─────────────────────────────────────────────
                      RUN A                    RUN B
  ID          2Xk7p9Qr...              2Xk7p0Ab...
  Workflow    cicd-pipeline            cicd-pipeline
  Status      success              →   failed
  Duration    1m 12s               →   2m 41s
  Mode        work-stealing            work-stealing

── Task Differences ───────────────────────────────────────────
  Task                RUN A        RUN B
  lint                ✓ success    ✓ success
  test-unit           ✓ success    ✓ success
  test-integration    ✓ success    ✗ failed       ← CHANGED
  build-linux         ✓ success    ✕ cancelled    ← CHANGED
  build-darwin        ✓ success    ✕ cancelled    ← CHANGED
  build-windows       ✓ success    ✕ cancelled    ← CHANGED
  deploy              ✓ success    ✕ cancelled    ← CHANGED

── Variable Differences ───────────────────────────────────────
  Variable       RUN A             RUN B
  version        "1.4.2"           "1.4.2"
  git_sha        "a3f2b91"    →    "c9e4d22"      ← CHANGED
  test_coverage  "87.4"       →    (not set)      ← REMOVED
```

## See Also

- [wf inspect](inspect.md) — deep dive into a single run
- [wf audit](audit.md) — chronological event trail
- [wf runs](runs.md) — find run IDs to compare
