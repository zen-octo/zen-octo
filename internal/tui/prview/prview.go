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

	"github.com/praxis-labs-io/zen-octo/internal/gh"
	"github.com/praxis-labs-io/zen-octo/internal/store"
	"github.com/praxis-labs-io/zen-octo/internal/tui/comp"
	"github.com/praxis-labs-io/zen-octo/internal/tui/keys"
	"github.com/praxis-labs-io/zen-octo/internal/tui/paint"
	"github.com/praxis-labs-io/zen-octo/internal/tui/syntax"
	"github.com/praxis-labs-io/zen-octo/internal/tui/theme"
)

// BackMsg asks the root to return to the list.
type BackMsg struct{}

// NeedFilesMsg asks the root for a diff this screen does not hold. The screen
// cannot fetch, so opening the Files tab reaches the root as a message and the
// request starts there.
type NeedFilesMsg struct{ ID string }

// NeedJobMsg asks the root for the metadata and log of one concrete Actions
// job. A rerun gets a new id, even though it remains the same logical check.
type NeedJobMsg struct {
	JobID   int64
	Refresh bool
}

// JobSettleMsg is a selected check that stayed under the cursor long enough to
// be worth fetching. Walking the column arms many waits; only the one still
// selected when it fires can reach the network.
type JobSettleMsg struct {
	Key     string
	JobID   int64
	Refresh bool
}

// ToggleFileViewedMsg asks the root to mark one file viewed or unviewed.
type ToggleFileViewedMsg struct {
	ID     string
	Path   string
	Viewed bool
}

// RefreshMsg asks the root to refetch this pull request. The detail feeds three
// of the four tabs, so it is always wanted; the other two fields name the diff
// the tab in front of the reader is showing, and are empty on the tabs that
// show none.
type RefreshMsg struct {
	ID    string
	Files bool
	SHA   string
}

// PostCommentMsg asks the root to write a comment on this pull request. The
// screen cannot reach the network, so the buffer leaves here as a message and
// the write starts at the root, the same way a fetch does.
type PostCommentMsg struct {
	ID   string
	Body string
}

// PostReplyMsg asks the root to answer a review thread. It carries the pull
// request as well as the thread: the mutation is addressed to the thread, and
// everything the root does around it, the placeholder and the toast and the
// screen it puts them on, is keyed by the pull request.
type PostReplyMsg struct {
	ID       string
	ThreadID string
	Body     string
}

// ResolveThreadMsg asks the root to settle a review thread or open it again.
// Resolved is the state the key is asking for rather than the one the thread is
// in, so the root writes what it was handed and never reads the page back.
type ResolveThreadMsg struct {
	ID       string
	ThreadID string
	Resolved bool
}

// SplitTooNarrowMsg reports a side-by-side toggle the pane has no room for, and
// how many columns short it is. The root raises the toast.
type SplitTooNarrowMsg struct{ Short int }

// ThreadNotInDiffMsg reports a jump with nowhere to land: the thread's file is
// not among the changed files the diff carries. The root raises the toast, for
// the reason EditorFailedMsg gives.
type ThreadNotInDiffMsg struct{ Path string }

// EditorFailedMsg reports an editor that would not run or a file that would not
// be read. The buffer is untouched and the pane is still open; the root raises
// the toast, because the screen has nowhere to say it.
type EditorFailedMsg struct{ Err error }

// CopyLinkMsg asks the root to put the pull request's URL on the clipboard.
type CopyLinkMsg struct{ PR gh.PullRequest }

// BrowseMsg asks the root to open the pull request in a browser.
type BrowseMsg struct{ PR gh.PullRequest }

// RailPreference is what the user last asked of the details rail. The root
// carries it from one pull request to the next, so hiding the rail stays hidden
// instead of having to be redone on every open.
type RailPreference struct {
	On  bool
	Set bool
}

// columnWidth is the column down the left of the screen: the details rail on
// the conversation, the file tree on the diff, the commit list on the commits.
// One number serves all three, and one edge does too. They never share a frame,
// so the only place either difference shows is in the jump when you change tab,
// and there it reads as a mistake: the rail sat on the right until it was the
// only secondary pane that did.
//
// It is fixed rather than proportional: a column that grows with the frame just
// moves the content around. Wide enough for a branch name, which is the longest
// thing the rail carries.
const columnWidth = 37

// railMinFrame is the width below which the rail hides itself. Under it the
// conversation drops past the point where a diff inside a review comment reads.
const railMinFrame = 120

// railColumnFrom is the width at which the rail is worth a column of its own.
// Below it a conversation has too little left to give, so the rail lands over.
const railColumnFrom = columnWidth + 40

// railGutter is the space between the rail's left border and what it holds.
// Text against a border reads as a rendering fault rather than as a column.
//
// Two cells, and the cursor bar takes the first of them. One was enough while
// the gutter was blank, and left the bar against the row it marks: a dot or a
// glyph leading a name then sat on the bar rather than beside it, which reads
// as one mark rather than a row that has been marked. The second cell is what
// the headings indent by too, so a name lines up whether or not it is the row
// the cursor is on.
//
// The right side is railNameRoom's business: a row runs the full width so the
// cursor line reaches the border, and it is the name that stops short.
const railGutter = 2

// branchMeasure caps the whole branch line, both names and the arrow between
// them. A wide terminal is not a reason to spend all of it on two refs, and the
// line reads as a pair rather than as a sentence running the frame.
//
// The room is shared rather than split. A name inside its share is never cut,
// and what it leaves goes to the other one, so merging a long branch into main
// spends four columns on main and the rest on the name worth reading. Two long
// names take half each, which is the only answer when neither will fit.
//
// A cut takes the tail. The key these names carry sits at the front, so what
// goes is the sentence after it and never which pull request this is.
const branchMeasure = 96

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
// beside it is down to a gutter and a fragment.
const treeMinFrame = 70

// diffMeasure is the width the diff keeps before the tree gives any up. Below
// it a hunk stops reading as code.
const diffMeasure = 80

// contentLead is the blank line every pane opens with. The diff records where
// each file starts in its own body, and scrolling to one has to clear this.
const contentLead = 1

// headGutter holds the pinned header off the terminal's edges. One column, so
// the title starts level with the first content cell of the pane under it and
// the far edge ends level with its last.
const headGutter = 1

// headRoom is what the panes keep whatever the header wants: two borders and a
// line of content. Below it the header is clipped rather than the frame
// rendering taller than the terminal it was given.
const headRoom = 3

type pane int

