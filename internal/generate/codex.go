package generate

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Codex runs the OpenAI Codex CLI (codex exec) with a sandbox that can only read the working directory.
type Codex struct {
	Bin string
}

func (c Codex) Name() string { return "Codex CLI" }

// codexProfile is the name of the permissions profile defined on the command line.
const codexProfile = "diff_tour_read"

// codexArgs are the flags for running read-only (checked against codex-cli 0.160.1).
// Codex has no read-only tool set: it reads files by running shell commands (cat, sed, rg),
// so the guarantees come from the sandbox those commands run in.
//
//	--ignore-user-config            does not load ~/.codex/config.toml, so the user's MCP servers, plugins, hooks,
//	                                sandbox settings and trusted projects do not apply. With no trusted projects,
//	                                .codex/ config inside the workspace is skipped too. Auth is still read from CODEX_HOME
//	--ignore-rules                  does not load execpolicy .rules files, which could allow commands outside the sandbox
//	-c default_permissions, -c permissions.<name>.filesystem
//	                                every command runs in a sandbox (bubblewrap on Linux, Seatbelt on macOS) that can read
//	                                only the workspace, the system paths Codex calls :minimal (/usr, /etc, /lib, ...) and
//	                                the codex executable itself, which Linux re-runs inside the sandbox. Nothing is writable.
//	                                The profile leaves network off, so commands cannot reach the network.
//	                                (Not --sandbox read-only: it can read the whole disk, and it would disable this profile.)
//	-c approval_policy="never"      anything the sandbox blocks fails instead of asking to be allowed (also exec's default)
//	-c web_search="disabled"        removes the web search tool
//	--disable ...                   turns off tools that reach outside the workspace: app connectors and plugins (MCP),
//	                                sub-agents, memories, image generation, browser and computer use, image viewing
//	--ephemeral                     writes no session files; -c history.persistence="none" writes no history
//	--skip-git-repo-check           the workspace is a plain directory, not a repository
//	-C dir                          makes the workspace the working root (and the only workspace root)
//
// Verified on Linux (bubblewrap) only. Not enforceable from here: managed or system config (/etc/codex) still applies, and the model
// can still be told what is in the files it reads (prompt injection), though it cannot act on it.
func codexArgs(dir string, extraReadable []string) []string {
	readable := []string{`":minimal"="read"`, `":workspace_roots"={"."="read"}`}
	for _, p := range extraReadable {
		readable = append(readable, fmt.Sprintf("%q=%q", p, "read"))
	}
	args := []string{
		"exec", "--json", "--color", "never",
		"--ephemeral", "--ignore-user-config", "--ignore-rules", "--skip-git-repo-check",
		"-C", dir,
		"-c", fmt.Sprintf("default_permissions=%q", codexProfile),
		"-c", fmt.Sprintf("permissions.%s.filesystem={%s}", codexProfile, strings.Join(readable, ", ")),
		"-c", `approval_policy="never"`,
		"-c", `web_search="disabled"`,
		"-c", `history.persistence="none"`,
	}
	for _, f := range []string{
		"apps", "plugins", "remote_plugin", "multi_agent", "memories", "image_generation",
		"browser_use", "browser_use_external", "computer_use", "in_app_browser",
		"skill_mcp_dependency_install", "tool_suggest", "goals", "view_image",
	} {
		args = append(args, "--disable", f)
	}
	// The prompt is read from stdin ("-"), so a long prompt does not hit the argument size limit.
	return append(args, "-")
}

// codexReadablePaths is what the sandbox must be able to read so the codex executable can run inside it
// (on Linux, Codex re-runs its own executable inside bubblewrap to finish setting up the sandbox).
// exe is the real path of what was found on PATH.
//
//	standalone package (codex-package.json next to bin/)  the package directory, which also holds the bundled rg
//	native binary                                         the binary alone
//	npm package (node_modules/@openai/codex/bin/codex.js) node_modules/@openai, where codex.js finds the native binary
//
// Any other wrapper (a version manager's shim, a shell script) hides where the native binary is. Then nothing
// extra is granted, every command fails inside the sandbox, and Run reports it (it fails closed).
func codexReadablePaths(exe string) []string {
	if exe == "" {
		return nil
	}
	if pkg := filepath.Dir(filepath.Dir(exe)); isStandalonePackage(pkg, exe) {
		return []string{pkg}
	}
	if isNativeBinary(exe) {
		return []string{exe}
	}
	sep := string(filepath.Separator)
	if i := strings.LastIndex(exe, sep+filepath.Join("node_modules", "@openai", "codex")+sep); i >= 0 {
		return []string{exe[:i+len(sep+filepath.Join("node_modules", "@openai"))]}
	}
	return nil
}

