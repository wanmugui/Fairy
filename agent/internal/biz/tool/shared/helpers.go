package shared

import (
	"agentloop/agent/internal/dtypes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type ToolContext struct {
	Workspace   string
	SessionFile string
}

// ReadablePathRoot identifies the logical root used to resolve a read-only
// local file path. It mirrors the two sandbox roots exposed in production:
// session data and bundled skills.
type ReadablePathRoot string

const (
	ReadablePathWorkspace ReadablePathRoot = "workspace"
	ReadablePathSkills    ReadablePathRoot = "skills"
)

func ContextForInvocation(invocation dtypes.ToolInvocation) (ToolContext, error) {
	workspace := strings.TrimSpace(invocation.Workspace)
	if workspace == "" {
		return ToolContext{}, fmt.Errorf("workspace is required")
	}
	absWorkspace, err := filepath.Abs(workspace)
	if err != nil {
		return ToolContext{}, fmt.Errorf("resolve workspace: %w", err)
	}
	return ToolContext{Workspace: filepath.Clean(absWorkspace), SessionFile: invocation.SessionFile}, nil
}

func DecodeArgs(invocation dtypes.ToolInvocation) (map[string]any, error) {
	raw := strings.TrimSpace(string(invocation.Args))
	if raw == "" {
		return map[string]any{}, nil
	}
	var args map[string]any
	if err := json.Unmarshal([]byte(raw), &args); err != nil {
		return nil, fmt.Errorf("parse tool arguments: %w", err)
	}
	if args == nil {
		return map[string]any{}, nil
	}
	return args, nil
}

func ResolveWorkspacePath(workspace, requested string) (string, error) {
	workspace = strings.TrimSpace(workspace)
	requested = strings.TrimSpace(requested)
	if workspace == "" {
		return "", fmt.Errorf("workspace is required")
	}
	if requested == "" {
		return "", fmt.Errorf("path is required")
	}
	absWorkspace, err := filepath.Abs(workspace)
	if err != nil {
		return "", fmt.Errorf("resolve workspace: %w", err)
	}
	absWorkspace = filepath.Clean(absWorkspace)
	for _, prefix := range []string{"local://", "memory://", "knowledge://"} {
		if strings.HasPrefix(strings.ToLower(requested), prefix) {
			requested = requested[len(prefix):]
			break
		}
	}
	// /mnt/data is the production sandbox's workspace root.  Local process
	// tools receive the same logical paths from Skills, so accept it wherever a
	// workspace-relative path is accepted (including bash.working_dir).
	requested = strings.ReplaceAll(requested, "\\", "/")
	if relative, ok := virtualRootRelative(requested, "mnt/data"); ok {
		requested = relative
	}
	requested = filepath.FromSlash(requested)
	fullPath := requested
	if !filepath.IsAbs(fullPath) {
		fullPath = filepath.Join(absWorkspace, fullPath)
	}
	fullPath, err = filepath.Abs(filepath.Clean(fullPath))
	if err != nil {
		return "", fmt.Errorf("resolve path %q: %w", requested, err)
	}
	// Post-loosen: absolute paths are accepted as-is so the agent can reach
	// any host directory the OS user can reach. Workspace containment still
	// applies to relative paths (joined above). Destructive operations are
	// policed at the bash layer, not here.
	return fullPath, nil
}

