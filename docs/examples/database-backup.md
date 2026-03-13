# Example 03 — Database Backup

**File**: `files/examples/database-backup.toml`
**Industry**: Database Administration / SRE
**Tags**: `backup`, `daily`, `ops`

## Features Demonstrated

- `matrix` expansion — three databases in one task definition
- `clean_env = true` — backup tasks run in isolation
- Per-matrix-node `on_failure` forensic handlers
- `register` capturing per-database checksums
- `working_dir` for consistent backup output
- `retries` on verification tasks
- `timeout` per stage

## Why this pattern matters

A backup script that runs three databases sequentially loses the advantage of independent I/O parallelism and hides individual failures behind a single exit code. If the MySQL dump fails but PostgreSQL and Redis succeed, you want to know *which one* failed and have a compensating alert fire for *that* database — not a generic "backup failed" notification that leaves you guessing which databases you can trust.

The matrix expansion means one task definition backs up all three databases, each node independent in the DAG and able to run in parallel. Each node registers its own checksum. The forensic `alert-backup-failure` fires per-node — so if only `backup-dump[db=mysql]` fails, only that alert fires. `clean_env = true` ensures that a leaked credential in the environment from one backup process cannot influence another.

## Pipeline Structure

```
[init]
  └── [backup-dump[db=postgres]] ─┐
      [backup-dump[db=mysql]]     ├→ [verify-all] → [upload] → [notify]
      [backup-dump[db=redis]]     ┘
           ↓ (on_failure each)
      [alert-backup-failure]
```

## Run Commands

```bash
# Run all three backups in parallel
wf run database-backup --parallel --print-output

# Work-stealing for maximum throughput
wf run database-backup --work-stealing --print-output

# Visualise matrix expansion
wf graph database-backup --matrix

# Visualise with forensic handlers
wf graph database-backup --forensic
```

## What to Observe

- `wf graph database-backup --matrix` shows three expanded nodes: `backup-dump[db=postgres]`, `backup-dump[db=mysql]`, `backup-dump[db=redis]`
- All three `backup-dump` nodes start simultaneously in parallel mode
- `verify-all` only starts after all three nodes complete
- `wf inspect` shows `checksum_postgres`, `checksum_mysql`, `checksum_redis` variables
- `clean_env = true` on backup tasks — no parent environment inherited

## Inspect After Running

```bash
RUN_ID=$(wf runs --tag backup --limit 1 | awk 'NR==2{print $1}')
wf inspect $RUN_ID
wf logs    $RUN_ID "backup-dump[db=postgres]"
wf logs    $RUN_ID "backup-dump[db=mysql]"
```
