package prview

import (
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/praxis-labs-io/zen-octo/internal/gh"
	"github.com/praxis-labs-io/zen-octo/internal/store"
	"github.com/praxis-labs-io/zen-octo/internal/tui/comp"
	"github.com/praxis-labs-io/zen-octo/internal/tui/paint"
)

const (
	checkParentPrefix = "workflow\x00"
	checkJobPrefix    = "job\x00"
)

// RerunCheckMsg asks the root to rerun the selected failed Actions job. The
// concrete job id makes the write precise; the logical key remains selected
// when GitHub replaces it with the new attempt.
type RerunCheckMsg struct {
	Repo  string
	JobID int64
	Name  string
}

// checkGroup is one workflow and the jobs that ran under it. A group with no
// workflow is the status contexts posted directly against the commit.
type checkGroup struct {
	name   string
	checks []gh.Check
	state  gh.CheckState
}

// checkTreeRow is one visible line in the Checks column. A parent folds a
// multi-job workflow; every other row is the same logical check the details
// rail lists.
const jobSettleDelay = 150 * time.Millisecond

type rerunPending struct {
	jobID       int64
	startedAt   time.Time
	completedAt time.Time
	acceptedAt  time.Time
}

type checkTreeRow struct {
	key      string
	label    string
	checkKey string
	state    gh.CheckState
	count    int
	depth    int
	parent   bool
	folded   bool
}

// checks owns the stable logical selection and the concrete attempt loaded for
// it. selected survives a rerun because Check.Key does; wanted and job use the
// new JobID so an earlier attempt's log can never appear under it.
type checks struct {
	cursor   int
	groups   []checkGroup
	rows     []checkTreeRow
	selected string
	folded   map[string]bool

	wanted          int64
	refreshJob      bool
	job             store.Job
	jobStale        bool
	parsing         bool
	sections        []jobSection
	rendered        []string
	renderedBody    string
	renderWidth     int
	renderQuery     string
	rendering       bool
	renderToken     uint64
	renderWantWidth int
	renderWantQuery string

	step       int
	line       int
	stepLines  int
	stepStarts []int
	stepOpen   map[int]bool
	stepSeen   map[int]bool

	searching    bool
	search       comp.Search
	matchLines   []int
	searchStep   int
	searchWithin int

	reruns map[string]rerunPending
}

// groupChecks keeps workflow order from the rollup. Status contexts are flat
// leaves in the tree, but collecting them here and moving them to the end keeps
// the ordering rule in one place.
func groupChecks(r gh.CheckRollup) []checkGroup {
	at := make(map[string]int, len(r.Checks))
	var out []checkGroup
	for _, c := range r.Checks {
		i, ok := at[c.Workflow]
		if !ok {
			i = len(out)
			at[c.Workflow] = i
			out = append(out, checkGroup{name: c.Workflow})
		}
		out[i].checks = append(out[i].checks, c)
	}
	if i, ok := at[""]; ok && i != len(out)-1 {
		g := out[i]
		out = append(append(out[:i:i], out[i+1:]...), g)
	}
	for i := range out {
		out[i].state = worst(out[i].checks)
	}
	return out
}

func worst(list []gh.Check) gh.CheckState {
	out := gh.CheckStateNone
	for _, c := range list {
		if rank(c.State) > rank(out) {
			out = c.State
		}
	}
	return out
}

func rank(s gh.CheckState) int {
	switch s {
	case gh.CheckStateFailure, gh.CheckStateError:
		return 4
	case gh.CheckStatePending, gh.CheckStateExpected:
		return 3
	case gh.CheckStateSuccess:
		return 2
	case gh.CheckStateSkipped:
		return 1
	}
	return 0
}

func checkParentKey(workflow string) string { return checkParentPrefix + workflow }
func checkRowKey(c gh.Check) string         { return checkJobPrefix + c.Key() }

