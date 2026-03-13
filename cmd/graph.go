package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/silocorp/workflow/internal/dag"
	"github.com/silocorp/workflow/internal/logger"
	"github.com/spf13/cobra"
)

var (
	graphFormat       string
	graphDetail       bool
	graphShowStats    bool
	graphShowTriggers bool
	graphShowMatrix   bool
	graphShowForensic bool
	graphColour       bool
	graphHighlight    string
	graphExport       string
)

// GraphRenderEngine handles all graph rendering operations
type GraphRenderEngine struct {
	dag       *dag.DAG
	detailed  bool
	stats     bool
	triggers  bool
	matrix    bool
	forensic  bool
	colour    bool
	highlight string
}

// graphCmd displays the directed acyclic graph (DAG) structure of a workflow.
// Supports multiple output formats: ascii, dot (Graphviz), html, json, mermaid.
var graphCmd = &cobra.Command{
	Use:   "graph <workflow>",
	Short: "Display workflow DAG structure with visualization",
	Long: `Visualise the workflow as a directed acyclic graph in multiple formats.
Supports ASCII art, Graphviz DOT, HTML (interactive), Mermaid, and JSON output.
Can display task dependencies, execution levels, matrix expansions, failure handling, and statistics.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		workflowName := args[0]

		// Parse and build DAG
		parser := dag.NewParser(workflowName)
		definition, err := parser.Parse()
		if err != nil {
			logger.Error("failed to parse workflow", "workflow", workflowName, "error", err)
			return fmt.Errorf("failed to parse workflow %s: %w", workflowName, err)
		}

		builder := dag.NewBuilder(definition)
		d, err := builder.Build()
		if err != nil {
			logger.Error("failed to build DAG", "workflow", workflowName, "error", err)
			return fmt.Errorf("failed to build DAG for %s: %w", workflowName, err)
		}

		engine := &GraphRenderEngine{
			dag:       d,
			detailed:  graphDetail,
			stats:     graphShowStats,
			triggers:  graphShowTriggers,
			matrix:    graphShowMatrix,
			forensic:  graphShowForensic,
			colour:    graphColour,
			highlight: graphHighlight,
		}

		logger.Info("rendering workflow graph",
			"workflow", workflowName,
			"format", graphFormat,
			"nodes", len(d.Nodes),
		)

		switch graphFormat {
		case "ascii":
			return engine.renderASCII()
		case "dot":
			return engine.renderDot(graphExport)
		case "html":
			return engine.renderHTML(graphExport)
		case "mermaid":
			return engine.renderMermaid(graphExport)
		case "json":
			return engine.renderJSON()
		default:
			return fmt.Errorf("unsupported format: %s (supported: ascii, dot, html, mermaid, json)", graphFormat)
		}
	},
}

// renderASCII displays the workflow as a formatted ASCII tree with colors and details.
func (e *GraphRenderEngine) renderASCII() error {
	output := strings.Builder{}

	// Header
	output.WriteString("\n╔════════════════════════════════════════════════════════════╗\n")
	output.WriteString(fmt.Sprintf("║  Workflow: %-51s║\n", e.dag.Name))
	output.WriteString(fmt.Sprintf("║  Tasks: %-54d║\n", e.dag.TotalTasks))
	output.WriteString(fmt.Sprintf("║  Levels: %-53d║\n", len(e.dag.Levels)))
	output.WriteString("╚════════════════════════════════════════════════════════════╝\n\n")

	// Render execution levels
	if len(e.dag.Levels) > 0 {
		output.WriteString("Execution Levels (Parallelisation Strategy):\n\n")
		for levelIdx, level := range e.dag.Levels {
			output.WriteString(fmt.Sprintf("  Level %d (%d parallel task%s):\n",
				levelIdx, len(level), pluralise(len(level))))

			for _, node := range level {
				output.WriteString(e.formatNodeLine(node, "    "))
			}

			if levelIdx < len(e.dag.Levels)-1 {
				output.WriteString("    ↓\n")
			}
			output.WriteString("\n")
		}
	}

	// Render root to leaf paths
	output.WriteString("Critical Path(s):\n\n")
	paths := e.findCriticalPaths()
	for i, path := range paths {
		output.WriteString(fmt.Sprintf("  Path %d:\n", i+1))
		for j, node := range path {
			prefix := "    "
			if j > 0 {
				prefix = "    → "
			}
			output.WriteString(prefix + e.colourise(node.ID, getNodeColour(node)) + "\n")
		}
		output.WriteString("\n")
	}

	// Forensic tasks
	if e.forensic {
		forensicTasks := e.findForensicTasks()
		if len(forensicTasks) > 0 {
			output.WriteString("Forensic Tasks (Failure Handlers):\n\n")
			for _, node := range forensicTasks {
				output.WriteString(fmt.Sprintf("  🔴 %s [%s]\n", node.ID, node.TaskDef.OnFailure))
				if e.detailed && node.TaskDef.Description != "" {
					output.WriteString(fmt.Sprintf("     %s\n", node.TaskDef.Description))
				}
			}
			output.WriteString("\n")
		}
	}

	// Detailed information
	if e.detailed {
		output.WriteString(e.renderDetailedNodeInfo())
	}

	// Statistics
	if e.stats {
		output.WriteString(e.renderStatistics())
	}

	// Trigger information
	if e.triggers && e.dag.Nodes != nil {
		output.WriteString(e.renderTriggerInfo())
	}

	// Matrix information
	if e.matrix {
		matrixNodes := e.findMatrixNodes()
		if len(matrixNodes) > 0 {
			output.WriteString("Matrix Expanded Tasks:\n\n")
			for _, node := range matrixNodes {
				output.WriteString(fmt.Sprintf("  ◈ %s\n", node.ID))
				output.WriteString(fmt.Sprintf("     Variables: %v\n", node.MatrixVars))
			}
			output.WriteString("\n")
		}
	}

	fmt.Print(output.String())
	return nil
}

// renderDot generates Graphviz DOT format with enhanced styling and features.
func (e *GraphRenderEngine) renderDot(exportPath string) error {
	output := strings.Builder{}

	output.WriteString("digraph ")
	output.WriteString(sanitiseName(e.dag.Name))
	output.WriteString(" {\n")

	// Graph styling
	output.WriteString("  graph [rankdir=LR, splines=ortho, nodesep=0.5, ranksep=1.0];\n")
	output.WriteString("  node [shape=box, style=\"rounded,filled\", fontname=\"Helvetica\"];\n")
	output.WriteString("  edge [fontname=\"Helvetica\", color=\"#666666\"];\n\n")

	// Define styles for different node types
	output.WriteString("  // Node style definitions\n")
	output.WriteString("  node_normal [fillcolor=\"#4A90E2\", fontcolor=\"white\"];\n")
	output.WriteString("  node_forensic [fillcolor=\"#E24A4A\", fontcolor=\"white\"];\n")
	output.WriteString("  node_matrix [fillcolor=\"#F5A623\", fontcolor=\"white\"];\n")
	output.WriteString("  node_root [fillcolor=\"#7ED321\", fontcolor=\"white\"];\n")
	output.WriteString("  node_leaf [fillcolor=\"#9013FE\", fontcolor=\"white\"];\n\n")

	for _, level := range e.dag.Levels {
		output.WriteString("  { rank=same;")
		for _, node := range level {
			output.WriteString(fmt.Sprintf(" %q;", node.ID))
		}
		output.WriteString(" }\n")
	}

	output.WriteString("\n  // Nodes\n")
	for _, node := range e.dag.Nodes {
		var nodeStyle string
		label := node.ID

		// Determine node colour and style
		switch {
		case node.TaskDef.Type == dag.TaskTypeForensic:
			nodeStyle = "node_forensic"
			label = "🔴 " + label
		case node.IsExpanded:
			nodeStyle = "node_matrix"
			label = "◈ " + label
		case len(node.Dependencies) == 0:
			nodeStyle = "node_root"
			label = "⬤ " + label
		case len(node.Dependents) == 0:
			nodeStyle = "node_leaf"
			label = "▼ " + label
		default:
			nodeStyle = "node_normal"
		}

		// Add task information to tooltip
		tooltip := fmt.Sprintf("Command: %s", strings.TrimSpace(node.TaskDef.Cmd))
		if node.TaskDef.Timeout > 0 {
			tooltip += fmt.Sprintf("\\nTimeout: %v", node.TaskDef.Timeout)
		}
		if node.TaskDef.Retries > 0 {
			tooltip += fmt.Sprintf("\\nRetries: %d", node.TaskDef.Retries)
		}

		output.WriteString(fmt.Sprintf("  %q [label=%q, %s, tooltip=%q];\n",
			node.ID, label, nodeStyle, tooltip))
	}

	output.WriteString("\n  // Edges\n")
	for _, node := range e.dag.Nodes {
		for _, dep := range node.Dependencies {
			style := ""
			if node.TaskDef.If != "" {
				style = ", style=dashed"
			}
			output.WriteString(fmt.Sprintf("  %q -> %q%s;\n", dep.ID, node.ID, style))
		}
	}

	// Global forensic trap
	if e.dag.GlobalTrap != nil {
		output.WriteString("\n  // Global failure handler\n")
		for _, leaf := range e.dag.LeafNodes {
			output.WriteString(fmt.Sprintf("  %q -> %q [style=dotted, color=%q, label=%q];\n",
				leaf.ID, e.dag.GlobalTrap.ID, "#E24A4A", "on_failure"))
		}
	}

	output.WriteString("}\n")

	// Print to stdout
	fmt.Print(output.String())

	// Export to file if requested
	if exportPath != "" {
		if err := os.WriteFile(exportPath, []byte(output.String()), 0644); err != nil {
			logger.Error("failed to export DOT file", "path", exportPath, "error", err)
			return fmt.Errorf("failed to export DOT file: %w", err)
		}
		fmt.Fprintf(os.Stderr, "\n✓ DOT file exported to: %s\n", exportPath)
		fmt.Fprintf(os.Stderr, "  Visualise with: dot -Tpng %s -o %s.png\n", exportPath, exportPath)
	}

	return nil
}

// renderHTML generates an interactive HTML visualization using D3.js/similar.
func (e *GraphRenderEngine) renderHTML(exportPath string) error {
	htmlContent := e.generateHTMLVisualization()

	output := exportPath
	if output == "" {
		output = fmt.Sprintf("%s_graph.html", e.dag.Name)
	}

	if err := os.WriteFile(output, []byte(htmlContent), 0644); err != nil {
		logger.Error("failed to export HTML file", "path", output, "error", err)
		return fmt.Errorf("failed to export HTML file: %w", err)
	}

	fmt.Printf("✓ Interactive HTML graph exported to: %s\n", output)
	fmt.Printf("  Open in your browser to interact with the visualization\n")
	logger.Info("HTML graph generated", "path", output)

	return nil
}

// renderMermaid generates Mermaid diagram format (markdown compatible).
func (e *GraphRenderEngine) renderMermaid(exportPath string) error {
	output := strings.Builder{}

	output.WriteString("```mermaid\ngraph TD\n")

	// Define node styles
	for _, node := range e.dag.Nodes {
		var icon, colour string

		switch {
		case node.TaskDef.Type == dag.TaskTypeForensic:
			icon = "🔴"
			colour = "#E24A4A"
		case node.IsExpanded:
			icon = "◈"
			colour = "#F5A623"
		case len(node.Dependencies) == 0:
			icon = "⬤"
			colour = "#7ED321"
		case len(node.Dependents) == 0:
			icon = "▼"
			colour = "#9013FE"
		default:
			icon = "•"
			colour = "#4A90E2"
		}

		output.WriteString(fmt.Sprintf("    %s[\"%s %s\"]\n",
			sanitiseName(node.ID), icon, node.ID))
		output.WriteString(fmt.Sprintf("    style %s fill:%s,color:#fff,stroke:#333\n",
			sanitiseName(node.ID), colour))
	}

	// Add edges
	output.WriteString("\n")
	for _, node := range e.dag.Nodes {
		for _, dep := range node.Dependencies {
			if node.TaskDef.If != "" {
				output.WriteString(fmt.Sprintf("    %s -->|conditional| %s\n",
					sanitiseName(dep.ID), sanitiseName(node.ID)))
			} else {
				output.WriteString(fmt.Sprintf("    %s --> %s\n",
					sanitiseName(dep.ID), sanitiseName(node.ID)))
			}
		}
	}

	// Global forensic
	if e.dag.GlobalTrap != nil {
		for _, leaf := range e.dag.LeafNodes {
			output.WriteString(fmt.Sprintf("    %s -->|failure| %s\n",
				sanitiseName(leaf.ID), sanitiseName(e.dag.GlobalTrap.ID)))
		}
	}

	output.WriteString("```\n")

	mermaidOutput := output.String()

	if exportPath != "" {
		mdContent := fmt.Sprintf("# Workflow: %s\n\n%s\n", e.dag.Name, mermaidOutput)
		if err := os.WriteFile(exportPath, []byte(mdContent), 0644); err != nil {
			logger.Error("failed to export Mermaid file", "path", exportPath, "error", err)
			return fmt.Errorf("failed to export Mermaid file: %w", err)
		}
		fmt.Printf("✓ Mermaid diagram exported to: %s\n", exportPath)
	} else {
		fmt.Print(mermaidOutput)
	}

	return nil
}

