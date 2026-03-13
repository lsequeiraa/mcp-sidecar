package sidecar

import (
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

// --- LoadSecurityConfig ---

func TestLoadSecurityConfig_Disabled(t *testing.T) {
	t.Setenv("SIDECAR_ALLOWED_EXECUTABLES", "")
	t.Setenv("SIDECAR_BLOCKED_PATTERNS", "")

	v, err := LoadSecurityConfig()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v != nil {
		t.Error("expected nil validator when env var is empty")
	}
}

func TestLoadSecurityConfig_AllowlistOnly(t *testing.T) {
	t.Setenv("SIDECAR_ALLOWED_EXECUTABLES", "git,npm,dotnet")
	t.Setenv("SIDECAR_BLOCKED_PATTERNS", "")

	v, err := LoadSecurityConfig()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v == nil {
		t.Fatal("expected non-nil validator")
	}
	if len(v.allowedExecutables) != 3 {
		t.Errorf("allowed = %v, want 3 entries", v.allowedExecutables)
	}
	if len(v.blockedPatterns) != 0 {
		t.Errorf("blocked = %v, want 0 entries", v.blockedPatterns)
	}
}

func TestLoadSecurityConfig_WithBlockedPatterns(t *testing.T) {
	t.Setenv("SIDECAR_ALLOWED_EXECUTABLES", "git")
	t.Setenv("SIDECAR_BLOCKED_PATTERNS", `rm\s+-rf,sudo\s+`)

	v, err := LoadSecurityConfig()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(v.blockedPatterns) != 2 {
		t.Fatalf("blocked = %d, want 2", len(v.blockedPatterns))
	}
}

func TestLoadSecurityConfig_InvalidPattern(t *testing.T) {
	t.Setenv("SIDECAR_ALLOWED_EXECUTABLES", "git")
	t.Setenv("SIDECAR_BLOCKED_PATTERNS", "[invalid")

	_, err := LoadSecurityConfig()
	if err == nil {
		t.Fatal("expected error for invalid regex, got nil")
	}
	if !strings.Contains(err.Error(), "invalid blocked pattern") {
		t.Errorf("error = %q, want it to mention 'invalid blocked pattern'", err.Error())
	}
}

func TestLoadSecurityConfig_WhitespaceHandling(t *testing.T) {
	t.Setenv("SIDECAR_ALLOWED_EXECUTABLES", " git , npm , dotnet ")
	t.Setenv("SIDECAR_BLOCKED_PATTERNS", "")

	v, err := LoadSecurityConfig()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"git", "npm", "dotnet"}
	for i, got := range v.allowedExecutables {
		if got != want[i] {
			t.Errorf("allowed[%d] = %q, want %q", i, got, want[i])
		}
	}
}

// --- IsEnabled ---

func TestIsEnabled_Nil(t *testing.T) {
	var v *SecurityValidator
	if v.IsEnabled() {
		t.Error("nil validator should not be enabled")
	}
}

func TestIsEnabled_Empty(t *testing.T) {
	v := &SecurityValidator{}
	if v.IsEnabled() {
		t.Error("empty validator should not be enabled")
	}
}

func TestIsEnabled_WithAllowlist(t *testing.T) {
	v := &SecurityValidator{allowedExecutables: []string{"git"}}
	if !v.IsEnabled() {
		t.Error("validator with allowlist should be enabled")
	}
}

// --- ValidateCommand ---

func TestValidateCommand_Disabled(t *testing.T) {
	var v *SecurityValidator
	if err := v.ValidateCommand("rm -rf /"); err != nil {
		t.Errorf("disabled validator should allow anything, got: %v", err)
	}
}

func TestValidateCommand_EmptyCommand(t *testing.T) {
	v := &SecurityValidator{allowedExecutables: []string{"git"}}
	err := v.ValidateCommand("   ")
	if err == nil {
		t.Fatal("expected error for empty command")
	}
	if !strings.Contains(err.Error(), "empty command") {
		t.Errorf("error = %q, want 'empty command'", err.Error())
	}
}

