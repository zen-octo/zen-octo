// Package prview is the pull request detail screen: a conversation pane with
// its own tab strip, and a details rail beside it that collapses when the frame
// gets narrow.
package prview

import (
	"strconv"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/zen-octo/zen-octo/internal/gh"
	"github.com/zen-octo/zen-octo/internal/store"
	"github.com/zen-octo/zen-octo/internal/tui/comp"
	"github.com/zen-octo/zen-octo/internal/tui/keys"
	"github.com/zen-octo/zen-octo/internal/tui/theme"
)

// BackMsg asks the root to return to the list.
type BackMsg struct{}

// NeedFilesMsg asks the root for a diff this screen does not hold. The screen
// cannot fetch, so opening the Files tab reaches the root as a message and the
// request starts there.
type NeedFilesMsg struct{ ID string }

// RefreshMsg asks the root to refetch this pull request. The detail feeds three
// of the four tabs, so it is always wanted; the other two fields name the diff
// the tab in front of the reader is showing, and are empty on the tabs that
// show none.
type RefreshMsg struct {
	ID    string
	Files bool
	SHA   string
}

// RailPreference is what the user last asked of the details rail. The root
// carries it from one pull request to the next, so hiding the rail stays hidden
// instead of having to be redone on every open.
type RailPreference struct {
	On  bool
	Set bool
}

// columnWidth is the side column on either edge of the screen: the details rail
// on the conversation, the file tree on the diff, the commit list on the
// commits. One number serves all three. They never share a frame, so the only
// place the difference shows is in the jump when you tab between them, and
// there it reads as a mistake.
//
// It is fixed rather than proportional: a column that grows with the frame just
// moves the content around. Wide enough for a branch name, which is the longest
// thing the rail carries.
const columnWidth = 37

// railMinFrame is the width below which the rail hides itself. Under it the
// conversation drops past the point where a diff inside a review comment reads.
const railMinFrame = 120

// railMinForced is the floor even when the user asks for the rail by hand. A
// conversation narrower than this is not worth the trade.
const railMinForced = columnWidth + 40

// railGutter is the space between the rail's left border and what it holds.
// Text against a border reads as a rendering fault rather than as a column.
//
// The right side is railNameRoom's business: a row runs the full width so the
// cursor line reaches the border, and it is the name that stops short.
const railGutter = 1

// contentMeasure caps the conversation and centres it. Text set the full width
// of a wide terminal is a paragraph the eye loses its place in on every line.
// The diff is exempt: code wants every column it can have.
const contentMeasure = 90

// sideMin is as narrow as the left column goes before it hides instead. It is
// the one column that shrinks rather than holding its width, because it is the
// only navigation the pane beside it has and hiding it costs more than
// narrowing it.
const sideMin = 24

// treeMinFrame is the width below which a left column hides. Under it the pane
// beside it is down to a gutter and a fragment, and the tab strip above it no
// longer fits the frame at all.
const treeMinFrame = 70

// diffMeasure is the width the diff keeps before the tree gives any up. Below
// it a hunk stops reading as code.
const diffMeasure = 80

// contentLead is the blank line every pane opens with. The diff records where
// each file starts in its own body, and scrolling to one has to clear this.
const contentLead = 1

type pane int

const (
	paneSide pane = iota
	paneMain
	paneRail
)

// The tabs are a slice rather than an enum, so the three with a body of their
// own are named. The conversation is index zero and the fallthrough.
const (
	tabCommits = 1
	tabChecks  = 2
	tabFiles   = 3
)

// tabs on the detail screen.
var tabs = []comp.Tab{
	{Label: "Conversation"},
	{Label: "Commits"},
	{Label: "Checks"},
	{Label: "Files"},
}

