package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"text/tabwriter"
	"time"

	"github.com/joelfokou/workflow/internal/config"
	"github.com/joelfokou/workflow/internal/storage"
	"github.com/spf13/cobra"
)

var diffJSON bool

// diffCmd implements `wf diff <run-id-a> <run-id-b>`.
var diffCmd = &cobra.Command{
	Use:   "diff <run-id-a> <run-id-b>",
	Short: "Compare two workflow runs",
	Long: `Show the differences between two workflow runs side-by-side.

The comparison covers:
  • Overall status and duration
  • Task-level status, duration, attempt count, and error messages
  • Context variables written during each run

Tasks that exist in only one run are flagged as added (+) or removed (-).`,
	Args: cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		idA, idB := args[0], args[1]

		st, err := storage.New(config.Get().Paths.Database)
		if err != nil {
			return fmt.Errorf("failed to open store: %w", err)
		}
		defer st.Close()

		runA, err := st.GetRun(idA)
		if err != nil {
			return fmt.Errorf("run %q not found: %w", idA, err)
		}
		runB, err := st.GetRun(idB)
		if err != nil {
			return fmt.Errorf("run %q not found: %w", idB, err)
		}

		tasksA, err := st.ListTaskExecutions(storage.TaskFilters{RunID: idA})
		if err != nil {
			return fmt.Errorf("failed to list tasks for run A: %w", err)
		}
		tasksB, err := st.ListTaskExecutions(storage.TaskFilters{RunID: idB})
		if err != nil {
			return fmt.Errorf("failed to list tasks for run B: %w", err)
		}

		varsA, err := st.ListContextSnapshots(storage.ContextSnapshotFilters{RunID: idA})
		if err != nil {
			return fmt.Errorf("failed to list context for run A: %w", err)
		}
		varsB, err := st.ListContextSnapshots(storage.ContextSnapshotFilters{RunID: idB})
		if err != nil {
			return fmt.Errorf("failed to list context for run B: %w", err)
		}

		diff := buildDiff(runA, runB, tasksA, tasksB, varsA, varsB)

		if diffJSON {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(diff)
		}

		printDiff(diff)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(diffCmd)
	diffCmd.Flags().BoolVarP(&diffJSON, "json", "j", false, "Output diff in JSON format")
}

// ─── Diff model ───────────────────────────────────────────────────────────────

// RunDiff is the complete result of comparing two runs.
type RunDiff struct {
	RunA    RunSummary `json:"run_a"`
	RunB    RunSummary `json:"run_b"`
	Tasks   []TaskDiff `json:"tasks"`
	Vars    []VarDiff  `json:"variables"`
	Changed bool       `json:"changed"`
}

// RunSummary is the subset of run fields used in a diff.
type RunSummary struct {
	ID           string            `json:"id"`
	Workflow     string            `json:"workflow"`
	Status       storage.RunStatus `json:"status"`
	StartedAt    time.Time         `json:"started_at"`
	DurationMs   int64             `json:"duration_ms"`
	TotalTasks   int               `json:"total_tasks"`
	TasksSuccess int               `json:"tasks_success"`
	TasksFailed  int               `json:"tasks_failed"`
	TasksSkipped int               `json:"tasks_skipped"`
	Tags         []string          `json:"tags,omitempty"`
}

// TaskDiff represents the comparison of a single task across two runs.
type TaskDiff struct {
	TaskID   string `json:"task_id"`
	TaskName string `json:"task_name"`
	// DiffKind: "same", "changed", "only_a", "only_b"
	DiffKind  string `json:"diff"`
	StatusA   string `json:"status_a,omitempty"`
	StatusB   string `json:"status_b,omitempty"`
	DurationA int64  `json:"duration_ms_a,omitempty"`
	DurationB int64  `json:"duration_ms_b,omitempty"`
	AttemptA  int    `json:"attempt_a,omitempty"`
	AttemptB  int    `json:"attempt_b,omitempty"`
	ErrorA    string `json:"error_a,omitempty"`
	ErrorB    string `json:"error_b,omitempty"`
}

