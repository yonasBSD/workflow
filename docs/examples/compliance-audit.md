# Example 11 — Regulatory Compliance Audit

**File**: `files/examples/11-compliance-audit.toml`
**Industry**: Finance / Healthcare / Government
**Tags**: `compliance`, `audit`, `evidence`, `annual`

## Features Demonstrated

- `register` — captures control pass/fail counts and evidence paths per domain
- `if` conditional — gates report generation on ≥ 90% control coverage
- `matrix` — maps gathered evidence to three regulatory frameworks (SOC 2, ISO 27001, HIPAA) in parallel
- `on_failure` (workflow-level) — escalates via forensic task if the audit cannot complete
- `timeout` — bounds all evidence-collection tasks
- `ignore_failure` — a single control domain failure must not abort the full audit
- `working_dir` — evidence stored in a controlled directory path
- `env` vars — framework selector, compliance portal endpoint, API token

## What Makes This Different from Example 06 (Security Audit)

Example 06 (`security-audit`) executes vulnerability *scanners* — it finds current attack surface. Example 11 (`compliance-audit`) verifies *control existence and effectiveness* against regulatory frameworks. The distinction matters:

- Security audit: "Are we exploitable right now?"
- Compliance audit: "Can we prove we've followed the required controls over the audit period?"

Both produce evidence, but compliance audits produce artefacts that external auditors review — attestation reports, evidence packages submitted to compliance portals (Vanta, Drata, etc.), and framework-mapped control mappings.

## Pipeline Structure

```
[init-audit-session]
  ├── [verify-access-controls]  ─┐  (AC: MFA, least-privilege, access logging)
  ├── [verify-data-controls]     ├→ [collect-evidence]
  └── [verify-operational-controls] ─┘  (OC: incident response, patching, DR)
                                       ↓
                        [map-framework-controls] × {SOC2, ISO27001, HIPAA}
                                       ↓
                        [compute-coverage-score]
                                       ↓
                        [generate-compliance-report]  ← gated on coverage ≥ 90%
                          ↓                     ↓
               [submit-to-compliance-portal]  [notify-compliance-team]

[escalate-incomplete-audit]  ← forensic, fires if any task fails fatally
```

## Run Commands

```bash
# Standard audit run
wf run compliance-audit --work-stealing --print-output

# Parallel, max 4 concurrent tasks
wf run compliance-audit --parallel --max-parallel 4 --print-output

# Full run with timeout ceiling
wf run compliance-audit --work-stealing --timeout 2h --print-output

# Dry-run to inspect execution plan
wf run compliance-audit --dry-run

# Visualise the DAG (note matrix expansion for three frameworks)
wf graph compliance-audit
wf graph compliance-audit --format html --export compliance-audit.html   # interactive HTML file
```

## What to Observe

- Three domain checks (`verify-access-controls`, `verify-data-controls`, `verify-operational-controls`) start simultaneously
- `register` captures integer pass counts (`ac_pass`, `dc_pass`, `oc_pass`) — inspect these with `wf inspect`
- Matrix expansion creates three `map-framework-controls` nodes (SOC2, ISO27001, HIPAA) — each uses `{{.framework}}` in its `name` and `cmd`
- `generate-compliance-report` is gated by `if = 'coverage_result == "pass"'` — if coverage falls below 90%, this task is skipped (visible in `wf audit`)
- `escalate-incomplete-audit` is a `type = "forensic"` task wired to `on_failure` at the workflow level — it only runs if the audit fails to complete; inspect the audit trail to see if it fired

## Inspect After Running

```bash
RUN_ID=$(wf runs --tag compliance --limit 1 | awk 'NR==2{print $1}')

# Check control pass counts per domain
wf inspect $RUN_ID

# Chronological event trail — see task order, skips, forensic fire
wf audit $RUN_ID

# Per-task output
wf logs $RUN_ID verify-data-controls      # see the DC-9 BAA finding
wf logs $RUN_ID compute-coverage-score    # see the 96% / 97% coverage score
wf logs $RUN_ID generate-compliance-report

# Compare two audit runs side by side
wf diff <run-id-previous> $RUN_ID
```

## Adapting for Production

1. Replace the simulated `sleep` + `echo` blocks with real tool invocations:
   - Access controls: query your IdP (Okta, Azure AD) API for MFA status, policy reviews
   - Data controls: query AWS Config, Azure Policy, or GCP Security Command Center
   - Ops controls: pull from your ticketing system (Jira), pentest portal (Cobalt), or change management system
2. Update `env` blocks with real API endpoints and tokens (inject via `WF_*` env vars or `--var`)
3. Replace `submit-to-compliance-portal` with your Vanta/Drata/Tugboat API client call
4. Adjust the coverage threshold in `compute-coverage-score` for your organisation's requirements
5. Add your `working_dir` path for evidence storage — recommend a version-controlled location