// flattenChecks makes single-job workflows one row and gives only multi-job
// workflows a parent. Status contexts are never a synthetic workflow.
func flattenChecks(groups []checkGroup, folded map[string]bool) []checkTreeRow {
	var out []checkTreeRow
	for _, g := range groups {
		switch {
		case g.name == "":
			for _, c := range g.checks {
				out = append(out, checkTreeRow{
					key: checkRowKey(c), label: c.Name, checkKey: c.Key(), state: c.State,
				})
			}
		case len(g.checks) == 1:
			c := g.checks[0]
			out = append(out, checkTreeRow{
				key: checkRowKey(c), label: g.name + " / " + c.Name, checkKey: c.Key(), state: c.State,
			})
		default:
			key := checkParentKey(g.name)
			closed := folded[key]
			out = append(out, checkTreeRow{
				key: key, label: g.name, state: g.state, count: len(g.checks), parent: true, folded: closed,
			})
			if closed {
				continue
			}
			for _, c := range g.checks {
				out = append(out, checkTreeRow{
					key: checkRowKey(c), label: c.Name, checkKey: c.Key(), state: c.State, depth: 1,
				})
			}
		}
	}
	return out
}

// syncChecks rebuilds the visible tree after a detail or fold changes. Both
// cursor and selected job are restored by stable keys, never by indexes that a
// poll may have moved.
func (m *Model) syncChecks() {
	var cursorKey string
	if m.check.cursor < len(m.check.rows) {
		cursorKey = m.check.rows[m.check.cursor].key
	}
	if m.check.folded == nil {
		m.check.folded = make(map[string]bool)
	}
	if m.check.reruns == nil {
		m.check.reruns = make(map[string]rerunPending)
	}

	m.check.groups = groupChecks(m.detail.Detail.Rollup)
	m.check.rows = flattenChecks(m.check.groups, m.check.folded)

	for logical, pending := range m.check.reruns {
		replacement, any := m.rerunReplacement(logical, pending)
		if !any {
			delete(m.check.reruns, logical)
			continue
		}
		if replacement == nil {
			continue
		}
		delete(m.check.reruns, logical)
		if m.check.selected == logical || strings.HasPrefix(m.check.selected, logical+"\x00") {
			m.check.selected = replacement.Key()
		}
	}

	if m.checkForKey(m.check.selected) == nil {
		m.check.selected = ""
		for _, r := range m.check.rows {
			if r.checkKey != "" {
				m.check.selected = r.checkKey
				break
			}
		}
	}

	m.check.cursor = 0
	found := false
	if cursorKey != "" {
		for i, r := range m.check.rows {
			if r.key == cursorKey {
				m.check.cursor, found = i, true
				break
			}
		}
	}
	if !found {
		for i, r := range m.check.rows {
			if r.checkKey == m.check.selected {
				m.check.cursor = i
				break
			}
		}
	}
	if c := m.selectedCheck(); c == nil {
		m.resetCheckJob()
	} else {
		stateChanged := m.check.job.Loaded && m.check.job.Job.ID == c.JobID && m.check.job.Job.State != c.State
		changed := c.JobID != m.check.wanted || stateChanged
		_, rerunning := m.check.reruns[c.LogicalKey()]
		if changed && !rerunning {
			m.resetCheckJob()
			m.check.refreshJob = stateChanged
		}
	}
	showRow(&m.sideView, m.check.cursor)
}

func (m *Model) checkForKey(key string) *gh.Check {
	for i := range m.detail.Detail.Rollup.Checks {
		if m.detail.Detail.Rollup.Checks[i].Key() == key {
			return &m.detail.Detail.Rollup.Checks[i]
		}
	}
	return nil
}

func (m *Model) selectedCheck() *gh.Check { return m.checkForKey(m.check.selected) }

func (m Model) rerunReplacement(logical string, pending rerunPending) (*gh.Check, bool) {
	var newest *gh.Check
	for i := range m.detail.Detail.Rollup.Checks {
		check := &m.detail.Detail.Rollup.Checks[i]
		if check.LogicalKey() != logical {
			continue
		}
		if check.JobID == pending.jobID {
			continue
		}
		if newest == nil || attemptTime(*check).After(attemptTime(*newest)) {
			newest = check
		}
	}
	if newest == nil {
		return nil, m.checkForLogical(logical) != nil
	}
	if newest.State == gh.CheckStatePending || newest.State == gh.CheckStateExpected {
		return newest, true
	}
	if !pending.acceptedAt.IsZero() && !newest.StartedAt.IsZero() {
		if newest.StartedAt.Before(pending.acceptedAt.Add(-5 * time.Second)) {
			return nil, true
		}
		return newest, true
	}
	oldAt, newAt := pending.completedAt, attemptTime(*newest)
	if oldAt.IsZero() {
		oldAt = pending.startedAt
	}
	// statusCheckRollup reports the current attempts. Where either side lacks a
	// timestamp, a changed concrete job id is the only ordering evidence GitHub
	// exposes and must not leave the optimistic write locked forever.
	if oldAt.IsZero() || newAt.IsZero() || newAt.After(oldAt) {
		return newest, true
	}
	return nil, true
}

