// Package app holds the root Bubble Tea model. It owns the screens, divides the
// frame between them and the status bar, and handles the keys that answer
// whatever has focus. All model mutation happens in Update; commands do the
// asynchronous work and deliver typed messages back.
package app

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/praxis-labs-io/zen-octo/internal/config"
	"github.com/praxis-labs-io/zen-octo/internal/gh"
	"github.com/praxis-labs-io/zen-octo/internal/store"
	"github.com/praxis-labs-io/zen-octo/internal/tui/comp"
	"github.com/praxis-labs-io/zen-octo/internal/tui/keys"
	"github.com/praxis-labs-io/zen-octo/internal/tui/list"
	"github.com/praxis-labs-io/zen-octo/internal/tui/prview"
	"github.com/praxis-labs-io/zen-octo/internal/tui/syntax"
	"github.com/praxis-labs-io/zen-octo/internal/tui/theme"
)

// GitHub is the slice of the client this model needs. Declaring it here rather
// than in the gh package is what lets tests drive the UI without a network.
type GitHub interface {
	Viewer(ctx context.Context) (gh.ViewerResult, error)
	SearchPullRequests(ctx context.Context, query string, limit int) (gh.SearchResult, error)
	PullRequest(ctx context.Context, id, headRef string) (gh.DetailResult, error)
	Pulse(ctx context.Context, id string) (gh.PulseResult, error)
	PullRequestFiles(ctx context.Context, prID, repo string, number, changedFiles int) (gh.FilesResult, error)
	CommitFiles(ctx context.Context, repo, sha string) (gh.FilesResult, error)
	Job(ctx context.Context, repo string, jobID int64) (gh.Job, error)
	JobLogs(ctx context.Context, repo string, jobID int64) ([]byte, error)
	RerunJob(ctx context.Context, repo string, jobID int64) (time.Time, error)
	SetFileViewed(ctx context.Context, prID, path string, viewed bool) error
	AddComment(ctx context.Context, subjectID, body string) (gh.CommentResult, error)
	AddReply(ctx context.Context, threadID, body string) (gh.CommentResult, error)
	SetThreadResolved(ctx context.Context, threadID string, resolved bool) (gh.ThreadResult, error)

	// SetReaction takes a node id and no kind: one pair of calls covers a
	// comment, a review, a review comment and the pull request whose
	// description is on screen.
	SetReaction(ctx context.Context, subjectID string, content gh.ReactionContent, on bool) (gh.ReactionResult, error)

	// The kind picks the mutation: one comment type up here, three documents
	// down there. A review's own body has no delete, and DeleteComment refuses
	// one rather than sending a call GitHub answers with a refusal.
	UpdateComment(ctx context.Context, kind gh.CommentKind, id, body string) (gh.CommentResult, error)
	DeleteComment(ctx context.Context, kind gh.CommentKind, id string) error

	// SetBody is the description, which is a field of the pull request rather
	// than a comment however it reads on the page.
	SetBody(ctx context.Context, prID, body string) (gh.BodyResult, error)
	RepoMeta(ctx context.Context, repo string) (gh.RepoMetaResult, error)
	SetLabels(ctx context.Context, prID string, labelIDs []string) (gh.LabelsResult, error)
	SetState(ctx context.Context, prID string, to gh.PRTransition) (gh.PRStateResult, error)
	SetAssignees(ctx context.Context, prID string, assigneeIDs []string) (gh.AssigneesResult, error)
	SetBase(ctx context.Context, prID, base string) (gh.BaseResult, error)

	// Merge and DeleteRef are one intention and two calls. The second cannot
	// undo the first, which is why it runs off the back of the first's answer
	// rather than beside it.
	Merge(ctx context.Context, prID string, opts gh.MergeOptions) (gh.MergeResult, error)
	DeleteRef(ctx context.Context, refID string) error

	// Branches is a search rather than a read of the repository, and it is the
	// one call keyed by what somebody typed. RepoMeta beside it is fetched once.
	Branches(ctx context.Context, repo, query string) (gh.BranchResult, error)

	// The two REST writes, addressed by repository and number rather than by
	// node id: GraphQL cannot request Copilot, so this pair goes the other way.
	RequestReviews(ctx context.Context, repo string, number int, logins []string) error
	RemoveReviewRequests(ctx context.Context, repo string, number int, logins []string) error
}

// The viewer is asked for once, at startup. It names nothing because there is
// only ever one of it, and the failure carries nothing because there is nowhere
// to put it: see the handler.
type viewerFetchedMsg struct {
	res gh.ViewerResult
}

type viewerFailedMsg struct{}

type sectionFetchedMsg struct {
	index int
	res   gh.SearchResult
}

type sectionFailedMsg struct {
	index int
	err   error
}

// The detail messages name a pull request rather than a screen. Open one,
// escape, open another, and the first response still arrives; the id is what
// keeps it off the screen that replaced it.
type detailFetchedMsg struct {
	id  string
	res gh.DetailResult
}

type detailFailedMsg struct {
	id  string
	err error
}

// The diff is a second request, made the first time someone opens the Files
// tab. It names its pull request for the same reason the detail messages do.
type filesFetchedMsg struct {
	id  string
	res gh.FilesResult
}

type filesFailedMsg struct {
	id  string
	err error
}

type fileViewedMsg struct {
	id  string
	key string
}

type fileViewFailedMsg struct {
	id   string
	key  string
	path string
	err  error
}

// A commit's diff is a request of its own, made when someone selects the commit
// on the Commits tab. It names its commit rather than its pull request: the
// same commit is the same diff wherever it is opened from.
type commitFilesFetchedMsg struct {
	sha string
	res gh.FilesResult
}

type commitFilesFailedMsg struct {
	sha string
	err error
}

type jobFetchedMsg struct {
	id  int64
	job gh.Job
	log []byte
}

type jobFailedMsg struct {
	id  int64
	job gh.Job
	err error
}

// A comment is applied here before it is sent, so both outcomes name the write
// rather than the pull request alone. Two comments can be in flight at once,
// and the key is what tells one answer from the other.
//
// The failure carries the body back. The pane emptied when the write left, and
// the words are the only thing in this program that cannot be fetched again.
type commentPostedMsg struct {
	id  string
	key string
	res gh.CommentResult
}

type commentFailedMsg struct {
	id   string
	key  string
	body string
	err  error
}

// A reply settles the same way and lands somewhere else, so it answers for
// itself. The thread comes back on the failure because the words go back into
// the box that was open on it, and by then that box has closed.
type replyPostedMsg struct {
	id  string
	key string
	res gh.CommentResult
}

type replyFailedMsg struct {
	id     string
	key    string
	thread string
	body   string
	err    error
}

// A resolve settles the same way and writes no words, so the failure carries
// only what the toast has to say: which direction the press was going. The
// store puts the thread back on its own.
type threadResolvedMsg struct {
	id  string
	key string
	res gh.ThreadResult
}

type resolveFailedMsg struct {
	id       string
	key      string
	resolved bool
	err      error
}

// fetchTimeout bounds a single request. Without it a half-open socket leaves
// the UI spinning with no error and no way out but quitting.
const fetchTimeout = 30 * time.Second

// statusBarHeight is the one line the status bar occupies. It is subtracted
// once, here, and every region below is told what it got.
const statusBarHeight = 1

type screen int

const (
	screenList screen = iota
	screenDetail
)

