package prview

import (
	"image/color"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/zen-octo/zen-octo/internal/gh"
	"github.com/zen-octo/zen-octo/internal/store"
	"github.com/zen-octo/zen-octo/internal/tui/comp"
)

// gutterMin is the narrowest a line-number column gets. A file under ten lines
// still reads better with the two columns lined up against its neighbours.
const gutterMin = 2

// tabWidth is what a tab expands to. A raw tab is a variable number of cells,
// and one anywhere in a line puts every column after it out of step with the
// line above.
const tabWidth = 4

// threadIndent sets a review thread in from the code it hangs off, so a card
// full of prose does not read as another hunk.
const threadIndent = 3

// anchor is where a review thread hangs: a line on one side of the diff.
type anchor struct {
	side gh.DiffSide
	line int
}

// fileSpan is where one file's block sits in the diff body. It is what lets the
// tree scroll to a file and the cursor follow the diff back.
type fileSpan struct {
	key   string
	start int
	end   int
}

// blockKey identifies a rendered file block. Folding changes what a block is,
// so the same file has one under each state.
type blockKey struct {
	key    string
	folded bool
}

// blockState is everything outside a single file that its block is rendered
// against. A change to either retires the whole cache.
//
// folds counts how many times a <details> block has been folded or unfolded.
// Which ones are open is a map, and a count says the same thing to a cache key
// without the cache having to hold the map: a toggle moves it by one either way,
// so no two consecutive states share a number.
type blockState struct {
	width int
	folds int
}

// diffBody is one rendered diff: where each file's block sits inside it, and
// the blocks themselves. Each tab that shows a diff keeps one, because the
// render is the same over different files.
//
// The blocks are cached because moving a cursor one row repaints the diff, and
// rendering a block tokenises the whole file: without this a single keystroke
// costs the diff twice over. at is what the cache was built against; anything
// else invalidates the lot.
type diffBody struct {
	spans  []fileSpan
	blocks map[blockKey]string
	at     blockState

	// threads is whether review threads hang off these lines. They are written
	// against the pull request's head, and the same line number in an older
	// commit is different code, so a commit's diff carries none.
	threads bool

	// lead is what sits above the first block. The spans are what a jump lands
	// on, and they have to clear whatever the tab put in front of them.
	lead int
}

// filesBody is the diff. A diff that has loaded once keeps rendering through a
// failed refetch; the root raises a toast for that.
func (m *Model) filesBody() string {
	switch {
	case m.files.Loaded:
		return m.renderDiff(m.rows, m.files, &m.diff)
	case m.files.Status == store.StatusFailed:
		return m.faint().Render("Could not load the diff: " + m.files.Err.Error())
	}
	return m.spinner.Render("Loading the diff")
}

// renderDiff renders every file in a set of rows, and records where each one
// starts so a column beside it can scroll to it.
func (m *Model) renderDiff(rows []row, res store.Files, d *diffBody) string {
	if len(res.Files) == 0 {
		return m.faint().Render("No files changed.")
	}

	width := m.bodyWidth()
	d.spans = d.spans[:0]

	// A fold only reaches blocks with review threads in them. The Commits tab's
	// diff carries none, so counting folds against it retires the whole cache
	// and re-tokenises a commit for a keypress that changed nothing it renders.
	state := blockState{width: width}
	if d.threads {
		state.folds = m.folds
	}
	if d.blocks == nil || d.at != state {
		d.blocks, d.at = make(map[blockKey]string, len(rows)), state
	}

	blocks := make([]string, 0, len(rows))
	at := d.lead
	for _, r := range rows {
		if r.file == nil {
			continue
		}

		bk := blockKey{key: r.key, folded: r.folded}
		block, ok := d.blocks[bk]
		if !ok {
			block = m.fileBlock(*r.file, r.folded, width, d.threads)
			d.blocks[bk] = block
		}
		blocks = append(blocks, block)

		lines := strings.Count(block, "\n") + 1
		d.spans = append(d.spans, fileSpan{key: r.key, start: at, end: at + lines})
		// The join puts a blank line after every block but the last, and the
		// next one starts on the line after that.
		at += lines + 1
	}

	if note := overflow(res); note != "" {
		blocks = append(blocks, wrap(m.faint().Render(note), width))
	}
	return strings.Join(blocks, "\n\n")
}

