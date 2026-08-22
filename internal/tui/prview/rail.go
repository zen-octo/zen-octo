package prview

import (
	"image/color"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/praxis-labs-io/zen-octo/internal/gh"
	"github.com/praxis-labs-io/zen-octo/internal/tui/comp"
	"github.com/praxis-labs-io/zen-octo/internal/tui/paint"
)

// glyphFile marks the changed-file count. It comes from the codicons range, the
// same set the draft and closed markers come from.
const glyphFile = "" // nf-cod-file

// glyphCheck marks every row in the Checks section. Color carries which state
// it is in: a column of one shape reads down faster than a column of four.
const glyphCheck = "●"

// markLead is the state dot and the space after it. Only the rows carrying one
// spend it, which is why the room a name gets is not a constant.
const markLead = 2

// railBody renders the details column: what is happening to the pull request,
// then who is on it, then what it touches.
//
// Every section renders, empty or not. No reviewer and no label are both facts
// worth reading, and a section that disappears when it has nothing behind it
// reads as one that was never fetched.
//
// The branch is not here. It is the second line of the header, and the rail has
// no room to spend saying it twice.
//
// It records the ring as it goes. The four sections a later milestone acts on
// are walkable; the rest state a fact and there is nothing to do to them.
//
// Rows are handed over already the full width, gutter and all, the same way the
// file column hands over its own. The selected row is a background, and one
// stops at the last word unless the row runs to the border.
func (m *Model) railBody(width int) string {
	d := m.railDetail()
	pr := d.PullRequest

	m.railRing.reset()

	at := 0
	var blocks []string

	// section lays a heading over its rows and records the ones there is
	// something to do to. Rows carry their own key rather than taking one from
	// the section, because a section can hold two kinds: the reviewers on it
	// and the row that adds another.
	section := func(title string, rows []railEntry) {
		lines := make([]string, len(rows))
		for i, r := range rows {
			if r.key.kind != focusNone {
				m.railRing.add(r.key, at+1+i, 1)
			}
			lines[i] = r.line
		}
		head := m.faint().Render(strings.Repeat(" ", railGutter) + title)
		blocks = append(blocks, head+"\n"+strings.Join(lines, "\n"))
		at += len(rows) + 2
	}

	section("State", m.stateRow(d, width))
	section("Author", m.authorRow(pr.Author, width))
	section("Reviewers", m.reviewerRows(d.Reviewers, width))
	section("Assignees", m.actorRows(d, width))
	section("Labels", m.labelRows(d.Labels, width))
	section("Changes", m.changeRow(pr, width))
	// Checks runs to any length, so it goes below everything of a fixed size.
	// The two rows under it are what you read just before merging, which is the
	// other reason they sit at the bottom.
	section("Checks", m.checkRows(d.Rollup, width))
	section("Base", m.baseRow(d, width))
	section("Merge", m.mergeRow(d, width))

	return strings.Join(blocks, "\n\n")
}

// railEntry is one rendered row and what the ring calls it. A key of no kind is
// a row tab walks past.
type railEntry struct {
	line string
	key  focusKey
}

// railRow is the style every cell in a row is rendered against. The selected
// row carries a background, and it has to be set on each cell: every styled run
// ends in a reset that clears the background with it, so a joined row wrapped in
// the background afterwards paints only its first cell.
//
// Only the pane the keys are going to paints its selection, the same rule the
// conversation's cards hold to. Both lit at once says the keys go to both.
//
// It answers with whether the row is the lit one as well, because the bar in
// the gutter reads the same fact and two tests of it could disagree about which
// row is marked.
func (m Model) railRow(selected bool) (lipgloss.Style, bool) {
	if selected && m.focus == paneRail {
		return lipgloss.NewStyle().Background(m.theme.SelectedBackground), true
	}
	return lipgloss.NewStyle(), false
}