// Model is the detail screen.
type Model struct {
	theme theme.Theme
	side  comp.Pane
	main  comp.Pane
	rail  comp.Pane

	// Each pane scrolls on its own. The rail overflows a short frame as readily
	// as the conversation does, and its branch names are the only place some of
	// them appear.
	sideView viewport.Model
	view     viewport.Model
	railView viewport.Model

	md      comp.Markdown
	syntax  comp.Syntax
	spinner comp.Spinner

	pr     gh.PullRequest
	detail store.Detail
	files  store.Files
	tab    int
	focus  pane

	// rows is what the file tree has on screen, and cursor points into it. The
	// diff beside it is keyed by the same rows, so folding one folds both.
	rows      []row
	cursor    int
	collapsed map[string]bool

	// diff is the rendered Files tab: where each file's block sits, and the
	// blocks themselves. The Commits tab keeps one of its own.
	diff diffBody

	// commit is the Commits tab: what the column has on screen, where its
	// cursor is, and the diff of the commit that was last selected.
	commit commits

	// check is the Checks tab: the head commit's rollup grouped by workflow,
	// and which of them the column is on.
	check checks

	// shown is the body the main viewport already holds, and shownAt the width
	// it was measured at.
	shown   string
	shownAt int

	// filesAsked is whether this screen has asked for the diff yet. The diff
	// costs a second request, so it waits for the tab; it is asked for once
	// here rather than once per cached diff, which is what makes a reopen after
	// a push fetch the change rather than the one before it.
	filesAsked bool

	// convRing and railRing are what tab walks: the conversation's cards and
	// the rail's rows. The panes with a column of their own have a cursor
	// instead, and no ring.
	convRing ring
	railRing ring

	// expanded is which cards have their <details> blocks unfolded, keyed the
	// same way the ring keys what it points at. A review thread renders on the
	// Files tab as well as in the conversation, so both read this.
	//
	// folds counts the toggles, which is what tells the diff's block cache that
	// a thread inside one of its files renders differently now.
	expanded map[focusKey]bool
	folds    int

	// offsets parks the scroll position of each tab. One viewport serves all
	// four, and without this switching to a short tab clamps the offset to zero
	// and switching back lands at the top of a conversation you were halfway
	// down.
	offsets []int

	// railOn is what the user last asked for, and railUserSet whether they have
	// asked at all. Until they do, width decides.
	railOn      bool
	railUserSet bool

	width  int
	height int
}

// New builds the screen over one pull request row, carrying forward whatever
// the user last asked of the rail. The row is what the list already had, so the
// header and the rail paint before the detail query answers.
func New(th theme.Theme, pr gh.PullRequest, rail RailPreference, syntax comp.Syntax) Model {
	return Model{
		theme:    th,
		side:     comp.NewPane(th),
		main:     comp.NewPane(th),
		rail:     comp.NewPane(th),
		sideView: newViewport(),
		view:     newViewport(),
		railView: newViewport(),
		md:       comp.NewMarkdown(th),
		syntax:   syntax,
		spinner:  comp.NewSpinner(th),
		pr:       pr,
		focus:    paneMain,
		// The pull request's own diff is the one review threads were written
		// against. The Commits tab keeps a diffBody of its own, which does not.
		diff:        diffBody{threads: true},
		collapsed:   make(map[string]bool),
		expanded:    make(map[focusKey]bool),
		offsets:     make([]int, len(tabs)),
		railOn:      rail.On,
		railUserSet: rail.Set,
	}
}

// SetFiles takes what the store holds for this pull request's diff. A cursor
// still at the top goes to the first file rather than the directory above it,
// which is what the diff beside it is already showing.
func (m *Model) SetFiles(f store.Files) {
	m.files = f
	m.diff.blocks = nil
	m.syncRows()

	m.cursor = min(m.cursor, max(0, len(m.rows)-1))
	if m.cursor == 0 {
		m.cursor = m.firstFile()
	}
	m.syncContent()
}

func (m Model) firstFile() int {
	for i, r := range m.rows {
		if r.file != nil {
			return i
		}
	}
	return 0
}

// SetDetail takes what the store holds for this pull request. A detail that has
// loaded replaces the row, so the header and the rail stop showing the thinner
// version search returned.
// SetDetail carries the review threads the diff hangs off its lines, so a
// block rendered before they landed is stale. It carries the commits and the
// head commit's checks too, so both columns beside it come with it.
//
// It arms the commit wait, because this is where a cold Commits tab first has
// a commit to point at. Opening the tab while the detail is still out arms
// nothing, and without this the column would fill beside a pane that never did.
func (m *Model) SetDetail(d store.Detail) tea.Cmd {
	m.detail = d
	m.diff.blocks = nil
	if d.Loaded {
		m.pr = d.Detail.PullRequest
	}
	m.syncCommits()
	m.syncChecks()
	m.syncContent()
	return m.armCommit()
}

func newViewport() viewport.Model {
	vp := viewport.New()
	vp.SoftWrap = true
	vp.FillHeight = true
	return vp
}

// Init starts the spinner, which runs until the conversation lands.
func (m Model) Init() tea.Cmd { return m.spinner.Tick() }

// Update handles the keys that belong to this screen, and the spinner.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		return m.handleKey(msg)

	case CommitSettleMsg:
		return m, m.settleCommit(msg)

	case spinner.TickMsg:
		cmd := m.spinner.Advance(msg, m.waiting())
		// The body is built into the viewport rather than in View, so a new
		// frame of the glyph only reaches the screen through a resync.
		m.syncContent()
		return m, cmd
	}
	return m, nil
}

// waiting is the one state the spinner belongs in: nothing to read yet. A
// refetch behind content already on screen spins over nothing.
//
// It answers for every request rather than the tab's own, because the tick
// chain is one and the answer is what keeps it alive. Reading only the tab in
// front of the user killed the chain the moment they left the Files tab
// mid-fetch, and coming back found a spinner that never moved again.
func (m Model) waiting() bool {
	return (!m.detail.Loaded && m.detail.Status == store.StatusLoading) ||
		waitingFor(m.files) || waitingFor(m.commit.files) || m.commitBlank()
}

