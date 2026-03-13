package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/joelfokou/workflow/internal/config"
	"github.com/joelfokou/workflow/internal/contextmap"
	"github.com/joelfokou/workflow/internal/dag"
	"github.com/joelfokou/workflow/internal/executor"
	"github.com/joelfokou/workflow/internal/logger"
	"github.com/joelfokou/workflow/internal/storage"
	"github.com/joelfokou/workflow/internal/tty"
	"github.com/spf13/cobra"
)

var (
	dryRun          bool
	dryRunJSON      bool
	runParallel     bool
	runWorkStealing bool
	maxParallel     int
	runVars         []string
	runTimeout      time.Duration // per-run timeout (0 = no limit)
	runPrintOutput  bool          // print task output atomically on completion
	runCleanEnv     bool          // do not inherit parent process environment
)

// runCmd executes a specified workflow by loading its definition, setting up a context with cancellation support, handling interrupts (Ctrl+C), and then running the workflow using an executor.
var runCmd = &cobra.Command{
	Use:   "run <workflow>",
	Short: "Run a workflow",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		workflowName := args[0]

		// Parse workflow
		parser := dag.NewParser(workflowName)
		definition, err := parser.Parse()
		if err != nil {
			return fmt.Errorf("failed to parse workflow: %w", err)
		}

		// Build DAG
		builder := dag.NewBuilder(definition)
		dag, err := builder.Build()
		if err != nil {
			logger.Error("DAG construction failed", "workflow", workflowName, "error", err)
			return fmt.Errorf("DAG construction failed: %w", err)
		}

		logger.Debug("DAG ready",
			"workflow", workflowName,
			"tasks", dag.TotalTasks, "levels", len(dag.Levels))

		// Dry run mode - just show the plan
		if dryRun {
			if dryRunJSON {
				if err := printExecutionPlanJSON(dag); err != nil {
					return fmt.Errorf("failed to print execution plan in JSON: %w", err)
				}
			} else {
				printExecutionPlan(dag)
			}
			fmt.Println("\nNo tasks were executed.")
			return nil
		}

		// Initialise database
		store, err := storage.New(config.Get().Paths.Database)
		if err != nil {
			return fmt.Errorf("database error: %w", err)
		}
		defer store.Close()

		// Create executor
		var exec executor.Executor
		switch {
		case runWorkStealing:
			logger.Debug("using work-stealing executor", "workflow", workflowName, "workers", maxParallel)
			exec = executor.NewWorkStealingExecutor(store, maxParallel)
		case runParallel:
			logger.Debug("using parallel executor", "workflow", workflowName, "max_parallel", maxParallel)
			exec = executor.NewParallelExecutor(store, maxParallel)
		default:
			logger.Debug("using sequential executor", "workflow", workflowName)
			exec = executor.NewSequentialExecutor(store)
		}

		// Execute workflow with signal-aware context
		var ctx context.Context
		var cancel context.CancelFunc
		if runTimeout > 0 {
			ctx, cancel = context.WithTimeout(context.Background(), runTimeout)
		} else {
			ctx, cancel = context.WithCancel(context.Background())
		}
		defer cancel()

		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		go func() {
			sig := <-sigCh
			logger.Warn("received signal, cancelling workflow", "signal", sig)
			cancel()
		}()

		if runCleanEnv {
			ctx = executor.WithCleanEnv(ctx)
		}
		if runPrintOutput {
			ctx = executor.WithPrintOutput(ctx)
		}

		ctxMap := contextmap.NewContextMap()
		for _, kv := range runVars {
			parts := strings.SplitN(kv, "=", 2)
			if len(parts) != 2 {
				return fmt.Errorf("invalid --var format %q (expected KEY=VALUE)", kv)
			}
			if err := ctxMap.Set("__runtime__", parts[0], parts[1]); err != nil {
				return fmt.Errorf("failed to set runtime variable %q: %w", parts[0], err)
			}
		}

		// Attach a progress channel so we can render live task updates.
		progCh := make(chan executor.ProgressEvent, 64)
		ctx = executor.WithProgress(ctx, progCh)

		// Consume events in a background goroutine.
		// taskStarted tracks when each task began so we can display elapsed time.
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

		result, err := exec.Execute(ctx, dag, ctxMap)

		// Signal the renderer and wait for it to drain.
		close(progCh)
		<-progressDone

		// Report results
		printExecutionResult(result, err)

		return err
	},
}

func init() {
	rootCmd.AddCommand(runCmd)

	runCmd.Flags().BoolVar(&dryRun, "dry-run", false, "Print execution plan without running tasks")
	runCmd.Flags().BoolVarP(&dryRunJSON, "json", "j", false, "Output the execution plan in JSON format (requires --dry-run)")
	runCmd.Flags().BoolVar(&runParallel, "parallel", false, "Enable parallel execution of independent tasks (level-based)")
	runCmd.Flags().BoolVar(&runWorkStealing, "work-stealing", false, "Enable work-stealing scheduler: tasks start as soon as their dependencies finish (maximum throughput)")
	runCmd.Flags().IntVar(&maxParallel, "max-parallel", 4, "Maximum number of concurrent tasks (applies to --parallel and --work-stealing)")
	runCmd.Flags().StringArrayVar(&runVars, "var", nil, "Set a runtime variable (KEY=VALUE); may be repeated")
	runCmd.Flags().DurationVar(&runTimeout, "timeout", 0, "Maximum run duration (e.g. 30m, 2h); 0 = no limit")
	runCmd.Flags().BoolVar(&runPrintOutput, "print-output", false, "Print each task's stdout/stderr atomically after it completes")
	runCmd.Flags().BoolVar(&runCleanEnv, "clean-env", false, "Start tasks with an empty environment (do not inherit parent process env)")
}

