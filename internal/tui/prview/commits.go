package prview

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/praxis-labs-io/zen-octo/internal/gh"
	"github.com/praxis-labs-io/zen-octo/internal/store"
	"github.com/praxis-labs-io/zen-octo/internal/tui/comp"
	"github.com/praxis-labs-io/zen-octo/internal/tui/paint"
)

// commitRowHeight is what one commit takes in the column. The sha, the marker
// and the headline fill a line on their own at this width; the author and the
// age go under them.
//
// Every row is the same height on purpose. A headline wrapped to whatever it
// needs makes the scroll arithmetic a walk over the rows rather than a
// multiply, and a column that lands mid-row cuts a sha off above the window.
const commitRowHeight = 2

// commitSettleDelay is how long the cursor has to sit still before its commit
// is asked for. The diff is a request of its own, and walking a long branch a
// keystroke at a time would spend one per commit passed through.
const commitSettleDelay = 150 * time.Millisecond

// commits is the Commits tab: what the column holds, where its cursor is, and
// the diff of the commit the cursor last settled on.
//
// sha is what the pane is painting and pending is what has been asked for. They
// differ for the one hop it takes the root to answer, which is where the
// distinction earns its place: a diff already cached comes back inside that hop,
// and a pane cleared to meet it flashes a spinner over a diff that was never
// going to be waited for.
type commits struct {
	cursor  int
	sha     string
	pending string
	files   store.Files
	rows    []row
	diff    diffBody
}

// NeedCommitMsg asks the root for a commit's diff. Same shape as the pull
// request's own: the screen cannot fetch, so the selection reaches the root as
// a message and the request starts there.
type NeedCommitMsg struct{ SHA string }

// CommitSettleMsg is a cursor that stopped moving. It carries the sha rather
// than a sequence number because the sha is already the identity: one fired for
// a commit the cursor has since walked off is told from the current one by
// comparing against the cursor, with no counter to keep in step.
type CommitSettleMsg struct{ SHA string }

// SetCommitFiles takes what the store holds for a commit's diff, and is what
// puts a newly asked-for commit on the pane. Anything that is neither the commit
// showing nor the one asked for is dropped: the cursor can have moved on and a
// third been asked for while the first was still out.
//
// The store answers a commit that is out as well as one that is held, so a real
// fetch arrives loading and spins, and a cached one arrives ready and paints.
// Nothing here has to know which it was.
func (m *Model) SetCommitFiles(sha string, f store.Files) {
	// Cleared on any answer, not just one that takes the pane. A retry of the
	// commit already showing asks for a sha that never becomes a take, and
	// leaving it pending would swallow every retry after it.
	if sha == m.commit.pending {
		m.commit.pending = ""
	}

	took := sha != m.commit.sha
	if took {
		// A diff takes the pane only while the column still points at it. An
		// answer for a commit the reader has walked back off would paint under
		// the wrong row, with nothing armed to put it right.
		if under, ok := m.underCursor(); !ok || under != sha {
			return
		}
		m.commit.sha = sha
	}

	m.commit.files = f
	m.commit.diff.blocks = nil
	m.commit.rows = flatten(buildTree(f.Files), nil, 0, nil)
	m.syncContent()

	// After the content, because the viewport clamps an offset to what it holds,
	// and only for the tab that owns it: one viewport serves all four, so a
	// commit landing while the reader is elsewhere would throw away their place.
	if took && m.tab == tabCommits {
		m.view.GotoTop()
	}
}

// syncCommits keeps the cursor inside a commit list that has just arrived or
// changed under it.
func (m *Model) syncCommits() {
	m.commit.cursor = min(m.commit.cursor, max(0, len(m.detail.Detail.Commits)-1))
}

// wanted is the commit the pane should be showing and whether it is worth
// asking for. Both ends of the wait read it, so the question is answered once:
// arming and settling that disagree spend a request the tab no longer wants.
//
// The tab is as much of the answer as the cursor is. A wait armed on the way
// through the Commits tab runs out wherever the reader has got to by then.
func (m Model) wanted() (string, bool) {
	sha, ok := m.underCursor()
	if m.tab != tabCommits || !ok {
		return "", false
	}
	// The commit already painted is worth asking for again only when its last
	// answer was a failure. That is the whole of the retry: no key selects a
	// commit any more, so nothing else would ever ask twice.
	if sha == m.commit.sha && m.commit.files.Status != store.StatusFailed {
		return sha, false
	}
	return sha, true
}