// renderJSON outputs the complete workflow structure as JSON.
func (e *GraphRenderEngine) renderJSON() error {
	type nodeJSON struct {
		ID             string            `json:"id"`
		TaskName       string            `json:"task_name"`
		Command        string            `json:"command"`
		Type           string            `json:"type"`
		Level          int               `json:"level"`
		Dependencies   []string          `json:"dependencies,omitempty"`
		Dependents     []string          `json:"dependents,omitempty"`
		IsExpanded     bool              `json:"is_expanded,omitempty"`
		MatrixVars     map[string]string `json:"matrix_vars,omitempty"`
		Timeout        string            `json:"timeout,omitempty"`
		Retries        int               `json:"retries,omitempty"`
		IsForensic     bool              `json:"is_forensic,omitempty"`
		CanParallelise bool              `json:"can_parallelise,omitempty"`
	}

	type levelJSON struct {
		LevelIndex     int      `json:"level_index"`
		Tasks          []string `json:"tasks"`
		Parallelisable int      `json:"parallelisable_count"`
	}

	type dagJSON struct {
		Name             string      `json:"name"`
		TotalTasks       int         `json:"total_tasks"`
		ExecutionLevels  []levelJSON `json:"execution_levels"`
		Nodes            []nodeJSON  `json:"nodes"`
		RootNodeIDs      []string    `json:"root_nodes,omitempty"`
		LeafNodeIDs      []string    `json:"leaf_nodes,omitempty"`
		GlobalTrap       string      `json:"global_trap,omitempty"`
		HasForensicTasks bool        `json:"has_forensic_tasks"`
		HasMatrixTasks   bool        `json:"has_matrix_tasks"`
	}

	var nodes []nodeJSON
	rootIDs := []string{}
	leafIDs := []string{}

	for _, node := range e.dag.Nodes {
		depIDs := make([]string, len(node.Dependencies))
		for i, dep := range node.Dependencies {
			depIDs[i] = dep.ID
		}

		dependentIDs := make([]string, len(node.Dependents))
		for i, dep := range node.Dependents {
			dependentIDs[i] = dep.ID
		}

		nodeJSON := nodeJSON{
			ID:             node.ID,
			TaskName:       node.TaskDef.Name,
			Command:        node.TaskDef.Cmd,
			Type:           string(node.TaskDef.Type),
			Level:          node.Level,
			Dependencies:   depIDs,
			Dependents:     dependentIDs,
			IsExpanded:     node.IsExpanded,
			MatrixVars:     node.MatrixVars,
			Retries:        node.TaskDef.Retries,
			IsForensic:     node.TaskDef.Type == dag.TaskTypeForensic,
			CanParallelise: node.CanRunInParallel,
		}

		if node.TaskDef.Timeout > 0 {
			nodeJSON.Timeout = node.TaskDef.Timeout.String()
		}

		nodes = append(nodes, nodeJSON)
	}

	for _, node := range e.dag.RootNodes {
		rootIDs = append(rootIDs, node.ID)
	}
	for _, node := range e.dag.LeafNodes {
		leafIDs = append(leafIDs, node.ID)
	}

	var levels []levelJSON
	for levelIdx, level := range e.dag.Levels {
		taskIDs := make([]string, len(level))
		for i, node := range level {
			taskIDs[i] = node.ID
		}
		parallelCount := 0
		if len(level) > 1 {
			parallelCount = len(level)
		}
		levels = append(levels, levelJSON{
			LevelIndex:     levelIdx,
			Tasks:          taskIDs,
			Parallelisable: parallelCount,
		})
	}

	globalTrap := ""
	if e.dag.GlobalTrap != nil {
		globalTrap = e.dag.GlobalTrap.ID
	}

	result := dagJSON{
		Name:             e.dag.Name,
		TotalTasks:       e.dag.TotalTasks,
		ExecutionLevels:  levels,
		Nodes:            nodes,
		RootNodeIDs:      rootIDs,
		LeafNodeIDs:      leafIDs,
		GlobalTrap:       globalTrap,
		HasForensicTasks: len(e.findForensicTasks()) > 0,
		HasMatrixTasks:   len(e.findMatrixNodes()) > 0,
	}

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(result)
}

