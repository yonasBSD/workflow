// Package security provides validation helpers that enforce the security
// model for the workflow orchestrator.
//
// Security model summary:
//   - Workflow files must reside within the configured workflows directory.
//     Names are rejected if they contain path-traversal sequences ("..") or
//     resolve outside that directory.
//   - Task commands are interpolated with a safe, logic-free substitutor.
//     No template loops, conditionals, or function calls are permitted.
//   - Environment variable keys must be valid POSIX names.  A curated deny-list
//     blocks dynamic-linker overrides (LD_PRELOAD, etc.) that could hijack
//     shared-library loading.
//   - Working directories are checked for null bytes and filesystem-level
//     traversal that could reference restricted system paths.
//   - Variable names must consist of safe characters to prevent injection
//     through the interpolation layer.
//   - Log and database files are created with mode 0600 so that sibling
//     processes owned by other users cannot read sensitive task output.
package security

import (
	"fmt"
	"path/filepath"
	"strings"
	"unicode"
)

// ErrPathTraversal is returned when a path traversal attempt is detected.
var ErrPathTraversal = fmt.Errorf("path traversal attempt")

// ErrRestrictedEnvKey is returned when an environment variable key is on the
// deny-list or does not conform to the POSIX naming rules.
var ErrRestrictedEnvKey = fmt.Errorf("restricted environment variable key")

// ValidateWorkflowName ensures that name, when resolved against workflowsDir,
// stays within workflowsDir and does not contain dangerous sequences.
//
// The function is intentionally strict:
//   - Null bytes are rejected outright.
//   - Absolute paths are rejected.
//   - Any path component equal to ".." or "." is rejected.
//   - The final resolved absolute path must be a strict descendant of
//     the absolute workflows directory.
func ValidateWorkflowName(name, workflowsDir string) error {
	clean := strings.TrimSuffix(name, ".toml")

	if strings.ContainsRune(clean, '\x00') {
		return fmt.Errorf("%w: null byte in workflow name", ErrPathTraversal)
	}

	// filepath.IsAbs returns false for Unix-style "/foo" paths on Windows,
	// so check for both the platform-native form and the Unix "/" prefix.
	if filepath.IsAbs(clean) || strings.HasPrefix(clean, "/") {
		return fmt.Errorf("%w: absolute path not allowed as workflow name", ErrPathTraversal)
	}

	// Check each path component individually before Join cleans the path.
	for _, part := range strings.Split(filepath.ToSlash(clean), "/") {
		if part == ".." || part == "." {
			return fmt.Errorf("%w: %q not allowed in workflow name", ErrPathTraversal, part)
		}
	}

	// Final containment check: the resolved path must be a descendant of the
	// workflows directory.  filepath.Join cleans the path so any remaining
	// ".." sequences are resolved here.
	absWorkflows, err := filepath.Abs(workflowsDir)
	if err != nil {
		return fmt.Errorf("cannot resolve workflows directory: %w", err)
	}
	// Ensure the prefix comparison works correctly by appending a separator.
	resolved := filepath.Join(absWorkflows, clean+".toml")
	if !strings.HasPrefix(resolved, absWorkflows+string(filepath.Separator)) {
		return fmt.Errorf("%w: resolved path %q escapes workflows directory", ErrPathTraversal, resolved)
	}

	return nil
}