// Model is the root of the UI.
type Model struct {
	client GitHub
	theme  theme.Theme
	syntax syntax.Syntax
	limit  int

	store store.Store

	screen screen
	list   list.Model
	detail prview.Model
	status comp.StatusBar
	toasts comp.Toasts
	help   help.Model

	// notice reports a recoverable config problem, like a theme name that isn't
	// registered. Silently falling back reads as "my config is ignored".
	notice   string
	showHelp bool

	// chords is whether the terminal can tell ctrl+enter from enter. It is held
	// here rather than on the screen because the screen is rebuilt on every
	// open and the terminal only answers once.
	chords bool

	// refreshing is the sections the last r press actually started. A refresh
	// returns the same rows more often than not, so the toast is the only sign
	// it happened, and it has to report the sections it fetched rather than
	// every section configured: store.Begin refuses one already in flight.
	refreshing []int

	// detailRefreshing is the same for the detail screen, and refreshSpin is the
	// glyph that stands in for the body spinner the detail screen deliberately
	// does not run over content already on it.
	detailRefreshing detailRefresh
	refreshSpin      comp.Spinner

	// poller is the background beat's own bookkeeping, kept apart from the two
	// above because nothing it starts is a refresh anybody asked for.
	poller poller

	width  int
	height int
}

// detailRefresh is the requests one r on the detail screen started, so the
// toast waits for the last of them rather than for whichever answers first.
//
// The legs are held apart rather than counted. A refresh does not always start
// all three, and a response to something else that happened to be out would
// otherwise take the slot of one that never came back.
type detailRefresh struct {
	detail leg
	files  leg
	commit leg
}

// leg is one request a refresh started. The key is what a response has to name
// to belong to it, and an empty one is a leg this refresh never started: the
// toast reports what it asked for, so a leg that never ran cannot be the one it
// says failed.
type leg struct {
	key    string
	done   bool
	failed bool
}

func (l leg) started() bool { return l.key != "" }
func (l leg) running() bool { return l.started() && !l.done }

// claim takes a response and reports whether this leg was waiting on it.
func (l *leg) claim(key string, err error) bool {
	if !l.running() || key != l.key {
		return false
	}
	l.done, l.failed = true, err != nil
	return true
}

func (r detailRefresh) running() bool {
	return r.detail.running() || r.files.running() || r.commit.running()
}

// refreshLeg names the request a response answers.
type refreshLeg int

const (
	legDetail refreshLeg = iota
	legFiles
	legCommit
)

// New builds the root model over the configured PR sections.
func New(cfg *config.Config, client GitHub) Model {
	th, ok := theme.Get(cfg.Theme)

	// The syntax palette is a separate question from the chrome's. A theme
	// names the Chroma style that matches it, and config overrides that for a
	// theme with no counterpart.
	syntaxName := cmp.Or(cfg.SyntaxTheme, th.Syntax)
	syn, syntaxOK := syntax.New(syntaxName)

	h := help.New()
	h.Styles = helpStyles(th)

	m := Model{
		client: client,
		theme:  th,
		syntax: syn,
		limit:  cfg.Defaults.PRsLimit,
		store:  store.New(cfg.PRSections),
		list:   list.New(th),
		status: comp.NewStatusBar(th),
		help:   h,

		refreshSpin: comp.NewSpinner(th),
	}
	// Init fetches every section, and a command runs off the update loop where
	// it cannot mark anything, so the store is put in that state here.
	m.store.BeginAll()
	m.list.SetSections(m.store.Sections())

	switch {
	case !ok:
		m.notice = fmt.Sprintf("Unknown theme %q, using %s. Known: %s",
			cfg.Theme, th.Name, strings.Join(theme.Names(), ", "))
	case !syntaxOK:
		m.notice = fmt.Sprintf("Unknown syntax theme %q, using Chroma's default. Known: %s",
			syntaxName, strings.Join(syntax.Names(), ", "))
	}
	return m
}

// Init starts the list, asks who the token belongs to, and fetches every
// section. tea.Batch runs its commands concurrently, which is the whole of the
// concurrency here: no goroutine of ours touches the model.
func (m Model) Init() tea.Cmd {
	// The background beat starts here and is armed nowhere else, which is what
	// keeps it to one chain for the life of the session.
	cmds := []tea.Cmd{m.list.Init(), m.fetchViewer(), armPoll()}
	for i, section := range m.store.Sections() {
		cmds = append(cmds, m.fetchSection(i, section.Filters))
	}
	return tea.Batch(cmds...)
}

func (m Model) fetchViewer() tea.Cmd {
	client := m.client
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), fetchTimeout)
		defer cancel()

		res, err := client.Viewer(ctx)
		if err != nil {
			return viewerFailedMsg{}
		}
		return viewerFetchedMsg{res: res}
	}
}

// postComment writes a comment and puts it on the screen before it is sent.
// The card is the acknowledgement; a toast saying "posting" would be a second
// one for the same fact and would take the status bar off the keymap for it.
//
// The store holds the placeholder beside the fetched detail, so an r pressed
// while this is out cannot take it away.
func (m Model) postComment(msg prview.PostCommentMsg) (tea.Model, tea.Cmd) {
	key := m.store.PendingComment(msg.ID, gh.Comment{
		Kind:      gh.CommentIssue,
		Author:    m.store.Viewer(),
		CreatedAt: time.Now(),
		Body:      msg.Body,
	})

	shown := m.detail.SetDetail(m.store.Detail(msg.ID))
	return m, tea.Batch(shown, m.sendComment(msg.ID, key, msg.Body))
}

func (m Model) sendComment(id, key, body string) tea.Cmd {
	client := m.client
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), fetchTimeout)
		defer cancel()

		res, err := client.AddComment(ctx, id, body)
		if err != nil {
			return commentFailedMsg{id: id, key: key, body: body, err: err}
		}
		return commentPostedMsg{id: id, key: key, res: res}
	}
}

// commentLanded swaps the placeholder for what GitHub recorded.
func (m Model) commentLanded(msg commentPostedMsg) (tea.Model, tea.Cmd) {
	m.store.PendingApplied(msg.id, msg.key, msg.res)

	toast := m.toasts.Show(comp.ToastSuccess, "Posted")
	if !m.showing(msg.id) {
		return m, toast
	}
	return m, tea.Batch(m.detail.SetDetail(m.store.Detail(msg.id)), toast)
}

// commentFailed is the revert branch. The placeholder comes off the screen and
// the words go back in the pane, because a comment lost to a dropped
// connection is the one thing here that cannot be fetched again.
func (m Model) commentFailed(msg commentFailedMsg) (tea.Model, tea.Cmd) {
	m.store.PendingReverted(msg.id, msg.key)

	toast := m.toasts.Show(comp.ToastError, "Could not post the comment: "+msg.err.Error())

	// A reader who left has no pane to put the words back into. The toast still
	// goes up: they are about to find the comment is not there.
	if !m.showing(msg.id) {
		return m, toast
	}

	shown := m.detail.SetDetail(m.store.Detail(msg.id))
	restored := m.detail.RestoreDraft(msg.body)
	m.resize()
	return m, tea.Batch(shown, restored, toast)
}

// postReply answers a review thread, putting the reply in the thread before it
// is sent. Same shape as postComment and a different place on the page: the
// store hangs the placeholder off the thread rather than the timeline.
func (m Model) postReply(msg prview.PostReplyMsg) (tea.Model, tea.Cmd) {
	key := m.store.PendingReply(msg.ID, msg.ThreadID, gh.Comment{
		Author:    m.store.Viewer(),
		CreatedAt: time.Now(),
		Body:      msg.Body,
	})

	shown := m.detail.SetDetail(m.store.Detail(msg.ID))
	return m, tea.Batch(shown, m.sendReply(msg, key))
}