// railLine is one row: its gutter, its content, and the padding that runs the
// cursor line out to the border rather than stopping at the last word.
//
// The gutter is the leading cell the diff puts its cursor bar in, and it is the
// same bar here. The glyph is one cell and so is the first of railGutter's, so
// a row gains nothing by being the one under the cursor and the rows below it
// do not step. What is left of the gutter holds the content off the bar.
//
// The fill stays under it, which is a second mark the cards were refused. This
// rail earns it where they do not: its ring walks controls and steps over the
// headings between them, so the cursor lands on rows that are not neighbours and
// a reader tracking it is looking for where it went rather than watching it
// move. The file tree, the commit list and the workflow column are flat, every
// row a stop, so a fill alone says everything there and none of them takes a
// bar.
func (m Model) railLine(base lipgloss.Style, lit bool, content string, width int) string {
	var bar color.Color
	if lit {
		bar = m.theme.Accent
	}
	gutter := base.Render(strings.Repeat(" ", max(0, railGutter-1)))
	return m.padTo(paint.Lead(bar, base)+gutter+content, width, base)
}

// railFact is a one-row section stating something about the pull request. There
// is nothing to do to it, so the ring walks past.
func (m Model) railFact(text string, c color.Color, width int) []railEntry {
	base, lit := m.railRow(false)
	return []railEntry{{line: m.railLine(base, lit, base.Foreground(c).Render(text), width)}}
}

// railControl is a one-row section that is also something to act on. State is
// one: draft and ready, closed and reopened are all changes to what it says.
// Merge is the other.
func (m Model) railControl(kind focusKind, text string, c color.Color, width int) []railEntry {
	key := focusKey{kind: kind}
	base, lit := m.railRow(m.railRing.focused(key))
	return []railEntry{{line: m.railLine(base, lit, base.Foreground(c).Render(text), width), key: key}}
}

// stateRow is where the pull request sits, and a stop on the ring only while
// there is somewhere to move it to. A merged one is the end of the line, and a
// reader with no write access to the repository can move none of them; both
// leave the row stating a fact, the way an empty Checks section does.
//
// Only once the detail has landed, and never while a lifecycle write is still
// out. Before the detail arrives nothing is known about what the viewer may do,
// which is not the same as nothing being allowed. During a write the two halves
// disagree on purpose: the store moves the state and never the permissions, so
// a close that has just been applied optimistically still carries the
// CanReopen GitHub gave for an open pull request, and believing it would take
// the ring out from under the reader standing on this very row.
//
// Either way the key stays and enter is inert, on openRailPicker's own guard
// and on startPicker refusing to open a menu with nothing in it.
func (m Model) stateRow(d gh.PullRequestDetail, width int) []railEntry {
	icon, _ := comp.PRStateIcon(m.theme, d.PullRequest)
	label, c := comp.PRStateLabel(m.theme, d.PullRequest)
	text := icon + " " + label

	if m.detail.Loaded && !m.detail.StateWriting && len(stateChoices(d)) == 0 {
		return m.railFact(text, c, width)
	}
	return m.railControl(focusState, text, c, width)
}

// addRow opens the picker for a section. It sits under whatever is already
// there and stands in for the empty note, because a section with none of
// something and no way to add one has nothing to say.
func (m Model) addRow(kind focusKind, label string, width int) railEntry {
	key := focusKey{kind: kind}
	base, lit := m.railRow(m.railRing.focused(key))
	return railEntry{
		line: m.railLine(base, lit, base.Foreground(m.theme.Subtle).Render("+ "+label), width),
		key:  key,
	}
}

// changeRow is the churn and the file count on one row, the count marked with a
// glyph rather than the word: the rail has thirty-odd columns and "files" earns
// none of them.
func (m Model) changeRow(pr gh.PullRequest, width int) []railEntry {
	base, lit := m.railRow(false)

	churn := base.Foreground(m.theme.Success).Render("+"+strconv.Itoa(pr.Additions)) +
		base.Render(" ") + base.Foreground(m.theme.Error).Render("−"+strconv.Itoa(pr.Deletions))
	files := base.Foreground(m.theme.Subtle).
		Render("  " + strconv.Itoa(pr.ChangedFiles) + " " + glyphFile)

	return []railEntry{{line: m.railLine(base, lit, churn+files, width)}}
}

