# Workflow File Reference

Workflow files are TOML documents that live in the configured `workflows` directory. Each file defines exactly one workflow. The filename (without `.toml`) must match the `name` field.

---

## File Naming

```
workflows/
├── my-pipeline.toml        # name = "my-pipeline"
├── nightly-etl.toml        # name = "nightly-etl"
└── deploy-staging.toml     # name = "deploy-staging"
```

---

## Workflow-Level Fields

These fields appear at the top of the file, outside any `[tasks.*]` section.

| Field | Type | Required | Description |
|---|---|---|---|
| `name` | string | **yes** | Workflow identifier. Must match filename without `.toml`. |
| `description` | string | no | Human-readable description shown in `wf list`. |
| `tags` | []string | no | Searchable labels. Used with `wf runs --tag`. |
| `on_failure` | string | no | Task ID of the global forensic handler. Runs when any task fails. |

### Example

```toml
name        = "deploy-production"
description = "Full production deployment with smoke tests"
tags        = ["deploy", "production", "nightly"]
on_failure  = "alert-oncall"
```

---

## Task Fields

Tasks are declared as TOML tables: `[tasks.<id>]`. The task ID is the key used in `depends_on` references.

### Required

| Field | Type | Description |
|---|---|---|
| `cmd` | string | Shell command to execute. Supports multi-line with `"""..."""`. Interpolated with `{{.varname}}` before execution. |

### Identity and Type

| Field | Type | Default | Description |
|---|---|---|---|
| `name` | string | task ID | Display name shown in progress output and reports. |
| `type` | string | `"normal"` | `"normal"` or `"forensic"`. Forensic tasks only run on failure, wired via `on_failure`. |

### Dependencies

| Field | Type | Default | Description |
|---|---|---|---|
| `depends_on` | []string | `[]` | List of task IDs that must complete successfully before this task runs. |

### Execution Environment

| Field | Type | Default | Description |
|---|---|---|---|
| `working_dir` | string | process CWD | Directory in which `cmd` is executed. Validated against path traversal at parse time. |
| `env` | map[string]string | `{}` | Additional environment variables injected into `cmd`. Keys must be valid POSIX names. |
| `clean_env` | bool | `false` | If `true`, start with an **empty** environment instead of inheriting the parent process env. `WF_RUN_ID`, `WF_WORKFLOW`, and `WF_TASK_ID` are still injected. |

### Variables

| Field | Type | Default | Description |
|---|---|---|---|
| `register` | string | — | Variable name to store the last non-empty stdout line. Available in downstream tasks as `{{.name}}`. |
| `if` | string | — | Boolean expression. Task is **skipped** (not failed) if it evaluates to `false`. References registered variables by name. |

### Matrix Expansion

| Field | Type | Default | Description |
|---|---|---|---|
| `matrix` | map[string][]string | — | Parameter grid. Each key-value-array combination produces one expanded node. |

### Resilience

| Field | Type | Default | Description |
|---|---|---|---|
| `ignore_failure` | bool | `false` | If `true`, non-zero exit codes are treated as success. Dependants continue normally. |
| `retries` | int | `0` | Number of retry attempts after a non-zero exit. Total executions = `retries + 1`. |
| `retry_delay` | duration | `0s` | Delay between retry attempts. Supports Go duration syntax: `5s`, `1m30s`, `2h`. |
| `timeout` | duration | — | Per-task execution timeout. The task is killed (SIGKILL to process group) if it exceeds this limit. |

### Failure Handling

| Field | Type | Default | Description |
|---|---|---|---|
| `on_failure` | string | — | Task ID of a forensic handler to run when this task fails. The referenced task must have `type = "forensic"`. |

---

## `cmd` — Command Syntax

`cmd` is executed by `/bin/sh -c` (Unix) or `cmd.exe /C` (Windows). All standard shell syntax is available.

### Multi-line commands

```toml
[tasks.setup]
cmd = """
set -euo pipefail
mkdir -p /tmp/workspace
cd /tmp/workspace
echo "Ready"
"""
```

### Variable interpolation in `cmd`

```toml
[tasks.deploy]
cmd = "./deploy.sh --env={{.target_env}} --version={{.app_version}}"
```

Only `{{.varname}}` substitution is supported. Template logic (`{{if}}`, `{{range}}`, `{{call}}`) is emitted verbatim — it will cause the shell command to fail visibly.

---

## `env` — Environment Variables

```toml
[tasks.build]
cmd = "go build ."
env = {GOOS = "linux", GOARCH = "amd64", CGO_ENABLED = "0"}
```

Keys must satisfy POSIX naming rules: letters, digits, underscore; must not start with a digit.

The following keys are **blocked** and will cause a parse error:

| Blocked Key | Reason |
|---|---|
| `LD_PRELOAD` | Dynamic-linker preload override |
| `LD_LIBRARY_PATH` | Dynamic-linker library path override |
| `LD_AUDIT` | Dynamic-linker audit module |
| `LD_DEBUG` | Dynamic-linker debug flag |
| `LD_DEBUG_OUTPUT` | Dynamic-linker debug output redirect |
| `DYLD_INSERT_LIBRARIES` | macOS dynamic-linker insert override |
| `DYLD_LIBRARY_PATH` | macOS dynamic-linker library path |
| `DYLD_FRAMEWORK_PATH` | macOS dynamic-linker framework path |

