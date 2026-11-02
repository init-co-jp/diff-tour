package generate

import (
	"bufio"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Pi runs the Pi coding agent (pi -p, with the prompt on stdin) restricted to read-only tools.
type Pi struct {
	Bin string
}

func (p Pi) Name() string { return "Pi" }

// piGuard is the extension that keeps Pi's tools inside the working directory. Pi has no permissions or sandbox
// of its own: its read tools take any path, so this is what refuses the rest.
//
//go:embed pi-guard.ts
var piGuard string

// ErrPiVersion means pi is older than 1.5, which lacks flags tour relies on (and pi silently accepts long flags it
// does not know), or is 2.0 or later, whose extension API may have changed.
var ErrPiVersion = errors.New("pi: Pi 1.5 or a later 1.x is needed")

// FindPi returns the path of a supported pi on PATH.
func FindPi() (string, error) {
	path, err := exec.LookPath("pi")
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, path, "--version").Output()
	if err != nil || !piVersionSupported(string(out)) {
		return "", ErrPiVersion
	}
	return path, nil
}

var piVersion = regexp.MustCompile(`(\d+)\.(\d+)\.\d+`)

// piVersionSupported reports whether `pi --version` ("1.5.0") is 1.5 or a later 1.x.
func piVersionSupported(version string) bool {
	m := piVersion.FindStringSubmatch(version)
	if m == nil {
		return false
	}
	major, _ := strconv.Atoi(m[1])
	minor, _ := strconv.Atoi(m[2])
	return major == 1 && minor >= 5
}

// piTools are the only tools the model is given. ls lists a directory, find matches file names, grep searches.
var piTools = []string{"read", "grep", "find", "ls"}

// piArgs are the flags for running read-only (checked against Pi 1.5.0). The prompt is read from stdin.
//
//	--tools read,grep,find,ls  the model sees only these: no bash, edit or write
//	--no-extensions -e <guard> none of the user's or the built-in extensions (MCP, codemode, tool search), only
//	                           piGuard, which refuses any other tool and any path outside the working directory
//	--no-mcp, --no-skills, --no-prompt-templates, --no-themes
//	                           no MCP servers, and nothing else is loaded from the user's Pi directory
//	--no-context-files         no AGENTS.md or CLAUDE.md
//	--no-approve               the repository's .pi directory (settings, extensions, MCP servers) is not trusted
//	--no-session               nothing is saved
//	--offline                  no update check, and no download of fd or rg
func piArgs(guard string) []string {
	return []string{
		"-p", "--mode", "json",
		"--tools", strings.Join(piTools, ","),
		"--no-extensions", "-e", guard,
		"--no-mcp", "--no-skills", "--no-context-files", "--no-prompt-templates", "--no-themes",
		"--no-session", "--no-approve", "--offline",
	}
}

// piEnv turns off Pi's network activity besides the model: PI_OFFLINE (as --offline) and PI_TELEMETRY. The user's
// Pi directory is still used, for the sign-in and the chosen model. PI_SHARE_VIEWER_URL is removed (nothing is
// shared from a run that saves no session, but it would point elsewhere).
func piEnv() []string {
	var env []string
	for _, kv := range os.Environ() {
		k, _, _ := strings.Cut(kv, "=")
		if k != "PI_SHARE_VIEWER_URL" {
			env = append(env, kv)
		}
	}
	return append(env, "PI_OFFLINE=1", "PI_TELEMETRY=0")
}

type piEvent struct {
	Type     string `json:"type"`
	ToolName string `json:"toolName"`
	Args     struct {
		Path    string `json:"path"`
		Pattern string `json:"pattern"`
	} `json:"args"`
	Message struct {
		Role    string `json:"role"`
		Content json.RawMessage
		Usage   struct {
			Cost struct {
				Total float64 `json:"total"`
			} `json:"cost"`
		} `json:"usage"`
		StopReason   string `json:"stopReason"`
		ErrorMessage string `json:"errorMessage"`
	} `json:"message"`
}

// piStream reads the --mode json events. Each assistant message comes whole in message_end; the answer is the text
// of the last one that has text. A failed model call is a message that stopped with an error (pi still exits with 0).
type piStream struct {
	dir    string
	events func(Event)
	text   string
	cost   float64
	err    error
}

func (s *piStream) line(b []byte) {
	var e piEvent
	if json.Unmarshal(b, &e) != nil {
		return
	}
	switch e.Type {
	case "tool_execution_start":
		path := e.Args.Path
		if path != "" && !filepath.IsAbs(path) {
			path = filepath.Join(s.dir, path)
		}
		switch e.ToolName {
		case "read", "ls":
			s.events(Event{"read", relative(s.dir, path)})
		case "grep", "find":
			s.events(Event{"search", e.Args.Pattern})
		}
	case "message_end":
		if e.Message.Role != "assistant" {
			return
		}
		s.cost += e.Message.Usage.Cost.Total
		switch e.Message.StopReason {
		case "error", "aborted":
			msg := strings.TrimSpace(e.Message.ErrorMessage)
			if msg == "" {
				msg = "the run ended with " + e.Message.StopReason
			}
			s.err = errors.New(msg)
			return
		}
		s.err = nil
		var parts []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}
		_ = json.Unmarshal(e.Message.Content, &parts)
		var text strings.Builder
		for _, p := range parts {
			if p.Type == "text" {
				text.WriteString(p.Text)
			}
		}
		if strings.TrimSpace(text.String()) != "" {
			s.text = text.String()
		}
	}
}

func (p Pi) Run(ctx context.Context, dir, prompt string, events func(Event)) (Result, error) {
	bin := p.Bin
	if bin == "" {
		var err error
		if bin, err = FindPi(); err != nil {
			return Result{}, err
		}
	}
	dir, err := filepath.Abs(dir)
	if err != nil {
		return Result{}, err
	}
	// The guard lives outside the working directory, so the model cannot read it.
	guardDir, err := os.MkdirTemp("", "diff-tour-pi-")
	if err != nil {
		return Result{}, err
	}
	defer os.RemoveAll(guardDir)
	guard := filepath.Join(guardDir, "diff-tour-guard.ts")
	if err := os.WriteFile(guard, []byte(piGuard), 0o600); err != nil {
		return Result{}, err
	}

	cmd := exec.CommandContext(ctx, bin, piArgs(guard)...)
	cmd.Dir = dir
	cmd.Env = piEnv()
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

	s := &piStream{dir: dir, events: events}
	sc := bufio.NewScanner(out)
	sc.Buffer(make([]byte, 1<<20), 64<<20)
	for sc.Scan() {
		s.line(sc.Bytes())
	}
	waitErr := cmd.Wait()
	res := Result{Text: s.text, CostUSD: s.cost}
	if ctx.Err() != nil {
		return res, ctx.Err()
	}
	if s.err != nil {
		return res, fmt.Errorf("pi: %w", s.err)
	}
	// A missing sign-in or model, or a guard that fails to load, is reported on stderr before the session starts.
	if waitErr != nil || strings.TrimSpace(res.Text) == "" {
		msg := strings.TrimSpace(stderr.String())
		if waitErr != nil && msg == "" {
			msg = waitErr.Error()
		}
		if msg == "" {
			msg = "no reply"
		}
		return res, fmt.Errorf("pi: %s", msg)
	}
	return res, nil
}
