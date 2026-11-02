package generate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
)

func TestPiArgs(t *testing.T) {
	args := piArgs("/tmp/x/guard.ts")
	want := "-p --mode json --tools read,grep,find,ls --no-extensions -e /tmp/x/guard.ts --no-mcp --no-skills " +
		"--no-context-files --no-prompt-templates --no-themes --no-session --no-approve --offline"
	if strings.Join(args, " ") != want {
		t.Errorf("args = %q", args)
	}
	// --approve would load the repository's .pi extensions, and a message argument would bypass stdin.
	for _, bad := range []string{"-a", "--approve", "-c", "--continue", "--session"} {
		if slices.Contains(args, bad) {
			t.Errorf("args contain %s", bad)
		}
	}
}

func TestPiEnv(t *testing.T) {
	t.Setenv("PI_OFFLINE", "0")
	t.Setenv("PI_TELEMETRY", "1")
	t.Setenv("PI_SHARE_VIEWER_URL", "https://example.com/")
	env := piEnv()
	last := map[string]string{}
	for _, kv := range env {
		k, v, _ := strings.Cut(kv, "=")
		last[k] = v
	}
	if last["PI_OFFLINE"] != "1" || last["PI_TELEMETRY"] != "0" {
		t.Errorf("PI_OFFLINE = %q, PI_TELEMETRY = %q", last["PI_OFFLINE"], last["PI_TELEMETRY"])
	}
	if _, ok := last["PI_SHARE_VIEWER_URL"]; ok {
		t.Error("PI_SHARE_VIEWER_URL is kept")
	}
}

func TestPiVersionSupported(t *testing.T) {
	for v, want := range map[string]bool{
		"1.5.0\n": true, "1.5.3": true, "1.12.0": true, "v1.6.1": true,
		"1.4.9": false, "0.9.0": false, "2.0.0": false, "": false, "pi": false,
	} {
		if got := piVersionSupported(v); got != want {
			t.Errorf("piVersionSupported(%q) = %v, want %v", v, got, want)
		}
	}
}

func TestFindPi(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in pi is a shell script")
	}
	for _, c := range []struct {
		version string
		err     error
	}{{"1.5.0", nil}, {"1.4.2", ErrPiVersion}, {"2.0.0", ErrPiVersion}} {
		dir := t.TempDir()
		script := "#!/bin/sh\necho " + c.version + "\n"
		if err := os.WriteFile(filepath.Join(dir, "pi"), []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
		t.Setenv("PATH", dir)
		path, err := FindPi()
		if !errors.Is(err, c.err) {
			t.Errorf("%s: err = %v, want %v", c.version, err, c.err)
		}
		if c.err == nil && path != filepath.Join(dir, "pi") {
			t.Errorf("%s: path = %q", c.version, path)
		}
	}
	t.Setenv("PATH", t.TempDir())
	if _, err := FindPi(); !errors.Is(err, exec.ErrNotFound) {
		t.Errorf("no pi: err = %v", err)
	}
}

// piSample is the --mode json output of a run, shortened (message_update events and most fields are left out).
const piSample = `{"type":"session","version":3,"id":"s","timestamp":"t","cwd":"/w"}
{"type":"agent_start"}
{"type":"message_end","message":{"role":"user","content":"go"}}
{"type":"message_end","message":{"role":"assistant","content":[{"type":"text","text":"I will read the diff."},{"type":"toolCall","id":"c1","name":"read"}],"usage":{"cost":{"total":0.25}},"stopReason":"toolUse"}}
{"type":"tool_execution_start","toolCallId":"c1","toolName":"read","args":{"path":"change.diff"}}
{"type":"tool_execution_start","toolCallId":"c2","toolName":"grep","args":{"pattern":"used_count","path":"head"}}
{"type":"tool_execution_start","toolCallId":"c3","toolName":"find","args":{"pattern":"**/*.go"}}
{"type":"tool_execution_start","toolCallId":"c4","toolName":"ls","args":{"path":"head/internal"}}
{"type":"tool_execution_start","toolCallId":"c5","toolName":"read","args":{"path":"/w/head/a.go"}}
{"type":"message_end","message":{"role":"assistant","content":[{"type":"thinking","thinking":"done"},{"type":"text","text":"=== ツアー JSON ===\n"},{"type":"text","text":"{\"version\":1}"}],"usage":{"cost":{"total":0.5}},"stopReason":"stop"}}
{"type":"agent_end","messages":[],"willRetry":false}
{"type":"agent_settled","aborted":false}`