func (m Model) sendReply(msg prview.PostReplyMsg, key string) tea.Cmd {
	client := m.client
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), fetchTimeout)
		defer cancel()

		res, err := client.AddReply(ctx, msg.ThreadID, msg.Body)
		if err != nil {
			return replyFailedMsg{id: msg.ID, key: key, thread: msg.ThreadID, body: msg.Body, err: err}
		}
		return replyPostedMsg{id: msg.ID, key: key, res: res}
	}
}

// replyLanded swaps the placeholder for what GitHub recorded.
func (m Model) replyLanded(msg replyPostedMsg) (tea.Model, tea.Cmd) {
	m.store.PendingApplied(msg.id, msg.key, msg.res)

	toast := m.toasts.Show(comp.ToastSuccess, "Replied")
	if !m.showing(msg.id) {
		return m, toast
	}
	return m, tea.Batch(m.detail.SetDetail(m.store.Detail(msg.id)), toast)
}

// replyFailed is the revert branch. The reply comes off the thread and the words
// go back to the thread they were written for, which is not the same place a
// failed comment goes: dropping a reply into the box at the foot of the page
// would file it against the pull request instead.
func (m Model) replyFailed(msg replyFailedMsg) (tea.Model, tea.Cmd) {
	m.store.PendingReverted(msg.id, msg.key)

	toast := m.toasts.Show(comp.ToastError, "Could not post the reply: "+msg.err.Error())
	if !m.showing(msg.id) {
		return m, toast
	}

	shown := m.detail.SetDetail(m.store.Detail(msg.id))
	restored := m.detail.RestoreReply(msg.thread, msg.body)
	m.resize()
	return m, tea.Batch(shown, restored, toast)
}

// resolveThread settles a review thread, closing it on the page before the
// write leaves. The card collapsing is the acknowledgement, the way the
// optimistic comment is one for a comment.
func (m Model) resolveThread(msg prview.ResolveThreadMsg) (tea.Model, tea.Cmd) {
	key := m.store.PendingResolve(msg.ID, msg.ThreadID, msg.Resolved)

	shown := m.detail.SetDetail(m.store.Detail(msg.ID))
	return m, tea.Batch(shown, m.sendResolve(msg, key))
}

func (m Model) sendResolve(msg prview.ResolveThreadMsg, key string) tea.Cmd {
	client := m.client
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), fetchTimeout)
		defer cancel()

		res, err := client.SetThreadResolved(ctx, msg.ThreadID, msg.Resolved)
		if err != nil {
			return resolveFailedMsg{id: msg.ID, key: key, resolved: msg.Resolved, err: err}
		}
		return threadResolvedMsg{id: msg.ID, key: key, res: res}
	}
}

// resolveLanded takes GitHub's answer, which carries the permissions the next
// press needs as well as the state.
func (m Model) resolveLanded(msg threadResolvedMsg) (tea.Model, tea.Cmd) {
	m.store.ResolveApplied(msg.id, msg.key, msg.res)

	said := "Unresolved"
	if msg.res.IsResolved {
		said = "Resolved"
	}

	toast := m.toasts.Show(comp.ToastSuccess, said)
	if !m.showing(msg.id) {
		return m, toast
	}
	return m, tea.Batch(m.detail.SetDetail(m.store.Detail(msg.id)), toast)
}

// resolveFailed is the revert branch. Nothing was typed and no box changed
// height, so the thread going back where it was is the whole of it.
func (m Model) resolveFailed(msg resolveFailedMsg) (tea.Model, tea.Cmd) {
	m.store.ResolveReverted(msg.id, msg.key)

	doing := "unresolve"
	if msg.resolved {
		doing = "resolve"
	}

	toast := m.toasts.Show(comp.ToastError, "Could not "+doing+" the thread: "+msg.err.Error())
	if !m.showing(msg.id) {
		return m, toast
	}
	return m, tea.Batch(m.detail.SetDetail(m.store.Detail(msg.id)), toast)
}

// showing is whether the detail screen is up on this pull request. A response
// outlives the screen that asked for it, and every settle path checks.
func (m Model) showing(id string) bool {
	return m.screen == screenDetail && m.detail.PullRequest().ID == id
}

func (m Model) fetchSection(index int, query string) tea.Cmd {
	client, limit := m.client, m.limit
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), fetchTimeout)
		defer cancel()

		// Expanded here rather than at the call site, so a window named in the
		// filter is measured from when the request goes out.
		res, err := client.SearchPullRequests(ctx, config.ExpandQuery(query, time.Now()), limit)
		if err != nil {
			return sectionFailedMsg{index: index, err: err}
		}
		return sectionFetchedMsg{index: index, res: res}
	}
}

// refresh refetches every section not already on its way and waits on the ones
// that are. The tab counts are on screen too: a partial refresh is a part-true frame.
func (m Model) refresh() (tea.Model, tea.Cmd) {
	sections := m.store.Sections()

	var cmds []tea.Cmd
	started := make([]int, 0, len(sections))
	for i, section := range sections {
		// Begin refuses one already in flight and nothing else, so a section it
		// turns down has an answer coming: wait on that rather than drop it.
		started = append(started, i)
		if m.store.Begin(i) {
			cmds = append(cmds, m.fetchSection(i, section.Filters))
		}
	}
	if len(started) == 0 {
		return m, nil
	}

	m.refreshing = started
	m.list.SetSections(m.store.Sections())
	// The screen's own chain, for a section that has never answered and has the
	// pane; and the bar's, which is the only sign a reload gives.
	return m, tea.Batch(append(cmds, m.list.Init(), m.refreshSpin.Tick())...)
}

// sectionSettled pushes the new snapshot down and, once the sections a refresh
// started have all landed, reports it. Waiting on the whole store instead would
// hold the toast behind a section the refresh never touched.
func (m Model) sectionSettled() (tea.Model, tea.Cmd) {
	sections := m.store.Sections()
	m.list.SetSections(sections)

	if len(m.refreshing) == 0 || stillLoading(sections, m.refreshing) {
		return m, nil
	}

	kind, text := refreshSummary(sections, m.refreshing)
	m.refreshing = nil
	return m, m.toasts.Show(kind, text)
}

func stillLoading(sections []store.Section, indices []int) bool {
	for _, i := range indices {
		if sections[i].Status == store.StatusLoading {
			return true
		}
	}
	return false
}

// open puts the detail screen up over whatever the store already holds for this
// pull request, then fetches anyway. A pull request opened before paints on the
// first frame; the refetch swaps in behind it, and SetContent keeps the scroll
// position, so nothing moves under the reader.
func (m Model) open(pr gh.PullRequest) (tea.Model, tea.Cmd) {
	m.detail = prview.New(m.theme, pr, m.detail.Rail(), m.syntax)
	m.detail.SetChords(m.chords)
	m.detail.SetViewer(m.store.Viewer())
	m.screen = screenDetail
	m.detailRefreshing = detailRefresh{}

	var cmds []tea.Cmd
	if m.store.BeginDetail(pr.ID) {
		cmds = append(cmds, m.fetchDetail(pr.ID, pr.HeadRefName))
	}
	// Init arms this screen's own spinner chain, and the screen is new on every
	// open. Arming it with the fetch instead would leave it frozen on a reopen
	// while the first request is still out: BeginDetail refuses that one, and
	// the old chain's ticks carry a tag the new spinner drops. It costs a tick
	// where there is nothing to wait for, which is what ends the chain anyway.
	cmds = append(cmds, m.detail.Init())

	cmds = append(cmds, m.detail.SetDetail(m.store.Detail(pr.ID)))
	// Reading a held diff is using it. Without this the cache ages a diff on
	// time since it was fetched, and one reopened daily still falls out.
	m.store.UseFiles(pr.ID)
	cmds = append(cmds, m.detail.SetFiles(m.store.Files(pr.ID)))
	m.resize()
	return m, tea.Batch(cmds...)
}