// overflow says what the response did not reach. A pull request's diff is
// measured against the file count it carries, and a commit's has no count to
// measure against, so that one says there is more without saying how much.
func overflow(res store.Files) string {
	switch {
	case res.MoreFiles > 0:
		return comp.Plural(res.MoreFiles, "more file") + " on GitHub"
	case res.Truncated:
		return "More files on GitHub"
	}
	return ""
}

// fileBlock is one file in a box of its own: the path and the churn in a
// heading, ruled off from the hunks and the review threads anchored inside
// them. A folded one collapses to the heading line without the box, the same
// way a resolved thread does in the conversation.
func (m *Model) fileBlock(f gh.ChangedFile, folded bool, width int, threads bool) string {
	if folded {
		return m.fileHead(f, true, width)
	}

	inner := max(1, width-2)
	body := m.fileBody(f, inner, threads)
	lines := strings.Count(body, "\n") + 1

	pane := comp.NewPane(m.theme).Header(" " + m.fileHead(f, false, inner-1))
	return pane.Size(width, lines+pane.Chrome()).Render(body)
}

// fileBody is everything under a file's heading, already the full inner width
// so a changed line's background runs to the border. The pane pads with plain
// spaces, which would leave a hole at the end of every one.
func (m *Model) fileBody(f gh.ChangedFile, width int, threads bool) string {
	if f.Omitted != "" {
		return " " + clipTo(m.faint().Render(f.Omitted), width-1, m.faint())
	}

	// A nil map answers nothing, so the lines below need no guard of their own.
	var anchored map[anchor][]int
	if threads {
		anchored = m.threadsIn(f.Path)
	}
	placed := make(map[int]bool, len(anchored))

	tokens := m.lineTokens(f)
	gutter := max(gutterMin, len(strconv.Itoa(widest(f))))

	var lines []string
	seen := 0
	for _, h := range f.Hunks {
		lines = append(lines, m.hunkHead(h, gutter, width))
		for _, l := range h.Lines {
			lines = append(lines, m.diffLine(l, tokens[seen], gutter, width))
			seen++
			lines = append(lines, m.threadsAt(anchored, placed, l, width)...)
		}
	}

	if threads {
		lines = append(lines, m.strayThreads(f.Path, placed, width)...)
	}
	return strings.Join(lines, "\n")
}

// fileHead is the path and the churn, with a marker saying whether the diff
// under it is folded away.
func (m Model) fileHead(f gh.ChangedFile, folded bool, width int) string {
	marker := "▾ "
	if folded {
		marker = "▸ "
	}

	path := f.Path
	if f.PreviousPath != "" {
		path = f.PreviousPath + " → " + f.Path
	}

	lead := m.faint().Render(marker) +
		lipgloss.NewStyle().Foreground(m.theme.Primary).Bold(true).Render(path)

	churn := m.fileChurn(f)
	room := max(0, width-lipgloss.Width(churn)-1)
	if lipgloss.Width(lead) > room {
		lead = comp.Clip(lead, room, m.faint())
	}

	gap := max(1, width-lipgloss.Width(lead)-lipgloss.Width(churn))
	// The gap has a floor, so a width with no room for the churn still asks for
	// one more cell than it has. The pane would take that off the end of the
	// count, and a truncated count reads as a real one.
	return clipTo(lead+strings.Repeat(" ", gap)+churn, width, m.faint())
}

func (m Model) fileChurn(f gh.ChangedFile) string {
	return lipgloss.NewStyle().Foreground(m.theme.Success).Render("+"+strconv.Itoa(f.Additions)) +
		" " + lipgloss.NewStyle().Foreground(m.theme.Error).Render("−"+strconv.Itoa(f.Deletions))
}

// hunkHead is the @@ line, set in over the gutter the numbers below it use.
func (m Model) hunkHead(h gh.Hunk, gutter, width int) string {
	line := strings.Repeat(" ", gutter*2+4) +
		lipgloss.NewStyle().Foreground(m.theme.Secondary).Render(h.Header)
	return clipTo(line, width, m.faint())
}

