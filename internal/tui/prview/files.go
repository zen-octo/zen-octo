package prview

import (
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/praxis-labs-io/zen-octo/internal/gh"
	"github.com/praxis-labs-io/zen-octo/internal/store"
	"github.com/praxis-labs-io/zen-octo/internal/tui/comp"
	"github.com/praxis-labs-io/zen-octo/internal/tui/paint"
	"github.com/praxis-labs-io/zen-octo/internal/tui/syntax"
)

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

// blockKey identifies a rendered file block. The heading is part of it because
// only one of the two tabs draws one.
type blockKey struct {
	key     string
	heading bool
}

// blockStop is one segment of a file that something outside the painted code
// can change. Exactly one of the two indexes is set; the other is stopNone.
type blockStop struct {
	hunk   int
	thread int
}

// stopNone is the index a blockStop does not carry.
const stopNone = -1

// diffRow is a painted row and what it was painted from, so the row under the
// cursor is drawn again lit while the rest stay cached. right is the head
// column of a side-by-side row, and zero on a unified one.
type diffRow struct {
	line  paint.Line
	right paint.Line
	text  string
}

// code is whether the cursor can sit on a row in a column. A blank half and the
// blank between two hunks carry no number, which no real line does.
func (r diffRow) code(column gh.DiffSide) bool {
	if column == gh.SideRight {
		return r.right.Old != 0 || r.right.New != 0
	}
	return r.line.Old != 0 || r.line.New != 0
}

// run is a stretch of painted code between two stops, kept as rows rather than
// joined so one of them can be repainted without re-tokenising the file, and
// joined once beside them so a frame that lights none of them pays nothing.
type run struct {
	rows []diffRow
	text string
}

// newRun joins a run back into the page it was cut out of, once.
func newRun(rows []diffRow) run {
	out := make([]string, len(rows))
	for i, row := range rows {
		out[i] = row.text
	}
	return run{rows: rows, text: strings.Join(out, "\n")}
}

// codeRows is how many rows of a run the cursor can stand on in a column.
func (r run) codeRows(column gh.DiffSide) int {
	n := 0
	for _, row := range r.rows {
		if row.code(column) {
			n++
		}
	}
	return n
}

// rowAt is where the nth code row of a run sits among all of them, counting
// from one. 0 is the stop above the run, and past the end is -1.
func (r run) rowAt(n int, column gh.DiffSide) int {
	if n <= 0 {
		return -1
	}
	for i, row := range r.rows {
		if !row.code(column) {
			continue
		}
		if n--; n == 0 {
			return i
		}
	}
	return -1
}

// drawnFile is one file assembled from its block: the page it wrote, the stops
// in it, and where a box open inside one of them starts.
type drawnFile struct {
	text   string
	stops  []focusItem
	boxAt  int
	boxCol int

	// cursorAt is the line the cursor landed on, and -1 is a file it is not in.
	cursorAt int

	// rows is how far the cursor can walk into each block drawn here. It is read
	// by the next key rather than derived again: only the render knows which of
	// a thread's cards the code below it hangs under.
	rows map[focusKey]int

	// columns is the column each block was walked in, which is not always the
	// one asked for: a block the focused column has no rows in is walked in the
	// other. A key that steps columns reads the one the bar is actually in.
	columns map[focusKey]gh.DiffSide
}

// block is one file rendered. Tokenising is what it costs and that is all in
// the runs, so a stop is drawn again every frame and never held lit.
type block struct {
	at blockState

	// runs and stops interleave, runs first: one more run than stop, always.
	runs  []run
	stops []blockStop
}

// blockState is what one file block is rendered against. The fold signature is
// local to that file, so folding a hunk does not retire blocks already painted
// for other files.
type blockState struct {
	width int
	folds string

	// split is a mode rather than an identity, so it retires the block it
	// replaces instead of keeping one painted per mode for the rest of the run.
	split bool
}