const (
	// paneNone leads so that it is the zero value: the slice parking a pane per
	// tab is then a slice of tabs nobody has opened, which is what it is before
	// anybody presses anything. Nothing focuses it.
	paneNone pane = iota
	paneSide
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

// tabs on the detail screen. The counts are filled in per render by tabCounts;
// these are the labels and the order.
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
	syntax  syntax.Syntax
	painter paint.Painter
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

	// shownPath is the one file the diff pane draws. The cursor names it as it
	// passes a file row, and a directory row leaves whatever was there.
	shownPath string

	// diff is the rendered Files tab: where each file's block sits, and the
	// blocks themselves. The Commits tab keeps one of its own.
	diff diffBody

	// diffCursor is how far into diffOn the row cursor has walked, and 0 is the
	// block itself. Held against a key, so any other block reads as unwalked.
	diffCursor int
	diffOn     focusKey

	// split is what the reader asked for, not what they are getting: a pane too
	// narrow draws unified until it is widened, and splitting() is the one to
	// read. column is the one the cursor is in, which only a split diff has.
	split  bool
	column gh.DiffSide

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

	// led is whether the screen has been through the layout it arrives on. It
	// happens there rather than in New, because which pane leads is a question
	// about a frame and New has not been given one.
	//
	// It says arrived rather than handed over, and the difference is a frame
	// that opens too narrow for a second pane. Latched on having found a lead,
	// a reader working in the only pane there was would have the keys taken off
	// them by the first widen past railMinFrame, which is the terminal getting
	// bigger and not an arrival at all.
	led bool

	// jump is the review thread v is on its way to, empty when there is none.
	// The diff costs a request of its own, so a jump made before it has ever
	// been asked for waits here and lands when the answer arrives.
	jump string

	// pageRing is whatever the main pane is showing: the conversation's cards,
	// or the hunks and comments of a diff. Only one tab draws into it at a time.
	pageRing ring

	// railRing is the rail's rows, which are on screen beside the page rather
	// than instead of it, so the two cursors are two.
	railRing ring

	// open is the fold state of blocks the reader has moved from their default,
	// keyed the same way the ring keys what it points at. Prose starts closed;
	// hunks start open. A review thread renders on the Files tab as well as in
	// the conversation, so both read this.
	open map[focusKey]bool

	// offsets parks the scroll position of each tab. One viewport serves all
	// four, and without this switching to a short tab clamps the offset to zero
	// and switching back lands at the top of a conversation you were halfway
	// down.
	offsets []int

	// panes parks which pane the reader had the keys on, per tab. Focus is one
	// field where the scroll is four, so without this a round trip through a
	// tab that takes its own column comes back having lost the choice: the
	// column goes off screen and focus falls to whatever is left.
	//
	// A tab nobody has been to yet holds paneNone, which is what lets Commits
	// and Checks take their column on arrival and only on arrival.
	panes []pane

	// compose is the box a comment is written in: the last card in the
	// conversation, always on the page.
	//
	// who is the account it will be from, for its heading. The root knows it and
	// the screen does not, so it is handed over rather than fetched.
	compose composer
	who     gh.Actor

	// inline is the box the page summons: r opens one under a review thread and
	// e opens one over a comment. Separate from compose because the two hold
	// different drafts for different targets, and only one of them has the
	// keyboard at a time.
	inline inline

	// conv is the conversation above the box, kept while it is being written in.
	conv convCache

	// boxLine is the page line an open box's first row landed on, recorded as
	// the page is built and read by the scroll that keeps the caret in sight. A
	// box grows with what is typed into it, so it can be taller than the window
	// and the block holding it says nothing about where in it the caret is.
	//
	// boxCol is the page column it landed on, recorded beside it and read by
	// the popup that draws at the caret. A block behind a review's branch is
	// set in from the measure, and only the render knows by how much.
	boxLine int
	boxCol  int

	// mention is the @-autocomplete over an open box, closed when nothing is
	// being written in.
	mention mention

	// mentionsAsked is whether this screen has asked for the people a mention
	// offers. Once per screen rather than once per popup, the way filesAsked
	// is: every keystroke inside an @word re-enters the open path, and a fetch
	// that failed must not be retried on each of them. The sync key is the
	// retry, which is where it is for the rail's pickers too.
	mentionsAsked bool

	// railOn is what the user last asked for, and railUserSet whether they have
	// asked at all. Until they do, width decides.
	railOn      bool
	railUserSet bool

	// repo is the choices the rail's pickers offer, held by the root and handed
	// down. It belongs to the repository rather than to this pull request, so
	// it survives opening the next one.
	repo store.Repo

	// branches is the last branch search to answer, held apart from repo for
	// the reason the store holds it apart: it is keyed by what somebody typed
	// into the base picker rather than fetched once and kept.
	branches store.Branches

	// picking is the picker over the screen, empty when there is none.
	picking picking

	// merging is the merge form over the screen, empty when there is none. It
	// is not a picker: merging is a method, a commit message and a branch to
	// delete, which is a form, and only one of the two can be up at a time.
	merging merging

	width  int
	height int
}

// New builds the screen over one pull request row, carrying forward whatever
// the user last asked of the rail. The row is what the list already had, so the
// header and the rail paint before the detail query answers.
func New(th theme.Theme, pr gh.PullRequest, rail RailPreference, syntax syntax.Syntax) Model {
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
		painter:  paint.Painter{Theme: th},
		spinner:  comp.NewSpinner(th),
		pr:       pr,
		focus:    paneMain,
		// The pull request's own diff is the one review threads were written
		// against. The Commits tab keeps a diffBody of its own, which does not.
		diff: diffBody{threads: true},
		// The head is the side a reader comes to read, so a split diff opens
		// with the cursor in it and h is the step to the base.
		column:      gh.SideRight,
		commit:      commits{diff: diffBody{headings: true}},
		compose:     newComposer(th),
		inline:      newInline(th),
		mention:     mention{dismissed: -1},
		collapsed:   make(map[string]bool),
		open:        make(map[focusKey]bool),
		offsets:     make([]int, len(tabs)),
		panes:       make([]pane, len(tabs)),
		railOn:      rail.On,
		railUserSet: rail.Set,
	}
}

// SetFiles takes what the store holds for this pull request's diff. A cursor
// still at the top goes to the first file rather than the directory above it,
// which is what the diff beside it is already showing.
//
// It returns a command because a jump can be waiting on this: v pressed before
// the diff was ever asked for lands here, and what it cannot do it has to be
// able to say.
func (m *Model) SetFiles(f store.Files) tea.Cmd {
	m.files = f
	m.diff.blocks = nil
	m.syncRows()

	m.cursor = min(m.cursor, max(0, len(m.rows)-1))
	if m.cursor == 0 {
		m.cursor = m.firstFile()
	}

	// Named again because the cursor moved after syncRows placed it, and laid
	// out because the pinned heading lands with the diff and costs the pane rows.
	m.nameShownFile()
	m.layout()
	return m.finishJump()
}

// firstFile is the row the tab opens on: a file with something to read, since a
// binary or an omitted body opens the reader on an empty pane.
func (m Model) firstFile() int {
	for i, r := range m.rows {
		if r.file != nil && len(r.file.Hunks) > 0 {
			return i
		}
	}
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
	headMoved := m.detail.Loaded && d.Loaded && m.detail.Detail.HeadRefOid != d.Detail.HeadRefOid
	m.detail = d
	m.diff.blocks = nil
	m.conv.ok = false
	if d.Loaded {
		m.pr = d.Detail.PullRequest
	}
	if headMoved {
		for key := range m.open {
			if key.kind == focusHunk {
				delete(m.open, key)
			}
		}
	}

	// A thread that came back open keeps no fold of its own. The flag is read
	// only while a thread is resolved, and one left from the last time it was
	// would hold the card open against the next resolve, which is the write's
	// only acknowledgement.
	for _, t := range d.Detail.Threads {
		if !t.IsResolved {
			delete(m.open, threadKey(t))
		}
	}
	m.syncCommits()
	m.syncChecks()

	// Taken before the relayout, because the answer is about the page the reader
	// was looking at rather than the one that replaces it.
	held := m.focusWhole()

	// layout rather than syncContent: the detail replaces the row the header is
	// built from, and a status line that gains a timestamp can wrap onto a
	// second row. The panes divide what the header leaves, so its height has to
	// be taken again before they are sized.
	m.layout()
	m.keepFocusWhole(held)
	return tea.Batch(m.armCommit(), m.armJob(), m.armJobRender())
}

// focusWhole is whether the focused card is on the screen entire. A card that
// was and no longer is has been grown or moved by whatever landed, and the
// reader is looking at part of a thing they were looking at all of.
func (m Model) focusWhole() bool {
	top := bodyTop(&m.view)
	return m.mainRing().show(top, m.view.Height()) == top
}

// keepFocusWhole brings the focused card back into view after a write changed
// its height under the reader.
//
// A resolve is the one that needs it. Unresolving opens a collapsed thread into
// its card, its code and every reply hanging off it, and the growth arrives
// through the store rather than under the key: o re-shows the focus itself and
// x has no equivalent, so the thread grew off the bottom of the window and sat
// there.
//
// It moves only where the card was whole on the screen before. A refetch
// landing while the reader has scrolled somewhere else is not a reason to haul
// them back to the focus they left behind, which is the rule every key that
// reads the ring already holds to.
func (m *Model) keepFocusWhole(was bool) {
	if !was || m.focusWhole() {
		return
	}
	m.showFocus(&m.pageRing, &m.view, bodyTop(&m.view))
}

func newViewport() viewport.Model {
	vp := viewport.New()
	vp.SoftWrap = true
	vp.FillHeight = true
	return vp
}

