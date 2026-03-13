package executor

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/silocorp/workflow/internal/contextmap"
	"github.com/silocorp/workflow/internal/dag"
	"github.com/silocorp/workflow/internal/logger"
	"github.com/silocorp/workflow/internal/storage"
)

type SequentialExecutor struct {
	db *storage.Store
}

func NewSequentialExecutor(db *storage.Store) *SequentialExecutor {
	return &SequentialExecutor{
		db: db,
	}
}

func (e *SequentialExecutor) Execute(ctx context.Context, d *dag.DAG, ctxMap *contextmap.ContextMap) (*storage.Run, error) {
	resetDAGState(d)
	run, err := initialiseRun(e, d, storage.ExecutionSequential, 1)
	if err != nil {
		return nil, fmt.Errorf("failed to initialise run: %w", err)
	}

	persistDAGCache(e, d, run.ID)
	persistTaskDependencies(e, d, run.ID)

	emitProgress(ctx, ProgressEvent{Kind: ProgressRunStarted, RunID: run.ID, RunStatus: storage.RunRunning})
	emitAuditEvent(e, run.ID, "run_started", map[string]interface{}{
		"workflow": d.Name, "mode": string(storage.ExecutionSequential),
	})
	logger.Info("run started",
		"run_id", run.ID, "workflow", d.Name,
		"executor", "sequential", "task_count", d.TotalTasks, "levels", len(d.Levels))

	// Execute level by level (respects dependencies)
	for levelIdx, level := range d.Levels {
		logger.Debug("executing level",
			"run_id", run.ID, "workflow", d.Name,
			"level", levelIdx, "tasks", len(level))

		for _, node := range level {
			// Check context cancellation between tasks.
			select {
			case <-ctx.Done():
				run.Status = storage.RunCancelled
				run.EndTime = sql.NullTime{Time: time.Now(), Valid: true}
				run.DurationMs = sql.NullInt64{Int64: run.EndTime.Time.Sub(run.StartTime).Milliseconds(), Valid: true}
				updateRunStatus(e, run)
				logger.Warn("run cancelled",
					"run_id", run.ID, "workflow", d.Name, "reason", ctx.Err())
				return run, ctx.Err()
			default:
			}

			if err := executeNode(e, ctx, run.ID, node, d, ctxMap); err != nil {
				// External cancellation (SIGINT, parent timeout): the task was
				// killed by the signal rather than failing on its own.
				if ctx.Err() != nil {
					run.Status = storage.RunCancelled
					run.EndTime = sql.NullTime{Time: time.Now(), Valid: true}
					run.DurationMs = sql.NullInt64{Int64: run.EndTime.Time.Sub(run.StartTime).Milliseconds(), Valid: true}
					updateRunStatus(e, run)
					logger.Warn("run cancelled during task",
						"run_id", run.ID, "workflow", d.Name,
						"task", node.ID, "reason", ctx.Err())
					return run, ctx.Err()
				}

				// Genuine task failure.
				run.Status = storage.RunFailed
				run.EndTime = sql.NullTime{Time: time.Now(), Valid: true}
				run.DurationMs = sql.NullInt64{Int64: run.EndTime.Time.Sub(run.StartTime).Milliseconds(), Valid: true}
				updateRunStatus(e, run)

				// Trigger global forensic trap
				if d.GlobalTrap != nil {
					executeGlobalForensicTrap(e, ctx, run.ID, d, ctxMap, err)
				}

				// Refresh from DB to get trigger-managed stats
				if updated, rerr := e.GetStore().GetRun(run.ID); rerr == nil {
					run = updated
				}

				runFailureHook(e, run)
				return run, fmt.Errorf("task %s failed: %w", node.ID, err)
			}
		}
	}

	run.Status = storage.RunSuccess
	run.EndTime = sql.NullTime{Time: time.Now(), Valid: true}
	run.DurationMs = sql.NullInt64{Int64: run.EndTime.Time.Sub(run.StartTime).Milliseconds(), Valid: true}
	updateRunStatus(e, run)

	// Refresh from DB to get trigger-managed stats
	if updated, err := e.GetStore().GetRun(run.ID); err == nil {
		run = updated
	}

	emitProgress(ctx, ProgressEvent{Kind: ProgressRunDone, RunID: run.ID, RunStatus: storage.RunSuccess})
	emitAuditEvent(e, run.ID, "run_completed", map[string]interface{}{
		"status": string(storage.RunSuccess), "duration_ms": run.DurationMs.Int64,
	})
	logger.Info("run completed",
		"run_id", run.ID, "workflow", d.Name,
		"status", "success", "duration_ms", run.DurationMs.Int64,
		"tasks_total", run.TotalTasks, "tasks_success", run.TasksSuccess,
		"tasks_failed", run.TasksFailed, "tasks_skipped", run.TasksSkipped)

	return run, nil
}

func (e *SequentialExecutor) Resume(ctx context.Context, runID string) (*storage.Run, error) {
	return doResume(e, ctx, runID, 1)
}

func (e *SequentialExecutor) GetStore() *storage.Store {
	return e.db
}
