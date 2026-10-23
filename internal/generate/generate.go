// Package generate has an LLM write tours.
package generate

import (
	"bytes"
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/init-co-jp/diff-tour/internal/git"
	"github.com/init-co-jp/diff-tour/internal/i18n"
	"github.com/init-co-jp/diff-tour/internal/render"
	"github.com/init-co-jp/diff-tour/internal/tour"
)

//go:embed prompts/*.md
var prompts embed.FS

// lessons is the reference the prompts point the LLM to: what senior engineers have learned
// about reading, questioning and judging a change. It is copied into the workspace as lessons/.
//
//go:embed lessons/*.md
var lessons embed.FS

// PromptVersion must be bumped whenever the prompts change, so saved tours get regenerated.
const PromptVersion = "v6"

// MaxRepairs is the maximum number of times the LLM is asked to fix errors found by validation.
const MaxRepairs = 2

type Request struct {
	Repo *git.Repo
	Base string
	Head string
	Lang i18n.Lang
}

// Progress is how far generation has got.
type Progress struct {
	Event  *Event
	Phase  string // "reading", "repairing"
	Repair int
}

type Stats struct {
	CostUSD float64
	Repairs int
}

var languageRules = map[i18n.Lang]string{
	i18n.EN:     "Write all text in English. Use short, plain sentences.",
	i18n.JA:     "文章は日本語。です・ます調ではなく、短い常体（「〜している」「〜になる」）で書く。",
	i18n.ZH:     "全部文字使用简体中文，用短句，表达直白。代码标识符、文件名、命令和通用技术术语保留原文。中文与英文、数字之间加空格。用中文全角标点，引号用“”，不用日文用词和「」。",
	i18n.ES:     "Escribe todo el texto en español neutro, con frases cortas y claras. Deja sin traducir los identificadores, los nombres de archivo, los comandos y los términos técnicos de uso habitual.",
	i18n.RU:     "Пиши весь текст по-русски, короткими и простыми фразами, без канцелярита. Не переводи идентификаторы, имена файлов, команды и привычные технические термины. Кавычки — «ёлочки».",
	i18n.ZHHant: "全部文字使用繁體中文，用詞依台灣習慣（程式碼、檔案、資料表、設定）。用短句，表達直白。程式碼識別字、檔名、指令和通用技術術語保留原文。中文與英文、數字之間加空格。用全形標點，引號用「」，不用日文用詞。",
}

func buildPrompt(req Request) (string, error) {
	var b strings.Builder
	for _, name := range []string{"common.md", "planC.md", "format.md", "output-C.md"} {
		src, err := prompts.ReadFile("prompts/" + name)
		if err != nil {
			return "", err
		}
		t, err := template.New(name).Parse(string(src))
		if err != nil {
			return "", err
		}
		if err := t.Execute(&b, map[string]string{"LanguageRule": languageRules[req.Lang]}); err != nil {
			return "", err
		}
		b.WriteString("\n\n")
		if name == "common.md" {
			fmt.Fprintf(&b, "対象の変更: base は `%s`、head は `%s`。JSON の base と head にはこの名前をそのまま書く。\n", req.Base, req.Head)
			if req.Head == git.Worktree {
				b.WriteString("head はコミットではなく、まだコミットしていない変更を含む作業ツリー。commits.txt にあるコミットのあとに、未コミットの変更が続いている。未コミットの変更だけの差分は uncommitted.diff にある。\n")
			}
			b.WriteString("\n")
		}
	}
	return b.String(), nil
}

// separator divides the research notes from the tour JSON (as instructed in prompts/output-C.md).
const separator = "=== ツアー JSON ==="

// extractJSON pulls the JSON out of a reply (stripping research notes, ```json fences and any preamble).
func extractJSON(text string) []byte {
	if _, after, ok := strings.Cut(text, separator); ok {
		text = after
	}
	start, end := strings.Index(text, "{"), strings.LastIndex(text, "}")
	if start < 0 || end < start {
		return nil
	}
	return []byte(text[start : end+1])
}

// parse turns a reply into a tour and checks its format and that the referenced files and lines exist. Problems are returned as a list.
func parse(req Request, text string) (*tour.Tour, []string) {
	raw := extractJSON(text)
	if raw == nil {
		return nil, []string{"the reply does not contain a JSON object"}
	}
	var t tour.Tour
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&t); err != nil {
		return nil, []string{"invalid JSON: " + err.Error()}
	}
	// Pin base and head to what was requested, so a misspelled name cannot make it read a different commit.
	t.Base, t.Head = req.Base, req.Head
	if err := t.Validate(); err != nil {
		return &t, []string{err.Error()}
	}
	t.DropRevealingAsks()
	return &t, render.Check(render.Ctx{Tour: &t, Repo: req.Repo})
}

