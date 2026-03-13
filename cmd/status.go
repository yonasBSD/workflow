package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/silocorp/workflow/internal/config"
	"github.com/silocorp/workflow/internal/storage"
	"github.com/silocorp/workflow/internal/tty"
	"github.com/spf13/cobra"
)

var (
	statusJSON     bool
	statusInterval int
)

var statusCmd = &cobra.Command{
	Use:   "status <run-id>",
	Short: "Live status of a workflow run",
	Long:  "Poll the database at a configurable interval and display the live state of each task. Exits automatically when the run completes.",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		runID := args[0]

		store, err := storage.New(config.Get().Paths.Database)
		if err != nil {
			return fmt.Errorf("database error: %w", err)
		}
		defer store.Close()

		// Verify the run exists before starting the polling loop.
		if _, err := store.GetRun(runID); err != nil {
			return fmt.Errorf("run not found: %w", err)
		}

		ticker := time.NewTicker(time.Duration(statusInterval) * time.Second)
		defer ticker.Stop()

		// Render once immediately, then on each tick.
		renderStatus(store, runID, statusJSON)
		for range ticker.C {
			done := renderStatus(store, runID, statusJSON)
			if done {
				return nil
			}
		}
		return nil
	},
}

// renderStatus prints the current run status. It returns true when the run has
// reached a terminal state so the polling loop can exit.
func renderStatus(store *storage.Store, runID string, asJSON bool) bool {
	run, err := store.GetRun(runID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "status: fetch error: %v\n", err)
		return false
	}

	tasks, err := store.ListTaskExecutions(storage.TaskFilters{RunID: runID})
	if err != nil {
		fmt.Fprintf(os.Stderr, "status: fetch tasks error: %v\n", err)
		return false
	}

	if asJSON {
		out := map[string]interface{}{"run": run, "tasks": tasks}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		enc.Encode(out) //nolint:errcheck
	} else {
		// Clear screen only on a real terminal.
		if tty.IsColourEnabled() {
			fmt.Print("\033[2J\033[H")
		}
		fmt.Printf("=== LIVE STATUS  run=%s ===\n", runID)
		fmt.Printf("Workflow: %-30s  Status: %s\n", run.WorkflowName, coloriseStatus(&run.Status))
		fmt.Printf("Tasks: %d total  %d✓  %d✗  %d⊘   Updated: %s\n\n",
			run.TotalTasks, run.TasksSuccess, run.TasksFailed, run.TasksSkipped,
			time.Now().Format("15:04:05"))

		fmt.Printf("%-34s %-10s %-10s %s\n", "TASK", "STATUS", "DURATION", "ERROR")
		fmt.Printf("%-34s %-10s %-10s %s\n", "----", "------", "--------", "-----")

		// Track unique task IDs to show the latest attempt only
		seen := make(map[string]bool)
		for i := len(tasks) - 1; i >= 0; i-- {
			t := tasks[i]
			if seen[t.TaskID] {
				continue
			}
			seen[t.TaskID] = true

			errMsg := ""
			if t.ErrorMessage.Valid {
				errMsg = truncate(t.ErrorMessage.String, 40)
			}
			fmt.Printf("%-34s %-10s %-10s %s\n",
				truncate(t.TaskID, 33),
				coloriseTaskStatus(string(t.State)),
				formatDurationOrDash(&t.DurationMs),
				errMsg,
			)
		}
	}

	return isTerminal(run.Status)
}

func isTerminal(s storage.RunStatus) bool {
	return s == storage.RunSuccess || s == storage.RunFailed || s == storage.RunCancelled
}

func init() {
	rootCmd.AddCommand(statusCmd)
	statusCmd.Flags().BoolVarP(&statusJSON, "json", "j", false, "Output in JSON format (one object per poll)")
	statusCmd.Flags().IntVar(&statusInterval, "interval", 1, "Poll interval in seconds")
}
