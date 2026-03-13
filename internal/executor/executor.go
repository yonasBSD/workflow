// Package executor provides functionality to execute workflows defined as directed acyclic graphs (DAGs).
package executor

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/joelfokou/workflow/internal/config"
	"github.com/joelfokou/workflow/internal/contextmap"
	"github.com/joelfokou/workflow/internal/dag"
	"github.com/joelfokou/workflow/internal/logger"
	"github.com/joelfokou/workflow/internal/storage"
)

// Executor defines the interface for task execution
type Executor interface {
	// Execute runs the entire DAG
	Execute(ctx context.Context, dag *dag.DAG, ctxMap *contextmap.ContextMap) (*storage.Run, error)

	// Resume continues execution from a previous failure
	Resume(ctx context.Context, runID string) (*storage.Run, error)

	// GetStore returns the underlying database store
	GetStore() *storage.Store
}

// resetDAGState clears all mutable node state before a new execution run.
// This allows the same *dag.DAG to be safely re-used across multiple Execute calls.
func resetDAGState(d *dag.DAG) {
	for _, node := range d.Nodes {
		node.Reset()
	}
	for _, node := range d.ForensicTasks {
		node.Reset()
	}
	if d.GlobalTrap != nil {
		d.GlobalTrap.Reset()
	}
}

func initialiseRun(e Executor, d *dag.DAG, executionMode storage.ExecutionMode, maxParallel int) (*storage.Run, error) {
	// Encode tags as a JSON array for storage and json_each() queries.
	tagsJSON := "[]"
	if len(d.Tags) > 0 {
		if b, err := json.Marshal(d.Tags); err == nil {
			tagsJSON = string(b)
		}
	}

	run := &storage.Run{
		WorkflowName:  d.Name,
		WorkflowFile:  d.FilePath,
		Status:        storage.RunRunning,
		StartTime:     time.Now(),
		TotalTasks:    d.TotalTasks,
		ExecutionMode: executionMode,
		MaxParallel:   maxParallel,
		Tags:          tagsJSON,
	}

	runID, err := e.GetStore().CreateRun(run)
	if err != nil {
		return nil, fmt.Errorf("failed to create run: %w", err)
	}
	run.ID = runID

	return run, nil
}

func initialiseTaskExecution(e Executor, runID string, node *dag.Node) (*storage.TaskExecution, error) {
	taskExec := &storage.TaskExecution{
		RunID:             runID,
		TaskID:            node.ID,
		TaskName:          node.TaskDef.Name,
		State:             storage.TaskPending,
		Command:           node.TaskDef.Cmd,
		WorkingDir:        sql.NullString{String: node.TaskDef.WorkingDir, Valid: true},
		MaxRetries:        node.TaskDef.Retries,
		IsForensic:        node.TaskDef.Type == dag.TaskTypeForensic,
		IsMatrixExpansion: node.IsExpanded,
		ConditionExpr:     sql.NullString{String: node.TaskDef.If, Valid: true},
		ConditionResult:   sql.NullBool{Bool: node.ConditionResult, Valid: true},
	}
	jsonBytes, err := json.Marshal(node.MatrixVars)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal matrix vars: %w", err)
	}
	taskExec.MatrixVars = sql.NullString{String: string(jsonBytes), Valid: true}

	taskExecID, err := e.GetStore().CreateTaskExecution(taskExec)
	if err != nil {
		return nil, fmt.Errorf("failed to create task execution: %w", err)
	}
	taskExec.ID = taskExecID

	return taskExec, nil
}