// Init starts the spinner, which runs until the conversation lands.
func (m Model) Init() tea.Cmd { return tea.Batch(m.spinner.Tick(), m.armJobRender()) }

// Update handles the keys that belong to this screen, and the spinner.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		return m.handleKey(msg)

	case CommitSettleMsg:
		return m, m.settleCommit(msg)
	case JobSettleMsg:
		return m, m.settleJob(msg)
	case jobParsedMsg:
		return m, m.jobParsed(msg)
	case jobRenderMsg:
		return m, m.jobRendered(msg)
	case SearchSettleMsg:
		return m, m.settleCheckSearch(msg)

	case BranchSettleMsg:
		return m, m.settleBranches(msg)

	case editorDoneMsg:
		return m.editorReturned(msg)

	case spinner.TickMsg:
		cmd := m.spinner.Advance(msg, m.waiting())
		// The body is built into the viewport rather than in View, so a new
		// frame of the glyph only reaches the screen through a resync.
		m.syncContent()
		return m, cmd

	default:
		// A paste is not a keypress. The terminal sends it whole, as its own
		// message, and the textarea answers two of them: the bracketed paste the
		// terminal wraps a middle-click or a cmd+v in, and the clipboard read its
		// own ctrl+v asks for. Neither reaches handleKey, so while a box has the
		// keyboard anything this screen does not recognise goes to it.
		//
		// The merge form holds two boxes of its own and is checked first,
		// because it owns the keyboard whenever it is up. Its caret comes
		// through here too: the blink is a message rather than a key, and a
		// field that never receives one has a caret that never blinks.
		if m.merging.open {
			return m, m.merging.update(msg)
		}

		box := m.writing()
		if box == nil {
			return m, nil
		}

		var cmd tea.Cmd
		box.area, cmd = box.area.Update(msg)

		// A paste can land an @word under the caret as readily as typing one,
		// and it never reaches handleKey, so the token is re-read here too.
		ask := m.syncMention()
		m.showBox()
		return m, tea.Batch(cmd, ask)
	}
}

// writing is the box with the keyboard, or nil when the screen has it. Two boxes
// can be on the page and only one of them can be typed in.
func (m *Model) writing() *composer {
	switch {
	case m.compose.typing:
		return &m.compose
	case m.inline.typing:
		return &m.inline.composer
	}
	return nil
}

