# Architecture Overview

`wf` is a single static Go binary. The core execution model requires no background services and no network calls to external systems. Everything happens in a single process execution.

A trigger listener (`wf listen`) is planned as an optional additive component for event-driven scheduling — it does not affect direct `wf run` invocations. See [Triggers](../concepts/triggers.md) for details.

---

## Execution Pipeline

```
wf run my-workflow
      │
      ▼
┌─────────────────────────────────────────────────────────┐
│  CLI (cmd/run.go)                                       │
│  • Parse flags                                          │
│  • Validate --var values                                │
│  • Select executor                                      │
└────────────────────┬────────────────────────────────────┘
                     │
                     ▼
┌─────────────────────────────────────────────────────────┐
│  Parser (internal/dag/parser.go)                        │
│  • Validate workflow name (path traversal check)        │
│  • Read TOML file                                       │
│  • Validate env keys, working_dir, variable names       │
│  → WorkflowDefinition                                   │
└────────────────────┬────────────────────────────────────┘
                     │
                     ▼
┌─────────────────────────────────────────────────────────┐
│  Builder (internal/dag/builder.go)                      │
│  • Expand matrix tasks                                  │
│  • Resolve depends_on edges                             │
│  • Detect cycles (Kahn's algorithm)                     │
│  • Topological sort → Levels                            │
│  → DAG{Levels [][]*Node}                                │
└────────────────────┬────────────────────────────────────┘
                     │
                     ▼
┌─────────────────────────────────────────────────────────┐
│  Executor (internal/executor/)                          │
│  • Create Run record in SQLite                          │
│  • For each task:                                       │
│    - Evaluate `if` condition (ContextMap)               │
│    - Inject matrix vars                                 │
│    - Interpolate {{.var}} in cmd                        │
│    - Execute shell command (retry loop)                 │
│    - Capture stdout/stderr (10 MiB limit)               │
│    - Write log file (0600)                              │
│    - Capture last line → ContextMap.Set()               │
│    - Update TaskExecution in SQLite                     │
│    - Fire forensic handler on failure                   │
└────────────────────┬────────────────────────────────────┘
                     │
                     ▼
┌─────────────────────────────────────────────────────────┐
│  Storage (internal/storage/)                            │
│  • SQLite with WAL mode                                 │
│  • Append-only audit trail                              │
│  • Variable snapshots at task completion                │
└─────────────────────────────────────────────────────────┘
```

---

## Key Packages

| Package | File(s) | Responsibility |
|---|---|---|
| `cmd/` | `run.go`, `resume.go`, `graph.go`, … | Cobra CLI commands; flag parsing; executor selection |
| `internal/dag` | `parser.go`, `builder.go`, `dag.go` | TOML → `WorkflowDefinition` → `DAG`; cycle detection; topological sort |
| `internal/executor` | `executor.go`, `sequential.go`, `parallel.go`, `work_stealing.go` | Task lifecycle; retry loop; forensic trap invocation; progress events |
| `internal/storage` | `store.go`, `schema.go`, `model.go` | SQLite persistence; versioned migrations; audit trail; context snapshots |
| `internal/contextmap` | `contextmap.go`, `template.go` | Thread-safe variable registry; `{{.var}}` interpolation; `if` evaluation |
| `internal/security` | `validate.go` | Path traversal prevention; env key deny-list; working_dir restrictions |
| `internal/config` | `config.go` | Viper-based configuration; `config.Get()` singleton |
| `internal/logger` | `logger.go` | `log/slog` wrapper; console + file output |
| `internal/tty` | `tty.go` | Terminal detection for ANSI colour suppression |

---

## Key Data Structures

### `WorkflowDefinition`

Raw parsed TOML. One-to-one mapping with the TOML schema. Contains all task definitions before matrix expansion or dependency resolution.

### `DAG`

The processed, validated execution graph. Contains:

- `Nodes map[string]*Node` — all nodes (including matrix-expanded)
- `Levels [][]*Node` — topologically sorted slices; level 0 has no dependencies
- `ForensicTasks map[string]*Node` — forensic tasks excluded from normal levels

### `Node`

A single executable unit. May be a matrix expansion. All mutable fields are protected by a `sync.RWMutex` and accessed through thread-safe methods:

```go
node.MarkRunning(startTime time.Time)
node.MarkSuccess(endTime time.Time, output string, exitCode int)
node.MarkFailed(endTime time.Time, err error)
node.MarkSkipped(output string, conditionResult bool)
node.MarkEarlyFailed(err error, exitCode int, stackTrace string)
node.MarkConditionMet(result bool)
node.GetState() NodeState
node.Reset()
```

