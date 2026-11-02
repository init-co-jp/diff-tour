package ui

import (
	"errors"
	"os/exec"

	tea "charm.land/bubbletea/v2"

	"github.com/init-co-jp/diff-tour/internal/generate"
	"github.com/init-co-jp/diff-tour/internal/i18n"
	"github.com/init-co-jp/diff-tour/internal/text"
)

// LLMOption is a candidate LLM for writing tours.
type LLMOption struct {
	ID        string
	Name      string
	Available bool
	// Status is the i18n key for "found" or for why it can't be used
	Status string
}

// DetectLLMs looks for the supported LLM CLIs. Ones that aren't installed are returned too, so the screen can say so.
func DetectLLMs() []LLMOption {
	command := func(id, name, bin string) LLMOption {
		if _, err := exec.LookPath(bin); err != nil {
			return LLMOption{ID: id, Name: name, Status: "setup.notFound"}
		}
		return LLMOption{ID: id, Name: name, Available: true, Status: "setup.found"}
	}
	return []LLMOption{
		command("claude-code", "Claude Code", "claude"),
		command("codex", "Codex CLI", "codex"),
		command("gemini", "Gemini CLI", "gemini"),
		openCode(),
		command("copilot", "Copilot CLI", "copilot"),
		pi(),
	}
}

// openCode is found only as OpenCode 2. OpenCode 1 is reported as too old.
func openCode() LLMOption {
	o := LLMOption{ID: "opencode", Name: "OpenCode", Status: "setup.notFound"}
	switch _, err := generate.FindOpenCode(); {
	case err == nil:
		o.Available, o.Status = true, "setup.found"
	case errors.Is(err, generate.ErrOpenCodeV1):
		o.Status = "setup.needsV2"
	}
	return o
}

// pi is found only in a version tour has been checked against (1.5 or a later 1.x).
func pi() LLMOption {
	o := LLMOption{ID: "pi", Name: "Pi", Status: "setup.notFound"}
	switch _, err := generate.FindPi(); {
	case err == nil:
		o.Available, o.Status = true, "setup.found"
	case errors.Is(err, generate.ErrPiVersion):
		o.Status = "setup.piVersion"
	}
	return o
}

// Setup is the first-run screen for choosing the language and LLM. The recommended options are preselected, so Enter alone moves on.
type Setup struct {
	detectedFrom string
	lang         int
	llms         []LLMOption
	llm          int // -1 means nothing is usable
	focus        int // 0: language, 1: LLM
	width        int
	Done         bool
}

func NewSetup(current i18n.Lang, detectedFrom, currentLLM string, llms []LLMOption) *Setup {
	s := &Setup{detectedFrom: detectedFrom, llms: llms, llm: -1}
	for i, l := range i18n.Langs {
		if l == current {
			s.lang = i
		}
	}
	for i, o := range llms {
		if o.Available && (s.llm < 0 || o.ID == currentLLM) {
			s.llm = i
		}
	}
	i18n.Set(current)
	return s
}

func (s *Setup) Lang() i18n.Lang { return i18n.Langs[s.lang] }

func (s *Setup) LLM() string {
	if s.llm < 0 {
		return ""
	}
	return s.llms[s.llm].ID
}

func (s *Setup) Init() tea.Cmd { return nil }

// moveLLM moves the selection, skipping unusable candidates.
func (s *Setup) moveLLM(delta int) {
	for i := s.llm + delta; i >= 0 && i < len(s.llms); i += delta {
		if s.llms[i].Available {
			s.llm = i
			return
		}
	}
}

func (s *Setup) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		s.width = msg.Width
	case tea.KeyPressMsg:
		switch msg.String() {
		case "up", "k":
			if s.focus == 0 {
				s.lang = max(0, s.lang-1)
			} else if s.llm >= 0 {
				s.moveLLM(-1)
			}
		case "down", "j":
			if s.focus == 0 {
				s.lang = min(len(i18n.Langs)-1, s.lang+1)
			} else if s.llm >= 0 {
				s.moveLLM(1)
			}
		case "enter", "right", "space":
			if s.focus == 0 {
				s.focus = 1
			} else {
				s.Done = true
				return s, tea.Quit
			}
		case "left", "backspace":
			s.focus = 0
		case "q", "ctrl+c":
			return s, tea.Quit
		}
		// when the language is changed, switch this screen's messages on the spot
		i18n.Set(s.Lang())
	}
	return s, nil
}

func (s *Setup) View() tea.View {
	width := s.width
	if width == 0 {
		width = 80
	}
	inner := width - 2
	var lines []text.Line
	add := func(l text.Line) { lines = append(lines, text.Concat(text.Line{{Text: " "}}, text.Fit(l, inner, ""))) }
	option := func(selected, focused bool, label, note, noteStyle string, enabled bool) {
		cursor, style := "  ", ""
		if selected {
			cursor = "▸ "
			if focused {
				style = "1;36"
			} else {
				style = "36"
			}
		}
		if !enabled {
			style = "90"
		}
		l := text.Line{{Text: " " + cursor, Style: "36"}, {Text: label, Style: style}}
		if note != "" {
			l = append(l, text.Span{Text: "  " + note, Style: noteStyle})
		}
		add(l)
	}

	add(nil)
	add(text.Line{{Text: " tour ", Style: "30;46"}, {Text: "  " + i18n.T("setup.welcome"), Style: "1"}})
	add(nil)
	headStyle := func(group int) string {
		if s.focus == group {
			return "1"
		}
		return "90"
	}
	add(text.Line{{Text: " " + i18n.T("setup.lang"), Style: headStyle(0)}})
	for i, l := range i18n.Langs {
		note := ""
		if l == i18n.Langs[s.lang] && s.detectedFrom != "" {
			note = i18n.T("setup.detected", s.detectedFrom)
		}
		option(i == s.lang, s.focus == 0, l.Name(), note, "90", true)
	}
	add(nil)
	add(text.Line{{Text: " " + i18n.T("setup.llm"), Style: headStyle(1)}})
	for i, o := range s.llms {
		noteStyle := "90"
		if o.Available {
			noteStyle = "32"
		}
		option(i == s.llm, s.focus == 1, o.Name, i18n.T(o.Status), noteStyle, o.Available)
	}
	add(text.Line{{Text: "   ────────────────", Style: "90"}})
	if s.llm < 0 {
		add(text.Line{{Text: "   " + i18n.T("setup.none"), Style: "33"}})
	} else {
		add(text.Line{{Text: "   " + i18n.T("setup.sendsCode"), Style: "33"}})
	}
	add(nil)
	add(text.Line{{Text: " " + i18n.T("setup.keys"), Style: "90"}})

	view := tea.NewView(join(lines, false))
	view.AltScreen = true
	return view
}