func TestValidateCommand_AllowedExecutable(t *testing.T) {
	v := &SecurityValidator{allowedExecutables: []string{"git", "npm", "dotnet"}}

	tests := []struct {
		command string
		wantErr bool
	}{
		{"git status", false},
		{"npm install", false},
		{"dotnet run --project Foo", false},
		{"rm -rf /", true},
		{"python script.py", true},
	}

	for _, tt := range tests {
		t.Run(tt.command, func(t *testing.T) {
			err := v.ValidateCommand(tt.command)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateCommand(%q) error = %v, wantErr = %v", tt.command, err, tt.wantErr)
			}
			if tt.wantErr && err != nil && !strings.Contains(err.Error(), "not in allowed list") {
				t.Errorf("error = %q, want 'not in allowed list'", err.Error())
			}
		})
	}
}

func TestValidateCommand_ShellMetachars(t *testing.T) {
	v := &SecurityValidator{allowedExecutables: []string{"echo", "git"}}

	metaTests := []struct {
		name    string
		command string
	}{
		{"pipe", "echo hello | grep h"},
		{"semicolon", "echo hello; rm -rf /"},
		{"ampersand", "echo hello & echo world"},
		{"dollar", "echo $HOME"},
		{"backtick", "echo `whoami`"},
		{"redirect out", "echo hello > file.txt"},
		{"redirect in", "echo hello < file.txt"},
		{"subshell open", "echo (hello)"},
		{"subshell close", "git log )"},
	}

	for _, tt := range metaTests {
		t.Run(tt.name, func(t *testing.T) {
			err := v.ValidateCommand(tt.command)
			if err == nil {
				t.Errorf("expected error for %q", tt.command)
			}
			if err != nil && !strings.Contains(err.Error(), "shell metacharacter") {
				t.Errorf("error = %q, want 'shell metacharacter'", err.Error())
			}
		})
	}
}

func TestValidateCommand_BackslashAllowed(t *testing.T) {
	v := &SecurityValidator{allowedExecutables: []string{"dotnet"}}

	// Backslash should NOT be rejected — it's a Windows path separator.
	err := v.ValidateCommand(`dotnet run --project C:\Projects\Foo`)
	if err != nil {
		t.Errorf("backslash in path should be allowed, got: %v", err)
	}
}

func TestValidateCommand_QuotedMetacharsAllowed(t *testing.T) {
	v := &SecurityValidator{allowedExecutables: []string{"grep", "echo"}}

	tests := []struct {
		command string
		wantErr bool
		errMsg  string
	}{
		{`grep "error|warning" file.txt`, false, ""},      // pipe inside double quotes
		{`echo 'a & b'`, false, ""},                       // ampersand inside single quotes
		{`grep "hello" | wc`, true, "metacharacter"},      // pipe outside quotes
		{`echo "safe;text" && rm`, true, "metacharacter"}, // && outside quotes
	}

	for _, tt := range tests {
		t.Run(tt.command, func(t *testing.T) {
			err := v.ValidateCommand(tt.command)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateCommand(%q) error = %v, wantErr = %v", tt.command, err, tt.wantErr)
			}
			if tt.wantErr && err != nil && !strings.Contains(err.Error(), tt.errMsg) {
				t.Errorf("error = %q, want it to contain %q", err.Error(), tt.errMsg)
			}
		})
	}
}

func TestValidateCommand_BlockedPatterns(t *testing.T) {
	v := &SecurityValidator{
		allowedExecutables: []string{"git", "rm"},
		blockedPatterns:    compilePatterns(t, `git\s+push\s+--force`, `rm\s+-rf`),
	}

	tests := []struct {
		command string
		wantErr bool
		errMsg  string
	}{
		{"git status", false, ""},
		{"git push origin main", false, ""},
		{"git push --force origin main", true, "blocked pattern"},
		{"rm -rf /", true, "blocked pattern"},
		{"rm file.txt", false, ""},
	}

	for _, tt := range tests {
		t.Run(tt.command, func(t *testing.T) {
			err := v.ValidateCommand(tt.command)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateCommand(%q) error = %v, wantErr = %v", tt.command, err, tt.wantErr)
			}
			if tt.wantErr && err != nil && !strings.Contains(err.Error(), tt.errMsg) {
				t.Errorf("error = %q, want it to contain %q", err.Error(), tt.errMsg)
			}
		})
	}
}