// ============ Helper Methods ============

// formatNodeLine formats a single node line for display
func (e *GraphRenderEngine) formatNodeLine(node *dag.Node, indent string) string {
	line := indent

	switch {
	case node.TaskDef.Type == dag.TaskTypeForensic:
		line += "🔴 "
	case node.IsExpanded:
		line += "◈ "
	case len(node.Dependencies) == 0:
		line += "⬤ "
	case len(node.Dependents) == 0:
		line += "▼ "
	default:
		line += "• "
	}

	line += e.colourise(node.ID, getNodeColour(node))

	if e.detailed {
		line += fmt.Sprintf(" [%s]", node.TaskDef.Cmd[:min(40, len(node.TaskDef.Cmd))])
		if node.TaskDef.Timeout > 0 {
			line += fmt.Sprintf(" ⏱️ %v", node.TaskDef.Timeout)
		}
	}

	return line
}

// renderDetailedNodeInfo renders detailed information for all nodes
func (e *GraphRenderEngine) renderDetailedNodeInfo() string {
	output := strings.Builder{}

	output.WriteString("\n" + strings.Repeat("=", 62) + "\n")
	output.WriteString("DETAILED TASK INFORMATION\n")
	output.WriteString(strings.Repeat("=", 62) + "\n\n")

	// Sort nodes by level for consistent output
	type nodeWithIndex struct {
		index int
		node  *dag.Node
	}

	var indexedNodes []nodeWithIndex
	for _, node := range e.dag.Nodes {
		indexedNodes = append(indexedNodes, nodeWithIndex{0, node})
	}

	sort.Slice(indexedNodes, func(i, j int) bool {
		if indexedNodes[i].node.Level != indexedNodes[j].node.Level {
			return indexedNodes[i].node.Level < indexedNodes[j].node.Level
		}
		return indexedNodes[i].node.ID < indexedNodes[j].node.ID
	})

	for i, ni := range indexedNodes {
		node := ni.node
		output.WriteString(fmt.Sprintf("[%d] %s\n", i+1, node.ID))
		output.WriteString(fmt.Sprintf("    Type:      %s\n", node.TaskDef.Type))
		output.WriteString(fmt.Sprintf("    Level:     %d\n", node.Level))
		output.WriteString(fmt.Sprintf("    Command:   %s\n", node.TaskDef.Cmd))

		if node.TaskDef.Description != "" {
			output.WriteString(fmt.Sprintf("    Desc:      %s\n", node.TaskDef.Description))
		}

		if len(node.Dependencies) > 0 {
			depNames := make([]string, len(node.Dependencies))
			for i, dep := range node.Dependencies {
				depNames[i] = dep.ID
			}
			output.WriteString(fmt.Sprintf("    Depends:   %v\n", depNames))
		}

		if node.TaskDef.Timeout > 0 {
			output.WriteString(fmt.Sprintf("    Timeout:   %v\n", node.TaskDef.Timeout))
		}

		if node.TaskDef.Retries > 0 {
			output.WriteString(fmt.Sprintf("    Retries:   %d\n", node.TaskDef.Retries))
			if node.TaskDef.RetryDelay > 0 {
				output.WriteString(fmt.Sprintf("    Retry Delay: %v\n", node.TaskDef.RetryDelay))
			}
		}

		if node.TaskDef.If != "" {
			output.WriteString(fmt.Sprintf("    If:        %s\n", node.TaskDef.If))
		}

		if len(node.MatrixVars) > 0 {
			output.WriteString(fmt.Sprintf("    Matrix:    %v\n", node.MatrixVars))
		}

		if len(node.TaskDef.Env) > 0 {
			output.WriteString(fmt.Sprintf("    Env Vars:  %d\n", len(node.TaskDef.Env)))
		}

		output.WriteString("\n")
	}

	return output.String()
}

