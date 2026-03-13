package contextmap

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/silocorp/workflow/internal/logger"
)

// ContextMap is the thread-safe variable registry
type ContextMap struct {
	mu        sync.RWMutex
	variables map[string]*Variable
	audit     []AuditEntry
}

// Variable represents a single stored value
type Variable struct {
	Name     string
	Value    interface{}
	Type     VarType
	SetBy    string // Task ID that registered this
	SetAt    time.Time
	ReadOnly bool // True for matrix vars
}

// VarType defines supported variable types
type VarType int

const (
	VarTypeString VarType = iota
	VarTypeInt
	VarTypeFloat
	VarTypeBool
)

// AuditEntry tracks variable mutations
type AuditEntry struct {
	Timestamp time.Time
	TaskID    string
	VarName   string
	OldValue  interface{}
	NewValue  interface{}
}

func NewContextMap() *ContextMap {
	return &ContextMap{
		variables: make(map[string]*Variable),
		audit:     make([]AuditEntry, 0),
	}
}

// Set registers or updates a variable.
// A task may overwrite a variable it previously set (e.g. on retry).
// Variables set by a different task or marked read-only cannot be overwritten.
func (cm *ContextMap) Set(taskID, name string, value interface{}) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	// Check if already exists; capture the old value for the audit trail.
	var oldValue interface{}
	if existing, exists := cm.variables[name]; exists {
		if existing.ReadOnly {
			logger.Warn("attempt to overwrite read-only variable",
				"variable", name, "set_by", existing.SetBy, "attempted_by", taskID)
			return fmt.Errorf("cannot overwrite read-only variable: %s", name)
		}
		if existing.SetBy != taskID {
			logger.Warn("variable conflict: already set by different task",
				"variable", name, "set_by", existing.SetBy, "attempted_by", taskID)
			return fmt.Errorf("variable %s already set by task %s", name, existing.SetBy)
		}
		// Same task overwriting its own variable (e.g. retry) — allowed.
		oldValue = existing.Value
		logger.Debug("variable overwritten by same task", "variable", name, "task", taskID)
	}

	// Infer type
	varType, err := cm.inferType(value)
	if err != nil {
		return err
	}

	// Store variable
	variable := &Variable{
		Name:  name,
		Value: value,
		Type:  varType,
		SetBy: taskID,
		SetAt: time.Now(),
	}
	cm.variables[name] = variable
	logger.Debug("variable set", "variable", name, "task", taskID, "type", varType)

	// Audit — oldValue is nil on first write, previous value on overwrite.
	cm.audit = append(cm.audit, AuditEntry{
		Timestamp: time.Now(),
		TaskID:    taskID,
		VarName:   name,
		OldValue:  oldValue,
		NewValue:  value,
	})

	return nil
}

// Get retrieves a variable value
func (cm *ContextMap) Get(name string) (interface{}, bool) {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	if v, exists := cm.variables[name]; exists {
		return v.Value, true
	}
	return nil, false
}

func (cm *ContextMap) Variables() []*Variable {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	var vars []*Variable
	for _, v := range cm.variables {
		vars = append(vars, v)
	}
	return vars
}

// SetMatrix registers read-only matrix variables for a task
func (cm *ContextMap) SetMatrix(taskID string, vars map[string]string) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	for name, value := range vars {
		// Matrix variables are scoped to the task
		scopedName := fmt.Sprintf("%s.%s", taskID, name)

		cm.variables[scopedName] = &Variable{
			Name:     scopedName,
			Value:    value,
			Type:     VarTypeString,
			SetBy:    taskID,
			SetAt:    time.Now(),
			ReadOnly: true,
		}
	}

	return nil
}

func (cm *ContextMap) inferType(value interface{}) (VarType, error) {
	switch value.(type) {
	case string:
		return VarTypeString, nil
	case int:
		return VarTypeInt, nil
	case float64:
		return VarTypeFloat, nil
	case bool:
		return VarTypeBool, nil
	default:
		return 0, fmt.Errorf("unsupported type: %T", value)
	}
}

// EvalCondition evaluates an "if" expression
func (cm *ContextMap) EvalCondition(expr string) (bool, error) {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	// Simple expression parser for v2.0
	// Supports: var > value, var < value, var == value, var != value

	expr = strings.TrimSpace(expr)

	// Parse operator
	var operator string
	var parts []string

	for _, op := range []string{">=", "<=", "!=", "==", ">", "<"} {
		if strings.Contains(expr, op) {
			operator = op
			parts = strings.SplitN(expr, op, 2)
			break
		}
	}

	if operator == "" {
		return false, fmt.Errorf("invalid condition: no operator found in '%s'", expr)
	}

	if len(parts) != 2 {
		return false, fmt.Errorf("invalid condition: expected 2 operands in '%s'", expr)
	}

	left := strings.TrimSpace(parts[0])
	right := strings.TrimSpace(parts[1])

	// Get variable value
	variable, exists := cm.variables[left]
	if !exists {
		return false, fmt.Errorf("undefined variable in condition: %s", left)
	}

	// Compare based on type
	switch variable.Type {
	case VarTypeInt:
		return cm.compareInt(variable.Value, right, operator)
	case VarTypeFloat:
		return cm.compareFloat(variable.Value, right, operator)
	case VarTypeString:
		return cm.compareString(variable.Value, right, operator)
	case VarTypeBool:
		return cm.compareBool(variable.Value, right, operator)
	default:
		return false, fmt.Errorf("unsupported type for comparison: %v", variable.Type)
	}
}