---

## `if` — Condition Expressions

```toml
[tasks.cleanup]
cmd = "rm -rf /tmp/cache"
if  = 'disk_pct > "85"'
```

The condition is evaluated after all `depends_on` tasks complete. If it evaluates to `false`, the task state is set to `skipped` and dependants continue as if it succeeded.

### Operators

```
==   !=   >   <   >=   <=   &&   ||   !
```

### Examples

```toml
if = 'status == "healthy"'
if = 'cert_days < "30"'
if = 'errors != "0"'
if = 'score >= "0.9" && model_type == "transformer"'
if = 'env == "production" || env == "staging"'
```

---

## `matrix` — Expansion

```toml
[tasks.test]
cmd    = "go test ./{{.test.pkg}}/..."
matrix = {pkg = ["api", "storage", "executor", "contextmap"]}
```

Produces: `test[pkg=api]`, `test[pkg=storage]`, `test[pkg=executor]`, `test[pkg=contextmap]`.

Matrix variables are referenced in `cmd` as `{{.taskid.paramname}}`.

Multi-dimensional:

```toml
[tasks.build]
cmd    = "GOOS={{.build.os}} GOARCH={{.build.arch}} go build -o bin/app-{{.build.os}}-{{.build.arch}} ."
matrix = {os = ["linux", "darwin"], arch = ["amd64", "arm64"]}
```

Produces 4 nodes (2×2).

---

## Forensic Tasks

```toml
[tasks.rollback-db]
type    = "forensic"
cmd     = "psql -c 'ROLLBACK TO SAVEPOINT pre_deploy;'"
timeout = "2m"
retries = 1
```

Forensic tasks:

- Must have `type = "forensic"`
- Are excluded from normal DAG execution (not counted in levels)
- Run only when wired via `on_failure`
- Support all standard task fields (`timeout`, `retries`, `retry_delay`, `env`, `clean_env`, `ignore_failure`)
- Have access to `{{.failed_task}}` and `{{.error_message}}` injected variables

---

## Duration Syntax

`timeout` and `retry_delay` use Go duration syntax:

| Example | Meaning |
|---|---|
| `30s` | 30 seconds |
| `5m` | 5 minutes |
| `1m30s` | 1 minute 30 seconds |
| `2h` | 2 hours |
| `24h` | 24 hours |

---

## Complete Example

```toml
name        = "data-pipeline"
description = "Nightly data processing pipeline"
tags        = ["etl", "nightly", "data"]
on_failure  = "notify-failure"

# ── Setup ─────────────────────────────────────────────────

[tasks.init]
name    = "Initialise Run"
cmd     = """
DATE=$(date +%Y-%m-%d)
echo "Starting pipeline for $DATE"
echo "$DATE"
"""
register = "run_date"

# ── Parallel extraction ────────────────────────────────────

[tasks.extract-api]
name           = "Extract from API"
cmd            = "python extract_api.py --date={{.run_date}} && echo 12500"
register       = "api_rows"
depends_on     = ["init"]
ignore_failure = true
retries        = 2
retry_delay    = "30s"
timeout        = "15m"
env            = {API_TOKEN = "{{.run_date}}", MAX_RETRIES = "3"}

[tasks.extract-db]
name           = "Extract from Database"
cmd            = "python extract_db.py --date={{.run_date}} && echo 84200"
register       = "db_rows"
depends_on     = ["init"]
ignore_failure = true
timeout        = "20m"
clean_env      = true

# ── Transform ─────────────────────────────────────────────

[tasks.transform]
name       = "Transform and Merge"
cmd        = """
python transform.py \
  --api-rows={{.api_rows}} \
  --db-rows={{.db_rows}} \
  --date={{.run_date}}
echo 96700
"""
register   = "merged_rows"
depends_on = ["extract-api", "extract-db"]
timeout    = "30m"
working_dir = "/data/pipeline"

# ── Load ──────────────────────────────────────────────────

[tasks.load]
name       = "Load to Warehouse"
cmd        = "python load.py --rows={{.merged_rows}}"
depends_on = ["transform"]
retries    = 3
retry_delay = "1m"
timeout    = "1h"

# ── Validate ──────────────────────────────────────────────

[tasks.validate]
name       = "Validate Row Count"
cmd        = "echo 'Loaded {{.merged_rows}} rows successfully'"
depends_on = ["load"]
if         = 'merged_rows > "0"'

# ── Forensic handler ──────────────────────────────────────

[tasks.notify-failure]
type           = "forensic"
cmd            = """
echo "Pipeline failed at task: {{.failed_task}}"
echo "Error: {{.error_message}}"
curl -s $SLACK_WEBHOOK -d "{\"text\": \"ETL failed: {{.failed_task}}\"}"
"""
ignore_failure = true
timeout        = "30s"
```