func renderProgressEvent(evt executor.ProgressEvent, taskStarted map[string]time.Time) {
	const nameWidth = 24
	switch evt.Kind {
	case executor.ProgressRunStarted:
		fmt.Printf("  %s  run %s\n",
			tty.Colourise("▶ started", "94"),
			evt.RunID[:8],
		)
	case executor.ProgressTaskStarted:
		fmt.Printf("  %s  %-*s  %s\n",
			tty.Colourise("→", "94"),
			nameWidth, evt.TaskName,
			evt.At.Format("15:04:05"),
		)
	case executor.ProgressTaskRetrying:
		fmt.Printf("  %s  %-*s  attempt %d\n",
			tty.Colourise("↻ retrying", "93"),
			nameWidth, evt.TaskName,
			evt.Attempt,
		)
	case executor.ProgressTaskDone:
		elapsed := ""
		if start, ok := taskStarted[evt.TaskID]; ok {
			elapsed = "  " + formatDuration(evt.At.Sub(start))
		}
		fmt.Printf("  %s  %-*s%s\n",
			tty.Colourise("✓", "92"),
			nameWidth, evt.TaskName,
			elapsed,
		)
	case executor.ProgressTaskFailed:
		msg := ""
		if evt.Error != nil {
			msg = "  " + evt.Error.Error()
		}
		fmt.Printf("  %s  %-*s%s\n",
			tty.Colourise("✗ failed", "91"),
			nameWidth, evt.TaskName,
			msg,
		)
	case executor.ProgressTaskOutput:
		header := "output"
		if evt.Truncated {
			header = "output (truncated to 64 KiB)"
		}
		fmt.Printf("  %-*s %s:\n%s\n", nameWidth, evt.TaskName, header, evt.Output)
	case executor.ProgressRunDone:
		code := "92"
		switch evt.RunStatus {
		case storage.RunFailed:
			code = "91"
		case storage.RunCancelled:
			code = "93"
		}
		fmt.Printf("  %s  run %s  %s\n",
			tty.Colourise("■ done", code),
			evt.RunID[:8],
			coloriseStatus(&evt.RunStatus),
		)
	}
}

func printExecutionPlan(dag *dag.DAG) {
	fmt.Println("\n=== EXECUTION PLAN ===")
	fmt.Printf("Workflow: %s\n", dag.Name)
	fmt.Printf("Total Tasks: %d\n", dag.TotalTasks)
	fmt.Printf("Execution Levels: %d\n\n", len(dag.Levels))
	for i, level := range dag.Levels {
		fmt.Printf("Level %d (%d tasks):\n", i, len(level))
		for _, node := range level {
			parallel := ""
			if node.CanRunInParallel {
				parallel = " [PARALLEL]"
			}
			fmt.Printf("  - %s%s\n", node.ID, parallel)
			if node.TaskDef.If != "" {
				fmt.Printf("    Condition: %s\n", node.TaskDef.If)
			}
		}
		fmt.Println()
	}
}

func printExecutionPlanJSON(dag *dag.DAG) error {
	type TaskInfo struct {
		ID               string   `json:"id"`
		CanRunInParallel bool     `json:"can_run_in_parallel"`
		DependsOn        []string `json:"depends_on"`
		Condition        string   `json:"condition,omitempty"`
	}

	type LevelInfo struct {
		Level int        `json:"level"`
		Tasks []TaskInfo `json:"tasks"`
	}

	type PlanInfo struct {
		WorkflowName string      `json:"workflow_name"`
		TotalTasks   int         `json:"total_tasks"`
		Levels       []LevelInfo `json:"levels"`
	}

	var levels []LevelInfo
	for i, level := range dag.Levels {
		var tasks []TaskInfo
		for _, node := range level {
			dependencyIDs := make([]string, 0, len(node.Dependencies))
			for _, dep := range node.Dependencies {
				dependencyIDs = append(dependencyIDs, dep.ID)
			}
			tasks = append(tasks, TaskInfo{
				ID:               node.ID,
				CanRunInParallel: node.CanRunInParallel,
				DependsOn:        dependencyIDs,
				Condition:        node.TaskDef.If,
			})
		}
		levels = append(levels, LevelInfo{
			Level: i,
			Tasks: tasks,
		})
	}

	plan := PlanInfo{
		WorkflowName: dag.Name,
		TotalTasks:   dag.TotalTasks,
		Levels:       levels,
	}

	data, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal execution plan to JSON: %w", err)
	}

	fmt.Println(string(data))
	return nil
}

func printExecutionResult(run *storage.Run, err error) {
	fmt.Println("\n=== EXECUTION RESULT ===")
	fmt.Printf("Run ID:   %s\n", run.ID)
	fmt.Printf("Status:   %s\n", coloriseStatus(&run.Status))
	fmt.Printf("Duration: %s\n", formatDurationOrDash(&run.DurationMs))
	fmt.Printf("Tasks:    %d total  %d✓  %d✗  %d⊘\n",
		run.TotalTasks, run.TasksSuccess, run.TasksFailed, run.TasksSkipped)
	if err != nil {
		fmt.Printf("\n%s %v\n", tty.Colourise("Error:", "91"), err)
	}
}