// isStandalonePackage reports whether exe is the entrypoint of a Codex standalone package in pkg.
func isStandalonePackage(pkg, exe string) bool {
	b, err := os.ReadFile(filepath.Join(pkg, "codex-package.json"))
	if err != nil {
		return false
	}
	var m struct {
		Entrypoint string `json:"entrypoint"`
	}
	return json.Unmarshal(b, &m) == nil && m.Entrypoint != "" && filepath.Join(pkg, m.Entrypoint) == exe
}

// isNativeBinary reports whether path is an executable binary (ELF or Mach-O) rather than a script.
func isNativeBinary(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	head := make([]byte, 4)
	if _, err := io.ReadFull(f, head); err != nil {
		return false
	}
	switch {
	case bytes.Equal(head, []byte("\x7fELF")),
		bytes.Equal(head, []byte{0xcf, 0xfa, 0xed, 0xfe}), // Mach-O 64-bit
		bytes.Equal(head, []byte{0xca, 0xfe, 0xba, 0xbe}): // Mach-O universal
		return true
	}
	return false
}

// findExecutable returns the path to run bin from and its real path with symlinks followed.
// A native binary is run from its real path: the sandbox lets it read that, and on macOS it cannot re-run
// itself inside the sandbox through a symlink (Homebrew's /opt/homebrew/bin/codex). Anything else keeps
// its path on PATH, as a shim needs its own name.
func findExecutable(bin string) (path, real string, err error) {
	if path, err = exec.LookPath(bin); err != nil {
		return "", "", err
	}
	if path, err = filepath.Abs(path); err != nil {
		return "", "", err
	}
	if real, err = filepath.EvalSymlinks(path); err != nil {
		return "", "", err
	}
	if isNativeBinary(real) {
		path = real
	}
	return path, real, nil
}

// sandboxBroken is the output of a command when the sandbox could not start (see codexReadablePaths).
const sandboxBroken = "bwrap: execvp "

type codexEvent struct {
	Type string `json:"type"`
	Item struct {
		Type    string `json:"type"`
		Text    string `json:"text"`
		Command string `json:"command"`
		Output  string `json:"aggregated_output"`
		Message string `json:"message"`
	} `json:"item"`
	Message string `json:"message"`
	Error   struct {
		Message string `json:"message"`
	} `json:"error"`
}

// codexStream is what was read from the --json event stream.
type codexStream struct {
	text     string // the last agent message
	gotText  bool
	failure  string   // set when the turn failed
	warnings []string // non-fatal error items
	broken   string   // set when a command could not start inside the sandbox
}

func readCodexStream(r io.Reader, dir string, events func(Event)) (codexStream, error) {
	var s codexStream
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 1<<20), 64<<20)
	for sc.Scan() {
		var e codexEvent
		if json.Unmarshal(sc.Bytes(), &e) != nil {
			continue
		}
		switch e.Type {
		case "item.started":
			if e.Item.Type == "command_execution" {
				if ev, ok := commandEvent(dir, e.Item.Command); ok {
					events(ev)
				}
			}
		case "item.completed":
			switch e.Item.Type {
			case "agent_message":
				s.text, s.gotText = e.Item.Text, true
			case "error":
				s.warnings = append(s.warnings, strings.TrimSpace(e.Item.Message))
			case "command_execution":
				if strings.HasPrefix(e.Item.Output, sandboxBroken) {
					s.broken = strings.TrimSpace(e.Item.Output)
				}
			}
		case "turn.failed":
			s.failure = strings.TrimSpace(e.Error.Message)
		case "error":
			s.failure = strings.TrimSpace(e.Message)
		}
	}
	return s, sc.Err()
}

func (c Codex) Run(ctx context.Context, dir, prompt string, events func(Event)) (Result, error) {
	bin := c.Bin
	if bin == "" {
		bin = "codex"
	}
	// Run a native binary from where it really is and a shim by its name on PATH, but grant what it really is.
	path, real, err := findExecutable(bin)
	if err != nil {
		return Result{}, err
	}
	cmd := exec.CommandContext(ctx, path, codexArgs(dir, codexReadablePaths(real))...)
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
	s, readErr := readCodexStream(out, dir, events)
	waitErr := cmd.Wait()
	res := Result{Text: s.text}
	if ctx.Err() != nil {
		return res, ctx.Err()
	}
	if s.failure != "" {
		return res, errors.New("codex: " + s.failure)
	}
	if s.broken != "" {
		// The tour would be written without reading anything, so it is not used.
		return res, fmt.Errorf("codex: commands could not run in the read-only sandbox (%s); run the codex binary itself rather than %s", s.broken, path)
	}
	if !s.gotText {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" && len(s.warnings) > 0 {
			msg = strings.Join(s.warnings, "; ")
		}
		if msg == "" && waitErr != nil {
			msg = waitErr.Error()
		}
		if msg == "" && readErr != nil {
			msg = readErr.Error()
		}
		return res, fmt.Errorf("codex: %s", msg)
	}
	return res, nil
}