// refreshDetail refetches the pull request on screen. The detail feeds the
// conversation, the commit column and the checks, so it always goes; the diff
// the reader is actually looking at goes with it, because the detail carries
// neither.
//
// The screen keeps what it has throughout. Every Begin refuses only a request
// already out, so a second r while the first is still running asks for whatever
// the first did not, and joins it: the legs merge into the one record rather
// than replacing it, or the earlier press loses the leg it is still waiting on
// and never reports at all.
func (m Model) refreshDetail(msg prview.RefreshMsg) (tea.Model, tea.Cmd) {
	pr := m.detail.PullRequest()
	if m.screen != screenDetail || pr.ID != msg.ID {
		return m, nil
	}

	// The repository's choices go stale too, and nothing else ever drops them:
	// BeginRepoMeta refuses one already loaded, so without this a label created
	// in the browser stays out of the picker for the rest of the session. They
	// are dropped rather than refetched, because the next picker to open is the
	// first thing that needs them and they cost a request.
	//
	// The screen holds its own copy and asks the root only when it has none, so
	// clearing the store alone would leave it opening pickers over the stale
	// set it is still carrying. Both have to go.
	//
	// The branch search goes with them, for the same reason and one more: below
	// comp.Picker's filter threshold there is no field to type a fresh search
	// into, so the sync key is the only way a branch made since startup reaches
	// the picker at all.
	m.store.InvalidateRepoMeta(pr.Repository)
	m.store.InvalidateBranches(pr.Repository)
	// Dropping the held choices opens no picker, and it does ask again where a
	// mention popup is up on them: the reader is mid-word over a list that has
	// just gone empty, and the whole point of the sync key is that what comes
	// back is current.
	cmds := []tea.Cmd{m.detail.SetRepo(store.Repo{})}
	m.detail.SetBranches(store.Branches{})

	started := m.detailRefreshing
	switch {
	case m.store.BeginDetail(msg.ID):
		started.detail = leg{key: msg.ID}
		cmds = append(cmds, m.fetchDetail(msg.ID, pr.HeadRefName))

	// One is already on its way, which on this screen means a write asked for
	// it. Wait on that one rather than reporting nothing: the reader pressed the
	// key and a detail genuinely is in flight, so the spinner and the summary
	// are both true. Without this the key is silent and gets pressed again.
	case m.store.Detail(msg.ID).Status == store.StatusLoading:
		started.detail = leg{key: msg.ID}
	}
	if msg.Files && m.store.BeginFiles(msg.ID) {
		started.files = leg{key: msg.ID}
		// A diff already on the pane stays exactly as it is. Pushing the store's
		// loading state through would throw away its rendered blocks and buy a
		// re-highlight for a frame that reads the same. One that failed has
		// nothing worth keeping, so it takes the loading state and spins.
		if held := m.store.Files(msg.ID); !held.Loaded {
			cmds = append(cmds, m.detail.SetFiles(held))
		}
		cmds = append(cmds, m.fetchFiles(msg.ID, pr.Repository, pr.Number, pr.ChangedFiles))
	} else if msg.Files && m.store.Files(msg.ID).Status == store.StatusLoading {
		// Already out, so wait on it the way the detail above is waited on: a
		// summary that skipped it named half of what r asked for.
		started.files = leg{key: msg.ID}
	}
	if msg.SHA != "" && m.store.BeginCommitFiles(msg.SHA) {
		started.commit = leg{key: msg.SHA}
		if held := m.store.CommitFiles(msg.SHA); !held.Loaded {
			m.detail.SetCommitFiles(msg.SHA, held)
		}
		cmds = append(cmds, m.fetchCommitFiles(pr.Repository, msg.SHA))
	} else if msg.SHA != "" && m.store.CommitFiles(msg.SHA).Status == store.StatusLoading {
		started.commit = leg{key: msg.SHA}
	}
	if m.detail.ShowsChecks() {
		cmds = append(cmds, m.detail.RefreshJob())
	}
	// Everything this refresh would have asked for is already on its way.
	if !started.running() {
		return m, nil
	}

	m.detailRefreshing = started
	// The screen's own chain, for a diff that failed and has no content to hold
	// the pane; and the bar's, which is the only thing on screen saying r did
	// anything at all when the content stays put.
	return m, tea.Batch(append(cmds, m.detail.Init(), m.refreshSpin.Tick())...)
}

// claim takes a response the refresh in flight was waiting on and answers
// whether the caller should stay quiet. The summary is the one toast a refresh
// raises: the per-request error beside it would report the same failure twice.
func (m *Model) claim(which refreshLeg, key string, err error) (tea.Cmd, bool) {
	r := &m.detailRefreshing

	var took bool
	switch which {
	case legDetail:
		took = r.detail.claim(key, err)
	case legFiles:
		took = r.files.claim(key, err)
	case legCommit:
		took = r.commit.claim(key, err)
	}
	if !took {
		return nil, false
	}

	if r.running() {
		return nil, true
	}

	kind, text := detailRefreshSummary(*r, m.detail.PullRequest().Number)
	*r = detailRefresh{}
	return m.toasts.Show(kind, text), true
}

// detailRefreshSummary names what came back, and only what the refresh asked
// for: a leg that never ran is not the one that failed. The diff is named
// rather than counted, because with two requests out "one failed" leaves the
// reader guessing whether the thing in front of them is the stale one.
func detailRefreshSummary(r detailRefresh, number int) (comp.ToastKind, string) {
	var landed, failed []string
	for _, l := range []struct {
		leg  leg
		name string
	}{
		{r.detail, "#" + strconv.Itoa(number)},
		{r.files, "the diff"},
		{r.commit, "the diff"},
	} {
		switch {
		case !l.leg.started():
		case l.leg.failed:
			failed = append(failed, l.name)
		default:
			landed = append(landed, l.name)
		}
	}

	switch {
	case len(failed) == 0:
		return comp.ToastSuccess, "Refreshed " + strings.Join(landed, " and ")
	case len(landed) == 0:
		return comp.ToastError, "Refresh failed"
	default:
		return comp.ToastError, "Refreshed " + strings.Join(landed, " and ") +
			", " + strings.Join(failed, " and ") + " failed"
	}
}

// needFiles answers the screen asking for a diff it does not have. The screen
// cannot fetch, so entering the Files tab reaches the root as a message and the
// request starts here.
func (m Model) needFiles(id string) (tea.Model, tea.Cmd) {
	if m.detail.PullRequest().ID != id || !m.store.BeginFiles(id) {
		return m, nil
	}
	pr := m.detail.PullRequest()
	shown := m.detail.SetFiles(m.store.Files(id))
	return m, tea.Batch(shown, m.fetchFiles(id, pr.Repository, pr.Number, pr.ChangedFiles), m.detail.Init())
}

// fetchFiles carries the repository and number because the diff comes over
// REST, which addresses a pull request by path rather than by node id. The
// count is what the response is measured against to report its overflow.
func (m Model) fetchFiles(id, repo string, number, changedFiles int) tea.Cmd {
	client := m.client
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), fetchTimeout)
		defer cancel()

		res, err := client.PullRequestFiles(ctx, id, repo, number, changedFiles)
		if err != nil {
			return filesFailedMsg{id: id, err: err}
		}
		return filesFetchedMsg{id: id, res: res}
	}
}