func (cm *ContextMap) compareInt(leftVal interface{}, rightStr, operator string) (bool, error) {
	var left int
	switch v := leftVal.(type) {
	case int:
		left = v
	case int64:
		left = int(v)
	default:
		return false, fmt.Errorf("internal: compareInt received unexpected type %T", leftVal)
	}
	right, err := strconv.Atoi(rightStr)
	if err != nil {
		return false, fmt.Errorf("cannot convert '%s' to int", rightStr)
	}

	switch operator {
	case ">":
		return left > right, nil
	case "<":
		return left < right, nil
	case ">=":
		return left >= right, nil
	case "<=":
		return left <= right, nil
	case "==":
		return left == right, nil
	case "!=":
		return left != right, nil
	}
	return false, fmt.Errorf("unknown operator: %s", operator)
}

func (cm *ContextMap) compareFloat(leftVal interface{}, rightStr, operator string) (bool, error) {
	left := leftVal.(float64)
	right, err := strconv.ParseFloat(rightStr, 64)
	if err != nil {
		return false, fmt.Errorf("cannot convert '%s' to float", rightStr)
	}

	switch operator {
	case ">":
		return left > right, nil
	case "<":
		return left < right, nil
	case ">=":
		return left >= right, nil
	case "<=":
		return left <= right, nil
	case "==":
		return left == right, nil
	case "!=":
		return left != right, nil
	}
	return false, fmt.Errorf("unknown operator: %s", operator)
}

func (cm *ContextMap) compareString(leftVal interface{}, rightStr, operator string) (bool, error) {
	left := leftVal.(string)
	right := strings.Trim(rightStr, "\"'") // Remove quotes

	switch operator {
	case "==":
		return left == right, nil
	case "!=":
		return left != right, nil
	default:
		return false, fmt.Errorf("operator %s not supported for strings", operator)
	}
}

func (cm *ContextMap) compareBool(leftVal interface{}, rightStr, operator string) (bool, error) {
	left := leftVal.(bool)
	right, err := strconv.ParseBool(rightStr)
	if err != nil {
		return false, fmt.Errorf("cannot convert '%s' to bool", rightStr)
	}

	switch operator {
	case "==":
		return left == right, nil
	case "!=":
		return left != right, nil
	default:
		return false, fmt.Errorf("operator %s not supported for booleans", operator)
	}
}

// Snapshot creates a serializable copy of the context
func (cm *ContextMap) Snapshot() ([]byte, error) {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	snapshot := struct {
		Variables map[string]*Variable
		Audit     []AuditEntry
		Timestamp time.Time
	}{
		Variables: cm.variables,
		Audit:     cm.audit,
		Timestamp: time.Now(),
	}

	return json.Marshal(snapshot)
}

// RestoreVariable bypasses ownership checks and directly inserts a variable.
// Used during resume to restore previously-persisted context snapshots without
// triggering the "variable already set by another task" guard.
func (cm *ContextMap) RestoreVariable(name string, value interface{}, varType VarType, setBy string, setAt time.Time, readOnly bool) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	cm.variables[name] = &Variable{
		Name:     name,
		Value:    value,
		Type:     varType,
		SetBy:    setBy,
		SetAt:    setAt,
		ReadOnly: readOnly,
	}
}

// Restore loads a context from a snapshot
func (cm *ContextMap) Restore(data []byte) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	var snapshot struct {
		Variables map[string]*Variable
		Audit     []AuditEntry
		Timestamp time.Time
	}

	if err := json.Unmarshal(data, &snapshot); err != nil {
		return err
	}

	cm.variables = snapshot.Variables
	cm.audit = snapshot.Audit

	return nil
}

// GetAuditTrail returns the complete variable mutation history
func (cm *ContextMap) GetAuditTrail() []AuditEntry {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	trail := make([]AuditEntry, len(cm.audit))
	copy(trail, cm.audit)
	return trail
}

// Validate checks for type consistency across the context
func (cm *ContextMap) Validate() error {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	for name, variable := range cm.variables {
		// Ensure value matches declared type
		if _, err := cm.inferType(variable.Value); err != nil {
			return fmt.Errorf("variable %s has invalid type: %w", name, err)
		}
	}

	return nil
}