// renderStatistics renders execution statistics
func (e *GraphRenderEngine) renderStatistics() string {
	output := strings.Builder{}

	output.WriteString("\n" + strings.Repeat("=", 62) + "\n")
	output.WriteString("WORKFLOW STATISTICS\n")
	output.WriteString(strings.Repeat("=", 62) + "\n\n")

	totalDependencies := 0
	totalRetries := 0
	totalTimeout := time.Duration(0)
	tasksWithConditions := 0
	tasksWithEnv := 0

	for _, node := range e.dag.Nodes {
		totalDependencies += len(node.Dependencies)
		totalRetries += node.TaskDef.Retries
		totalTimeout += time.Duration(node.TaskDef.Timeout)
		if node.TaskDef.If != "" {
			tasksWithConditions++
		}
		if len(node.TaskDef.Env) > 0 {
			tasksWithEnv += len(node.TaskDef.Env)
		}
	}

	parallelisable := 0
	for _, level := range e.dag.Levels {
		if len(level) > 1 {
			parallelisable += len(level)
		}
	}

	output.WriteString(fmt.Sprintf("Total Nodes:              %d\n", len(e.dag.Nodes)))
	output.WriteString(fmt.Sprintf("Execution Levels:        %d\n", len(e.dag.Levels)))
	output.WriteString(fmt.Sprintf("Root Nodes:              %d\n", len(e.dag.RootNodes)))
	output.WriteString(fmt.Sprintf("Leaf Nodes:              %d\n", len(e.dag.LeafNodes)))
	output.WriteString(fmt.Sprintf("Parallelisable Tasks:    %d\n", parallelisable))
	output.WriteString(fmt.Sprintf("Total Dependencies:      %d\n", totalDependencies))
	output.WriteString(fmt.Sprintf("Total Retries:           %d\n", totalRetries))
	output.WriteString(fmt.Sprintf("Total Timeout:           %v\n", totalTimeout))
	output.WriteString(fmt.Sprintf("Tasks with Conditions:   %d\n", tasksWithConditions))
	output.WriteString(fmt.Sprintf("Total Env Variables:     %d\n", tasksWithEnv))
	output.WriteString(fmt.Sprintf("Forensic Tasks:          %d\n", len(e.findForensicTasks())))
	output.WriteString(fmt.Sprintf("Matrix Expanded:         %d\n", len(e.findMatrixNodes())))

	if len(e.dag.Levels) > 1 {
		avgTasksPerLevel := float64(len(e.dag.Nodes)) / float64(len(e.dag.Levels))
		output.WriteString(fmt.Sprintf("Avg Tasks/Level:         %.1f\n", avgTasksPerLevel))
	}

	output.WriteString("\n")
	return output.String()
}

