package executor

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/joelfokou/workflow/internal/contextmap"
	"github.com/joelfokou/workflow/internal/dag"
	"github.com/joelfokou/workflow/internal/logger"
	"github.com/joelfokou/workflow/internal/storage"
)

// WorkStealingExecutor is a dependency-aware scheduler that maintains a ready-queue
// and dispatches tasks as soon as all their dependencies complete — regardless of
// topological level.  This achieves true maximum concurrency: a task no longer needs
// to wait for unrelated siblings that happen to share its level.
//
// Concurrency model:
//   - One goroutine per launched task (capped by semaphore at numWorkers).
//   - Each goroutine, after finishing its task, decrements the pending-dependency
//     counter of every direct dependent.  When a counter hits 0 the goroutine
//     launches that dependent inline (wg.Add before the parent's wg.Done fires),
//     keeping the WaitGroup count > 0 for as long as any work is reachable.
//   - The main goroutine waits on wg.Wait() and therefore has no polling loop.
type WorkStealingExecutor struct {
	db         *storage.Store
	numWorkers int
}

// NewWorkStealingExecutor returns a WorkStealingExecutor that runs at most
// numWorkers tasks concurrently.  If numWorkers ≤ 0 it is set to 4.
func NewWorkStealingExecutor(db *storage.Store, numWorkers int) *WorkStealingExecutor {
	if numWorkers <= 0 {
		numWorkers = 4
	}
	return &WorkStealingExecutor{db: db, numWorkers: numWorkers}
}

