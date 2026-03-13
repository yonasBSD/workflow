// Package dag provides structures and methods to manage Directed Acyclic Graphs (DAGs) for task scheduling.
package dag

import (
	"regexp"
	"sync"
	"time"
)

// WorkflowDefinition is the root parsed from TOML
type WorkflowDefinition struct {
	Name        string                     `toml:"name"`
	FilePath    string                     `toml:"-"`
	Description string                     `toml:"description,omitempty"`
	Tags        []string                   `toml:"tags,omitempty"`       // searchable labels; stored as JSON in the DB
	Triggers    []Trigger                  `toml:"triggers,omitempty"`   // v2.0
	OnFailure   string                     `toml:"on_failure,omitempty"` // Global forensic trap task name
	Tasks       map[string]*TaskDefinition `toml:"tasks"`
	Warnings    []string                   `toml:"-"`
	Errors      []error                    `toml:"-"`
}

// Trigger represents an event that can start the workflow (v2.0)
type Trigger struct {
	Type   TriggerType       `toml:"type"`             // e.g., "cron", "file_watch", "webhook"
	Config map[string]string `toml:"config,omitempty"` // Trigger-specific configuration
}

type TriggerType string

const (
	TriggerTypeCron      TriggerType = "cron"
	TriggerTypeFileWatch TriggerType = "file_watch"
	TriggerTypeWebhook   TriggerType = "webhook"
)

// TaskDefinition represents a single task in TOML
type TaskDefinition struct {
	// Identity
	Name        string   `toml:"name"`
	Description string   `toml:"description,omitempty"`
	Type        TaskType `toml:"type,omitempty"` // "normal" | "forensic"

	// Execution
	Cmd        string            `toml:"cmd"`
	WorkingDir string            `toml:"working_dir,omitempty"`
	Env        map[string]string `toml:"env,omitempty"`
	// When true, do NOT inherit the parent process environment.
	// Overrides the per-run --clean-env flag for this specific task.
	CleanEnv bool `toml:"clean_env,omitempty"`

	// Dependencies
	DependsOn []string `toml:"depends_on,omitempty"`

	// Control Flow (v2.0)
	If     string              `toml:"if,omitempty"`     // Conditional expression
	Matrix map[string][]string `toml:"matrix,omitempty"` // Loop variables

	// Output Handling (v2.0)
	Register      string `toml:"register,omitempty"`       // Variable name to store output
	IgnoreFailure bool   `toml:"ignore_failure,omitempty"` // Continue on non-zero exit

	// Resilience
	Retries    int      `toml:"retries,omitempty"`
	RetryDelay Duration `toml:"retry_delay,omitempty"`
	Timeout    Duration `toml:"timeout,omitempty"`

	// Failure Handling
	OnFailure string `toml:"on_failure,omitempty"` // Task-specific forensic trap
}

// TaskNamePattern is a regular expression for validating task names (alphanumeric, underscores, hyphens)
var TaskNamePattern = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

// TaskType defines task execution behavior
type TaskType string

const (
	TaskTypeNormal   TaskType = "normal"   // Regular execution
	TaskTypeForensic TaskType = "forensic" // Only runs on failure
)

// DAG is the immutable execution graph
type DAG struct {
	// Identity
	Name     string
	FilePath string
	Tags     []string // labels propagated from WorkflowDefinition; stored as JSON in the DB

	// Graph Structure
	Nodes     map[string]*Node
	RootNodes []*Node // Tasks with no dependencies
	LeafNodes []*Node // Tasks with no dependents

	// Execution Metadata
	GlobalTrap    *Node            // Global forensic task (optional)
	ForensicTasks map[string]*Node // Task-level forensic trap tasks, keyed by task ID
	TotalTasks    int

	// Validation State
	Validated bool
	Errors    []error
	Warnings  []string

	// Parallel Execution Analysis
	Levels [][]*Node // Topologically sorted levels for parallel execution
}