### `ContextMap`

Thread-safe variable registry backed by `sync.RWMutex`. Key operations:

```go
cm.Set(taskID, name, value)           // register a variable (owner = taskID)
cm.Get(name)                          // read a variable value
cm.SetMatrix(taskID, vars)            // register read-only matrix vars
cm.EvalCondition(expr)                // evaluate an `if` expression
cm.InterpolateCommand(taskID, cmd)    // substitute {{.var}} in a command
cm.Snapshot()                         // serialise to JSON for DB storage
cm.Restore(data)                      // deserialise from JSON on resume
```

### `Store` (SQLite)

All database access goes through the `Store` interface. The implementation uses parameterized queries exclusively. Schema migrations are numbered and forward-only.

---

## Executor Implementations

Three structs implement the `Executor` interface (`Execute`, `Resume`, `GetStore`):

| Implementation | File | Strategy |
|---|---|---|
| `SequentialExecutor` | `sequential.go` | Level-by-level, one task at a time |
| `ParallelExecutor` | `parallel.go` | Level-by-level, tasks within a level run concurrently under a semaphore |
| `WorkStealingExecutor` | `work_stealing.go` | Dependency-driven; task enqueued the moment all deps complete |

All three share the core task lifecycle logic in `executor.go`:

- `executeNode()` — evaluates condition, interpolates command, runs subprocess, handles retry loop
- `runCommand()` — subprocess setup, process group management, timeout via context, output capture
- `doResume()` — shared resume logic; variable restoration; pre-marking succeeded tasks

---

## Process Management

Task commands are executed as `sh -c <cmd>` (Unix) or `cmd.exe /C <cmd>` (Windows).

On Unix, `SysProcAttr.Setpgid = true` creates a new process group for each task. On timeout or context cancellation, `SIGKILL` is sent to the **process group** (negative PID) — this kills the shell and all its children, preventing orphaned subprocesses from keeping stdout/stderr pipes open.

---

## Storage Schema

Tables (versioned migrations in `internal/storage/schema.go`):

| Table | Purpose |
|---|---|
| `runs` | One row per workflow run. Stores status, mode, timeout, tags, task counts. |
| `task_executions` | One row per task per run. Stores state, exit code, attempt, log path, matrix vars. |
| `context_snapshots` | Variable values at task completion. Used for resume variable restoration. |
| `forensic_logs` | Output from forensic tasks; crash dumps; timeout records. |
| `task_dependencies` | Dependency edges for the DAG — used for re-building the graph on inspect/resume. |
| `audit_trail` | Append-only chronological event log. |
| `dag_cache` | Serialised DAG JSON for fast inspection without re-parsing. |

### Run ID Format

Run IDs use [KSUID](https://github.com/segmentio/ksuid) — K-Sortable Unique IDentifiers. They are:

- Time-sortable (embedded timestamp) — `wf runs` default sort is chronological without an index scan
- Globally unique without coordination — no central ID server needed
- URL-safe (base62 encoded)

---

## Configuration Architecture

Configuration follows the [12-factor app](https://12factor.net/config) model:

```
CLI flags  →  config file  →  WF_* env vars  →  built-in defaults
(highest priority)                              (lowest priority)
```

`internal/config/config.go` wraps Viper. Access is through `config.Get()` which returns a pointer to the current config struct. There is no global `config.C` variable — callers must always call `config.Get()`.

---

## Design Decisions

**Pure Go SQLite** — `modernc.org/sqlite` is used instead of `mattn/go-sqlite3`. This avoids CGo, keeping the binary fully static and cross-compilable without a C toolchain.

**No goroutine pools** — the work-stealing executor uses one goroutine per task. Tasks are expected to be long-running (seconds to minutes), so goroutine overhead is negligible. This keeps the scheduling logic simple and avoids deadlocks from pool exhaustion.

**`log/slog` instead of `zap`** — the standard library logger was chosen to eliminate a dependency and align with the Go standard. `slog` provides structured logging with equivalent performance for the throughput required by `wf`.

**Append-only audit trail** — the audit table has no UPDATE or DELETE code paths. This is a deliberate constraint that provides a tamper-evident history within the database's integrity guarantees.

**`limitedBuffer` instead of `io.LimitReader`** — `io.LimitReader` returns EOF at the limit, which would terminate the subprocess's pipe and send SIGPIPE. `limitedBuffer` silently drops bytes beyond the limit and always returns `(len(p), nil)` — the subprocess continues running and completes normally; only the in-memory capture is truncated.