// --- matchesExecutable ---

func TestMatchesExecutable(t *testing.T) {
	tests := []struct {
		name       string
		executable string
		pattern    string
		want       bool
	}{
		{"exact match", "git", "git", true},
		{"no match", "git", "npm", false},
		{"basename vs basename", "dotnet", "dotnet", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := matchesExecutable(tt.executable, tt.pattern)
			if got != tt.want {
				t.Errorf("matchesExecutable(%q, %q) = %v, want %v",
					tt.executable, tt.pattern, got, tt.want)
			}
		})
	}
}

func TestMatchesExecutable_AbsolutePath(t *testing.T) {
	var absPath string
	if runtime.GOOS == "windows" {
		absPath = `C:\Windows\System32\cmd.exe`
	} else {
		absPath = "/usr/bin/git"
	}

	// Absolute pattern should not match a bare name.
	if matchesExecutable("git", absPath) {
		t.Errorf("bare 'git' should not match absolute pattern %q", absPath)
	}
}

func TestMatchesExecutable_AbsolutePathPositive(t *testing.T) {
	// Use a file that exists in the test directory. filepath.Abs resolves
	// a relative executable against cwd, so we verify that branch works.
	relPath := "security.go"
	absPath, err := filepath.Abs(relPath)
	if err != nil {
		t.Fatalf("filepath.Abs(%q): %v", relPath, err)
	}

	// Relative executable should match its own absolute path as pattern.
	if !matchesExecutable(relPath, absPath) {
		t.Errorf("matchesExecutable(%q, %q) = false, want true", relPath, absPath)
	}

	// Different absolute path should not match.
	if runtime.GOOS == "windows" {
		if matchesExecutable(relPath, `C:\nonexistent\other.go`) {
			t.Error("should not match a different absolute path")
		}
	} else {
		if matchesExecutable(relPath, "/nonexistent/other.go") {
			t.Error("should not match a different absolute path")
		}
	}
}

func TestMatchesExecutable_PathLookup(t *testing.T) {
	// "go" should be in PATH in any Go test environment.
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go not in PATH")
	}
	if !matchesExecutable("go", "go") {
		t.Error("matchesExecutable('go', 'go') should be true when go is in PATH")
	}
}

// --- findShellMetacharacter ---

func TestFindShellMetacharacter(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"echo hello", false},
		{"dotnet run --project Foo", false},
		{`path\to\thing`, false},                 // backslash is NOT a metachar
		{`grep "error|warning" file.txt`, false}, // pipe inside double quotes is safe
		{`grep 'error|warning' file.txt`, false}, // pipe inside single quotes is safe
		{`echo "hello;world"`, false},            // semicolon inside double quotes is safe
		{`echo 'a & b'`, false},                  // ampersand inside single quotes is safe
		{"echo | grep", true},                    // pipe outside quotes
		{"echo; rm", true},                       // semicolon outside quotes
		{"echo $HOME", true},                     // dollar sign outside quotes
		{"echo `cmd`", true},                     // backtick outside quotes
		{"a > b", true},                          // redirect outside quotes
		{"a < b", true},                          // redirect outside quotes
		{"a & b", true},                          // ampersand outside quotes
		{"(cmd)", true},                          // parens outside quotes
		{`grep "hello" | wc`, true},              // pipe outside quotes despite quoted arg
		{`echo "hello;world" && rm`, true},       // && outside quotes despite ; inside
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			_, found := findShellMetacharacter(tt.input)
			if found != tt.want {
				t.Errorf("findShellMetacharacter(%q) = %v, want %v", tt.input, found, tt.want)
			}
		})
	}
}

// --- ParseCommand / splitShellArgs ---

