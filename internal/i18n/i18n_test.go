package i18n

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

func TestDetect(t *testing.T) {
	cases := []struct {
		all, lang string
		want      Lang
	}{
		{"", "ja_JP.UTF-8", JA},
		{"", "en_US.UTF-8", EN},
		{"", "zh_CN.UTF-8", ZH},
		{"", "zh_TW.UTF-8", ZHHant},
		{"", "ZH_HK.UTF-8", ZHHant},
		{"", "zh_MO.UTF-8", ZHHant},
		{"", "zh-Hant.UTF-8", ZHHant},
		{"", "zh_CN.UTF-8@pinyin", ZH},
		{"", "zh", ZH},
		{"zh_SG.UTF-8", "en_US.UTF-8", ZH},
		{"en_US.UTF-8", "zh_CN.UTF-8", EN},
		{"", "es_MX.UTF-8", ES},
		{"", "ru_RU.UTF-8", RU},
		{"ru_UA.UTF-8", "en_US.UTF-8", RU},
		{"", "fr_FR.UTF-8", EN}, // unsupported languages fall back to English
		{"C", "ja_JP.UTF-8", JA},
		{"en_US.UTF-8", "ja_JP.UTF-8", EN}, // LC_ALL takes precedence
		{"", "", EN},
	}
	for _, c := range cases {
		t.Setenv("LC_ALL", c.all)
		t.Setenv("LC_MESSAGES", "")
		t.Setenv("LANG", c.lang)
		if got, _ := Detect(); got != c.want {
			t.Errorf("LC_ALL=%q LANG=%q: got %s, want %s", c.all, c.lang, got, c.want)
		}
	}
}

// TestTranslatedMessages checks that every language has every message, with the same format verbs as English.
func TestTranslatedMessages(t *testing.T) {
	placeholder := regexp.MustCompile(`%[^a-zA-Z%]*[a-zA-Z%]`)
	for _, lang := range Langs[1:] {
		if len(messages[lang]) != len(messages[EN]) {
			t.Errorf("%s: key count = %d, want %d", lang, len(messages[lang]), len(messages[EN]))
		}
		for key, en := range messages[EN] {
			translated, ok := messages[lang][key]
			if !ok {
				t.Errorf("%s: missing message: %s", lang, key)
				continue
			}
			if translated == "" {
				t.Errorf("%s: empty message: %s", lang, key)
			}
			want := placeholder.FindAllString(en, -1)
			got := placeholder.FindAllString(translated, -1)
			if !slices.Equal(got, want) {
				t.Errorf("%s %s: placeholders = %v, want %v", lang, key, got, want)
			}
		}
	}
}

// TestParse checks the Chinese aliases and keeps the English and Japanese behavior.
func TestParse(t *testing.T) {
	for _, c := range []struct {
		input string
		want  Lang
		ok    bool
	}{
		{"en", EN, true}, {"EN", EN, true}, {"ja", JA, true}, {"JA", JA, true},
		{"zh", ZH, true}, {"ZH", ZH, true}, {"zh-CN", ZH, true}, {"ZH-cn", ZH, true},
		{"zh_CN", ZH, true}, {"ZH_cn", ZH, true}, {"zh-Hans", ZH, true}, {"ZH-hANS", ZH, true}, {"zh-SG", ZH, true},
		{"zh-Hant", ZHHant, true}, {"ZH-HANT", ZHHant, true}, {"zh-TW", ZHHant, true}, {"zh_tw", ZHHant, true},
		{"zh-HK", ZHHant, true}, {"zh_MO", ZHHant, true}, {"zh-Hant-HK", ZHHant, true}, {"zh-Hans-TW", ZH, true},
		{"zhx", EN, false}, {"es", ES, true}, {"ES", ES, true}, {"ru", RU, true}, {"RU", RU, true},
		{"", EN, false}, {"fr", EN, false}, {"en-US", EN, false}, {"ja_JP", EN, false},
	} {
		if got, ok := Parse(c.input); got != c.want || ok != c.ok {
			t.Errorf("Parse(%q) = %s, %t; want %s, %t", c.input, got, ok, c.want, c.ok)
		}
	}
}

// TestLanguageChoices fixes the order of the choices in setup and the names of Chinese, Spanish and Russian.
func TestLanguageChoices(t *testing.T) {
	if !slices.Equal(Langs, []Lang{EN, JA, ZH, ZHHant, ES, RU}) || ZH.Name() != "简体中文" || ZHHant.Name() != "繁體中文" || ES.Name() != "Español" || RU.Name() != "Русский" {
		t.Fatalf("choices = %v, names = %q, %q, %q, %q", Langs, ZH.Name(), ZHHant.Name(), ES.Name(), RU.Name())
	}
}

// TestDetectMessages checks that LC_MESSAGES wins over LANG and is reported as the source.
func TestDetectMessages(t *testing.T) {
	t.Setenv("LC_ALL", "")
	t.Setenv("LC_MESSAGES", "zh_TW.UTF-8")
	t.Setenv("LANG", "ja_JP.UTF-8")
	if got, source := Detect(); got != ZHHant || source != "LC_MESSAGES=zh_TW.UTF-8" {
		t.Fatalf("Detect() = %s, %q", got, source)
	}
}

// Every language needs the messages in use, so the screen never shows a raw key.
func TestUsedKeysExist(t *testing.T) {
	re := regexp.MustCompile(`i18n\.T\("([a-zA-Z.]+)"|"(setup\.[a-zA-Z]+)(?:[:|"])`)
	err := filepath.WalkDir("../..", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, m := range re.FindAllStringSubmatch(string(b), -1) {
			key := m[1] + m[2]
			for _, lang := range Langs {
				if _, ok := messages[lang][key]; !ok {
					t.Errorf("%s: %s message missing: %s", path, lang, key)
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