// commandEvent describes a shell command Codex ran as a read or a search.
// Commands come wrapped like `/usr/bin/bash -lc 'sed -n 1,80p head/x.go'`; only the first command
// of a pipeline or list is looked at.
func commandEvent(dir, command string) (Event, bool) {
	words := shellWords(command)
	if len(words) >= 3 && (words[1] == "-lc" || words[1] == "-c") && isShell(words[0]) {
		words = shellWords(words[2])
	}
	for i, w := range words {
		if w == "|" || w == "&&" || w == "||" || w == ";" || w == "&" {
			words = words[:i]
			break
		}
	}
	if len(words) == 0 {
		return Event{}, false
	}
	name, args := filepath.Base(words[0]), operands(words[1:])
	switch name {
	case "cat", "sed", "head", "tail", "nl", "less", "more", "bat", "wc", "awk":
		if name == "sed" || name == "awk" {
			// The first operand is the script.
			if len(args) > 1 {
				args = args[1:]
			}
		}
		if len(args) == 0 {
			return Event{}, false
		}
		return Event{"read", relative(dir, absIn(dir, args[len(args)-1]))}, true
	case "rg", "grep", "egrep", "ag", "git":
		if name == "git" {
			if len(args) == 0 || args[0] != "grep" {
				return Event{"read", short(command)}, true
			}
			args = args[1:]
		}
		if len(args) == 0 {
			return Event{"search", short(strings.Join(words[1:], " "))}, true
		}
		return Event{"search", args[0]}, true
	case "ls", "tree":
		if len(args) == 0 {
			args = []string{"."}
		}
		return Event{"search", short(strings.Join(args, " "))}, true
	case "find", "fd":
		return Event{"search", short(strings.Join(words[1:], " "))}, true
	}
	return Event{"read", short(strings.Join(words, " "))}, true
}

func isShell(path string) bool {
	switch filepath.Base(path) {
	case "bash", "sh", "zsh", "dash":
		return true
	}
	return false
}

// operands drops flags. A flag's value is not recognised, so `-n 1,80p` counts the value as an operand.
func operands(words []string) []string {
	var out []string
	for i, w := range words {
		if w == "--" {
			return append(out, words[i+1:]...)
		}
		if strings.HasPrefix(w, "-") && w != "-" {
			continue
		}
		out = append(out, w)
	}
	return out
}

func absIn(dir, p string) string {
	if filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(dir, p)
}

func short(s string) string {
	const max = 80
	if r := []rune(s); len(r) > max {
		return string(r[:max-1]) + "…"
	}
	return s
}

// shellWords splits a command line the way a POSIX shell would for simple commands
// (quotes and backslashes; |, ;, && and || become separate words). It is only used for display.
func shellWords(s string) []string {
	var words []string
	var cur strings.Builder
	inWord := false
	flush := func() {
		if inWord {
			words = append(words, cur.String())
			cur.Reset()
			inWord = false
		}
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '\'':
			inWord = true
			j := strings.IndexByte(s[i+1:], '\'')
			if j < 0 {
				cur.WriteString(s[i+1:])
				i = len(s)
				break
			}
			cur.WriteString(s[i+1 : i+1+j])
			i += j + 1
		case c == '"':
			inWord = true
			for i++; i < len(s) && s[i] != '"'; i++ {
				if s[i] == '\\' && i+1 < len(s) && strings.IndexByte("\"\\$`", s[i+1]) >= 0 {
					i++
				}
				cur.WriteByte(s[i])
			}
		case c == '\\' && i+1 < len(s):
			inWord = true
			i++
			cur.WriteByte(s[i])
		case c == ' ' || c == '\t' || c == '\n':
			flush()
		case c == '|' || c == ';' || c == '&':
			flush()
			op := string(c)
			if i+1 < len(s) && (s[i+1] == c) && c != ';' {
				op += string(c)
				i++
			}
			words = append(words, op)
		default:
			inWord = true
			cur.WriteByte(c)
		}
	}
	flush()
	return words
}
