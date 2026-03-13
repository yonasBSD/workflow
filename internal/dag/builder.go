package dag

import (
	"fmt"
	"sort"
	"strings"

	"github.com/silocorp/workflow/internal/contextmap"
	"github.com/silocorp/workflow/internal/logger"
)

type Builder struct {
	definition *WorkflowDefinition
	context    *contextmap.ContextMap // Variable registry
}

func NewBuilder(def *WorkflowDefinition) *Builder {
	return &Builder{
		definition: def,
		context:    contextmap.NewContextMap(),
	}
}

// Build constructs the DAG with full validation
func (b *Builder) Build() (*DAG, error) {
	logger.Debug("building DAG", "workflow", b.definition.Name)

	dag := &DAG{
		Name:          b.definition.Name,
		FilePath:      b.definition.FilePath,
		Tags:          b.definition.Tags,
		Nodes:         make(map[string]*Node),
		ForensicTasks: make(map[string]*Node),
	}

	// Expand matrix tasks into concrete nodes
	b.expandMatrices(dag)

	// Build dependency graph
	if err := b.buildEdges(dag); err != nil {
		logger.Error("dependency resolution failed", "workflow", b.definition.Name, "error", err)
		return nil, fmt.Errorf("dependency resolution failed: %w", err)
	}

	// Detect cycles
	if err := b.detectCycles(dag); err != nil {
		logger.Error("cycle detected in workflow", "workflow", b.definition.Name, "error", err)
		return nil, fmt.Errorf("circular dependency detected: %w", err)
	}

	// Topological sort + level assignment
	if err := b.assignLevels(dag); err != nil {
		logger.Error("topological sort failed", "workflow", b.definition.Name, "error", err)
		return nil, fmt.Errorf("topological sort failed: %w", err)
	}

	// Identify parallelizable clusters
	b.analyzeParallelism(dag)

	// Attach global forensic trap
	if b.definition.OnFailure != "" {
		if err := b.attachGlobalTrap(dag); err != nil {
			return nil, fmt.Errorf("global trap attachment failed: %w", err)
		}
	}

	// Final validation and warnings
	b.validate(dag)

	dag.Validated = true
	dag.TotalTasks = len(dag.Nodes)

	logger.Debug("DAG built",
		"workflow", dag.Name,
		"nodes", dag.TotalTasks, "levels", len(dag.Levels),
		"has_global_trap", dag.GlobalTrap != nil)

	return dag, nil
}

// expandMatrices converts matrix tasks into individual nodes
func (b *Builder) expandMatrices(dag *DAG) {
	for taskName, taskDef := range b.definition.Tasks {

		// Forensic tasks are not part of the normal execution flow.
		// Register them in ForensicTasks so task-level on_failure lookups work.
		if taskDef.Type == TaskTypeForensic {
			dag.ForensicTasks[taskName] = &Node{
				ID:      taskName,
				TaskDef: taskDef,
				State:   NodeStatePending,
			}
			continue
		}

		if len(taskDef.Matrix) == 0 {
			// Simple task - create single node
			node := &Node{
				ID:         taskName,
				TaskDef:    taskDef,
				IsExpanded: false,
				State:      NodeStatePending,
			}
			dag.Nodes[taskName] = node
			continue
		}

		// Matrix task - expand into multiple nodes
		expansions := b.generateMatrixExpansions(taskDef.Matrix)

		for _, expansion := range expansions {
			// Create deterministic node ID
			nodeID := b.createMatrixNodeID(taskName, expansion)

			node := &Node{
				ID:         nodeID,
				TaskDef:    taskDef,
				IsExpanded: true,
				MatrixVars: expansion,
				State:      NodeStatePending,
			}
			dag.Nodes[nodeID] = node
		}
	}

}

