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

var auditJSON bool

var auditCmd = &cobra.Command{
	Use:   "audit <run-id>",
	Short: "Show the audit trail for a workflow run",
	Long:  "Display a chronological log of all significant events that occurred during a workflow run.",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		runID := args[0]

		store, err := storage.New(config.Get().Paths.Database)
		if err != nil {
			return fmt.Errorf("database error: %w", err)
		}
		defer store.Close()

		// Verify the run exists
		if _, err := store.GetRun(runID); err != nil {
			return fmt.Errorf("run not found: %w", err)
		}

		entries, err := store.ListAuditTrail(storage.AuditTrailFilters{RunID: runID})
		if err != nil {
			return fmt.Errorf("failed to fetch audit trail: %w", err)
		}

		if auditJSON {
			return json.NewEncoder(os.Stdout).Encode(entries)
		}

		if len(entries) == 0 {
			fmt.Println("No audit events recorded for this run.")
			return nil
		}

		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "TIME\tEVENT\tDATA")
		fmt.Fprintln(w, "----\t-----\t----")
		for _, e := range entries {
			// Pretty-print the JSON data as a single compact line
			data := compactJSON(e.EventData)
			fmt.Fprintf(w, "%s\t%s\t%s\n",
				e.CreatedAt.Format("15:04:05.000"),
				e.EventType,
				data,
			)
		}
		return w.Flush()
	},
}

// compactJSON returns s re-marshalled with no extra whitespace, or s verbatim
// on parse error.
func compactJSON(s string) string {
	var v interface{}
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		return s
	}
	b, err := json.Marshal(v)
	if err != nil {
		return s
	}
	return string(b)
}

func init() {
	rootCmd.AddCommand(auditCmd)
	auditCmd.Flags().BoolVarP(&auditJSON, "json", "j", false, "Output in JSON format")
}