// diffBody is one rendered diff: where each file's block sits inside it, and
// the blocks themselves. Each tab that shows a diff keeps one, because the
// render is the same over different files.
//
// The blocks are cached because moving a cursor one row repaints the diff, and
// rendering a block tokenises the whole file: without this a single keystroke
// costs the diff twice over. Each block carries the state it was built against,
// so a change retires that file alone.
type diffBody struct {
	spans  []fileSpan
	blocks map[blockKey]block

	// stops is every hunk heading and thread card in the rendered body, in the
	// same lines the file spans are counted in. The ring is built from them.
	stops []focusItem

	// headings is whether each file draws its own heading row. The Files tab
	// draws one file and puts its heading in the pane, where it cannot scroll.
	headings bool

	// threads is whether review threads hang off these lines. They are written
	// against the pull request's head, and the same line number in an older
	// commit is different code, so a commit's diff carries none.
	threads bool

	// split is whether these blocks draw two columns. The Commits tab never
	// does: it draws every file at once and each one is half as wide.
	split bool

	// lead is what sits above the first block. The spans are what a jump lands
	// on, and they have to clear whatever the tab put in front of them.
	lead int

	// cursorLine is where the row cursor landed in this body, and -1 is a body
	// with no cursor in it.
	cursorLine int

	// rows is every drawn block's walkable row count, from the last render.
	rows map[focusKey]int

	// columns is every drawn block's walked column, from the last render. It is
	// the render's answer the way rows is, and for the same reason.
	columns map[focusKey]gh.DiffSide
}

// filesBody is the diff. A diff that has loaded once keeps rendering through a
// failed refetch; the root raises a toast for that.
func (m *Model) filesBody() string {
	switch {
	case m.files.Loaded:
		m.diff.split = m.splitting()
		body := m.renderDiff(m.shownRows(), m.files, &m.diff)
		for _, s := range m.diff.stops {
			m.pageRing.add(s.focusKey, s.start, s.lines)
		}
		return body
	case m.files.Status == store.StatusFailed:
		return m.faint().Render("Could not load the diff: " + m.files.Err.Error())
	}
	return m.spinner.Render("Loading the diff")
}