// generateMatrixExpansions creates all combinations deterministically
func (b *Builder) generateMatrixExpansions(matrix map[string][]string) []map[string]string {
	// Sort keys for deterministic iteration
	keys := make([]string, 0, len(matrix))
	for k := range matrix {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	// Recursive Cartesian product
	return b.cartesianProduct(keys, matrix, 0, make(map[string]string))
}

func (b *Builder) cartesianProduct(keys []string, matrix map[string][]string, index int, current map[string]string) []map[string]string {
	if index == len(keys) {
		// Base case: copy current combination
		result := make(map[string]string)
		for k, v := range current {
			result[k] = v
		}
		return []map[string]string{result}
	}

	key := keys[index]
	values := matrix[key]

	var results []map[string]string
	for _, value := range values {
		current[key] = value
		results = append(results, b.cartesianProduct(keys, matrix, index+1, current)...)
	}

	return results
}

// createMatrixNodeID generates unique, deterministic ID
func (b *Builder) createMatrixNodeID(taskName string, vars map[string]string) string {
	// Sort vars for deterministic output
	keys := make([]string, 0, len(vars))
	for k := range vars {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	id := taskName + "["
	for i, k := range keys {
		if i > 0 {
			id += ","
		}
		id += k + "=" + vars[k]
	}
	id += "]"

	return id
}

// buildEdges connects nodes based on depends_on declarations
func (b *Builder) buildEdges(dag *DAG) error {
	for _, node := range dag.Nodes {
		taskDef := node.TaskDef

		for _, depName := range taskDef.DependsOn {
			// Check if dependency is a matrix task
			if b.isMatrixTask(depName) {
				// Depend on ALL expansions of that matrix task
				for _, depNode := range dag.Nodes {
					if b.matchesMatrixBase(depNode.ID, depName) {
						node.Dependencies = append(node.Dependencies, depNode)
						depNode.Dependents = append(depNode.Dependents, node)
					}
				}
			} else {
				// Simple 1:1 dependency
				depNode, exists := dag.Nodes[depName]
				if !exists {
					return fmt.Errorf("task %s depends on undefined task %s", node.ID, depName)
				}
				node.Dependencies = append(node.Dependencies, depNode)
				depNode.Dependents = append(depNode.Dependents, node)
			}
		}
	}

	// Sort each node's Dependents for deterministic dispatch order.
	for _, node := range dag.Nodes {
		sort.Slice(node.Dependents, func(i, j int) bool { return node.Dependents[i].ID < node.Dependents[j].ID })
	}

	// Identify root nodes (no dependencies)
	for _, node := range dag.Nodes {
		if len(node.Dependencies) == 0 {
			dag.RootNodes = append(dag.RootNodes, node)
		}
	}
	sort.Slice(dag.RootNodes, func(i, j int) bool { return dag.RootNodes[i].ID < dag.RootNodes[j].ID })

	// Identify leaf nodes (no dependents)
	for _, node := range dag.Nodes {
		if len(node.Dependents) == 0 {
			dag.LeafNodes = append(dag.LeafNodes, node)
		}
	}
	sort.Slice(dag.LeafNodes, func(i, j int) bool { return dag.LeafNodes[i].ID < dag.LeafNodes[j].ID })

	return nil
}

func (b *Builder) isMatrixTask(taskName string) bool {
	taskDef, exists := b.definition.Tasks[taskName]
	return exists && len(taskDef.Matrix) > 0
}

func (b *Builder) matchesMatrixBase(nodeID, baseName string) bool {
	// Check if nodeID is an expansion of baseName
	// e.g., "deploy-services[env=dev,region=us-east]" matches "deploy-services"
	if len(nodeID) <= len(baseName) {
		return nodeID == baseName
	}
	return nodeID[:len(baseName)] == baseName && nodeID[len(baseName)] == '['
}

// detectCycles uses DFS to find circular dependencies
func (b *Builder) detectCycles(dag *DAG) error {
	visited := make(map[string]bool)
	recStack := make(map[string]bool)

	for _, node := range dag.Nodes {
		if !visited[node.ID] {
			if b.hasCycle(node, visited, recStack) {
				return fmt.Errorf("circular dependency detected involving task: %s", node.ID)
			}
		}
	}

	return nil
}

func (b *Builder) hasCycle(node *Node, visited, recStack map[string]bool) bool {
	visited[node.ID] = true
	recStack[node.ID] = true

	for _, dep := range node.Dependents {
		if !visited[dep.ID] {
			if b.hasCycle(dep, visited, recStack) {
				return true
			}
		} else if recStack[dep.ID] {
			return true
		}
	}

	recStack[node.ID] = false
	return false
}

// assignLevels performs topological sort and assigns execution levels
func (b *Builder) assignLevels(dag *DAG) error {
	// Kahn's algorithm with level tracking
	inDegree := make(map[string]int)
	for id, node := range dag.Nodes {
		inDegree[id] = len(node.Dependencies)
	}

	queue := make([]*Node, 0)
	for _, node := range dag.RootNodes {
		queue = append(queue, node)
		node.Level = 0
	}

	var levels [][]*Node
	currentLevel := 0
	processed := 0

	for len(queue) > 0 {
		// Get all nodes at current level
		levelSize := len(queue)
		levelNodes := make([]*Node, 0, levelSize)

		for i := 0; i < levelSize; i++ {
			node := queue[0]
			queue = queue[1:]

			levelNodes = append(levelNodes, node)
			processed++

			// Process dependents
			for _, dependent := range node.Dependents {
				inDegree[dependent.ID]--

				if inDegree[dependent.ID] == 0 {
					dependent.Level = currentLevel + 1
					queue = append(queue, dependent)
				}
			}
		}

		sort.Slice(levelNodes, func(i, j int) bool { return levelNodes[i].ID < levelNodes[j].ID })
		levels = append(levels, levelNodes)
		currentLevel++
	}

	if processed != len(dag.Nodes) {
		return fmt.Errorf("topological sort incomplete: possible cycle")
	}

	dag.Levels = levels
	return nil
}

// analyzeParallelism marks tasks safe for concurrent execution
func (b *Builder) analyzeParallelism(dag *DAG) {
	for _, level := range dag.Levels {
		if len(level) > 1 {
			// Multiple tasks at same level CAN run in parallel
			// But check for resource conflicts

			for _, node := range level {
				node.CanRunInParallel = b.checkParallelSafety(node)
			}
		}
	}
}

func (b *Builder) checkParallelSafety(node *Node) bool {
	// Forensic tasks always run in isolation, never concurrently.
	if node.TaskDef.Type == TaskTypeForensic {
		return false
	}
	return true
}

func (b *Builder) attachGlobalTrap(dag *DAG) error {
	trapDef, exists := b.definition.Tasks[b.definition.OnFailure]
	if !exists {
		return fmt.Errorf("global trap task %s not defined", b.definition.OnFailure)
	}

	trapNode := &Node{
		ID:         "__GLOBAL_FAILURE_TRAP__",
		TaskDef:    trapDef,
		IsExpanded: false,
		State:      NodeStatePending,
	}

	dag.GlobalTrap = trapNode

	// Connect all normal nodes to the trap on failure.
	// The trap lives in dag.GlobalTrap, not dag.Nodes, so it is
	// excluded from TotalTasks and topological levels.
	for _, node := range dag.Nodes {
		node.Dependents = append(node.Dependents, trapNode)
		trapNode.Dependencies = append(trapNode.Dependencies, node)
	}

	return nil
}

func (b *Builder) validate(d *DAG) {
	b.validateFailureHandling(d)
	b.validateMatrixExpansion(d)
}

// validateFailureHandling validates forensic task configuration and failure handling.
func (b *Builder) validateFailureHandling(d *DAG) {
	if d.GlobalTrap != nil {
		if d.GlobalTrap.TaskDef.Type != TaskTypeForensic {
			d.Warnings = append(d.Warnings, fmt.Sprintf("global on_failure task '%s' should have type='forensic'", d.GlobalTrap.ID))
		}
	}

	// Check for forensic tasks
	forensicCount := 0
	for _, node := range d.Nodes {
		if node.TaskDef.Type == TaskTypeForensic {
			forensicCount++
			// Forensic tasks should not have dependencies on normal tasks
			if len(node.Dependencies) > 0 {
				hasNormalDep := false
				for _, dep := range node.Dependencies {
					if dep.TaskDef.Type != TaskTypeForensic {
						hasNormalDep = true
						break
					}
				}
				if hasNormalDep {
					d.Warnings = append(d.Warnings, fmt.Sprintf("forensic task '%s' has dependencies on normal tasks", node.ID))
				}
			}
		}
	}
}

// validateMatrixExpansion validates matrix expansion configuration.
func (b *Builder) validateMatrixExpansion(d *DAG) {
	for _, node := range d.Nodes {
		if node.IsExpanded {
			if len(node.MatrixVars) == 0 {
				d.Warnings = append(d.Warnings, fmt.Sprintf("node '%s' marked as expanded but has no matrix variables", node.ID))
			}

			// Warn about large expansions
			estimatedExpansion := 1
			for _, v := range node.MatrixVars {
				estimatedExpansion *= len(strings.Split(v, ","))
			}
			if estimatedExpansion > 100 {
				d.Warnings = append(d.Warnings, fmt.Sprintf("node '%s' matrix expansion may be very large (~%d combinations)", node.ID, estimatedExpansion))
			}
		}
	}
}
