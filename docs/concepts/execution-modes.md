# Execution Modes

`wf` provides three execution strategies. All three produce identical *results* — the difference is in how much wall-clock time they take and how tasks are scheduled.

---

## Sequential

**Default mode.** One task runs at a time, level by level.

```bash
wf run my-workflow
```

**How it works:**

1. Start at level 0
2. Run each task in that level, one after another
3. Advance to the next level only after every task in the current level completes
4. Repeat

**When to use:**

- Tasks share resources (a single database connection, a serial port)
- The workflow has a strict side-effecting sequence and parallelism would cause conflicts
- Debugging — easier to follow one task at a time

**Example timing** for a workflow with levels `[lint] → [test-unit, test-integration] → [build]`:

```
lint             (5s)
test-unit        (10s)   ← runs after lint
test-integration (8s)    ← runs after test-unit completes (not parallel!)
build            (4s)
Total: 27s
```

---

## Parallel (Level-Based)

```bash
wf run my-workflow --parallel
wf run my-workflow --parallel --max-parallel 8
```

**How it works:**

1. Process one topological level at a time
2. Within a level, all tasks are dispatched concurrently, bounded by `--max-parallel`
3. The next level only starts after *every* task in the current level has settled

**When to use:**

- Tasks within a level are independent and safe to run simultaneously
- You want predictable, level-gated progress (all tasks at one stage complete before the next begins)
- Safe default for most CI/CD pipelines

**Example timing** (same workflow):

```
lint              (5s)
test-unit         (10s) ┐
test-integration  (8s)  ┘  run simultaneously
build             (4s)
Total: 19s   ← test-unit and test-integration overlap
```

**`--max-parallel`** caps the number of concurrently-running tasks across all levels. Defaults to `4`.

---

## Work-Stealing (Dependency-Driven)

```bash
wf run my-workflow --work-stealing
wf run my-workflow --work-stealing --max-parallel 16
```

**How it works:**

A task becomes **eligible** the moment all of its `depends_on` tasks succeed — not when the entire level completes. A worker goroutine immediately picks it up from the shared queue.

This is fundamentally different from parallel mode: a task at level 3 can start before all tasks at level 2 have finished, as long as its specific dependencies are done.

**When to use:**

- Complex dependency graphs where different branches complete at different times
- Maximum throughput is the goal
- Long-running tasks where waiting for the whole level is wasteful

**Example** — a diamond-plus-extra shape:

```
        [fetch-data]          (2s)
       /             \
[parse-a]           [parse-b]   (10s)   (3s)
       \             /
        [merge]                 (1s)
           |
        [report]                (2s)

[index-cache]   (depends only on fetch-data, 8s)
```

In work-stealing mode:

```
t=0   fetch-data starts
t=2   parse-a, parse-b, index-cache all start immediately
t=5   parse-b done → merge starts immediately (doesn't wait for parse-a)
t=6   merge done → report starts
t=8   report done
t=12  parse-a done
t=12  index-cache done
Total wall time: 12s
```

In parallel mode, `merge` would wait until *both* parse-a and parse-b complete (t=12), and the total would be 15s.

---

## Comparison

| Property | Sequential | Parallel | Work-Stealing |
|---|---|---|---|
| Concurrency | None | Level-gated | Dependency-gated |
| Throughput | Lowest | Medium | Highest |
| Predictability | Highest | Medium | Lower |
| Resource pressure | Lowest | Medium | Highest |
| Best for | Strict serial workflows | Staged pipelines | Complex DAGs |
| Flag | *(default)* | `--parallel` | `--work-stealing` |

---

## `--max-parallel`

Applies to both `--parallel` and `--work-stealing`. Sets the maximum number of tasks that can run simultaneously.

```bash
wf run my-workflow --parallel --max-parallel 2
wf run my-workflow --work-stealing --max-parallel $(nproc)
```

Default is `4`. There is no per-level or per-stage cap — the limit is global across all active tasks.

---

## `--timeout`

Applies to all modes. Sets a wall-clock limit on the entire run. When the timeout expires, all running tasks are killed (via `SIGKILL` on the process group) and the run is marked `cancelled`.

```bash
wf run my-workflow --parallel --timeout 10m
wf run my-workflow --work-stealing --timeout 2h30m
```

Retry delays are context-aware: if the run timeout fires while a task is waiting between retries, the retry is cancelled immediately rather than waiting out the full delay.

---

## Execution Mode Storage

The execution mode used for a run is stored in the database. When you `wf resume <run-id>`, the resume operation uses the same mode as the original run. You can override this:

```bash
wf resume <run-id> --parallel     # resume with parallel even if original was sequential
wf resume <run-id> --work-stealing
```
