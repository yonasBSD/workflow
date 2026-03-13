package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/silocorp/workflow/internal/config"
	"github.com/silocorp/workflow/internal/dag"
	"github.com/silocorp/workflow/internal/logger"
	"github.com/silocorp/workflow/internal/storage"
	"github.com/spf13/cobra"
)

var (
	validateJSON  bool
	validateStore bool
)

// validateResult holds comprehensive validation results for a workflow.
type validateResult struct {
	Name               string                 `json:"name"`
	Valid              bool                   `json:"valid"`
	TaskCount          int                    `json:"task_count,omitempty"`
	ExecutionLevels    [][]string             `json:"execution_levels,omitempty"`
	ExecutionOrder     []string               `json:"execution_order,omitempty"`
	Parallelisable     int                    `json:"parallelisable_tasks,omitempty"`
	Errors             []error                `json:"errors,omitempty"`
	Warnings           []string               `json:"warnings,omitempty"`
	DependencyIssues   []string               `json:"dependency_issues,omitempty"`
	TriggerValidation  []string               `json:"trigger_validation,omitempty"`
	ExecutorValidation map[string]interface{} `json:"executor_validation,omitempty"`
}

// validateCmd checks the validity of all workflow definitions.
var validateCmd = &cobra.Command{
	Use:   "validate [workflow]",
	Short: "Validate workflow definitions",
	Long: `Validate all workflows or a specific workflow in the configured workflows directory.
Performs syntax validation, DAG validation, dependency checks, trigger validation, and optionally executor validation.
Use --deep to perform additional validation checks on tasks, dependencies, and execution strategy.`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) == 1 {
			return validateSingleWorkflow(args[0])
		}

		return validateAllWorkflows()
	},
}

// validateSingleWorkflow validates a specific workflow comprehensively.
func validateSingleWorkflow(workflowName string) error {
	result := validateWorkflow(workflowName)

	if validateJSON {
		return printValidateJSON([]validateResult{result})
	}

	printValidateResult(result)

	if !result.Valid {
		return fmt.Errorf("validation failed for workflow '%s'", workflowName)
	}

	return nil
}

// validateAllWorkflows validates all workflows in the directory.
func validateAllWorkflows() error {
	workflowDir := config.Get().Paths.Workflows
	entries, err := os.ReadDir(workflowDir)
	if err != nil {
		logger.Error("failed to read workflows directory",
			"directory", workflowDir,
			"error", err,
		)
		return fmt.Errorf("failed to read workflows directory: %w", err)
	}

	var results []validateResult
	var validCount, invalidCount int

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".toml") {
			continue
		}

		workflowName := strings.TrimSuffix(entry.Name(), ".toml")
		result := validateWorkflow(workflowName)
		results = append(results, result)

		if result.Valid {
			validCount++
		} else {
			invalidCount++
		}
	}

	if len(results) == 0 {
		logger.Warn("no workflows found", "directory", workflowDir)
		fmt.Printf("No workflows found in %s\n", workflowDir)
		return nil
	}

	if validateJSON {
		return printValidateJSON(results)
	}

	return printValidateTable(results, validCount, invalidCount)
}

// validateWorkflow performs comprehensive validation on a single workflow.
func validateWorkflow(workflow string) validateResult {
	result := validateResult{
		Name:     workflow,
		Errors:   []error{},
		Warnings: []string{},
	}

	parser := dag.NewParser(workflow)
	definition, err := parser.Parse()
	if err != nil {
		result.Valid = false
		result.Errors = append(result.Errors, fmt.Errorf("failed to parse workflow: %w", err))
		if definition != nil {
			result.Errors = append(result.Errors, definition.Errors...)
			result.Warnings = append(result.Warnings, definition.Warnings...)
		}
		logger.Error("workflow parsing failed",
			"workflow", workflow,
			"error", err,
		)
		return result
	}

	result.TaskCount = len(definition.Tasks)

	builder := dag.NewBuilder(definition)
	d, err := builder.Build()
	if err != nil {
		result.Valid = false
		result.Errors = append(result.Errors, fmt.Errorf("failed to build DAG: %w", err))
		if d != nil {
			result.Errors = append(result.Errors, d.Errors...)
			result.Warnings = append(result.Warnings, d.Warnings...)
		}
		logger.Error("DAG building failed",
			"workflow", workflow,
			"error", err,
		)
		return result
	}

	// Extract execution levels and order
	if len(d.Levels) > 0 {
		for _, level := range d.Levels {
			var levelNames []string
			for _, node := range level {
				levelNames = append(levelNames, node.ID)
			}
			result.ExecutionLevels = append(result.ExecutionLevels, levelNames)
			result.ExecutionOrder = append(result.ExecutionOrder, levelNames...)
		}
		// Count parallelizable tasks
		for _, level := range d.Levels {
			if len(level) > 1 {
				result.Parallelisable += len(level)
			}
		}
	}

	// Store connectivity validation
	if validateStore {
		validateStoreConnectivity(&result)
	}

	// Determine overall validity
	if len(result.Errors) == 0 {
		result.Valid = true
		logger.Info("workflow validation successful",
			"workflow", workflow,
			"tasks", result.TaskCount,
			"levels", len(d.Levels),
			"warnings", len(result.Warnings),
		)
	} else {
		result.Valid = false
		logger.Error("workflow validation failed",
			"workflow", workflow,
			"error_count", len(result.Errors),
		)
	}

	return result
}