// renderTriggerInfo renders trigger configuration information
func (e *GraphRenderEngine) renderTriggerInfo() string {
	output := strings.Builder{}
	output.WriteString("Trigger Configuration:\n\n")
	output.WriteString("(Trigger information would come from DAG definition)\n\n")
	return output.String()
}

// findCriticalPaths finds the longest execution paths in the DAG.
func (e *GraphRenderEngine) findCriticalPaths() [][]*dag.Node {
	var paths [][]*dag.Node

	for _, root := range e.dag.RootNodes {
		paths = append(paths, e.tracePath(root))
	}

	// Sort by length, longest first.
	sort.Slice(paths, func(i, j int) bool {
		return len(paths[i]) > len(paths[j])
	})

	if len(paths) > 3 {
		paths = paths[:3]
	}

	return paths
}

// tracePath traces the longest path from node to a leaf.
func (e *GraphRenderEngine) tracePath(node *dag.Node) []*dag.Node {
	path := []*dag.Node{node}

	if len(node.Dependents) == 0 {
		return path
	}

	var longestSub []*dag.Node
	for _, dep := range node.Dependents {
		sub := e.tracePath(dep)
		if len(sub) > len(longestSub) {
			longestSub = sub
		}
	}

	return append(path, longestSub...)
}

// findForensicTasks finds all forensic (failure handler) tasks
func (e *GraphRenderEngine) findForensicTasks() []*dag.Node {
	var forensic []*dag.Node
	for _, node := range e.dag.Nodes {
		if node.TaskDef.Type == dag.TaskTypeForensic {
			forensic = append(forensic, node)
		}
	}
	return forensic
}