// executeNode runs a single task with full lifecycle
func executeNode(e Executor, ctx context.Context, runID string, node *dag.Node, d *dag.DAG, ctxMap *contextmap.ContextMap) error {

	// Check conditional (if specified)
	if node.TaskDef.If != "" {
		conditionalResult, err := ctxMap.EvalCondition(node.TaskDef.If)
		if err != nil {
			node.MarkEarlyFailed(err, -1, fmt.Sprintf("Condition '%s' evaluation failed: %v", node.TaskDef.If, err))
			_, err = initialiseTaskExecution(e, runID, node)
			if err != nil {
				return fmt.Errorf("failed to initialise task execution after condition failure: %w", err)
			}
			return fmt.Errorf("condition evaluation failed: %w", err)
		}

		if !conditionalResult {
			logger.Info("skipping task: condition not met",
				"run_id", runID, "workflow", d.Name,
				"task", node.ID, "task_name", node.TaskDef.Name,
				"condition", node.TaskDef.If)
			node.MarkSkipped(fmt.Sprintf("Condition '%s' evaluated to false", node.TaskDef.If), conditionalResult)
			taskExec, err := initialiseTaskExecution(e, runID, node)
			if err != nil {
				return fmt.Errorf("failed to initialise task execution after skipping: %w", err)
			}
			// Persist the skipped state so callers (and resume logic) see TaskSkipped, not TaskPending.
			taskExec.State = storage.TaskSkipped
			if updateErr := e.GetStore().UpdateTaskExecution(taskExec); updateErr != nil {
				logger.Error("failed to persist skipped state",
					"run_id", runID, "task", node.ID, "error", updateErr)
			}
			emitAuditEvent(e, runID, "task_skipped", map[string]interface{}{
				"task_id": node.ID, "condition": node.TaskDef.If,
			})
			return nil
		}
		logger.Debug("condition met",
			"run_id", runID, "workflow", d.Name,
			"task", node.ID, "task_name", node.TaskDef.Name,
			"condition", node.TaskDef.If)
		node.MarkConditionMet(conditionalResult)
	}

	// Register matrix variables (if expanded task)
	if node.IsExpanded {
		if err := ctxMap.SetMatrix(node.ID, node.MatrixVars); err != nil {
			node.MarkEarlyFailed(err, -1, fmt.Sprintf("Failed to set matrix variables: %v", err))
			_, err = initialiseTaskExecution(e, runID, node)
			if err != nil {
				return fmt.Errorf("failed to initialise task execution after matrix failure: %w", err)
			}
			return fmt.Errorf("failed to set matrix vars: %w", err)
		}
	}

	// Interpolate command template
	cmd, err := ctxMap.InterpolateCommand(node.ID, node.TaskDef.Cmd)
	if err != nil {
		interpErr := err
		node.MarkEarlyFailed(interpErr, -1, fmt.Sprintf("Command interpolation failed: %v", interpErr))
		if _, initErr := initialiseTaskExecution(e, runID, node); initErr != nil {
			return fmt.Errorf("failed to initialise task execution after command interpolation failure: %w", initErr)
		}
		return fmt.Errorf("command interpolation failed: %w", interpErr)
	}

	// Execute with retry logic
	var lastErr error
	maxAttempts := node.TaskDef.Retries + 1

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		taskExecution, err := initialiseTaskExecution(e, runID, node)
		if err != nil {
			return fmt.Errorf("failed to initialise task execution: %w", err)
		}

		if attempt == 1 {
			emitProgress(ctx, ProgressEvent{
				Kind: ProgressTaskStarted, RunID: runID,
				TaskID: node.ID, TaskName: node.TaskDef.Name,
				Status: storage.TaskRunning, Attempt: attempt,
			})
			emitAuditEvent(e, runID, "task_started", map[string]interface{}{
				"task_id": node.ID, "task_name": node.TaskDef.Name,
			})
		} else {
			emitProgress(ctx, ProgressEvent{
				Kind: ProgressTaskRetrying, RunID: runID,
				TaskID: node.ID, TaskName: node.TaskDef.Name,
				Status: storage.TaskRunning, Attempt: attempt,
			})
			emitAuditEvent(e, runID, "task_retried", map[string]interface{}{
				"task_id": node.ID, "attempt": attempt,
			})
		}

		if attempt == 1 {
			logger.Info("task started",
				"run_id", runID, "workflow", d.Name,
				"task", node.ID, "task_name", node.TaskDef.Name,
				"max_attempts", maxAttempts)
		}
		if attempt > 1 {
			logger.Warn("retrying task",
				"run_id", runID, "workflow", d.Name,
				"task", node.ID, "task_name", node.TaskDef.Name,
				"attempt", attempt, "max_attempts", maxAttempts)
			// Context-aware sleep: a run-level timeout or SIGINT cancels the
			// retry delay immediately instead of blocking for its full duration.
			if node.TaskDef.RetryDelay > 0 {
				select {
				case <-time.After(time.Duration(node.TaskDef.RetryDelay)):
				case <-ctx.Done():
					return fmt.Errorf("cancelled during retry delay: %w", ctx.Err())
				}
			}
		}

		// Update state
		now := time.Now()
		taskExecution.State = storage.TaskRunning
		taskExecution.StartTime = sql.NullTime{Time: now, Valid: true}
		taskExecution.Attempt = attempt
		node.MarkRunning(now)
		if err = e.GetStore().UpdateTaskExecution(taskExecution); err != nil {
			return fmt.Errorf("failed to update task execution: %w", err)
		}

		// Build log file path
		logPath := filepath.Join(config.Get().Paths.Logs, runID, node.ID, fmt.Sprintf("attempt_%d_%s.log", attempt, time.Now().Format("20060102_150405")))

		// Execute command
		output, exitCode, err := runCommand(ctx, cmd, node.TaskDef)
		if err == nil && exitCode == 0 {
			// Success
			endTime := time.Now()
			node.MarkSuccess(endTime, output, exitCode)
			taskExecution.State = storage.TaskSuccess
			taskExecution.EndTime = sql.NullTime{Time: endTime, Valid: true}
			taskExecution.DurationMs = sql.NullInt64{Int64: endTime.Sub(taskExecution.StartTime.Time).Milliseconds(), Valid: true}
			taskExecution.LogPath = sql.NullString{String: logPath, Valid: true}
			taskExecution.ExitCode = sql.NullInt64{Int64: int64(exitCode), Valid: true}

			if err := e.GetStore().UpdateTaskExecution(taskExecution); err != nil {
				return fmt.Errorf("failed to update task execution on success: %w", err)
			}

			if err := writeLogs(logPath, output); err != nil {
				logger.Error("failed to write task log",
					"run_id", runID, "workflow", d.Name,
					"task", node.ID, "path", logPath, "error", err)
			}

			logger.Info("task completed",
				"run_id", runID, "workflow", d.Name,
				"task", node.ID, "task_name", node.TaskDef.Name,
				"attempt", attempt, "duration_ms", taskExecution.DurationMs.Int64,
				"exit_code", exitCode)

			// Register output if specified.
			// Only the last non-empty line of stdout is captured (the rest is
			// available in the log file). This matches the shell convention of
			// echoing a single "return value" as the final line of a task.
			if node.TaskDef.Register != "" {
				registerVar := node.TaskDef.Register
				if strings.Contains(registerVar, "{{") {
					if interpolated, ierr := ctxMap.InterpolateCommand(node.ID, registerVar); ierr == nil {
						registerVar = interpolated
					}
				}
				if err := ctxMap.Set(node.ID, registerVar, lastOutputLine(output)); err != nil {
					return fmt.Errorf("failed to register output: %w", err)
				}
				logger.Debug("registered output variable",
					"run_id", runID, "workflow", d.Name,
					"task", node.ID, "variable", registerVar)
			}

			// Persist context checkpoint and mark dependency edges satisfied
			persistContextCheckpoint(e, runID, node.ID, ctxMap)
			markDependenciesSatisfied(e, runID, node)

			emitProgress(ctx, ProgressEvent{
				Kind: ProgressTaskDone, RunID: runID,
				TaskID: node.ID, TaskName: node.TaskDef.Name,
				Status: storage.TaskSuccess, Attempt: attempt,
			})
			// Emit buffered output so the single-threaded renderer can
			// print it atomically, preventing interleaving in parallel mode.
			if isPrintOutput(ctx) && output != "" {
				out, truncated := truncateOutput(output)
				emitProgress(ctx, ProgressEvent{
					Kind: ProgressTaskOutput, RunID: runID,
					TaskID: node.ID, TaskName: node.TaskDef.Name,
					Output: out, Truncated: truncated,
				})
			}
			emitAuditEvent(e, runID, "task_succeeded", map[string]interface{}{
				"task_id": node.ID, "attempt": attempt,
			})
			return nil
		}

		// Failure
		lastErr = err
		if exitCode != 0 && err == nil {
			lastErr = fmt.Errorf("command exited with code %d", exitCode)
		}

		taskExecution.State = storage.TaskFailed
		taskExecution.EndTime = sql.NullTime{Time: time.Now(), Valid: true}
		taskExecution.DurationMs = sql.NullInt64{Int64: taskExecution.EndTime.Time.Sub(taskExecution.StartTime.Time).Milliseconds(), Valid: true}
		taskExecution.LogPath = sql.NullString{String: logPath, Valid: true}
		taskExecution.ExitCode = sql.NullInt64{Int64: int64(exitCode), Valid: true}
		taskExecution.ErrorMessage = sql.NullString{String: lastErr.Error(), Valid: true}
		taskExecution.StackTrace = sql.NullString{String: fmt.Sprintf("Attempt failed: %v", lastErr), Valid: true}

		if err := e.GetStore().UpdateTaskExecution(taskExecution); err != nil {
			return fmt.Errorf("failed to update task execution on failure: %w", err)
		}

		if err := writeLogs(logPath, output); err != nil {
			logger.Error("failed to write task log",
				"run_id", runID, "workflow", d.Name,
				"task", node.ID, "path", logPath, "error", err)
		}

		logger.Warn("task attempt failed",
			"run_id", runID, "workflow", d.Name,
			"task", node.ID, "task_name", node.TaskDef.Name,
			"attempt", attempt, "max_attempts", maxAttempts,
			"exit_code", exitCode, "error", lastErr)
	}

	// Trigger forensic trap if configured
	if node.TaskDef.OnFailure != "" {
		executeTaskForensicTrap(e, ctx, runID, node, d, ctxMap, lastErr)
	}

	// Check if we should ignore failure
	if node.TaskDef.IgnoreFailure {
		logger.Warn("task failed, continuing (ignore_failure=true)",
			"run_id", runID, "workflow", d.Name,
			"task", node.ID, "task_name", node.TaskDef.Name, "error", lastErr)
		node.MarkFailed(time.Now(), lastErr)
		emitProgress(ctx, ProgressEvent{
			Kind: ProgressTaskFailed, RunID: runID,
			TaskID: node.ID, TaskName: node.TaskDef.Name,
			Status: storage.TaskFailed, Error: lastErr,
		})
		return nil
	}

	// All retries exhausted
	node.MarkFailed(time.Now(), lastErr)

	emitProgress(ctx, ProgressEvent{
		Kind: ProgressTaskFailed, RunID: runID,
		TaskID: node.ID, TaskName: node.TaskDef.Name,
		Status: storage.TaskFailed, Error: lastErr,
	})
	emitAuditEvent(e, runID, "task_failed", map[string]interface{}{
		"task_id": node.ID, "attempts": maxAttempts, "error": lastErr.Error(),
	})
	logger.Error("task failed",
		"run_id", runID, "workflow", d.Name,
		"task", node.ID, "task_name", node.TaskDef.Name,
		"attempts", maxAttempts, "error", lastErr)
	return fmt.Errorf("task failed after %d attempts: %w", maxAttempts, lastErr)
}