// checkRows is every check on the head commit, each marked with its own state.
// They all take the same dot: a column of four different shapes is harder to
// read down than a column of one.
func (m Model) checkRows(r gh.CheckRollup, width int) []railEntry {
	// The one section with no add row. A check is something a workflow runs,
	// not something to put on the pull request, so the note stands alone and
	// the ring walks past it: there is nothing to do to a check that is not
	// there. It carries no key for the same reason it needs none. Sharing the
	// first check's would leave focus parked here to light up whichever check
	// arrived in its place.
	if len(r.Checks) == 0 {
		base, lit := m.railRow(false)
		return []railEntry{{
			line: m.railLine(base, lit, base.Foreground(m.theme.Subtle).Render("None yet"), width),
		}}
	}

	out := make([]railEntry, 0, len(r.Checks))
	for _, check := range r.Checks {
		key := focusKey{kind: focusCheck, id: check.Key()}
		base, lit := m.railRow(m.railRing.focused(key))
		_, c := comp.CheckStateIcon(m.theme, check.State)

		faint := base.Foreground(m.theme.Subtle)
		out = append(out, railEntry{
			line: m.railLine(base, lit, base.Foreground(c).Render(glyphCheck)+faint.Render(" ")+
				m.fit(faint, checkName(check), railNameRoom(width, markLead)), width),
			key: key,
		})
	}
	return out
}

// checkName is what GitHub calls the check on its own page. The job name alone
// is not enough: five suites in a repository each have a job called "test", and
// five identical rows say nothing about which one broke.
func checkName(c gh.Check) string {
	if c.Workflow == "" {
		return c.Name
	}
	return c.Workflow + " / " + c.Name
}

// reviewerRows marks each reviewer with where they stand. The rail has no room
// for the words, and the same dot the checks use carries it in one cell.
func (m Model) reviewerRows(reviewers []gh.Reviewer, width int) []railEntry {
	out := make([]railEntry, 0, len(reviewers)+1)
	for _, r := range reviewers {
		key := focusKey{kind: focusReviewer, id: r.Actor.Login}
		base, lit := m.railRow(m.railRing.focused(key))
		out = append(out, railEntry{
			line: m.railLine(base, lit,
				base.Foreground(comp.ReviewerColor(m.theme, r)).Render(glyphCheck)+
					base.Render(" ")+
					m.fit(base.Foreground(m.theme.Actor), comp.Handle(r.Actor.Login), railNameRoom(width, markLead)), width),
			key: key,
		})
	}
	return append(out, m.addRow(focusAddReviewer, "Add reviewer", width))
}

// railNameRoom is what a row leaves a name: its gutter, whatever leads the name
// on that row, and one column short of the border on the right. A row with no
// state dot has those two cells to spend on the name.
func railNameRoom(width, lead int) int {
	return max(1, width-railGutter-lead-1)
}

// fit renders a name and clips it to the room the row actually has. A bot login
// runs past the rail as readily as a workflow name does, and the rail is a
// column: wrapping turns one row into two that read as two.
//
// It renders before it clips, the way the file column does. Clipping the plain
// text and styling the result puts the ellipsis inside the run, and the cursor
// line stops at it. The mark keeps the row's own style so the background
// carries through it.
func (m Model) fit(style lipgloss.Style, name string, room int) string {
	return clipTo(style.Render(name), room, style.Foreground(m.theme.Subtle))
}