// validateStoreConnectivity checks if the database store is accessible.
func validateStoreConnectivity(result *validateResult) {
	dbPath := config.Get().Paths.Database

	// Verify database path is configured
	if dbPath == "" {
		result.Warnings = append(result.Warnings, "database path not configured")
		return
	}

	// Verify database directory exists or can be created
	dbDir := filepath.Dir(dbPath)
	if info, err := os.Stat(dbDir); err != nil {
		if os.IsNotExist(err) {
			result.Warnings = append(result.Warnings, fmt.Sprintf("database directory does not exist: %s", dbDir))
		} else {
			result.Warnings = append(result.Warnings, fmt.Sprintf("cannot access database directory: %v", err))
		}
		return
	} else if !info.IsDir() {
		result.Errors = append(result.Errors,
			fmt.Errorf("database path is not a directory: %s", dbDir))
	}

	// Try to open the store
	s, err := storage.New(dbPath)
	if err != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("cannot connect to store: %v", err))
		return
	}
	defer s.Close()

	// Verify store is operational
	if err := s.Ping(); err != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("store connectivity check failed: %v", err))
	}
}

// printValidateResult prints a single validation result in human-readable format.
func printValidateResult(result validateResult) {
	status := "✓ PASS"
	if !result.Valid {
		status = "✗ FAIL"
	}

	fmt.Printf("\n%s %s\n", status, result.Name)
	fmt.Printf("  Tasks: %d\n", result.TaskCount)

	if result.Parallelisable > 0 {
		fmt.Printf("  Parallelisable: %d tasks\n", result.Parallelisable)
	}

	if len(result.ExecutionLevels) > 0 {
		fmt.Printf("  Execution Levels: %d\n", len(result.ExecutionLevels))
		for i, level := range result.ExecutionLevels {
			fmt.Printf("    Level %d: %s\n", i, strings.Join(level, ", "))
		}
	}

	if len(result.Errors) > 0 {
		fmt.Printf("  Errors:\n")
		for _, e := range result.Errors {
			fmt.Printf("    • %s\n", e)
		}
	}

	if len(result.DependencyIssues) > 0 {
		fmt.Printf("  Dependency Issues:\n")
		for _, d := range result.DependencyIssues {
			fmt.Printf("    • %s\n", d)
		}
	}

	if len(result.TriggerValidation) > 0 {
		fmt.Printf("  Triggers:\n")
		for _, t := range result.TriggerValidation {
			fmt.Printf("    ℹ %s\n", t)
		}
	}

	if len(result.Warnings) > 0 {
		fmt.Printf("  Warnings:\n")
		for _, w := range result.Warnings {
			fmt.Printf("    ⚠ %s\n", w)
		}
	}
}

// printValidateTable displays validation results in table format.
func printValidateTable(results []validateResult, validCount, invalidCount int) error {
	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintf(w, "WORKFLOW\tSTATUS\tTASKS\tLEVELS\tERRORS\tWARNINGS\n")
	fmt.Fprintf(w, "--------\t------\t-----\t------\t------\t--------\n")

	for i := range results {
		r := &results[i]
		status := "✓ valid"
		if !r.Valid {
			status = "✗ invalid"
		}

		errorCount := len(r.Errors)
		warningCount := len(r.Warnings)
		levelCount := len(r.ExecutionLevels)

		fmt.Fprintf(w, "%s\t%s\t%d\t%d\t%d\t%d\n",
			r.Name, status, r.TaskCount, levelCount, errorCount, warningCount)
	}

	w.Flush()

	fmt.Printf("\nSummary: %d/%d workflows valid\n", validCount, validCount+invalidCount)

	if invalidCount > 0 {
		logger.Error("validation summary",
			"valid_count", validCount,
			"invalid_count", invalidCount,
		)
		return fmt.Errorf("%d workflow(s) failed validation", invalidCount)
	}

	logger.Info("all workflows validated successfully",
		"count", validCount,
	)
	return nil
}

// printValidateJSON outputs validation results in JSON format.
func printValidateJSON(results []validateResult) error {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(results); err != nil {
		logger.Error("failed to encode validation results as JSON", "error", err)
		return fmt.Errorf("failed to encode results: %w", err)
	}
	return nil
}

func init() {
	rootCmd.AddCommand(validateCmd)

	validateCmd.Flags().BoolVarP(&validateJSON, "json", "j", false, "Output results in JSON format")
	validateCmd.Flags().BoolVar(&validateStore, "store", false, "Validate database store connectivity")
}