func readPiSample(sample string) (*piStream, []Event) {
	var events []Event
	s := &piStream{dir: "/w", events: func(e Event) { events = append(events, e) }}
	for _, l := range strings.Split(sample, "\n") {
		s.line([]byte(l))
	}
	return s, events
}

func TestPiStream(t *testing.T) {
	s, events := readPiSample(piSample)
	want := []Event{{"read", "change.diff"}, {"search", "used_count"}, {"search", "**/*.go"}, {"read", "head/internal"}, {"read", "head/a.go"}}
	if !slices.Equal(events, want) {
		t.Errorf("events = %v, want %v", events, want)
	}
	if s.err != nil || s.cost != 0.75 {
		t.Errorf("err = %v, cost = %v", s.err, s.cost)
	}
	if s.text != "=== ツアー JSON ===\n{\"version\":1}" {
		t.Errorf("text = %q", s.text)
	}
}

func TestPiStreamErrors(t *testing.T) {
	// A failed model call ends with an assistant message whose stopReason is error; pi still exits with 0.
	s, _ := readPiSample(`{"type":"message_end","message":{"role":"assistant","content":[],"stopReason":"error","errorMessage":"Connection error."}}`)
	if s.err == nil || s.err.Error() != "Connection error." {
		t.Errorf("err = %v", s.err)
	}
	// A retry that succeeds clears the error.
	s, _ = readPiSample(`{"type":"message_end","message":{"role":"assistant","content":[],"stopReason":"error","errorMessage":"529 overloaded"}}
{"type":"message_end","message":{"role":"assistant","content":[{"type":"text","text":"{}"}],"stopReason":"stop"}}`)
	if s.err != nil || s.text != "{}" {
		t.Errorf("err = %v, text = %q", s.err, s.text)
	}
	s, _ = readPiSample(`{"type":"message_end","message":{"role":"assistant","content":[],"stopReason":"aborted"}}`)
	if s.err == nil {
		t.Error("an aborted run is not an error")
	}
}

// fakeOpenAI is a scripted stand-in model with the OpenAI chat completions API. Each reply is either tool calls
// or text, chosen by how many assistant messages the request already has. It records the tool results it is sent.
type fakeOpenAI struct {
	mu      sync.Mutex
	replies []any // [][2]any{name, args} for tool calls, or a string
	results []string
	prompts string // everything else it was sent
}