// commitBlank is a commit to show with nothing on the pane yet: the settle
// window and the hop after it. The glyph is on screen through both, so the tick
// chain has to stay alive through both or it freezes on its first frame.
func (m Model) commitBlank() bool {
	if m.tab != tabCommits || m.commit.sha != "" {
		return false
	}
	_, ok := m.underCursor()
	return ok
}

func waitingFor(f store.Files) bool {
	return !f.Loaded && f.Status == store.StatusLoading
}

func (m Model) handleKey(keyMsg tea.KeyPressMsg) (Model, tea.Cmd) {
	k := keys.Detail
	before := m.view.YOffset()

	// A key that placed the cursor itself must not then have the diff place it
	// again: a short file scrolled to the top of a tall window is not the file
	// filling most of it.
	follow := true

	switch {
	case key.Matches(keyMsg, k.Back):
		// Letting go of a card and leaving the screen are two intentions on one
		// key. The narrower one goes first.
		if m.clearFocus() {
			return m, nil
		}
		return m, func() tea.Msg { return BackMsg{} }

	case key.Matches(keyMsg, k.Refresh):
		return m, m.refresh()

	case key.Matches(keyMsg, k.FocusNext):
		m.stepFocus(1)
	case key.Matches(keyMsg, k.FocusPrev):
		m.stepFocus(-1)

	case key.Matches(keyMsg, k.NextTab):
		return m, m.changeTab(1)
	case key.Matches(keyMsg, k.PrevTab):
		return m, m.changeTab(-1)

	// A file belongs to whichever tab is showing a diff. The spans outlive a
	// tab switch, so without the guard the conversation scrolls to wherever the
	// diff last had a file.
	case key.Matches(keyMsg, k.NextFile) && m.tab == tabFiles:
		m.jumpFile(1)
		follow = false
	case key.Matches(keyMsg, k.PrevFile) && m.tab == tabFiles:
		m.jumpFile(-1)
		follow = false
	case key.Matches(keyMsg, k.NextFile) && m.tab == tabCommits:
		m.jumpCommitFile(1)
		follow = false
	case key.Matches(keyMsg, k.PrevFile) && m.tab == tabCommits:
		m.jumpCommitFile(-1)
		follow = false

	case key.Matches(keyMsg, k.PaneLeft):
		m.focusPane(m.focus - 1)
	case key.Matches(keyMsg, k.PaneRight):
		m.focusPane(m.focus + 1)
	case key.Matches(keyMsg, k.FocusPane):
		m.focusIndex(keyMsg.String())

	// On the Files tab the key folds whichever pane has focus, because the
	// cursor it folds at is the same one either pane is driving, and the tree
	// it belongs to is not on screen at all on a narrow frame.
	case key.Matches(keyMsg, k.Expand) && m.tab == tabFiles:
		m.toggleFold()
	case key.Matches(keyMsg, k.Expand):
		m.toggleExpanded()

	// Neither tab with a column has a rail to toggle. Reading railVisible here
	// would take its hard false for the user's preference and turn a hidden rail
	// back on, with nothing on screen to show for the keypress.
	case key.Matches(keyMsg, k.ToggleRail) && !m.railTab():

	case key.Matches(keyMsg, k.ToggleRail):
		m.railOn, m.railUserSet = !m.railVisible(), true
		// Focus cannot stay on a rail that just went away.
		if !m.railVisible() && m.focus == paneRail {
			m.focus = paneMain
		}
		m.layout()

	case key.Matches(keyMsg, k.Down):
		m.move(1)
	case key.Matches(keyMsg, k.Up):
		m.move(-1)
	case key.Matches(keyMsg, k.Top):
		if !m.jumped(-m.sideRows()) {
			m.scroll().GotoTop()
		}
	case key.Matches(keyMsg, k.Bottom):
		if !m.jumped(m.sideRows()) {
			m.scroll().GotoBottom()
		}
	case key.Matches(keyMsg, k.PageDown):
		if !m.jumped(m.sidePage()) {
			m.scroll().PageDown()
		}
	case key.Matches(keyMsg, k.PageUp):
		if !m.jumped(-m.sidePage()) {
			m.scroll().PageUp()
		}
	case key.Matches(keyMsg, k.HalfPageDown):
		if !m.jumped(m.sidePage() / 2) {
			m.scroll().HalfPageDown()
		}
	case key.Matches(keyMsg, k.HalfPageUp):
		if !m.jumped(-m.sidePage() / 2) {
			m.scroll().HalfPageUp()
		}
	}

	// Whatever the key did to the diff, the file column follows it. Gated on
	// the diff having actually moved: a key that only changes focus must not
	// pull the cursor off the row the reader left it on.
	if follow && m.view.YOffset() != before {
		m.trackDiff()
	}
	return m, m.armCommit()
}