// runCommand executes a shell command with timeout
func runCommand(ctx context.Context, cmdStr string, taskDef *dag.TaskDefinition) (string, int, error) {

	// Create command context with timeout
	execCtx := ctx
	if taskDef.Timeout > 0 {
		var cancel context.CancelFunc
		execCtx, cancel = context.WithTimeout(ctx, time.Duration(taskDef.Timeout))
		defer cancel()
		logger.Debug("task timeout set", "task", taskDef.Name, "timeout", taskDef.Timeout.String())
	}

	// Execute via platform shell (sh on Unix, cmd.exe on Windows).
	cmd := shellCommand(execCtx, cmdStr)

	// Put the shell in its own process group so we can kill the whole group.
	setCmdProcessAttrs(cmd)

	// Override the default cancel (which only kills the shell PID) to kill the
	// entire process group. This prevents orphaned children (e.g. "sleep 30")
	// from holding stdout/stderr pipes open after context cancellation or timeout.
	cmd.Cancel = func() error {
		return killProcessGroup(cmd)
	}

	// Set working directory
	if taskDef.WorkingDir != "" {
		cmd.Dir = taskDef.WorkingDir
	}

	// Build merged environment.
	// By default tasks inherit the parent process environment, with task-level
	// keys overriding inherited ones.  Setting CleanEnv=true (TOML) or the
	// run-level --clean-env flag starts with an empty base instead.
	cleanEnv := taskDef.CleanEnv || isCleanEnv(ctx)
	if !cleanEnv && len(taskDef.Env) == 0 {
		// Fastest path: inherit everything, nothing to override.
		// cmd.Env = nil already means "inherit os.Environ()" in Go.
	} else {
		// Build a map so task-level vars shadow inherited ones deterministically.
		base := os.Environ()
		if cleanEnv {
			base = nil
		}
		envMap := make(map[string]string, len(base)+len(taskDef.Env))
		for _, kv := range base {
			if idx := strings.IndexByte(kv, '='); idx >= 0 {
				envMap[kv[:idx]] = kv[idx+1:]
			}
		}
		for k, v := range taskDef.Env {
			envMap[k] = v
		}
		merged := make([]string, 0, len(envMap))
		for k, v := range envMap {
			merged = append(merged, k+"="+v)
		}
		cmd.Env = merged
	}

	// Capture output, capped at maxCaptureBytes to prevent memory exhaustion
	// from runaway or adversarial tasks.
	stdout := &limitedBuffer{maxBytes: maxCaptureBytes}
	stderr := &limitedBuffer{maxBytes: maxCaptureBytes}
	cmd.Stdout = stdout
	cmd.Stderr = stderr

	// Run
	err := cmd.Run()
	output := stdout.String()
	if stderr.Len() > 0 {
		output += "\n[STDERR]\n" + stderr.String()
	}

	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = -1
		}
	}

	return output, exitCode, err
}