func attemptTime(check gh.Check) time.Time {
	if !check.CompletedAt.IsZero() {
		return check.CompletedAt
	}
	return check.StartedAt
}

func (m *Model) checkForLogical(logical string) *gh.Check {
	for i := range m.detail.Detail.Rollup.Checks {
		if m.detail.Detail.Rollup.Checks[i].LogicalKey() == logical {
			return &m.detail.Detail.Rollup.Checks[i]
		}
	}
	return nil
}

func (m Model) checkRerunning(key string) bool {
	check := m.checkForKey(key)
	if check == nil {
		return false
	}
	_, ok := m.check.reruns[check.LogicalKey()]
	return ok
}

func (m Model) canRerunCheck() bool {
	if m.tab != tabChecks || m.checkRerunning(m.check.selected) {
		return false
	}
	check := m.selectedCheck()
	return check != nil && check.JobID != 0 &&
		(check.State == gh.CheckStateFailure || check.State == gh.CheckStateError)
}

func (m *Model) rerunCheck() tea.Cmd {
	if !m.canRerunCheck() {
		return nil
	}
	check := *m.selectedCheck()
	if m.check.reruns == nil {
		m.check.reruns = make(map[string]rerunPending)
	}
	m.check.reruns[check.LogicalKey()] = rerunPending{
		jobID: check.JobID, startedAt: check.StartedAt, completedAt: check.CompletedAt,
	}
	m.syncContent()
	name := cleanJobLabel(check.Name)
	if check.Workflow != "" {
		name = cleanJobLabel(check.Workflow) + " / " + name
	}
	return func() tea.Msg {
		return RerunCheckMsg{Repo: m.pr.Repository, JobID: check.JobID, Name: name}
	}
}

// RerunSettled releases a refused write wherever the reader has navigated.
// Accepted writes stay marked until polling publishes their replacement.
func (m *Model) RerunAccepted(jobID int64, acceptedAt time.Time) {
	for key, pending := range m.check.reruns {
		if pending.jobID == jobID {
			pending.acceptedAt = acceptedAt
			m.check.reruns[key] = pending
		}
	}
}

func (m *Model) RerunSettled(jobID int64) {
	for key, pending := range m.check.reruns {
		if pending.jobID == jobID {
			delete(m.check.reruns, key)
		}
	}
	m.syncContent()
}

// moveCheck walks visible tree rows. A parent leaves the selected job in the
// pane, the same way a directory row leaves the shown file alone.
func (m *Model) moveCheck(delta int) {
	if len(m.check.rows) == 0 {
		return
	}
	m.check.cursor = min(max(m.check.cursor+delta, 0), len(m.check.rows)-1)
	droppedHeader := false
	if key := m.check.rows[m.check.cursor].checkKey; key != "" && key != m.check.selected {
		droppedHeader = m.check.searching || !m.check.search.Empty()
		m.check.selected = key
		m.resetCheckJob()
		m.view.SetYOffset(0)
	}
	showRow(&m.sideView, m.check.cursor)
	if droppedHeader {
		m.layout()
	} else {
		m.syncContent()
	}
}

func (m *Model) resetCheckJob() {
	m.check.wanted = 0
	m.check.refreshJob = false
	m.check.job = store.Job{}
	m.check.jobStale = false
	m.check.parsing = false
	m.check.sections = nil
	m.invalidateJobRender()
	m.check.step = 0
	m.check.line = 0
	m.check.stepLines = 0
	m.check.stepStarts = nil
	m.check.stepOpen = nil
	m.check.stepSeen = nil
	m.check.searching = false
	m.check.search = comp.Search{}
	m.check.matchLines = nil
	m.check.searchStep = 0
	m.check.searchWithin = 0
}

