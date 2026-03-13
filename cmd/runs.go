package cmd

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/silocorp/workflow/internal/config"
	"github.com/silocorp/workflow/internal/logger"
	"github.com/silocorp/workflow/internal/storage"
	"github.com/silocorp/workflow/internal/tty"
	"github.com/spf13/cobra"
)

var (
	runsWorkflow    string
	runsStatus      string
	runsTag         string
	runsLimit       int
	runsOffset      int
	runsJSON        bool
	runsDetailed    bool
	runsStats       bool
	runsTimeline    bool
	runsSort        string
	runsStartDate   string
	runsEndDate     string
	runsMinDuration string
	runsShowTasks   bool
	runsShowLogs    bool
)

// RunsFilterOptions holds all filtering and display options
type RunsFilterOptions struct {
	WorkflowName string
	Status       string
	Limit        int
	Offset       int
	StartDate    time.Time
	EndDate      time.Time
	MinDuration  time.Duration
	SortBy       string
}

// RunsSummary holds aggregated statistics about runs
type RunsSummary struct {
	TotalRuns      int
	SuccessfulRuns int
	FailedRuns     int
	RunningRuns    int
	CancelledRuns  int
	ResumingRuns   int
	AvgDuration    time.Duration
	TotalDuration  time.Duration
	SuccessRate    float64
	AverageTasks   float64
	AverageSuccess float64
	AverageFailed  float64
	AverageSkipped float64
}