// maxCaptureBytes is the hard cap on how many bytes are buffered from a single
// task's combined stdout+stderr.  Output beyond this limit is silently dropped
// from the in-memory buffer; it is never written to the log file either, because
// writing multi-gigabyte logs from a single task is itself a security/resource
// concern.  Tasks that legitimately produce large output should redirect to a
// file inside their command.
const maxCaptureBytes = 10 * 1024 * 1024 // 10 MiB

// limitedBuffer wraps bytes.Buffer and stops accepting writes once maxBytes is
// reached.  Excess writes are silently discarded (the caller sees no error) so
// that the executing command is not killed just because it is verbose.
type limitedBuffer struct {
	buf      bytes.Buffer
	maxBytes int
	written  int
}

func (lb *limitedBuffer) Write(p []byte) (int, error) {
	remaining := lb.maxBytes - lb.written
	if remaining <= 0 {
		// Silently drop — return len(p) so the writer does not see an error.
		return len(p), nil
	}
	if len(p) > remaining {
		p = p[:remaining]
	}
	n, err := lb.buf.Write(p)
	lb.written += n
	return n, err
}

func (lb *limitedBuffer) String() string { return lb.buf.String() }
func (lb *limitedBuffer) Len() int       { return lb.buf.Len() }

// Ensure limitedBuffer satisfies io.Writer at compile time.
var _ io.Writer = (*limitedBuffer)(nil)