// refresh names what is stale to the reader. The Conversation and Checks tabs
// read the detail, so they ask for nothing beyond it; the other two are each
// showing a diff the detail does not carry.
//
// The commit is the one on the pane rather than the one under the cursor. They
// differ only inside the settle window, and there the pane is still showing the
// old one, which is what the reader is asking to have refetched.
func (m Model) refresh() tea.Cmd {
	msg := RefreshMsg{ID: m.pr.ID}
	switch m.tab {
	case tabFiles:
		msg.Files = true
	case tabCommits:
		msg.SHA = m.commit.sha
	}
	return func() tea.Msg { return msg }
}

// move is a row in the left column and a line everywhere else. The column is
// the only pane with something to point at.
func (m *Model) move(delta int) {
	if m.sideDriving() {
		m.moveSide(delta)
		return
	}
	if delta > 0 {
		m.scroll().ScrollDown(delta)
		return
	}
	m.scroll().ScrollUp(-delta)
}

// jumped is the same split for the keys that move further than a line, and
// reports whether it took the key. In the column they move the cursor: a column
// scrolled away from its own cursor answers nothing.
func (m *Model) jumped(rows int) bool {
	if !m.sideDriving() {
		return false
	}
	m.moveSide(rows)
	return true
}

// sideDriving is whether the movement keys belong to the column. A column with
// nothing in it has no cursor to walk, and taking the keys anyway leaves the
// pane beside it unscrollable: the tab opens on the column, and a detail that
// failed to load puts its error there with no way to reach the end of it.
func (m Model) sideDriving() bool { return m.focus == paneSide && m.sideRows() > 0 }

// moveSide walks whichever column the tab has: commits, workflows, or files.
func (m *Model) moveSide(delta int) {
	switch m.tab {
	case tabCommits:
		m.moveCommit(delta)
	case tabChecks:
		m.moveCheck(delta)
	default:
		m.moveCursor(delta)
	}
}

// sideRows is how far the column runs, which is what the keys that go to one
// end of it move by.
func (m Model) sideRows() int {
	switch m.tab {
	case tabCommits:
		return len(m.detail.Detail.Commits)
	case tabChecks:
		return len(m.check.groups)
	}
	return len(m.rows)
}

// sidePage is a page of the column in its own rows. The keys that move by one
// are counting rows, and a commit row is two lines: paged by the line count the
// cursor clears a screenful of commits on every press.
func (m Model) sidePage() int {
	if m.tab == tabCommits {
		return max(1, m.sideView.Height()/commitRowHeight)
	}
	return m.sideView.Height()
}

// showRow keeps a one-line row inside a column's own window. Both one-line
// columns share it: two copies of the same window arithmetic drift, and the one
// that drifts opens on a row cut off above the window.
func showRow(v *viewport.Model, cursor int) {
	// Called before the first layout as well as after one, and a window of no
	// rows would put the cursor one row past itself.
	height := max(1, v.Height())

	switch offset := v.YOffset(); {
	case cursor < offset:
		v.SetYOffset(cursor)
	case cursor >= offset+height:
		v.SetYOffset(cursor - height + 1)
	}
}

// showSideCursor puts the column's own cursor back inside its window. One
// viewport serves all three columns and their rows are not the same height, so
// an offset left behind by another tab opens this one mid-row.
func (m *Model) showSideCursor() {
	if m.sideRows() == 0 {
		m.sideView.SetYOffset(0)
		return
	}
	switch m.tab {
	case tabCommits:
		m.moveCommit(0)
	case tabChecks:
		showRow(&m.sideView, m.check.cursor)
	default:
		m.showCursorRow()
	}
}

// changeTab moves the strip and takes the scroll position with it. The offset
// is restored after the content, because SetYOffset clamps to what is there.
//
// Both diff tabs open on content rather than on an empty pane. Files asks the
// root outright; Commits arms the same wait a cursor move does, so landing on
// the tab loads whatever the cursor is already pointing at. The fetch is the
// root's to make either way, and the tab is the only thing that says it is
// wanted.
func (m *Model) changeTab(delta int) tea.Cmd {
	m.offsets[m.tab] = m.view.YOffset()
	m.tab = (m.tab + delta + len(tabs)) % len(tabs)

	// The column comes and goes with the tab, and focus cannot sit on a pane
	// that is no longer on screen.
	m.layout()
	m.view.SetYOffset(m.offsets[m.tab])
	m.showSideCursor()

	// Commits opens with an empty diff pane, so the column is the only thing on
	// the tab there is anything to do with. Checks opens on a full pane, but
	// every key a reader presses there is picking a workflow. The other two open
	// on content worth reading and leave focus on the pane holding it.
	if (m.tab == tabCommits || m.tab == tabChecks) && m.sideVisible() {
		m.focus = paneSide
		m.syncContent()
	}

	// Asked once per screen rather than once per cached diff. The screen is new
	// on every open, so a pull request reopened after a push fetches its diff
	// again instead of reading the one from before the push for the rest of the
	// session, and revisiting the tab on this screen still costs nothing.
	if m.tab != tabFiles || m.filesAsked || m.files.Status == store.StatusLoading {
		return m.armCommit()
	}
	m.filesAsked = true

	id := m.pr.ID
	return func() tea.Msg { return NeedFilesMsg{ID: id} }
}

