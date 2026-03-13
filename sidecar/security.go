package sidecar

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// SecurityValidator checks commands against an allowlist of executables
// and blocked patterns. When enabled, commands are parsed into
// executable + arguments and run directly without a shell.
type SecurityValidator struct {
	allowedExecutables []string
	blockedPatterns    []*regexp.Regexp
}

// LoadSecurityConfig creates a SecurityValidator from environment variables.
// Returns nil if SIDECAR_ALLOWED_EXECUTABLES is not set (security disabled).
func LoadSecurityConfig() (*SecurityValidator, error) {
	raw := os.Getenv("SIDECAR_ALLOWED_EXECUTABLES")
	if raw == "" {
		return nil, nil
	}

	allowed := splitCSV(raw)
	if len(allowed) == 0 {
		return nil, nil
	}

	var blocked []*regexp.Regexp
	if patterns := os.Getenv("SIDECAR_BLOCKED_PATTERNS"); patterns != "" {
		for _, p := range splitCSV(patterns) {
			re, err := regexp.Compile(p)
			if err != nil {
				return nil, fmt.Errorf("invalid blocked pattern %q: %w", p, err)
			}
			blocked = append(blocked, re)
		}
	}

	return &SecurityValidator{
		allowedExecutables: allowed,
		blockedPatterns:    blocked,
	}, nil
}

// IsEnabled reports whether security validation is active.
// Safe to call on a nil receiver.
func (v *SecurityValidator) IsEnabled() bool {
	return v != nil && len(v.allowedExecutables) > 0
}

// ValidateCommand checks a command string against the security policy.
// It rejects shell metacharacters, verifies the executable is in the
// allowlist, and checks against blocked patterns.
func (v *SecurityValidator) ValidateCommand(command string) error {
	if !v.IsEnabled() {
		return nil
	}

	command = strings.TrimSpace(command)
	if command == "" {
		return fmt.Errorf("empty command")
	}

	// Reject shell metacharacters.
	if ch, found := findShellMetacharacter(command); found {
		return fmt.Errorf("command contains shell metacharacter %q (not allowed in secure mode)", string(ch))
	}

	// Parse into executable + args.
	exe, _, err := ParseCommand(command)
	if err != nil {
		return err
	}

	// Check allowlist.
	if !v.isAllowed(exe) {
		return fmt.Errorf("executable %q not in allowed list %v", exe, v.allowedExecutables)
	}

	// Check blocked patterns against the full command string.
	for _, re := range v.blockedPatterns {
		if re.MatchString(command) {
			return fmt.Errorf("command matches blocked pattern: %s", re.String())
		}
	}

	return nil
}

// BuildCommand creates an *exec.Cmd for a validated command using direct
// execution (no shell). Call ValidateCommand first.
func (v *SecurityValidator) BuildCommand(command string) (*exec.Cmd, error) {
	exe, args, err := ParseCommand(command)
	if err != nil {
		return nil, err
	}
	return exec.Command(exe, args...), nil
}

// isAllowed checks whether an executable name matches any allowlist entry.
func (v *SecurityValidator) isAllowed(executable string) bool {
	for _, pattern := range v.allowedExecutables {
		if matchesExecutable(executable, pattern) {
			return true
		}
	}
	return false
}

// matchesExecutable checks if an executable matches an allowed entry.
// Supports exact match, absolute-path match, and basename match (with
// PATH lookup).
func matchesExecutable(executable, pattern string) bool {
	// Exact match.
	if executable == pattern {
		return true
	}

	// If the pattern is an absolute path, compare resolved paths.
	if filepath.IsAbs(pattern) {
		if abs, err := filepath.Abs(executable); err == nil {
			return abs == pattern
		}
		return false
	}

	// Basename match: pattern "git" matches executable "git" found in PATH.
	if !filepath.IsAbs(executable) && filepath.Base(executable) == pattern {
		if _, err := exec.LookPath(executable); err == nil {
			return true
		}
	}

	return false
}

// findShellMetacharacter returns the first shell metacharacter found in s.
// These characters could cause injection if a command were ever passed to
// a shell. Backslash is intentionally NOT included because it is the
// Windows path separator.
func findShellMetacharacter(s string) (rune, bool) {
	const metachars = "|&;<>()$`"
	inSingle := false
	inDouble := false
	for _, ch := range s {
		if ch == '\'' && !inDouble {
			inSingle = !inSingle
			continue
		}
		if ch == '"' && !inSingle {
			inDouble = !inDouble
			continue
		}
		if !inSingle && !inDouble && strings.ContainsRune(metachars, ch) {
			return ch, true
		}
	}
	return 0, false
}

// ParseCommand splits a command string into an executable and arguments
// using shell-like quoting rules. Both single and double quotes are
// supported. Backslash is treated as a literal character (not an escape)
// so that Windows paths work correctly.
func ParseCommand(command string) (string, []string, error) {
	parts, err := splitShellArgs(command)
	if err != nil {
		return "", nil, fmt.Errorf("failed to parse command: %w", err)
	}
	if len(parts) == 0 {
		return "", nil, fmt.Errorf("empty command after parsing")
	}
	return parts[0], parts[1:], nil
}

// splitShellArgs splits a command string into tokens, handling single
// and double quotes. Backslash is always literal.
func splitShellArgs(s string) ([]string, error) {
	var args []string
	var current strings.Builder
	inSingle := false
	inDouble := false

	for _, ch := range s {
		switch {
		case ch == '\'' && !inDouble:
			inSingle = !inSingle
		case ch == '"' && !inSingle:
			inDouble = !inDouble
		case (ch == ' ' || ch == '\t') && !inSingle && !inDouble:
			if current.Len() > 0 {
				args = append(args, current.String())
				current.Reset()
			}
		default:
			current.WriteRune(ch)
		}
	}

	if inSingle {
		return nil, fmt.Errorf("unterminated single quote")
	}
	if inDouble {
		return nil, fmt.Errorf("unterminated double quote")
	}

	if current.Len() > 0 {
		args = append(args, current.String())
	}

	return args, nil
}

// splitCSV splits a comma-separated string, trimming whitespace and
// discarding empty entries.
func splitCSV(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
