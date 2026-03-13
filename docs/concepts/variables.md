# Variables & Interpolation

`wf` has a lightweight but powerful variable system that lets tasks share data at runtime without temporary files or environment variable gymnastics.

---

## `register` — Capture Task Output

The `register` field names a variable. At task completion, the **last non-empty line** of the task's stdout is captured and stored under that name.

```toml
[tasks.get-version]
cmd      = "git describe --tags --abbrev=0"
register = "version"

[tasks.build]
cmd        = "go build -ldflags '-X main.version={{.version}}' -o app ."
depends_on = ["get-version"]
```

The captured value is available in any downstream task that depends (directly or transitively) on the registering task.

!!! tip "Registering structured values"
    If the command prints multiple lines, only the last non-empty line is captured. To register a specific value, make it the final `echo`:

    ```bash
    cmd = """
    echo "Processing..."
    RESULT=$(do_something)
    echo "done"
    echo "$RESULT"    # ← this line is captured
    """
    register = "result"
    ```

---

## `{{.varname}}` — Interpolation

Use `{{.varname}}` anywhere in a `cmd` string to substitute a registered variable.

```toml
[tasks.deploy]
cmd = "./deploy.sh --version={{.version}} --env={{.target_env}}"
```

**Syntax rules:**

- Must use the `{{.name}}` form — curly braces, dot prefix, closing braces
- Variable names may contain: letters, digits, `_`, `-`, `.`, `[`, `]`, `=`, `,`
- Template logic (`{{if}}`, `{{range}}`, `{{call}}`) is **not supported** and will be emitted verbatim — this is intentional for security
- An undefined variable causes the task to fail at the interpolation step, before the shell command runs

---

## `if` — Conditional Execution

The `if` field contains a boolean expression evaluated against registered variables. If it evaluates to `false`, the task is **skipped** (not failed).

```toml
[tasks.promote-model]
cmd        = "./promote.sh {{.champion_model}}"
depends_on = ["select-champion"]
if         = 'best_f1 > "0.90"'
```

### Supported Operators

| Operator | Meaning |
|---|---|
| `==` | Equal |
| `!=` | Not equal |
| `>` | Greater than |
| `<` | Less than |
| `>=` | Greater than or equal |
| `<=` | Less than or equal |
| `&&` | Logical AND |
| `\|\|` | Logical OR |
| `!` | Logical NOT |

### Examples

```toml
if = 'disk_pct > "80"'                          # numeric string comparison
if = 'status == "healthy"'                       # string equality
if = 'error_count != "0"'                        # non-zero check
if = 'risk_level == "high" || risk_level == "critical"'   # OR
if = 'row_count > "0" && pipeline_status == "ready"'      # AND
```

!!! note "String comparisons"
    All registered values are strings. Numeric comparisons work because `wf` parses both operands as numbers when they look like numbers. Non-numeric strings fall back to lexicographic comparison.

---

## `--var` — Runtime Variables

Inject variables at run time without modifying the TOML file:

```bash
wf run deploy --var TARGET_ENV=production --var VERSION=1.4.2
```

The variables are available via `{{.TARGET_ENV}}` and `{{.VERSION}}` in any task command.

Multiple `--var` flags are allowed:

```bash
wf run etl \
  --var WAREHOUSE_SCHEMA=analytics \
  --var BATCH_DATE=2026-03-09 \
  --var DRY_RUN=false
```

Runtime variables are stored in the `ContextMap` alongside `register`-ed values and behave identically in interpolation and `if` conditions.

---

## Variable Scoping

Variables are **global within a run**. Any task can read any registered variable from any task that has already completed. There is no namespace or scope isolation.

The exception is **matrix variables**: when a task uses the `matrix` field, the expanded parameter values are stored as read-only, task-scoped variables. They cannot be overwritten by other tasks.

---

## Variable Ownership

A task that registers a variable **owns** it. Other tasks cannot overwrite it. If two tasks attempt to register the same variable name, the second write is rejected and the task fails.

This is intentional: it prevents a compromised or misbehaving task from silently poisoning variables that downstream tasks trust.

---

## Variable Persistence and Resume

All registered variable values are **snapshotted** to the database at task completion. When a run is resumed:

1. The most recent snapshot for each variable is loaded
2. Variables are restored into the `ContextMap` before any task executes
3. Downstream tasks that were already completed are skipped
4. Resuming tasks have access to all variables from the original run

This means `{{.varname}}` references in resumed tasks work exactly as they did in the original run.

---

## Inspecting Variables

After a run completes:

```bash
wf inspect <run-id>
```

This shows all registered variables and their final values, including the task that registered each one.

---

## Variable Name Constraints

Variable names must consist only of: letters, digits, `_`, `-`, `.`, `[`, `]`, `=`, `,`.

Attempting to register an empty name or a name with other characters causes a validation error at parse time.

---

## Forensic Context Variables

Forensic (failure handler) tasks have access to two additional variables automatically injected by the executor:

| Variable | Available in | Value |
|---|---|---|
| `{{.failed_task}}` | Task-level `on_failure` handler | ID of the failed task |
| `{{.error_message}}` | Task-level and global `on_failure` handler | Error output from the failed task |
| `{{.failed_dag}}` | Global `on_failure` handler | Workflow name |

```toml
[tasks.rollback]
type = "forensic"
cmd  = """
echo "Task {{.failed_task}} failed: {{.error_message}}"
./rollback.sh --reason "{{.failed_task}}"
"""
```