// focusRing is the ring the focused pane walks, with the viewport it scrolls
// in. Nil on a pane that has a cursor of its own: the file tree, the commit
// column and the workflow column are all walked by j and k already.
func (m *Model) focusRing() (*ring, *viewport.Model) {
	switch {
	case m.focus == paneRail && m.railVisible():
		return &m.railRing, &m.railView
	case m.focus == paneMain && m.railTab():
		return &m.convRing, &m.view
	}
	return nil, nil
}

// stepFocus walks the ring one item and brings what it lands on into view.
//
// The body is rebuilt before the offset moves, because a focused card renders
// differently and SetYOffset clamps to the content the viewport is holding.
func (m *Model) stepFocus(delta int) {
	r, vp := m.focusRing()
	if r == nil {
		return
	}

	top := bodyTop(vp)
	if !r.step(delta, top, vp.Height()) {
		return
	}

	m.syncContent()
	vp.SetYOffset(contentLead + r.show(top, vp.Height()))
}

// bodyTop is the viewport's offset in the lines the ring recorded, which sit
// one below it: every pane opens with a blank the items do not count.
//
// It is not clamped at zero. Clamping on the way in while contentLead goes back
// on the way out moves the page a line at the top of a scrollable pane, on a
// keypress that was meant to leave it alone.
func bodyTop(vp *viewport.Model) int { return vp.YOffset() - contentLead }

// clearFocus lets go of a focus the reader can see, and reports whether there
// was one. Both rings, because focus survives a move to the other pane and esc
// should not have to be pressed once per ring.
//
// A focus that is not on the screen is not one to let go of. Swallowing esc for
// a highlight nowhere on the frame reads as a key that does nothing, and the
// tabs with a column show no ring at all.
func (m *Model) clearFocus() bool {
	if !m.railTab() {
		return false
	}

	cleared := false
	for _, r := range []struct {
		ring *ring
		vp   *viewport.Model
	}{{&m.convRing, &m.view}, {&m.railRing, &m.railView}} {
		if r.ring.live(bodyTop(r.vp), r.vp.Height()) && r.ring.clear() {
			cleared = true
		}
	}

	if cleared {
		m.syncContent()
	}
	return cleared
}

// toggleExpanded unfolds the <details> blocks in the focused card. There is
// nothing to unfold with no card focused, which is what tab is one key away
// for. Rail rows hold no prose and answer to it with nothing.
//
// A card scrolled off the screen is not the one the reader means, any more than
// it is the one tab lands on. Acting on it would unfold something out of sight
// and drag the page back to it.
func (m *Model) toggleExpanded() {
	r, vp := m.focusRing()
	if r == nil || !r.on.kind.prose() {
		return
	}

	top := bodyTop(vp)
	if !r.live(top, vp.Height()) {
		return
	}

	m.expanded[r.on] = !m.expanded[r.on]
	m.folds++

	// Unfolding pushes everything under it down. Without this the card that
	// just grew opens below the window it was read in.
	m.syncContent()
	vp.SetYOffset(contentLead + r.show(top, vp.Height()))
}

// focusPane moves focus to a pane, skipping whatever is not on screen. Focus
// walks left to right, which is the order the panes are numbered in.
func (m *Model) focusPane(want pane) {
	for _, p := range []pane{paneSide, paneMain, paneRail} {
		if p == want && m.paneVisible(p) {
			m.focus = p
			m.syncContent()
			return
		}
	}
}

// focusIndex answers a digit with the pane sitting in that position. The panes
// are numbered by where they are rather than by what they hold, so 2 is the
// diff on the tabs with a column and the rail on the ones without.
func (m *Model) focusIndex(digit string) {
	n, err := strconv.Atoi(digit)
	if err != nil {
		return
	}
	at := 0
	for _, p := range []pane{paneSide, paneMain, paneRail} {
		if !m.paneVisible(p) {
			continue
		}
		if at++; at == n {
			m.focus = p
			m.syncContent()
			return
		}
	}
}

func (m Model) paneVisible(p pane) bool {
	switch p {
	case paneSide:
		return m.sideVisible()
	case paneRail:
		return m.railVisible()
	}
	return true
}