// armCommit starts the wait for the commit under the cursor.
func (m Model) armCommit() tea.Cmd {
	sha, want := m.wanted()
	if !want {
		return nil
	}
	return tea.Tick(commitSettleDelay, func(time.Time) tea.Msg {
		return CommitSettleMsg{SHA: sha}
	})
}

// settleCommit asks for the diff of a wait that ran out, and drops one the
// reader has moved past. Every keypress arms its own, so walking five commits
// sets five timers and only the last still names where the cursor ended up.
//
// Nothing on the pane moves here. The commit before this one holds it until the
// store answers, which is one hop away and is the only thing that knows whether
// there is a wait to show.
func (m *Model) settleCommit(msg CommitSettleMsg) tea.Cmd {
	sha, want := m.wanted()
	// One already asked for is on its way, and the root answers it either way.
	if !want || sha != msg.SHA || sha == m.commit.pending {
		return nil
	}

	m.commit.pending = sha
	return func() tea.Msg { return NeedCommitMsg{SHA: sha} }
}

// underCursor is the sha the column is pointing at, and whether it is pointing
// at anything. A commit with no sha is one the query returned empty.
func (m Model) underCursor() (string, bool) {
	list := m.detail.Detail.Commits
	if m.commit.cursor >= len(list) || list[m.commit.cursor].SHA == "" {
		return "", false
	}
	return list[m.commit.cursor].SHA, true
}

// commitColumn is the list. It paints its own selection, so it hands the
// viewport lines already the full inner width.
func (m Model) commitColumn(width int) string {
	if !m.detail.Loaded {
		return ""
	}

	list := m.detail.Detail.Commits
	if len(list) == 0 {
		return m.faint().Render("No commits.")
	}

	lines := make([]string, 0, len(list)*commitRowHeight)
	for i, c := range list {
		lines = append(lines, m.commitRow(c, width, i == m.commit.cursor)...)
	}
	return strings.Join(lines, "\n")
}

// commitRow is one commit over two lines: the check marker and the headline,
// then the short sha with who wrote it and when.
//
// The headline gets the top line to itself because it is the only part worth
// reading at a glance, and the sha sits with the rest of the metadata under it.
// The marker stays up top: it says whether the commit is worth opening.
//
// Selection is painted cell by cell, the same way the file column paints it.
// Every styled run ends in a reset that clears the background with it, so a
// joined row wrapped in the background style afterwards paints only its first
// cell.
func (m Model) commitRow(c gh.Commit, width int, selected bool) []string {
	base := lipgloss.NewStyle()
	if selected {
		base = base.Background(m.theme.SelectedBackground)
	}

	_, checks := comp.CheckStateIcon(m.theme, c.Checks)
	head := base.Foreground(checks).Render(glyphCheck) + base.Render(" ") +
		base.Foreground(m.theme.Text).Render(c.Headline)

	// The second line is set in under the headline rather than under the
	// marker, so the two lines read as one row.
	by := base.Render("  ") + base.Foreground(m.theme.Accent).Render(c.Short)
	if who := commitBy(c); who != "" {
		by += base.Foreground(m.theme.Subtle).Render(" · " + who)
	}

	return []string{m.padTo(head, width, base), m.padTo(by, width, base)}
}

// commitBy names who wrote a commit and when. The account is empty when the
// commit email matches none, and the name git recorded stands in for it: a row
// with a blank where the author goes reads as a rendering fault.
func commitBy(c gh.Commit) string {
	who := comp.Handle(c.Author.Login)
	if who == "" {
		who = c.AuthorName
	}

	at := comp.RelativeTime(c.CommittedAt)
	switch {
	case who != "" && at != "":
		return who + " · " + at
	case who != "":
		return who
	}
	return at
}

// padTo fills a line out to the column, or clips it when it runs past. Both the
// fill and the clip mark carry the row's own style, so a selection background
// runs the full width rather than stopping at the last word.
func (m Model) padTo(line string, width int, base lipgloss.Style) string {
	switch w := lipgloss.Width(line); {
	case w > width:
		return paint.Clip(line, width, base.Foreground(m.theme.Subtle))
	case w < width:
		return line + base.Render(strings.Repeat(" ", width-w))
	}
	return line
}

