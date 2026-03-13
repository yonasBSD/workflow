package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/joelfokou/workflow/internal/config"
	"github.com/joelfokou/workflow/internal/dag"
	"github.com/joelfokou/workflow/internal/storage"
	"github.com/joelfokou/workflow/internal/tty"
	"github.com/spf13/cobra"
)

var healthJSON bool

// HealthReport holds the structured result of a health check.
type HealthReport struct {
	DatabaseOK        bool     `json:"database_ok"`
	DatabaseSizeBytes int64    `json:"database_size_bytes,omitempty"`
	StaleRuns         int64    `json:"stale_runs"`
	WorkflowsValid    bool     `json:"workflows_valid"`
	WorkflowErrors    []string `json:"workflow_errors,omitempty"`
	LogDiskBytes      int64    `json:"log_disk_bytes,omitempty"`
	SuccessRate7d     float64  `json:"success_rate_7d"` // -1 = no data
	Healthy           bool     `json:"healthy"`
}

var healthCmd = &cobra.Command{
	Use:   "health",
	Short: "Show system health status",
	Long:  "Check database reachability, workflow validity, stale runs, log disk usage, and 7-day success rate.",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg := config.Get()
		report := HealthReport{SuccessRate7d: -1}

		// ── 1. Database reachability + size ──────────────────────────────
		store, err := storage.New(cfg.Paths.Database)
		if err != nil {
			report.DatabaseOK = false
		} else {
			defer store.Close()
			report.DatabaseOK = store.Ping() == nil

			if report.DatabaseOK {
				if sz, err := store.DBSizeBytes(); err == nil {
					report.DatabaseSizeBytes = sz
				}
				if stale, err := store.StaleRunCount(30); err == nil {
					report.StaleRuns = stale
				}
				if rate, err := store.RunSuccessRate(7); err == nil {
					report.SuccessRate7d = rate
				}
			}
		}

		// ── 2. Workflow validation ────────────────────────────────────────
		report.WorkflowsValid = true
		if cfg.Paths.Workflows != "" {
			entries, err := os.ReadDir(cfg.Paths.Workflows)
			if err == nil {
				for _, e := range entries {
					if e.IsDir() || filepath.Ext(e.Name()) != ".toml" {
						continue
					}
					name := e.Name()[:len(e.Name())-5]
					parser := dag.NewParser(name)
					def, perr := parser.Parse()
					if perr != nil {
						report.WorkflowsValid = false
						report.WorkflowErrors = append(report.WorkflowErrors, fmt.Sprintf("%s: %v", name, perr))
						continue
					}
					if _, berr := dag.NewBuilder(def).Build(); berr != nil {
						report.WorkflowsValid = false
						report.WorkflowErrors = append(report.WorkflowErrors, fmt.Sprintf("%s: %v", name, berr))
					}
				}
			}
		}

		// ── 3. Log disk usage ─────────────────────────────────────────────
		if cfg.Paths.Logs != "" {
			report.LogDiskBytes = dirSize(cfg.Paths.Logs)
		}

		// ── Overall health ────────────────────────────────────────────────
		report.Healthy = report.DatabaseOK && report.WorkflowsValid && report.StaleRuns == 0

		if healthJSON {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			if err := enc.Encode(report); err != nil {
				return err
			}
		} else {
			printHealthText(report)
		}

		if !report.Healthy {
			return fmt.Errorf("one or more health checks failed")
		}
		return nil
	},
}

func printHealthText(r HealthReport) {
	ok := func(v bool) string {
		if v {
			return tty.Colourise("✓ OK", "92")
		}
		return tty.Colourise("✗ FAIL", "91")
	}

	fmt.Println("=== SYSTEM HEALTH ===")
	fmt.Printf("Database:        %s", ok(r.DatabaseOK))
	if r.DatabaseOK && r.DatabaseSizeBytes > 0 {
		fmt.Printf("  (%s)", humanBytes(r.DatabaseSizeBytes))
	}
	fmt.Println()

	fmt.Printf("Stale runs:      ")
	if r.StaleRuns == 0 {
		fmt.Println(tty.Colourise("✓ none", "92"))
	} else {
		fmt.Println(tty.Colourise(fmt.Sprintf("✗ %d orphaned run(s) detected", r.StaleRuns), "91"))
	}

	fmt.Printf("Workflows:       %s", ok(r.WorkflowsValid))
	if len(r.WorkflowErrors) > 0 {
		fmt.Println()
		for _, e := range r.WorkflowErrors {
			fmt.Printf("  ! %s\n", e)
		}
	} else {
		fmt.Println()
	}

	if r.LogDiskBytes > 0 {
		fmt.Printf("Log disk usage:  %s\n", humanBytes(r.LogDiskBytes))
	}

	if r.SuccessRate7d >= 0 {
		pct := r.SuccessRate7d * 100
		code := "92"
		if pct < 80 {
			code = "91"
		} else if pct < 95 {
			code = "93"
		}
		fmt.Printf("Success rate 7d: %s\n", tty.Colourise(fmt.Sprintf("%.1f%%", pct), code))
	} else {
		fmt.Println("Success rate 7d: no data")
	}

	fmt.Println()
	if r.Healthy {
		fmt.Println(tty.Colourise("● System is healthy", "92"))
	} else {
		fmt.Println(tty.Colourise("● System needs attention", "91"))
	}
}

func humanBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(b)/float64(div), "KMGTPE"[exp])
}

func dirSize(path string) int64 {
	var size int64
	filepath.Walk(path, func(_ string, info os.FileInfo, err error) error { //nolint:errcheck
		if err == nil && !info.IsDir() {
			size += info.Size()
		}
		return nil
	})
	return size
}

func init() {
	rootCmd.AddCommand(healthCmd)
	healthCmd.Flags().BoolVarP(&healthJSON, "json", "j", false, "Output in JSON format")
}
