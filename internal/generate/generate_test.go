package generate

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/init-co-jp/diff-tour/internal/git"
	"github.com/init-co-jp/diff-tour/internal/i18n"
	"github.com/init-co-jp/diff-tour/internal/tour"
)

func TestExtractJSON(t *testing.T) {
	cases := map[string]string{
		`{"a":1}`:                          `{"a":1}`,
		"```json\n{\"a\":1}\n```":          `{"a":1}`,
		"notes first\n\n{\"a\":{\"b\":2}}": `{"a":{"b":2}}`,
		"=== 下調べ ===\n- {x} を見る\n=== ツアー JSON ===\n{\"a\":1}": `{"a":1}`,
	}
	for in, want := range cases {
		if got := string(extractJSON(in)); got != want {
			t.Errorf("extractJSON(%q) = %q, want %q", in, got, want)
		}
	}
	if extractJSON("no json here") != nil {
		t.Error("expected nil")
	}
}

func TestParseReportsProblems(t *testing.T) {
	req := Request{Base: "main", Head: "feature"}
	if _, problems := parse(req, "not json"); len(problems) != 1 {
		t.Errorf("problems = %v", problems)
	}
	// Fields not in the format are treated as typos and must be fixed.
	if _, problems := parse(req, `{"version":1,"title":"x","base":"main","head":"feature","chapters":[],"extra":1}`); len(problems) != 1 || !strings.Contains(problems[0], "extra") {
		t.Errorf("problems = %v", problems)
	}
}

func TestBuildPrompt(t *testing.T) {
	for _, lang := range i18n.Langs {
		p, err := buildPrompt(Request{Base: "main", Head: "feature/x", Lang: lang})
		if err != nil {
			t.Fatal(err)
		}
		if languageRules[lang] == "" || !strings.Contains(p, languageRules[lang]) {
			t.Errorf("%s: language rule missing", lang)
		}
		if strings.Contains(p, "{{") {
			t.Errorf("%s: template left unexpanded", lang)
		}
		if !strings.Contains(p, "`feature/x`") {
			t.Errorf("%s: target missing", lang)
		}
	}
}

func TestWriteLessons(t *testing.T) {
	dir := t.TempDir()
	if err := writeLessons(dir); err != nil {
		t.Fatal(err)
	}
	// Every lesson file the prompts name must be in the workspace
	prompt, err := buildPrompt(Request{Base: "main", Head: "feature", Lang: i18n.EN})
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range regexp.MustCompile("`(lessons/[^`]+\\.md)`").FindAllStringSubmatch(prompt, -1) {
		if _, err := os.Stat(filepath.Join(dir, m[1])); err != nil {
			t.Errorf("prompt names %s, which is not in the workspace", m[1])
		}
	}
}

// TestAskPromptLanguages checks that answers to a question use the chosen language too.
func TestAskPromptLanguages(t *testing.T) {
	q := Question{Tour: &tour.Tour{Chapters: []tour.Chapter{{Steps: []tour.Step{{Title: "当前步骤"}}}}}, Text: "为什么？"}
	for _, lang := range i18n.Langs {
		p, err := askPrompt(Request{Base: "main", Head: "feature", Lang: lang}, q)
		if err != nil {
			t.Fatal(err)
		}
		if languageRules[lang] == "" || !strings.Contains(p, languageRules[lang]) || strings.Contains(p, "{{") {
			t.Errorf("%s: missing or unexpanded language rule", lang)
		}
	}
}

// TestCacheKeyLanguages checks that tours in different languages do not share a cache entry.
func TestCacheKeyLanguages(t *testing.T) {
	dir := t.TempDir()
	run(t, dir, "init", "-q", "-b", "main")
	run(t, dir, "commit", "--allow-empty", "-q", "-m", "base")
	seen := map[string]i18n.Lang{}
	for _, lang := range i18n.Langs {
		key := CacheKey(git.Open(dir), "main", "main", lang, "test")
		if previous, ok := seen[key]; ok {
			t.Errorf("%s and %s share cache key %s", previous, lang, key)
		}
		seen[key] = lang
	}
}
