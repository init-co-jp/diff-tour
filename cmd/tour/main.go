// tour presents git changes as a tour that lets you relive what went through a senior engineer's head.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/init-co-jp/diff-tour/internal/config"
	"github.com/init-co-jp/diff-tour/internal/generate"
	"github.com/init-co-jp/diff-tour/internal/git"
	"github.com/init-co-jp/diff-tour/internal/i18n"
	"github.com/init-co-jp/diff-tour/internal/render"
	"github.com/init-co-jp/diff-tour/internal/tour"
	"github.com/init-co-jp/diff-tour/internal/ui"
)

const usage = `usage:
  tour                      choose a branch and tour it
  tour <branch>             tour a branch, e.g. feature/x or origin/feature/x
                            (compared with the default branch)
  tour <commit>             tour a single commit
  tour <base>..<head>       tour a range
  tour <tour.json>          play a tour file
  tour setup                choose the language and the LLM again

options:
  --repo <path>   repository to read (default: current directory)
  --lang <lang>   language for this run only: en, ja, zh (Simplified Chinese),
                  zh-Hant (Traditional Chinese), es or ru
  --dump          print every step to stdout instead of opening the TUI
  --cols, --rows  screen size for --dump (default 100x30)
  --plain         --dump without colors
  --version       print the version
`

// version is set by the release build (-ldflags "-X main.version=...").
var version = ""

// versionString falls back to the module version that go install records.
func versionString() string {
	if version != "" {
		return version
	}
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	return "dev"
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "tour:", err)
		os.Exit(1)
	}
}

func isTerminal() bool {
	for _, f := range []*os.File{os.Stdin, os.Stdout} {
		info, err := f.Stat()
		if err != nil || info.Mode()&os.ModeCharDevice == 0 {
			return false
		}
	}
	return true
}

// parse can read flags even when they are mixed with positional arguments (Go's flag package stops at the first positional argument).
func parse(fs *flag.FlagSet, args []string) ([]string, error) {
	var positional []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		if fs.NArg() == 0 {
			return positional, nil
		}
		positional = append(positional, fs.Arg(0))
		args = fs.Args()[1:]
	}
}

