# Resume

A failed or interrupted run can be resumed from the point of failure. Already-succeeded tasks are skipped and their registered variables are restored — no re-work.

---

## Basic Usage

```bash
wf resume <run-id>
```

To find the run ID:

```bash
wf runs --status failed --limit 5
```

---

## What Happens During Resume

1. **Reload the workflow TOML** — the current file on disk is re-parsed. If the file has changed since the original run, the new version is used.

2. **Restore the ContextMap** — variable snapshots stored in the database are replayed. Each variable is restored to its most recent snapshot value. Downstream tasks will see the same `{{.varname}}` values they would have seen in the original run.

3. **Pre-mark succeeded tasks** — every task that reached `success` in the original run is immediately marked `NodeStateSuccess`. The executor skips these tasks entirely.

4. **Re-run remaining tasks** — failed, cancelled, and pending tasks are executed using the same level-parallel logic (or the mode specified with `--parallel` / `--work-stealing`).

---

## Execution Mode on Resume

By default, `wf resume` uses the same execution mode as the original run. You can override:

```bash
wf resume <run-id> --parallel
wf resume <run-id> --work-stealing --max-parallel 8
```

---

## Variable Restoration

Variable snapshots are written to the database each time a task completes. On resume, the most recent snapshot for each variable is loaded. This means:

- Variables registered by tasks that already succeeded are available to resumed tasks
- If the original run partially registered a variable (task started but crashed), the last snapshot before the crash is used
- Runtime `--var` values from the original run are not automatically re-applied — pass them again if needed:

```bash
wf resume <run-id> --var TARGET_ENV=production
```

---

## Forensic Task Behaviour on Resume

Forensic tasks (tasks with `type = "forensic"`) are not re-run during resume unless the specific task they are assigned to fails again during the resumed execution.

---

## When Resume is Not Appropriate

| Situation | Recommendation |
|---|---|
| The workflow definition changed in a breaking way (renamed tasks, changed deps) | Start a fresh run instead |
| Side effects from already-completed tasks need to be undone | Implement a rollback workflow and run it separately |
| The failure was in infrastructure (DB down, disk full) and is now fixed | Resume is appropriate — the task will retry against the restored infra |

---

## Resume and the Database

The resume operation reads from and writes to the same run record. The run status transitions from `failed` → `resuming` → `success` (or back to `failed`).

A resumed run creates new `task_execution` records for re-run tasks (with an incremented attempt counter) while keeping the original records for reference.

---

## Example: Partial Pipeline Recovery

```
Original run:
  ✓ lint              (succeeded)
  ✓ test-unit         (succeeded)
  ✗ test-integration  (failed — db connection refused)
  ✗ build             (cancelled — dependency failed)
  ✗ deploy            (cancelled)

After fixing the database:

$ wf resume 2Xk7p9QrVnYoJ1mT3sWdBfHuAeC

Resumed run:
  → lint              (skipped — already succeeded)
  → test-unit         (skipped — already succeeded)
  ✓ test-integration  (re-run — now passes)
  ✓ build             (re-run)
  ✓ deploy            (re-run)

✓ Pipeline completed in 23s
```

---

## Checking Resume Status

```bash
wf status <run-id>      # live polling while running
wf inspect <run-id>     # full details after completion
wf audit   <run-id>     # chronological event trail including resume events
```
