# Example 02 — Data ETL

**File**: `files/examples/02-data-etl.toml`
**Industry**: Data Engineering
**Tags**: `etl`, `nightly`, `data`

## Features Demonstrated

- Three parallel data source extractions
- `register` capturing row counts
- `if` conditional on total row count (skip alert if data looks normal)
- `retries` + `retry_delay` on transform (network-sensitive)
- `working_dir` for consistent report output path
- Global `on_failure` forensic handler
- Runtime `--var` for warehouse schema target
- `timeout` per stage

## Why this pattern matters

Nightly ETL jobs are among the most common failure points in a data organisation: they fail silently, produce partial data, or succeed on corrupted input — and the data team discovers it at 9am when a dashboard shows yesterday as blank.

Running extractions in parallel cuts wall-clock time, but the more important property is that each source registers its own row count. The `if` condition on `total_rows` is evaluated against live pipeline output, not a hardcoded threshold in a cron script. If the CRM goes dark and returns 0 rows, the alert fires from the same run that produced the bad data — with the full context of which source failed, at what step, and with what output — accessible via `wf inspect` and `wf logs`.

The global `on_failure` forensic handler means an engineer on call gets notified with the run ID they need to `wf resume` from, not a cryptic cron email.

## Pipeline Structure

```
[init]
  ├── [extract-crm]      ─┐
  ├── [extract-erp]       ├→ [transform] → [load] → [validate] → [generate-report]
  └── [extract-analytics]─┘
                                                         (if total_rows < threshold)
                                                         → [send-alert]
```

Global forensic: `[notify-failure]`

## Run Commands

```bash
# With custom warehouse schema
wf run data-etl --var WAREHOUSE_SCHEMA=staging_test --parallel --print-output

# Nightly production run
wf run data-etl \
  --var WAREHOUSE_SCHEMA=analytics_prod \
  --work-stealing \
  --timeout 2h \
  --print-output

# Visualise
wf graph data-etl
```

## What to Observe

- `extract-crm`, `extract-erp`, `extract-analytics` start simultaneously — verify with `wf audit`
- `transform` waits for all three to complete before starting
- `wf inspect` shows `crm_rows`, `erp_rows`, `analytics_rows`, `total_rows` variables
- `send-alert` is skipped if `total_rows` is above the threshold — check `wf inspect` to see the condition result

## Inspect After Running

```bash
RUN_ID=$(wf runs --tag etl --limit 1 | awk 'NR==2{print $1}')
wf inspect $RUN_ID        # row count variables
wf logs    $RUN_ID transform  # transform task output

# Compare two ETL runs
RUN_A=$RUN_ID
wf run data-etl --var WAREHOUSE_SCHEMA=staging_test --parallel
RUN_B=$(wf runs --tag etl --limit 1 | awk 'NR==2{print $1}')
wf diff $RUN_A $RUN_B
```
