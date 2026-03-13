package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/joelfokou/workflow/internal/config"
	"github.com/joelfokou/workflow/internal/executor"
	"github.com/joelfokou/workflow/internal/logger"
	"github.com/joelfokou/workflow/internal/storage"
	"github.com/spf13/cobra"
)

var (
	resumeParallel     bool
	resumeWorkStealing bool
	resumeMaxParallel  int
	resumeVars         []string
	resumePrintOutput  bool
)

var resumeCmd = &cobra.Command{
	Use:   "resume <run_id>",
	Short: "Resume a failed workflow run",
	Long:  "Resume a failed workflow run from the point of failure",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		runID := args[0]

		store, err := storage.New(config.Get().Paths.Database)
		if err != nil {
			return fmt.Errorf("database error: %w", err)
		}
		defer store.Close()

		// Load and validate the run
		run, err := store.GetRun(runID)
		if err != nil {
			return fmt.Errorf("run not found: %w", err)
		}
		if run.Status != storage.RunFailed && run.Status != storage.RunCancelled {
			return fmt.Errorf("run %s is not resumable (status: %s)", runID, run.Status)
		}

		logger.Info("resuming workflow run", "run_id", runID, "workflow", run.WorkflowName, "status", run.Status)

		// Resolve execution mode: CLI flags override, otherwise use original.
		exec := resolveResumeExecutor(store, run)

		// Signal-aware context
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		go func() {
			sig := <-sigCh
			logger.Warn("received signal, cancelling resume", "signal", sig)
			cancel()
		}()

		if resumePrintOutput {
			ctx = executor.WithPrintOutput(ctx)
		}

		// Inject runtime variables via context for doResume to pick up.
		if len(resumeVars) > 0 {
			vars := make(map[string]string, len(resumeVars))
			for _, kv := range resumeVars {
				parts := strings.SplitN(kv, "=", 2)
				if len(parts) != 2 {
					return fmt.Errorf("invalid --var format %q (expected KEY=VALUE)", kv)
				}
				vars[parts[0]] = parts[1]
			}
			ctx = executor.WithRuntimeVars(ctx, vars)
		}

		// Attach a progress channel so we can render live task updates.
		progCh := make(chan executor.ProgressEvent, 64)
		ctx = executor.WithProgress(ctx, progCh)

		progressDone := make(chan struct{})
		go func() {
			defer close(progressDone)
			taskStarted := make(map[string]time.Time)
			for evt := range progCh {
				if evt.Kind == executor.ProgressTaskStarted {
					taskStarted[evt.TaskID] = evt.At
				}
				renderProgressEvent(evt, taskStarted)
			}
		}()

		result, err := exec.Resume(ctx, runID)

		close(progCh)
		<-progressDone

		printExecutionResult(result, err)

		if err != nil {
			return fmt.Errorf("resume failed: %w", err)
		}
		return nil
	},
}

// resolveResumeExecutor selects the executor for a resume operation.
// CLI flags (--parallel, --work-stealing) take precedence; otherwise the
// original run's execution mode is preserved.
func resolveResumeExecutor(store *storage.Store, run *storage.Run) executor.Executor {
	// Determine effective max-parallel. CLI flag overrides; otherwise fall
	// back to the value recorded in the original run.
	effectiveMax := run.MaxParallel
	if resumeMaxParallel > 0 {
		effectiveMax = resumeMaxParallel
	}
	if effectiveMax < 1 {
		effectiveMax = 4
	}

	// CLI flags override the stored execution mode.
	switch {
	case resumeWorkStealing:
		logger.Debug("resume: using work-stealing executor (CLI override)", "workers", effectiveMax)
		return executor.NewWorkStealingExecutor(store, effectiveMax)
	case resumeParallel:
		logger.Debug("resume: using parallel executor (CLI override)", "max_parallel", effectiveMax)
		return executor.NewParallelExecutor(store, effectiveMax)
	}

	// No CLI override — use the original run's mode.
	switch run.ExecutionMode {
	case storage.ExecutionWorkStealing:
		logger.Debug("resume: using work-stealing executor (original mode)", "workers", effectiveMax)
		return executor.NewWorkStealingExecutor(store, effectiveMax)
	case storage.ExecutionParallel:
		logger.Debug("resume: using parallel executor (original mode)", "max_parallel", effectiveMax)
		return executor.NewParallelExecutor(store, effectiveMax)
	default:
		logger.Debug("resume: using sequential executor (original mode)")
		return executor.NewSequentialExecutor(store)
	}
}

func init() {
	rootCmd.AddCommand(resumeCmd)

	resumeCmd.Flags().BoolVar(&resumeParallel, "parallel", false, "Resume with level-based parallel execution")
	resumeCmd.Flags().BoolVar(&resumeWorkStealing, "work-stealing", false, "Resume with work-stealing scheduler")
	resumeCmd.Flags().IntVar(&resumeMaxParallel, "max-parallel", 0, "Maximum concurrent tasks during resume (0 = use original run's value, default 4)")
	resumeCmd.Flags().StringArrayVar(&resumeVars, "var", nil, "Inject a runtime variable (KEY=VALUE); may be repeated")
	resumeCmd.Flags().BoolVar(&resumePrintOutput, "print-output", false, "Print each task's stdout/stderr atomically after it completes")
}