// filesSettled pushes a diff into the screen, but only while the screen is
// still showing the pull request it was fetched for.
func (m Model) filesSettled(id string, err error) (tea.Model, tea.Cmd) {
	held := m.store.Files(id)

	// The response that just answered was measured against a base a retarget
	// has since moved, and BeginFiles refused the correction while it was out.
	// Ask again, from wherever the reader now is.
	var owed tea.Cmd
	if err == nil {
		owed = m.correctFiles(id)
	}

	if m.screen != screenDetail || m.detail.PullRequest().ID != id {
		return m, owed
	}

	// A jump waiting on this diff lands inside SetFiles and answers here.
	shown := m.detail.SetFiles(held)
	if cmd, claimed := m.claim(legFiles, id, err); claimed {
		return m, tea.Batch(shown, cmd, owed)
	}
	if err != nil && held.Loaded {
		toast := m.toasts.Show(comp.ToastError, "Could not refresh the diff for #"+strconv.Itoa(m.detail.PullRequest().Number))
		return m, tea.Batch(shown, toast)
	}
	return m, tea.Batch(shown, owed)
}

// needCommit answers the screen asking for a commit's diff, the same way
// needFiles answers it asking for the pull request's.
func (m Model) needCommit(sha string) (tea.Model, tea.Cmd) {
	if m.screen != screenDetail {
		return m, nil
	}

	// A commit's diff is the same wherever it is read, so one already held is
	// pushed rather than fetched again. One already in flight is pushed too:
	// selecting resets the pane to idle, and a spinner over an idle pane stops
	// ticking and sits there until the first response happens to land.
	if held := m.store.CommitFiles(sha); held.Loaded || held.Status == store.StatusLoading {
		// The commit a reader keeps coming back to is the oldest fetch on a long
		// branch, and without this it is the first one dropped.
		m.store.UseCommitFiles(sha)
		m.detail.SetCommitFiles(sha, held)
		return m, m.detail.Init()
	}

	if !m.store.BeginCommitFiles(sha) {
		return m, nil
	}
	m.detail.SetCommitFiles(sha, m.store.CommitFiles(sha))
	return m, tea.Batch(m.fetchCommitFiles(m.detail.PullRequest().Repository, sha), m.detail.Init())
}

func (m Model) fetchCommitFiles(repo, sha string) tea.Cmd {
	client := m.client
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), fetchTimeout)
		defer cancel()

		res, err := client.CommitFiles(ctx, repo, sha)
		if err != nil {
			return commitFilesFailedMsg{sha: sha, err: err}
		}
		return commitFilesFetchedMsg{sha: sha, res: res}
	}
}

// commitFilesSettled pushes a commit's diff into the screen. The screen drops
// it unless that commit is still the one selected, so a response arriving after
// the cursor moved on lands nowhere.
func (m Model) commitFilesSettled(sha string, err error) (tea.Model, tea.Cmd) {
	held := m.store.CommitFiles(sha)
	if m.screen != screenDetail {
		return m, nil
	}

	m.detail.SetCommitFiles(sha, held)
	if cmd, claimed := m.claim(legCommit, sha, err); claimed {
		return m, cmd
	}
	if err != nil && held.Loaded {
		return m, m.toasts.Show(comp.ToastError, "Could not refresh the diff for "+short(sha))
	}
	return m, nil
}

// needJob answers the Checks screen asking for the selected concrete attempt.
// A cached log is pushed immediately; a cold one gets the same loading-first
// lifecycle as a commit diff.
func (m Model) needJob(id int64, refresh bool) (tea.Model, tea.Cmd) {
	if m.screen != screenDetail || id == 0 {
		return m, nil
	}
	if held := m.store.Job(id); held.Status == store.StatusLoading ||
		(held.Loaded && held.Status != store.StatusFailed && !refresh) {
		m.store.UseJob(id)
		return m, m.detail.SetJobAsync(id, held)
	}
	if !m.store.BeginJob(id) {
		return m, nil
	}
	held := m.store.Job(id)
	var loading tea.Cmd
	if !held.Loaded {
		loading = m.detail.SetJob(id, held)
	}
	return m, tea.Batch(m.fetchJob(m.detail.PullRequest().Repository, id), loading, m.detail.Init())
}

func (m Model) fetchJob(repo string, id int64) tea.Cmd {
	client := m.client
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), fetchTimeout)
		defer cancel()

		job, err := client.Job(ctx, repo, id)
		if err != nil {
			return jobFailedMsg{id: id, err: err}
		}
		// GitHub does not publish the downloadable blob until the job has
		// finished. Asking while it is running follows a signed redirect to a
		// blob that does not exist yet and turns normal progress into a 404.
		if job.State == gh.CheckStatePending || job.State == gh.CheckStateExpected {
			return jobFetchedMsg{id: id, job: job}
		}
		log, err := client.JobLogs(ctx, repo, id)
		if err != nil {
			return jobFailedMsg{id: id, job: job, err: err}
		}
		return jobFetchedMsg{id: id, job: job, log: log}
	}
}

func (m Model) jobSettled(id int64, err error, hadLoaded bool) (tea.Model, tea.Cmd) {
	if m.screen != screenDetail {
		return m, nil
	}
	held := m.store.Job(id)
	cmd := m.detail.SetJobAsync(id, held)
	if err != nil && hadLoaded {
		return m, tea.Batch(cmd, m.toasts.Show(comp.ToastError, "Could not refresh the job log"))
	}
	return m, cmd
}

// short is a sha cut to what GitHub prints. A toast has no room for forty
// characters and nobody reads them anyway.
func short(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}

// fetchDetail carries the head branch as well as the id: the query asks how far
// behind the base the branch has fallen, and it needs the name to do it.
func (m Model) fetchDetail(id, headRef string) tea.Cmd {
	client := m.client
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), fetchTimeout)
		defer cancel()

		res, err := client.PullRequest(ctx, id, headRef)
		if err != nil {
			return detailFailedMsg{id: id, err: err}
		}
		return detailFetchedMsg{id: id, res: res}
	}
}

// detailSettled pushes a response into the screen, but only when the screen is
// still showing the pull request it was fetched for.
//
// A failure over a detail already on screen only gets a toast: the screen keeps
// what it had, so without one nothing would say the refetch happened at all.
func (m Model) detailSettled(id string, err error) (tea.Model, tea.Cmd) {
	held := m.store.Detail(id)

	// The store wrote this pull request back over the row search returned, and
	// the list is holding a snapshot taken before it did.
	m.list.SetSections(m.store.Sections())

	// The response that just answered was asked for before a write settled, so
	// the store dropped it. Ask again, from wherever the reader now is: the
	// correction belongs to the store rather than to the screen showing it.
	var owed tea.Cmd
	if err == nil && m.store.StaleDetail(id) {
		owed = m.correctDetail(id)
	}

	// A retarget marked the diff stale and left it for this leg, because the
	// changed-file count its overflow line is measured against arrives with the
	// detail. Nil unless something owes one.
	if err == nil {
		owed = tea.Batch(owed, m.correctFiles(id))
	}

	if m.screen != screenDetail || m.detail.PullRequest().ID != id {
		return m, owed
	}

	armed := m.detail.SetDetail(held)
	if cmd, claimed := m.claim(legDetail, id, err); claimed {
		return m, tea.Batch(armed, cmd, owed)
	}
	if err != nil && held.Loaded {
		return m, tea.Batch(armed,
			m.toasts.Show(comp.ToastError, "Could not refresh #"+strconv.Itoa(m.detail.PullRequest().Number)))
	}
	return m, tea.Batch(armed, owed)
}

