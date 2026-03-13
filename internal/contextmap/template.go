package contextmap

import (
	"fmt"
	"regexp"
	"strings"
)

// varPattern matches {{.varname}} placeholders in command templates.
//
// Security note: this intentionally supports only simple variable references —
// no template logic (range, if, call, define), no function invocations, and no
// access to the root context object.  The character class covers every character
// that appears in legitimate variable and matrix-expansion node IDs:
//
//	letters, digits, underscores, hyphens, dots, brackets, equals, commas
//
// Anything outside this set is not a recognised placeholder and is left as-is.
var varPattern = regexp.MustCompile(`\{\{\.([a-zA-Z0-9_.+\-\[\]=,]+)\}\}`)

// InterpolateCommand replaces {{.var}} placeholders in cmdTemplate with their
// values from the ContextMap.
//
// Only the {{.varname}} form is supported.  Attempts to use template logic
// ({{if}}, {{range}}, {{call}}, etc.) will not be interpreted — those tokens
// do not match the pattern and are emitted verbatim, which will cause the shell
// command to fail visibly rather than silently executing attacker-controlled
// template directives.
//
// An error is returned if a placeholder references a variable that does not
// exist in the map.
func (cm *ContextMap) InterpolateCommand(taskID, cmdTemplate string) (string, error) {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	// Build a flat string-value lookup from all registered variables.
	// Matrix variables are stored as "taskID.varName"; expose them under their
	// short name so that {{.framework}} resolves inside a matrix-expanded task.
	prefix := taskID + "."
	data := make(map[string]string, len(cm.variables))
	for name, variable := range cm.variables {
		data[name] = fmt.Sprintf("%v", variable.Value)
		if strings.HasPrefix(name, prefix) {
			shortName := strings.TrimPrefix(name, prefix)
			data[shortName] = fmt.Sprintf("%v", variable.Value)
		}
	}

	var firstErr error
	result := varPattern.ReplaceAllStringFunc(cmdTemplate, func(match string) string {
		sub := varPattern.FindStringSubmatch(match)
		if len(sub) < 2 {
			return match
		}
		varName := sub[1]
		val, found := data[varName]
		if !found {
			if firstErr == nil {
				firstErr = fmt.Errorf("undefined variable %q in command template", varName)
			}
			return match
		}
		return val
	})

	if firstErr != nil {
		return "", firstErr
	}
	return result, nil
}