// diffLine is one line of code: the two line numbers, the marker, and the
// highlighted source, on a tint of the color its change is in.
//
// The tint is painted cell by cell and the line is padded out to the full
// width. Every styled run ends in a reset that clears the background with it,
// so a joined line wrapped in the background style afterwards would carry it
// only as far as the first token.
func (m Model) diffLine(l gh.DiffLine, tokens []comp.Token, gutter, width int) string {
	marker, c := " ", m.theme.Faint
	base := lipgloss.NewStyle()

	switch l.Kind {
	case gh.DiffAdded:
		marker, c = "+", m.theme.Success
		base = background(base, m.theme.AddedBackground)
	case gh.DiffRemoved:
		marker, c = "−", m.theme.Error
		base = background(base, m.theme.RemovedBackground)
	}

	kind := base.Foreground(c)
	faint := base.Foreground(m.theme.Faint)
	oldNum, newNum := faint, faint
	switch l.Kind {
	case gh.DiffAdded:
		newNum = kind
	case gh.DiffRemoved:
		oldNum = kind
	}

	line := base.Render(" ") +
		oldNum.Render(number(l.Old, gutter)) + base.Render(" ") +
		newNum.Render(number(l.New, gutter)) + base.Render(" ") +
		kind.Render(marker) + base.Render(" ") + code(tokens, base)

	if w := lipgloss.Width(line); w > width {
		return comp.Clip(line, width, faint)
	} else if l.Kind != gh.DiffContext {
		// A context line needs no fill: it has no background to run out.
		line += base.Render(strings.Repeat(" ", width-w))
	}
	return line
}

// background applies a color the theme may not define. A nil one leaves the
// terminal's own showing, which is what keeps a transparent one transparent.
func background(s lipgloss.Style, c color.Color) lipgloss.Style {
	if c == nil {
		return s
	}
	return s.Background(c)
}

// code renders one line's tokens over the style the row is painted in. Every
// token takes only a foreground from it, so whatever the caller put behind the
// line survives all the way across.
func code(tokens []comp.Token, base lipgloss.Style) string {
	var b strings.Builder
	for _, t := range tokens {
		text := strings.ReplaceAll(t.Text, "\t", strings.Repeat(" ", tabWidth))
		if t.Color == nil {
			b.WriteString(base.Render(text))
			continue
		}
		b.WriteString(base.Foreground(t.Color).Render(text))
	}
	return b.String()
}

// lineTokens colors a whole file at once, one pass per side. A lexer carries
// state across lines, so highlighting line by line comes apart on the first
// multi-line string; and running the two sides together would feed it a file
// holding both halves of every change.
//
// A context line goes into both sides so neither reads as source with its
// unchanged lines missing, and takes its color from the new one.
func (m *Model) lineTokens(f gh.ChangedFile) [][]comp.Token {
	type at struct {
		left bool
		i    int
	}

	var oldSrc, newSrc []string
	var index []at

	for _, h := range f.Hunks {
		for _, l := range h.Lines {
			switch l.Kind {
			case gh.DiffRemoved:
				index = append(index, at{left: true, i: len(oldSrc)})
				oldSrc = append(oldSrc, l.Content)
			case gh.DiffAdded:
				index = append(index, at{i: len(newSrc)})
				newSrc = append(newSrc, l.Content)
			default:
				index = append(index, at{i: len(newSrc)})
				oldSrc = append(oldSrc, l.Content)
				newSrc = append(newSrc, l.Content)
			}
		}
	}

	oldTok := m.syntax.Lines(f.Path, strings.Join(oldSrc, "\n"))
	newTok := m.syntax.Lines(f.Path, strings.Join(newSrc, "\n"))

	out := make([][]comp.Token, len(index))
	for i, a := range index {
		src := newTok
		if a.left {
			src = oldTok
		}
		if a.i < len(src) {
			out[i] = src[a.i]
		}
	}
	return out
}

