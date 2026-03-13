# Configuration

`wf` uses a layered configuration system. Values are resolved in this priority order (highest first):

1. **CLI flags** — override everything
2. **Config file** — `~/.config/workflow/config.yaml`
3. **Environment variables** — `WF_` prefix
4. **Built-in defaults**

---

## Config File

The config file is YAML. `wf init` creates a starter file at the default location.

```yaml
# ~/.config/workflow/config.yaml

paths:
  workflows: /home/alice/workflows       # where .toml files live
  database:  /home/alice/.cache/workflow/workflow.db
  logs:      /home/alice/.cache/workflow/logs
  logs_file: /home/alice/.cache/workflow/workflow.log

log_level: info   # debug | info | warn | error
```

Use a custom config file location with the `--config` global flag:

```bash
wf --config /etc/wf/config.yaml run my-workflow
```

---

## Environment Variables

Every config key has a corresponding `WF_` environment variable. The key mapping uses `_` as the section separator.

| Environment Variable | Config Key | Default | Description |
|---|---|---|---|
| `WF_PATHS_WORKFLOWS` | `paths.workflows` | platform-specific | Directory containing `.toml` workflow files |
| `WF_PATHS_DATABASE` | `paths.database` | platform-specific | Path to the SQLite database file |
| `WF_PATHS_LOGS` | `paths.logs` | platform-specific | Directory for per-task log files |
| `WF_PATHS_LOGS_FILE` | `paths.logs_file` | platform-specific | Application log file |
| `WF_LOG_LEVEL` | `log_level` | `info` | Log verbosity |

### Quick override for a single run

```bash
WF_PATHS_WORKFLOWS=./my-workflows wf run my-workflow
```

---

## Default Paths by Platform

=== "Linux"

    | Resource | Default Path |
    |---|---|
    | Config file | `~/.config/workflow/config.yaml` |
    | Workflows directory | `~/.config/workflow/workflows/` |
    | Database | `~/.cache/workflow/workflow.db` |
    | Task logs | `~/.cache/workflow/logs/` |
    | Application log | `~/.cache/workflow/workflow.log` |

=== "macOS"

    | Resource | Default Path |
    |---|---|
    | Config file | `~/Library/Application Support/workflow/config.yaml` |
    | Workflows directory | `~/Library/Application Support/workflow/workflows/` |
    | Database | `~/Library/Caches/workflow/workflow.db` |
    | Task logs | `~/Library/Caches/workflow/logs/` |
    | Application log | `~/Library/Caches/workflow/workflow.log` |

=== "Windows"

    | Resource | Default Path |
    |---|---|
    | Config file | `%AppData%\workflow\config.yaml` |
    | Workflows directory | `%AppData%\workflow\workflows\` |
    | Database | `%LocalAppData%\workflow\workflow.db` |
    | Task logs | `%LocalAppData%\workflow\logs\` |
    | Application log | `%LocalAppData%\workflow\workflow.log` |

---

## Global CLI Flags

These flags apply to every `wf` subcommand.

| Flag | Type | Default | Description |
|---|---|---|---|
| `--config` | string | platform default | Path to config file |
| `--log-level` | string | `info` | Log verbosity: `debug`, `info`, `warn`, `error` |
| `--verbose`, `-v` | bool | `false` | Shorthand for `--log-level debug` |

---

## Environment Variables Set During Task Execution

When `wf` executes a task command, it injects the following variables into the task's environment:

| Variable | Value |
|---|---|
| `WF_RUN_ID` | The KSUID run identifier |
| `WF_WORKFLOW` | The workflow name |
| `WF_TASK_ID` | The current task ID |

These are available inside any shell command as `$WF_RUN_ID`, `$WF_WORKFLOW`, etc.

!!! note "clean_env and injected variables"
    When `clean_env = true` is set on a task, the parent process environment is not inherited. However, `WF_RUN_ID`, `WF_WORKFLOW`, and `WF_TASK_ID` are still injected so that task scripts can identify themselves.

---

## Minimal Setup (no init)

If you prefer not to run `wf init`, set environment variables before running:

```bash
export WF_PATHS_WORKFLOWS=/path/to/workflows
export WF_PATHS_DATABASE=/tmp/wf.db

wf run my-workflow
```

`wf` creates the database and log directories on first use.
