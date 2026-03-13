# Security Policy

## Supported Versions

Only the latest release receives security updates.

| Version | Supported |
|---|---|
| latest | ✔ |
| older  | ✖ |

---

## Reporting a Vulnerability

**Do not report security vulnerabilities through public GitHub issues.**

Use [GitHub Security Advisories](https://github.com/joelfokou/workflow/security/advisories/new) to submit a private report. You will receive an acknowledgement within 48 hours.

Include:
1. `wf --version` output
2. A description of the vulnerability
3. Steps to reproduce — a malicious TOML file, a crafted workflow name, or a specific command sequence
4. The impact you believe the vulnerability enables

---

## Review process

1. Acknowledgement within 48 hours
2. Investigation and impact assessment
3. If confirmed: patch released, CVE requested if warranted
4. Contribution acknowledged in release notes unless you prefer anonymity

---

## Security model

`wf` has a formal, documented security model. Full details at [docs/security/model.md](docs/security/model.md).

| Threat | Defence |
|---|---|
| Path traversal via workflow name | Two-layer validation (`internal/security/validate.go`) |
| Template injection via registered variables | Regex-only `{{.var}}` substitution — no template logic |
| Dynamic-linker injection via task `env` | Deny-list: `LD_PRELOAD`, `LD_LIBRARY_PATH`, `DYLD_*`, and others |
| Memory exhaustion via unbounded output | 10 MiB capture cap with silent drop |
| Working directory escape | Blocks `/proc`, `/sys`, `/dev` and null bytes |
| Log and database information disclosure | Files created at mode `0600` |
| SQL injection | Parameterised queries throughout |

Automated tests covering each class are in `tests/security/`.

---

## Scope

Areas of particular interest:
- Arbitrary code execution — `wf` executing commands not in the intended workflow
- Privilege escalation — commands running with higher privileges than the calling user
- Audit trail tampering — any path to modify or delete audit records after creation
- Variable poisoning — a task overwriting another task's registered variable
- Path traversal — loading a workflow file outside the configured workflows directory

Out of scope: attacks requiring physical host access, vulnerabilities in the host OS or SQLite itself, and attacks that require the attacker to already control the workflows directory.