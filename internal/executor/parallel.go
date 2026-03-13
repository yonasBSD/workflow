package executor

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"time"

	"github.com/silocorp/workflow/internal/contextmap"
	"github.com/silocorp/workflow/internal/dag"
	"github.com/silocorp/workflow/internal/logger"
	"github.com/silocorp/workflow/internal/storage"
)

type ParallelExecutor struct {
	db          *storage.Store
	maxParallel int // Maximum concurrent tasks
}

func NewParallelExecutor(db *storage.Store, maxParallel int) *ParallelExecutor {
	if maxParallel <= 0 {
		maxParallel = 4 // Default concurrency
	}

	return &ParallelExecutor{
		db:          db,
		maxParallel: maxParallel,
	}
}

func (e *ParallelExecutor) Execute(ctx context.Context, d *dag.DAG, ctxMap *contextmap.ContextMap) (*storage.Run, error) {
	resetDAGState(d)
	run, err := initialiseRun(e, d, storage.ExecutionParallel, e.maxParallel)
	if err != nil {
		return nil, fmt.Errorf("failed to initialise run: %w", err)
	}

	persistDAGCache(e, d, run.ID)
	persistTaskDependencies(e, d, run.ID)

	emitProgress(ctx, ProgressEvent{Kind: ProgressRunStarted, RunID: run.ID, RunStatus: storage.RunRunning})
	emitAuditEvent(e, run.ID, "run_started", map[string]interface{}{
		"workflow": d.Name, "mode": string(storage.ExecutionParallel), "max_parallel": e.maxParallel,
	})
	logger.Info("run started",
		"run_id", run.ID, "workflow", d.Name,
		"executor", "parallel", "max_parallel", e.maxParallel,
		"task_count", d.TotalTasks, "levels", len(d.Levels))

	// Semaphore to limit concurrency
	sem := make(chan struct{}, e.maxParallel)

	// Execute level by level
	for levelIdx, level := range d.Levels {
		logger.Debug("executing level in parallel",
			"run_id", run.ID, "workflow", d.Name,
			"level", levelIdx, "tasks", len(level))

		// Wait group for this level
		var wg sync.WaitGroup
		var mu sync.Mutex
		var levelErrors []error

		for _, node := range level {
			// Check context cancellation
			select {
			case <-ctx.Done():
				run.Status = storage.RunCancelled
				run.EndTime = sql.NullTime{Time: time.Now(), Valid: true}
				run.DurationMs = sql.NullInt64{Int64: run.EndTime.Time.Sub(run.StartTime).Milliseconds(), Valid: true}
				updateRunStatus(e, run)
				return run, ctx.Err()
			default:
			}

			if !node.CanRunInParallel || len(level) == 1 {
				// Execute sequentially
				if err := executeNode(e, ctx, run.ID, node, d, ctxMap); err != nil {
					mu.Lock()
					levelErrors = append(levelErrors, err)
					mu.Unlock()
					break // Stop on first error
				}
				continue
			}

			// Execute in parallel
			wg.Add(1)
			go func(n *dag.Node) {
				defer wg.Done()

				// Acquire semaphore
				sem <- struct{}{}
				defer func() { <-sem }()

				if err := executeNode(e, ctx, run.ID, n, d, ctxMap); err != nil {
					mu.Lock()
					levelErrors = append(levelErrors, err)
					mu.Unlock()
				}
			}(node)
		}

		// Wait for all parallel tasks in this level
		wg.Wait()

		// Check for errors
		if len(levelErrors) > 0 {
			for _, lerr := range levelErrors {
				logger.Error("level task failed",
					"run_id", run.ID, "workflow", d.Name,
					"level", levelIdx, "error", lerr)
			}

			// External cancellation (SIGINT, parent timeout): errors are
			// artefacts of the kill signal, not genuine task failures.
			if ctx.Err() != nil {
				run.Status = storage.RunCancelled
				run.EndTime = sql.NullTime{Time: time.Now(), Valid: true}
				run.DurationMs = sql.NullInt64{Int64: run.EndTime.Time.Sub(run.StartTime).Milliseconds(), Valid: true}
				updateRunStatus(e, run)
				return run, ctx.Err()
			}

			// Genuine task failure.
			run.Status = storage.RunFailed
			run.EndTime = sql.NullTime{Time: time.Now(), Valid: true}
			run.DurationMs = sql.NullInt64{Int64: run.EndTime.Time.Sub(run.StartTime).Milliseconds(), Valid: true}
			updateRunStatus(e, run)

			// Trigger global forensic trap
			if d.GlobalTrap != nil {
				executeGlobalForensicTrap(e, ctx, run.ID, d, ctxMap, fmt.Errorf("level %d failed", levelIdx))
			}

			// Refresh from DB to get trigger-managed stats
			if updated, rerr := e.GetStore().GetRun(run.ID); rerr == nil {
				run = updated
			}

			runFailureHook(e, run)
			return run, fmt.Errorf("level %d failed with %d errors", levelIdx, len(levelErrors))
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

func (e *ParallelExecutor) Resume(ctx context.Context, runID string) (*storage.Run, error) {
	return doResume(e, ctx, runID, e.maxParallel)
}

func (e *ParallelExecutor) GetStore() *storage.Store {
	return e.db
}