// scroll is the viewport the movement keys drive. Focus decides, which is what
// the help text promises and what the pane borders show.
func (m *Model) scroll() *viewport.Model {
	switch {
	case m.sideDriving():
		return &m.sideView
	case m.focus == paneRail:
		return &m.railView
	}
	return &m.view
}

// Rail is the preference to hand the next screen.
func (m Model) Rail() RailPreference {
	return RailPreference{On: m.railOn, Set: m.railUserSet}
}

// SetSize takes the frame and divides it between the panes.
func (m *Model) SetSize(width, height int) {
	m.width, m.height = width, height
	m.layout()
}

// layout sizes the panes for the current frame, tab, and rail state.
func (m *Model) layout() {
	// Focus follows the panes: a tab switch or a resize can take the one that
	// had it off the screen.
	if !m.paneVisible(m.focus) {
		m.focus = paneMain
	}

	mainWidth := m.width
	if m.sideVisible() {
		column := m.sideColumn()
		mainWidth -= column
		m.side = m.side.Size(column, m.height)
		m.sideView.SetWidth(m.side.InnerWidth())
		m.sideView.SetHeight(m.sideHeight())
	}
	if m.railVisible() {
		mainWidth -= columnWidth
		m.rail = m.rail.Size(columnWidth, m.height)
		m.railView.SetWidth(m.rail.InnerWidth())
		m.railView.SetHeight(m.rail.InnerHeight())
	}

	m.main = m.main.Size(mainWidth, m.height)
	m.view.SetWidth(m.main.InnerWidth())
	m.view.SetHeight(m.main.InnerHeight())
	m.syncContent()
}

// railVisible decides whether the rail is on screen. Width decides until the
// user overrides it, and even then the conversation keeps a floor.
//
// The rail belongs to the conversation. Everything it carries is about the
// pull request rather than about what a tab is showing, and the three tabs with
// a column already spend that side of the frame on one.
func (m Model) railVisible() bool {
	if !m.railTab() {
		return false
	}
	if m.railUserSet {
		return m.railOn && m.width >= railMinForced
	}
	return m.width >= railMinFrame
}

// railTab is whether this tab has a rail at all, at any width.
func (m Model) railTab() bool { return !m.sideVisibleTab() }

// sideVisibleTab is whether this tab has a column, before width has a say.
func (m Model) sideVisibleTab() bool {
	switch m.tab {
	case tabCommits, tabChecks, tabFiles:
		return true
	}
	return false
}

// sideVisible decides whether the left column is on screen. All three columns
// share the floor: below it the main pane is too narrow for its own tab strip,
// and the frame renders wider than the terminal it was given.
//
// Files falls back to the file headings inside the diff, and Checks to every
// workflow's card at once. Commits has no fallback, so a frame that narrow
// shows the diff it was last given and nothing to change it with. The card
// above the diff still names the commit.
func (m Model) sideVisible() bool {
	return m.sideVisibleTab() && m.width >= treeMinFrame
}

// sideColumn is what the left column gets. It takes its full width where the
// diff can still be read at its measure and gives the rest back below that,
// down to a floor.
func (m Model) sideColumn() int {
	return min(columnWidth, max(sideMin, m.width-diffMeasure))
}

// sideHeight is what the column's viewport holds. The commit column stacks two
// lines to a row, and an odd number of them leaves a last offset the viewport
// clamps off a row boundary, opening the column on a row's second line with its
// headline cut off above. The pane pads the line back.
func (m Model) sideHeight() int {
	h := m.side.InnerHeight()
	if m.tab == tabCommits && h >= commitRowHeight {
		return h / commitRowHeight * commitRowHeight
	}
	return h
}

// PullRequest is what the screen is showing.
func (m Model) PullRequest() gh.PullRequest { return m.pr }

// Keys is the keymap live while this screen is up.
func (m Model) Keys() keys.DetailMap { return keys.Detail }

