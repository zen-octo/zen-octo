// Package paint renders one row of a diff. Every exported function is pure:
// the same line at the same width gives the same string. Folding, scroll,
// side-by-side layout, hunk grouping and review state belong to the caller.
package paint

import (
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/praxis-labs-io/zen-octo/internal/tui/syntax"
	"github.com/praxis-labs-io/zen-octo/internal/tui/theme"
)

// defaultTabWidth is what a tab expands to when a Painter names no width. A raw
// tab is a variable number of cells, and one anywhere in a line puts every
// column after it out of step with the line above.
const defaultTabWidth = 4

// Kind is the side of the change a line belongs to.
type Kind int

const (
	Context Kind = iota
	Added
	Removed
)

// Line is one row ready to paint. Old and New are line numbers; 0 means that
// side has none, and the column is still held open so the marker beside it
// does not move.
type Line struct {
	Kind     Kind
	Old, New int
	Tokens   []syntax.Token

	// Fill beats the kind's tint, nil uses it. A cursor and a lit card are the
	// caller's state and both have to win.
	Fill color.Color

	// Bar paints the leading cell, and nil leaves it blank. A tint says a row is
	// lit and a bar says where it starts, which is what an eye follows.
	Bar color.Color
}

// Painter paints rows from one theme.
type Painter struct {
	Theme    theme.Theme
	TabWidth int // 0 means 4
}

// Line is one row of code: the two line numbers, the marker, and the
// highlighted source over a tint of the change it is part of.
//
// The tint is painted cell by cell and the row is padded out to the full width.
// Every styled run ends in a reset that clears the background with it, so a
// joined row wrapped in the background style afterwards would carry it only as
// far as the first token.
//
// Anything wider than the pane is clipped rather than wrapped: a wrapped row
// puts its tail under the gutter and every row below it out of step.
func (p Painter) Line(l Line, gutter, width int) string {
	marker, c, tint := p.weight(l.Kind)
	if l.Fill != nil {
		tint = l.Fill
	}

	base := background(lipgloss.NewStyle(), tint)
	kind := base.Foreground(c)
	faint := base.Foreground(p.Theme.Subtle)

	oldNum, newNum := faint, faint
	switch l.Kind {
	case Added:
		newNum = kind
	case Removed:
		oldNum = kind
	}

	row := Lead(l.Bar, base) +
		oldNum.Render(number(l.Old, gutter)) + base.Render(" ") +
		newNum.Render(number(l.New, gutter)) + base.Render(" ") +
		kind.Render(marker) + base.Render(" ") + p.code(l.Tokens, base)

	if w := lipgloss.Width(row); w > width {
		return Clip(row, width, faint)
	} else if tint != nil {
		// Only a row with a background has one to run out. A context line with
		// no fill is left short, and the pane's own padding finishes it.
		row += base.Render(strings.Repeat(" ", width-w))
	}
	return row
}

// Half paints one column of a side-by-side row. A half carries one number, so
// whichever of Old and New is set shows, and a zero Line paints a blank column.
func (p Painter) Half(l Line, gutter, width int) string {
	marker, c, tint := p.weight(l.Kind)
	if l.Fill != nil {
		tint = l.Fill
	}

	base := background(lipgloss.NewStyle(), tint)
	kind := base.Foreground(c)

	num := base.Foreground(p.Theme.Subtle)
	if l.Kind != Context {
		num = kind
	}

	row := Lead(l.Bar, base) + num.Render(number(max(l.Old, l.New), gutter)) +
		base.Render(" ") + kind.Render(marker) + base.Render(" ") +
		p.code(l.Tokens, base)

	if w := lipgloss.Width(row); w > width {
		return Clip(row, width, base.Foreground(p.Theme.Subtle))
	} else if w < width {
		// Padded whether or not it is tinted, where Line leaves that to the pane.
		// A short half puts the column beside it out of step.
		row += base.Render(strings.Repeat(" ", width-w))
	}
	return row
}

// weight is the marker, foreground and tint one kind of line is painted in.
func (p Painter) weight(k Kind) (string, color.Color, color.Color) {
	switch k {
	case Added:
		return "+", p.Theme.Success, p.Theme.AddedBackground
	case Removed:
		return "−", p.Theme.Error, p.Theme.RemovedBackground
	}
	return " ", p.Theme.Subtle, nil
}

