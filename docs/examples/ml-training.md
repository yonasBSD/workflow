# Example 04 — ML Training Pipeline

**File**: `files/examples/04-ml-training.toml`
**Industry**: Machine Learning / MLOps
**Tags**: `ml`, `training`, `model`

## Features Demonstrated

- Three parallel model trainers (XGBoost, LightGBM, TabNet)
- `register` capturing experiment ID and best F1 score
- `if` conditional on F1 score for model promotion gate
- `working_dir` for experiment artifacts
- `env` for experiment tracking
- `ignore_failure` on optional Optuna sweep
- Global `on_failure` forensic handler
- Runtime `--var` for experiment naming
- `timeout` on training tasks

## Why this pattern matters

Model training pipelines are expensive — hours of GPU time, terabytes of data movement. A failure mid-run without resume capability means restarting from scratch. A promotion gate without an auditable record means "we deployed the best model we had at 2am" is institutional memory rather than a verifiable fact.

The three trainers run in parallel, each registering its F1 score as a named variable. `select-champion` evaluates those scores and registers the winner. The promotion gate uses `if` to check the champion's score against a threshold — the threshold is a runtime `--var`, not hardcoded. Every decision — which experiment ID was selected, what score triggered promotion — is in the run record and retrievable with `wf inspect` long after the training cluster has been torn down.

## Pipeline Structure

```
[prepare-data]
  ├── [train-xgboost]  ─┐
  ├── [train-lightgbm]  ├→ [select-champion]
  └── [train-tabnet]   ─┘        ↓
  └── [optuna-sweep]             (if best_f1 > 0.90)
                             [promote-champion] → [deploy-champion]
```

Global forensic: `[alert-ml-failure]`

## Run Commands

```bash
# Standard run with experiment name
wf run ml-training --var EXPERIMENT_NAME=run-$(date +%s) --parallel --print-output

# With timeout for training tasks
wf run ml-training \
  --var EXPERIMENT_NAME=experiment-001 \
  --work-stealing \
  --timeout 2h \
  --print-output

# Visualise
wf graph ml-training
```

## What to Observe

- Three model training tasks run simultaneously
- `wf inspect` shows `exp_id`, `best_f1`, `champion_model` variables
- `promote-champion` is gated by `if = 'best_f1 > "0.90"'` — inspect to see whether the condition was met
- `optuna-sweep` has `ignore_failure = true` — it won't abort the run if it fails
- `deploy-champion` uses `{{.champion_model}}` interpolation — confirm the model name appears in the log

## Inspect After Running

```bash
RUN_ID=$(wf runs --tag ml --limit 1 | awk 'NR==2{print $1}')
wf inspect $RUN_ID                     # exp_id, best_f1, champion_model
wf logs    $RUN_ID select-champion     # champion selection logic output
wf audit   $RUN_ID                     # see if promote-champion was skipped or ran
```
