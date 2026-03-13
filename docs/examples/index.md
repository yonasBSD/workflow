# Examples

Twelve production-grade example workflows, each targeting a real industry use case. Together they exercise every implemented feature.

## Setup

```bash
export WF_PATHS_WORKFLOWS="$(pwd)/files/examples"
wf list    # confirms all 12 workflows are visible
```

## Feature Coverage

| Feature | 01 | 02 | 03 | 04 | 05 | 06 | 07 | 08 | 09 | 10 | 11 | 12 |
|---|:---:|:---:|:---:|:---:|:---:|:---:|:---:|:---:|:---:|:---:|:---:|:---:|
| `depends_on` | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |
| `register` | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |
| `if` conditional | ✓ | ✓ | — | ✓ | ✓ | ✓ | — | ✓ | ✓ | ✓ | ✓ | ✓ |
| `matrix` expansion | — | — | ✓ | — | — | — | ✓ | ✓ | — | — | ✓ | ✓ |
| `ignore_failure` | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |
| `retries` + `retry_delay` | ✓ | ✓ | ✓ | — | ✓ | — | ✓ | — | ✓ | — | — | ✓ |
| `timeout` | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |
| `env` vars | ✓ | ✓ | — | ✓ | ✓ | ✓ | ✓ | — | ✓ | ✓ | ✓ | ✓ |
| `clean_env` | — | — | ✓ | — | — | ✓ | — | — | — | ✓ | — | — |
| `working_dir` | — | ✓ | ✓ | ✓ | — | — | ✓ | ✓ | — | ✓ | ✓ | — |
| `tags` | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |
| Task `on_failure` | ✓ | — | ✓ | — | ✓ | — | — | — | ✓ | — | — | ✓ |
| Global `on_failure` | ✓ | ✓ | — | ✓ | ✓ | — | — | — | — | — | ✓ | — |
| `--var` runtime vars | — | ✓ | — | ✓ | ✓ | ✓ | ✓ | — | — | — | — | ✓ |

## Examples

| # | File | Industry | Key Features |
|---|---|---|---|
| 01 | [CI/CD Pipeline](cicd-pipeline.md) | Platform Engineering | Parallel builds, forensic rollback, conditional gate |
| 02 | [Data ETL](data-etl.md) | Data Engineering | Parallel extraction, register + if, retries |
| 03 | [Database Backup](database-backup.md) | Database Admin | Matrix expansion, clean_env, per-matrix forensics |
| 04 | [ML Training Pipeline](ml-training.md) | MLOps | Parallel trainers, champion selection, if conditional |
| 05 | [Infrastructure Provisioning](infra-provisioning.md) | Cloud Engineering | Strict chain, cascading forensic teardown |
| 06 | [Security Audit](security-audit.md) | AppSec / SecOps | Six parallel scanners, clean_env, pentest gate |
| 07 | [Release Management](release-management.md) | DevOps | Five-registry matrix, retries, runtime --var |
| 08 | [Log Analysis](log-analysis.md) | SRE / Observability | Service matrix, SLO breach condition |
| 09 | [Order Processing](order-processing.md) | E-Commerce | Saga pattern, compensating transactions |
| 10 | [System Maintenance](system-maintenance.md) | SRE / Platform | Six parallel health checks, cert renewal condition |
| 11 | [Compliance Audit](compliance-audit.md) | Finance / Healthcare / Gov | Framework matrix (SOC2/ISO27001/HIPAA), coverage gate |
| 12 | [Cost Modeling](cost-modeling.md) | Engineering / FinOps | Pre/post deployment cost delta, budget gate, variance alert |
