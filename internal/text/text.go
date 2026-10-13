// Package text treats a terminal line as a sequence of styled strings.
// Japanese characters take two cells each, so all widths are measured in display width.
package text

import (
	"regexp"
	"strings"

	"github.com/mattn/go-runewidth"
)

// Span is a string drawn in a single style. Style holds SGR parameters (e.g. "1;36").
type Span struct {
	Text  string
	Style string
}

// Line is one terminal line.
type Line []Span

// cond counts East Asian ambiguous-width characters, such as box-drawing characters, as one cell.
// go-runewidth treats them as two cells when LANG is ja_JP, but most terminals draw them in one.
var cond = func() *runewidth.Condition {
	c := runewidth.NewCondition()
	c.EastAsianWidth = false
	return c
}()

func RuneWidth(r rune) int { return cond.RuneWidth(r) }

func Width(s string) int { return cond.StringWidth(s) }

func (l Line) Width() int {
	w := 0
	for _, s := range l {
		w += Width(s.Text)
	}
	return w
}

// Join joins the non-empty styles with ";".
func Join(styles ...string) string {
	parts := make([]string, 0, len(styles))
	for _, s := range styles {
		if s != "" {
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, ";")
}

// Append merges s into the last Span if the style matches, and otherwise adds a new Span.
func Append(l Line, s, style string) Line {
	if s == "" {
		return l
	}
	if n := len(l); n > 0 && l[n-1].Style == style {
		l[n-1].Text += s
		return l
	}
	return append(l, Span{s, style})
}

func Concat(lines ...Line) Line {
	var out Line
	for _, l := range lines {
		for _, s := range l {
			out = Append(out, s.Text, s.Style)
		}
	}
	return out
}

// Map returns the line with the style of every Span rewritten.
func (l Line) Map(f func(style string) string) Line {
	out := make(Line, len(l))
	for i, s := range l {
		out[i] = Span{s.Text, f(s.Style)}
	}
	return out
}

// Fit truncates the line to exactly width (ending with … if it overflows), or pads it with spaces in padStyle.
func Fit(l Line, width int, padStyle string) Line {
	if width <= 0 {
		return nil
	}
	overflow := l.Width() > width
	limit := width
	if overflow {
		limit = width - 1
	}
	var out Line
	w := 0
outer:
	for _, s := range l {
		for _, r := range s.Text {
			rw := RuneWidth(r)
			if w+rw > limit {
				break outer
			}
			out = Append(out, string(r), s.Style)
			w += rw
		}
	}
	if overflow {
		out = Append(out, "…", Join(padStyle, "2"))
		w++
	}
	if w < width {
		out = Append(out, strings.Repeat(" ", width-w), padStyle)
	}
	return out
}

// TrimRight removes trailing whitespace from the line.
func TrimRight(l Line) Line {
	out := append(Line(nil), l...)
	for len(out) > 0 {
		last := &out[len(out)-1]
		last.Text = strings.TrimRight(last.Text, " ")
		if last.Text != "" {
			break
		}
		out = out[:len(out)-1]
	}
	return out
}

func ANSI(l Line, plain bool) string {
	var b strings.Builder
	for _, s := range l {
		if s.Style == "" || plain {
			b.WriteString(s.Text)
			continue
		}
		b.WriteString("\x1b[" + s.Style + "m" + s.Text + "\x1b[0m")
	}
	return b.String()
}

var (
	boldRe = regexp.MustCompile(`\*\*.+?\*\*`)
	codeRe = regexp.MustCompile("`[^`]+`")
)

// split returns the parts that match re and the parts that don't, in order.
func split(s string, re *regexp.Regexp) (parts []string, matched []bool) {
	last := 0
	for _, loc := range re.FindAllStringIndex(s, -1) {
		if loc[0] > last {
			parts, matched = append(parts, s[last:loc[0]]), append(matched, false)
		}
		parts, matched = append(parts, s[loc[0]:loc[1]]), append(matched, true)
		last = loc[1]
	}
	if last < len(s) {
		parts, matched = append(parts, s[last:]), append(matched, false)
	}
	return parts, matched
}

// ParseInline interprets only **bold** and `code`. code may appear inside bold.
func ParseInline(s, base string) []Span {
	var spans []Span
	parts, bold := split(s, boldRe)
	for i, part := range parts {
		style := base
		if bold[i] {
			part = part[2 : len(part)-2]
			style = Join(base, "1")
		}
		pieces, code := split(part, codeRe)
		for j, piece := range pieces {
			if code[j] {
				spans = append(spans, Span{piece[1 : len(piece)-1], Join(style, "36")})
			} else {
				spans = append(spans, Span{piece, style})
			}
		}
	}
	return spans
}

// noLineStart lists characters that must not start a line (Chinese and Japanese line-breaking rules). They stick to the preceding unit and wrap together with it.
const noLineStart = "、。，．）》〉］｝〗〙〛」』】〕！？：；・ー…’”)]}.,!?:;"

// unit is the smallest piece Wrap never splits. It can span several styles, e.g. code followed by a period.
type unit struct {
	parts []Span
}

func (u unit) text() string {
	if len(u.parts) == 1 {
		return u.parts[0].Text
	}
	var b strings.Builder
	for _, p := range u.parts {
		b.WriteString(p.Text)
	}
	return b.String()
}

func units(spans []Span) []unit {
	var out []unit
	for _, s := range spans {
		var word strings.Builder
		flush := func() {
			if word.Len() > 0 {
				out = append(out, unit{[]Span{{Text: word.String(), Style: s.Style}}})
				word.Reset()
			}
		}
		for _, r := range s.Text {
			switch {
			case r == '\n' || r == ' ':
				flush()
				out = append(out, unit{[]Span{{Text: string(r), Style: s.Style}}})
			case RuneWidth(r) == 2:
				flush()
				out = append(out, unit{[]Span{{Text: string(r), Style: s.Style}}})
			default:
				word.WriteRune(r)
			}
		}
		flush()
	}
	var merged []unit
	for _, u := range out {
		if n := len(merged); n > 0 {
			prev := &merged[n-1]
			pt := prev.text()
			first := []rune(u.text())[0]
			if pt != "\n" && pt != " " && strings.ContainsRune(noLineStart, first) {
				prev.parts = append(prev.parts, u.parts...)
				continue
			}
		}
		merged = append(merged, u)
	}
	return merged
}

// Wrap wraps the line to fit width. English words are never split, while Japanese can break between any characters.
func Wrap(spans []Span, width int) []Line {
	var lines []Line
	var cur Line
	w := 0
	newline := func() {
		lines = append(lines, TrimRight(cur))
		cur, w = nil, 0
	}
	for _, u := range units(spans) {
		t := u.text()
		if t == "\n" {
			newline()
			continue
		}
		uw := Width(t)
		if t == " " && (w == 0 || w+1 > width) {
			continue
		}
		if w+uw > width && w > 0 {
			newline()
		}
		for _, p := range u.parts {
			if uw <= width {
				cur = Append(cur, p.Text, p.Style)
				continue
			}
			for _, r := range p.Text {
				rw := RuneWidth(r)
				if w+rw > width {
					newline()
				}
				cur = Append(cur, string(r), p.Style)
				w += rw
			}
		}
		if uw <= width {
			w += uw
		}
	}
	return append(lines, TrimRight(cur))
}
