package generate

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

// Event is something that happened during generation (for streaming to the screen).
type Event struct {
	Kind   string // "read", "search"
	Detail string
}

type Result struct {
	Text    string
	CostUSD float64
}

// Backend is the LLM that writes the tour.
type Backend interface {
	Name() string
	// Run passes prompt with dir as the working directory and returns the final reply.
	Run(ctx context.Context, dir, prompt string, events func(Event)) (Result, error)
}

// ClaudeCode runs the Claude Code CLI (claude -p) restricted to read-only tools.
type ClaudeCode struct {
	Bin string
}

func (c ClaudeCode) Name() string { return "Claude Code" }

// claudeArgs are the flags for running read-only.
//
//	--restricted                removes the command-execution tools and WebFetch, and confines file tools to the working directory
//	--strict-mcp-config         loads no MCP servers
//	--tools Read,Grep,Glob      limits the available tools to the three read-only ones
//	--permission-mode dontAsk   denies, without asking, any operation that needs confirmation
//
// The prompt goes in on stdin rather than as an argument: on Windows, claude installed with npm is claude.cmd,
// which runs through cmd.exe, and cmd.exe cuts command lines at 8191 characters.
func claudeArgs() []string {
	return []string{
		"-p",
		"--restricted", "--strict-mcp-config",
		"--tools", "Read,Grep,Glob",
		"--permission-mode", "dontAsk", "--permission-prompts", "none",
		"--no-session-persistence",
		"--output-format", "stream-json", "--verbose",
	}
}

type streamEvent struct {
	Type    string `json:"type"`
	Message struct {
		Content []struct {
			Type  string          `json:"type"`
			Name  string          `json:"name"`
			Input json.RawMessage `json:"input"`
		} `json:"content"`
	} `json:"message"`
	Result  string  `json:"result"`
	IsError bool    `json:"is_error"`
	Cost    float64 `json:"total_cost_usd"`
}

func (c ClaudeCode) Run(ctx context.Context, dir, prompt string, events func(Event)) (Result, error) {
	bin := c.Bin
	if bin == "" {
		bin = "claude"
	}
	cmd := exec.CommandContext(ctx, bin, claudeArgs()...)
	cmd.Dir = dir
	cmd.Stdin = strings.NewReader(prompt)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.StdoutPipe()
	if err != nil {
		return Result{}, err
	}
	if err := cmd.Start(); err != nil {
		return Result{}, err
	}

	var res Result
	gotResult := false
	sc := bufio.NewScanner(out)
	sc.Buffer(make([]byte, 1<<20), 64<<20)
	for sc.Scan() {
		var e streamEvent
		if json.Unmarshal(sc.Bytes(), &e) != nil {
			continue
		}
		switch e.Type {
		case "assistant":
			for _, part := range e.Message.Content {
				if part.Type != "tool_use" {
					continue
				}
				var in struct {
					FilePath string `json:"file_path"`
					Pattern  string `json:"pattern"`
					Path     string `json:"path"`
				}
				_ = json.Unmarshal(part.Input, &in)
				switch part.Name {
				case "Read":
					events(Event{"read", relative(dir, in.FilePath)})
				case "Grep", "Glob":
					events(Event{"search", in.Pattern})
				}
			}
		case "result":
			res = Result{Text: e.Result, CostUSD: e.Cost}
			gotResult = true
			if e.IsError {
				err = errors.New(strings.TrimSpace(e.Result))
			}
		}
	}
	waitErr := cmd.Wait()
	if ctx.Err() != nil {
		return res, ctx.Err()
	}
	if err != nil {
		return res, err
	}
	if !gotResult {
		msg := strings.TrimSpace(stderr.String())
		if waitErr != nil && msg == "" {
			msg = waitErr.Error()
		}
		return res, fmt.Errorf("claude: %s", msg)
	}
	return res, nil
}

func relative(dir, path string) string {
	if rel, err := filepath.Rel(dir, path); err == nil && !strings.HasPrefix(rel, "..") {
		return rel
	}
	return path
}