// renderDiff renders every file in a set of rows, and records where each one
// starts so a column beside it can scroll to it.
func (m *Model) renderDiff(rows []row, res store.Files, d *diffBody) string {
	width := m.bodyWidth()
	d.spans, d.stops = d.spans[:0], d.stops[:0]
	d.cursorLine, d.rows = -1, make(map[focusKey]int)
	d.columns = make(map[focusKey]gh.DiffSide)

	// After the reset, or a refetch answering with nothing leaves the last
	// render's stops on a body that is one line saying there are none.
	if len(res.Files) == 0 {
		return m.faint().Render("No files changed.")
	}

	if d.blocks == nil {
		d.blocks = make(map[blockKey]block, len(rows))
	}

	blocks := make([]string, 0, len(rows))
	at := d.lead
	for _, r := range rows {
		if r.file == nil {
			continue
		}

		bk := blockKey{key: r.key, heading: d.headings}
		state := blockState{width: width, folds: m.hunkFoldState(*r.file), split: d.split}
		b, ok := d.blocks[bk]
		if !ok || b.at != state {
			b = m.fileBlock(*r.file, width, d.threads, d.headings, d.split)
			b.at = state
			d.blocks[bk] = b
		}

		drawn := m.fileText(*r.file, b, width, d.split)
		blocks = append(blocks, drawn.text)

		for _, p := range drawn.stops {
			p.start += at
			d.stops = append(d.stops, p)
		}
		if drawn.boxAt > 0 {
			m.boxLine, m.boxCol = at+drawn.boxAt, drawn.boxCol
		}
		if drawn.cursorAt >= 0 {
			d.cursorLine = at + drawn.cursorAt
		}
		for k, n := range drawn.rows {
			d.rows[k], d.columns[k] = n, drawn.columns[k]
		}

		lines := strings.Count(drawn.text, "\n") + 1
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

// fileBlock is one file: the heading, then its hunks and the review threads
// anchored inside them. No box, or a thread sits three borders deep.
func (m *Model) fileBlock(f gh.ChangedFile, width int, threads, heading, split bool) block {
	b := m.fileBody(f, width, threads, split)
	if !heading {
		return b
	}

	// The heading is not code and takes no cursor, so it goes in as a zero row.
	head := diffRow{text: m.fileHead(f, "▾ ", width)}
	b.runs[0] = newRun(append([]diffRow{head}, b.runs[0].rows...))
	return b
}

// fileBody is everything under a file's heading, already the full inner width
// so a changed line's background runs to the border. The pane pads with plain
// spaces, which would leave a hole at the end of every one.
func (m *Model) fileBody(f gh.ChangedFile, width int, threads, split bool) block {
	if f.Omitted != "" {
		text := " " + clipTo(m.faint().Render(f.Omitted), width-1, m.faint())
		return block{runs: []run{newRun([]diffRow{{text: text}})}}
	}

	// A nil map answers nothing, so the lines below need no guard of their own.
	var anchored map[anchor][]int
	if threads {
		anchored = m.threadsIn(f.Path)
	}
	placed := make(map[int]bool, len(anchored))

	tokens := m.lineTokens(f)
	gutter := paint.Gutter(widest(f))

	var b block
	var open []diffRow

	// close ends the run being gathered, so the stop after it starts a new one.
	closeRun := func() {
		b.runs = append(b.runs, newRun(open))
		open = nil
	}
	stop := func(s blockStop) {
		closeRun()
		b.stops = append(b.stops, s)
	}

	seen := 0
	for i, h := range f.Hunks {
		hunkOpen := m.hunkOpen(hunkKey(f.Path, h))

		// A hunk is a jump to somewhere else in the file, so it gets the blank
		// line every other break on this screen gets.
		if i > 0 {
			open = append(open, diffRow{})
		}
		stop(blockStop{hunk: i, thread: stopNone})

		own := tokens[seen : seen+len(h.Lines)]
		for _, p := range pairs(h.Lines, split) {
			if hunkOpen {
				open = append(open, m.paintRow(h.Lines, p, own, gutter, width, split))
			}
			// A row naming two lines answers a comment written against either.
			for _, j := range sides(p) {
				for _, at := range threadsAt(anchored, placed, h.Lines[j]) {
					if hunkOpen {
						stop(blockStop{hunk: stopNone, thread: at})
					}
				}
			}
		}
		seen += len(h.Lines)
	}

	if threads {
		for _, at := range m.strayThreads(f.Path, placed) {
			stop(blockStop{hunk: stopNone, thread: at})
		}
	}
	closeRun()
	return b
}

// fileHead is the path and the churn, with a marker saying whether the diff
// under it is folded away.
func (m Model) fileHead(f gh.ChangedFile, marker string, width int) string {
	path := f.Path
	if f.PreviousPath != "" {
		path = f.PreviousPath + " → " + f.Path
	}

	lead := m.faint().Render(marker) +
		lipgloss.NewStyle().Foreground(m.theme.Text).Bold(true).Render(path)

	churn := m.fileChurn(f)
	room := max(0, width-lipgloss.Width(churn)-1)
	if lipgloss.Width(lead) > room {
		lead = paint.Clip(lead, room, m.faint())
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

// cursorOn is whether the cursor is on a block itself rather than in the code
// under it. Two rows claiming to be where the reader stands is one too many, so
// a heading gives up its fill and a card its border once the walk leaves them.
func (m Model) cursorOn(key focusKey) bool { return m.lit(key) && !m.walkedInto(key) }

// litRun draws one row of a run again with the cursor on it. The rest are the
// strings already painted, so lighting a row costs no tokenising. An empty
// column is a unified row, which has only the one to light.
func (m Model) litRun(r run, at, gutter, width int, column gh.DiffSide) run {
	rows := make([]diffRow, len(r.rows))
	copy(rows, r.rows)

	fill, bar := m.theme.SelectedBackground, m.theme.Accent
	if column != "" {
		rows[at].text = m.halves(rows[at], column, gutter, width, fill, bar)
		return newRun(rows)
	}

	l := rows[at].line
	l.Fill, l.Bar = fill, bar
	rows[at].text = m.painter.Line(l, gutter, width)
	return newRun(rows)
}

// hunkHead is the @@ line, landing at the column the source under it starts in.
// Its marker names whether the source below it is open.
func (m Model) hunkHead(h gh.Hunk, gutter, width int, key focusKey, open, split bool) string {
	marker := ""
	if open {
		marker = ""
	}

	head := paint.Header{Text: h.Header, Marker: marker}
	if m.cursorOn(key) {
		head.Fill, head.Bar = m.theme.SelectedBackground, m.theme.Accent
	}

	// A heading spans the pane and belongs to neither column, so its bar sits at
	// the pane edge. Only its indent follows the source under it.
	if split {
		return m.painter.HalfHeader(head, gutter, width)
	}
	return m.painter.HunkHeader(head, gutter, width)
}

// paintRow is one row of the diff painted plain, kept beside what painted it so
// the cursor can light it later without the file being tokenised again.
func (m Model) paintRow(lines []gh.DiffLine, p pair, tokens [][]syntax.Token, gutter, width int, split bool) diffRow {
	if split {
		return m.splitRow(lines, p, tokens, gutter, width)
	}
	return m.diffRow(lines[p.left], tokens[p.left], gutter, width)
}

// diffRow is one unified row, carrying both line numbers.
func (m Model) diffRow(l gh.DiffLine, tokens []syntax.Token, gutter, width int) diffRow {
	line := paint.Line{Kind: kindOf(l.Kind), Old: l.Old, New: l.New, Tokens: tokens}
	return diffRow{line: line, text: m.painter.Line(line, gutter, width)}
}

// kindOf maps a fetched line onto the painter's own three.
func kindOf(k gh.DiffKind) paint.Kind {
	switch k {
	case gh.DiffAdded:
		return paint.Added
	case gh.DiffRemoved:
		return paint.Removed
	}
	return paint.Context
}

// lineTokens colors a whole file at once, one pass per side. A lexer carries
// state across lines, so highlighting line by line comes apart on the first
// multi-line string; and running the two sides together would feed it a file
// holding both halves of every change.
//
// A context line goes into both sides so neither reads as source with its
// unchanged lines missing, and takes its color from the new one.
func (m *Model) lineTokens(f gh.ChangedFile) [][]syntax.Token {
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

	out := make([][]syntax.Token, len(index))
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

// threadsAt is whatever hangs off a line. A context line sits on both sides of
// the diff, so it answers to a comment written against either.
func threadsAt(threads map[anchor][]int, placed map[int]bool, l gh.DiffLine) []int {
	var out []int
	for _, key := range anchorsOf(l) {
		for _, i := range threads[key] {
			placed[i] = true
			out = append(out, i)
		}
	}
	return out
}

// diffThread draws one thread inline in the diff, with its stops and its box.
// Replies are stops of their own, and only the column moves under the indent.
func (m *Model) diffThread(i, width int) rendered {
	t := m.detail.Detail.Threads[i]
	v := m.thread(t, width-threadIndent, false)
	v.block = indent(v.block, threadIndent)
	if v.boxAt > 0 {
		v.boxCol += threadIndent
	}
	return v
}

// fileText joins a cached block back into a page, drawing every stop fresh. It
// says where each one landed, and where a box open inside one of them sits.
func (m *Model) fileText(f gh.ChangedFile, b block, width int, split bool) drawnFile {
	gutter := paint.Gutter(widest(f))

	var sb strings.Builder
	out := drawnFile{
		stops:    make([]focusItem, 0, len(b.stops)),
		rows:     make(map[focusKey]int, len(b.stops)),
		columns:  make(map[focusKey]gh.DiffSide, len(b.stops)),
		cursorAt: -1,
	}
	at, wrote := 0, false

	write := func(text string, lines int) {
		if lines == 0 {
			return
		}
		if wrote {
			sb.WriteByte('\n')
		}
		sb.WriteString(text)
		at, wrote = at+lines, true
	}

	// owner is the block the next run belongs to: the last stop drawn, which for
	// a review thread is its deepest reply rather than the card that opened it.
	// One stop renders several, and crediting the run to the first of them walks
	// the cursor straight past every reply.
	var owner focusKey
	for i, r := range b.runs {
		if owner != (focusKey{}) {
			column, rows := m.walkColumn(r, split)
			out.rows[owner], out.columns[owner] = rows, column
			if m.lit(owner) && m.walkedInto(owner) {
				if lit := r.rowAt(min(m.diffCursor, rows), column); lit >= 0 {
					out.cursorAt = at + lit
					r = m.litRun(r, lit, gutter, width, column)
				}
			}
		}
		write(r.text, len(r.rows))
		if i == len(b.stops) {
			break
		}

		s := b.stops[i]
		var text string
		if s.hunk != stopNone {
			h := f.Hunks[s.hunk]
			key := hunkKey(f.Path, h)
			text = m.hunkHead(h, gutter, width, key, m.hunkOpen(key), split)
			if m.cursorOn(key) {
				out.cursorAt = at
			}
			out.stops = append(out.stops, focusItem{focusKey: key, start: at, lines: 1})
			owner = key
		} else {
			v := m.diffThread(s.thread, width)
			text = v.block
			if v.boxAt > 0 {
				out.boxAt, out.boxCol = at+v.boxAt, v.boxCol
			}
			for _, st := range v.stops {
				if m.cursorOn(st.focusKey) {
					out.cursorAt = at + st.start
				}
				out.stops = append(out.stops, focusItem{focusKey: st.focusKey, start: at + st.start, lines: st.lines})
				out.rows[st.focusKey] = 0
				owner = st.focusKey
			}
		}

		write(text, strings.Count(text, "\n")+1)
	}

	out.text = sb.String()
	return out
}

// hunkOpen reads the default without writing it into the shared map. View runs
// on model copies, so a missing entry has to remain a read.
func (m Model) hunkOpen(key focusKey) bool {
	open, set := m.open[key]
	return !set || open
}

// hunkFoldState names the folded set in source order. The file path and hunk
// headers are already in the keys, so equal signatures mean equal rendered
// runs for this file.
func (m Model) hunkFoldState(f gh.ChangedFile) string {
	state := make([]byte, len(f.Hunks))
	for i, h := range f.Hunks {
		if !m.hunkOpen(hunkKey(f.Path, h)) {
			state[i] = 1
		}
	}
	return string(state)
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
func (m Model) strayThreads(path string, placed map[int]bool) []int {
	var out []int
	for i, t := range m.detail.Detail.Threads {
		if t.Path != path || t.Line == 0 || placed[i] {
			continue
		}
		out = append(out, i)
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

// clipTo cuts a line to the pane rather than letting it wrap. A wrapped line of
// code puts its tail under the gutter and every line below it out of step.
func clipTo(line string, width int, mark lipgloss.Style) string {
	if lipgloss.Width(line) <= width {
		return line
	}
	return paint.Clip(line, width, mark)
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

// fileHeading is the path and the churn of the file in the pane, pinned in the
// pane's own header so it never scrolls off the code it names.
func (m Model) fileHeading() string {
	f := m.shownFile()
	if m.tab != tabFiles || f == nil || !m.files.Loaded {
		return ""
	}
	return " " + m.fileHead(*f, "", max(0, m.main.InnerWidth()-1))
}

// moveCursor walks the tree and takes the diff with it. The rows are one line
// each, so keeping the cursor on screen is a clamp rather than the boundary
// arithmetic a two-line row needs.
func (m *Model) moveCursor(delta int) {
	if len(m.rows) == 0 {
		return
	}
	m.cursor = min(max(m.cursor+delta, 0), len(m.rows)-1)

	m.nameShownFile()
	m.showCursorRow()
	m.syncContent()
}

// showCursorRow keeps the cursor inside the tree's own window. The column opens
// with no blank line, so a row is its own offset.
func (m *Model) showCursorRow() { showRow(&m.sideView, m.cursor) }

// shownRows is the one row the diff draws: the file the cursor last named.
// Empty until a file has been named, which SetFiles does as the diff lands.
func (m Model) shownRows() []row {
	for _, r := range m.rows {
		if r.file != nil && r.file.Path == m.shownPath {
			return []row{r}
		}
	}
	return nil
}

// shownFile is the file in the pane, or nil before one has been named.
func (m Model) shownFile() *gh.ChangedFile {
	if rows := m.shownRows(); len(rows) == 1 {
		return rows[0].file
	}
	return nil
}

// nameShownFile points the pane at the row under the cursor. A directory names
// no file and leaves the pane on whatever it was reading.
func (m *Model) nameShownFile() {
	if m.cursor >= len(m.rows) {
		return
	}
	f := m.rows[m.cursor].file
	if f == nil || f.Path == m.shownPath {
		return
	}
	m.shownPath = f.Path

	// The pane is shared with the other tabs, so a file named while one of them
	// is up must not scroll the page the reader is actually on.
	if m.tab == tabFiles {
		m.view.SetYOffset(0)
	}
}

// jumpFile moves the cursor a whole file at a time, skipping the directory rows
// between them, and reports whether there was one to move to.
func (m *Model) jumpFile(delta int) bool {
	step := 1
	if delta < 0 {
		step = -1
	}

	for at := m.cursor + step; at >= 0 && at < len(m.rows); at += step {
		if m.rows[at].file == nil {
			continue
		}
		m.cursor = at
		m.nameShownFile()
		m.showCursorRow()
		m.syncContent()
		return true
	}
	return false
}

// crossFile is what a brace means at the end of a file's ring. The pane holds
// one file, so the stop after the last one is in a file nothing has drawn yet.
func (m *Model) crossFile(delta int) {
	// The cursor moving is not the pane moving: from a directory row the next
	// file row is the one already drawn, and it is walked again rather than left.
	was := m.shownPath
	for m.shownPath == was {
		if !m.jumpFile(delta) {
			return
		}
	}

	// jumpFile has rebuilt the body, so the stops are the new file's. Forward
	// arrives at its head and back at its foot, which is where the reader left.
	if m.pageRing.stops() == 0 {
		return
	}
	at := 0
	if delta < 0 {
		at = m.pageRing.stops() - 1
	}
	m.pageRing.on = m.pageRing.items[at].focusKey

	m.syncContent()
	m.showFocus(&m.pageRing, &m.view, bodyTop(&m.view))
}

// toggleFold folds the directory under the cursor out of the tree. A file has
// no fold of its own: the pane draws one file, and folding it leaves nothing.
func (m *Model) toggleFold() {
	if m.cursor >= len(m.rows) || m.rows[m.cursor].file != nil {
		return
	}
	key := m.rows[m.cursor].key
	m.collapsed[key] = !m.collapsed[key]

	m.syncRows()
	m.cursor = min(m.cursor, max(0, len(m.rows)-1))
	m.syncContent()
}

// syncRows rebuilds what the tree has on screen. Folding changes it, and so
// does a diff arriving.
func (m *Model) syncRows() {
	m.rows = flatten(buildTree(m.files.Files), m.collapsed, 0, nil)

	// A fold can take the file being drawn off the tree, and the first render
	// has none named at all. Either way the cursor is what says which.
	if m.shownFile() == nil {
		m.cursor = m.firstFile()
		m.nameShownFile()
	}
}
