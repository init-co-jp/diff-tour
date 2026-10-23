package generate

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

func TestClaudeArgs(t *testing.T) {
	args := claudeArgs()
	joined := strings.Join(args, " ")
	for _, want := range []string{
		"-p", "--restricted", "--strict-mcp-config", "--tools Read,Grep,Glob",
		"--permission-mode dontAsk", "--permission-prompts none", "--no-session-persistence",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("args lack %q: %s", want, joined)
		}
	}
	// The prompt goes in on stdin: claude.cmd on Windows runs through cmd.exe, which cuts command lines at 8191 characters.
	if last := args[len(args)-1]; last != "--verbose" {
		t.Errorf("last arg = %q (no prompt should follow the flags)", last)
	}
}

func TestClaudeCodeRunPromptOnStdin(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a shell script as claude")
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "claude")
	// Reports the number of arguments and the stdin it was given.
	script := "#!/bin/sh\nprintf '%s' \"$#\" > \"$0.args\"\ncat > \"$0.stdin\"\necho '{\"type\":\"result\",\"result\":\"tour\"}'\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	prompt := strings.Repeat("a long prompt with % and ^ and \" ", 1000)
	res, err := ClaudeCode{Bin: bin}.Run(context.Background(), dir, prompt, func(Event) {})
	if err != nil {
		t.Fatal(err)
	}
	if res.Text != "tour" {
		t.Errorf("result = %q", res.Text)
	}
	if got, _ := os.ReadFile(bin + ".stdin"); string(got) != prompt {
		t.Errorf("stdin = %d bytes, want the %d-byte prompt", len(got), len(prompt))
	}
	if got, _ := os.ReadFile(bin + ".args"); string(got) != strconv.Itoa(len(claudeArgs())) {
		t.Errorf("claude got %s args, want %d", got, len(claudeArgs()))
	}
}