// Update applies every message. Nothing else mutates the model.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.resize()
		if m.screen == screenDetail {
			return m, m.detail.Init()
		}
		return m, nil

	case tea.KeyPressMsg:
		return m.handleKey(msg)

	case viewerFetchedMsg:
		m.store.ViewerApplied(msg.res)
		// A screen already open took the login when it opened, which at startup
		// is before this lands. Without this the comment box is headed by nobody
		// for the rest of the session.
		m.detail.SetViewer(m.store.Viewer())
		return m, nil

	case viewerFailedMsg:
		// Nothing is shown, and the error is not kept. The login only changes
		// how a name is written, and a token broken enough to fail this fails
		// every section beside it, which the list says out loud with the reason.
		// A toast here would be the only one at startup, for the one failure the
		// reader cannot see the effect of.
		return m, nil

	case sectionFetchedMsg:
		// No staleness guard: store.Begin refuses a section that already has a
		// request out, so a response always belongs in the slot it names.
		m.store.Applied(msg.index, msg.res)
		m.poller.stampSection(msg.index, len(m.store.Sections()), time.Now())
		return m.sectionSettled()

	case sectionFailedMsg:
		m.store.Failed(msg.index, msg.err)
		m.poller.stampSection(msg.index, len(m.store.Sections()), time.Now())
		return m.sectionSettled()

	case sectionPollFailedMsg:
		// Stamped like any other answer: a beat nobody asked for costs one
		// interval when it fails, rather than being retried on the next.
		m.poller.stampSection(msg.index, len(m.store.Sections()), time.Now())
		// A refresh adopted this one, so the reader did ask. It gets the answer a
		// manual fetch gets: the error on the tab and a place in the summary.
		if slices.Contains(m.refreshing, msg.index) {
			m.store.Failed(msg.index, msg.err)
			return m.sectionSettled()
		}
		m.store.PollFailed(msg.index)
		return m, nil

	case spinner.TickMsg:
		// Both screens get every tick, and each drops the ones that are not its
		// own: comp.Spinner tags them. Delegating by focus instead would kill
		// the list's chain the moment the detail screen opened over a fetch.
		var listCmd, detailCmd tea.Cmd
		m.list, listCmd = m.list.Update(msg)
		m.detail, detailCmd = m.detail.Update(msg)
		spinCmd := m.refreshSpin.Advance(msg, m.refreshRunning())
		return m, tea.Batch(listCmd, detailCmd, spinCmd)

	case comp.ToastExpiredMsg:
		m.toasts.Expire(msg)
		return m, nil

	case detailFetchedMsg:
		// Before the response replaces what is held: the probe arms on a first
		// landing, and past this line one cannot be told from a refetch.
		probe := m.probeMergeability(msg.id, msg.res)

		m.store.DetailApplied(msg.id, msg.res)
		m.poller.stampDetail(msg.id, time.Now())
		model, cmd := m.detailSettled(msg.id, nil)
		return model, tea.Batch(cmd, probe)

	case detailFailedMsg:
		m.store.DetailFailed(msg.id, msg.err)
		m.poller.stampDetail(msg.id, time.Now())
		return m.detailSettled(msg.id, msg.err)

	case pulseFetchedMsg:
		moved := m.store.PulseApplied(msg.id, msg.res)
		m.poller.stampDetail(msg.id, time.Now())
		return m.pulseSettled(msg.id, moved)

	case pulseFailedMsg:
		m.store.PulseFailed(msg.id)
		m.poller.stampDetail(msg.id, time.Now())
		return m, nil

	case pageFailedMsg:
		// The store keeps what it held and the screen is told nothing. The debt
		// stands, and the stamp is what keeps it from being retried every beat.
		m.store.DetailFailed(msg.id, msg.err)
		m.poller.stampDetail(msg.id, time.Now())
		m.poller.stampPageFailed(msg.id, time.Now())
		// Unless r adopted this flight. A leg nobody claims never ends: the bar
		// spins for the rest of the session and the summary never lands.
		cmd, _ := m.claim(legDetail, msg.id, msg.err)
		return m, cmd

	case pollTickMsg:
		return m.poll(msg)

	case checksTickMsg:
		return m.pollChecks(msg)

	case filesFetchedMsg:
		m.store.FilesApplied(msg.id, msg.res)
		return m.filesSettled(msg.id, nil)

	case filesFailedMsg:
		m.store.FilesFailed(msg.id, msg.err)
		return m.filesSettled(msg.id, msg.err)

	case fileViewedMsg:
		m.store.FileViewApplied(msg.id, msg.key)
		if !m.showing(msg.id) {
			return m, nil
		}
		return m, m.detail.SetFiles(m.store.Files(msg.id))

	case fileViewFailedMsg:
		m.store.FileViewReverted(msg.id, msg.key)
		toast := m.toasts.Show(comp.ToastError, "Could not update "+msg.path+": "+msg.err.Error())
		if !m.showing(msg.id) {
			return m, toast
		}
		return m, tea.Batch(m.detail.SetFiles(m.store.Files(msg.id)), toast)

	case commitFilesFetchedMsg:
		m.store.CommitFilesApplied(msg.sha, msg.res)
		return m.commitFilesSettled(msg.sha, nil)

	case commitFilesFailedMsg:
		m.store.CommitFilesFailed(msg.sha, msg.err)
		return m.commitFilesSettled(msg.sha, msg.err)

	case jobFetchedMsg:
		m.store.JobApplied(msg.id, msg.job, msg.log)
		return m.jobSettled(msg.id, nil, false)

	case jobFailedMsg:
		hadLoaded := m.store.Job(msg.id).Loaded
		if msg.job.ID != 0 {
			m.store.JobLogFailed(msg.id, msg.job, msg.err)
		} else {
			m.store.JobFailed(msg.id, msg.err)
		}
		return m.jobSettled(msg.id, msg.err, hadLoaded)

	case prview.NeedFilesMsg:
		return m.needFiles(msg.ID)

	case prview.ToggleFileViewedMsg:
		return m.toggleFileViewed(msg)

	case prview.NeedCommitMsg:
		return m.needCommit(msg.SHA)

	case prview.NeedJobMsg:
		return m.needJob(msg.JobID, msg.Refresh)

	case prview.RerunCheckMsg:
		return m.rerunCheck(msg)

	case checkRerunMsg:
		return m.checkRerunLanded(msg)

	case checkRerunFailedMsg:
		return m.checkRerunFailed(msg)

	case prview.RefreshMsg:
		return m.refreshDetail(msg)

	case prview.PostCommentMsg:
		return m.postComment(msg)

	case prview.EditorFailedMsg:
		return m, m.toasts.Show(comp.ToastError, "Could not open an editor: "+msg.Err.Error())

	case commentPostedMsg:
		return m.commentLanded(msg)

	case commentFailedMsg:
		return m.commentFailed(msg)

	case prview.PostReplyMsg:
		return m.postReply(msg)

	case replyPostedMsg:
		return m.replyLanded(msg)

	case replyFailedMsg:
		return m.replyFailed(msg)

	case prview.EditCommentMsg:
		return m.editComment(msg)

	case commentEditedMsg:
		return m.editLanded(msg)

	case commentEditFailedMsg:
		return m.editFailed(msg)

	case prview.DeleteCommentMsg:
		return m.deleteComment(msg)

	case commentDeletedMsg:
		return m.deleteLanded(msg)

	case commentDeleteFailedMsg:
		return m.deleteFailed(msg)

	case prview.SetBodyMsg:
		return m.setBody(msg)

	case bodySetMsg:
		return m.bodyLanded(msg)

	case bodyFailedMsg:
		return m.bodyFailed(msg)

	case prview.NeedRepoMetaMsg:
		return m.needRepoMeta(msg.Repo)

	case repoMetaFetchedMsg:
		return m.repoMetaLanded(msg)

	case repoMetaFailedMsg:
		return m.repoMetaFailed(msg)

	case prview.SetLabelsMsg:
		return m.setLabels(msg)

	case labelsSetMsg:
		return m.labelsLanded(msg)

	case labelsFailedMsg:
		return m.labelsFailed(msg)

	case prview.SetReviewersMsg:
		return m.setReviewers(msg)

	case reviewersSetMsg:
		return m.reviewersLanded(msg)

	case reviewersFailedMsg:
		return m.reviewersFailed(msg)

	case prview.SetAssigneesMsg:
		return m.setAssignees(msg)

	case assigneesSetMsg:
		return m.assigneesLanded(msg)

	case assigneesFailedMsg:
		return m.assigneesFailed(msg)

	case prview.SetStateMsg:
		return m.setState(msg)

	case stateSetMsg:
		return m.stateLanded(msg)

	case stateFailedMsg:
		return m.stateFailed(msg)

	case prview.NeedBranchesMsg:
		return m.needBranches(msg)

	case branchesFetchedMsg:
		return m.branchesLanded(msg)

	case branchesFailedMsg:
		return m.branchesFailed(msg)

	case prview.SetBaseMsg:
		return m.setBase(msg)

	case baseSetMsg:
		return m.baseLanded(msg)

	case baseFailedMsg:
		return m.baseFailed(msg)

	case prview.MergeMsg:
		return m.merge(msg)

	case mergedMsg:
		return m.mergeLanded(msg)

	case mergeFailedMsg:
		return m.mergeFailed(msg)

	case refDeleteFailedMsg:
		return m.refDeleteFailed(msg)

	case mergeProbeMsg:
		return m.mergeProbe(msg)

	case prview.ResolveThreadMsg:
		return m.resolveThread(msg)

	case threadResolvedMsg:
		return m.resolveLanded(msg)

	case resolveFailedMsg:
		return m.resolveFailed(msg)

	case prview.ReactMsg:
		return m.react(msg)

	case reactedMsg:
		return m.reactionLanded(msg)

	case reactFailedMsg:
		return m.reactionFailed(msg)

	// Nothing failed: the reader asked for a place in the diff that is not in
	// it, and the screen has nowhere of its own to say so.
	case prview.ThreadNotInDiffMsg:
		return m, m.toasts.Show(comp.ToastInfo, msg.Path+" is not in the diff")

	// A fact about the frame rather than a failure: the pane is too narrow to
	// draw two columns of source and the diff is still readable unified.
	case prview.SplitTooNarrowMsg:
		return m, m.toasts.Show(comp.ToastInfo,
			"Side by side needs "+comp.Plural(msg.Short, "more column")+" in the pane")

	// The terminal answers once, at startup, and only the compose pane cares:
	// it decides whether ctrl+enter is a key worth naming in its footer.
	case tea.KeyboardEnhancementsMsg:
		m.chords = msg.SupportsKeyDisambiguation()
		m.detail.SetChords(m.chords)
		return m, nil

	case list.OpenMsg:
		return m.open(msg.PR)

	case list.RefreshMsg:
		return m.refresh()

	case list.CopyLinkMsg:
		return m, copyLinkCmd(msg.PR)

	case prview.CopyLinkMsg:
		return m, copyLinkCmd(msg.PR)

	case list.BrowseMsg:
		return m, browseCmd(msg.PR)

	case prview.BrowseMsg:
		return m, browseCmd(msg.PR)

	case linkCopiedMsg:
		return m.linkCopied(msg)

	case browseFailedMsg:
		return m, m.toasts.Show(comp.ToastError, "Could not open a browser: "+msg.err.Error())

	case prview.BackMsg:
		m.screen = screenList
		// A refresh left behind never settles: every settle path drops a response
		// for a screen that is gone. Without this the bar spins over the list
		// with nothing coming.
		m.detailRefreshing = detailRefresh{}
		m.resize()
		return m, nil
	}

	return m.delegate(msg)
}

