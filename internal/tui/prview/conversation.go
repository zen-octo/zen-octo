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

// treeGutter is the rail hanging a review's threads off the review that opened
// them. GitHub draws a line down the side to say the same thing; two columns is
// what a terminal has to spend on it.
const treeGutter = 2

// cardGutter is the space between a card's border and what it holds. Text
// against the border reads as a rendering fault rather than as a box.
const cardGutter = 1

// threadHunkLines caps the context shown above a review comment. GitHub returns
// the whole hunk, which on a large change is a screenful, and the line the
// comment is about is the last one in it.
const threadHunkLines = 4

// conversationBody is the description and everything said since. It renders
// markdown, so it belongs on an Update path: m.md caches what it produces.
//
// A detail that has loaded once keeps rendering through a failed refetch. The
// root raises a toast for that; blanking the screen would be worse news than
// the news.
func (m *Model) conversationBody() string {
	// The ring is rebuilt from the blocks below. A tab that renders none of them
	// leaves nothing behind for tab to land on.
	m.convRing.reset()

	switch {
	case m.detail.Loaded:
		return m.entries()
	case m.detail.Status == store.StatusFailed:
		return m.faint().Render("Could not load the conversation: " + m.detail.Err.Error())
	}
	return m.spinner.Render("Loading the conversation")
}

func (m *Model) entries() string {
	d := m.detail.Detail
	width := m.bodyWidth()

	var blocks []string
	at := m.convRing.lead

	// push records where a block landed before the join puts a blank line under
	// it. A key of no kind is a block tab walks past: a merge or a run of
	// commits is something to read, not something to act on.
	push := func(block string, key focusKey) {
		blocks = append(blocks, block)
		lines := strings.Count(block, "\n") + 1
		if key.kind != focusNone {
			m.convRing.add(key, at, lines)
		}
		at += lines + 1
	}

	push(m.description(d, width), focusKey{kind: focusDescription})

	// A thread whose review never made this page would otherwise never render.
	// Whatever is left after the walk goes at the end rather than nowhere.
	shown := make(map[int]bool, len(d.Threads))

	for i := 0; i < len(d.Timeline); i++ {
		item := d.Timeline[i]
		switch item.Kind {
		case gh.TimelineComment:
			key := focusKey{kind: focusComment, index: i}
			head := m.said(item.Actor, "commented", m.theme.Faint, item)
			push(m.card(head, m.body(item.Body, m.cardWidth(width), "No comment.", key), width, key), key)

		case gh.TimelineReview:
			// The review records its own card and every thread hung off it, so
			// it takes the line it starts on and push takes no key.
			push(m.review(item, i, d.Threads, shown, width, at), focusKey{})

		case gh.TimelineCommit:
			// A push arrives as one item per commit. They fold back into the one
			// line here rather than in the gh package, because how many rows a
			// run is worth is a rendering question and the Commits tab wants
			// them apart.
			run := commitRun(d.Timeline[i:])
			push(m.pushed(run), focusKey{})
			i += len(run) - 1

		default:
			// An event this build has no words for renders to nothing, and an
			// empty block still costs the blank line the join puts after it.
			if line := m.event(item); line != "" {
				push(line, focusKey{})
			}
		}
	}

	for i, thread := range d.Threads {
		if !shown[i] {
			key := focusKey{kind: focusThread, index: i}
			push(m.thread(thread, width, true, key), key)
		}
	}

	if n := d.MoreComments; n > 0 {
		push(wrap(m.faint().Render(comp.Plural(n, "older comment")+" on GitHub"), width), focusKey{})
	}
	if n := d.MoreThreads; n > 0 {
		push(wrap(m.faint().Render(comp.Plural(n, "more review thread")+" on GitHub"), width), focusKey{})
	}

	return strings.Join(blocks, "\n\n")
}