// VarDiff represents a variable that differs between the two runs.
type VarDiff struct {
	Name string `json:"name"`
	// DiffKind: "same", "changed", "only_a", "only_b"
	DiffKind string `json:"diff"`
	ValueA   string `json:"value_a,omitempty"`
	ValueB   string `json:"value_b,omitempty"`
}

// ─── Build diff ───────────────────────────────────────────────────────────────

func buildDiff(
	runA, runB *storage.Run,
	tasksA, tasksB []*storage.TaskExecution,
	varsA, varsB []*storage.ContextSnapshot,
) RunDiff {
	diff := RunDiff{
		RunA: toRunSummary(runA),
		RunB: toRunSummary(runB),
	}

	// ── Task diff ────────────────────────────────────────────────────────────
	// Index tasks by task_id; last write per task_id wins (covers retries).
	indexA := indexTasks(tasksA)
	indexB := indexTasks(tasksB)

	// Union of all task IDs, preserving order: A first, then B-only extras.
	seen := make(map[string]bool)
	var order []string
	for _, t := range tasksA {
		if !seen[t.TaskID] {
			seen[t.TaskID] = true
			order = append(order, t.TaskID)
		}
	}
	for _, t := range tasksB {
		if !seen[t.TaskID] {
			seen[t.TaskID] = true
			order = append(order, t.TaskID)
		}
	}

	for _, id := range order {
		tA, inA := indexA[id]
		tB, inB := indexB[id]

		td := TaskDiff{TaskID: id}

		switch {
		case inA && !inB:
			td.TaskName = tA.TaskName
			td.DiffKind = "only_a"
			td.StatusA = string(tA.State)
			td.DurationA = nullInt64(tA.DurationMs.Int64, tA.DurationMs.Valid)
			td.AttemptA = tA.Attempt
			td.ErrorA = nullString(tA.ErrorMessage.String, tA.ErrorMessage.Valid)
			diff.Changed = true

		case !inA && inB:
			td.TaskName = tB.TaskName
			td.DiffKind = "only_b"
			td.StatusB = string(tB.State)
			td.DurationB = nullInt64(tB.DurationMs.Int64, tB.DurationMs.Valid)
			td.AttemptB = tB.Attempt
			td.ErrorB = nullString(tB.ErrorMessage.String, tB.ErrorMessage.Valid)
			diff.Changed = true

		default: // in both
			td.TaskName = tA.TaskName
			td.StatusA = string(tA.State)
			td.StatusB = string(tB.State)
			td.DurationA = nullInt64(tA.DurationMs.Int64, tA.DurationMs.Valid)
			td.DurationB = nullInt64(tB.DurationMs.Int64, tB.DurationMs.Valid)
			td.AttemptA = tA.Attempt
			td.AttemptB = tB.Attempt
			td.ErrorA = nullString(tA.ErrorMessage.String, tA.ErrorMessage.Valid)
			td.ErrorB = nullString(tB.ErrorMessage.String, tB.ErrorMessage.Valid)

			if td.StatusA == td.StatusB && td.ErrorA == td.ErrorB {
				td.DiffKind = "same"
			} else {
				td.DiffKind = "changed"
				diff.Changed = true
			}
		}

		diff.Tasks = append(diff.Tasks, td)
	}

	// ── Variable diff ─────────────────────────────────────────────────────────
	// Use only the latest value per variable name (highest snapshot_time).
	latestA := latestVars(varsA)
	latestB := latestVars(varsB)

	varsSeen := make(map[string]bool)
	for name := range latestA {
		varsSeen[name] = true
	}
	for name := range latestB {
		varsSeen[name] = true
	}

	for name := range varsSeen {
		valA, hasA := latestA[name]
		valB, hasB := latestB[name]

		vd := VarDiff{Name: name}
		switch {
		case hasA && !hasB:
			vd.DiffKind = "only_a"
			vd.ValueA = valA
			diff.Changed = true
		case !hasA && hasB:
			vd.DiffKind = "only_b"
			vd.ValueB = valB
			diff.Changed = true
		case valA == valB:
			vd.DiffKind = "same"
			vd.ValueA = valA
			vd.ValueB = valB
		default:
			vd.DiffKind = "changed"
			vd.ValueA = valA
			vd.ValueB = valB
			diff.Changed = true
		}
		diff.Vars = append(diff.Vars, vd)
	}

	// Stable sort: changed/only first, then alphabetically.
	sortVarDiffs(diff.Vars)
	sortTaskDiffs(diff.Tasks)

	return diff
}

