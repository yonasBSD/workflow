# wf init

Initialise the `wf` workspace. Creates the workflows directory, log directory, SQLite database, and a starter config file.

## Synopsis

```
wf init [flags]
```

## Description

`wf init` is the first command to run on a new machine. It creates all required directories and files using the paths from the current configuration (defaults or overrides).

If a file or directory already exists, it is left untouched — `wf init` is safe to run multiple times.

## What it creates

| Path (Linux default) | Description |
|---|---|
| `~/.config/workflow/config.yaml` | Starter configuration file |
| `~/.config/workflow/workflows/` | Workflows directory |
| `~/.cache/workflow/workflow.db` | SQLite database (mode 0600) |
| `~/.cache/workflow/logs/` | Per-task log files directory (mode 0700) |
| `~/.cache/workflow/workflow.log` | Application log file |

## Flags

None beyond the [global flags](index.md#global-flags).

## Example

```bash
wf init
# Initialised:
#   Workflows : /home/alice/.config/workflow/workflows
#   Database  : /home/alice/.cache/workflow/workflow.db
#   Logs      : /home/alice/.cache/workflow/logs
#   Config    : /home/alice/.config/workflow/config.yaml

# Use a custom location
WF_PATHS_WORKFLOWS=/opt/workflows wf init
```

## See Also

- [Configuration](../../getting-started/configuration.md)
- [Installation](../../getting-started/installation.md)