func (e *WorkStealingExecutor) Execute(ctx context.Context, d *dag.DAG, ctxMap *contextmap.ContextMap) (*storage.Run, error) {
	resetDAGState(d)
	run, err := initialiseRun(e, d, storage.ExecutionWorkStealing, e.numWorkers)
	if err != nil {
		return nil, fmt.Errorf("failed to initialise run: %w", err)
	}

	persistDAGCache(e, d, run.ID)
	persistTaskDependencies(e, d, run.ID)

	emitProgress(ctx, ProgressEvent{Kind: ProgressRunStarted, RunID: run.ID, RunStatus: storage.RunRunning})
	emitAuditEvent(e, run.ID, "run_started", map[string]interface{}{
		"workflow": d.Name, "mode": string(storage.ExecutionWorkStealing), "workers": e.numWorkers,
	})
	logger.Info("run started",
		"run_id", run.ID, "workflow", d.Name,
		"executor", "work-stealing", "workers", e.numWorkers,
		"task_count", d.TotalTasks)

	// ── Build per-node pending dependency counters ───────────────────────────
	// Forensic tasks are excluded; they are triggered explicitly on failure only.
	pendingDeps := make(map[string]*atomic.Int32, len(d.Nodes))
	for id, node := range d.Nodes {
		if node.TaskDef.Type == dag.TaskTypeForensic {
			continue
		}
		c := new(atomic.Int32)
		c.Store(int32(len(filterNonForensicDeps(node.Dependencies)))) //nolint:gosec // dep counts cannot realistically exceed int32 max
		pendingDeps[id] = c
	}

	// ── Concurrency primitives ───────────────────────────────────────────────
	sem := make(chan struct{}, e.numWorkers) // bounds active goroutines
	var wg sync.WaitGroup                    // tracks all launched goroutines

	cancelCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Error collection — first non-ignorable failure wins.
	var (
		errMu   sync.Mutex
		runErrs []error
	)
	recordError := func(err error) {
		errMu.Lock()
		runErrs = append(runErrs, err)
		errMu.Unlock()
	}

	// ── launch starts a goroutine for node n ─────────────────────────────────
	// IMPORTANT: wg.Add(1) is called BEFORE returning so callers can rely on
	// the WaitGroup being incremented even when the goroutine hasn't started.
	var launch func(n *dag.Node)
	launch = func(n *dag.Node) {
		wg.Add(1)
		go func() {
			defer wg.Done()

			// Wait for a concurrency slot or bail on cancellation.
			select {
			case sem <- struct{}{}:
			case <-cancelCtx.Done():
				return
			}

			err := executeNode(e, cancelCtx, run.ID, n, d, ctxMap)
			<-sem // release slot immediately; dependent tasks may grab it

			if err != nil && !n.TaskDef.IgnoreFailure {
				recordError(err)
				cancel() // propagate cancellation to all in-flight tasks
				return
			}

			// If cancelled by a sibling failure, don't queue dependents.
			if cancelCtx.Err() != nil {
				return
			}

			// Decrement each non-forensic dependent's pending count.
			// The goroutine that drives the counter to 0 is responsible for
			// launching that dependent — ensuring exactly-once dispatch and
			// keeping wg > 0 until all reachable work is done.
			for _, dep := range n.Dependents {
				if dep.TaskDef.Type == dag.TaskTypeForensic {
					continue
				}
				pc, tracked := pendingDeps[dep.ID]
				if !tracked {
					continue
				}
				if pc.Add(-1) == 0 {
					launch(dep)
				}
			}
		}()
	}

	// ── Seed with root (dependency-free) non-forensic nodes ─────────────────
	for _, node := range d.RootNodes {
		if node.TaskDef.Type == dag.TaskTypeForensic {
			continue
		}
		launch(node)
	}

	// ── Wait for all goroutines ──────────────────────────────────────────────
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()

	select {
	case <-done:
		// All launched goroutines finished.
	case <-ctx.Done():
		// External cancellation (e.g. SIGINT).
		cancel()
		<-done
	}

	// ── Determine outcome ────────────────────────────────────────────────────
	errMu.Lock()
	defer errMu.Unlock()

	// External cancellation (SIGINT, parent timeout, etc.) takes priority.
	// Any errors collected after the external cancel are artefacts of the
	// kill signal, not genuine workflow failures.
	if ctx.Err() != nil {
		run.Status = storage.RunCancelled
		run.EndTime = sql.NullTime{Time: time.Now(), Valid: true}
		run.DurationMs = sql.NullInt64{Int64: run.EndTime.Time.Sub(run.StartTime).Milliseconds(), Valid: true}
		updateRunStatus(e, run)
		logger.Warn("run cancelled",
			"run_id", run.ID, "workflow", d.Name, "reason", ctx.Err())
		return run, ctx.Err()
	}

	if len(runErrs) > 0 {
		run.Status = storage.RunFailed
		run.EndTime = sql.NullTime{Time: time.Now(), Valid: true}
		run.DurationMs = sql.NullInt64{Int64: run.EndTime.Time.Sub(run.StartTime).Milliseconds(), Valid: true}
		updateRunStatus(e, run)

		logger.Error("run failed",
			"run_id", run.ID, "workflow", d.Name,
			"errors", len(runErrs), "error", runErrs[0])

		// Trigger global forensic trap if configured.
		if d.GlobalTrap != nil {
			executeGlobalForensicTrap(e, ctx, run.ID, d, ctxMap, runErrs[0])
		}

		// Refresh from DB to get trigger-managed stats.
		if updated, rerr := e.GetStore().GetRun(run.ID); rerr == nil {
			run = updated
		}

		runFailureHook(e, run)
		emitProgress(ctx, ProgressEvent{Kind: ProgressRunDone, RunID: run.ID, RunStatus: storage.RunFailed})
		emitAuditEvent(e, run.ID, "run_completed", map[string]interface{}{
			"status": string(storage.RunFailed), "errors": len(runErrs),
		})
		return run, fmt.Errorf("workflow failed: %w", runErrs[0])
	}

	run.Status = storage.RunSuccess
	run.EndTime = sql.NullTime{Time: time.Now(), Valid: true}
	run.DurationMs = sql.NullInt64{Int64: run.EndTime.Time.Sub(run.StartTime).Milliseconds(), Valid: true}
	updateRunStatus(e, run)

	// Refresh from DB to get trigger-managed stats.
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

func (e *WorkStealingExecutor) Resume(ctx context.Context, runID string) (*storage.Run, error) {
	return doResume(e, ctx, runID, e.numWorkers)
}

func (e *WorkStealingExecutor) GetStore() *storage.Store {
	return e.db
}

// filterNonForensicDeps returns only non-forensic nodes from a dependency list.
// Used when computing the initial pending count for a node, since forensic
// nodes are never part of the normal execution graph.
func filterNonForensicDeps(deps []*dag.Node) []*dag.Node {
	out := deps[:0:0] // zero-alloc when slice is empty
	for _, d := range deps {
		if d.TaskDef.Type != dag.TaskTypeForensic {
			out = append(out, d)
		}
	}
	return out
}