// ─── Print diff ───────────────────────────────────────────────────────────────

func printDiff(diff RunDiff) {
	a, b := diff.RunA, diff.RunB

	fmt.Println("\n═══ RUN DIFF ═══")
	fmt.Printf("  A: %s  (%s)  %s\n", truncate(a.ID, 12), a.Workflow, a.Status)
	fmt.Printf("  B: %s  (%s)  %s\n", truncate(b.ID, 12), b.Workflow, b.Status)

	// Overall
	fmt.Println("\n─── Overview ───────────────────────────────────────────────────")
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "  FIELD\tA\tB\tΔ")
	fmt.Fprintln(w, "  -----\t-\t-\t-")

	printRunRow(w, "status", string(a.Status), string(b.Status))
	printRunRow(w, "duration", fmtMs(a.DurationMs), fmtMs(b.DurationMs))
	printRunRow(w, "total_tasks", fmt.Sprint(a.TotalTasks), fmt.Sprint(b.TotalTasks))
	printRunRow(w, "tasks_success", fmt.Sprint(a.TasksSuccess), fmt.Sprint(b.TasksSuccess))
	printRunRow(w, "tasks_failed", fmt.Sprint(a.TasksFailed), fmt.Sprint(b.TasksFailed))
	printRunRow(w, "tasks_skipped", fmt.Sprint(a.TasksSkipped), fmt.Sprint(b.TasksSkipped))
	w.Flush()

	// Tasks
	changed := filterTaskDiffs(diff.Tasks, func(td TaskDiff) bool { return td.DiffKind != "same" })
	if len(changed) == 0 {
		fmt.Println("\n─── Tasks: no differences ──────────────────────────────────────")
	} else {
		fmt.Printf("\n─── Tasks (%d changed) ─────────────────────────────────────────\n", len(changed))
		tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(tw, "  DIFF\tTASK\tSTATUS A\tSTATUS B\tDURATION A\tDURATION B\tATTEMPT A\tATTEMPT B")
		fmt.Fprintln(tw, "  ----\t----\t--------\t--------\t----------\t----------\t---------\t---------")
		for i := range changed {
			td := &changed[i]
			marker := diffMarker(td.DiffKind)
			fmt.Fprintf(tw, "  %s\t%s\t%s\t%s\t%s\t%s\t%d\t%d\n",
				marker, td.TaskName,
				td.StatusA, td.StatusB,
				fmtMs(td.DurationA), fmtMs(td.DurationB),
				td.AttemptA, td.AttemptB,
			)
			if td.ErrorA != "" {
				fmt.Fprintf(tw, "   \terror A: %s\n", td.ErrorA)
			}
			if td.ErrorB != "" {
				fmt.Fprintf(tw, "   \terror B: %s\n", td.ErrorB)
			}
		}
		tw.Flush()
	}

	// Variables
	changedVars := filterVarDiffs(diff.Vars, func(vd VarDiff) bool { return vd.DiffKind != "same" })
	if len(changedVars) == 0 {
		fmt.Println("\n─── Variables: no differences ──────────────────────────────────")
	} else {
		fmt.Printf("\n─── Variables (%d changed) ──────────────────────────────────────\n", len(changedVars))
		vw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(vw, "  DIFF\tVARIABLE\tVALUE A\tVALUE B")
		fmt.Fprintln(vw, "  ----\t--------\t-------\t-------")
		for _, vd := range changedVars {
			fmt.Fprintf(vw, "  %s\t%s\t%s\t%s\n",
				diffMarker(vd.DiffKind), vd.Name, vd.ValueA, vd.ValueB)
		}
		vw.Flush()
	}

	fmt.Println()
	if diff.Changed {
		fmt.Println("Result: runs differ")
	} else {
		fmt.Println("Result: runs are identical")
	}
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

