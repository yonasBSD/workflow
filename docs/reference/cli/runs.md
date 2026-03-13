# wf runs

List, filter, and analyse past workflow runs.

## Synopsis

```
wf runs [flags]
```

## Flags

### Filtering

| Flag | Type | Default | Description |
|---|---|---|---|
| `--workflow`, `-w` | string | — | Filter by workflow name |
| `--status`, `-s` | string | — | Filter by status: `pending`, `running`, `success`, `failed`, `cancelled`, `resuming` |
| `--tag` | string | — | Filter by tag (exact match against the workflow's `tags` array) |
| `--start-date` | string | — | Show runs from this date (`YYYY-MM-DD`) |
| `--end-date` | string | — | Show runs until this date (`YYYY-MM-DD`) |
| `--min-duration` | string | — | Show only runs that took at least this long (e.g. `30s`, `5m`, `1h`) |

### Pagination

| Flag | Type | Default | Description |
|---|---|---|---|
| `--limit`, `-l` | int | `10` | Maximum number of results |
| `--offset`, `-o` | int | `0` | Pagination offset |
| `--sort`, `-S` | string | `time` | Sort by: `time`, `name`, `duration`, `tasks`, `status` |

### Display

| Flag | Type | Default | Description |
|---|---|---|---|
| `--detailed`, `-d` | bool | `false` | Show per-task breakdown for each run |
| `--stats` | bool | `false` | Show only aggregate statistics |
| `--tasks` | bool | `false` | Show task summaries for each run |
| `--logs` | bool | `false` | Show log file paths for failed tasks |
| `--timeline` | bool | `false` | Display runs in a timeline format |
| `--json`, `-j` | bool | `false` | Output as JSON |

## Run Status Values

| Status | Description |
|---|---|
| `pending` | Run created but not yet started |
| `running` | Currently executing |
| `success` | All tasks completed successfully |
| `failed` | One or more tasks failed |
| `cancelled` | Interrupted by Ctrl+C or `--timeout` |
| `resuming` | Resume operation in progress |

## Examples

```bash
# List 10 most recent runs
wf runs

# Filter by workflow
wf runs --workflow cicd-pipeline

# Filter by status
wf runs --status failed
wf runs --status success --limit 20

# Filter by tag
wf runs --tag production
wf runs --tag nightly --limit 30

# Date range
wf runs --start-date 2026-03-01 --end-date 2026-03-09

# Show only slow runs
wf runs --min-duration 10m

# Detailed view with task breakdown
wf runs --detailed --limit 5

# Statistics summary
wf runs --stats
wf runs --workflow data-etl --stats

# JSON output for scripting
wf runs --json | jq '.[] | {id: .id, status: .status, duration: .duration}'

# Show failed runs with log paths
wf runs --status failed --logs

# Get latest run ID
wf runs --limit 1 | awk 'NR==2{print $1}'
```

## Output Format

Default:

```
RUN ID                  WORKFLOW        STATUS    DURATION   STARTED
2Xk7p9QrVnYoJ1mT3s...  cicd-pipeline   success   1m12s      2026-03-09 14:22:01
2Xk7p0AbCdEfGhIjKl...  data-etl        failed    23m41s     2026-03-09 02:00:03
```

With `--detailed`:

```
Run: 2Xk7p9QrVnYoJ1mT3s...
  Workflow : cicd-pipeline
  Status   : success
  Duration : 1m12s
  Started  : 2026-03-09 14:22:01
  Tasks    : 12 total, 12 success, 0 failed, 0 skipped

  lint              success   3.2s
  test-unit         success   8.1s
  test-integration  success   11.4s
  build-linux       success   15.2s
  build-darwin      success   14.8s
  build-windows     success   16.1s
  ...
```

## See Also

- [wf inspect](inspect.md) — deep dive into a single run
- [wf audit](audit.md) — chronological event trail
- [wf diff](diff.md) — compare two runs