// ValidateWorkingDir checks that a working-directory declaration does not
// reference null bytes or well-known restricted virtual filesystems.
//
// Existence and permission checks are left to the OS at execution time.
// This function is a static guard against obviously dangerous declarations.
func ValidateWorkingDir(dir string) error {
	if dir == "" {
		return nil
	}

	if strings.ContainsRune(dir, '\x00') {
		return fmt.Errorf("null byte in working_dir")
	}

	cleaned := filepath.Clean(dir)

	// Resolve symlinks for paths that already exist on disk so that a
	// symlink such as /tmp/kernelfs -> /proc cannot bypass the restricted
	// prefix check below.  If the path does not exist yet (created at
	// runtime by an earlier task), EvalSymlinks returns an error and we
	// fall through to the lexical check — the OS will enforce permissions
	// at execution time.
	if resolved, err := filepath.EvalSymlinks(cleaned); err == nil {
		cleaned = filepath.Clean(resolved)
	}

	// Block virtual/kernel filesystems that have no legitimate use in a
	// workflow task but are prime targets for information disclosure.
	restricted := []string{"/proc", "/sys", "/dev"}
	for _, prefix := range restricted {
		if cleaned == prefix || strings.HasPrefix(cleaned, prefix+"/") {
			return fmt.Errorf("working_dir %q references restricted system path %q", dir, prefix)
		}
	}

	return nil
}

// ValidateVariableName checks that name contains only characters safe for use
// in command interpolation: letters, digits, underscores, hyphens, dots,
// brackets, and equals signs (all appear in matrix-expanded node IDs).
func ValidateVariableName(name string) error {
	if name == "" {
		return fmt.Errorf("variable name cannot be empty")
	}
	for _, r := range name {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) &&
			r != '_' && r != '.' && r != '-' && r != '[' && r != ']' && r != '=' && r != ',' {
			return fmt.Errorf("variable name %q contains invalid character %q", name, r)
		}
	}
	return nil
}

// ValidateEnvKey enforces POSIX environment-variable naming rules and blocks
// keys that override dynamic-linker behaviour.
//
// POSIX: env-var names consist solely of uppercase letters, digits, and
// underscores, must not begin with a digit, and are conventionally upper-case
// (though lower-case keys are allowed by most implementations).  This function
// follows the permissive interpretation: any letter, digit, or underscore is
// accepted.
//
// The deny-list targets keys that redirect shared-library loading and are
// therefore capable of escalating an arbitrary-write into code execution:
//
//	LD_PRELOAD, LD_LIBRARY_PATH, LD_AUDIT, LD_DEBUG, LD_DEBUG_OUTPUT,
//	DYLD_INSERT_LIBRARIES, DYLD_LIBRARY_PATH (macOS equivalents)
func ValidateEnvKey(key string) error {
	if key == "" {
		return fmt.Errorf("%w: key cannot be empty", ErrRestrictedEnvKey)
	}

	// Must not start with a digit.
	if unicode.IsDigit(rune(key[0])) {
		return fmt.Errorf("%w: %q must not start with a digit", ErrRestrictedEnvKey, key)
	}

	for _, r := range key {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' {
			return fmt.Errorf("%w: %q contains invalid character %q (only letters, digits, and underscore are allowed)", ErrRestrictedEnvKey, key, r)
		}
	}

	// Dynamic-linker overrides: setting these allows a TOML author to inject
	// arbitrary shared libraries into any child process.
	blocked := map[string]string{
		"LD_PRELOAD":            "dynamic-linker preload override",
		"LD_LIBRARY_PATH":       "dynamic-linker library path override",
		"LD_AUDIT":              "dynamic-linker audit module override",
		"LD_DEBUG":              "dynamic-linker debug flag",
		"LD_DEBUG_OUTPUT":       "dynamic-linker debug output redirect",
		"DYLD_INSERT_LIBRARIES": "macOS dynamic-linker insert override",
		"DYLD_LIBRARY_PATH":     "macOS dynamic-linker library path override",
		"DYLD_FRAMEWORK_PATH":   "macOS dynamic-linker framework path override",
	}
	// Normalise to uppercase for the deny-list check so that lowercase
	// variants (e.g. ld_preload) are caught on libc implementations that
	// honour them case-insensitively.
	if reason, blocked := blocked[strings.ToUpper(key)]; blocked {
		return fmt.Errorf("%w: %q is blocked (%s)", ErrRestrictedEnvKey, key, reason)
	}

	return nil
}