// commitBody is the diff of the commit the cursor settled on, through the same
// renderer the Files tab uses, under a card naming the commit it belongs to.
func (m *Model) commitBody() string {
	switch {
	case m.commit.sha == "":
		if _, ok := m.underCursor(); !ok {
			// No commits to point at. The column says so, and a second line
			// under it saying the same is noise.
			return ""
		}
		// The settle window and the hop after it. A diff is coming, so this
		// spins rather than sitting blank: an empty pane beside a full column
		// reads as a rendering fault.
		return m.spinner.Render("Loading the diff")
	case m.commit.files.Loaded:
		card := m.commitCard(m.bodyWidth())
		if card == "" {
			m.commit.diff.lead = 0
			return m.renderDiff(m.commit.rows, m.commit.files, &m.commit.diff)
		}
		// The card and the blank line under it sit above the first block, and
		// the spans have to clear both or a jump to the first file lands inside
		// the card instead.
		m.commit.diff.lead = strings.Count(card, "\n") + 2
		return card + "\n\n" + m.renderDiff(m.commit.rows, m.commit.files, &m.commit.diff)
	case m.commit.files.Status == store.StatusFailed:
		return m.faint().Render("Could not load the diff: " + m.commit.files.Err.Error())
	}
	return m.spinner.Render("Loading the diff")
}

// commitCard is the commit above its diff: the headline with its check state,
// the message under it, then the full sha and who wrote it when. The column
// beside it has room for none of that, and the full sha is the one thing there
// is no other way to read.
func (m Model) commitCard(width int) string {
	c, ok := m.selected()
	if !ok {
		return ""
	}

	inner := max(1, width-2)
	_, checks := comp.CheckStateIcon(m.theme, c.Checks)

	lines := []string{wrap(lipgloss.NewStyle().Foreground(checks).Render(glyphCheck)+" "+
		lipgloss.NewStyle().Foreground(m.theme.Text).Bold(true).Render(c.Headline), inner)}

	// The body keeps the line breaks whoever wrote it chose; wrap only folds
	// the ones that run past the card.
	if body := strings.TrimSpace(c.Body); body != "" {
		lines = append(lines, "", wrap(m.faint().Render(body), inner))
	}

	meta := lipgloss.NewStyle().Foreground(m.theme.Accent).Render(c.SHA)
	if who := commitBy(c); who != "" {
		meta += m.faint().Render(" · " + who)
	}
	lines = append(lines, "", wrap(meta, inner))

	body := strings.Join(lines, "\n")
	pane := comp.NewPane(m.theme)
	return pane.Size(width, strings.Count(body, "\n")+1+pane.Chrome()).Render(body)
}

// selected is the commit whose diff is on screen. It is found by sha rather
// than by the cursor: the cursor is free to walk on while a diff is being read.
func (m Model) selected() (gh.Commit, bool) {
	for _, c := range m.detail.Detail.Commits {
		if c.SHA == m.commit.sha {
			return c, true
		}
	}
	return gh.Commit{}, false
}

// moveCommit walks the column and keeps the cursor inside its own window. The
// diff follows once the cursor stops; arming that is the caller's, because this
// runs on a resize as well as on a key.
func (m *Model) moveCommit(delta int) {
	list := m.detail.Detail.Commits
	if len(list) == 0 {
		return
	}
	m.commit.cursor = min(max(m.commit.cursor+delta, 0), len(list)-1)

	// The window is counted in rows rather than lines, so the offset always
	// lands on a row boundary. It holds all the way to the end of the list
	// because the viewport is sized to a whole number of rows; against an odd
	// height the last offset clamps back off the boundary and opens the column
	// on a row's second line.
	rows := max(1, m.sideView.Height()/commitRowHeight)
	first := m.sideView.YOffset() / commitRowHeight

	switch {
	case m.commit.cursor < first:
		first = m.commit.cursor
	case m.commit.cursor >= first+rows:
		first = m.commit.cursor - rows + 1
	}

	m.sideView.SetYOffset(first * commitRowHeight)
	m.syncContent()
}

// jumpCommitFile moves the commit's diff a whole file at a time. There is no
// cursor beside it to drive, so the next file is the first one starting below
// the window and the previous the last one starting above it.
func (m *Model) jumpCommitFile(delta int) {
	spans := m.commit.diff.spans
	if len(spans) == 0 {
		return
	}

	at := m.view.YOffset()
	next := contentLead + spans[0].start

	if delta > 0 {
		next = contentLead + spans[len(spans)-1].start
		for _, s := range spans {
			if start := contentLead + s.start; start > at {
				next = start
				break
			}
		}
	} else {
		for _, s := range spans {
			if start := contentLead + s.start; start < at {
				next = start
			}
		}
	}
	m.view.SetYOffset(next)
}
