package dag

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/silocorp/workflow/internal/config"
	"github.com/silocorp/workflow/internal/logger"
	"github.com/silocorp/workflow/internal/security"
	"github.com/pelletier/go-toml/v2"
)

type Parser struct {
	workflow string
}

func NewParser(workflow string) *Parser {
	return &Parser{workflow: workflow}
}

func (p *Parser) Parse() (*WorkflowDefinition, error) {
	logger.Debug("parsing workflow", "workflow", p.workflow)

	data, err := p.readWorkflowFile()
	if err != nil {
		return nil, fmt.Errorf("failed to read workflow file: %w", err)
	}

	var def WorkflowDefinition

	if err := toml.Unmarshal(data, &def); err != nil {
		logger.Error("failed to unmarshal workflow TOML", "workflow", p.workflow, "error", err)
		return nil, fmt.Errorf("failed to unmarshal workflow: %w", err)
	}

	def.FilePath = filepath.Join(config.Get().Paths.Workflows, strings.TrimSuffix(p.workflow, ".toml")+".toml")

	if err := p.validate(&def); err != nil {
		logger.Error("workflow validation failed", "workflow", p.workflow, "error", err)
		return &def, fmt.Errorf("workflow definition validation failed: %w", err)
	}

	logger.Debug("workflow parsed", "workflow", p.workflow, "tasks", len(def.Tasks))
	return &def, nil
}

func (p *Parser) readWorkflowFile() ([]byte, error) {
	workflowsDir := config.Get().Paths.Workflows

	// Guard against path traversal before touching the filesystem.
	if err := security.ValidateWorkflowName(p.workflow, workflowsDir); err != nil {
		return nil, fmt.Errorf("invalid workflow name %q: %w", p.workflow, err)
	}

	workflow := strings.TrimSuffix(p.workflow, ".toml")
	filePath := filepath.Join(workflowsDir, workflow+".toml")
	data, err := os.ReadFile(filePath)
	if err != nil {
		logger.Error("failed to read workflow file", "path", filePath, "error", err)
		return nil, fmt.Errorf("failed to read workflow file %s: %w", filePath, err)
	}

	return data, nil
}