// ResolveReadablePath resolves a path accepted by read-only local tools.
//
// Post-loosen behavior:
//   - /skills/... still maps to the configured skills root (read-only).
//   - /mnt/data/... still maps to the workspace for production-skill compat.
//   - `local://...` accepts both virtual roots plus workspace-relative.
//   - Other absolute paths are accepted unchanged; the bash policy layer
//     is responsible for blocking destructive operations.
//   - `memory://` / `knowledge://` remain unsupported by this local backend.
func ResolveReadablePath(workspace, skillsRoot, requested string) (string, ReadablePathRoot, error) {
	raw := strings.TrimSpace(requested)
	if raw == "" {
		return "", "", fmt.Errorf("path is required")
	}

	lower := strings.ToLower(raw)
	hasLocalScheme := false
	switch {
	case strings.HasPrefix(lower, "memory://"), strings.HasPrefix(lower, "knowledge://"):
		return "", "", fmt.Errorf("path %q uses an unsupported filesystem; this local runtime supports only local:// workspace files and bundled skills", requested)
	case strings.HasPrefix(lower, "local://"):
		raw = raw[len("local://"):]
		hasLocalScheme = true
	}

	// Treat protocol paths as slash-separated regardless of the host OS. This
	// accepts both production-compatible spellings: local://skills/... and
	// local:///skills/....
	raw = strings.ReplaceAll(strings.TrimSpace(raw), "\\", "/")
	if hasLocalScheme || strings.HasPrefix(raw, "/") {
		if relative, ok := virtualRootRelative(raw, "skills"); ok {
			if strings.TrimSpace(skillsRoot) == "" {
				return "", "", fmt.Errorf("path %q refers to bundled skills, but no skills directory is configured", requested)
			}
			fullPath, err := resolvePathWithinRoot(skillsRoot, relative)
			if err != nil {
				return "", "", err
			}
			return fullPath, ReadablePathSkills, nil
		}
		if relative, ok := virtualRootRelative(raw, "mnt/data"); ok {
			fullPath, err := resolvePathWithinRoot(workspace, relative)
			if err != nil {
				return "", "", err
			}
			return fullPath, ReadablePathWorkspace, nil
		}
		// Any other absolute path: accept it literally. Pick ReadablePathSkills
		// only when the resolved file actually lives under skillsRoot; the
		// workspace tag is the safe default for everything else.
		if hasLocalScheme || filepath.IsAbs(filepath.FromSlash(raw)) {
			cleaned, err := filepath.Abs(filepath.FromSlash(raw))
			if err != nil {
				return "", "", fmt.Errorf("resolve path %q: %w", raw, err)
			}
			if strings.TrimSpace(skillsRoot) != "" {
				if rel, relErr := filepath.Rel(skillsRoot, cleaned); relErr == nil &&
					rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel) {
					return cleaned, ReadablePathSkills, nil
				}
			}
			return cleaned, ReadablePathWorkspace, nil
		}
	}

	fullPath, err := resolvePathWithinRoot(workspace, raw)
	if err != nil {
		return "", "", err
	}
	return fullPath, ReadablePathWorkspace, nil
}

func virtualRootRelative(raw, root string) (string, bool) {
	trimmed := strings.TrimLeft(raw, "/")
	if trimmed == root {
		return "", true
	}
	if strings.HasPrefix(trimmed, root+"/") {
		return strings.TrimPrefix(trimmed, root+"/"), true
	}
	return "", false
}

func resolvePathWithinRoot(root, requested string) (string, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return "", fmt.Errorf("root directory is required")
	}
	if requested == "" {
		requested = "."
	}
	requested = filepath.FromSlash(requested)
	if filepath.IsAbs(requested) {
		return "", fmt.Errorf("path %q is outside allowed root", requested)
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("resolve root: %w", err)
	}
	absRoot = filepath.Clean(absRoot)
	fullPath := filepath.Clean(filepath.Join(absRoot, requested))
	relative, err := filepath.Rel(absRoot, fullPath)
	if err != nil {
		return "", fmt.Errorf("check path %q: %w", requested, err)
	}
	if relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
		return "", fmt.Errorf("path %q is outside allowed root", requested)
	}
	return fullPath, nil
}

func RelativePath(workspace, fullPath string) string {
	relative, err := filepath.Rel(workspace, fullPath)
	if err != nil {
		return filepath.ToSlash(fullPath)
	}
	return filepath.ToSlash(relative)
}

func ErrorResult(toolName string, err error) dtypes.ToolResult {
	return dtypes.ToolResult{Value: map[string]any{"tool": toolName, "error": err.Error()}, IsError: true}
}

func StringArg(args map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := args[key].(string); ok {
			return value
		}
	}
	return ""
}

func IntArg(args map[string]any, key string, defaultValue int) int {
	value, ok := args[key].(float64)
	if !ok {
		return defaultValue
	}
	return int(value)
}

func BoolArg(args map[string]any, key string) bool {
	value, _ := args[key].(bool)
	return value
}

func WriteJSONFileAtomically(path string, value any) error {
	parent := filepath.Dir(path)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return fmt.Errorf("create parent directory: %w", err)
	}
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal JSON: %w", err)
	}
	raw = append(raw, '\n')
	tmp, err := os.CreateTemp(parent, ".agent-tool-*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary JSON file: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("protect temporary JSON file: %w", err)
	}
	if _, err := tmp.Write(raw); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temporary JSON file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("sync temporary JSON file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temporary JSON file: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		if removeErr := os.Remove(path); removeErr != nil && !os.IsNotExist(removeErr) {
			return fmt.Errorf("replace JSON file: %w (remove destination: %v)", err, removeErr)
		}
		if retryErr := os.Rename(tmpPath, path); retryErr != nil {
			return fmt.Errorf("rename temporary JSON file: %w", retryErr)
		}
	}
	return nil
}