// showBox brings whichever box is being written in back onto the page. They
// scroll differently: the compose card is the last block and the reply box is
// somewhere in the middle.
func (m *Model) showBox() {
	if m.inline.typing {
		m.showInline()
		return
	}
	m.showCompose()
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
		waitingFor(m.files) || waitingFor(m.commit.files) || waitingForJob(m.check.job) || m.commitBlank() ||
		m.mentionWaiting()
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

func waitingForJob(j store.Job) bool {
	return !j.Loaded && j.Status == store.StatusLoading
}

func (m Model) handleKey(keyMsg tea.KeyPressMsg) (Model, tea.Cmd) {
	k := keys.Detail

	// A box takes the keyboard while it is being written in, and so does a
	// picker. Every letter is a letter then, so nothing below this line gets a
	// look.
	switch {
	case m.merging.open:
		return m.mergeKey(keyMsg)
	case m.picking.open():
		return m.pickerKey(keyMsg)
	case m.compose.typing:
		return m.composeKey(keyMsg)
	case m.inline.typing:
		return m.inlineKey(keyMsg)
	case m.check.searching:
		return m.checkSearchKey(keyMsg)
	}

	switch {
	case key.Matches(keyMsg, k.Back):
		// An accepted search remains active for n and N. Esc clears that before
		// it is allowed to leave the detail screen.
		if m.tab == tabChecks && !m.check.search.Empty() {
			m.clearCheckSearch()
			return m, nil
		}
		// Esc leaves, and that is the whole of it. Letting go of a card used to
		// come first, on the argument that the narrower intention wins the key.
		// But a cursor is landed now wherever there is something to land it on,
		// so there is always something to let go of, and the reader who wanted
		// the list was paying two presses for it every time. A cursor that
		// always exists is not a thing to be dismissed.
		return m, func() tea.Msg { return BackMsg{} }

	case key.Matches(keyMsg, k.Sync):
		return m, m.refresh()

	case key.Matches(keyMsg, k.ToggleViewed) && m.tab == tabFiles:
		return m.toggleFileViewed()

	case key.Matches(keyMsg, k.Search) && m.tab == tabChecks:
		m.startCheckSearch()
		return m, nil
	case key.Matches(keyMsg, k.NextMatch) && m.tab == tabChecks:
		m.moveCheckMatch(1)
		return m, nil
	case key.Matches(keyMsg, k.PrevMatch) && m.tab == tabChecks:
		m.moveCheckMatch(-1)
		return m, nil
	case key.Matches(keyMsg, k.FirstFailure) && m.tab == tabChecks:
		m.jumpFirstCheckFailure()
		return m, nil

	// Both mean the pull request on every tab, whatever the ring is on: nothing
	// below it carries a URL to reach.
	case key.Matches(keyMsg, k.CopyLink):
		return m, func() tea.Msg { return CopyLinkMsg{PR: m.pr} }

	case key.Matches(keyMsg, k.Browse):
		return m, func() tea.Msg { return BrowseMsg{PR: m.pr} }

	// A top-level comment lands in the conversation, so it is written from
	// there. The three tabs with a column each anchor their own kind of comment
	// and none of them is this one.
	// Both gated on the box being on the screen. The ring keeps its focus across
	// a tab switch, so without the guard enter would start typing on a tab that
	// draws no box at all.
	case key.Matches(keyMsg, k.Comment) && m.canCompose():
		return m.writeComment()

	// A rail row opens what it holds. This is ahead of the compose card because
	// the ring on the conversation keeps its focus while the rail has the keys,
	// and enter with the rail focused means the row under the rail's cursor.
	case key.Matches(keyMsg, k.Activate) && m.focus == paneRail:
		return m.openRailPicker()

	// The box is a card like any other, so the ring reaches it and enter is what
	// steps into it, the same key that presses the button once inside.
	case key.Matches(keyMsg, k.Activate) && m.canCompose() && m.pageRing.on.kind == focusCompose:
		return m.writeComment()

	// Checks has no comment to reply to, so r takes the failed job under the
	// logical selection and keeps that selection through the new attempt.
	case key.Matches(keyMsg, k.Reply) && m.canRerunCheck():
		return m, m.rerunCheck()

	// A reply answers the comment the ring is on, so both keys read the focus
	// and do nothing without one. The gate is inside: whether GitHub will take
	// a reply is the thread's answer, not this screen's.
	case key.Matches(keyMsg, k.Reply):
		return m.startReply(false)
	case key.Matches(keyMsg, k.QuoteReply):
		return m.startReply(true)

	// Both read the focus and keep their gate inside, the way the reply keys do:
	// whether GitHub will take a rewrite is the comment's answer, not this
	// screen's. Delete opens a confirm rather than writing, because it is the
	// one write here that cannot be taken back.
	case key.Matches(keyMsg, k.Edit):
		return m.startEdit()
	case key.Matches(keyMsg, k.Delete):
		return m.startDelete()

	// React reads the focus too, and its gate is the only one of the three that
	// is not about whose writing it is: GitHub lets anybody react to anything
	// they can see, their own comment included.
	case key.Matches(keyMsg, k.React):
		return m.startReact()

	// Both act on the thread the ring is holding, and both keep their gate
	// inside: whether GitHub will take a resolve is the thread's answer, and
	// whether the file is in the diff is the diff's.
	case key.Matches(keyMsg, k.Resolve):
		return m.toggleResolved()
	case key.Matches(keyMsg, k.Jump):
		return m.showInDiff()

	case key.Matches(keyMsg, k.NextTab):
		return m, m.changeTab(1)
	case key.Matches(keyMsg, k.PrevTab):
		return m, m.changeTab(-1)

	// The strip is ] and [ on both screens. tab is the file, which only the tab
	// showing one at a time has.
	case key.Matches(keyMsg, k.NextFile) && m.tab == tabFiles:
		m.jumpFile(1)
	case key.Matches(keyMsg, k.PrevFile) && m.tab == tabFiles:
		m.jumpFile(-1)

	// A block in a diff is a hunk or a comment written against one, and both are
	// in the pane, so the key takes the pane along with the block.
	case key.Matches(keyMsg, k.NextBlock) && m.tab == tabFiles:
		m.walkDiff(1)
	case key.Matches(keyMsg, k.PrevBlock) && m.tab == tabFiles:
		m.walkDiff(-1)

	// The Commits tab has no ring, so there a block is still a whole file and
	// the key moves the column's cursor to it. A block on Checks is one job
	// step; j and k remain line motion through its output.
	case key.Matches(keyMsg, k.NextBlock) && m.tab == tabCommits:
		m.jumpCommitFile(1)
	case key.Matches(keyMsg, k.PrevBlock) && m.tab == tabCommits:
		m.jumpCommitFile(-1)
	case key.Matches(keyMsg, k.NextBlock) && m.tab == tabChecks:
		m.moveCheckStep(1)
	case key.Matches(keyMsg, k.PrevBlock) && m.tab == tabChecks:
		m.moveCheckStep(-1)

	// The rail is a list of controls rather than blocks of anything, and it
	// answers to the movement keys instead. Leaving the braces on it as well
	// would give one pane two ways to do the same thing and teach neither.
	case key.Matches(keyMsg, k.NextBlock) && !m.railDriving():
		m.stepFocus(1)
	case key.Matches(keyMsg, k.PrevBlock) && !m.railDriving():
		m.stepFocus(-1)

	// h and l step, and a diff in two columns is one more thing to step to. A
	// pane that took the key keeps the focus it already had.
	case key.Matches(keyMsg, k.PaneLeft):
		if !m.stepColumn(gh.SideLeft) {
			m.stepPane(-1)
		}
	case key.Matches(keyMsg, k.PaneRight):
		if !m.stepColumn(gh.SideRight) {
			m.stepPane(1)
		}
	case key.Matches(keyMsg, k.SplitView) && m.tab == tabFiles:
		return m, m.toggleSplit()
	case key.Matches(keyMsg, k.FocusPane):
		m.focusIndex(keyMsg.String())

	// The trees are the column's, and the blocks are the pane's. Falling from
	// one to the other folds something the reader is not looking at.
	case key.Matches(keyMsg, k.Expand) && m.checkFoldable():
		m.toggleCheckFold()
	case key.Matches(keyMsg, k.Expand) && m.checkStepFoldable():
		m.toggleCheckStep()
	case key.Matches(keyMsg, k.Expand) && m.tab == tabFiles && m.focus != paneMain:
		m.toggleFold()
	case key.Matches(keyMsg, k.Expand):
		m.toggleBlockFold()

	// Neither tab with a column has a rail to toggle. Reading railVisible here
	// would take its hard false for the user's preference and turn a hidden rail
	// back on, with nothing on screen to show for the keypress.
	case key.Matches(keyMsg, k.ToggleRail) && !m.railTab():

	case key.Matches(keyMsg, k.ToggleRail):
		m.railOn, m.railUserSet = !m.railVisible(), true
		// The rail takes the keys as it opens, and gives them back as it goes. A
		// reader asking for the controls is asking to use them.
		switch {
		case m.railVisible():
			m.focus = paneRail
		case m.focus == paneRail:
			m.focus = paneMain
		}
		m.layout()
		m.landCursor()

	case key.Matches(keyMsg, k.Down):
		m.move(1)
	case key.Matches(keyMsg, k.Up):
		m.move(-1)
	case key.Matches(keyMsg, k.Top):
		if !m.gotoCheckLine(false) && !m.jumped(-m.sideRows()) {
			m.scroll().GotoTop()
		}
	case key.Matches(keyMsg, k.Bottom):
		if !m.gotoCheckLine(true) && !m.jumped(m.sideRows()) {
			m.scroll().GotoBottom()
		}
	case key.Matches(keyMsg, k.PageDown):
		if !m.pageCheckLine(m.view.Height(), true) && !m.jumped(m.sidePage()) {
			m.scroll().PageDown()
		}
	case key.Matches(keyMsg, k.PageUp):
		if !m.pageCheckLine(-m.view.Height(), true) && !m.jumped(-m.sidePage()) {
			m.scroll().PageUp()
		}
	case key.Matches(keyMsg, k.HalfPageDown):
		// A half-page key on the Checks column is deliberately inert. The
		// column has its own cursor, and scrolling a pane that does not have the
		// keys separates its viewport from the cursor inside it.
		if m.tab == tabChecks && m.focus == paneSide {
			break
		}
		if !m.pageCheckLine(max(1, m.view.Height()/2), false) && !m.jumped(m.sidePage()/2) {
			m.scroll().HalfPageDown()
		}
	case key.Matches(keyMsg, k.HalfPageUp):
		if m.tab == tabChecks && m.focus == paneSide {
			break
		}
		if !m.pageCheckLine(-max(1, m.view.Height()/2), false) && !m.jumped(-m.sidePage()/2) {
			m.scroll().HalfPageUp()
		}
	}

	return m, tea.Batch(m.armCommit(), m.armJob(), m.armJobRender())
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

// move is a row in the left column, a row on the rail, a rendered line in a
// job log, and a scrolled line everywhere else. The first three carry cursors;
// panes holding prose do not.
//
// The rail's cursor stops at each end rather than coming back round, and hands
// the key to the pane there. So does the conversation's ring: a cursor that
// jumped from the last stop to the first would move the reader a screen away
// from what they were reading, and a page of cards is the deeper of the two.
// Scrolling instead is the honest answer to a key with nowhere to put a cursor,
// though bringing the focused control into view leaves the rail at its end
// anyway on every layout here, so it currently has nothing left to scroll.
func (m *Model) move(delta int) {
	switch {
	case m.moveCheckLine(delta):
		return
	case m.moveDiffCursor(delta):
		return
	case m.sideDriving():
		m.moveSide(delta)
		return
	case m.railDriving() && m.stepFocus(delta):
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
//
// The rail is not in it. Those keys go to the ends of a pane, and the rail has
// rows past its last control that are the reason a reader presses them.
func (m *Model) jumped(rows int) bool {
	if !m.sideDriving() {
		return false
	}
	m.moveSide(rows)
	return true
}

// railDriving is whether the line keys belong to the rail's cursor. Its rows
// are a list of controls rather than paragraphs of anything, so they are walked
// the way the column beside them is: the cursor moves and the pane follows it.
//
// A rail with no walkable row keeps the scrolling keys. Every row can be inert
// at once, on a pull request nobody may change, and the rail is taller than a
// short frame whether or not there is anything to press.
func (m Model) railDriving() bool {
	return m.focus == paneRail && m.railVisible() && m.railRing.stops() > 0
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
		return len(m.check.rows)
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

// changeTab walks the strip, wrapping at either end.
func (m *Model) changeTab(delta int) tea.Cmd {
	return m.goToTab((m.tab + delta + len(tabs)) % len(tabs))
}

// goToTab moves to a tab and takes the scroll position with it. The offset is
// restored after the content, because SetYOffset clamps to what is there.
//
// Both diff tabs open on content rather than on an empty pane. Files asks the
// root outright; Commits arms the same wait a cursor move does, so landing on
// the tab loads whatever the cursor is already pointing at. The fetch is the
// root's to make either way, and the tab is the only thing that says it is
// wanted.
//
// The destination is named rather than stepped, because a jump from the
// conversation into the diff owes the screen everything a tab switch does.
func (m *Model) goToTab(at int) tea.Cmd {
	m.offsets[m.tab] = m.view.YOffset()
	m.panes[m.tab] = m.focus
	m.tab = at

	// The conversation is the only tab a box is drawn on, so a popup anchored to
	// one has nothing under it anywhere else.
	m.clearMention()

	// The column comes and goes with the tab, and focus cannot sit on a pane
	// that is no longer on screen.
	m.layout()
	m.view.SetYOffset(m.offsets[m.tab])
	m.showSideCursor()

	// A tab gives back the pane the reader left it on. Focus is one field where
	// the scroll is four, and a tab that takes its own column takes the focus
	// with it, so without this the way back is a pane nobody chose: the column
	// leaves the screen and layout puts the keys wherever is left.
	//
	// The leading pane takes the keys on the way in to the screen and not here.
	// A reader changing tab has already chosen a pane, and handing the keys back
	// to the rail on every press of the strip makes them ask for the page again
	// each time they come round to it.
	m.focusPane(m.panes[m.tab])

	// Commits opens with an empty diff pane, so the column is the only thing on
	// the tab there is anything to do with. Checks opens on a full pane, but
	// every key a reader presses there is picking a workflow. On arrival alone,
	// because a reader who walked off the column once meant it.
	if m.panes[m.tab] == paneNone && (m.tab == tabCommits || m.tab == tabChecks) && m.sideVisible() {
		m.focus = paneSide
		m.syncContent()
	}

	// Asked once per screen rather than once per cached diff. The screen is new
	// on every open, so a pull request reopened after a push fetches its diff
	// again instead of reading the one from before the push for the rest of the
	// session, and revisiting the tab on this screen still costs nothing.
	if m.tab != tabFiles || m.filesAsked || m.files.Status == store.StatusLoading {
		return tea.Batch(m.armCommit(), m.armJob(), m.armJobRender())
	}
	m.filesAsked = true

	id := m.pr.ID
	return func() tea.Msg { return NeedFilesMsg{ID: id} }
}

// leadPane gives the keys to the leftmost pane on screen. A lone pane is
// already holding them, so this is only ever a move onto a column or the rail,
// and a frame too narrow for a second one has no lead to give.
func (m *Model) leadPane() {
	if panes := m.visiblePanes(); len(panes) > 1 {
		m.focusPane(panes[0])
	}
}

// focusRing is the ring the focused pane walks, with the viewport it scrolls
// in. Nil on a pane that has a cursor of its own: the file tree, the commit
// column and the workflow column are all walked by j and k already.
func (m *Model) focusRing() (*ring, *viewport.Model) {
	switch {
	case m.focus == paneRail && m.railVisible():
		return &m.railRing, &m.railView
	case m.focus == paneMain && m.ringTab():
		return &m.pageRing, &m.view
	}
	return nil, nil
}

// stepFocus walks the ring one item and brings what it lands on into view. It
// reports whether the ring took the key, which is what lets a caller hand the
// key on to the pane rather than swallowing it.
//
// The body is rebuilt before the offset moves, because a focused card renders
// differently and SetYOffset clamps to the content the viewport is holding.
func (m *Model) stepFocus(delta int) bool {
	r, vp := m.focusRing()
	if r == nil {
		return false
	}

	top := bodyTop(vp)
	if !r.step(delta, top, vp.Height()) {
		return false
	}

	m.syncContent()
	m.showFocus(r, vp, top)
	return true
}

// walkDiff steps the diff's ring, taking the main pane first. A reader asking
// for the next block is asking for the pane the blocks are in.
func (m *Model) walkDiff(delta int) {
	// Taking the pane can land the cursor on a block, and that landing is the
	// move the key asked for. Stepping again on top of it would skip the first
	// block on the page, which is the one a reader pressing a brace from the
	// column means.
	if m.focus != paneMain && m.focusPane(paneMain) {
		return
	}

	// A block is what the braces name, so they land on its own head rather than
	// on the row the cursor last walked to inside it.
	m.unpoint()
	if !m.stepFocus(delta) {
		m.crossFile(delta)
	}
}

// showFocus brings the focused block into view. A comment in a diff hangs under
// the line it answers, so it opens above that rather than on the top row.
func (m *Model) showFocus(r *ring, vp *viewport.Model, top int) {
	at := r.index()
	if m.tab == tabFiles && at >= 0 && r.items[at].kind != focusHunk && !r.items[at].whole(top, vp.Height()) {
		vp.SetYOffset(contentLead + m.jumpTop(m.shownPath, r.items[at].start))
		return
	}
	vp.SetYOffset(contentLead + r.show(top, vp.Height()))
}

// bodyTop is the viewport's offset in the lines the ring recorded, which sit
// one below it: every pane opens with a blank the items do not count.
//
// It is not clamped at zero. Clamping on the way in while contentLead goes back
// on the way out moves the page a line at the top of a scrollable pane, on a
// keypress that was meant to leave it alone.
func bodyTop(vp *viewport.Model) int { return vp.YOffset() - contentLead }

// toggleBlockFold moves the focused prose or hunk from its resting fold state.
// There is nothing to move with no block focused, and rail rows answer to it
// with nothing.
//
// A card scrolled off the screen is not the one the reader means, any more than
// it is the one tab lands on. Acting on it would unfold something out of sight
// and drag the page back to it.
func (m *Model) toggleBlockFold() bool {
	r, vp := m.focusRing()
	if r == nil || (!r.on.kind.prose() && r.on.kind != focusHunk) {
		return false
	}

	top := bodyTop(vp)
	if !r.live(top, vp.Height()) {
		return false
	}

	key := m.foldTarget()
	if key.kind == focusHunk {
		m.open[key] = !m.hunkOpen(key)
	} else {
		m.open[key] = !m.open[key]
		m.conv.ok = false
	}

	// Unfolding pushes everything under it down. Without this the card that
	// just grew opens below the window it was read in.
	m.syncContent()
	m.showFocus(r, vp, top)
	return true
}

// foldTarget is what space folds.
//
// A resolved thread answers with the whole card: closed is its resting state and
// opening it is the only thing space could usefully mean there. Anywhere else the
// focus already names one comment, its own or the one its card was opened with,
// and the fold is per comment on both tabs that draw one.
func (m Model) foldTarget() focusKey {
	if m.mainRing().on.kind == focusHunk {
		return m.mainRing().on
	}
	t, ok := m.threadOnRing()
	if !ok {
		return m.mainRing().on
	}
	if t.IsResolved {
		return threadKey(t)
	}
	if within := m.within(t); within != "" {
		return focusKey{kind: focusThreadComment, id: within}
	}
	return m.mainRing().on
}

// visiblePanes is the panes on screen, left to right. It is the one place that
// order is written down.
//
// The enum cannot carry it. The rail and the column both sit on the left and
// are never on screen together, so stepping focus by adding one to the enum is
// right on whichever kind of tab the order was written for and wrong on the
// other.
func (m Model) visiblePanes() []pane {
	out := make([]pane, 0, 2)
	for _, p := range []pane{paneRail, paneSide, paneMain} {
		if m.paneVisible(p) {
			out = append(out, p)
		}
	}
	return out
}

// focusPane moves focus to a pane, and does nothing where that pane is not on
// screen.
//
// It reports whether taking the pane also landed a cursor, so a caller that was
// going to step can tell the arrival was the step.
func (m *Model) focusPane(want pane) bool {
	if !m.paneVisible(want) {
		return false
	}
	m.focus = want
	m.syncContent()
	return m.landCursor()
}

// stepPane moves focus one pane along the screen. Both ends are boundaries
// rather than seams, which is what every other cursor here does.
//
// A focus that is on no visible pane takes the key and does nothing, and it
// cannot arise: layout puts the focus back on the main pane whenever the one
// holding it leaves the screen, and layout runs on every resize and every tab
// change. The loop is written to fall out rather than to assume it, because
// what makes it safe is forty lines away in another function.
func (m *Model) stepPane(delta int) {
	panes := m.visiblePanes()
	for i, p := range panes {
		if p != m.focus {
			continue
		}
		if next := i + delta; next >= 0 && next < len(panes) {
			m.focusPane(panes[next])
		}
		return
	}
}

// landCursor puts the cursor on the first stop of whichever ring has the keys,
// when it has none. What is lit is what says where the next key acts, and a
// pane that has to be pressed once before it will say so is one the reader has
// to guess at.
//
// The rail always did this. The conversation and the diff did not: both opened
// with nothing lit at all, so the first press of a motion key was spent
// arriving rather than moving.
//
// A cursor already placed is left where it is. Coming back to a pane and
// finding it at the top would throw away the row the reader walked to, and a
// refetch is not a reason to move anybody.
//
// It reports whether it placed one, because arriving somewhere is a move: a
// caller that was about to step has already had what it asked for.
func (m *Model) landCursor() bool {
	r, _ := m.focusRing()
	if r == nil || r.index() >= 0 {
		return false
	}
	return m.stepFocus(1)
}

// focusIndex answers a digit with the pane sitting in that position. The panes
// are numbered by where they are rather than by what they hold, so 1 is the
// column or the rail, whichever this tab has, and 2 is what it sits beside.
func (m *Model) focusIndex(digit string) {
	n, err := strconv.Atoi(digit)
	if err != nil {
		return
	}
	if panes := m.visiblePanes(); n >= 1 && n <= len(panes) {
		m.focusPane(panes[n-1])
	}
}

func (m Model) paneVisible(p pane) bool {
	switch p {
	case paneSide:
		return m.sideVisible()
	case paneRail:
		return m.railVisible()
	case paneMain:
		return true
	}
	// paneNone is a tab nobody has opened, not a pane. Answering true here puts
	// the keys on it the first time a tab hands its parked pane back.
	return false
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

	// The form sizes its own boxes and cannot do it while rendering: View is
	// reached through value receivers, so a width set there is set on a copy.
	m.merging.resize(width, height)
}

// layout sizes the panes for the current frame, tab, and rail state.
func (m *Model) layout() {
	// Every block is measured against a width, so none of them survive one.
	m.conv.ok = false

	// Focus follows the panes: a tab switch or a resize can take the one that
	// had it off the screen. It lands on the leading pane rather than on the
	// main one, because the rail and the file column are exclusive and sit in
	// the same place: a column going away leaves whatever stands where it stood,
	// and on a tab with nothing beside the page that is the page itself.
	if !m.paneVisible(m.focus) {
		m.focus = m.visiblePanes()[0]
	}

	// The header is pinned above the panes, so what they divide is the frame it
	// leaves. Measured off the rendered block rather than counted: a short frame
	// keeps the panes their floor and gives the header what is left.
	paneHeight := m.height
	if head := m.head(); head != "" {
		paneHeight = max(0, m.height-strings.Count(head, "\n")-1)
	}

	mainWidth := m.width
	if m.sideVisible() {
		column := m.sideColumn()
		mainWidth -= column
		m.side = m.side.Size(column, paneHeight)
		m.sideView.SetWidth(m.side.InnerWidth())
		m.sideView.SetHeight(m.sideHeight())
	}
	if m.railVisible() {
		// An overlaid rail costs the conversation no width. It covers the right of
		// it instead, and toggling the rail then relays nothing out.
		if m.railColumn() {
			mainWidth -= columnWidth
		}
		m.rail = m.rail.Size(columnWidth, paneHeight)
		m.railView.SetWidth(m.rail.InnerWidth())
		m.railView.SetHeight(m.rail.InnerHeight())
	}

	m.main = m.main.Size(mainWidth, paneHeight).Header(m.mainHeading())
	m.view.SetWidth(m.main.InnerWidth())
	// The pinned heading is drawn by the pane, above the window rather than in
	// it, so the viewport is short by whatever the pane spends on it.
	m.view.SetHeight(max(0, m.main.InnerHeight()-(m.main.Above()-1)))
	m.syncContent()

	// The leading pane takes the keys, once, on the way in. It is the one the
	// reader navigates with, and it is numbered first because it is where the
	// eye lands. Only here: a reader who has moved has chosen a pane, and this
	// runs on every resize.
	//
	// The arrival is what is marked, not the handover. A frame with no width
	// yet has not arrived anywhere, and one too narrow for a second pane has
	// arrived with no lead to take: both are settled here rather than later, or
	// widening the terminal would move the keys under a reader mid-page.
	if !m.led && m.width > 0 {
		m.led = true
		m.leadPane()
	}

	// Last, because a ring has no stops until a body has been rendered into it.
	// Every arrival runs through here: a resize, a detail, a diff, a tab switch.
	// So this is the one place that has to remember.
	m.landCursor()
}

// railVisible is whether the rail is on screen. Width decides until the reader
// asks, and a reader who asks is answered at every width this client draws.
func (m Model) railVisible() bool {
	if !m.railTab() {
		return false
	}
	// An overlaid rail covers the box a comment is written in, which is drawn
	// down the page rather than over it. It steps aside until the box is done.
	if !m.railColumn() && m.Composing() {
		return false
	}
	if m.railUserSet {
		return m.railOn
	}
	return m.width >= railMinFrame
}

// railColumn is whether the rail takes a column of the frame or lands over the
// right of the conversation. The same pane in the same state either way.
func (m Model) railColumn() bool { return m.width >= railColumnFrom }

// railTab is whether this tab has a rail at all, at any width.
func (m Model) railTab() bool { return !m.sideVisibleTab() }

// ringTab is whether this tab's main pane is made of blocks a ring can walk.
// Commits and Checks are lists with a cursor of their own.
func (m Model) ringTab() bool { return m.tab == tabFiles || m.railTab() }

// mainRing is the page's ring by value, for the readers that cannot take a
// pointer. Zero off a ring tab, or they walk what the last one left behind.
func (m Model) mainRing() ring {
	if !m.ringTab() {
		return ring{}
	}
	return m.pageRing
}

// sideVisibleTab is whether this tab has a column, before width has a say.
func (m Model) sideVisibleTab() bool {
	switch m.tab {
	case tabCommits, tabChecks, tabFiles:
		return true
	}
	return false
}

// sideVisible decides whether the left column is on screen. Files and Commits
// hide theirs below the shared floor so the diff keeps its measure. Checks is
// different: its pane shows one selected job, so hiding the column would leave
// every other job unreachable. Its compact one-line tree stays at every width
// the shell draws.
func (m Model) sideVisible() bool {
	return m.sideVisibleTab() && (m.width >= treeMinFrame || m.tab == tabChecks)
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

// ShowsTimeline is whether the conversation is the tab up. It is the only tab a
// comment, a review or a label reaches, and the only one worth a refetch.
func (m Model) ShowsTimeline() bool { return m.railTab() }

// ShowsChecks is whether the Checks tab is up. The root owns the timer that
// refreshes it, so the tab answers the question without exposing its indices.
func (m Model) ShowsChecks() bool { return m.tab == tabChecks }

// Keys is the keymap live while this screen is up.
func (m Model) Keys() keys.DetailMap { return keys.Detail }

// ShortHelp is the line the status bar carries for this screen, built from what
// the tab on it can actually do. The screen is the only thing that knows: the
// keymap is the same on all four, and the difference is which of them holds
// blocks, folds and a rail.
//
// Checks has a fold only while its cursor is on a multi-job workflow parent.
// It has no rail because it already has a column.
func (m Model) ShortHelp() []key.Binding {
	file := m.fileViewTarget()
	return keys.Detail.ShortHelp(keys.DetailContext{
		Blocks:     m.tab != tabChecks || m.check.job.Loaded,
		Expand:     m.tab == tabFiles || m.railTab() || m.checkFoldable() || m.checkStepFoldable(),
		Rail:       m.railTab(),
		Files:      m.tab == tabFiles,
		Split:      m.tab == tabFiles && m.files.Loaded,
		FileView:   file != nil && !file.Viewing,
		FileViewed: file != nil && file.Viewed == gh.FileViewed,
		JobLog:     m.tab == tabChecks && m.check.job.Loaded,
		JobFailure: m.tab == tabChecks && m.checkHasFailure(),
		JobMatches: m.tab == tabChecks && len(m.check.matchLines) > 0,
		JobRerun:   m.canRerunCheck(),
	})
}

func (m Model) fileViewTarget() *gh.ChangedFile {
	if m.tab != tabFiles {
		return nil
	}
	if m.sideDriving() {
		if m.cursor >= 0 && m.cursor < len(m.rows) {
			return m.rows[m.cursor].file
		}
		return nil
	}
	return m.shownFile()
}

func (m Model) toggleFileViewed() (Model, tea.Cmd) {
	file := m.fileViewTarget()
	if file == nil || file.Viewing {
		return m, nil
	}
	msg := ToggleFileViewedMsg{
		ID:     m.pr.ID,
		Path:   file.Path,
		Viewed: file.Viewed != gh.FileViewed,
	}
	if msg.Viewed {
		if !m.sideDriving() {
			m.pointFileCursor(file.Path)
		}
		m.jumpFile(1)
	}
	return m, func() tea.Msg { return msg }
}

func (m *Model) pointFileCursor(path string) {
	for i, row := range m.rows {
		if row.file != nil && row.file.Path == path {
			m.cursor = i
			return
		}
	}
}

func (m *Model) syncContent() {
	// Code is clipped to the pane rather than wrapped: a line of source folded
	// onto a second row puts its tail under the gutter and every line below it
	// out of step with its own number. Both tabs that show a diff want it off.
	//
	// So does the conversation, for a different reason. Every block it builds is
	// already wrapped to bodyWidth before it gets here, so soft wrap has nothing
	// to fold and spends half the cost of setting the content proving it: 12.7ms
	// against 7.0ms on a hundred-and-forty-comment thread, which is paid on
	// every keystroke once a comment is being written into it.
	// Every tab hands the viewport rows already wrapped or clipped. Checks used
	// to leave soft wrap on, making the viewport measure and fold megabytes of
	// log text that cannot overflow its pre-clipped rows.
	m.view.SoftWrap = false

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
	index := map[pane]int{}
	if panes := m.visiblePanes(); len(panes) > 1 {
		for i, p := range panes {
			index[p] = i + 1
		}
	}

	mainView := m.view.View()
	if m.tab == tabChecks {
		mainView = m.paintCheckCursor(mainView)
	}
	panes := []string{m.main.
		Index(index[paneMain]).
		Title(m.mainTitle()).
		Header(m.mainHeading()).
		Footer(scrollFooter(m.view)).
		Focus(m.focus == paneMain).
		Render(mainView)}

	if m.sideVisible() {
		column := m.side.
			Index(index[paneSide]).
			Title(m.sideTitle()).
			Footer(scrollFooter(m.sideView)).
			Focus(m.focus == paneSide).
			Render(m.sideView.View())
		panes = append([]string{column}, panes...)
	}

	rail := ""
	if m.railVisible() {
		rail = m.rail.
			Index(index[paneRail]).
			Title("Details").
			Footer(scrollFooter(m.railView)).
			Focus(m.focus == paneRail).
			Render(m.railView.View())
	}
	if m.railColumn() && rail != "" {
		panes, rail = append([]string{rail}, panes...), ""
	}

	// The overlays composite against the whole screen, so the header goes on
	// first: a modal centred on the panes sits low by half the header.
	frame := lipgloss.JoinHorizontal(lipgloss.Top, panes...)
	lead := 0
	if head := m.head(); head != "" {
		frame = lipgloss.JoinVertical(lipgloss.Left, head, frame)
		lead = strings.Count(head, "\n") + 1
	}

	// Against the left edge rather than centred: it is a column that ran out of
	// room for one, and it lands where that column would have been.
	if rail != "" {
		frame = comp.At(frame, rail, 0, lead, m.width, m.height)
	}

	// The mention popup goes on first, so a picker or the merge form composites
	// over it. The three cannot be up together, and the order says so rather
	// than leaving it to be assumed.
	return m.mergeOverlay(m.pickerOverlay(m.mentionOverlay(frame, lead)))
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
	// Here rather than in each body, because a tab that returns a note instead
	// of its blocks still has to drop the stops the last one left.
	m.pageRing.reset()

	switch m.tab {
	case tabCommits:
		return m.commitBody()
	case tabChecks:
		return m.checkBody()
	case tabFiles:
		return m.filesBody()
	}

	// A conversation with nothing in it yet is one block saying why, and it goes
	// in the middle of the pane rather than at the top of it, where it reads as
	// the first thing said rather than as the page waiting.
	if body, ok := m.conversationNote(); ok {
		// Less the blank the pane opens with, which is a row of the window the
		// block does not get. Centred against the whole height it sits a row low
		// and loses its last line off the bottom.
		return comp.Centered(body, m.bodyWidth(), m.view.Height()-contentLead)
	}
	return m.conversationBody()
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

// sideTitle names the column by what it holds. It carries no count any more:
// the strip above carries all four, where the column could only ever say its
// own, and the same number in both places is one of them saying nothing.
func (m Model) sideTitle() string {
	switch m.tab {
	case tabCommits:
		return "Commits"
	case tabChecks:
		return "Checks"
	}
	return "Files"
}

// mainTitle names the pane by what it holds rather than by the tab it is under.
// The strip already says which tab this is, and a border repeating it spends
// itself on a fact the reader can see a row above.
func (m Model) mainTitle() string {
	switch m.tab {
	case tabCommits, tabFiles:
		return "Diff"
	case tabChecks:
		return "Log"
	}
	return "Feed"
}

// frameHead is the header block every GitHub PR page leads with, before the
// frame has a say in how much of it fits. It comes off the list row, so it is
// on screen before the detail query answers.
//
// It sits above the panes rather than inside one, so it names the pull request
// on every tab and holds its column when a tab opens a left column under it.
// The blank inside it is a blank rather than a rule: the pane border below is
// already a horizontal, and a second one two rows above it read as a box that
// had come open.
func (m Model) frameHead() string {
	width := m.headWidth()

	// Two lines and four corners, one group of facts in each: what it is and how
	// big, then where it is going and how it is doing. Who opened it and when is
	// the group that went, to the status bar: it is read once, and the bar's
	// right side is empty the rest of the time.
	//
	// The same lines whatever the rail is doing. A row that came and went with
	// it would move every pane border under it on the tab switch that hid the
	// rail, which is the jump the header is here to take out.
	//
	// The strip closes the block rather than leading it: it is navigation for
	// the screen below it, and the two rows above it name the pull request all
	// four tabs share. It is held off the pane borders by a blank of its own,
	// the way the two rows above it are held off the strip: an underline landed
	// on a border reads as a rule the border grew rather than as a mark on the
	// tab it sits under.
	lines := []string{m.titleLine(width), m.branchRow(width), "", m.tabStrip(width), ""}
	return indent(strings.Join(lines, "\n"), headGutter)
}

// tabStrip is the detail screen's navigation, on a row of its own above the
// panes. It is not comp.Pane's strip and cannot be: that one is set into a
// border and separates its segments with border-coloured punctuation so the run
// reads unbroken, where this one sits on a bare row and would read as dashes.
//
// The counts go before a name does. At the narrowest frame the shell will draw,
// the counted strip is a few cells over, and clipping there takes the tail off
// the last tab, which may be the one the reader is standing on.
func (m Model) tabStrip(width int) string {
	strip := m.renderTabs(m.tabCounts())
	if lipgloss.Width(strip) > width {
		strip = m.renderTabs(tabs)
	}
	if lipgloss.Width(strip) > width {
		return paint.Clip(strip, width, lipgloss.NewStyle().Foreground(m.theme.Subtle))
	}
	return strip
}

// renderTabs lays the segments out. The current tab is underlined rather than
// marked with a glyph, so it takes no cell of its own and nothing to its right
// steps sideways as the reader moves through the strip.
func (m Model) renderTabs(list []comp.Tab) string {
	active := lipgloss.NewStyle().Foreground(m.theme.Accent).Bold(true).Underline(true)
	idle := lipgloss.NewStyle().Foreground(m.theme.Subtle)
	count := lipgloss.NewStyle().Foreground(m.theme.MutedOrSubtle())

	parts := make([]string, 0, len(list))
	for i, tab := range list {
		style := idle
		if i == m.tab {
			style = active
		}
		part := style.Render(tab.Label)
		if tab.Badge != "" {
			part += count.Render(" " + tab.Badge)
		}
		parts = append(parts, part)
	}
	return strings.Join(parts, "  ")
}

// tabCounts is how much each tab holds. Conversation and Files come off the
// list row, so they are there before the detail query answers; the other two
// wait for it and render bare meanwhile, because a zero claims a tab is empty
// rather than unanswered.
func (m Model) tabCounts() []comp.Tab {
	counted := make([]comp.Tab, len(tabs))
	copy(counted, tabs)

	counted[0].Badge = tabCount(m.pr.Comments)
	counted[tabFiles].Badge = tabCount(m.pr.ChangedFiles)
	if m.detail.Loaded {
		counted[tabCommits].Badge = tabCount(len(m.detail.Detail.Commits))
		counted[tabChecks].Badge = tabCount(len(m.detail.Detail.Rollup.Checks))
	}
	return counted
}

// tabCount is the list screen's own spelling, parentheses and all: they read as
// holding a quantity where a bare number beside a word reads as part of it.
//
// Zero renders as nothing, which reads the same as a count that has not arrived.
// That conflation is worth taking: both are a tab the reader would open to find
// out, and "(0)" on four tabs of a fresh screen is a row of noise.
func tabCount(n int) string {
	if n == 0 {
		return ""
	}
	return "(" + strconv.Itoa(n) + ")"
}

// head is what renders: frameHead clipped to the rows left once the panes have
// their floor. A terminal too short for both keeps the panes, because the
// header names a pull request the reader already chose to open.
func (m Model) head() string {
	lines := strings.Split(m.frameHead(), "\n")

	room := max(0, m.height-headRoom)
	if len(lines) <= room {
		return strings.Join(lines, "\n")
	}

	// Cut short, and never left ending on a blank. The last one sets the header
	// apart from the panes, and there is nothing under it to set it apart from
	// once the rest has gone: the row goes back to the pane, on the frame least
	// able to spare one.
	lines = lines[:room]
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	return strings.Join(lines, "\n")
}

// headWidth is the measure the header is set to. The frame rather than the
// conversation's own: the header belongs to the screen, the way the status bar
// closing it does, and a block centred on the main pane would move with it.
func (m Model) headWidth() int { return max(1, m.width-headGutter*2) }

// titleLine is the number, the title, and what the pull request changes pushed
// to the far edge.
func (m Model) titleLine(width int) string {
	// The number leads, in the accent the list numbers rows with, so the same
	// pull request reads the same on both screens.
	lead := lipgloss.NewStyle().Foreground(m.theme.Accent).Bold(true).
		Render("#"+strconv.Itoa(m.pr.Number)) + " " +
		lipgloss.NewStyle().Foreground(m.theme.Text).Bold(true).Render(m.pr.Title)

	return m.spread(lead, m.changes(), width)
}

// spread lays left against the header's left edge and right against its right.
// The right half is a fixed few cells on both rows that use this and the left
// half is not, so the left is the one that gives way rather than pushing what
// sits at the edge off the line.
//
// Nothing clips the header the way the panes clip their content, so a line that
// overran its width would render wider than the terminal it was given.
func (m Model) spread(left, right string, width int) string {
	faint := lipgloss.NewStyle().Foreground(m.theme.Subtle)

	// The left half needs a cell of its own and a space before the right one.
	// Under that it goes entirely: a fragment and an ellipsis say less than what
	// stands at the edge.
	room := width
	if right != "" {
		room = width - lipgloss.Width(right) - 1
	}
	if room < 1 {
		// No room for the left half, so the right one stands alone. Clipped
		// only where it overruns on its own: clipping it because the left could
		// not fit beside it marks a cut that never happened.
		if lipgloss.Width(right) > width {
			return paint.Clip(right, width, faint)
		}
		return right
	}
	if lipgloss.Width(left) > room {
		left = paint.Clip(left, room, faint)
	}

	gap := max(0, width-lipgloss.Width(left)-lipgloss.Width(right))
	return left + strings.Repeat(" ", gap) + right
}

// Readout is who raised the pull request and how long ago, for the status bar.
// The root asks the screen that has focus, the same way it asks for the hints
// beside it.
//
// Compact rather than the sentence the header used to carry. The bar's left
// half is a line of key hints that runs most of the width, so a clause spelled
// out is one clipped mid-handle on any frame a reader is likely to have.
//
// Either half can be missing: a deleted account has no login, and the row the
// list opens with carries no timestamp until the detail query answers.
func (m Model) Readout() string {
	age, login := comp.RelativeTime(m.pr.CreatedAt), comp.Handle(m.pr.Author.Login)

	switch {
	case age != "" && login != "":
		return login + " · " + age
	case age != "":
		return age
	}
	return login
}

// branchLine is where the work is going and where it came from. It stays on one
// line: the head branch is the long one and the one carrying a ticket key at
// the front, so it is what gives way rather than the line wrapping.
func (m Model) branchLine(width int) string {
	faint := lipgloss.NewStyle().Foreground(m.theme.Subtle)
	arrow := " ← "

	base, head := m.pr.BaseRefName, m.pr.HeadRefName
	baseRoom, headRoom := shareBranchRoom(lipgloss.Width(base), lipgloss.Width(head),
		min(width, branchMeasure)-lipgloss.Width(arrow))

	return clipTo(faint.Render(base), baseRoom, faint) +
		faint.Render(arrow) +
		clipTo(faint.Render(head), headRoom, faint)
}

// shareBranchRoom divides what the line has between the two names.
//
// A name that fits inside half is never cut, and the room it does not want goes
// to the other one: main merged into from a long branch takes its four columns
// and leaves the rest, which is the case worth getting right because it is
// nearly every pull request. Only where neither will fit is the room split, and
// then it is halved, because there is nothing to choose between two names that
// are both too long.
//
// The odd column goes to the head. It is the name that says what is being
// merged, and the base is usually the one a reader already knows.
func shareBranchRoom(base, head, room int) (int, int) {
	if room <= 0 {
		return 0, 0
	}
	if base+head <= room {
		return base, head
	}

	half := room / 2
	switch {
	case base <= half:
		return base, room - base
	case head <= half:
		return room - head, head
	}
	return half, room - half
}

// branchRow is the second line: where the work is going, and where it stands.
//
// The two halves are measured together rather than one after the other. spread
// gives the right half the width and clips the left, and branchLine has already
// cut its names to the room it thought it had; handed the frame, it would cut a
// name to a width the row does not have and spread would then cut it again,
// putting an ellipsis after an ellipsis. So the status is measured first and the
// branches are told what is left.
func (m Model) branchRow(width int) string {
	status := m.statusHalf()

	room := width
	if status != "" {
		room = width - lipgloss.Width(status) - 1
	}
	return m.spread(m.branchLine(room), status, width)
}

// statusHalf is where the pull request stands, with where the checks and the
// review got to after it. The state always has something to say, so this is
// never empty even when the rollup behind it is.
func (m Model) statusHalf() string {
	label, c := comp.PRStateLabel(m.theme, m.pr)
	icon, _ := comp.PRStateIcon(m.theme, m.pr)

	state := lipgloss.NewStyle().Foreground(c).Render(icon + " " + label)
	if rollup := m.rollup(); rollup != "" {
		return state + lipgloss.NewStyle().Foreground(m.theme.Subtle).Render(" · ") + rollup
	}
	return state
}

// changes is how much the pull request touches: the file count, then the diff
// stat in the colors the list gives its own columns. The count is marked with a
// glyph rather than the word, the same way the rail's own Changes row writes
// the pair.
func (m Model) changes() string {
	files := lipgloss.NewStyle().Foreground(m.theme.Subtle).
		Render(strconv.Itoa(m.pr.ChangedFiles) + " " + glyphFile)

	return files + "  " +
		lipgloss.NewStyle().Foreground(m.theme.Success).Render("+"+strconv.Itoa(m.pr.Additions)) +
		" " + lipgloss.NewStyle().Foreground(m.theme.Error).Render("−"+strconv.Itoa(m.pr.Deletions))
}

// rollup is where the checks got to and what the reviewers decided, which are
// the two things standing between the pull request and a merge.
//
// It is ungated. The rail carries both as controls and the rail is on one tab
// of four, so reading it off the rail alone would leave three tabs unable to
// say; and a header row that came and went with the rail would move every pane
// border under it on the tab switch that hid it.
func (m Model) rollup() string {
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
	return strings.Join(parts, lipgloss.NewStyle().Foreground(m.theme.Subtle).Render(" · "))
}
