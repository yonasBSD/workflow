package dag

import "encoding/json"

// SerializableNode is a JSON-safe representation of a Node (omits sync.RWMutex and pointer cycles).
type SerializableNode struct {
	ID               string            `json:"id"`
	TaskDef          *TaskDefinition   `json:"task_def"`
	IsExpanded       bool              `json:"is_expanded"`
	MatrixVars       map[string]string `json:"matrix_vars,omitempty"`
	DependencyIDs    []string          `json:"dependency_ids,omitempty"`
	Level            int               `json:"level"`
	CanRunInParallel bool              `json:"can_run_in_parallel"`
}

// SerializableDAG is a JSON-safe representation of a DAG.
type SerializableDAG struct {
	Name       string              `json:"name"`
	FilePath   string              `json:"file_path"`
	TotalTasks int                 `json:"total_tasks"`
	GlobalTrap string              `json:"global_trap,omitempty"` // trap node ID, empty if none
	Nodes      []*SerializableNode `json:"nodes"`
}

// Serialise returns a JSON encoding of the DAG suitable for storage in dag_cache.
func (d *DAG) Serialise() ([]byte, error) {
	sd := &SerializableDAG{
		Name:       d.Name,
		FilePath:   d.FilePath,
		TotalTasks: d.TotalTasks,
	}
	if d.GlobalTrap != nil {
		sd.GlobalTrap = d.GlobalTrap.ID
	}
	for _, node := range d.Nodes {
		depIDs := make([]string, 0, len(node.Dependencies))
		for _, dep := range node.Dependencies {
			depIDs = append(depIDs, dep.ID)
		}
		sd.Nodes = append(sd.Nodes, &SerializableNode{
			ID:               node.ID,
			TaskDef:          node.TaskDef,
			IsExpanded:       node.IsExpanded,
			MatrixVars:       node.MatrixVars,
			DependencyIDs:    depIDs,
			Level:            node.Level,
			CanRunInParallel: node.CanRunInParallel,
		})
	}
	return json.Marshal(sd)
}