// threadsIn is every review thread written against a file, keyed by where it
// hangs. The buckets hold the thread's place in the detail rather than the
// thread: that index is what the conversation calls the same thread, so the two
// tabs agree on which one has been unfolded.
func (m Model) threadsIn(path string) map[anchor][]int {
	out := make(map[anchor][]int)
	for i, t := range m.detail.Detail.Threads {
		if t.Path != path || t.Line == 0 {
			continue
		}
		key := anchor{side: t.Side, line: t.Line}
		out[key] = append(out[key], i)
	}
	return out
}

// threadsAt renders whatever hangs off a line. A context line sits on both
// sides of the diff, so it answers to a comment written against either.
func (m *Model) threadsAt(threads map[anchor][]int, placed map[int]bool, l gh.DiffLine, width int) []string {
	var out []string
	for _, key := range anchorsOf(l) {
		for _, i := range threads[key] {
			placed[i] = true
			out = append(out, m.diffThread(i, width))
		}
	}
	return out
}

// diffThread renders one thread inline in the diff, under the same key the
// conversation gives it, so a <details> block unfolded on one tab is unfolded
// on the other. Focus is not shared: a card is only lit on the tab with a ring.
func (m *Model) diffThread(i, width int) string {
	key := focusKey{kind: focusThread, index: i}
	return indent(m.thread(m.detail.Detail.Threads[i], width-threadIndent, false, key), threadIndent)
}

func anchorsOf(l gh.DiffLine) []anchor {
	switch l.Kind {
	case gh.DiffAdded:
		return []anchor{{side: gh.SideRight, line: l.New}}
	case gh.DiffRemoved:
		return []anchor{{side: gh.SideLeft, line: l.Old}}
	}
	return []anchor{{side: gh.SideRight, line: l.New}, {side: gh.SideLeft, line: l.Old}}
}

// strayThreads is what the diff had no line for. An outdated thread anchors to
// a line the pull request has since moved past, and dropping it loses the only
// record of what was asked.
//
// It walks the threads the query returned rather than the map they were
// bucketed into: ranging a map is ordered at random, so the same comments came
// out under the file in a different order every time it was rendered.
func (m *Model) strayThreads(path string, placed map[int]bool, width int) []string {
	var out []string
	for i, t := range m.detail.Detail.Threads {
		if t.Path != path || t.Line == 0 || placed[i] {
			continue
		}
		out = append(out, m.diffThread(i, width))
	}
	return out
}

// widest is the longest line number the file has to print, which is what the
// gutter is sized to.
func widest(f gh.ChangedFile) int {
	n := 0
	for _, h := range f.Hunks {
		for _, l := range h.Lines {
			n = max(n, l.Old, l.New)
		}
	}
	return n
}

// number right-aligns a line number, or leaves the column blank on the side a
// line does not belong to.
func number(n, width int) string {
	if n == 0 {
		return strings.Repeat(" ", width)
	}
	s := strconv.Itoa(n)
	return strings.Repeat(" ", max(0, width-len(s))) + s
}

// clipTo cuts a line to the pane rather than letting it wrap. A wrapped line of
// code puts its tail under the gutter and every line below it out of step.
func clipTo(line string, width int, mark lipgloss.Style) string {
	if lipgloss.Width(line) <= width {
		return line
	}
	return comp.Clip(line, width, mark)
}

// treeBody is the file column. It paints its own selection, so it hands the
// viewport lines already the full inner width.
func (m *Model) treeBody(width int) string {
	if !m.files.Loaded {
		return ""
	}
	if len(m.rows) == 0 {
		return m.faint().Render("No files changed.")
	}

	// The cursor stays painted with focus elsewhere. Which file the diff is
	// showing is the question the column exists to answer, and the pane borders
	// already say where the keys go.
	lines := make([]string, len(m.rows))
	for i, r := range m.rows {
		lines[i] = renderRow(m.theme, r, width, i == m.cursor)
	}
	return strings.Join(lines, "\n")
}

// treeTitle names the file column by what it holds.
func (m Model) treeTitle() string {
	if !m.files.Loaded {
		return "Files"
	}
	return comp.Plural(len(m.files.Files), "file")
}

