# Your First Workflow

This guide walks through every step of creating, validating, running, and inspecting a workflow. It introduces the most important features one at a time.

## Prerequisites

- `wf` binary built and on your `PATH`
- `wf init` run at least once

## Step 1 — Create a workflows directory

```bash
mkdir ~/workflows
export WF_PATHS_WORKFLOWS=~/workflows
```

Or add it to your config:

```yaml
# ~/.config/workflow/config.yaml
paths:
  workflows: /home/alice/workflows
```

## Step 2 — Write the workflow file

```bash
cat > ~/workflows/first.toml <<'EOF'
name        = "first"
description = "My first wf workflow"

# Step 1: capture some facts about the environment
[tasks.gather-info]
cmd = """
echo "Host: $(hostname)"
echo "User: $(whoami)"
echo "Date: $(date +%Y-%m-%d)"
echo "$(date +%Y-%m-%d)"
"""
register = "run_date"

# Step 2: do some work (two independent tasks)
[tasks.task-a]
cmd        = "sleep 1 && echo 'Task A done'"
depends_on = ["gather-info"]
register   = "result_a"

[tasks.task-b]
cmd        = "sleep 1 && echo 'Task B done'"
depends_on = ["gather-info"]
register   = "result_b"

# Step 3: aggregate results
[tasks.summary]
cmd        = """
echo "Run date : {{.run_date}}"
echo "Task A   : {{.result_a}}"
echo "Task B   : {{.result_b}}"
"""
depends_on = ["task-a", "task-b"]
EOF
```

## Step 3 — Validate

```bash
wf validate first
# ✓ first — 4 tasks, no cycles detected
```

Validation checks:

- TOML syntax
- Required fields
- No duplicate task IDs
- All `depends_on` targets exist
- No cycles

## Step 4 — Visualise the DAG

```bash
wf graph first
```

```
[gather-info]
  ├── [task-a]
  │     └── [summary]
  └── [task-b]
        └── [summary]
```

`task-a` and `task-b` are at the same topological level — they have no dependency on each other.

```bash
wf graph first                   # ASCII output (default)
wf graph first --format html     # writes first_graph.html — open in browser
```

## Step 5 — Run it

### Sequential (default)

```bash
wf run first --print-output
```

`task-a` runs, then `task-b`, then `summary` — one at a time. Total: ~2 seconds.

### Parallel

```bash
wf run first --parallel --print-output
```

`task-a` and `task-b` run simultaneously. Total: ~1 second.

## Step 6 — Find the run ID

```bash
wf runs
# RUN ID                   WORKFLOW   STATUS    DURATION
# 2Xk7p9QrVnYoJ1mT3s...   first      success   1.1s
```

```bash
RUN_ID=$(wf runs --limit 1 | awk 'NR==2{print $1}')
```

## Step 7 — Inspect the run

```bash
wf inspect $RUN_ID
```

Look for the **Variables** section — `run_date`, `result_a`, `result_b` should all be listed with their captured values.

```bash
wf logs $RUN_ID
wf logs $RUN_ID task-a
```

## Step 8 — Audit trail

```bash
wf audit $RUN_ID
```

Every state transition is recorded with a timestamp.

## Step 9 — Add a failure handler

Edit the workflow to add a notification when something goes wrong:

```toml
name       = "first"
on_failure = "on-error"

# ... existing tasks ...

[tasks.on-error]
type           = "forensic"
cmd            = """
echo "Something went wrong!"
echo "Failed task  : {{.failed_task}}"
echo "Error output : {{.error_message}}"
"""
ignore_failure = true
```

To test it, add a task that fails:

```toml
[tasks.might-fail]
cmd        = "exit 1"
depends_on = ["gather-info"]
```

```bash
wf run first --print-output
# ... on-error forensic task fires ...

wf runs --status failed
RUN_ID=$(wf runs --status failed --limit 1 | awk 'NR==2{print $1}')
wf resume $RUN_ID
```

## What you've learned

- Workflow file structure (`name`, `cmd`, `depends_on`, `register`)
- Variable capture (`register`) and interpolation (`{{.varname}}`)
- Parallel execution with `--parallel`
- DAG visualisation with `wf graph`
- Forensic failure handlers with `type = "forensic"` and `on_failure`
- Resuming failed runs with `wf resume`

## Next Steps

- [Parallel Execution](parallel-execution.md) — deep dive into execution modes
- [Error Handling & Retries](error-handling.md) — retries, timeouts, and `ignore_failure`
- [The Saga Pattern](saga-pattern.md) — compensating transactions
- [Matrix Expansion](../concepts/matrix.md) — fan-out over parameters