// findMatrixNodes finds all matrix-expanded tasks
func (e *GraphRenderEngine) findMatrixNodes() []*dag.Node {
	var matrixNodes []*dag.Node
	for _, node := range e.dag.Nodes {
		if node.IsExpanded {
			matrixNodes = append(matrixNodes, node)
		}
	}
	return matrixNodes
}

// generateHTMLVisualization generates complete HTML with inline visualization
func (e *GraphRenderEngine) generateHTMLVisualization() string {
	// Create node data
	type nodeData struct {
		ID    string
		Label string
		Level int
		Type  string
	}

	var nodes []nodeData
	var edges []struct {
		Source string
		Target string
	}

	for _, node := range e.dag.Nodes {
		icon := "●"
		switch {
		case node.TaskDef.Type == dag.TaskTypeForensic:
			icon = "🔴"
		case node.IsExpanded:
			icon = "◈"
		case len(node.Dependencies) == 0:
			icon = "⬤"
		case len(node.Dependents) == 0:
			icon = "▼"
		}

		nodes = append(nodes, nodeData{
			ID:    node.ID,
			Label: icon + " " + node.ID,
			Level: node.Level,
			Type:  string(node.TaskDef.Type),
		})

		for _, dep := range node.Dependencies {
			edges = append(edges, struct {
				Source string
				Target string
			}{dep.ID, node.ID})
		}
	}

	nodesJSON, _ := json.Marshal(nodes)
	edgesJSON, _ := json.Marshal(edges)

	html := fmt.Sprintf(`<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Workflow Graph: %s</title>
    <script src="https://d3js.org/d3.v7.min.js"></script>
    <style>
        * { margin: 0; padding: 0; box-sizing: border-box; }
        body {
            font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif;
            background: linear-gradient(135deg, #667eea 0%%, #764ba2 100%%);
            min-height: 100vh;
            padding: 20px;
        }
        .container {
            max-width: 1600px;
            margin: 0 auto;
            background: white;
            border-radius: 12px;
            box-shadow: 0 20px 60px rgba(0,0,0,0.3);
            overflow: hidden;
        }
        .header {
            background: linear-gradient(135deg, #667eea 0%%, #764ba2 100%%);
            color: white;
            padding: 30px;
        }
        .header h1 { font-size: 2em; margin-bottom: 10px; }
        .header p { opacity: 0.9; }
        #graph {
            width: 100%%;
            height: 600px;
            background: #f8f9fa;
            border-bottom: 1px solid #e0e0e0;
        }
        .stats {
            display: grid;
            grid-template-columns: repeat(auto-fit, minmax(200px, 1fr));
            gap: 20px;
            padding: 30px;
            background: #f8f9fa;
        }
        .stat-box {
            background: white;
            padding: 20px;
            border-radius: 8px;
            border-left: 4px solid #667eea;
            box-shadow: 0 2px 8px rgba(0,0,0,0.1);
        }
        .stat-box h3 { color: #667eea; font-size: 0.9em; text-transform: uppercase; margin-bottom: 8px; }
        .stat-box .value { font-size: 2em; font-weight: bold; color: #333; }
        .node { cursor: pointer; }
        .node circle { stroke: #fff; stroke-width: 2px; }
        .node:hover circle { stroke-width: 3px; }
        .link { stroke: #999; stroke-opacity: 0.6; }
        .tooltip {
            position: absolute;
            background: rgba(0,0,0,0.8);
            color: white;
            padding: 8px 12px;
            border-radius: 4px;
            font-size: 12px;
            pointer-events: none;
            display: none;
            z-index: 1000;
        }
    </style>
</head>
<body>
    <div class="container">
        <div class="header">
            <h1>Workflow DAG: %s</h1>
            <p>Interactive visualization of workflow dependencies and execution levels</p>
        </div>
        <div id="graph"></div>
        <div class="stats">
            <div class="stat-box">
                <h3>Total Tasks</h3>
                <div class="value">%d</div>
            </div>
            <div class="stat-box">
                <h3>Execution Levels</h3>
                <div class="value">%d</div>
            </div>
            <div class="stat-box">
                <h3>Root Nodes</h3>
                <div class="value">%d</div>
            </div>
            <div class="stat-box">
                <h3>Leaf Nodes</h3>
                <div class="value">%d</div>
            </div>
        </div>
    </div>
    <div class="tooltip" id="tooltip"></div>
    <script>
        const nodes = %s;
        const edges = %s;

        // Build links
        const links = edges.map(e => ({
            source: nodes.find(n => n.ID === e.Source),
            target: nodes.find(n => n.ID === e.Target)
        }));

        // Create simulation
        const width = document.getElementById('graph').clientWidth;
        const height = 600;

        const simulation = d3.forceSimulation(nodes)
            .force("link", d3.forceLink(links).id(d => d.ID).distance(100))
            .force("charge", d3.forceManyBody().strength(-300))
            .force("center", d3.forceCenter(width / 2, height / 2))
            .force("y", d3.forceY().y(d => d.Level * 100));

        const svg = d3.select("#graph").append("svg")
            .attr("width", width)
            .attr("height", height);

        const link = svg.selectAll(".link")
            .data(links)
            .enter().append("line")
            .attr("class", "link");

        const node = svg.selectAll(".node")
            .data(nodes)
            .enter().append("g")
            .attr("class", "node")
            .call(d3.drag()
                .on("start", dragStarted)
                .on("drag", dragged)
                .on("end", dragEnded));

        node.append("circle")
            .attr("r", 8)
            .attr("fill", d => {
                switch(d.Type) {
                    case "forensic": return "#E24A4A";
                    default: return "#4A90E2";
                }
            });

        node.append("text")
            .attr("text-anchor", "middle")
            .attr("dy", "0.3em")
            .attr("font-size", "10px")
            .attr("fill", "white")
            .text(d => d.Label[0]);

        node.on("mouseover", function(event, d) {
            const tooltip = document.getElementById('tooltip');
            tooltip.innerHTML = d.Label;
            tooltip.style.display = 'block';
            tooltip.style.left = (event.pageX + 10) + 'px';
            tooltip.style.top = (event.pageY + 10) + 'px';
        }).on("mouseout", function() {
            document.getElementById('tooltip').style.display = 'none';
        });

        simulation.on("tick", () => {
            link
                .attr("x1", d => d.source.x)
                .attr("y1", d => d.source.y)
                .attr("x2", d => d.target.x)
                .attr("y2", d => d.target.y);

            node
                .attr("transform", d => 'translate(' + d.x + ',' + d.y + ')');
        });

        function dragStarted(event, d) {
            if (!event.active) simulation.alphaTarget(0.3).restart();
            d.fx = d.x;
            d.fy = d.y;
        }

        function dragged(event, d) {
            d.fx = event.x;
            d.fy = event.y;
        }

        function dragEnded(event, d) {
            if (!event.active) simulation.alphaTarget(0);
            d.fx = null;
            d.fy = null;
        }
    </script>
</body>
</html>`, e.dag.Name, e.dag.Name, e.dag.TotalTasks, len(e.dag.Levels),
		len(e.dag.RootNodes), len(e.dag.LeafNodes), string(nodesJSON), string(edgesJSON))

	return html
}