func (f *fakeOpenAI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Messages []struct {
			Role    string `json:"role"`
			Content any    `json:"content"`
		} `json:"messages"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	n := 0
	f.mu.Lock()
	f.results, f.prompts = f.results[:0], ""
	for _, m := range req.Messages {
		switch m.Role {
		case "assistant":
			n++
		}
		b, _ := json.Marshal(m.Content)
		if m.Role == "tool" {
			f.results = append(f.results, string(b))
		} else {
			f.prompts += string(b)
		}
	}
	f.mu.Unlock()
	reply := f.replies[min(n, len(f.replies)-1)]
	w.Header().Set("Content-Type", "text/event-stream")
	send := func(v any) {
		b, _ := json.Marshal(v)
		fmt.Fprintf(w, "data: %s\n\n", b)
	}
	chunk := func(delta map[string]any, finish any) map[string]any {
		return map[string]any{"id": "x", "object": "chat.completion.chunk", "model": "fake",
			"choices": []any{map[string]any{"index": 0, "delta": delta, "finish_reason": finish}}}
	}
	switch reply := reply.(type) {
	case string:
		send(chunk(map[string]any{"role": "assistant", "content": reply}, nil))
		send(chunk(map[string]any{}, "stop"))
	case [][2]any:
		var calls []any
		for i, c := range reply {
			args, _ := json.Marshal(c[1])
			calls = append(calls, map[string]any{"index": i, "id": fmt.Sprintf("call_%d_%d", n, i), "type": "function",
				"function": map[string]any{"name": c[0], "arguments": string(args)}})
		}
		send(chunk(map[string]any{"role": "assistant", "tool_calls": calls}, nil))
		send(chunk(map[string]any{}, "tool_calls"))
	}
	fmt.Fprint(w, "data: [DONE]\n\n")
}

// TestPiGuard runs the real pi, if a supported version is installed, with a scripted model that tries to read
// outside the working directory in every way the read tools allow, and to use tools it is not given.
func TestPiGuard(t *testing.T) {
	bin, err := FindPi()
	if err != nil {
		t.Skipf("pi: %v", err)
	}
	root := t.TempDir()
	outside := filepath.Join(root, "outside")
	dir := filepath.Join(root, "work")
	agent := filepath.Join(root, "agent")
	for _, d := range []string{outside, filepath.Join(dir, "head"), filepath.Join(dir, ".pi", "extensions"), agent} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	files := map[string]string{
		filepath.Join(outside, "secret.txt"): "OUTSIDE-SECRET",
		filepath.Join(dir, "head", "a.go"):   "package inside",
		filepath.Join(dir, "AGENTS.md"):      "AGENTS-MD-WAS-LOADED",
		// An extension in the repository would run code if the project were trusted.
		filepath.Join(dir, ".pi", "extensions", "x.ts"): `import { writeFileSync } from "node:fs";
export default function () { writeFileSync(` + fmt.Sprintf("%q", filepath.Join(root, "loaded")) + `, "x"); }`,
	}
	for name, content := range files {
		if err := os.WriteFile(name, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(filepath.Join(outside, "secret.txt"), filepath.Join(dir, "link.txt")); err != nil {
		t.Fatal(err)
	}

	secret := filepath.Join(outside, "secret.txt")
	fake := &fakeOpenAI{replies: []any{
		[][2]any{
			{"read", map[string]any{"path": secret}},
			{"read", map[string]any{"path": "../outside/secret.txt"}},
			{"read", map[string]any{"path": "@../outside/secret.txt"}},
			{"read", map[string]any{"path": "file://" + secret}},
			{"read", map[string]any{"path": "head/../../outside/secret.txt"}},
			{"read", map[string]any{"path": "link.txt"}},
			{"read", map[string]any{"path": "~/.bashrc"}},
			{"ls", map[string]any{"path": ".."}},
			{"grep", map[string]any{"pattern": "OUTSIDE", "path": outside}},
			{"grep", map[string]any{"pattern": "OUTSIDE", "glob": "../**"}},
			{"find", map[string]any{"pattern": "*.txt", "path": ".."}},
			{"find", map[string]any{"pattern": outside + "/*"}},
			{"bash", map[string]any{"command": "cat " + secret}},
			{"write", map[string]any{"path": "x.txt", "content": "x"}},
			{"read", map[string]any{"path": "head/a.go"}},
		},
		"done",
	}}
	srv := httptest.NewServer(fake)
	defer srv.Close()
	models := `{"providers":{"fake":{"baseUrl":"` + srv.URL + `/v1","api":"openai-completions","apiKey":"x",` +
		`"models":[{"id":"fake-1","cost":{"input":0,"output":0,"cacheRead":0,"cacheWrite":0}}]}}}`
	if err := os.WriteFile(filepath.Join(agent, "models.json"), []byte(models), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agent, "settings.json"), []byte(`{"defaultProvider":"fake","defaultModel":"fake-1"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PI_CODING_AGENT_DIR", agent)

	var events []Event
	res, err := Pi{Bin: bin}.Run(context.Background(), dir, "go", func(e Event) { events = append(events, e) })
	if err != nil {
		t.Fatal(err)
	}
	if res.Text != "done" {
		t.Errorf("text = %q", res.Text)
	}
	if len(fake.results) != 15 {
		t.Fatalf("got %d tool results: %q", len(fake.results), fake.results)
	}
	for i, r := range fake.results[:14] {
		if strings.Contains(r, "OUTSIDE-SECRET") || strings.Contains(r, "secret.txt") {
			t.Errorf("tool call %d read outside: %.80s", i, r)
		}
		// A glob or pattern that points outside only filters what is found inside; it is not refused.
		if i != 9 && i != 11 && !strings.Contains(r, "diff-tour allows") && !strings.Contains(r, "not found") {
			t.Errorf("tool call %d was not refused: %.80s", i, r)
		}
	}
	if strings.Contains(fake.prompts, "AGENTS-MD-WAS-LOADED") {
		t.Error("AGENTS.md was sent to the model")
	}
	if !strings.Contains(fake.results[14], "package inside") {
		t.Errorf("reading inside failed: %s", fake.results[14])
	}
	if _, err := os.Stat(filepath.Join(root, "loaded")); err == nil {
		t.Error("the repository's .pi extension was loaded")
	}
	if _, err := os.Stat(filepath.Join(dir, "x.txt")); err == nil {
		t.Error("a file was written")
	}
	if !slices.Contains(events, Event{"read", filepath.Join("head", "a.go")}) {
		t.Errorf("events = %v", events)
	}
}