// review is the verdict and body in a box, then the threads it opened, set in
// under it. The box is what stops a bot review that runs for forty lines
// reading as loose comments with no telling where it ends.
func (m *Model) review(item gh.TimelineItem, index int, threads []gh.ReviewThread, shown map[int]bool, width, at int) string {
	label, c := comp.ReviewStateLabel(m.theme, item.Review)
	head := m.said(item.Actor, label, c, item)

	key := focusKey{kind: focusReview, index: index}
	block := m.card(head, m.body(item.Body, m.cardWidth(width), "No comment.", key), width, key)

	used := strings.Count(block, "\n") + 1
	m.convRing.add(key, at, used)

	type hung struct {
		block string
		key   focusKey
	}

	var owned []hung
	for i, thread := range threads {
		if thread.ReviewID != item.ID || thread.ReviewID == "" {
			continue
		}
		shown[i] = true
		tk := focusKey{kind: focusThread, index: i}
		owned = append(owned, hung{m.thread(thread, width-treeGutter, true, tk), tk})
	}

	for i, t := range owned {
		lines := strings.Count(t.block, "\n") + 1
		// The rail opens with a line of its own above the thread's first, so the
		// thread starts one below where the branch does.
		m.convRing.add(t.key, at+used+1, lines)
		used += lines + 1
		block += "\n" + m.branch(t.block, i == len(owned)-1)
	}
	return block
}

// branch hangs one thread off the review above it. The last closes the run, so
// the rail stops rather than trailing into whatever comes next.
func (m Model) branch(block string, last bool) string {
	style := lipgloss.NewStyle().Foreground(m.theme.BorderFaintOrSecondary())
	down := style.Render("│ ")

	corner, under := style.Render("├─"), down
	if last {
		corner, under = style.Render("╰─"), "  "
	}

	lines := strings.Split(block, "\n")

	// The elbow meets the card's heading row rather than its top border, which
	// is where GitHub joins the two as well. A resolved thread is one line and
	// has no heading to meet.
	elbow := min(1, len(lines)-1)

	out := make([]string, 0, len(lines)+1)
	out = append(out, down)
	for i, line := range lines {
		switch {
		case i == elbow:
			out = append(out, corner+line)
		case i < elbow:
			out = append(out, down+line)
		default:
			out = append(out, under+line)
		}
	}
	return strings.Join(out, "\n")
}

// card is one entry: a heading row, a rule, then what was written. The heading
// is already styled piece by piece, so the pane takes it as-is.
//
// The gutter is the caller's rather than the pane's, because the rail already
// indents its own entries and would end up with two.
//
// A focused card takes its border in the accent, the same signal the panes
// around it already use for where the keys go.
func (m Model) card(head, content string, width int, key focusKey) string {
	pane := comp.NewPane(m.theme).Header(" " + head).Focus(m.lit(key))
	body := indent(content, cardGutter)
	lines := strings.Count(body, "\n") + 1
	return pane.Size(width, lines+pane.Chrome()).Render(body)
}

// lit is whether a block holds the conversation's focus. A card is only lit on
// the pane the keys are going to, which is neither the Files tab, where the
// same threads render under a ring tab does not walk, nor the conversation
// while the rail has focus. A card lit on a pane the key does nothing to is a
// lie about the key, and two panes lit at once is the same lie twice.
func (m Model) lit(key focusKey) bool {
	return m.railTab() && m.focus == paneMain && m.convRing.focused(key)
}

// cardWidth is what is left for text once the box has taken its sides and its
// gutter.
func (m Model) cardWidth(width int) int { return max(1, width-2-2*cardGutter) }

// markdown renders a body, folding every <details> block to the line that
// stands for it. GitHub collapses them in the browser for the same reason: a
// bot review pastes a table of every file it looked at, and it is never the
// thing you opened the pull request to read.
func (m *Model) markdown(text string, width int, key focusKey) string {
	if m.expanded[key] {
		return m.md.Render(text, width)
	}

	var out []string
	for _, seg := range comp.SplitDetails(text) {
		rendered := m.md.Render(seg.Text, width)
		if seg.Summary != "" {
			rendered = wrap(m.faint().Render("▸ "+seg.Summary+" · "+comp.Plural(seg.Lines, "line")), width)
		}
		// A segment that renders to nothing still costs the blank line the join
		// puts after it. Bot comments open with a hidden marker often enough
		// that the gap shows up as a hole under the heading.
		if strings.TrimSpace(rendered) == "" {
			continue
		}
		out = append(out, rendered)
	}
	return strings.Join(out, "\n\n")
}

