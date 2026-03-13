package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/silocorp/workflow/internal/config"
	"github.com/silocorp/workflow/internal/storage"
	"github.com/spf13/cobra"
)

var inspectJSON bool

var inspectCmd = &cobra.Command{
	Use:   "inspect <run-id>",
	Short: "Show detailed information about a workflow run",
	Long:  "Display run metadata, per-task execution details, variables at failure, forensic logs, and DAG structure.",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		runID := args[0]

		store, err := storage.New(config.Get().Paths.Database)
		if err != nil {
			return fmt.Errorf("database error: %w", err)
		}
		defer store.Close()

		run, err := store.GetRun(runID)
		if err != nil {
			return fmt.Errorf("run not found: %w", err)
		}

		tasks, err := store.ListTaskExecutions(storage.TaskFilters{RunID: runID})
		if err != nil {
			return fmt.Errorf("failed to fetch tasks: %w", err)
		}

		forensicLogs, err := store.ListForensicLogs(storage.ForensicLogFilters{RunID: runID})
		if err != nil {
			return fmt.Errorf("failed to fetch forensic logs: %w", err)
		}

		// Collect final variable snapshot (most recent per variable)
		snapshots, _ := store.ListContextSnapshots(storage.ContextSnapshotFilters{RunID: runID})
		vars := deduplicateSnapshots(snapshots)

		dagCache, _ := store.GetDAGCache(runID)

		if inspectJSON {
			return printInspectJSON(run, tasks, forensicLogs, vars, dagCache)
		}

		printInspectText(run, tasks, forensicLogs, vars, dagCache)
		return nil
	},
}

func deduplicateSnapshots(snapshots []*storage.ContextSnapshot) []*storage.ContextSnapshot {
	seen := make(map[string]bool)
	var result []*storage.ContextSnapshot
	for _, s := range snapshots {
		if !seen[s.VariableName] {
			seen[s.VariableName] = true
			result = append(result, s)
		}
	}
	return result
}

func printInspectText(run *storage.Run, tasks []*storage.TaskExecution, forensicLogs []*storage.ForensicLog, vars []*storage.ContextSnapshot, dagCache *storage.DAGCache) {
	fmt.Println("=== RUN METADATA ===")
	fmt.Printf("Run ID:    %s\n", run.ID)
	fmt.Printf("Workflow:  %s\n", run.WorkflowName)
	fmt.Printf("Status:    %s\n", coloriseStatus(&run.Status))
	fmt.Printf("Mode:      %s\n", run.ExecutionMode)
	fmt.Printf("Started:   %s\n", run.StartTime.Format("2006-01-02 15:04:05"))
	if run.EndTime.Valid {
		fmt.Printf("Ended:     %s\n", run.EndTime.Time.Format("2006-01-02 15:04:05"))
		fmt.Printf("Duration:  %s\n", formatDurationOrDash(&run.DurationMs))
	}
	fmt.Printf("Tasks:     %d total, %d success, %d failed, %d skipped\n",
		run.TotalTasks, run.TasksSuccess, run.TasksFailed, run.TasksSkipped)
	if run.ResumeCount > 0 {
		fmt.Printf("Resumes:   %d\n", run.ResumeCount)
	}

	fmt.Println("\n=== TASKS ===")
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "TASK\tSTATUS\tDURATION\tATTEMPT\tEXIT CODE\tERROR")
	fmt.Fprintln(w, "----\t------\t--------\t-------\t---------\t-----")
	for _, t := range tasks {
		errMsg := ""
		if t.ErrorMessage.Valid {
			errMsg = truncate(t.ErrorMessage.String, 60)
		}
		exitCode := "-"
		if t.ExitCode.Valid {
			exitCode = fmt.Sprintf("%d", t.ExitCode.Int64)
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%d/%d\t%s\t%s\n",
			truncate(t.TaskID, 32),
			coloriseTaskStatus(string(t.State)),
			formatDurationOrDash(&t.DurationMs),
			t.Attempt, t.MaxRetries+1,
			exitCode,
			errMsg,
		)
	}
	w.Flush()

	if len(vars) > 0 {
		fmt.Println("\n=== CONTEXT VARIABLES ===")
		w2 := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w2, "NAME\tTYPE\tVALUE\tSET BY")
		fmt.Fprintln(w2, "----\t----\t-----\t------")
		for _, v := range vars {
			fmt.Fprintf(w2, "%s\t%s\t%s\t%s\n",
				v.VariableName, v.VariableType,
				truncate(v.VariableValue, 48), v.SetByTask)
		}
		w2.Flush()
	}

	if len(forensicLogs) > 0 {
		fmt.Println("\n=== FORENSIC LOGS ===")
		for _, fl := range forensicLogs {
			taskID := "<global>"
			if fl.TaskID.Valid {
				taskID = fl.TaskID.String
			}
			fmt.Printf("[%s] task=%s type=%s\n%s\n\n",
				fl.CreatedAt.Format("15:04:05"), taskID, fl.LogType, fl.LogData)
		}
	}

	if dagCache != nil {
		fmt.Println("=== DAG STRUCTURE ===")
		fmt.Printf("Nodes: %d  Levels: %d  HasParallel: %v\n",
			dagCache.TotalNodes, dagCache.TotalLevels, dagCache.HasParallel)
	}
}

func printInspectJSON(run *storage.Run, tasks []*storage.TaskExecution, forensicLogs []*storage.ForensicLog, vars []*storage.ContextSnapshot, dagCache *storage.DAGCache) error {
	out := map[string]interface{}{
		"run":           run,
		"tasks":         tasks,
		"forensic_logs": forensicLogs,
		"variables":     vars,
		"dag_cache":     dagCache,
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}

func init() {
	rootCmd.AddCommand(inspectCmd)
	inspectCmd.Flags().BoolVarP(&inspectJSON, "json", "j", false, "Output in JSON format")
}
