# Example 07 — Release Management

**File**: `files/examples/07-release-management.toml`
**Industry**: Open Source / SaaS DevOps
**Tags**: `release`, `publish`, `registry`

## Features Demonstrated

- `matrix` expansion across five package registries (npm, pypi, dockerhub, ghcr, homebrew)
- `register` for the release version string
- `working_dir` per task
- `retries` on publish tasks (registry rate limits)
- `ignore_failure` on announcement tasks
- Runtime `--var` for release channel
- `env` for registry credentials
- `timeout` per publish task

## Why this pattern matters

Publishing a release to five registries sequentially takes five times as long as it needs to and fails the entire release if any one registry is temporarily rate-limited. Publishing them in parallel requires coordinating five processes that may succeed or fail independently — exactly what matrix expansion is for.

Each `publish[registry=X]` node is independent in the DAG. If npm publish fails due to a transient rate limit, the retries handle it without blocking PyPI or DockerHub. `ignore_failure` on announcement tasks means a failed Slack notification does not prevent the release from being recorded as complete. The release version is captured once at the top of the pipeline and flows downstream via `{{.release_version}}` — there is no risk of different tasks using different version strings from a file that may change between reads.

## Pipeline Structure

```
[prepare-release] → version
  └── [publish[registry=npm]]     ─┐
      [publish[registry=pypi]]     │
      [publish[registry=dockerhub]]├→ [announce-release]
      [publish[registry=ghcr]]     │       (ignore_failure)
      [publish[registry=homebrew]] ┘
```

## Run Commands

```bash
# Stable channel release
wf run release-management --var RELEASE_CHANNEL=stable --parallel --print-output

# Beta release
wf run release-management \
  --var RELEASE_CHANNEL=beta \
  --work-stealing \
  --print-output \
  --timeout 30m

# Visualise matrix
wf graph release-management --matrix
```

## What to Observe

- `wf graph release-management --matrix` shows five expanded `publish` nodes
- All five publish nodes run in parallel — verify with `wf audit`
- `wf inspect` shows `release_version` — the version captured from `prepare-release`
- `announce-release` uses `{{.release_version}}` — confirm in logs
- `retries = 3` on publish tasks — `wf audit` shows retry events if registries are slow
- `ignore_failure = true` on `announce-release` — Slack/tweet failures don't abort the run

## Inspect After Running

```bash
RUN_ID=$(wf runs --tag release --limit 1 | awk 'NR==2{print $1}')
wf inspect $RUN_ID           # release_version variable
wf logs    $RUN_ID "publish[registry=npm]"
wf export  $RUN_ID --format tar --output release-record.tar.gz
```