func (m *Model) toggleCheckFold() {
	if m.check.cursor >= len(m.check.rows) || !m.check.rows[m.check.cursor].parent {
		return
	}
	key := m.check.rows[m.check.cursor].key
	m.check.folded[key] = !m.check.folded[key]
	m.syncChecks()
	m.syncContent()
}

func (m Model) checkFoldable() bool {
	return m.tab == tabChecks && m.focus == paneSide && m.check.cursor < len(m.check.rows) &&
		m.check.rows[m.check.cursor].parent
}

// armJob starts a short wait for the concrete attempt selected now. A status
// context has no Actions job and deliberately asks for nothing.
func (m Model) armJob() tea.Cmd {
	if m.tab != tabChecks {
		return nil
	}
	c := m.selectedCheck()
	if c == nil || c.JobID == 0 || c.JobID == m.check.wanted {
		return nil
	}
	msg := JobSettleMsg{Key: c.Key(), JobID: c.JobID, Refresh: m.check.refreshJob}
	return tea.Tick(jobSettleDelay, func(time.Time) tea.Msg { return msg })
}

// settleJob spends only the wait that still names the selected job. Holding a
// movement key can arm one timer per row without fetching any row passed over.
func (m *Model) settleJob(msg JobSettleMsg) tea.Cmd {
	c := m.selectedCheck()
	if m.tab != tabChecks || c == nil || c.Key() != msg.Key || c.JobID != msg.JobID ||
		c.JobID == m.check.wanted {
		return nil
	}
	m.check.wanted = c.JobID
	refresh := m.check.refreshJob || msg.Refresh
	m.check.refreshJob = false
	return func() tea.Msg { return NeedJobMsg{JobID: c.JobID, Refresh: refresh} }
}

// PollJob refreshes volatile step metadata and retries a selected job whose
// last answer failed or contradicted the terminal rollup. Logs remain deferred
// until the metadata says the job is complete.
func (m *Model) PollJob() tea.Cmd {
	check := m.selectedCheck()
	if m.tab != tabChecks || check == nil || check.JobID == 0 {
		return nil
	}
	pending := check.State == gh.CheckStatePending || check.State == gh.CheckStateExpected
	if !pending && m.check.job.Status != store.StatusFailed && !m.check.jobStale {
		return nil
	}
	return m.refreshJob(check.JobID)
}

// RefreshJob is the explicit-sync form: the reader asked for the selected job
// regardless of its current state.
func (m *Model) RefreshJob() tea.Cmd {
	check := m.selectedCheck()
	if m.tab != tabChecks || check == nil || check.JobID == 0 {
		return nil
	}
	return m.refreshJob(check.JobID)
}

func (m *Model) refreshJob(id int64) tea.Cmd {
	m.check.wanted = id
	m.check.refreshJob = false
	return func() tea.Msg { return NeedJobMsg{JobID: id, Refresh: true} }
}

type jobParsedMsg struct {
	id       int64
	sections []jobSection
}

// SetJobAsync keeps maximum-size log parsing off Bubble Tea's update loop.
// Running jobs and fetch failures remain cheap enough to settle immediately.
func (m *Model) SetJobAsync(id int64, job store.Job) tea.Cmd {
	if !job.Loaded || job.Status == store.StatusFailed ||
		job.Job.State == gh.CheckStatePending || job.Job.State == gh.CheckStateExpected {
		return m.SetJob(id, job)
	}
	c := m.selectedCheck()
	if c == nil || c.JobID != id {
		return nil
	}
	m.takeJob(id, job)
	m.check.jobStale = false
	m.check.parsing = true
	m.check.sections = nil
	m.check.stepLines = 0
	m.check.stepStarts = nil
	m.syncContent()
	jobValue, log := job.Job, job.Log
	return func() tea.Msg {
		return jobParsedMsg{id: id, sections: splitJobLog(jobValue, log)}
	}
}

func (m *Model) jobParsed(msg jobParsedMsg) tea.Cmd {
	c := m.selectedCheck()
	if c == nil || c.JobID != msg.id || m.check.wanted != msg.id {
		return nil
	}
	m.check.parsing = false
	m.check.sections = msg.sections
	m.invalidateJobRender()
	m.syncContent()
	return m.armJobRender()
}