// Node represents a single executable unit in the DAG
type Node struct {
	// Identity (unique within DAG)
	ID      string // e.g., "backup-postgres" or "deploy-services[env=dev,region=us-east]"
	TaskDef *TaskDefinition

	// Matrix Expansion
	IsExpanded bool
	MatrixVars map[string]string // Values for this specific expansion

	// Graph Edges
	Dependencies []*Node // Tasks this depends on
	Dependents   []*Node // Tasks that depend on this

	// Execution Control
	Level            int // Topological level (0 = root)
	CanRunInParallel bool

	// State (mutable during execution)
	mu              sync.RWMutex
	trapOnce        sync.Once // ensures task-level forensic trap fires at most once per run
	State           NodeState
	StartTime       time.Time
	EndTime         time.Time
	ExitCode        int
	Output          string
	Error           error
	StackTrace      string
	ConditionResult bool
}

// NodeState tracks execution status
type NodeState int

const (
	NodeStatePending NodeState = iota // Not yet started
	NodeStateReady                    // Dependencies satisfied
	NodeStateRunning                  // Currently executing
	NodeStateSuccess                  // Completed successfully
	NodeStateFailed                   // Failed execution
	NodeStateSkipped                  // Skipped due to condition
)

// GetState returns the node's current state under a read lock.
func (n *Node) GetState() NodeState {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.State
}

// MarkRunning atomically transitions the node to the running state.
func (n *Node) MarkRunning(startTime time.Time) {
	n.mu.Lock()
	n.State = NodeStateRunning
	n.StartTime = startTime
	n.mu.Unlock()
}

// MarkSuccess atomically records a successful execution result.
func (n *Node) MarkSuccess(endTime time.Time, output string, exitCode int) {
	n.mu.Lock()
	n.State = NodeStateSuccess
	n.EndTime = endTime
	n.Output = output
	n.ExitCode = exitCode
	n.mu.Unlock()
}

// MarkFailed atomically records a terminal failure.
func (n *Node) MarkFailed(endTime time.Time, err error) {
	n.mu.Lock()
	n.State = NodeStateFailed
	n.EndTime = endTime
	n.Error = err
	n.mu.Unlock()
}

// MarkSkipped atomically transitions the node to the skipped state.
func (n *Node) MarkSkipped(output string, conditionResult bool) {
	n.mu.Lock()
	n.State = NodeStateSkipped
	n.Output = output
	n.ExitCode = 0
	n.ConditionResult = conditionResult
	n.mu.Unlock()
}

// MarkEarlyFailed atomically records a pre-execution failure (condition, matrix, interpolation).
func (n *Node) MarkEarlyFailed(err error, exitCode int, stackTrace string) {
	n.mu.Lock()
	n.State = NodeStateFailed
	n.ExitCode = exitCode
	n.Error = err
	n.StackTrace = stackTrace
	n.mu.Unlock()
}

// MarkConditionMet atomically records a successful condition evaluation.
func (n *Node) MarkConditionMet(result bool) {
	n.mu.Lock()
	n.ConditionResult = result
	n.mu.Unlock()
}

// Reset atomically clears all mutable execution state, preparing the node for a new run.
func (n *Node) Reset() {
	n.mu.Lock()
	n.State = NodeStatePending
	n.StartTime = time.Time{}
	n.EndTime = time.Time{}
	n.ExitCode = 0
	n.Output = ""
	n.Error = nil
	n.StackTrace = ""
	n.ConditionResult = false
	n.trapOnce = sync.Once{} // re-arm for the new run
	n.mu.Unlock()
}

// RunTrapOnce executes fn exactly once across all concurrent callers.
// Used by the work-stealing executor to prevent double-firing of a shared
// forensic trap when multiple tasks fail simultaneously.
func (n *Node) RunTrapOnce(fn func()) {
	n.trapOnce.Do(fn)
}