// moveCursor walks the tree and takes the diff with it. The rows are one line
// each, so keeping the cursor on screen is a clamp rather than the boundary
// arithmetic a two-line row needs.
func (m *Model) moveCursor(delta int) {
	if len(m.rows) == 0 {
		return
	}
	m.cursor = min(max(m.cursor+delta, 0), len(m.rows)-1)

	m.showCursorRow()
	m.syncContent()
	m.showCursorFile()
}

// showCursorRow keeps the cursor inside the tree's own window. The column opens
// with no blank line, so a row is its own offset.
func (m *Model) showCursorRow() { showRow(&m.sideView, m.cursor) }

// trackDiff points the file column at whatever the diff has scrolled to. The
// column answers "which file am I in", and a diff scrolled three files past its
// cursor answers it wrongly rather than not at all.
//
// The answer is the file filling most of the window rather than the one under
// its top line. One long file with a review thread hanging off it otherwise
// keeps the cursor for a whole screen of the files after it.
func (m *Model) trackDiff() {
	if m.tab != tabFiles || m.focus != paneMain || len(m.diff.spans) == 0 {
		return
	}

	top := max(0, m.view.YOffset()-contentLead)
	bottom := top + m.view.Height()

	best, seen := "", 0
	for _, s := range m.diff.spans {
		if covered := min(s.end, bottom) - max(s.start, top); covered > seen {
			best, seen = s.key, covered
		}
	}

	// The two ends are exact rather than proportional. Scrolled to the bottom
	// the answer is the last file, whatever share of the window it got.
	switch {
	case m.view.AtTop():
		best = m.diff.spans[0].key
	case m.view.AtBottom():
		best = m.diff.spans[len(m.diff.spans)-1].key
	}

	at := m.cursor
	for i, r := range m.rows {
		if r.key == best {
			at = i
			break
		}
	}

	if at == m.cursor {
		return
	}
	m.cursor = at
	m.showCursorRow()
	m.syncContent()
}

// cursorSpan is the file the column is pointing at, as an index into spans. A
// directory has no block of its own, so it answers with the first file under
// it. Minus one when there is no file left below the cursor at all.
func (m Model) cursorSpan() int {
	if m.cursor >= len(m.rows) {
		return -1
	}
	for _, r := range m.rows[m.cursor:] {
		for i, s := range m.diff.spans {
			if s.key == r.key {
				return i
			}
		}
	}
	return -1
}

// showCursorFile scrolls the diff to whatever the tree is pointing at.
func (m *Model) showCursorFile() {
	if at := m.cursorSpan(); at >= 0 {
		m.view.SetYOffset(contentLead + m.diff.spans[at].start)
	}
}

// jumpFile moves a whole file at a time, whichever pane has focus. Reading a
// diff is reading one file after another, and doing that by the line takes as
// many keystrokes as the file is long.
//
// From a directory row, forward means the first file inside it rather than the
// one after it: the reader has not seen that file yet.
func (m *Model) jumpFile(delta int) {
	at := m.cursorSpan()
	if at < 0 {
		return
	}

	next := at
	if delta < 0 || (m.cursor < len(m.rows) && m.rows[m.cursor].file != nil) {
		next = at + delta
	}
	next = min(max(next, 0), len(m.diff.spans)-1)

	key := m.diff.spans[next].key
	for i, r := range m.rows {
		if r.key == key {
			m.cursor = i
			break
		}
	}

	m.showCursorRow()
	m.syncContent()
	m.showCursorFile()
}

// toggleFold folds the row under the cursor: a directory out of the tree, a
// file's diff out of the pane beside it.
func (m *Model) toggleFold() {
	if m.cursor >= len(m.rows) {
		return
	}
	key := m.rows[m.cursor].key
	m.collapsed[key] = !m.collapsed[key]

	m.syncRows()
	m.cursor = min(m.cursor, max(0, len(m.rows)-1))
	m.syncContent()
	// Folding takes lines out from under the offset. Scrolled into the file
	// being folded, the pane would land in the middle of the next one and the
	// heading that just collapsed would be off screen entirely.
	m.showCursorFile()
}

// syncRows rebuilds what the tree has on screen. Folding changes it, and so
// does a diff arriving.
func (m *Model) syncRows() {
	m.rows = flatten(buildTree(m.files.Files), m.collapsed, 0, nil)
}