// actorRows names who the pull request is assigned to, and is a stop on the ring
// only while the reader may change that. GitHub answers viewerCanAssign for
// exactly the accounts addAssignee accepts, and a row offering a write it will
// refuse is worse than a row stating a fact.
//
// Only once the detail has landed, the same guard stateRow keeps. Before it
// arrives nothing is known about what the viewer may do, which is not the same
// as nothing being allowed, and dropping the keys early would move every stop
// under them the moment the answer came.
//
// The add row goes with the keys rather than staying as a dead "+ Add assignee".
// It is the one row in the section that is nothing but an offer.
func (m Model) actorRows(d gh.PullRequestDetail, width int) []railEntry {
	actors := d.Assignees

	// Both flags, because the write needs both. Assigning is CanAssign's to
	// permit, but the mutation behind it is updatePullRequest, which GitHub
	// governs with CanUpdate: a triage collaborator is answered true for the
	// first and false for the second, and would be offered a control that can
	// only come back refused.
	assignable := !m.detail.Loaded || (d.Viewer.CanAssign && d.Viewer.CanUpdate)

	out := make([]railEntry, 0, len(actors)+1)
	for _, a := range actors {
		var key focusKey
		if assignable {
			key = focusKey{kind: focusAssignee, id: a.Login}
		}
		base, lit := m.railRow(m.railRing.focused(key))
		out = append(out, railEntry{
			line: m.railLine(base, lit,
				m.fit(base.Foreground(m.theme.Actor), comp.Handle(a.Login), railNameRoom(width, 0)), width),
			key: key,
		})
	}

	if !assignable {
		// A section with nobody in it and no way to add one has nothing to say,
		// so it says that rather than leaving a blank where rows go.
		if len(out) == 0 {
			return m.railFact("None", m.theme.Subtle, width)
		}
		return out
	}
	return append(out, m.addRow(focusAddAssignee, "Add assignee", width))
}

// labelRows names each label in the theme's own accent. GitHub gives every
// label a color, and rendering that is the one thing this rail must not do: the
// hex is fixed against a white browser page, so a pale label disappears into a
// dark terminal and no theme can reach it. A terminal that only speaks ANSI
// cannot show it at all.
//
// Accent rather than Actor, which the handles above already hold, and rather
// than Subtle, which the add row under them holds. The section has to keep
// those three apart.
//
// It is a foreground, not a filled chip. A chip buys nothing a colored word does
// not already say, and the row already spends its background on the cursor.
func (m Model) labelRows(labels []gh.Label, width int) []railEntry {
	out := make([]railEntry, 0, len(labels)+1)
	for _, l := range labels {
		key := focusKey{kind: focusLabel, id: l.Name}
		base, lit := m.railRow(m.railRing.focused(key))
		out = append(out, railEntry{
			line: m.railLine(base, lit,
				m.fit(base.Foreground(m.theme.Accent), l.Name, railNameRoom(width, 0)), width),
			key: key,
		})
	}
	return append(out, m.addRow(focusAddLabel, "Add label", width))
}

// authorRow names who raised it. The login is empty once the account is
// deleted, which is a fact rather than a section to drop.
func (m Model) authorRow(a gh.Actor, width int) []railEntry {
	if a.Login == "" {
		return m.railFact("Unknown", m.theme.Subtle, width)
	}
	return m.railFact(comp.Handle(a.Login), m.theme.Actor, width)
}

// baseRow is what the pull request merges into and how far it has fallen behind
// it. GitHub says "out-of-date"; the number is the same answer with the size of
// the problem attached.
//
// A stop on the ring only while there is somewhere to move it to. A merged pull
// request cannot be retargeted and GitHub refuses the write, so the row states a
// fact there the way an empty Checks section does. viewerCanUpdate stays true on
// a merged one, because its title and body are still editable, which is why the
// state is checked rather than the flag alone.
//
// Only once the detail has landed. Before that nothing is known about what the
// viewer may do, which is not the same as nothing being allowed, and dropping
// the key early moves every rail stop by one the moment the answer arrives.
//
// It keeps the State row's write guard, though a retarget does not need one:
// that fold moves the branch and the count and touches neither the permissions
// nor the lifecycle read here. A merge does. It sets the state to merged before
// GitHub has answered, and without the guard this row turns into a fact in the
// same frame the Merge row does, so a reader standing on either loses their
// place on the ring to a write they have only just started.
func (m Model) baseRow(d gh.PullRequestDetail, width int) []railEntry {
	base := d.BaseRefName

	text, c := "Up to date with "+base, m.theme.Success
	switch {
	// A write still out. The old number was measured against a branch this pull
	// request no longer targets, and rendering it under the new name is the one
	// frame worth a third case.
	case m.detail.BaseWriting:
		text, c = "Retargeting to "+base, m.theme.Subtle

	// The write landed and nothing has counted yet. It is a fact rather than a
	// wait, and it has to read as one: the refetch behind it can fail, and a row
	// left saying "Retargeting" would report a finished write as in flight for
	// the rest of the session.
	case d.BehindBy == gh.BehindUnknown:
		text, c = "Merging into "+base, m.theme.Subtle

	// No head branch left to compare, so the row names the base and claims
	// nothing about the distance to it.
	case d.BehindBy == gh.BehindNoHead:
		text, c = "Based on "+base, m.theme.Subtle

	case d.BehindBy > 0:
		text, c = comp.Plural(d.BehindBy, "commit")+" behind "+base, m.theme.Warning
	}

	if m.detail.Loaded && !m.detail.StateWriting && (!d.Viewer.CanUpdate || d.State == gh.PRStateMerged) {
		return m.railFact(text, c, width)
	}
	return m.railControl(focusBase, text, c, width)
}

