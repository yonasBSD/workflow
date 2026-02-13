package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/joelfokou/workflow/internal/config"
	"github.com/joelfokou/workflow/internal/dag"
	"github.com/joelfokou/workflow/internal/logger"
	"github.com/joelfokou/workflow/internal/run"
	"github.com/spf13/cobra"
)

// workflowInfo holds metadata about a workflow.
type workflowInfo struct {
	Name         string `json:"name"`
	Tasks        int    `json:"tasks"`
	Valid        bool   `json:"valid"`
	LastRun      string `json:"last_run,omitempty"`
	TotalRuns    int    `json:"total_runs,omitempty"`
	SuccessCount int    `json:"success_count,omitempty"`
	FailedCount  int    `json:"failed_count,omitempty"`
}

// runStats holds statistics about workflow runs.
type runStats struct {
	LastRun      string
	TotalRuns    int
	SuccessCount int
	FailedCount  int
}

var (
	listJSON     bool
	listDetailed bool
)

// listCmd lists all available workflows with metadata including recent run statistics.
var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List workflows",
	Long:  "List all available workflows with optional run statistics",
	RunE: func(cmd *cobra.Command, args []string) error {
		entries, err := os.ReadDir(config.Get().Paths.Workflows)
		if err != nil {
			logger.Error("list command failed", "error", err)
			return fmt.Errorf("failed to read workflows directory: %w", err)
		}

		var workflows []*workflowInfo
		for _, entry := range entries {
			if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".toml") {
				name := strings.TrimSuffix(entry.Name(), ".toml")
				info := &workflowInfo{Name: name}

				// Load workflow definition to get task count
				d, err := dag.Load(name)
				if err != nil {
					logger.Warn("failed to load workflow definition", "workflow", name, "error", err)
					info.Tasks = 0
					info.Valid = false
				} else {
					info.Tasks = len(d.Tasks)
					info.Valid = true
				}

				// Get recent run statistics if detailed output requested
				if listDetailed {
					if stats, err := getRunStats(name); err == nil {
						info.LastRun = stats.LastRun
						info.TotalRuns = stats.TotalRuns
						info.SuccessCount = stats.SuccessCount
						info.FailedCount = stats.FailedCount
					}
				}

				workflows = append(workflows, info)
			}
		}

		if len(workflows) == 0 {
			logger.Debug("no workflows found", "directory", config.Get().Paths.Workflows)
			fmt.Printf("No workflows found in %s\n", config.Get().Paths.Workflows)
			return nil
		}

		// Sort workflows by name
		sort.Slice(workflows, func(i, j int) bool {
			return workflows[i].Name < workflows[j].Name
		})

		logger.Info("listing available workflows",
			"directory", config.Get().Paths.Workflows,
			"count", len(workflows),
		)

		if listJSON {
			return printWorkflowsJSON(workflows)
		}

		if listDetailed {
			return printWorkflowsDetailedTable(workflows)
		}

		return printWorkflowsTable(workflows)
	},
}

// getRunStats queries the database for workflow run statistics.
func getRunStats(workflowName string) (*runStats, error) {
	dbPath := config.Get().Paths.Database
	store, err := run.NewStore(dbPath)
	if err != nil {
		return nil, err
	}
	defer store.Close()

	runs, err := store.ListRuns(workflowName, "", 1000, 0)
	if err != nil {
		return nil, err
	}

	stats := &runStats{
		TotalRuns: len(runs),
	}

	if len(runs) > 0 {
		stats.LastRun = runs[0].CreatedAt.Format("2006-01-02 15:04:05")

		for _, r := range runs {
			switch r.Status {
			case run.StatusSuccess:
				stats.SuccessCount++
			case run.StatusFailed:
				stats.FailedCount++
			}
		}
	}

	return stats, nil
}

// printWorkflowsTable displays workflows in simple table format.
func printWorkflowsTable(workflows []*workflowInfo) error {
	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintf(w, "WORKFLOW\tTASKS\tSTATUS\n")
	fmt.Fprintf(w, "--------\t-----\t------\n")

	for _, wf := range workflows {
		status := "✓ valid"
		if !wf.Valid {
			status = "✗ invalid"
		}
		fmt.Fprintf(w, "%s\t%d\t%s\n", wf.Name, wf.Tasks, status)
	}

	w.Flush()
	return nil
}

// printWorkflowsDetailedTable displays workflows with run statistics.
func printWorkflowsDetailedTable(workflows []*workflowInfo) error {
	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintf(w, "WORKFLOW\tTASKS\tTOTAL RUNS\tSUCCESS\tFAILED\tLAST RUN\n")
	fmt.Fprintf(w, "--------\t-----\t----------\t-------\t------\t--------\n")

	for _, wf := range workflows {
		lastRun := "-"
		if wf.LastRun != "" {
			lastRun = wf.LastRun
		}

		fmt.Fprintf(w, "%s\t%d\t%d\t%d\t%d\t%s\n",
			wf.Name,
			wf.Tasks,
			wf.TotalRuns,
			wf.SuccessCount,
			wf.FailedCount,
			lastRun,
		)
	}

	w.Flush()
	return nil
}

// printWorkflowsJSON outputs workflows in JSON format.
func printWorkflowsJSON(workflows []*workflowInfo) error {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(workflows)
}

func init() {
	rootCmd.AddCommand(listCmd)

	listCmd.Flags().BoolVar(&listJSON, "json", false, "Output in JSON format")
	listCmd.Flags().BoolVarP(&listDetailed, "detailed", "d", false, "Show detailed statistics including run history")
}
