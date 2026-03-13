package executor

import (
	"context"
	"time"

	"github.com/silocorp/workflow/internal/storage"
)

// ProgressEventKind identifies what happened in a ProgressEvent.
type ProgressEventKind string

const (
	ProgressRunStarted   ProgressEventKind = "run_started"
	ProgressTaskStarted  ProgressEventKind = "task_started"
	ProgressTaskDone     ProgressEventKind = "task_done"   // success or skip
	ProgressTaskFailed   ProgressEventKind = "task_failed" // after all retries
	ProgressTaskRetrying ProgressEventKind = "task_retrying"
	// ProgressTaskOutput carries captured stdout/stderr for a completed task.
	// Emitted only when --print-output is set on the run context.
	// Output is truncated to 64 KiB before being placed on the channel; the
	// full content is always available in the on-disk log file.
	ProgressTaskOutput ProgressEventKind = "task_output"
	ProgressRunDone    ProgressEventKind = "run_done"
)

// maxOutputEvent is the maximum number of bytes emitted on the progress channel
// for a single task's output. Full output is always written to the log file.
const maxOutputEvent = 64 * 1024

// ProgressEvent is emitted on the channel returned by WithProgress as execution
// proceeds. Consumers can render a live status view without touching the DB.
type ProgressEvent struct {
	Kind      ProgressEventKind
	RunID     string
	TaskID    string
	TaskName  string
	Status    storage.TaskStatus // populated for task events
	RunStatus storage.RunStatus  // populated for run events
	Attempt   int
	Error     error
	// Output is populated for ProgressTaskOutput events.
	Output    string
	Truncated bool // true when output exceeded maxOutputEvent
	At        time.Time
}

// cleanEnvKey is the context key for the clean-env flag.
type cleanEnvKey struct{}

// WithCleanEnv attaches the clean-env flag to ctx.
// When set, tasks that do not specify clean_env=true in TOML also start
// with an empty environment (only task-level env vars are inherited).
func WithCleanEnv(ctx context.Context) context.Context {
	return context.WithValue(ctx, cleanEnvKey{}, true)
}

// isCleanEnv reports whether the clean-env flag was set on ctx.
func isCleanEnv(ctx context.Context) bool {
	v, _ := ctx.Value(cleanEnvKey{}).(bool)
	return v
}

// printOutputKey is the context key for the print-output flag.
type printOutputKey struct{}

// WithPrintOutput attaches the print-output flag to ctx.
func WithPrintOutput(ctx context.Context) context.Context {
	return context.WithValue(ctx, printOutputKey{}, true)
}

// isPrintOutput reports whether the print-output flag was set on ctx.
func isPrintOutput(ctx context.Context) bool {
	v, _ := ctx.Value(printOutputKey{}).(bool)
	return v
}

// runtimeVarsKey is the context key for runtime variables injected via --var.
type runtimeVarsKey struct{}

// WithRuntimeVars attaches runtime key=value pairs to ctx.
// doResume extracts these and injects them into the ContextMap after snapshot
// restoration, allowing the caller to override or supplement variables.
func WithRuntimeVars(ctx context.Context, vars map[string]string) context.Context {
	return context.WithValue(ctx, runtimeVarsKey{}, vars)
}

// runtimeVars returns the runtime vars attached to ctx, or nil.
func runtimeVars(ctx context.Context) map[string]string {
	v, _ := ctx.Value(runtimeVarsKey{}).(map[string]string)
	return v
}

type progressKey struct{}

// WithProgress attaches a progress-event channel to ctx.
// The caller owns the channel and is responsible for draining it.
func WithProgress(ctx context.Context, ch chan<- ProgressEvent) context.Context {
	return context.WithValue(ctx, progressKey{}, ch)
}

// emitProgress sends an event if a channel was attached to ctx.
// It never blocks: if the channel buffer is full the event is dropped.
func emitProgress(ctx context.Context, evt ProgressEvent) {
	evt.At = time.Now()
	ch, ok := ctx.Value(progressKey{}).(chan<- ProgressEvent)
	if !ok || ch == nil {
		return
	}
	select {
	case ch <- evt:
	default:
	}
}
