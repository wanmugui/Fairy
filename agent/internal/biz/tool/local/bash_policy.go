package local

import (
	"fmt"
	"strings"
)

// BashPolicy is the policy layer applied to every bash tool invocation. It is
// intentionally simple: a whitelist of command names (first token), a
// blacklist of command names, and a blacklist of path prefixes that should
// never appear as command arguments. Models can request an escalation by
// passing `escalate: true` in the tool arguments; that records a policy
// override in the result and lets the command through deny rules.
type BashPolicy struct {
	// AllowCommands is the whitelist. When non-empty, the FIRST token of the
	// command must match one of these names (case-insensitive). An empty list
	// disables the whitelist — every command passes this check.
	AllowCommands []string
	// DenyCommands is the blacklist. Each entry is matched against the first
	// token of the command (case-insensitive, exact match). Escalation can
	// bypass denylist.
	DenyCommands []string
	// DenyPaths is a list of absolute path prefixes (Windows: `C:\Windows`
	// or POSIX: `/etc`). If the command line contains any of these as
	// arguments, the command is denied. Use the absolute form; matching is
	// case-insensitive on Windows.
	DenyPaths []string
}

// DefaultBashPolicy returns a conservative policy: dangerous commands blocked,
// well-known paths protected, no whitelist (so common dev tools work).
func DefaultBashPolicy() BashPolicy {
	return BashPolicy{
		AllowCommands: nil, // open by default; uncomment to enforce whitelist
		DenyCommands: []string{
			// Format / disk wipe
			"format", "diskpart", "fdisk", "mkfs", "dd",
			// Privilege escalation
			"sudo", "su", "runas",
			// System-level shutdown
			"shutdown", "reboot", "halt", "poweroff", "init",
			// Network-config destroy
			"netsh", "iptables",
		},
		DenyPaths: []string{
			// Windows system directories
			`C:\Windows`,
			`C:\Windows\System32`,
			`C:\Program Files`,
			`C:\Program Files (x86)`,
			// Boot / system
			`/etc`,
			`/boot`,
			`/usr/bin`,
			`/usr/sbin`,
			`/sbin`,
		},
	}
}

// Check inspects a command line and returns a PolicyDecision. `escalate=true`
// from the model side overrides the deny rules but the decision still records
// that an escalation happened.
func (p BashPolicy) Check(command string) PolicyDecision {
	cmd := strings.TrimSpace(command)
	if cmd == "" {
		return PolicyDecision{Allowed: false, Reason: "empty command"}
	}

	firstToken := firstShellToken(cmd)
	lowerFirst := strings.ToLower(firstToken)

	if len(p.AllowCommands) > 0 {
		allowed := false
		for _, c := range p.AllowCommands {
			if strings.EqualFold(c, firstToken) || strings.EqualFold(c, lowerFirst) {
				allowed = true
				break
			}
		}
		if !allowed {
			return PolicyDecision{
				Allowed: false,
				Reason:  fmt.Sprintf("command %q is not in the whitelist (allowed: %s)", firstToken, strings.Join(p.AllowCommands, ", ")),
			}
		}
	}

	for _, deny := range p.DenyCommands {
		if strings.EqualFold(deny, firstToken) || strings.EqualFold(deny, lowerFirst) {
			return PolicyDecision{
				Allowed:    false,
				Reason:     fmt.Sprintf("command %q is in the deny list; rerun with escalate=true and a justification to override", firstToken),
				MatchedDeny: deny,
			}
		}
	}

	// DenyPaths: check every whitespace-separated token (skip flags, quoted
	// strings handled implicitly because we look for absolute prefixes). We
	// normalize both sides to forward slashes so the same policy works on
	// Windows and POSIX.
	for _, path := range extractPathLikeTokens(cmd) {
		normalized := strings.ToLower(strings.ReplaceAll(path, `\`, `/`))
		normalized = strings.TrimRight(normalized, "/")
		for _, deny := range p.DenyPaths {
			denyNorm := strings.ToLower(strings.ReplaceAll(strings.TrimRight(deny, `\/`), `\`, `/`))
			if normalized == denyNorm || strings.HasPrefix(normalized, denyNorm+"/") {
				return PolicyDecision{
					Allowed:     false,
					Reason:      fmt.Sprintf("path %q is under a protected directory (%s); rerun with escalate=true to override", path, deny),
					MatchedDeny: path,
				}
			}
		}
	}

	return PolicyDecision{Allowed: true}
}

// PolicyDecision is the result of a BashPolicy check.
type PolicyDecision struct {
	Allowed     bool
	Reason      string
	MatchedDeny string
	Escalated   bool
}

// WithEscalation marks an allow decision as having skipped deny rules.
func (d PolicyDecision) WithEscalation() PolicyDecision {
	d.Escalated = true
	return d
}

// firstShellToken returns the first whitespace-separated token of a command
// line, stripping any leading environment assignments (e.g. `FOO=bar cmd`).
func firstShellToken(cmd string) string {
	// Walk back-to-front to find the last `=` that isn't followed by a quote,
	// then check if everything before it is `[A-Z_][A-Z0-9_]*=`. If so, the
	// real command starts after the assignment.
	tokens := strings.Fields(cmd)
	for i, t := range tokens {
		if eq := strings.Index(t, "="); eq > 0 && !strings.ContainsAny(t, " \t\"'") {
			name := t[:eq]
			if isEnvAssignmentName(name) {
				if i+1 < len(tokens) {
					return tokens[i+1]
				}
				return ""
			}
		}
		break
	}
	if len(tokens) == 0 {
		return ""
	}
	return tokens[0]
}

func isEnvAssignmentName(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		if i == 0 {
			if !(r == '_' || (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z')) {
				return false
			}
			continue
		}
		if !(r == '_' || (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')) {
			return false
		}
	}
	return true
}

// extractPathLikeTokens pulls absolute-looking paths out of a command line. It
// is a best-effort token splitter: it does not understand shell quoting, but
// the deny-path check is forgiving enough (prefix match) that a missed quote
// rarely matters.
func extractPathLikeTokens(cmd string) []string {
	var out []string
	for _, raw := range strings.Fields(cmd) {
		// Strip leading/trailing quotes (single, double).
		cleaned := strings.Trim(raw, `"'`+"`")
		if cleaned == raw {
			// No quotes — keep as-is.
		} else {
			raw = cleaned
		}
		if strings.HasPrefix(raw, "/") || isWindowsAbsolute(raw) {
			out = append(out, raw)
		}
	}
	return out
}

func isWindowsAbsolute(p string) bool {
	if len(p) < 3 {
		return false
	}
	if !((p[0] >= 'A' && p[0] <= 'Z') || (p[0] >= 'a' && p[0] <= 'z')) {
		return false
	}
	if p[1] != ':' {
		return false
	}
	if p[2] != '\\' && p[2] != '/' {
		return false
	}
	return true
}