func writeLessons(dir string) error {
	sub, err := fs.Sub(lessons, "lessons")
	if err != nil {
		return err
	}
	return os.CopyFS(filepath.Join(dir, "lessons"), sub)
}

func readPrompt(name string) string {
	b, err := prompts.ReadFile("prompts/" + name)
	if err != nil {
		panic(err) // it's an embedded file, so it can't be missing
	}
	return string(b)
}

// repairPrompt asks for only the errors found by validation to be fixed. The JSON before fixing is placed in draft.txt in the working directory.
func repairPrompt(problems []string) string {
	return strings.Join([]string{
		readPrompt("fix.md"),
		"## 検証の結果\n- " + strings.Join(problems, "\n- "),
		readPrompt("format.md"),
		readPrompt("fix-output.md"),
	}, "\n\n")
}

// Generate has the tour written. If validation finds problems, it asks for fixes up to MaxRepairs times.
func Generate(ctx context.Context, backend Backend, req Request, workDir string, progress func(Progress)) (*tour.Tour, Stats, error) {
	var stats Stats
	ws, err := PrepareWorkspace(req.Repo, req.Base, req.Head, workDir)
	if err != nil {
		return nil, stats, err
	}
	defer ws.Remove()
	if err := writeLessons(ws.Dir); err != nil {
		return nil, stats, err
	}

	prompt, err := buildPrompt(req)
	if err != nil {
		return nil, stats, err
	}
	onEvent := func(e Event) { progress(Progress{Event: &e, Phase: "reading", Repair: stats.Repairs}) }
	progress(Progress{Phase: "reading"})
	res, err := backend.Run(ctx, ws.Dir, prompt, onEvent)
	stats.CostUSD += res.CostUSD
	if err != nil {
		return nil, stats, err
	}
	// If the reply has no JSON at all (e.g. it stopped midway), regenerate once instead of asking for a fix.
	if extractJSON(res.Text) == nil {
		res, err = backend.Run(ctx, ws.Dir, prompt, onEvent)
		stats.CostUSD += res.CostUSD
		if err != nil {
			return nil, stats, err
		}
	}
	t, err := settle(ctx, backend, req, ws.Dir, res.Text, &stats, progress, onEvent)
	return t, stats, err
}

// settle parses a reply and, while validation finds problems, asks for fixes (up to MaxRepairs times per reply).
func settle(ctx context.Context, backend Backend, req Request, dir, text string, stats *Stats, progress func(Progress), onEvent func(Event)) (*tour.Tour, error) {
	for repairs := 0; ; repairs++ {
		t, problems := parse(req, text)
		if len(problems) == 0 {
			return t, nil
		}
		if repairs >= MaxRepairs {
			return nil, &InvalidTourError{Problems: problems}
		}
		stats.Repairs++
		progress(Progress{Phase: "repairing", Repair: repairs + 1})
		draft := extractJSON(text)
		if draft == nil {
			draft = []byte(text)
		}
		if err := os.WriteFile(filepath.Join(dir, "draft.txt"), draft, 0o644); err != nil {
			return nil, err
		}
		res, err := backend.Run(ctx, dir, repairPrompt(problems), onEvent)
		stats.CostUSD += res.CostUSD
		if err != nil {
			return nil, err
		}
		text = res.Text
	}
}

// InvalidTourError reports that format or reference errors remained even after asking for fixes.
type InvalidTourError struct{ Problems []string }

func (e *InvalidTourError) Error() string {
	return "the generated tour is still invalid: " + strings.Join(e.Problems, "; ")
}

// CacheKey is the same for the same change, language, prompts and LLM.
func CacheKey(repo *git.Repo, base, head string, lang i18n.Lang, backend string) string {
	h := sha256.New()
	for _, s := range []string{repo.MergeBase(base, head), repo.RevParse(head), string(lang), PromptVersion, backend} {
		h.Write([]byte(s))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))[:24]
}

func cacheDir() (string, error) {
	dir, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "tour"), nil
}

// CachePath is where a saved tour is stored.
func CachePath(key string) (string, error) {
	dir, err := cacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "tours", key+".json"), nil
}

// WorkDir is where the working directory lives (removed once generation finishes).
func WorkDir(key string) (string, error) {
	dir, err := cacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "work", key), nil
}

// LoadCached reads a saved tour, or returns nil if there is none.
func LoadCached(key string) (*tour.Tour, error) {
	path, err := CachePath(key)
	if err != nil {
		return nil, err
	}
	t, err := tour.Load(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	return t, err
}

func SaveCached(key string, t *tour.Tour) error {
	path, err := CachePath(key)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o644)
}