func (m Model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	// ctrl+c goes first and answers from anywhere, including out of a pane
	// taking text. One way out of the program has to be unconditional.
	if key.Matches(msg, keys.Global.ForceQuit) {
		return m, tea.Quit
	}

	// A screen writing a comment or filtering a picker owns the keyboard. q is
	// a letter in there, and the root's own bindings would each eat one.
	capturing := m.screen == screenDetail && m.detail.Capturing()

	// Below the floor the frame is a message, so a key acts on a screen nobody
	// can see: a blind enter is a merge. Only the ways out answer.
	if m.width < minWidth || m.height < minHeight {
		switch {
		case msg.String() == "esc":
			return m.delegate(msg)
		case key.Matches(msg, keys.Global.Quit) && !capturing:
			return m, tea.Quit
		}
		return m, nil
	}

	if capturing {
		return m.delegate(msg)
	}

	switch {
	case key.Matches(msg, keys.Global.Quit):
		return m, tea.Quit

	case key.Matches(msg, keys.Global.Help):
		m.showHelp = !m.showHelp
		return m, nil
	}

	// While help is up it owns the keyboard, so a movement key scrolls the
	// screen underneath instead of dismissing what is covering it.
	if m.showHelp {
		if msg.String() == "esc" {
			m.showHelp = false
		}
		return m, nil
	}

	return m.delegate(msg)
}

// delegate hands a message to the screen that has focus.
func (m Model) delegate(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch m.screen {
	case screenList:
		m.list, cmd = m.list.Update(msg)
	case screenDetail:
		wasChecks := m.detail.ShowsChecks()
		m.detail, cmd = m.detail.Update(msg)
		nowChecks := m.detail.ShowsChecks()
		if !wasChecks && nowChecks {
			var checks tea.Cmd
			m, checks = m.startChecks()
			cmd = tea.Batch(cmd, checks)
		}
	}
	return m, cmd
}

// resize divides the frame. The status bar is fixed, the notice takes a line
// when there is one, and the active screen gets the rest.
func (m *Model) resize() {
	if m.width <= 0 || m.height <= 0 {
		return
	}

	// Nothing below the floor is drawn, and relaying the conversation out costs
	// milliseconds a step on a resize drag that renders no frame at all.
	if m.width < minWidth || m.height < minHeight {
		return
	}

	body := max(0, m.height-statusBarHeight-m.noticeHeight())
	m.status = m.status.Size(m.width)
	m.help.SetWidth(m.width)

	switch m.screen {
	case screenList:
		m.list.SetSize(m.width, body)
	case screenDetail:
		m.detail.SetSize(m.width, body)
	}
}

// noticeHeight is the line the notice takes. On a frame with room for one line
// the status bar wins it: the keys that quit matter more than a config warning.
func (m Model) noticeHeight() int {
	if m.notice == "" || m.height < 2 {
		return 0
	}
	return 1
}

func (m Model) screenView() string {
	if m.screen == screenDetail {
		return m.detail.View()
	}
	return m.list.View()
}

// View renders the model. It reads state and returns a frame; it never fetches
// or mutates. In v2 the view declares its own screen mode, so alt screen is set
// here rather than as a program option.
func (m Model) View() tea.View {
	v := tea.NewView(m.render())
	v.AltScreen = true
	return v
}