func toRunSummary(r *storage.Run) RunSummary {
	s := RunSummary{
		ID:           r.ID,
		Workflow:     r.WorkflowName,
		Status:       r.Status,
		StartedAt:    r.StartTime,
		TotalTasks:   r.TotalTasks,
		TasksSuccess: r.TasksSuccess,
		TasksFailed:  r.TasksFailed,
		TasksSkipped: r.TasksSkipped,
		Tags:         r.RunTags(),
	}
	if r.DurationMs.Valid {
		s.DurationMs = r.DurationMs.Int64
	}
	return s
}

func indexTasks(tasks []*storage.TaskExecution) map[string]*storage.TaskExecution {
	m := make(map[string]*storage.TaskExecution, len(tasks))
	for _, t := range tasks {
		// Later rows overwrite earlier ones — highest attempt wins.
		if prev, ok := m[t.TaskID]; !ok || t.Attempt > prev.Attempt {
			m[t.TaskID] = t
		}
	}
	return m
}

func latestVars(snaps []*storage.ContextSnapshot) map[string]string {
	m := make(map[string]string)
	latest := make(map[string]time.Time)
	for _, s := range snaps {
		if t, ok := latest[s.VariableName]; !ok || s.SnapshotTime.After(t) {
			m[s.VariableName] = s.VariableValue
			latest[s.VariableName] = s.SnapshotTime
		}
	}
	return m
}

func nullInt64(ms int64, valid bool) int64 {
	if valid {
		return ms
	}
	return 0
}

func nullString(s string, valid bool) string {
	if valid {
		return s
	}
	return ""
}

func fmtMs(ms int64) string {
	if ms == 0 {
		return "-"
	}
	return formatDuration(time.Duration(ms) * time.Millisecond)
}

func diffMarker(kind string) string {
	switch kind {
	case "only_a":
		return "-"
	case "only_b":
		return "+"
	case "changed":
		return "~"
	default:
		return " "
	}
}

func printRunRow(w *tabwriter.Writer, field, a, b string) {
	marker := " "
	if a != b {
		marker = "~"
	}
	fmt.Fprintf(w, "  %s %s\t%s\t%s\t\n", marker, field, a, b)
}

func filterTaskDiffs(in []TaskDiff, fn func(TaskDiff) bool) []TaskDiff {
	var out []TaskDiff
	for i := range in {
		if fn(in[i]) {
			out = append(out, in[i])
		}
	}
	return out
}

func filterVarDiffs(in []VarDiff, fn func(VarDiff) bool) []VarDiff {
	var out []VarDiff
	for _, vd := range in {
		if fn(vd) {
			out = append(out, vd)
		}
	}
	return out
}

func sortTaskDiffs(diffs []TaskDiff) {
	order := map[string]int{"only_a": 0, "only_b": 1, "changed": 2, "same": 3}
	sort.Slice(diffs, func(i, j int) bool {
		oi, oj := order[diffs[i].DiffKind], order[diffs[j].DiffKind]
		if oi != oj {
			return oi < oj
		}
		return diffs[i].TaskName < diffs[j].TaskName
	})
}

func sortVarDiffs(diffs []VarDiff) {
	order := map[string]int{"only_a": 0, "only_b": 1, "changed": 2, "same": 3}
	sort.Slice(diffs, func(i, j int) bool {
		oi, oj := order[diffs[i].DiffKind], order[diffs[j].DiffKind]
		if oi != oj {
			return oi < oj
		}
		return diffs[i].Name < diffs[j].Name
	})
}