// runsCmd lists workflow runs with comprehensive filtering and analytics.
var runsCmd = &cobra.Command{
	Use:   "runs",
	Short: "List and analyse workflow runs",
	Long: `List all workflow runs with advanced filtering, sorting, and analytics.
Supports filtering by workflow, status, date range, and duration.
Can display detailed metrics, task summaries, timeline visualisation, and audit trails.
Perfect for monitoring, debugging, and performance analysis.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		// Parse filter options
		options, err := parseRunsOptions()
		if err != nil {
			return fmt.Errorf("invalid filter options: %w", err)
		}

		dbPath := config.Get().Paths.Database
		st, err := storage.New(dbPath)
		if err != nil {
			logger.Error("failed to open store", "error", err)
			return fmt.Errorf("failed to open store: %w", err)
		}
		defer st.Close()

		// Retrieve runs
		runs, err := st.ListRuns(storage.RunFilters{
			WorkflowName: options.WorkflowName,
			Status:       storage.RunStatus(options.Status),
			Tag:          runsTag,
			StartAfter:   options.StartDate,
			Limit:        options.Limit,
			Offset:       options.Offset,
		})
		if err != nil {
			logger.Error("failed to retrieve runs", "error", err)
			return fmt.Errorf("failed to retrieve runs: %w", err)
		}

		if len(runs) == 0 {
			fmt.Println("No runs found matching the criteria")
			return nil
		}

		// Filter by duration if specified
		if options.MinDuration > 0 {
			runs = filterByMinDuration(runs, options.MinDuration)
			if len(runs) == 0 {
				fmt.Println("No runs found matching the duration criteria")
				return nil
			}
		}

		// Sort runs
		sortRuns(runs, options.SortBy)

		// Calculate summary statistics
		summary := calculateRunsSummary(runs)

		// Display results
		if runsJSON {
			return displayRunsJSON(runs, summary)
		}

		if runsTimeline {
			displayRunsTimeline(runs)
			return nil
		}

		displayRunsHeader(summary)

		// --stats: summary only, no table
		if runsStats {
			return nil
		}

		if runsDetailed {
			displayRunsDetailed(st, runs)
			return nil
		}

		if runsShowTasks {
			displayRunsWithTasks(st, runs)
			return nil
		}

		if runsShowLogs {
			displayRunsWithLogs(st, runs)
			return nil
		}

		displayRunsTable(runs)
		return nil
	},
}

// parseRunsOptions parses command-line options into filter options
func parseRunsOptions() (RunsFilterOptions, error) {
	options := RunsFilterOptions{
		WorkflowName: runsWorkflow,
		Status:       runsStatus,
		Limit:        runsLimit,
		Offset:       runsOffset,
		SortBy:       runsSort,
	}

	// Parse start date
	if runsStartDate != "" {
		startTime, err := time.Parse("2006-01-02", runsStartDate)
		if err != nil {
			return options, fmt.Errorf("invalid start-date format (use YYYY-MM-DD): %w", err)
		}
		options.StartDate = startTime
	} else {
		// Default to 30 days ago
		options.StartDate = time.Now().AddDate(0, 0, -30)
	}

	// Parse end date
	if runsEndDate != "" {
		endTime, err := time.Parse("2006-01-02", runsEndDate)
		if err != nil {
			return options, fmt.Errorf("invalid end-date format (use YYYY-MM-DD): %w", err)
		}
		options.EndDate = endTime
	} else {
		options.EndDate = time.Now()
	}

	// Parse minimum duration
	if runsMinDuration != "" {
		duration, err := time.ParseDuration(runsMinDuration)
		if err != nil {
			return options, fmt.Errorf("invalid min-duration format: %w", err)
		}
		options.MinDuration = duration
	}

	// Validate status
	if runsStatus != "" {
		validStatuses := map[string]bool{
			"pending":   true,
			"running":   true,
			"success":   true,
			"failed":    true,
			"cancelled": true,
			"resuming":  true,
		}
		if !validStatuses[strings.ToLower(runsStatus)] {
			return options, fmt.Errorf("invalid status: %s", runsStatus)
		}
	}

	return options, nil
}

// filterByMinDuration filters runs by minimum duration
func filterByMinDuration(runs []*storage.Run, minDuration time.Duration) []*storage.Run {
	var filtered []*storage.Run
	for _, r := range runs {
		if r.DurationMs.Valid {
			duration := time.Duration(r.DurationMs.Int64) * time.Millisecond
			if duration >= minDuration {
				filtered = append(filtered, r)
			}
		}
	}
	return filtered
}

// sortRuns sorts runs according to the specified field
func sortRuns(runs []*storage.Run, sortBy string) {
	switch sortBy {
	case "name":
		sortByWorkflowName(runs)
	case "duration":
		sortByDuration(runs)
	case "tasks":
		sortByTaskCount(runs)
	case "status":
		sortByStatus(runs)
	case "time":
		fallthrough
	default:
		sortByTime(runs)
	}
}

func sortByTime(runs []*storage.Run) {
	sort.Slice(runs, func(i, j int) bool {
		return runs[i].StartTime.After(runs[j].StartTime)
	})
}

func sortByWorkflowName(runs []*storage.Run) {
	sort.Slice(runs, func(i, j int) bool {
		return runs[i].WorkflowName < runs[j].WorkflowName
	})
}

func sortByDuration(runs []*storage.Run) {
	sort.Slice(runs, func(i, j int) bool {
		durI, durJ := int64(0), int64(0)
		if runs[i].DurationMs.Valid {
			durI = runs[i].DurationMs.Int64
		}
		if runs[j].DurationMs.Valid {
			durJ = runs[j].DurationMs.Int64
		}
		return durI > durJ
	})
}

func sortByTaskCount(runs []*storage.Run) {
	sort.Slice(runs, func(i, j int) bool {
		return runs[i].TotalTasks > runs[j].TotalTasks
	})
}

func sortByStatus(runs []*storage.Run) {
	priority := map[storage.RunStatus]int{
		storage.RunRunning:   0,
		storage.RunResuming:  1,
		storage.RunPending:   2,
		storage.RunSuccess:   3,
		storage.RunFailed:    4,
		storage.RunCancelled: 5,
	}
	sort.Slice(runs, func(i, j int) bool {
		return priority[runs[i].Status] < priority[runs[j].Status]
	})
}

// calculateRunsSummary calculates aggregate statistics about runs
func calculateRunsSummary(runs []*storage.Run) RunsSummary {
	summary := RunsSummary{
		TotalRuns: len(runs),
	}

	var totalDuration int64
	var totalTasks int
	var totalSuccess int
	var totalFailed int
	var totalSkipped int
	var validDurations int

	for _, r := range runs {
		switch r.Status {
		case storage.RunSuccess:
			summary.SuccessfulRuns++
		case storage.RunFailed:
			summary.FailedRuns++
		case storage.RunRunning:
			summary.RunningRuns++
		case storage.RunCancelled:
			summary.CancelledRuns++
		case storage.RunResuming:
			summary.ResumingRuns++
		}

		if r.DurationMs.Valid {
			totalDuration += r.DurationMs.Int64
			validDurations++
		}

		totalTasks += r.TotalTasks
		totalSuccess += r.TasksSuccess
		totalFailed += r.TasksFailed
		totalSkipped += r.TasksSkipped
	}

	if validDurations > 0 {
		summary.AvgDuration = time.Duration(totalDuration/int64(validDurations)) * time.Millisecond
		summary.TotalDuration = time.Duration(totalDuration) * time.Millisecond
	}

	if summary.TotalRuns > 0 {
		summary.SuccessRate = float64(summary.SuccessfulRuns) / float64(summary.TotalRuns) * 100
		summary.AverageTasks = float64(totalTasks) / float64(summary.TotalRuns)
		if totalTasks > 0 {
			summary.AverageSuccess = float64(totalSuccess) / float64(totalTasks) * 100
			summary.AverageFailed = float64(totalFailed) / float64(totalTasks) * 100
			summary.AverageSkipped = float64(totalSkipped) / float64(totalTasks) * 100
		}
	}

	return summary
}

// displayRunsHeader displays summary statistics header
func displayRunsHeader(summary RunsSummary) {
	const boxWidth = 64 // total width between the outer ║ characters
	bar := strings.Repeat("═", boxWidth)
	// cell pads content to exactly boxWidth-2 chars (the two ║ chars are outside)
	cell := func(s string) string {
		n := boxWidth - 2 - len(s)
		if n < 0 {
			return s[:boxWidth-2]
		}
		return s + strings.Repeat(" ", n)
	}
	fmt.Printf("\n╔%s╗\n", bar)
	fmt.Printf("║%s║\n", cell("  WORKFLOW RUNS SUMMARY"))
	fmt.Printf("╠%s╣\n", bar)
	fmt.Printf("║%s║\n", cell(fmt.Sprintf("  Total Runs:        %d", summary.TotalRuns)))
	fmt.Printf("║%s║\n", cell(fmt.Sprintf("  Success: %d   Failed: %d   Running: %d",
		summary.SuccessfulRuns, summary.FailedRuns, summary.RunningRuns)))
	fmt.Printf("║%s║\n", cell(fmt.Sprintf("  Success Rate:      %.1f%%", summary.SuccessRate)))
	fmt.Printf("║%s║\n", cell(fmt.Sprintf("  Avg Duration:      %v", summary.AvgDuration)))
	fmt.Printf("║%s║\n", cell(fmt.Sprintf("  Total Duration:    %v", summary.TotalDuration)))
	fmt.Printf("║%s║\n", cell(fmt.Sprintf("  Avg Tasks/Run:     %.1f", summary.AverageTasks)))
	fmt.Printf("║%s║\n", cell(fmt.Sprintf("  Task Success Rate: %.1f%%", summary.AverageSuccess)))
	fmt.Printf("╚%s╝\n\n", bar)
}

// displayRunsTable displays runs in a formatted table
func displayRunsTable(runs []*storage.Run) {
	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintf(w, "RUN ID\tWORKFLOW\tSTATUS\tSTARTED AT\tDURATION\tTASKS\tSUCCESS\tFAILED\tSKIPPED\n")
	fmt.Fprintf(w, "------\t--------\t------\t----------\t--------\t-----\t-------\t------\t-------\n")

	for _, r := range runs {
		duration := "-"
		if r.DurationMs.Valid {
			duration = formatDuration(time.Duration(r.DurationMs.Int64) * time.Millisecond)
		}

		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%d\t%d\t%d\t%d\n",
			truncate(r.ID, 8),
			r.WorkflowName,
			coloriseStatus(&r.Status),
			r.StartTime.Format("2006-01-02 15:04"),
			duration,
			r.TotalTasks,
			r.TasksSuccess,
			r.TasksFailed,
			r.TasksSkipped,
		)
	}

	w.Flush()
	logger.Info("displayed runs", "count", len(runs))
	fmt.Printf("\n")
}

// displayRunsDetailed displays detailed information for each run
func displayRunsDetailed(st *storage.Store, runs []*storage.Run) {
	for i, r := range runs {
		if i > 0 {
			fmt.Printf("\n")
		}

		fmt.Printf("┌─ Run #%d ─────────────────────────────────────────────────────┐\n", i+1)
		fmt.Printf("│ ID:                %s\n", r.ID)
		fmt.Printf("│ Workflow:          %s\n", r.WorkflowName)
		fmt.Printf("│ Status:            %s\n", coloriseStatus(&r.Status))
		fmt.Printf("│ Started:           %s\n", r.StartTime.Format("2006-01-02 15:04:05"))

		if r.EndTime.Valid {
			fmt.Printf("│ Ended:             %s\n", r.EndTime.Time.Format("2006-01-02 15:04:05"))
		}

		if r.DurationMs.Valid {
			fmt.Printf("│ Duration:          %s\n", formatDuration(time.Duration(r.DurationMs.Int64)*time.Millisecond))
		}

		fmt.Printf("│ Total Tasks:       %d\n", r.TotalTasks)
		fmt.Printf("│ Success:           %d  Failed: %d  Skipped: %d\n",
			r.TasksSuccess, r.TasksFailed, r.TasksSkipped)
		fmt.Printf("│ Execution Mode:    %s\n", r.ExecutionMode)
		fmt.Printf("│ Max Parallel:      %d\n", r.MaxParallel)
		if tags := r.RunTags(); len(tags) > 0 {
			fmt.Printf("│ Tags:              %s\n", strings.Join(tags, ", "))
		}

		if r.ResumeCount > 0 {
			fmt.Printf("│ Resume Count:      %d\n", r.ResumeCount)
			if r.LastResumeTime.Valid {
				fmt.Printf("│ Last Resume:       %s\n", r.LastResumeTime.Time.Format("2006-01-02 15:04:05"))
			}
		}

		// Get DAG cache info
		dagInfo, err := st.GetDAGCache(r.ID)
		if err == nil && dagInfo != nil {
			fmt.Printf("│ Total Nodes:       %d\n", dagInfo.TotalNodes)
			fmt.Printf("│ Total Levels:      %d\n", dagInfo.TotalLevels)
			fmt.Printf("│ Has Parallel:      %v\n", dagInfo.HasParallel)
		}

		fmt.Printf("└──────────────────────────────────────────────────────────────┘\n")
	}
}

// displayRunsWithTasks displays runs with their task summaries
func displayRunsWithTasks(st *storage.Store, runs []*storage.Run) {
	for _, r := range runs {
		fmt.Printf("\n╭─ %s (%s) ──────────────────────────────────╮\n", r.WorkflowName, r.ID[:8])
		fmt.Printf("│ Status: %s | Duration: %s\n",
			coloriseStatus(&r.Status),
			formatDurationOrDash(&r.DurationMs),
		)

		// Retrieve task executions
		tasks, err := st.ListTaskExecutions(storage.TaskFilters{RunID: r.ID})
		if err != nil {
			logger.Warn("failed to retrieve tasks for run", "run_id", r.ID, "error", err)
			fmt.Printf("│ (Unable to load task details)\n")
			fmt.Printf("╰───────────────────────────────────────────────╯\n")
			continue
		}

		if len(tasks) == 0 {
			fmt.Printf("│ (No tasks found)\n")
			fmt.Printf("╰───────────────────────────────────────────────╯\n")
			continue
		}

		w := tabwriter.NewWriter(os.Stdout, 0, 2, 1, ' ', 0)
		fmt.Fprintf(w, "│ # \tTASK NAME\tSTATUS\tATTEMPT\tDURATION\n")
		fmt.Fprintf(w, "│ ---\t---------\t------\t-------\t--------\n")

		for idx, task := range tasks {
			duration := "-"
			if task.DurationMs.Valid {
				duration = formatDuration(time.Duration(task.DurationMs.Int64) * time.Millisecond)
			}

			fmt.Fprintf(w, "│ %d\t%s\t%s\t%d\t%s\n",
				idx+1,
				task.TaskName,
				coloriseTaskStatus(string(task.State)),
				task.Attempt,
				duration,
			)
		}

		w.Flush()
		fmt.Printf("╰───────────────────────────────────────────────╯\n")
	}
}

// displayRunsWithLogs shows log file paths for failed tasks in each run.
func displayRunsWithLogs(st *storage.Store, runs []*storage.Run) {
	for _, r := range runs {
		fmt.Printf("\n%s  %s  %s\n", truncate(r.ID, 8), r.WorkflowName, coloriseStatus(&r.Status))

		tasks, err := st.ListTaskExecutions(storage.TaskFilters{RunID: r.ID, State: storage.TaskFailed})
		if err != nil {
			logger.Warn("failed to retrieve tasks", "run_id", r.ID, "error", err)
			continue
		}

		if len(tasks) == 0 {
			fmt.Printf("  (no failed tasks)\n")
			continue
		}

		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "  TASK\tATTEMPT\tEXIT CODE\tLOG PATH")
		fmt.Fprintln(w, "  ----\t-------\t---------\t--------")
		for _, t := range tasks {
			logPath := "-"
			if t.LogPath.Valid {
				logPath = t.LogPath.String
			}
			exitCode := "-"
			if t.ExitCode.Valid {
				exitCode = fmt.Sprintf("%d", t.ExitCode.Int64)
			}
			fmt.Fprintf(w, "  %s\t%d\t%s\t%s\n",
				truncate(t.TaskID, 28), t.Attempt, exitCode, logPath)
		}
		w.Flush()
	}
	fmt.Println()
}

// displayRunsTimeline displays runs in a timeline format
func displayRunsTimeline(runs []*storage.Run) {
	if len(runs) == 0 {
		fmt.Println("No runs to display")
		return
	}

	fmt.Printf("\nWorkflow Execution Timeline:\n\n")

	for _, r := range runs {
		date := r.StartTime.Format("2006-01-02")
		timeStr := r.StartTime.Format("15:04:05")
		duration := "-"

		if r.DurationMs.Valid {
			duration = formatDuration(time.Duration(r.DurationMs.Int64) * time.Millisecond)
		}

		// ASCII timeline indicator
		indicator := "→"
		switch r.Status {
		case storage.RunSuccess:
			indicator = "✓"
		case storage.RunFailed:
			indicator = "✗"
		case storage.RunRunning:
			indicator = "⟳"
		case storage.RunCancelled:
			indicator = "⊗"
		}

		fmt.Printf("[%s %s] %s %s (%s) %s - %s\n",
			date, timeStr,
			indicator,
			r.WorkflowName,
			coloriseStatus(&r.Status),
			duration,
			truncate(r.ID, 8),
		)
	}

	fmt.Printf("\n")
}

// displayRunsJSON outputs runs in JSON format with enriched data
func displayRunsJSON(runs []*storage.Run, summary RunsSummary) error {
	type RunJSON struct {
		ID            string                `json:"id"`
		Workflow      string                `json:"workflow"`
		Status        string                `json:"status"`
		StartedAt     time.Time             `json:"started_at"`
		EndedAt       *time.Time            `json:"ended_at,omitempty"`
		Duration      string                `json:"duration"`
		TotalTasks    int                   `json:"total_tasks"`
		TasksSuccess  int                   `json:"tasks_success"`
		TasksFailed   int                   `json:"tasks_failed"`
		TasksSkipped  int                   `json:"tasks_skipped"`
		ExecutionMode storage.ExecutionMode `json:"execution_mode"`
		MaxParallel   int                   `json:"max_parallel"`
		ResumeCount   int                   `json:"resume_count,omitempty"`
		Tags          []string              `json:"tags,omitempty"`
		SuccessRate   float64               `json:"success_rate"`
	}

	type ResponseJSON struct {
		Summary RunsSummary `json:"summary"`
		Runs    []RunJSON   `json:"runs"`
	}

	var runsList []RunJSON
	for _, r := range runs {
		runJSON := RunJSON{
			ID:            r.ID,
			Workflow:      r.WorkflowName,
			Status:        string(r.Status),
			StartedAt:     r.StartTime,
			TotalTasks:    r.TotalTasks,
			TasksSuccess:  r.TasksSuccess,
			TasksFailed:   r.TasksFailed,
			TasksSkipped:  r.TasksSkipped,
			ExecutionMode: r.ExecutionMode,
			MaxParallel:   r.MaxParallel,
			ResumeCount:   r.ResumeCount,
		}

		if r.EndTime.Valid {
			runJSON.EndedAt = &r.EndTime.Time
		}

		if r.DurationMs.Valid {
			runJSON.Duration = formatDuration(time.Duration(r.DurationMs.Int64) * time.Millisecond)
		}

		if r.TotalTasks > 0 {
			runJSON.SuccessRate = float64(r.TasksSuccess) / float64(r.TotalTasks) * 100
		}
		if tags := r.RunTags(); len(tags) > 0 {
			runJSON.Tags = tags
		}

		runsList = append(runsList, runJSON)
	}

	response := ResponseJSON{
		Summary: summary,
		Runs:    runsList,
	}

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(response)
}

// ============ Helper Functions ============

// coloriseStatus adds color and icon to run status.
// Color is suppressed when stdout is not a terminal (see internal/tty).
func coloriseStatus(status *storage.RunStatus) string {
	type entry struct {
		icon  string
		label string
		code  string
	}
	m := map[storage.RunStatus]entry{
		storage.RunSuccess:   {"✓", "SUCCESS", "92"},
		storage.RunFailed:    {"✗", "FAILED", "91"},
		storage.RunRunning:   {"⟳", "RUNNING", "94"},
		storage.RunPending:   {"⊙", "PENDING", "93"},
		storage.RunCancelled: {"⊗", "CANCELLED", "96"},
		storage.RunResuming:  {"↻", "RESUMING", "95"},
	}
	if e, ok := m[*status]; ok {
		return tty.Colourise(e.icon+" "+e.label, e.code)
	}
	return string(*status)
}

// coloriseTaskStatus adds color to task status.
// Color is suppressed when stdout is not a terminal (see internal/tty).
func coloriseTaskStatus(status string) string {
	type entry struct {
		icon string
		code string
	}
	m := map[string]entry{
		"success":   {"✓", "92"},
		"failed":    {"✗", "91"},
		"running":   {"⟳", "94"},
		"pending":   {"⊙", "93"},
		"ready":     {"→", "94"},
		"skipped":   {"⊘", "96"},
		"cancelled": {"⊗", "91"},
	}
	if e, ok := m[status]; ok {
		return tty.Colourise(e.icon, e.code)
	}
	return status
}

// formatDuration formats a duration in a human-readable way
func formatDuration(d time.Duration) string {
	if d == 0 {
		return "-"
	}

	hours := d / time.Hour
	minutes := (d % time.Hour) / time.Minute
	seconds := (d % time.Minute) / time.Second

	if hours > 0 {
		return fmt.Sprintf("%dh %dm %ds", hours, minutes, seconds)
	}
	if minutes > 0 {
		return fmt.Sprintf("%dm %ds", minutes, seconds)
	}
	return fmt.Sprintf("%ds", seconds)
}

// formatDurationOrDash formats duration or returns dash if null
func formatDurationOrDash(d *sql.NullInt64) string {
	if d == nil || !d.Valid {
		return "-"
	}
	return formatDuration(time.Duration(d.Int64) * time.Millisecond)
}

// truncate truncates a string to specified length
func truncate(s string, length int) string {
	if len(s) <= length {
		return s
	}
	return s[:length]
}

func init() {
	rootCmd.AddCommand(runsCmd)

	// Filtering flags
	runsCmd.Flags().StringVarP(&runsWorkflow, "workflow", "w", "", "Filter by workflow name")
	runsCmd.Flags().StringVarP(&runsStatus, "status", "s", "", "Filter by status (pending|running|success|failed|cancelled|resuming)")
	runsCmd.Flags().StringVar(&runsStartDate, "start-date", "", "Filter runs starting from date (YYYY-MM-DD)")
	runsCmd.Flags().StringVar(&runsEndDate, "end-date", "", "Filter runs until date (YYYY-MM-DD)")
	runsCmd.Flags().StringVar(&runsMinDuration, "min-duration", "", "Filter runs with minimum duration (e.g., 30s, 5m, 1h)")

	// Display flags
	runsCmd.Flags().IntVarP(&runsLimit, "limit", "l", 10, "Limit number of results")
	runsCmd.Flags().IntVarP(&runsOffset, "offset", "o", 0, "Offset for pagination")
	runsCmd.Flags().StringVarP(&runsSort, "sort", "S", "time", "Sort by field (time|name|duration|tasks|status)")
	runsCmd.Flags().BoolVarP(&runsJSON, "json", "j", false, "Output in JSON format")
	runsCmd.Flags().BoolVarP(&runsDetailed, "detailed", "d", false, "Show detailed run information")
	runsCmd.Flags().BoolVar(&runsStats, "stats", false, "Show only summary statistics")
	runsCmd.Flags().BoolVar(&runsTimeline, "timeline", false, "Display runs in timeline format")
	runsCmd.Flags().BoolVar(&runsShowTasks, "tasks", false, "Show task summaries for each run")
	runsCmd.Flags().BoolVar(&runsShowLogs, "logs", false, "Show log file paths for failed tasks")
	runsCmd.Flags().StringVar(&runsTag, "tag", "", "Filter runs that include this tag (exact match)")
}