// lastOutputLine returns the last non-empty line from the stdout portion of
// a task's captured output (before any appended [STDERR] block). This is the
// value stored when a task declares register = "var_name".
func lastOutputLine(output string) string {
	// Strip the appended stderr block if present.
	if idx := strings.Index(output, "\n[STDERR]\n"); idx != -1 {
		output = output[:idx]
	}
	lines := strings.Split(output, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if trimmed := strings.TrimSpace(lines[i]); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

// truncateOutput caps output at maxOutputEvent bytes for the progress channel.
// The full content is always written to the log file regardless.
func truncateOutput(s string) (out string, truncated bool) {
	if len(s) <= maxOutputEvent {
		return s, false
	}
	return s[:maxOutputEvent], true
}

func writeLogs(path, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return fmt.Errorf("error creating log path: %w", err)
	}
	// 0600: task output may contain secrets; restrict to the owning user only.
	return os.WriteFile(path, []byte(content), 0600)
}

// executeTaskForensicTrap runs failure handler tasks
func executeTaskForensicTrap(e Executor, ctx context.Context, runID string, failedNode *dag.Node, d *dag.DAG, ctxMap *contextmap.ContextMap, originalErr error) {
	logger.Warn("forensic trap triggered",
		"run_id", runID, "workflow", d.Name,
		"failed_task", failedNode.ID, "trap", failedNode.TaskDef.OnFailure)

	// Register failure context
	ctxMap.Set("__forensic__", "failed_task", failedNode.ID)         //nolint:errcheck // known-valid internal key names
	ctxMap.Set("__forensic__", "error_message", originalErr.Error()) //nolint:errcheck // known-valid internal key names

	// Execute task-specific trap (forensic tasks live in d.ForensicTasks, not d.Nodes).
	trapNode, exists := d.ForensicTasks[failedNode.TaskDef.OnFailure]
	if !exists {
		logger.Error("forensic trap task not found",
			"run_id", runID, "workflow", d.Name,
			"trap", failedNode.TaskDef.OnFailure)
		return
	}
	if trapNode.TaskDef.Type != dag.TaskTypeForensic {
		logger.Error("trap task is not forensic type",
			"run_id", runID, "workflow", d.Name, "trap", trapNode.ID)
		return
	}
	// RunTrapOnce ensures the trap fires exactly once even when multiple tasks
	// fail simultaneously in work-stealing mode and all trigger the same trap.
	trapNode.RunTrapOnce(func() {
		logger.Info("running task-level forensic trap",
			"run_id", runID, "workflow", d.Name,
			"trap", trapNode.ID, "failed_task", failedNode.ID)
		emitAuditEvent(e, runID, "forensic_trap_fired", map[string]interface{}{
			"trap_id": trapNode.ID, "failed_task": failedNode.ID,
		})
		if err := executeNode(e, ctx, runID, trapNode, d, ctxMap); err != nil {
			logger.Error("forensic trap execution failed",
				"run_id", runID, "workflow", d.Name,
				"trap", trapNode.ID, "error", err)
		}
	})
}

// executeGlobalForensicTrap runs failure handler tasks
func executeGlobalForensicTrap(e Executor, ctx context.Context, runID string, d *dag.DAG, ctxMap *contextmap.ContextMap, originalErr error) {
	if d.GlobalTrap == nil {
		return
	}

	// Register failure context
	ctxMap.Set("__global_forensic__", "failed_dag", d.Name)                 //nolint:errcheck // known-valid internal key names
	ctxMap.Set("__global_forensic__", "error_message", originalErr.Error()) //nolint:errcheck // known-valid internal key names

	// Execute global trap
	logger.Warn("global forensic trap triggered",
		"run_id", runID, "workflow", d.Name, "trap", d.GlobalTrap.ID)
	if err := executeNode(e, ctx, runID, d.GlobalTrap, d, ctxMap); err != nil {
		logger.Error("global forensic trap execution failed",
			"run_id", runID, "workflow", d.Name,
			"trap", d.GlobalTrap.ID, "error", err)
	}
}

// updateRunStatus persists a run's mutable fields and logs a structured error
// if the write fails.  The call is intentionally non-fatal: a transient DB
// hiccup at finalisation must not change the process exit code or suppress the
// caller's return value.  Operators should monitor for these log lines to detect
// runs whose terminal status was not durably persisted.
func updateRunStatus(e Executor, run *storage.Run) {
	if err := e.GetStore().UpdateRun(run); err != nil {
		logger.Error("failed to persist run status — DB record may be stale",
			"run_id", run.ID, "status", string(run.Status), "error", err)
	}
}

// emitAuditEvent appends an event to the audit_trail table. Errors are logged
// and never propagated — audit writes must never break execution.
func emitAuditEvent(e Executor, runID, eventType string, data map[string]interface{}) {
	b, err := json.Marshal(data)
	if err != nil {
		logger.Error("audit: marshal failed", "event", eventType, "error", err)
		return
	}
	entry := &storage.AuditTrailEntry{
		RunID:     runID,
		EventType: eventType,
		EventData: string(b),
	}
	if err := e.GetStore().CreateAuditTrailEntry(entry); err != nil {
		logger.Error("audit: write failed", "event", eventType, "run_id", runID, "error", err)
	}
}

// runFailureHook executes the configured on_failure_hook shell command, if any.
// It is a best-effort call — errors are logged and never propagated to the caller.
func runFailureHook(e Executor, run *storage.Run) {
	hook := config.Get().OnFailureHook
	if hook == "" {
		return
	}

	// Find the first failed task ID to expose via env
	failedTaskID := ""
	if tasks, err := e.GetStore().ListTaskExecutions(storage.TaskFilters{RunID: run.ID, State: storage.TaskFailed}); err == nil && len(tasks) > 0 {
		failedTaskID = tasks[0].TaskID
	}

	cmd := shellCommandNoCtx(hook)
	cmd.Env = append(os.Environ(),
		"WF_RUN_ID="+run.ID,
		"WF_WORKFLOW="+run.WorkflowName,
		"WF_FAILED_TASK="+failedTaskID,
		"WF_STATUS="+string(run.Status),
	)

	if err := cmd.Run(); err != nil {
		logger.Error("on_failure_hook failed", "hook", hook, "run_id", run.ID, "error", err)
	} else {
		logger.Info("on_failure_hook executed", "hook", hook, "run_id", run.ID)
	}
}

// persistDAGCache serialises the DAG and stores it in dag_cache for the run.
func persistDAGCache(e Executor, d *dag.DAG, runID string) {
	dagJSON, err := d.Serialise()
	if err != nil {
		logger.Error("failed to serialize DAG for cache", "run_id", runID, "error", err)
		return
	}
	hasParallel := false
outer:
	for _, level := range d.Levels {
		for _, node := range level {
			if node.CanRunInParallel {
				hasParallel = true
				break outer
			}
		}
	}
	cache := &storage.DAGCache{
		RunID:       runID,
		DAGJSON:     string(dagJSON),
		TotalNodes:  d.TotalTasks,
		TotalLevels: len(d.Levels),
		HasParallel: hasParallel,
	}
	if err := e.GetStore().UpsertDAGCache(cache); err != nil {
		logger.Error("failed to persist DAG cache", "run_id", runID, "error", err)
	}
}

// persistTaskDependencies bulk-inserts all dependency edges for the run.
func persistTaskDependencies(e Executor, d *dag.DAG, runID string) {
	var deps []*storage.TaskDependency
	for _, node := range d.Nodes {
		for _, dep := range node.Dependencies {
			deps = append(deps, &storage.TaskDependency{
				RunID:           runID,
				TaskID:          node.ID,
				DependsOnTaskID: dep.ID,
			})
		}
	}
	if len(deps) == 0 {
		return
	}
	if err := e.GetStore().CreateTaskDependencyBatch(deps); err != nil {
		logger.Error("failed to persist task dependencies", "run_id", runID, "error", err)
	}
}

func varTypeString(vt contextmap.VarType) string {
	switch vt {
	case contextmap.VarTypeInt:
		return "int"
	case contextmap.VarTypeFloat:
		return "float"
	case contextmap.VarTypeBool:
		return "bool"
	default:
		return "string"
	}
}

func varTypeFromString(s string) contextmap.VarType {
	switch s {
	case "int":
		return contextmap.VarTypeInt
	case "float":
		return contextmap.VarTypeFloat
	case "bool":
		return contextmap.VarTypeBool
	default:
		return contextmap.VarTypeString
	}
}

// convertVariableValue parses a string back into the correctly-typed value.
func convertVariableValue(s string, vt contextmap.VarType) interface{} {
	switch vt {
	case contextmap.VarTypeInt:
		if v, err := strconv.ParseInt(s, 10, 64); err == nil {
			return int(v)
		}
	case contextmap.VarTypeFloat:
		if v, err := strconv.ParseFloat(s, 64); err == nil {
			return v
		}
	case contextmap.VarTypeBool:
		if v, err := strconv.ParseBool(s); err == nil {
			return v
		}
	}
	return s
}

// persistContextCheckpoint saves a checkpoint snapshot of all current context
// variables after a task succeeds.
func persistContextCheckpoint(e Executor, runID, taskID string, ctxMap *contextmap.ContextMap) {
	vars := ctxMap.Variables()
	if len(vars) == 0 {
		return
	}
	now := time.Now()
	snapshots := make([]*storage.ContextSnapshot, 0, len(vars))
	for _, v := range vars {
		snapshots = append(snapshots, &storage.ContextSnapshot{
			RunID:         runID,
			SnapshotTime:  now,
			SnapshotType:  storage.SnapshotCheckpoint,
			VariableName:  v.Name,
			VariableValue: fmt.Sprintf("%v", v.Value),
			VariableType:  varTypeString(v.Type),
			SetByTask:     v.SetBy,
			SetAt:         v.SetAt,
			IsReadOnly:    v.ReadOnly,
		})
	}
	if err := e.GetStore().CreateContextSnapshotBatch(snapshots); err != nil {
		logger.Error("failed to persist context checkpoint", "run_id", runID, "task_id", taskID, "error", err)
	}
}

// markDependenciesSatisfied marks all dependents' edges to node as satisfied.
func markDependenciesSatisfied(e Executor, runID string, node *dag.Node) {
	for _, dep := range node.Dependents {
		if err := e.GetStore().MarkDependencySatisfied(runID, dep.ID, node.ID); err != nil {
			logger.Error("failed to mark dependency satisfied", "run_id", runID, "task_id", dep.ID, "depends_on", node.ID, "error", err)
		}
	}
}

// doResume is the shared implementation for SequentialExecutor.Resume and
// ParallelExecutor.Resume. maxParallel=1 forces sequential execution within
// each DAG level.
func doResume(e Executor, ctx context.Context, runID string, maxParallel int) (*storage.Run, error) {
	if maxParallel < 1 {
		maxParallel = 1
	}

	// Load the run
	run, err := e.GetStore().GetRun(runID)
	if err != nil {
		return nil, fmt.Errorf("doResume: get run: %w", err)
	}

	// Rebuild DAG from the original workflow file
	def, err := dag.NewParser(run.WorkflowName).Parse()
	if err != nil {
		return run, fmt.Errorf("doResume: parse workflow: %w", err)
	}
	d, err := dag.NewBuilder(def).Build()
	if err != nil {
		return run, fmt.Errorf("doResume: build DAG: %w", err)
	}

	// Refresh the DAG cache so that wf-inspect and wf-diff reflect the
	// rebuilt graph (important when the TOML was edited between runs).
	persistDAGCache(e, d, runID)

	// Replace stale dependency rows. task_dependencies has no unique
	// constraint so we must delete before re-inserting to avoid duplicates.
	if err := e.GetStore().DeleteTaskDependenciesForRun(runID); err != nil {
		logger.Warn("doResume: could not clear stale task dependencies",
			"run_id", runID, "error", err)
	}
	persistTaskDependencies(e, d, runID)

	// Restore context from the most recent snapshot for each variable
	ctxMap := contextmap.NewContextMap()
	snapshots, err := e.GetStore().ListContextSnapshots(storage.ContextSnapshotFilters{RunID: runID})
	if err != nil {
		logger.Warn("failed to load context snapshots, starting fresh", "run_id", runID, "error", err)
	} else {
		// ListContextSnapshots returns rows ordered by snapshot_time DESC — first
		// occurrence of each variable name is therefore the most recent.
		seen := make(map[string]bool)
		for _, snap := range snapshots {
			if seen[snap.VariableName] {
				continue
			}
			seen[snap.VariableName] = true
			vt := varTypeFromString(snap.VariableType)
			ctxMap.RestoreVariable(snap.VariableName, convertVariableValue(snap.VariableValue, vt), vt, snap.SetByTask, snap.SetAt, snap.IsReadOnly)
		}
	}

	// Inject runtime variables supplied via --var (overrides snapshots).
	if vars := runtimeVars(ctx); len(vars) > 0 {
		for k, v := range vars {
			if err := ctxMap.Set("__runtime__", k, v); err != nil {
				logger.Warn("doResume: could not set runtime variable",
					"variable", k, "error", err)
			}
		}
	}

	// Determine which tasks already succeeded in a previous attempt
	prevSuccesses, err := e.GetStore().ListTaskExecutions(storage.TaskFilters{RunID: runID, State: storage.TaskSuccess})
	if err != nil {
		return run, fmt.Errorf("doResume: list task executions: %w", err)
	}
	succeededIDs := make(map[string]bool, len(prevSuccesses))
	for _, t := range prevSuccesses {
		succeededIDs[t.TaskID] = true
	}
	for _, node := range d.Nodes {
		if succeededIDs[node.ID] {
			node.MarkSuccess(time.Time{}, "", 0)
		}
	}

	// Update the run record
	run.Status = storage.RunResuming
	run.ResumeCount++
	run.LastResumeTime = sql.NullTime{Time: time.Now(), Valid: true}
	if err := e.GetStore().UpdateRun(run); err != nil {
		return run, fmt.Errorf("doResume: update run status: %w", err)
	}

	emitAuditEvent(e, runID, "run_resumed", map[string]interface{}{
		"resume_count": run.ResumeCount, "already_succeeded": len(succeededIDs),
	})
	logger.Info("run resuming",
		"run_id", runID, "workflow", run.WorkflowName,
		"resume_count", run.ResumeCount, "tasks_already_succeeded", len(succeededIDs))

	sem := make(chan struct{}, maxParallel)

	for levelIdx, level := range d.Levels {
		logger.Debug("resuming level",
			"run_id", runID, "workflow", run.WorkflowName,
			"level", levelIdx, "tasks", len(level))

		var wg sync.WaitGroup
		var mu sync.Mutex
		var levelErrors []error

		for _, node := range level {
			// Skip tasks that already succeeded
			if node.GetState() == dag.NodeStateSuccess {
				logger.Debug("skipping already-succeeded task",
					"run_id", runID, "workflow", run.WorkflowName, "task", node.ID)
				continue
			}

			select {
			case <-ctx.Done():
				wg.Wait()
				run.Status = storage.RunCancelled
				run.EndTime = sql.NullTime{Time: time.Now(), Valid: true}
				run.DurationMs = sql.NullInt64{Int64: run.EndTime.Time.Sub(run.StartTime).Milliseconds(), Valid: true}
				updateRunStatus(e, run)
				return run, ctx.Err()
			default:
			}

			if maxParallel == 1 || !node.CanRunInParallel || len(level) == 1 {
				if err := executeNode(e, ctx, run.ID, node, d, ctxMap); err != nil {
					levelErrors = append(levelErrors, err)
					break
				}
				continue
			}

			wg.Add(1)
			go func(n *dag.Node) {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()
				if err := executeNode(e, ctx, run.ID, n, d, ctxMap); err != nil {
					mu.Lock()
					levelErrors = append(levelErrors, err)
					mu.Unlock()
				}
			}(node)
		}

		wg.Wait()

		if len(levelErrors) > 0 {
			run.Status = storage.RunFailed
			run.EndTime = sql.NullTime{Time: time.Now(), Valid: true}
			run.DurationMs = sql.NullInt64{Int64: run.EndTime.Time.Sub(run.StartTime).Milliseconds(), Valid: true}
			updateRunStatus(e, run)

			if d.GlobalTrap != nil {
				executeGlobalForensicTrap(e, ctx, run.ID, d, ctxMap, levelErrors[0])
			}

			if updated, rerr := e.GetStore().GetRun(run.ID); rerr == nil {
				run = updated
			}
			runFailureHook(e, run)
			return run, fmt.Errorf("resume: level %d failed with %d error(s)", levelIdx, len(levelErrors))
		}
	}

	run.Status = storage.RunSuccess
	run.EndTime = sql.NullTime{Time: time.Now(), Valid: true}
	run.DurationMs = sql.NullInt64{Int64: run.EndTime.Time.Sub(run.StartTime).Milliseconds(), Valid: true}
	updateRunStatus(e, run)
	if updated, err := e.GetStore().GetRun(run.ID); err == nil {
		run = updated
	}

	logger.Info("run resumed and completed",
		"run_id", run.ID, "workflow", run.WorkflowName,
		"status", "success", "duration_ms", run.DurationMs.Int64,
		"tasks_total", run.TotalTasks, "tasks_success", run.TasksSuccess,
		"tasks_failed", run.TasksFailed, "tasks_skipped", run.TasksSkipped)
	return run, nil
}