func TestParseCommand(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantExe string
		wantN   int // expected number of args (excluding exe)
	}{
		{"simple", "git status", "git", 1},
		{"multiple args", "dotnet run --project Foo", "dotnet", 3},
		{"single arg", "ls", "ls", 0},
		{"double quotes", `echo "hello world"`, "echo", 1},
		{"single quotes", "echo 'hello world'", "echo", 1},
		{"mixed quotes", `git commit -m "initial commit"`, "git", 3},
		{"windows path", `dotnet run --project C:\Projects\Foo`, "dotnet", 3},
		{"extra whitespace", "  git   status  ", "git", 1},
		{"tabs", "git\tstatus", "git", 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			exe, args, err := ParseCommand(tt.input)
			if err != nil {
				t.Fatalf("ParseCommand(%q) error: %v", tt.input, err)
			}
			if exe != tt.wantExe {
				t.Errorf("exe = %q, want %q", exe, tt.wantExe)
			}
			if len(args) != tt.wantN {
				t.Errorf("args = %v (len %d), want %d args", args, len(args), tt.wantN)
			}
		})
	}
}

func TestParseCommand_QuotedValues(t *testing.T) {
	exe, args, err := ParseCommand(`echo "hello world" 'foo bar'`)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if exe != "echo" {
		t.Errorf("exe = %q, want 'echo'", exe)
	}
	if len(args) != 2 {
		t.Fatalf("args = %v, want 2 args", args)
	}
	if args[0] != "hello world" {
		t.Errorf("args[0] = %q, want 'hello world'", args[0])
	}
	if args[1] != "foo bar" {
		t.Errorf("args[1] = %q, want 'foo bar'", args[1])
	}
}

func TestParseCommand_UnterminatedQuote(t *testing.T) {
	_, _, err := ParseCommand(`echo "hello`)
	if err == nil {
		t.Fatal("expected error for unterminated quote")
	}
	if !strings.Contains(err.Error(), "unterminated") {
		t.Errorf("error = %q, want 'unterminated'", err.Error())
	}
}

func TestParseCommand_Empty(t *testing.T) {
	_, _, err := ParseCommand("")
	if err == nil {
		t.Fatal("expected error for empty command")
	}
}

func TestParseCommand_WindowsPath(t *testing.T) {
	exe, args, err := ParseCommand(`dotnet run --project "C:\My Projects\App"`)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if exe != "dotnet" {
		t.Errorf("exe = %q, want 'dotnet'", exe)
	}
	if len(args) != 3 {
		t.Fatalf("args = %v, want 3", args)
	}
	if args[2] != `C:\My Projects\App` {
		t.Errorf("args[2] = %q, want %q", args[2], `C:\My Projects\App`)
	}
}

// --- splitCSV ---

func TestSplitCSV(t *testing.T) {
	tests := []struct {
		input string
		want  []string
	}{
		{"git,npm,dotnet", []string{"git", "npm", "dotnet"}},
		{" git , npm , dotnet ", []string{"git", "npm", "dotnet"}},
		{"git", []string{"git"}},
		{"", nil},
		{",,,", nil},
		{"git,,npm", []string{"git", "npm"}},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := splitCSV(tt.input)
			if len(got) != len(tt.want) {
				t.Fatalf("splitCSV(%q) = %v (len %d), want %v (len %d)",
					tt.input, got, len(got), tt.want, len(tt.want))
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("splitCSV(%q)[%d] = %q, want %q", tt.input, i, got[i], tt.want[i])
				}
			}
		})
	}
}

// --- BuildCommand ---

func TestBuildCommand_Direct(t *testing.T) {
	v := &SecurityValidator{allowedExecutables: []string{"echo"}}

	cmd, err := v.BuildCommand("echo hello world")
	if err != nil {
		t.Fatalf("BuildCommand error: %v", err)
	}

	// cmd.Path is resolved; cmd.Args[0] is the original name.
	if cmd.Args[0] != "echo" {
		t.Errorf("cmd.Args[0] = %q, want 'echo'", cmd.Args[0])
	}
	if len(cmd.Args) != 3 {
		t.Errorf("cmd.Args = %v, want 3 elements", cmd.Args)
	}
}

// --- helpers ---

func compilePatterns(t *testing.T, patterns ...string) []*regexp.Regexp {
	t.Helper()
	var out []*regexp.Regexp
	for _, p := range patterns {
		re, err := regexp.Compile(p)
		if err != nil {
			t.Fatalf("failed to compile pattern %q: %v", p, err)
		}
		out = append(out, re)
	}
	return out
}