func run(args []string) error {
	fs := flag.NewFlagSet("tour", flag.ContinueOnError)
	fs.Usage = func() { fmt.Fprint(os.Stderr, usage) }
	repoDir := fs.String("repo", ".", "")
	langFlag := fs.String("lang", "", "")
	dump := fs.Bool("dump", false, "")
	plain := fs.Bool("plain", false, "")
	cols := fs.Int("cols", 100, "")
	rows := fs.Int("rows", 30, "")
	showVersion := fs.Bool("version", false, "")
	positional, err := parse(fs, args)
	if err != nil {
		return err
	}
	if *showVersion {
		fmt.Println("tour", versionString())
		return nil
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	if len(positional) == 1 && positional[0] == "setup" {
		return runSetup(cfg)
	}
	if len(positional) > 1 {
		fs.Usage()
		os.Exit(2)
	}

	lang, _ := i18n.Detect()
	if cfg != nil {
		lang, _ = i18n.Parse(cfg.Lang)
	}
	if *langFlag != "" {
		l, ok := i18n.Parse(*langFlag)
		if !ok {
			return fmt.Errorf("unsupported language: %s", *langFlag)
		}
		lang = l
	}
	i18n.Set(lang)

	dir, err := filepath.Abs(*repoDir)
	if err != nil {
		return err
	}
	repo := git.Open(dir)

	// Play a tour file
	if len(positional) == 1 && strings.HasSuffix(positional[0], ".json") {
		t, err := tour.Load(positional[0])
		if err != nil {
			return err
		}
		if repo.RevParse("HEAD") == "" {
			return fmt.Errorf("%s", i18n.T("err.notRepo", dir))
		}
		for _, ref := range []*string{&t.Base, &t.Head} {
			name := repo.Resolve(*ref)
			if name == "" {
				return fmt.Errorf("%s", i18n.T("err.badRef", *ref))
			}
			*ref = name
		}
		ctx := render.Ctx{Tour: t, Repo: repo}
		if *dump {
			fmt.Print(ui.Dump(ctx, *cols, *rows, *plain))
			return nil
		}
		if !isTerminal() {
			return fmt.Errorf("%s", i18n.T("needTTY"))
		}
		// Questions work when an LLM is set up. Their answers stay only while the tour is open: a file the user gave is never written
		var asker *ui.Asker
		if cfg != nil {
			if backend, err := backendFor(cfg.LLM); err == nil {
				key := generate.CacheKey(repo, t.Base, t.Head, lang, backend.Name())
				if work, err := generate.WorkDir(key + "-ask"); err == nil {
					asker = &ui.Asker{Backend: backend, Req: generate.Request{Repo: repo, Base: t.Base, Head: t.Head, Lang: lang}, WorkDir: work}
				}
			}
		}
		_, err = tea.NewProgram(ui.NewViewer(ctx, asker)).Run()
		return err
	}

	if repo.RevParse("HEAD") == "" {
		return fmt.Errorf("%s", i18n.T("err.notRepo", dir))
	}
	if !isTerminal() {
		return fmt.Errorf("%s", i18n.T("needTTY"))
	}
	if cfg == nil {
		if err := runSetup(nil); err != nil {
			return err
		}
		if cfg, _ = config.Load(); cfg == nil {
			return nil // quit in the middle of the initial setup
		}
		if *langFlag == "" {
			lang, _ = i18n.Parse(cfg.Lang)
		}
		i18n.Set(lang)
	}
	backend, err := backendFor(cfg.LLM)
	if err != nil {
		return err
	}

	var app *ui.App
	if len(positional) == 0 {
		base := repo.DefaultBranch()
		if base == "" {
			return fmt.Errorf("%s", i18n.T("err.noBase"))
		}
		app = ui.NewPicker(repo, base, lang, backend)
	} else {
		base, head, err := resolve(repo, positional[0])
		if err != nil {
			return err
		}
		app = ui.NewGenerating(repo, base, head, lang, backend)
	}
	_, err = tea.NewProgram(app).Run()
	return err
}

// resolve turns the arguments into the two commits to compare.
//
//	a..b / a...b   from the merge base of a up to b
//	branch name    from the default branch up to that branch (local or remote-tracking)
//	commit         that single commit (from its parent to the commit)
func resolve(repo *git.Repo, arg string) (string, string, error) {
	if base, head, ok := strings.Cut(strings.Replace(arg, "...", "..", 1), ".."); ok {
		for _, r := range []string{base, head} {
			if repo.RevParse(r) == "" {
				return "", "", fmt.Errorf("%s", i18n.T("err.badRef", r))
			}
		}
		return base, head, nil
	}
	// A remote-tracking branch (e.g. origin/feature/x, for reviewing someone else's branch) is a branch too
	if repo.RevParse("refs/heads/"+arg) != "" || repo.RevParse("refs/remotes/"+arg) != "" {
		base := repo.DefaultBranch()
		if base == "" {
			return "", "", fmt.Errorf("%s", i18n.T("err.noBase"))
		}
		return base, arg, nil
	}
	if repo.RevParse(arg) == "" {
		return "", "", fmt.Errorf("%s", i18n.T("err.badRef", arg))
	}
	return arg + "^", arg, nil
}

func backendFor(llm string) (generate.Backend, error) {
	switch llm {
	case "claude-code":
		return generate.ClaudeCode{}, nil
	case "codex":
		return generate.Codex{}, nil
	case "gemini":
		return generate.Gemini{}, nil
	case "opencode":
		return generate.OpenCode{}, nil
	case "copilot":
		return generate.Copilot{}, nil
	case "":
		return nil, fmt.Errorf("%s", i18n.T("err.noLLM"))
	}
	name := llm
	for _, o := range ui.DetectLLMs() {
		if o.ID == llm {
			name = o.Name
		}
	}
	return nil, fmt.Errorf("%s", i18n.T("err.unsupported", name))
}

func runSetup(current *config.Config) error {
	if !isTerminal() {
		return fmt.Errorf("%s", i18n.T("needTTY"))
	}
	lang, from := i18n.Detect()
	llm := ""
	if current != nil {
		lang, _ = i18n.Parse(current.Lang)
		from, llm = "", current.LLM
	}
	s := ui.NewSetup(lang, from, llm, ui.DetectLLMs())
	if _, err := tea.NewProgram(s).Run(); err != nil {
		return err
	}
	if !s.Done {
		return nil
	}
	path, err := config.Save(&config.Config{Lang: string(s.Lang()), LLM: s.LLM()})
	if err != nil {
		return err
	}
	i18n.Set(s.Lang())
	fmt.Println(i18n.T("setup.saved", path))
	return nil
}
