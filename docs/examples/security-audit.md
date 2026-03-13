# Example 06 — Security Audit Pipeline

**File**: `files/examples/06-security-audit.toml`
**Industry**: Application Security / SecOps
**Tags**: `security`, `audit`, `compliance`

## Features Demonstrated

- Six independent parallel security scanners
- `clean_env = true` on all scanners — no environment leakage between tools
- `register` for finding counts per scanner
- `if` conditional gating the full pentest on risk level
- `timeout` per scanner tool
- `ignore_failure` per scanner — one tool failing does not abort the audit
- Runtime `--var` for scan target
- `env` for scanner API keys

## Why this pattern matters

Security audits run in practice the way they do in theory when each tool is independent and bounded. A DAST scanner that hangs for 6 hours should not block a SAST scan that takes 3 minutes. A secret scanner that returns a non-zero exit code should not abort the network exposure report. These are different tools with different failure modes — treating them as one script means one failure kills everything.

`clean_env = true` on each scanner is not cosmetic. Scanner tools frequently pick up credentials, proxy settings, and other env vars from the calling environment. An API token for Semgrep should not be visible to the Nmap process running in the same shell session. Each scanner starts with exactly what it needs and nothing more.

The pentest gate (`if` on `risk_level`) means the live penetration test only runs when the vulnerability surface is manageable. This is not a manual decision — it is a documented, auditable one: the run record shows what the risk level was and whether the pentest was triggered or skipped.

## Pipeline Structure

```
[init]
  ├── [scan-sast]      ─┐ (SAST — source code analysis)
  ├── [scan-sca]        │ (SCA — dependency vulnerabilities)
  ├── [scan-dast]       ├→ [analyse-findings] → [run-pentest] → [generate-report] → [notify-security]
  ├── [scan-secrets]    │ (secret scanning)
  ├── [scan-network]    │ (network exposure)
  └── [scan-config]    ─┘ (configuration audit)
       (all: clean_env, ignore_failure, timeout)
       (if risk_level condition) ↑
```

## Run Commands

```bash
# Full audit with target
wf run security-audit \
  --var PENTEST_TARGET=https://api.staging.example.com \
  --parallel \
  --print-output

# Work-stealing for fastest completion
wf run security-audit \
  --var PENTEST_TARGET=https://api.staging.example.com \
  --work-stealing \
  --max-parallel 6 \
  --print-output \
  --timeout 2h

# Visualise
wf graph security-audit
```

## What to Observe

- All six scanners start simultaneously — verify with `wf audit | grep task_started`
- Each scanner uses `clean_env = true` — no environment inheritance between tools
- `wf inspect` shows `sast_findings`, `sca_findings`, `dast_findings`, `secrets_findings`, `network_findings`, `config_findings`
- `run-pentest` is gated by an `if` condition on `risk_level` — check audit to see if it was skipped or executed
- Individual scanner failures are `ignore_failure = true` — the audit continues even if one tool is unavailable

## Inspect After Running

```bash
RUN_ID=$(wf runs --tag security --limit 1 | awk 'NR==2{print $1}')
wf inspect $RUN_ID      # all finding counts
wf audit   $RUN_ID      # see which tasks ran, which were skipped
wf logs    $RUN_ID generate-report   # full audit report output
```
