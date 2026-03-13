# Changelog

All notable changes to `wf` are documented here. This project follows [Semantic Versioning](https://semver.org/spec/v2.0.0.html) and [Conventional Commits](https://www.conventionalcommits.org/).

---

## [0.2.0] — 2026-03-13

Expands `wf` from a basic sequential workflow runner into a full infrastructure automation runtime. The execution engine, storage layer, and DAG model were substantially redesigned. Six new CLI commands, three execution modes, and a formal security model were added. The logger and config subsystems were refactored.

### Added

#### CLI Commands (new)
- `wf inspect <run-id>` — structured run details: registered variables, task timings, exit codes, forensic logs
- `wf status <run-id>` — live polling of an in-progress run with periodic refresh
- `wf audit <run-id>` — chronological, tamper-evident event trail for a run
- `wf diff <run-id-a> <run-id-b>` — side-by-side comparison of task outcomes and registered variables between two runs
- `wf export <run-id>` — export a full run record as `--format json` or `--format tar` (tar includes per-task log files)
- `wf health` — system health check: database connectivity, schema version, workflow directory, disk space

#### Execution Modes
- **Parallel** (`--parallel`, `--max-parallel N`) — level-locked concurrency; tasks within the same topological level run concurrently, bounded by a semaphore (default 4)
- **Work-stealing** (`--work-stealing`) — dependency-driven dispatch; a task is dispatched the moment all of its declared `depends_on` tasks complete, regardless of level — maximum throughput for sparse DAGs
- `--timeout <duration>` — per-run wall-clock limit; cancels the entire process group on expiry (context-aware: retry delays are also cancelled immediately)
- `--print-output` — buffer and emit task stdout/stderr atomically after completion (capped at 64 KiB on the progress channel; full content always written to log file)
- `--var KEY=VALUE` — inject variables at run time without modifying the workflow file

#### Workflow Fields (new)
- `register` — capture the **last non-empty stdout line** of a task as a named variable, scoped to that task (consistent with the shell convention of `echo $RESULT` as the final command)
- `{{.varname}}` — safe regex-based variable interpolation in downstream `cmd` fields and `if` expressions (no `text/template` logic — by design)
- `if` — conditional task execution evaluated against runtime variable values; supports `==`, `!=`, `<`, `<=`, `>`, `>=`, `contains`, `starts_with`, `ends_with`, `matches`
- `matrix` — expand a single task definition into N independent nodes, one per parameter combination (e.g. three databases, five platforms); matrix nodes are independent in the DAG and can run in parallel; matrix variable short names (e.g. `{{.env}}`) resolve inside the owning task without qualification
- `on_failure` (task-level) — wire a compensating `forensic` task to a specific task
- `on_failure` (workflow-level) — global failure handler that fires after the run settles
- `type = "forensic"` — tasks that run only on failure; excluded from normal DAG levels; receive `{{.failed_task}}` and `{{.error_message}}` automatically (Saga pattern)
- `ignore_failure` — continue workflow execution even if the task exits non-zero
- `retry_delay` — configurable delay between retry attempts (context-aware)
- `timeout` (per-task) — individual task execution limit; sends `SIGKILL` to the entire process group on expiry
- `env` — task-specific environment variable map
- `clean_env` / `--clean-env` — start task with an empty environment rather than inheriting the parent process env
- `working_dir` — per-task working directory (validated: `/proc`, `/sys`, `/dev`, and null bytes are blocked)
- `tags` — workflow-level string array, stored as JSON; filterable via `wf runs --tag <tag>`

#### Storage (`internal/storage/` — replaces `internal/run/`)
- Full rewrite of the persistence layer: SQLite with WAL mode enabled
- KSUID run IDs (sortable, collision-free, URL-safe base62) — replaces simple integer IDs
- Versioned, forward-only schema migrations applied at startup
- Append-only `audit_trail` table — every state transition recorded with timestamp, immutable after write
- `context_snapshots` table — variable state persisted after each task; consumed by `wf resume` to restore `ContextMap`
- `forensic_logs` table — output from forensic task executions stored separately
- `task_dependencies` table — DAG edges stored for `wf graph` and `wf diff`
- `dag_cache` table — DAG content hash cached to detect workflow file changes between runs and resumes
- All SQL uses parameterized queries — no string interpolation into queries
- Database file pre-created at mode `0600` before SQLite opens it

#### DAG Layer (`internal/dag/` — redesigned)
- `parser.go` — TOML → `WorkflowDefinition`; validates workflow name (path traversal prevention), `working_dir`, and `env` key names at parse time
- `builder.go` — `WorkflowDefinition` → `DAG`; expands matrix tasks, wires dependency edges, runs cycle detection (Kahn's algorithm), assigns topological levels → `DAG{Levels [][]*Node}`; populates `DAG.ForensicTasks` for task-level trap lookup
- `serialize.go` — DAG serialisation for `wf graph` output formats (HTML, ASCII, DOT, Mermaid, JSON)
- `Node` — all mutable fields (`State`, `Output`, `ExitCode`) protected by `sync.RWMutex` via thread-safe methods (`MarkRunning`, `MarkSuccess`, `MarkFailed`, `MarkSkipped`, `MarkEarlyFailed`, `Reset`, `GetState`)
- `MarkEarlyFailed()` — marks nodes whose dependencies failed as `NodeStateFailed`, allowing the executor to skip them cleanly
- `DAG.ForensicTasks map[string]*Node` — task-level forensic tasks registered here during build so the executor can look them up by name without polluting the normal node graph

#### Executor Layer (`internal/executor/` — expanded)
- `Executor` interface: `Execute(ctx, dag, ctxMap)`, `Resume(ctx, runID)`, `GetStore()` — all three implementations conform to the same interface
- `sequential.go` — single-task-at-a-time, level-by-level (replaces the original monolithic executor)
- `parallel.go` — level-locked concurrency with semaphore (`--parallel`)
- `work_stealing.go` — dependency-driven dispatch (`--work-stealing`); goroutine per node, pending-dep counter, shared work queue
- `progress.go` — progress event system (`ProgressTaskStarted`, `ProgressTaskCompleted`, `ProgressTaskOutput`) feeding the `--print-output` renderer
- `doResume()` — shared resume logic: reload TOML, restore `ContextMap` from snapshots, pre-mark succeeded tasks as `NodeStateSuccess`, re-run remaining tasks
- Forensic trap wiring: task-level `on_failure` fires immediately on task failure; workflow-level fires after the run settles
- `limitedBuffer` — caps stdout + stderr capture at 10 MiB per task; silently drops writes beyond the cap
- `resetDAGState` resets all nodes including forensic and global trap nodes — safe DAG re-use across multiple `Execute` calls

#### Logging (`internal/logger/`)
- **Migrated from `go.uber.org/zap` to `log/slog`** (standard library, Go 1.21+) — no external logger dependency
- **Comprehensive structured logging** across the entire execution pipeline: every event carries `run_id`, `workflow`, `task`, `task_name`, `attempt`, `duration_ms`, `exit_code`, and other contextual fields. Key log points: `task started` / `task completed` / `retrying task`, `run started` / `run completed` with full stats, `database opened`, `run created`, `variable set` / conflict warnings, forensic trap lifecycle

#### New Packages
- `internal/contextmap/` — thread-safe variable registry; `Set(taskID, name, value)`, `InterpolateCommand(taskID, cmd)`, `EvalCondition(expr)`, `Snapshot()`, `Restore(data)`. Regex-only `{{.var}}` substitutor — `text/template` was removed intentionally to prevent template injection
- `internal/security/` — centralised security validation: `ValidateWorkflowName()` (path traversal, null bytes, length), `ValidateWorkingDir()` (blocks `/proc`, `/sys`, `/dev`), `ValidateVariableName()` (alphanumeric + allowed symbols), `ValidateEnvKey()` (POSIX rules + deny-list for `LD_PRELOAD`, `LD_LIBRARY_PATH`, `DYLD_*`, and other dynamic-linker vars)
- `internal/tty/` — terminal detection for ANSI colour suppression; respects `NO_COLOR`, `TERM=dumb`, and non-TTY stdout

#### Configuration
- Config file paths set to platform-specific XDG/OS defaults: `~/.config/workflow/config.yaml` (Linux), `~/Library/Application Support/workflow/config.yaml` (macOS), `%AppData%\workflow\config.yaml` (Windows)
- Data directory defaults: `~/.cache/workflow/` (Linux), `~/Library/Caches/workflow/` (macOS), `%LocalAppData%\workflow\` (Windows)
- `config.example.yaml` documents all `WF_*` environment variable overrides

#### Test Suites
- `tests/security/security_test.go` — 22 security test functions covering path traversal, template injection, env-key deny-list, working\_dir restrictions, output buffer cap, file permissions, SQL parameterization; must pass with `-race`
- `tests/integration/scenarios_test.go` — 15 scenario tests covering: register last-line capture, matrix variable interpolation, working-dir pre-existence, task-level forensic traps, global forensic traps, `ignore_failure` continuation, condition skip/run, resume skipping succeeded tasks, parallel vs sequential equivalence, work-stealing diamond pattern, timeout, retry attempt count, `clean_env`, concurrent-runs stress (race detector guard)
- `tests/examples/examples_test.go` — `TestExamplesValidate` parses and builds all 12 example workflows on every CI run; `TestExamplesRun` executes them end-to-end (skipped under `go test -short`)
- `internal/storage/store_test.go` — run/task CRUD, filter-by-status/tag/name/limit, migration idempotency
- `tests/benchmarks/` — performance benchmarks for DAG construction, execution modes, and storage operations
- Existing e2e and integration tests updated for the new storage layer and executor interface

#### Documentation
- Full documentation site (MkDocs + Material theme) — 53 pages covering getting started, concepts, CLI reference, guides, examples, security model, and architecture
- ReadTheDocs configuration (`.readthedocs.yaml`)
- GitHub Actions workflow for automated GitHub Pages deployment (`.github/workflows/docs.yml`)
- Twelve production-grade example workflows in `files/examples/` covering all major features (filenames unified with their `name` field — no numeric prefixes)
- `files/examples/GUIDE.md` — feature coverage matrix and per-example run commands

#### CI / Quality
- `.golangci.yml` — curated linter set: `errcheck`, `staticcheck`, `gosec`, `gocritic`, `bodyclose`, `noctx`, `ineffassign`, `unconvert`, `unparam`
- CI `lint` job — runs golangci-lint v2 on every push/PR
- CI `example validation` step — runs `TestExamplesValidate` on every push/PR across all three OS targets
- CI `benchmarks` step — runs all benchmarks on every push/PR

#### Release
- GoReleaser ldflags version injection wired through `main.go` → `cmd.SetVersionInfo()` — `wf --version` now shows build version, commit hash, and build date
- SBOM generation via Syft integrated into the release workflow
- `-j` short flag added for `--json` on `wf validate` and `wf run`

#### Branding & Organisation
- Repository migrated to `github.com/silocorp/workflow` — Go module path, all imports, install script, GoReleaser config, docs, and CI updated
- Placeholder logo (`docs/assets/logo.svg`) added; used as both logo and favicon in docs
- README redesigned with centered header, logo, and tagline

### Changed

#### Config refactor (`internal/config/`)
- Removed package-level global `config.C`
- All config access now via `config.Get()` — returns the singleton `Config` struct
- Eliminates the race condition possible when `config.C` was written during init and read from multiple goroutines

#### DAG package restructure
- `internal/dag/loader.go` — removed; loading logic merged into `dag/parser.go`
- `internal/dag/topo.go` — removed; topological sort moved into `dag/builder.go` as part of the `Build()` pipeline
- `internal/dag/validate.go` — removed; validation integrated into `builder.go` and `security/validate.go`
- `internal/dag/dag_test.go` — removed; replaced by `builder_test.go`

#### Storage package replacement
- `internal/run/` package (model.go, store.go, run\_test.go) removed and replaced by `internal/storage/` with full WAL-mode SQLite, versioned migrations, and the expanded schema above

---

## [0.1.0] — 2026-02-03

Initial release of `wf` — a minimal, deterministic workflow orchestrator for local-first execution.

### Added

#### Core
- Single static Go binary, no CGo, no runtime dependencies
- TOML-based workflow definitions with strict DAG semantics
- Topological execution order (Kahn's algorithm)
- Deterministic, fail-fast task execution
- Per-task configurable retries
- Graceful cancellation via Ctrl+C (SIGINT/SIGTERM handling)

#### CLI Commands
- `wf init` — initialise workspace directories, database, and default config file
- `wf validate [workflow]` — validate workflow definitions (all or single), with `--json` output
- `wf run <workflow>` — execute a workflow with `--dry-run` support
- `wf resume <run-id>` — resume a failed run from the point of failure, skipping succeeded tasks
- `wf list` — list available workflows
- `wf runs` — list run history with `--workflow`, `--status`, `--limit`, and `--json` filters
- `wf logs <run-id> [--task <task-id>]` — view per-task and per-run logs
- `wf graph <workflow>` — display DAG structure with `--detail` and `--format ascii` options

#### Persistence
- SQLite run history — one record per run, one record per task execution
- Per-task structured logs captured and stored on disk
- Run state committed on every transition (start, success, fail, retry) for crash recovery

#### Workflow Schema
- `name` — required workflow identifier
- `[tasks.<id>]` — task definition sections
- `cmd` — shell command to execute
- `depends_on` — list of upstream task dependencies
- `retries` — number of retry attempts (default: 0)

#### Configuration
- Viper-based config with YAML config file, environment variables, and CLI flag overrides
- Platform-specific default paths (XDG on Linux, Application Support on macOS, AppData on Windows)
- `--config <path>`, `--log-level`, `--verbose`, `--version` global flags

#### Validation Rules
- Workflow must have a `name`
- Tasks must have a `cmd`
- Task names: alphanumeric, hyphen, underscore only
- No duplicate task names
- No missing dependencies
- No cycles in the DAG
- At least one task required

#### Cross-Platform
- Linux, macOS, and Windows support
- Pre-built binaries via GitHub Releases (`.github/workflows/release.yml`)