func (m Model) render() string {
	if m.width <= 0 || m.height <= 0 {
		return ""
	}

	// Ahead of the help overlay and of whatever else has the keyboard. A picker
	// or a form is still open under this, and closes on the key that closes it.
	if m.width < minWidth || m.height < minHeight {
		return m.tooSmall()
	}

	parts := make([]string, 0, 3)
	if m.noticeHeight() > 0 {
		parts = append(parts, m.status.Render(m.noticeLine(), ""))
	}
	// A pane with no room for content renders nothing at all, and appending
	// that empty string would still cost the line it was denied.
	if body := m.screenView(); body != "" {
		parts = append(parts, body)
	}
	if message := m.statusMessage(); message != "" {
		parts = append(parts, m.status.RenderMessage(m.statusHints(), message))
	} else {
		parts = append(parts, m.status.Render(m.statusHints(), m.statusReadout()))
	}

	frame := strings.Join(parts, "\n")
	if !m.showHelp {
		return frame
	}
	return comp.Over(frame, comp.Modal(m.theme, "Keys", m.helpBody()), m.width, m.height)
}

// noticeLine renders the config warning in the warning color. In the chrome
// grey it reads as decoration, and a notice nobody acts on is one that failed.
func (m Model) noticeLine() string {
	return lipgloss.NewStyle().Foreground(m.theme.Warning).Render(m.notice)
}

// statusHints is the left of the bar, and it is the hints whatever else is
// happening. They used to give way for a toast; a message on the right leaves
// the keys where the reader's eye already learned to find them.
//
// A picker or a form is the exception, because it has taken the keys the line
// names and carries a hint line of its own. The bar goes quiet rather than
// spending its width on keys that stopped working when the modal opened.
//
// The detail screen builds its own line: the keymap is the same on all four
// tabs and what they can do is not.
func (m Model) statusHints() string {
	if m.screen != screenDetail {
		return m.help.ShortHelpView(m.list.Keys().ShortHelp())
	}
	if m.detail.Capturing() {
		return ""
	}
	return m.help.ShortHelpView(m.detail.ShortHelp())
}

// statusMessage is what the right side says happened, and empty when nothing
// has. A refresh on the detail screen leaves the content where it is, so the
// bar is the only place that can say it is running.
func (m Model) statusMessage() string {
	if !m.toasts.Empty() {
		return m.toasts.Render(m.theme)
	}
	if m.refreshRunning() {
		return m.refreshSpin.RenderAccent("Refreshing")
	}
	return ""
}

// refreshRunning is whether a refresh the reader asked for is still out, on
// either screen. A first load is not one: it spins over the pane it is filling.
func (m Model) refreshRunning() bool {
	return m.detailRefreshing.running() || len(m.refreshing) > 0
}

// statusReadout is the right side the rest of the time: the remaining budget
// while it is low enough to be worth reading, and otherwise whatever the screen
// in front of the reader has to say.
//
// Neither screen names itself here. The list's section is the current tab in the
// top border and the detail's pull request is in its own header, so both were
// spending the line on a fact already on the screen. Who opened the pull request
// and when is not one of those any more: the header is two lines now and does
// not carry it, which is what makes this side the place for it.
//
// The budget wins. It is a number that changes and runs out, and the readout
// under it is a fact that does not.
func (m Model) statusReadout() string {
	// Limit is zero until a response lands. Gating on it rather than on
	// Remaining is what lets an exhausted budget still read as zero.
	if rate := m.store.Rate(); rate.Limit > 0 {
		if budget := m.status.Budget(rate.Remaining); budget != "" {
			return budget
		}
	}
	if m.screen == screenDetail {
		return lipgloss.NewStyle().Foreground(m.theme.MutedOrSubtle()).
			Render(m.detail.Readout())
	}
	return ""
}

// helpBody is the overlay's content, and says so when the frame cannot hold it.
//
// A narrow frame forces the columns down until the list is taller than the room
// there is, and the overlay is drawn over the screen rather than into a pane, so
// what does not fit is simply cut off the bottom. Bindings disappear with
// nothing to say they existed. The line is not a fix, it is the difference
// between a short list and a wrong one.
func (m Model) helpBody() string {
	groups := m.list.Keys().FullHelp()
	if m.screen == screenDetail {
		groups = m.detail.Keys().FullHelp()
	}

	body := m.help.FullHelpView(refitHelp(groups, m.width-modalChrome))

	// The overlay spends a line on each border and the frame spends one on the
	// status bar.
	room := m.height - statusBarHeight - m.noticeHeight() - 2
	if strings.Count(body, "\n")+1 <= room {
		return body
	}

	note := lipgloss.NewStyle().Foreground(m.theme.Warning).
		Render("… more keys than this frame can show")
	return strings.Join(append(strings.Split(body, "\n")[:max(0, room-1)], note), "\n")
}

// modalChrome is what the overlay spends on itself: two border runes and a
// space of padding either side.
const modalChrome = 4

// helpColumns is the widest the overlay gets, whatever the frame could carry.
// Every binding laid out in one row of columns runs most of the way across a
// wide terminal, and a list that wide is read by sweeping the eye sideways.
// Taller and narrower is read by going down it once.
const helpColumns = 3

// refitHelp re-columns the bindings to whatever the frame can carry, up to
// helpColumns. The help bubble sizes its columns from their contents and never
// wraps, so a set that is one column too wide gets sheared by the overlay
// rather than reflowed, and the modal loses its right border.
func refitHelp(groups [][]key.Binding, width int) [][]key.Binding {
	var flat []key.Binding
	widestKey, widestDesc := 0, 0
	for _, group := range groups {
		for _, b := range group {
			flat = append(flat, b)
			widestKey = max(widestKey, len(b.Help().Key))
			widestDesc = max(widestDesc, len(b.Help().Desc))
		}
	}
	if len(flat) == 0 {
		return groups
	}

	// A column is as wide as its widest key plus its widest description, and
	// the two sit on different rows as often as not. Measuring the widest
	// single binding instead reads a column narrower than it renders, and the
	// overlay then runs past the frame and loses its own right border.
	//
	// The help bubble puts a gap between columns; budget for it so the last
	// column is not the one that overflows.
	const columnGap = 4
	columns := max(1, min(helpColumns, width/(widestKey+widestDesc+1+columnGap)))
	if columns >= len(groups) {
		return groups
	}

	rows := (len(flat) + columns - 1) / columns
	out := make([][]key.Binding, 0, columns)
	for i := 0; i < len(flat); i += rows {
		out = append(out, flat[i:min(i+rows, len(flat))])
	}
	return out
}

// helpStyles dresses the help bubble in the active theme. Its own defaults are
// fixed greys that ignore whatever palette is loaded.
func helpStyles(th theme.Theme) help.Styles {
	key := lipgloss.NewStyle().Foreground(th.Accent)
	desc := lipgloss.NewStyle().Foreground(th.MutedOrSubtle())
	sep := lipgloss.NewStyle().Foreground(th.BorderMutedOrSubtle())

	return help.Styles{
		Ellipsis:       sep,
		ShortKey:       key,
		ShortDesc:      desc,
		ShortSeparator: sep,
		FullKey:        key,
		FullDesc:       desc,
		FullSeparator:  sep,
	}
}

// refreshSummary names what came back. It counts sections rather than rows, and
// only the ones this refresh is waiting on: started or adopted, never neither.
func refreshSummary(sections []store.Section, started []int) (comp.ToastKind, string) {
	failed := 0
	for _, i := range started {
		if sections[i].Status == store.StatusFailed {
			failed++
		}
	}

	switch {
	case failed == 0:
		return comp.ToastSuccess, "Refreshed " + comp.Plural(len(started), "section")
	case failed == len(started):
		return comp.ToastError, "Refresh failed"
	default:
		return comp.ToastError, "Refreshed " + comp.Plural(len(started)-failed, "section") +
			", " + strconv.Itoa(failed) + " failed"
	}
}