// ============ Utility Functions ============

func colourise(text string, colour string) string {
	if !graphColour {
		return text
	}

	colourCodes := map[string]string{
		"red":    "\033[91m",
		"green":  "\033[92m",
		"yellow": "\033[93m",
		"blue":   "\033[94m",
		"purple": "\033[95m",
		"cyan":   "\033[96m",
		"reset":  "\033[0m",
	}

	return colourCodes[colour] + text + colourCodes["reset"]
}

func (e *GraphRenderEngine) colourise(text string, colour string) string {
	if !e.colour {
		return text
	}
	return colourise(text, colour)
}

func getNodeColour(node *dag.Node) string {
	switch {
	case node.TaskDef.Type == dag.TaskTypeForensic:
		return "red"
	case node.IsExpanded:
		return "yellow"
	case len(node.Dependencies) == 0:
		return "green"
	case len(node.Dependents) == 0:
		return "purple"
	default:
		return "blue"
	}
}

func sanitiseName(name string) string {
	var result []rune
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' {
			result = append(result, r)
		} else {
			result = append(result, '_')
		}
	}
	return string(result)
}

func pluralise(count int) string {
	if count == 1 {
		return ""
	}
	return "s"
}

func init() {
	rootCmd.AddCommand(graphCmd)

	graphCmd.Flags().StringVarP(&graphFormat, "format", "f", "ascii", "Output format: ascii, dot (Graphviz), html (interactive), mermaid, or json")
	graphCmd.Flags().BoolVarP(&graphDetail, "detail", "d", false, "Show detailed task information")
	graphCmd.Flags().BoolVar(&graphShowStats, "stats", false, "Show workflow statistics")
	graphCmd.Flags().BoolVar(&graphShowTriggers, "triggers", false, "Show trigger configuration")
	graphCmd.Flags().BoolVar(&graphShowMatrix, "matrix", false, "Show matrix-expanded tasks")
	graphCmd.Flags().BoolVar(&graphShowForensic, "forensic", false, "Show forensic/failure-handler tasks")
	graphCmd.Flags().BoolVar(&graphColour, "colour", true, "Use coloured output (disable with --colour=false)")
	graphCmd.Flags().StringVar(&graphHighlight, "highlight", "", "Highlight a specific task or dependency path")
	graphCmd.Flags().StringVarP(&graphExport, "export", "o", "", "Export to file (format determined by extension or --format)")
}