func (m *Model) description(d gh.PullRequestDetail, width int) string {
	key := focusKey{kind: focusDescription}
	head := m.said(d.Author, "opened this", m.theme.Faint, gh.TimelineItem{CreatedAt: d.CreatedAt})
	return m.card(head, m.body(d.Body, m.cardWidth(width), "No description.", key), width, key)
}

// body renders markdown, falling back to a note rather than a hole in the page.
func (m *Model) body(text string, width int, empty string, key focusKey) string {
	if out := m.markdown(text, width, key); strings.TrimSpace(out) != "" {
		return out
	}
	return m.faint().Render(empty)
}

// said is the line above a block of writing: who, what they did, and when. A
// deleted account has no login, so the verb carries the line on its own.
func (m *Model) said(actor gh.Actor, verb string, c color.Color, item gh.TimelineItem) string {
	parts := make([]string, 0, 3)
	if actor.Login != "" {
		parts = append(parts, lipgloss.NewStyle().Foreground(m.theme.Actor).Render(actor.Login))
	}
	parts = append(parts, lipgloss.NewStyle().Foreground(c).Render(verb))
	if at := comp.RelativeTime(item.CreatedAt); at != "" {
		parts = append(parts, m.faint().Render(at))
	}
	return strings.Join(parts, m.faint().Render(" · "))
}

// event is one line. Nobody reads a merge twice, and giving it the same block
// treatment as a comment buries the discussion between them.
func (m *Model) event(item gh.TimelineItem) string {
	label, ok := eventLabels[item.Kind]
	if !ok {
		return ""
	}
	return wrap(m.faint().Render("● ")+m.said(item.Actor, label, m.theme.Faint, item), m.bodyWidth())
}

// commitRun is the commits that landed together, from the head of a timeline.
func commitRun(items []gh.TimelineItem) []gh.TimelineItem {
	for i, item := range items {
		if item.Kind != gh.TimelineCommit {
			return items[:i]
		}
	}
	return items
}

// pushed is a run of commits that landed together: a line saying how many and
// who, then the run itself, one commit to a row. A lone commit names its sha
// and headline on the line and has no rows under it.
//
// The rows are what makes the branch readable from the conversation. A long
// rebase is a long block, which is the honest shape of a long rebase; the
// alternative hides work that happened between two comments.
//
// A run written by one person names them. A mixed one drops the login rather
// than crediting the wrong author, and said carries the line without it.
func (m *Model) pushed(run []gh.TimelineItem) string {
	last := run[len(run)-1]

	verb := "pushed " + comp.Plural(len(run), "commit")
	if c := last.Commit; len(run) == 1 && c != nil {
		verb = "pushed " + c.Short + " " + c.Headline
	}

	actor := last.Actor
	for _, item := range run {
		if item.Actor != actor {
			actor = gh.Actor{}
			break
		}
	}

	lines := []string{wrap(m.faint().Render("● ")+m.said(actor, verb, m.theme.Faint, last), m.bodyWidth())}
	if len(run) == 1 {
		return lines[0]
	}

	for _, item := range run {
		if item.Commit != nil {
			lines = append(lines, m.pushedRow(*item.Commit))
		}
	}
	return strings.Join(lines, "\n")
}

// pushedRow is one commit under a push. It is set in past the marker above it,
// and clips rather than wrapping: a headline folded onto a second row reads as
// two commits.
func (m *Model) pushedRow(c gh.Commit) string {
	const indent = "    "

	sha := lipgloss.NewStyle().Foreground(m.theme.Secondary).Render(c.Short)
	line := indent + sha + m.faint().Render("  "+c.Headline)

	if width := m.bodyWidth(); lipgloss.Width(line) > width {
		return comp.Clip(line, width, m.faint())
	}
	return line
}

