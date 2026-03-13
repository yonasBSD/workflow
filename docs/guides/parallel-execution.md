# Parallel Execution

This guide explains when and how to use `wf`'s three execution modes, with concrete examples showing the timing differences.

## Choosing an Execution Mode

```
Is task order critical due to shared resources?
  YES → Sequential (default)
  NO  → Do tasks in different branches finish at very different times?
          YES → Work-Stealing (maximum throughput)
          NO  → Parallel (predictable, level-gated)
```

## Sequential

The default. Safe, simple, one task at a time.

```bash
wf run my-workflow
```

Use when:
- Tasks share a database connection or file handle
- Tasks have side effects that must not overlap
- You're debugging and want a clear, linear log

## Parallel (`--parallel`)

```bash
wf run my-workflow --parallel
wf run my-workflow --parallel --max-parallel 8
```

Within each topological level, all tasks are dispatched concurrently. The next level only starts after every task in the current level has settled.

### Example: CI pipeline

```toml
name = "ci"

[tasks.lint]
cmd = "golangci-lint run ./..."

[tasks.test-unit]
cmd        = "go test ./internal/..."
depends_on = ["lint"]

[tasks.test-integration]
cmd        = "go test ./tests/integration/..."
depends_on = ["lint"]

[tasks.test-e2e]
cmd        = "go test ./tests/e2e/..."
depends_on = ["lint"]

[tasks.build]
cmd        = "go build -o app ."
depends_on = ["test-unit", "test-integration", "test-e2e"]
```

Levels:
- Level 0: `lint`
- Level 1: `test-unit`, `test-integration`, `test-e2e` (run in parallel)
- Level 2: `build` (runs after all three tests complete)

```bash
wf run ci --parallel --max-parallel 3 --print-output
```

## Work-Stealing (`--work-stealing`)

```bash
wf run my-workflow --work-stealing
wf run my-workflow --work-stealing --max-parallel 16
```

Tasks are dispatched the moment their specific dependencies complete. There is no level barrier. A task at level 3 can start before all tasks at level 2 have finished, as long as its own deps are done.

### Example: Data pipeline with uneven branches

```toml
name = "pipeline"

[tasks.fetch]
cmd      = "python fetch.py"
register = "rows"

[tasks.parse-fast]
cmd        = "python parse_fast.py"  # takes 2s
depends_on = ["fetch"]

[tasks.parse-slow]
cmd        = "python parse_slow.py"  # takes 15s
depends_on = ["fetch"]

[tasks.index]
cmd        = "python index.py"       # takes 3s, depends only on fetch
depends_on = ["fetch"]

[tasks.merge]
cmd        = "python merge.py"
depends_on = ["parse-fast", "parse-slow"]

[tasks.report]
cmd        = "python report.py"
depends_on = ["merge", "index"]
```

**Parallel mode timeline** (level-gated):

```
t=0   fetch
t=1   parse-fast, parse-slow, index start
t=3   parse-fast done, index done  — both WAIT for parse-slow
t=16  parse-slow done              — merge and report can now start
t=17  merge done
t=18  report done
Total: 18s
```

**Work-stealing timeline** (dependency-driven):

```
t=0   fetch
t=1   parse-fast, parse-slow, index all start
t=3   parse-fast done → merge starts immediately (doesn't wait for parse-slow)
t=4   index done (report is still waiting for merge)
t=5   merge done → report starts
t=6   report done (doesn't wait for parse-slow)
t=16  parse-slow done
Total: 16s
```

Work-stealing is 11% faster here. The difference is larger with more uneven branch durations.

## `--max-parallel`

Global cap on the number of simultaneously running tasks. Applies to both `--parallel` and `--work-stealing`.

```bash
# Limit to 2 concurrent tasks (e.g., on a machine with 2 cores)
wf run heavy-workflow --parallel --max-parallel 2

# Use all available cores
wf run heavy-workflow --work-stealing --max-parallel $(nproc)
```

Default: `4`. Setting it too high on a resource-constrained machine can cause tasks to compete for CPU/memory and actually slow down the run.

## `--timeout`

Wall-clock limit for the entire run. When it fires, all running task processes are killed via `SIGKILL` and the run is marked `cancelled`.

```bash
wf run etl --parallel --timeout 1h
wf run quick-smoke --parallel --timeout 5m
```

Retry delays are context-aware: a task waiting between retries when the timeout fires is cancelled immediately.

## Verifying Parallelism

Use `wf audit` to see the actual start times of tasks:

```bash
RUN_ID=$(wf runs --limit 1 | awk 'NR==2{print $1}')
wf audit $RUN_ID | grep task_started
```

In a parallel run, you'll see multiple `task_started` events with the same or nearly identical timestamps.

## Mixing `--print-output` with Parallel

With `--print-output`, each task's output is buffered and printed atomically after it completes. In parallel mode, this means output from different tasks is interleaved by completion time, not start time — each task's output block is always complete and uninterrupted.

```bash
wf run ci --parallel --print-output
# [lint]             golangci-lint: no issues
# [test-unit]        ok  github.com/...  4.21s
# [test-integration] ok  github.com/...  11.40s
# (each block is complete before the next starts)
```