func (m *Model) syncContent() {
	// Code is clipped to the pane rather than wrapped: a line of source folded
	// onto a second row puts its tail under the gutter and every line below it
	// out of step with its own number. Both tabs that show a diff want it off.
	m.view.SoftWrap = m.tab != tabFiles && m.tab != tabCommits

	if inner := m.main.InnerWidth(); inner > 0 {
		// The blank line above the first block is the same one the list opens
		// with. Content flush against a border reads as clipped, so the last
		// block gets one under it too: scrolled to the end, the closing line of
		// a comment would otherwise sit on the border.
		body := "\n" + indent(m.tabBody(), m.bodyGutter()) + "\n"
		// Handing the viewport a body measures every line of it, and a diff is
		// tens of thousands. A cursor moving down the file column does not
		// change a character of it.
		if body != m.shown || inner != m.shownAt {
			m.view.SetContent(body)
			m.shown, m.shownAt = body, inner
		}
	}
	if m.sideVisible() {
		// No gutter and no opening blank line. The column is a list of rows,
		// each led by its own marker, and every cell it gives back to padding
		// comes off a name that was already clipping.
		if inner := m.side.InnerWidth(); inner > 0 {
			m.sideView.SetContent(m.sideBody(inner))
		}
	}
	if inner := m.rail.InnerWidth(); m.railVisible() && inner > railGutter*2 {
		// The rail opens with a blank line, the same as the conversation beside
		// it. Its own gutter is the rows', not this pane's: a selected row is a
		// background, and an indent added out here would leave the cursor line
		// starting one column in from the border.
		m.railView.SetContent("\n" + m.railBody(inner))
	} else {
		// A rail off the screen holds nothing to point at, and rows left in the
		// ring would be walked by a tab pressed after it came back.
		m.railRing = ring{}
	}
}

// bodyWidth is the measure the conversation is set to. Prose stops being
// readable somewhere past this, and a wide terminal would otherwise run a
// comment the whole way across the screen.
//
// The tabs with a column take the pane instead. None of them holds prose: a
// line of code, a matrix job name and a commit's diff all run long, and capping
// them clips a name while the pane beside it sits empty.
func (m Model) bodyWidth() int {
	if m.sideVisibleTab() {
		return m.main.InnerWidth()
	}
	return min(m.main.InnerWidth(), contentMeasure)
}

// bodyGutter centres the measure in the pane.
func (m Model) bodyGutter() int { return max(0, (m.main.InnerWidth()-m.bodyWidth())/2) }

// railDetail is what the rail has to work with. Before the query answers that
// is the list row alone, and the rail drops every section behind it.
func (m Model) railDetail() gh.PullRequestDetail {
	if m.detail.Loaded {
		return m.detail.Detail
	}
	return gh.PullRequestDetail{PullRequest: m.pr}
}

// View renders the screen. The panes carry a bracketed index only when there is
// more than one of them, because a lone pane numbered [1] is just noise, and
// they are numbered left to right rather than by what they hold.
func (m Model) View() string {
	index, at := make(map[pane]int), 0
	for _, p := range []pane{paneSide, paneMain, paneRail} {
		if m.paneVisible(p) {
			at++
			index[p] = at
		}
	}
	if at < 2 {
		index = map[pane]int{}
	}

	// The tab strip goes on the main pane rather than on the column beside it:
	// the strip is wider than the column and would clip to a fragment there.
	panes := []string{m.main.
		Index(index[paneMain]).
		Tabs(tabs, m.tab).
		Footer(scrollFooter(m.view)).
		Focus(m.focus == paneMain).
		Render(m.view.View())}

	if m.sideVisible() {
		column := m.side.
			Index(index[paneSide]).
			Title(m.sideTitle()).
			Footer(scrollFooter(m.sideView)).
			Focus(m.focus == paneSide).
			Render(m.sideView.View())
		panes = append([]string{column}, panes...)
	}

	if m.railVisible() {
		panes = append(panes, m.rail.
			Index(index[paneRail]).
			Title("Details").
			Footer(scrollFooter(m.railView)).
			Focus(m.focus == paneRail).
			Render(m.railView.View()))
	}

	return lipgloss.JoinHorizontal(lipgloss.Top, panes...)
}

// scrollFooter reports position only when there is somewhere to scroll to. A
// counter on content that already fits is noise.
func scrollFooter(v viewport.Model) string {
	total := v.TotalLineCount()
	if total <= v.Height() {
		return ""
	}
	return strconv.Itoa(min(v.YOffset()+v.Height(), total)) + "/" + strconv.Itoa(total)
}

// tabBody renders whichever tab is current. The conversation is the
// fallthrough: it is the tab a screen opens on.
func (m *Model) tabBody() string {
	switch m.tab {
	case tabCommits:
		return m.commitBody()
	case tabChecks:
		return m.checkBody()
	case tabFiles:
		return m.filesBody()
	}

	// The header block sits above the first card, and the ring has to clear it
	// or every card is that many lines out from where tab scrolls to.
	head := m.conversation()
	m.convRing.lead = strings.Count(head, "\n") + 1
	return head + "\n" + m.conversationBody()
}

// sideBody renders whichever column the tab has.
func (m *Model) sideBody(width int) string {
	switch m.tab {
	case tabCommits:
		return m.commitColumn(width)
	case tabChecks:
		return m.checkColumn(width)
	}
	return m.treeBody(width)
}

// sideTitle names the column by what it holds, since the tab strip beside it
// already says which tab this is.
func (m Model) sideTitle() string {
	switch m.tab {
	case tabCommits:
		return m.commitTitle()
	case tabChecks:
		return m.checkTitle()
	}
	return m.treeTitle()
}