// Header is the @@ line ready to paint.
type Header struct {
	Text string

	// Marker goes in the column Line puts + and − in, so a mark on a heading
	// lines up with the change marks under it. "" leaves the column blank. A
	// two-cell marker takes the space after it and anything wider is clipped,
	// so the text starts at the code column whatever the caller passes.
	Marker string

	// Badge is a second glyph, left of the marker, for a state the heading
	// carries whether or not the cursor is on it. It takes blank indent.
	Badge string

	// BadgeColor paints the badge, and nil paints it in Accent. A ladder of
	// states needs more than one weight; a cursor is one thing at one weight.
	BadgeColor color.Color

	// TextColor paints the @@ line and the marker, nil paints both Accent. A
	// column of headings at one weight cannot say which one the reader is in.
	TextColor color.Color

	// Fill is the row's background, and nil paints none. It is the caller's
	// state the same way Line.Fill is.
	Fill color.Color

	// Bar is the leading cell the same way Line.Bar is.
	Bar color.Color
}

// HunkHeader is the @@ line over a unified row, indented to the code column so
// it sits above the source it introduces.
func (p Painter) HunkHeader(h Header, gutter, width int) string {
	return p.header(h, CodeColumn(gutter), width)
}

// HalfHeader is HunkHeader over a side-by-side row, where the source starts one
// number column in rather than two. A caller cannot pass the wrong one.
func (p Painter) HalfHeader(h Header, gutter, width int) string {
	return p.header(h, HalfColumn(gutter), width)
}

// header is the @@ line indented to wherever the source under it starts.
func (p Painter) header(h Header, code, width int) string {
	base := background(lipgloss.NewStyle(), h.Fill)
	accent := base.Foreground(p.Theme.Accent)

	// The marker takes the text's colour rather than Accent. It is part of what
	// the heading says about itself, and a lit caret on a dimmed line reads odd.
	text := accent
	if h.TextColor != nil {
		text = base.Foreground(h.TextColor)
	}

	badge := accent
	if h.BadgeColor != nil {
		badge = base.Foreground(h.BadgeColor)
	}

	row := Lead(h.Bar, base) +
		base.Render(strings.Repeat(" ", max(0, code-2*markerSlot-1))) +
		slot(h.Badge, base, badge) + slot(h.Marker, base, text) +
		text.Render(h.Text)

	if w := lipgloss.Width(row); w > width {
		return Clip(row, width, base.Foreground(p.Theme.Subtle))
	} else if h.Fill != nil {
		row += base.Render(strings.Repeat(" ", width-w))
	}
	return row
}

// BarGlyph marks the row the cursor is on. It goes in the leading cell every row
// already holds open, so a row gains no width by being the one under the cursor.
//
// Exported so the rail and the tests reading either pane name it rather than
// repeating the rune, which is a cutset in three helpers and easy to miss one of.
const BarGlyph = "▌"

// Lead is a row's first cell: the bar, or the blank every other row keeps there.
//
// Exported for the details rail, which marks its cursor line the same way and
// holds a gutter of its own for it. One glyph, so a reader crossing from the
// diff to the rail is not asked to learn a second mark for the same fact.
func Lead(bar color.Color, base lipgloss.Style) string {
	if bar == nil {
		return base.Render(" ")
	}
	return base.Foreground(bar).Render(BarGlyph)
}

// slot renders one glyph in a fixed pair of columns, blank when there is none.
// A wider glyph eats the space after it rather than pushing the text along.
func slot(glyph string, base, on lipgloss.Style) string {
	if glyph == "" {
		return base.Render(strings.Repeat(" ", markerSlot))
	}
	g := lipgloss.NewStyle().MaxWidth(markerSlot).Render(glyph)
	return on.Render(g) + base.Render(strings.Repeat(" ", markerSlot-lipgloss.Width(g)))
}

// code renders one row's tokens over the style the row is painted in. Every
// token takes only a foreground from it, so whatever is behind the row survives
// all the way across.
func (p Painter) code(tokens []syntax.Token, base lipgloss.Style) string {
	tab := strings.Repeat(" ", p.tabWidth())

	var b strings.Builder
	for _, t := range tokens {
		text := strings.ReplaceAll(t.Text, "\t", tab)
		if t.Color == nil {
			b.WriteString(base.Render(text))
			continue
		}
		b.WriteString(base.Foreground(t.Color).Render(text))
	}
	return b.String()
}

func (p Painter) tabWidth() int {
	if p.TabWidth <= 0 {
		return defaultTabWidth
	}
	return p.TabWidth
}

// background applies a color the theme may not define. A nil one leaves the
// terminal's own showing, which is what keeps a transparent one transparent.
func background(s lipgloss.Style, c color.Color) lipgloss.Style {
	if c == nil {
		return s
	}
	return s.Background(c)
}