func (p *Parser) validate(def *WorkflowDefinition) error {
	if def.Name == "" {
		def.Name = p.workflow
		def.Warnings = append(def.Warnings, fmt.Sprintf("workflow name is required and has been set to filename '%s'", def.Name))
	}

	if def.Name != p.workflow {
		def.Warnings = append(def.Warnings, fmt.Sprintf("workflow name '%s' does not match expected name '%s'", def.Name, p.workflow))
	}

	if len(def.Tasks) == 0 {
		def.Errors = append(def.Errors, fmt.Errorf("workflow must define at least one task"))
	}

	seen := make(map[string]struct{}, len(def.Tasks))
	for name, task := range def.Tasks {
		prefix := fmt.Sprintf("task '%s'", name)

		if !TaskNamePattern.MatchString(name) {
			def.Errors = append(def.Errors, fmt.Errorf("%s has invalid name (allowed: letters, digits, _, -)", prefix))
		}

		if task.Cmd == "" {
			def.Errors = append(def.Errors, fmt.Errorf("%s must have a command", prefix))
		}

		if _, ok := seen[name]; ok {
			def.Errors = append(def.Errors, fmt.Errorf("duplicate task name: %s", name))
		}
		seen[name] = struct{}{}

		for _, dep := range task.DependsOn {
			if _, ok := def.Tasks[dep]; !ok {
				// Allow matrix-expanded references: "taskname[key=val,...]"
				// Strip the bracket suffix and check the base task name instead.
				baseDep := dep
				if idx := strings.Index(dep, "["); idx != -1 {
					baseDep = dep[:idx]
				}
				if baseDep == dep {
					// No bracket — genuinely missing task.
					def.Errors = append(def.Errors, fmt.Errorf("%s depends on missing task %s", prefix, dep))
				} else if baseTask, ok := def.Tasks[baseDep]; !ok {
					def.Errors = append(def.Errors, fmt.Errorf("%s depends on missing task %s (base task '%s' not found)", prefix, dep, baseDep))
				} else if len(baseTask.Matrix) == 0 {
					def.Errors = append(def.Errors, fmt.Errorf("%s depends on %s but task '%s' has no matrix", prefix, dep, baseDep))
				}
			}
		}

		// Validate timeout
		if task.Timeout > 0 && task.Timeout < Duration(time.Second) {
			def.Warnings = append(def.Warnings, fmt.Sprintf("%s has unusually low timeout: %v", prefix, task.Timeout))
		}

		if task.Timeout > Duration(24*time.Hour) {
			def.Warnings = append(def.Warnings, fmt.Sprintf("%s has very high timeout: %v", prefix, task.Timeout))
		}

		// Validate retries
		if task.Retries < 0 {
			def.Errors = append(def.Errors, fmt.Errorf("%s has invalid negative retries: %d", prefix, task.Retries))
		}

		if task.Retries > 10 {
			def.Warnings = append(def.Warnings, fmt.Sprintf("%s has high retry count: %d (consider reducing)", prefix, task.Retries))
		}

		// Validate retry delay
		if task.Retries > 0 && task.RetryDelay == 0 {
			def.Warnings = append(def.Warnings, fmt.Sprintf("%s has retries but no retry_delay specified", prefix))
		}

		if task.RetryDelay > 0 && task.RetryDelay < Duration(100*time.Millisecond) {
			def.Warnings = append(def.Warnings, fmt.Sprintf("%s has very short retry_delay: %v", prefix, task.RetryDelay))
		}

		// Validate working directory
		if task.WorkingDir != "" {
			if !filepath.IsAbs(task.WorkingDir) && !strings.HasPrefix(task.WorkingDir, ".") {
				def.Warnings = append(def.Warnings, fmt.Sprintf("%s working_dir is relative and may be unpredictable: %s", prefix, task.WorkingDir))
			}
			if err := security.ValidateWorkingDir(task.WorkingDir); err != nil {
				def.Errors = append(def.Errors, fmt.Errorf("%s invalid working_dir: %w", prefix, err))
			}
		}

		// Validate environment variables
		seen := make(map[string]bool)
		for key := range task.Env {
			if seen[key] {
				def.Errors = append(def.Errors, fmt.Errorf("%s has duplicate environment variable: %s", prefix, key))
			}
			seen[key] = true

			if err := security.ValidateEnvKey(key); err != nil {
				def.Errors = append(def.Errors, fmt.Errorf("%s env key %q: %w", prefix, key, err))
			}
		}

		// Validate output handling
		if task.Register != "" && task.IgnoreFailure && task.Retries > 0 {
			def.Warnings = append(def.Warnings, fmt.Sprintf("%s with register, ignore_failure, and retries may produce inconsistent output", prefix))
		}

		if task.If != "" {
			// Basic validation of condition syntax
			if !p.isValidCondition(task.If) {
				def.Warnings = append(def.Warnings, fmt.Sprintf("%s has potentially invalid condition: %s", prefix, task.If))
			}
		}
	}

	if len(def.Triggers) == 0 {
		def.Warnings = append(def.Warnings, "no triggers defined (manual execution only)")
	}

	for i, trigger := range def.Triggers {
		switch trigger.Type {
		case TriggerTypeCron:
			if config, ok := trigger.Config["schedule"]; !ok || config == "" {
				def.Errors = append(def.Errors, fmt.Errorf("trigger %d (cron): missing or empty 'schedule' configuration", i))
			}

		case TriggerTypeFileWatch:
			if path, ok := trigger.Config["path"]; !ok || path == "" {
				def.Errors = append(def.Errors, fmt.Errorf("trigger %d (file_watch): missing or empty 'path' configuration", i))
			}

		case TriggerTypeWebhook:
			if endpoint, ok := trigger.Config["endpoint"]; !ok || endpoint == "" {
				def.Errors = append(def.Errors, fmt.Errorf("trigger %d (webhook): missing or empty 'endpoint' configuration", i))
			}

		default:
			def.Warnings = append(def.Warnings, fmt.Sprintf("trigger %d: unknown trigger type '%s'", i, trigger.Type))
		}
	}

	if len(def.Errors) > 0 {
		return fmt.Errorf("workflow definition has %d error(s)", len(def.Errors))
	}

	return nil
}

// isValidCondition performs basic validation of condition syntax.
// EvalCondition supports only single binary comparisons of the form:
//
//	<variable> <op> <literal>
//
// where op is one of: ==  !=  >=  <=  >  <
// Compound expressions (&&, ||, etc.) are not supported.
func (p *Parser) isValidCondition(cond string) bool {
	cond = strings.TrimSpace(cond)
	if cond == "" {
		return false
	}

	// Only the operators that EvalCondition can actually execute.
	validPatterns := []string{"==", "!=", ">=", "<=", ">", "<"}

	for _, pattern := range validPatterns {
		if strings.Contains(cond, pattern) {
			return true
		}
	}

	return false
}