// mergeRow is whether it can be merged, and what is in the way if it cannot.
// The ring stops here for the same reason it stops on the state: merging is a
// change to what this row says, and this is the row that says it.
//
// Only where there is a merge to make. A pull request with conflicts, a draft,
// and one GitHub has not finished computing all state a fact the ring walks
// past, the way an empty Checks section does: a key that opens a form for a
// write GitHub will refuse is worse than no key.
//
// Only once the detail has landed, which is the rule every row here holds to.
// Before that nothing is known, which is not the same as nothing being allowed.
//
// And never while a lifecycle write is out, which is the guard the State row
// keeps and for the same reason. The optimistic merge moves the state under
// this row, so without it the key vanishes between the press and the answer,
// taking the ring stop out from under the reader standing on this very row: the
// highlight goes, and the next tab re-anchors to the top of the rail rather
// than stepping to the row below.
func (m Model) mergeRow(d gh.PullRequestDetail, width int) []railEntry {
	// What it says and whether it is a control are two questions, and the
	// optimistic merge answers them differently. The lifecycle decides the
	// words, because mergeStateStatus says what stands in the way of merging
	// and has nothing to say once the merge has happened; the base goes with
	// them rather than the bare word, which the State row above already
	// carries. So the row reads as merged the moment the key is pressed.
	text, c := comp.MergeStateLabel(m.theme, d.Merge, d.Checks)
	if d.State == gh.PRStateMerged {
		text, c = "Merged into "+d.BaseRefName, m.theme.Accent
	}

	// The key is the other question, and during the write it stays. Enter is
	// inert meanwhile, on startMerge's own guard, which is the arrangement the
	// State row keeps for the same reason.
	if m.detail.Loaded && !m.detail.StateWriting && !mergeable(d) {
		return m.railFact(text, c, width)
	}
	return m.railControl(focusMerge, text, c, width)
}

// mergeable is whether this pull request has a merge on offer.
//
// Clean is the ordinary case and checks failing is the other one: GitHub's own
// button merges an UNSTABLE pull request, because a red check that no rule
// requires is not a rule.
//
// Blocked and behind are a protection rule standing in the way, and
// viewerCanMergeAsAdmin is GitHub saying this account may lift it. They go
// together rather than apart because they are the same kind of refusal. Nothing
// lifts a conflict, and nothing merges a draft.
//
// The lifecycle is read first, and not merely for being merged. GitHub answers
// CLEAN on a closed pull request as readily as on an open one, and a close
// applied here moves the state and deliberately leaves the merge status alone,
// so reading the status by itself keeps a live "Ready to merge" control on a
// pull request nothing is going to merge.
func mergeable(d gh.PullRequestDetail) bool {
	if d.State != gh.PRStateOpen {
		return false
	}
	switch d.Merge {
	case gh.MergeClean, gh.MergeUnstable:
		return true
	case gh.MergeBlocked, gh.MergeBehind:
		return d.Viewer.CanMergeAsAdmin
	}
	return false
}