// conversation is the header block every GitHub PR page leads with. It comes
// off the list row, so it is on screen before the detail query answers.
func (m Model) conversation() string {
	rule := lipgloss.NewStyle().Foreground(m.theme.BorderFaintOrSecondary()).
		Render(strings.Repeat("─", max(0, m.bodyWidth())))

	// The blank sets the status apart from the two lines naming the pull
	// request. Three stacked lines read as one block and the eye skips the last.
	lines := []string{m.titleLine(), m.branchLine(), "", m.statusLine()}
	if status := m.collapsedStatus(); status != "" {
		lines = append(lines, wrap(status, m.bodyWidth()))
	}
	return strings.Join(append(lines, rule), "\n")
}

// titleLine is the number, the title, and the churn pushed to the far edge.
// The churn is a fixed few cells and the title is not, so the title is the one
// that gives way: it clips rather than pushing the numbers off the line.
func (m Model) titleLine() string {
	// The number leads, in the accent the list numbers rows with, so the same
	// pull request reads the same on both screens.
	lead := lipgloss.NewStyle().Foreground(m.theme.Secondary).Bold(true).
		Render("#"+strconv.Itoa(m.pr.Number)) + " " +
		lipgloss.NewStyle().Foreground(m.theme.Primary).Bold(true).Render(m.pr.Title)

	churn := m.churn()
	room := max(0, m.bodyWidth()-lipgloss.Width(churn)-1)
	if lipgloss.Width(lead) > room {
		lead = comp.Clip(lead, room, lipgloss.NewStyle().Foreground(m.theme.Faint))
	}

	gap := max(1, m.bodyWidth()-lipgloss.Width(lead)-lipgloss.Width(churn))
	return lead + strings.Repeat(" ", gap) + churn
}

// opened is when the pull request was raised and who raised it, as one clause.
// Either half can be missing: a deleted account has no login, and the row the
// list opens with carries no timestamp until the detail query answers.
func (m Model) opened() string {
	age, login := comp.LongAgo(m.pr.CreatedAt), m.pr.Author.Login

	switch {
	case age != "" && login != "":
		return "Opened " + age + " by " + comp.Handle(login)
	case age != "":
		return "Opened " + age
	case login != "":
		return "Opened by " + comp.Handle(login)
	}
	return ""
}

// branchLine is where the work is going and where it came from. It stays on one
// line: the head branch is the long one and the one carrying a ticket key at
// the front, so it is what gives way rather than the line wrapping.
func (m Model) branchLine() string {
	faint := lipgloss.NewStyle().Foreground(m.theme.Faint)
	target := faint.Render(m.pr.BaseRefName + " ← ")

	room := max(0, m.bodyWidth()-lipgloss.Width(target))
	if lipgloss.Width(m.pr.HeadRefName) > room {
		return target + comp.Clip(faint.Render(m.pr.HeadRefName), room, faint)
	}
	return target + faint.Render(m.pr.HeadRefName)
}

// statusLine is where the pull request stands, then who raised it and when. The
// state always has something to say, so this line is never empty even when the
// clause after it is.
func (m Model) statusLine() string {
	faint := lipgloss.NewStyle().Foreground(m.theme.Faint)

	label, c := comp.PRStateLabel(m.theme, m.pr)
	icon, _ := comp.PRStateIcon(m.theme, m.pr)

	line := lipgloss.NewStyle().Foreground(c).Render(icon + " " + label)
	if opened := m.opened(); opened != "" {
		line += faint.Render(" · " + opened)
	}
	return wrap(line, m.bodyWidth())
}

// churn is the diff stat in the colors the list gives its own columns.
func (m Model) churn() string {
	return lipgloss.NewStyle().Foreground(m.theme.Success).Render("+"+strconv.Itoa(m.pr.Additions)) +
		" " + lipgloss.NewStyle().Foreground(m.theme.Error).Render("−"+strconv.Itoa(m.pr.Deletions))
}

// collapsedStatus carries the two things the rail holds that the meta line does
// not, so hiding the rail loses nothing. Everything else in the rail is already
// on the line above it.
func (m Model) collapsedStatus() string {
	if m.railVisible() {
		return ""
	}

	var parts []string
	if label, c := comp.CheckStateLabel(m.theme, m.pr.Checks); label != "" {
		glyph, _ := comp.CheckStateIcon(m.theme, m.pr.Checks)
		parts = append(parts, lipgloss.NewStyle().Foreground(c).Render(glyph+" "+label))
	}
	if label, c := comp.ReviewLabel(m.theme, m.pr.ReviewDecision); label != "" {
		parts = append(parts, lipgloss.NewStyle().Foreground(c).Render(label))
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, lipgloss.NewStyle().Foreground(m.theme.Faint).Render(" · "))
}
