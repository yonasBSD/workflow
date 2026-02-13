package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/joelfokou/workflow/internal/config"
	"github.com/joelfokou/workflow/internal/dag"
	"github.com/joelfokou/workflow/internal/logger"
	"github.com/spf13/cobra"
)

var (
	validateJSON bool
)

// validateResult holds the result of validating a single workflow.
type validateResult struct {
	Name  string `json:"name"`
	Valid bool   `json:"valid"`
	Error string `json:"error,omitempty"`
}

// validateCmd checks the validity of all workflow definitions in the configured workflows directory, logging errors if any are found and confirming success if all workflows are valid.
var validateCmd = &cobra.Command{
	Use:   "validate [workflow]",
	Short: "Validate workflow definitions",
	Long:  "Validate all workflows or a specific workflow in the configured workflows directory",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) == 1 {
			return validateSingleWorkflow(args[0])
		}

		return validateAllWorkflows()
	},
}

// validateSingleWorkflow validates a specific workflow.
func validateSingleWorkflow(workflowName string) error {
	d, err := dag.Load(workflowName)
	if err != nil {
		logger.Error("workflow validation failed",
			"workflow", workflowName,
			"error", err,
		)

		result := validateResult{
			Name:  workflowName,
			Valid: false,
			Error: err.Error(),
		}

		if validateJSON {
			return printValidateJSON([]validateResult{result})
		}

		fmt.Printf("✗ %s: %v\n", workflowName, err)
		return err
	}

	// Additional validation checks
	order, err := d.TopologicalSort()
	if err != nil {
		logger.Error("topological sort failed",
			"workflow", workflowName,
			"error", err,
		)

		result := validateResult{
			Name:  workflowName,
			Valid: false,
			Error: err.Error(),
		}

		if validateJSON {
			return printValidateJSON([]validateResult{result})
		}

		fmt.Printf("✗ %s: %v\n", workflowName, err)
		return err
	}

	result := validateResult{
		Name:  workflowName,
		Valid: true,
	}

	logger.Info("workflow validation successful",
		"workflow", workflowName,
		"tasks", len(d.Tasks),
		"execution_order_length", len(order),
	)

	if validateJSON {
		return printValidateJSON([]validateResult{result})
	}

	fmt.Printf("✓ %s: valid (%d tasks)\n", workflowName, len(d.Tasks))
	return nil
}

// validateAllWorkflows validates all workflows in the directory.
func validateAllWorkflows() error {
	entries, err := os.ReadDir(config.Get().Paths.Workflows)
	if err != nil {
		logger.Error("failed to read workflows directory",
			"directory", config.Get().Paths.Workflows,
			"error", err,
		)
		return fmt.Errorf("failed to read workflows directory: %w", err)
	}

	var results []validateResult
	var failedCount int

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".toml") {
			continue
		}

		workflowName := strings.TrimSuffix(entry.Name(), ".toml")
		d, err := dag.Load(workflowName)

		if err != nil {
			logger.Warn("workflow validation failed",
				"workflow", workflowName,
				"error", err,
			)

			results = append(results, validateResult{
				Name:  workflowName,
				Valid: false,
				Error: err.Error(),
			})
			failedCount++
			continue
		}

		// Check topological sort
		if _, err := d.TopologicalSort(); err != nil {
			logger.Warn("topological sort failed",
				"workflow", workflowName,
				"error", err,
			)

			results = append(results, validateResult{
				Name:  workflowName,
				Valid: false,
				Error: err.Error(),
			})
			failedCount++
			continue
		}

		results = append(results, validateResult{
			Name:  workflowName,
			Valid: true,
		})
	}

	if validateJSON {
		return printValidateJSON(results)
	}

	return printValidateTable(results, failedCount)
}

// printValidateTable displays validation results in table format.
func printValidateTable(results []validateResult, failedCount int) error {
	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintf(w, "WORKFLOW\tSTATUS\tERROR\n")
	fmt.Fprintf(w, "--------\t------\t-----\n")

	for _, r := range results {
		status := "✓ valid"
		errMsg := "-"

		if !r.Valid {
			status = "✗ invalid"
			errMsg = truncateError(r.Error, 50)
		}

		fmt.Fprintf(w, "%s\t%s\t%s\n", r.Name, status, errMsg)
	}

	w.Flush()

	fmt.Printf("\n%d/%d workflows valid\n", len(results)-failedCount, len(results))

	if failedCount > 0 {
		logger.Error("validation failed", "failed_count", failedCount, "total_count", len(results))
		return fmt.Errorf("%d workflow(s) failed validation", failedCount)
	}

	logger.Info("all workflows validated successfully", "count", len(results))
	return nil
}

// printValidateJSON outputs validation results in JSON format.
func printValidateJSON(results []validateResult) error {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(results)
}

// truncateError truncates error messages to a maximum length.
func truncateError(msg string, maxLen int) string {
	if len(msg) > maxLen {
		return msg[:maxLen-3] + "..."
	}
	return msg
}

func init() {
	rootCmd.AddCommand(validateCmd)

	validateCmd.Flags().BoolVar(&validateJSON, "json", false, "Output in JSON format")
}