// SetJob is the synchronous seam used by focused view tests. Production uses
// SetJobAsync so parsing cannot block the application update loop.
func (m *Model) SetJob(id int64, job store.Job) tea.Cmd {
	c := m.selectedCheck()
	if c == nil || c.JobID != id {
		return nil
	}
	jobPending := job.Job.State == gh.CheckStatePending || job.Job.State == gh.CheckStateExpected
	checkTerminal := c.State != gh.CheckStatePending && c.State != gh.CheckStateExpected
	if job.Loaded && jobPending && checkTerminal {
		// A pulse can overtake the REST request. Do not let that stale pending
		// response overwrite the terminal rollup or suppress the now-ready log.
		m.check.wanted = id
		m.check.jobStale = true
		return nil
	}
	m.takeJob(id, job)
	m.check.parsing = false
	if job.Status != store.StatusFailed {
		m.check.jobStale = false
	}
	if job.Loaded {
		m.check.sections = splitJobLog(job.Job, job.Log)
		if width := m.bodyWidth(); width > 0 {
			m.renderJobSteps(width)
		}
	} else {
		m.check.sections = nil
	}
	m.syncContent()
	if job.Status == store.StatusFailed {
		return nil
	}
	return m.Init()
}

func (m *Model) takeJob(id int64, job store.Job) {
	if m.check.job.Job.ID != id && job.Loaded {
		m.check.step = 0
		m.check.line = 0
		m.check.stepLines = 0
		m.check.stepOpen = make(map[int]bool)
		m.check.stepSeen = make(map[int]bool)
	}
	m.check.wanted = id
	m.check.job = job
	m.invalidateJobRender()
}

func (m *Model) staleJobRender() {
	m.check.renderWidth = 0
	m.check.renderQuery = ""
	m.check.rendering = false
	m.check.renderToken++
}

func (m *Model) invalidateJobRender() {
	m.check.rendered = nil
	m.check.renderedBody = ""
	m.check.renderWidth = 0
	m.check.renderQuery = ""
	m.check.rendering = false
	m.check.renderToken++
}

func (m Model) checkColumn(width int) string {
	if !m.detail.Loaded {
		return ""
	}
	if len(m.check.rows) == 0 {
		return m.faint().Render("No checks.")
	}
	lines := make([]string, len(m.check.rows))
	for i, r := range m.check.rows {
		if m.checkRerunning(r.checkKey) {
			r.state = gh.CheckStatePending
		}
		lines[i] = m.checkTreeLine(r, width, i == m.check.cursor)
	}
	return strings.Join(lines, "\n")
}

func (m Model) checkTreeLine(r checkTreeRow, width int, selected bool) string {
	base := lipgloss.NewStyle()
	if selected {
		base = base.Background(m.theme.SelectedBackground)
	}
	_, c := comp.CheckStateIcon(m.theme, r.state)
	fold := ""
	if r.parent {
		fold = "▾ "
		if r.folded {
			fold = "▸ "
		}
	}
	indent := strings.Repeat("  ", r.depth)
	lead := base.Render(indent+fold) + base.Foreground(c).Render(glyphCheck) + base.Render(" ") +
		base.Foreground(m.theme.Text).Render(cleanJobLabel(r.label))
	right := ""
	if r.parent {
		right = base.Foreground(m.theme.Subtle).Render(strconv.Itoa(r.count))
	}
	return m.padTo(m.checkLine(lead, right, width, base), width, base)
}

func (m Model) checkLine(lead, right string, width int, base lipgloss.Style) string {
	room := max(0, width-lipgloss.Width(right)-1)
	if lipgloss.Width(lead) > room {
		lead = paint.Clip(lead, room, base.Foreground(m.theme.Subtle))
	}
	gap := max(1, width-lipgloss.Width(lead)-lipgloss.Width(right))
	return lead + base.Render(strings.Repeat(" ", gap)) + right
}

// checkBody is the selected job, not another rollup of everything in the
// column. On narrow frames the selected job remains named by its summary even
// though the column is hidden; switching jobs there follows in a later slice.
func (m *Model) checkBody() string {
	if !m.detail.Loaded {
		if m.detail.Status == store.StatusFailed {
			return m.faint().Render("Could not load the checks: " + m.detail.Err.Error())
		}
		return m.spinner.Render("Loading the checks")
	}
	c := m.selectedCheck()
	if c == nil {
		return m.faint().Render("No checks have reported.")
	}
	return m.jobBody(*c, m.bodyWidth())
}