var eventLabels = map[gh.TimelineKind]string{
	gh.TimelineMerged:         "merged this",
	gh.TimelineClosed:         "closed this",
	gh.TimelineReopened:       "reopened this",
	gh.TimelineReadyForReview: "marked this ready for review",
	gh.TimelineDraft:          "converted this to a draft",
	gh.TimelineForcePushed:    "force-pushed",
}

// thread renders a line-anchored discussion in a box of its own, its anchor in
// the top border, which is where GitHub puts the file name too.
//
// A resolved one collapses to a single line instead. GitHub hides them by
// default, and on a heavily reviewed pull request the settled nits bury the
// live ones.
//
// hunk asks for the code the thread was written against. The conversation
// wants it: a comment about a line nobody can see is an assertion about
// nothing. The Files tab does not, because the line is already on the screen.
func (m *Model) thread(t gh.ReviewThread, width int, hunk bool, key focusKey) string {
	anchor := t.Path
	if t.Line > 0 {
		anchor += ":" + strconv.Itoa(t.Line)
	}

	if t.IsResolved {
		// One line has no border to take the accent, so the text carries it.
		style := m.faint()
		if m.lit(key) {
			style = lipgloss.NewStyle().Foreground(m.theme.Secondary)
		}
		return wrap(style.Render("✓ "+anchor+" · resolved · "+
			comp.Plural(len(t.Comments), "comment")), width)
	}

	head := lipgloss.NewStyle().Foreground(m.theme.Primary).Render(anchor)
	if t.IsOutdated {
		head += m.faint().Render(" · outdated")
	}

	inner := m.cardWidth(width)
	var blocks []string
	if hunk {
		if code := m.threadHunk(t, inner); code != "" {
			blocks = append(blocks, code)
		}
	}
	for _, c := range t.Comments {
		said := m.said(c.Author, "said", m.theme.Faint, gh.TimelineItem{CreatedAt: c.CreatedAt})
		blocks = append(blocks, wrap(said, inner)+"\n\n"+m.body(c.Body, inner, "No comment.", key))
	}

	return m.card(head, strings.Join(blocks, "\n\n"), width, key)
}

// threadHunk is the tail of the diff the thread hangs off, rendered the same
// way the Files tab renders one. Only the last few lines: GitHub returns up to
// a screenful of leading context, and the line the comment is about is the last
// one.
func (m *Model) threadHunk(t gh.ReviewThread, width int) string {
	if t.Hunk == nil || len(t.Hunk.Lines) == 0 {
		return ""
	}

	lines := t.Hunk.Lines
	if len(lines) > threadHunkLines {
		lines = lines[len(lines)-threadHunkLines:]
	}

	gutter := gutterMin
	for _, l := range lines {
		gutter = max(gutter, len(strconv.Itoa(max(l.Old, l.New))))
	}

	tokens := m.syntax.Lines(t.Path, hunkSource(lines))
	out := make([]string, len(lines))
	for i, l := range lines {
		var row []comp.Token
		if i < len(tokens) {
			row = tokens[i]
		}
		out[i] = m.diffLine(l, row, gutter, width)
	}
	return strings.Join(out, "\n")
}

// hunkSource is the code behind a hunk, for the lexer. Both sides go in
// together here: a fragment this short is not valid source either way, and
// splitting it would leave the removed line with no context at all.
func hunkSource(lines []gh.DiffLine) string {
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = l.Content
	}
	return strings.Join(out, "\n")
}

func (m Model) faint() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(m.theme.Faint)
}

// wrap breaks a line at a width. Only markdown comes back already wrapped;
// every line this file builds by hand would otherwise be clipped by the card
// around it, or run the full width of a wide terminal past the measure.
func wrap(s string, width int) string {
	return lipgloss.NewStyle().Width(max(1, width)).Render(s)
}

func indent(s string, by int) string {
	pad := strings.Repeat(" ", by)
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		lines[i] = pad + line
	}
	return strings.Join(lines, "\n")
}
