package prview_test

import (
	"errors"
	"fmt"
	"image/color"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/praxis-labs-io/zen-octo/internal/gh"
	"github.com/praxis-labs-io/zen-octo/internal/store"
	"github.com/praxis-labs-io/zen-octo/internal/tui/paint"
	"github.com/praxis-labs-io/zen-octo/internal/tui/prview"
	"github.com/praxis-labs-io/zen-octo/internal/tui/syntax"
	"github.com/praxis-labs-io/zen-octo/internal/tui/theme"
)

const sampleURL = "https://github.com/praxis-labs-io/zen-octo/pull/412"

func samplePR() gh.PullRequest {
	return gh.PullRequest{
		ID: "PR_412", Number: 412, Title: "Fix the auth retry backoff loop",
		URL:        sampleURL,
		Repository: "zen-octo/zen-octo", Author: gh.Actor{Login: "drucial"},
		State: gh.PRStateOpen, BaseRefName: "main", HeadRefName: "fix-auth-retry",
		Additions: 42, Deletions: 7, ChangedFiles: 3, Comments: 24,
		Checks: gh.CheckStateFailure, ReviewDecision: gh.ReviewDecisionChangesRequested,
		CreatedAt: time.Now().Add(-50 * time.Hour),
	}
}

// colorizer is what the screen highlights code with. Tests use the style the
// default theme names, so the colors a diff test asserts are the ones a reader
// sees.
func colorizer() syntax.Syntax {
	s, _ := syntax.New(theme.RosePineMoon.Syntax)
	return s
}

// screen and detailed hand the keys to the page. A screen opens with them on
// the leading pane instead, which is the rail or the column, and almost every
// test here is about what sits beside that. The ones that are about the
// arrival itself take opened and onOpen and move nothing.
//
// 2 is the page on every tab, and a no-op on a frame with only one pane.
func screen(width, height int) prview.Model { return press(onOpen(width, height), "2") }

// onOpen is the screen as a reader meets it, keys and all.
func onOpen(width, height int) prview.Model { return sized(samplePR(), width, height) }

func sized(pr gh.PullRequest, width, height int) prview.Model {
	m := prview.New(theme.RosePineMoon, pr, prview.RailPreference{}, colorizer())
	m.SetSize(width, height)
	return m
}

func press(m prview.Model, keys ...string) prview.Model {
	for _, k := range keys {
		m, _ = m.Update(tea.KeyPressMsg{Code: rune(k[0]), Text: k})
	}
	return m
}

func fgSeq(c color.Color) string {
	r, g, b, _ := c.RGBA()
	return fmt.Sprintf("38;2;%d;%d;%d", r>>8, g>>8, b>>8)
}

func TestTheFrameFillsItsSizeExactly(t *testing.T) {
	sizes := []struct{ width, height int }{
		{width: 200, height: 40},
		{width: 160, height: 24},
		{width: 100, height: 20},
		{width: 60, height: 10},
	}

	for _, size := range sizes {
		name := fmt.Sprintf("%dx%d", size.width, size.height)
		t.Run(name, func(t *testing.T) {
			lines := strings.Split(screen(size.width, size.height).View(), "\n")

			if len(lines) != size.height {
				t.Errorf("frame is %d lines, want %d", len(lines), size.height)
			}
			for i, line := range lines {
				if w := lipgloss.Width(line); w != size.width {
					t.Errorf("line %d is %d cells wide, want %d", i, w, size.width)
				}
			}
		})
	}
}

func TestTabsSwitchAndOnlyOneReadsAsCurrent(t *testing.T) {
	m := detailed(held(sampleDetail()), 160, 24)

	if got := currentTab(t, m.View()); got != "Conversation" {
		t.Errorf("the current tab is %q on open, want Conversation", got)
	}

	next := press(m, "]")
	if got := currentTab(t, next.View()); got != "Commits" {
		t.Errorf("] moved to %q, want Commits", got)
	}
	if !strings.Contains(stripANSI(next.View()), "No commits.") {
		t.Error("the body did not follow the tab")
	}

	// Four tabs, so [ from the first wraps round to the last.
	if got := currentTab(t, press(m, "[").View()); got != "Files" {
		t.Errorf("[ from the first tab wrapped to %q, want Files", got)
	}
}

func TestEscapeAsksToGoBack(t *testing.T) {
	_, cmd := screen(160, 24).Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if cmd == nil {
		t.Fatal("escape produced no command, want a request to go back")
	}
	if _, ok := cmd().(prview.BackMsg); !ok {
		t.Errorf("escape produced %T, want a BackMsg", cmd())
	}
}

func TestTheRailCarriesEverySectionEmptyOrNot(t *testing.T) {
	out := screen(200, 36).View()

	for _, want := range []string{"State", "Checks", "Author", "Changes", "Base", "Merge"} {
		if !strings.Contains(out, want) {
			t.Errorf("rail is missing %q", want)
		}
	}

	// No reviewer and no label are both facts worth reading, and a section that
	// disappears when it is empty reads as one that was never fetched.
	//
	// The three sections with an add row say it with that instead. A row
	// reading "None yet" above one offering to add something says it twice.
	rows := railRows(t, screen(200, 40).View())
	empty := map[string]string{
		"Reviewers": "+ Add reviewer",
		"Assignees": "+ Add assignee",
		"Labels":    "+ Add label",
		"Checks":    "None yet",
	}
	for heading, want := range empty {
		found := false
		for i, row := range rows {
			if row != heading {
				continue
			}
			found = true
			if got := rows[i+1]; got != want {
				t.Errorf("%s = %q, want %q", heading, got, want)
			}
		}
		if !found {
			t.Errorf("rail dropped the %q section rather than showing it empty", heading)
		}
	}

	// State is marked the way Checks and Review are, so the column reads down
	// its glyphs rather than its words.
	for i, row := range rows {
		if row != "State" {
			continue
		}
		if got := rows[i+1]; got != "\uf407 Open" {
			t.Errorf("state row = %q, want it marked with the lifecycle glyph", got)
		}
		return
	}
	t.Fatalf("no State section in the rail: %q", rows)
}

// Collapsing the rail must not lose information. Everything else it carries is
// already on the meta line; checks and review are not.
func TestTheHeaderCarriesChecksAndReviewWhateverTheRailDoes(t *testing.T) {
	// "Author" is a rail heading; the header spells the login with an @ and no
	// heading, so it tells the two columns apart.
	wide := screen(200, 30).View()
	if !strings.Contains(wide, "Author") {
		t.Fatal("setup: the rail is not up at 200 columns")
	}

	narrow := screen(100, 30).View()
	if strings.Contains(narrow, "Author") {
		t.Fatal("setup: the rail is still up at 100 columns")
	}

	for _, frame := range []string{wide, narrow} {
		rows := strings.Join(headerRows(t, frame), "\n")
		for _, want := range []string{"failing", "changes requested"} {
			if !strings.Contains(rows, want) {
				t.Errorf("the header does not carry %q:\n%s", want, rows)
			}
		}
	}

	// The rollup shares the branch's row, so it must not repeat it.
	if strings.Count(narrow, "fix-auth-retry") != 1 {
		t.Error("the header repeats the branch")
	}
}

// Focus is only visible in the border color, so that is what these assert on.
// The conversation pane's own corner opens the frame, which makes it the one
// unambiguous place to read it.
func TestFocusMovesBetweenThePanes(t *testing.T) {
	var (
		focused = fgSeq(theme.RosePineMoon.Accent)
		idle    = fgSeq(theme.RosePineMoon.BorderSubtle)
	)

	m := screen(200, 30)
	if got := conversationBorder(t, m.View()); got != focused {
		t.Fatalf("conversation border = %s on open, want the focused accent", got)
	}

	rail := press(m, "h")
	if got := conversationBorder(t, rail.View()); got != idle {
		t.Errorf("conversation border = %s after h, want it to recede", got)
	}

	if got := conversationBorder(t, press(rail, "l").View()); got != focused {
		t.Errorf("conversation border = %s after l, want focus back on the right pane", got)
	}
	if got := conversationBorder(t, press(rail, "2").View()); got != focused {
		t.Errorf("conversation border = %s after 2, want focus jumped straight back", got)
	}
}

func TestFocusLeavesTheRailWhenTheRailDoes(t *testing.T) {
	hidden := press(screen(200, 30), "h", "d") // focus the rail, then hide it

	if got := conversationBorder(t, hidden.View()); got != fgSeq(theme.RosePineMoon.Accent) {
		t.Errorf("conversation border = %s, want focus back on it once the rail went away", got)
	}
}

// The rail overflows a short frame as readily as the conversation does, and its
// branch names are the only place some of them appear. Movement keys have to
// reach it, and only when it has focus.
func TestTheRailScrollsOnceItHasFocus(t *testing.T) {
	// "Checks" is the last section, so it is off the bottom until the rail
	// moves. It is also a tab label, so the search has to be rail-scoped.
	m := screen(200, 18)

	if railHas(t, m.View(), "Checks") {
		t.Fatal("setup: the rail already fits, so there is nothing to scroll")
	}
	if railHas(t, press(m, "G").View(), "Checks") {
		t.Error("G moved the rail while the conversation had focus")
	}

	rail := press(m, "h", "G")
	if !railHas(t, rail.View(), "Checks") {
		t.Error("the rail did not scroll once it had focus")
	}
	if !strings.Contains(stripANSI(rail.View()), "/") {
		t.Error("the rail carries no position counter, so there is nothing saying it scrolls")
	}
}

// railHas reports a heading being on screen in the details column, which the
// tab strip above it also spells. It matches on the start of the row, because
// the Checks heading carries its mark out at the far edge.
func railHas(t *testing.T, frame, heading string) bool {
	t.Helper()

	for _, row := range railRows(t, frame) {
		if strings.HasPrefix(row, heading) {
			return true
		}
	}
	return false
}

// Author is nil on GitHub once an account is deleted, so the login can be empty
// on a real pull request. The heading of every card is built from it.
func TestADeletedAuthorLeavesNoGapInACardHeading(t *testing.T) {
	d := sampleDetail()
	d.Author = gh.Actor{}
	d.Timeline[0].Actor = gh.Actor{}

	frame := detailed(held(d), 200, 30).View()
	left, right := paneEdges(t, frame)

	// A heading with no login must not open with the separator that would have
	// followed it.
	for i, line := range strings.Split(stripANSI(frame), "\n") {
		body := strings.TrimSpace(strings.Trim(paneBody(line, left, right), "│ "))
		if strings.HasPrefix(body, "·") {
			t.Errorf("line %d = %q, want no separator where the login would be", i, body)
		}
	}

	out := stripANSI(frame)
	for _, want := range []string{"opened this", "commented"} {
		if !strings.Contains(out, want) {
			t.Errorf("the heading lost %q along with the author", want)
		}
	}
}

// The head branch carries a ticket key at the front and runs long. Wrapping it
// costs a second line on every pull request; clipping it costs the tail nobody
// reads.
func TestTheBranchLineClipsTheHeadRatherThanWrapping(t *testing.T) {
	pr := samplePR()
	pr.HeadRefName = "feature/eng-9547-marketing-and-dashboard-share-one-globalscss-so-base-element-styles-leak"

	d := sampleDetail()
	d.PullRequest = pr

	m := prview.New(theme.RosePineMoon, pr, prview.RailPreference{}, colorizer())
	m.SetDetail(held(d))

	// Narrow enough that this branch overruns the header. The header measures
	// against the frame now, so a wide terminal has room for a name this long
	// and would prove nothing.
	m.SetSize(80, 30)

	frame := m.View()
	rows := 0
	for _, row := range headerRows(t, frame) {
		if !strings.Contains(row, "main ←") {
			continue
		}
		rows++
		if !strings.Contains(row, "…") {
			t.Errorf("branch line = %q, want the head marked where it was cut", row)
		}
	}
	if rows != 1 {
		t.Errorf("the branch takes %d lines, want 1", rows)
	}
}

// The room is shared, not split. main is four columns and never loses one of
// them, which is the case worth getting right because it is nearly every pull
// request; what it leaves goes to the name that says what is being merged.
func TestAShortBaseLeavesItsRoomToTheHead(t *testing.T) {
	pr := samplePR()
	pr.BaseRefName = "main"
	pr.HeadRefName = "feature/znn-16-a-release-skill-to-carry-the-judgement-the-workflow-cannot"

	base, head := branchHalves(t, sized(pr, 200, 30).View())
	if base != "main" {
		t.Errorf("the base reads %q, want main whole", base)
	}
	if head != pr.HeadRefName {
		t.Errorf("the head reads %q, want the room main did not take", head)
	}
}

// Two names that will not both fit take half each. There is nothing to choose
// between them, and the key each carries is at the front where a cut spares it.
func TestTwoLongBranchesTakeHalfEach(t *testing.T) {
	pr := samplePR()
	pr.BaseRefName = "feature/znn-15-cut-releases-from-a-tag-and-install-the-binary-from-one"
	pr.HeadRefName = "feature/znn-16-a-release-skill-to-carry-the-judgement-the-workflow-cannot"

	base, head := branchHalves(t, sized(pr, 200, 30).View())
	for _, half := range []struct{ what, name string }{{"base", base}, {"head", head}} {
		if !strings.HasSuffix(half.name, "…") {
			t.Errorf("the %s is not marked where it was cut: %q", half.what, half.name)
		}
		if !strings.Contains(half.name, "znn-1") {
			t.Errorf("the cut took the %s's ticket key with it: %q", half.what, half.name)
		}
	}
	if got := lipgloss.Width(base) - lipgloss.Width(head); got > 1 || got < -1 {
		t.Errorf("the halves differ by %d columns, want them even", got)
	}
}

// The line stops at its measure however wide the terminal is. Two refs running
// the width of a wide frame read as a sentence rather than as a pair.
func TestTheBranchLineStopsAtItsMeasure(t *testing.T) {
	pr := samplePR()
	pr.BaseRefName = strings.Repeat("a", 200)
	pr.HeadRefName = strings.Repeat("b", 200)

	base, head := branchHalves(t, sized(pr, 400, 30).View())
	if got := lipgloss.Width(base + " ← " + head); got != 96 {
		t.Errorf("the branch line is %d columns on a 400-column frame, want 96", got)
	}
}

// And it gives way to a frame narrower than the measure, because nothing else
// holds the header inside the terminal.
func TestTheBranchLineGivesWayToANarrowFrame(t *testing.T) {
	pr := samplePR()
	pr.BaseRefName = strings.Repeat("a", 200)
	pr.HeadRefName = strings.Repeat("b", 200)

	base, head := branchHalves(t, sized(pr, 60, 30).View())
	if got := lipgloss.Width(base + " ← " + head); got > 60-headGutterCols*2 {
		t.Errorf("the branch line is %d columns on a 60-column frame", got)
	}
}

// headGutterCols is what the header is held off the terminal's edges by.
const headGutterCols = 1

// branchHalves is the two names on the branch line, either side of the arrow.
func branchHalves(t *testing.T, frame string) (string, string) {
	t.Helper()

	for _, row := range headerRows(t, frame) {
		base, head, ok := strings.Cut(row, " ← ")
		if !ok {
			continue
		}
		// The status shares this row, at its far edge. Two spaces is the gap
		// spread leaves and neither half carries one.
		if at, _, cut := strings.Cut(head, "  "); cut {
			head = at
		}
		return base, head
	}
	t.Fatal("no branch line on screen")
	return "", ""
}

// The status shares this row, and the branches still take one line. Putting
// anything beside them used to push them onto two, which is answered now by
// measuring the two halves against each other rather than each against the
// frame.
func TestTheBranchesStillTakeOneLine(t *testing.T) {
	rows := 0
	for _, row := range headerRows(t, detailed(held(sampleDetail()), 200, 30).View()) {
		if !strings.Contains(row, "←") {
			continue
		}
		rows++

		base, head := branchHalves(t, detailed(held(sampleDetail()), 200, 30).View())
		if base+" ← "+head != "main ← fix-auth-retry" {
			t.Errorf("the branch half is %q ← %q, want the branches whole", base, head)
		}
	}
	if rows != 1 {
		t.Errorf("the branches take %d lines, want 1", rows)
	}
}

// A right half that fits is not a clipped one. It takes the line to itself
// where the left half will not fit beside it, and an ellipsis there marks a cut
// that never happened.
func TestAHeaderLineTooNarrowForBothHalvesDoesNotMarkACut(t *testing.T) {
	// Measured rather than written down. The half is a state and a rollup and
	// both are worded elsewhere, so a number here goes stale the first time one
	// of them gains a letter.
	var status string
	for _, row := range headerRows(t, detailed(held(sampleDetail()), 200, 30).View()) {
		if _, half, ok := strings.Cut(row, "  "); ok && strings.Contains(row, "failing") {
			status = strings.TrimSpace(half)
		}
	}
	if status == "" {
		t.Fatal("no status half on a frame with room for one")
	}

	// The gutters take two of the frame, so these are the widths where the half
	// fits exactly and with one to spare.
	exact := lipgloss.Width(status) + headGutterCols*2
	for _, width := range []int{exact, exact + 1} {
		t.Run(strconv.Itoa(width), func(t *testing.T) {
			for _, row := range headerRows(t, detailed(held(sampleDetail()), width, 30).View()) {
				if !strings.Contains(row, "failing") {
					continue
				}
				if row != status {
					t.Errorf("status line = %q, want %q whole and unmarked", row, status)
				}
				return
			}
			t.Fatal("no rollup on screen")
		})
	}
}

// Cut short, the header must not spend its last row on the blank that sets it
// apart from the panes. There is nothing left under it to set apart, and the
// frame that cut it is the one least able to spare a row.
func TestAClippedHeaderGivesItsSeparatorBack(t *testing.T) {
	frame := detailed(held(sampleDetail()), 200, 6).View()

	// Three rows are left once the panes have their floor and the header wants
	// four, so the strip goes; the separator goes with it, because a header cut
	// to its last carried row has nothing under it to be set apart from.
	if at := paneTopAt(frame); at != 2 {
		t.Errorf("the panes open on frame line %d, want line 2", at)
	}
	if lines := strings.Split(frame, "\n"); len(lines) != 6 {
		t.Errorf("frame is %d lines, want the 6 it was given", len(lines))
	}
}

// The lifecycle and where the checks and the review got to, pushed to the far
// edge of the branch line the way the title line pushes its numbers.
func TestTheBranchLineCarriesTheStatusAtItsFarEdge(t *testing.T) {
	for _, row := range headerRows(t, detailed(held(sampleDetail()), 200, 30).View()) {
		if !strings.Contains(row, "←") {
			continue
		}
		// The gap is what separates the two halves; neither carries one.
		branches, rollup, ok := strings.Cut(row, "  ")
		if !ok {
			t.Fatalf("branch line = %q, want the status pushed to the far edge", row)
		}
		if branches != "main ← fix-auth-retry" {
			t.Errorf("branch half = %q, want the branches alone", branches)
		}
		if got := strings.TrimSpace(rollup); !strings.HasSuffix(got, "Open · ✗ failing · changes requested") {
			t.Errorf("far edge = %q, want the state, the checks and the review decision", got)
		}
		return
	}
	t.Fatal("no status line on screen")
}

// paneTop is the frame's first pane border, which is the line the tab strip
// rides. It is not the frame's first line: the header is pinned above the panes
// and is what the frame opens with.
func paneTop(frame string) string {
	lines := strings.Split(frame, "\n")
	if at := paneTopAt(frame); at >= 0 {
		return lines[at]
	}
	return ""
}

// paneTopAt is the frame line the panes open on, which is what a row inside one
// is counted from.
func paneTopAt(frame string) int {
	for i, line := range strings.Split(frame, "\n") {
		if strings.HasPrefix(stripANSI(line), "╭") {
			return i
		}
	}
	return -1
}

// stripANSI drops SGR sequences so a test can reason about the text alone.
func stripANSI(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == 0x1b {
			for i < len(s) && s[i] != 'm' {
				i++
			}
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// conversationBorder reads the SGR foreground of the conversation pane's
// top-left corner.
func conversationBorder(t *testing.T, frame string) string {
	t.Helper()

	// Read at the pane's right border rather than its left corner. The first
	// sequence on the line is the rail's border now that the rail leads the
	// row, and the conversation's own corner is not always on the frame at all:
	// a rail too narrow for a column is drawn over it.
	_, right := paneEdges(t, frame)

	line, sgr, visible := paneTop(frame), "", 0
	for i := 0; i < len(line); {
		if strings.HasPrefix(line[i:], "\x1b[") {
			end := strings.IndexByte(line[i:], 'm')
			if end < 0 {
				break
			}
			sgr = line[i+2 : i+end]
			i += end + 1
			continue
		}
		if visible == right {
			return sgr
		}
		_, size := utf8.DecodeRuneInString(line[i:])
		i += size
		visible++
	}
	t.Fatalf("no styled border at the conversation's right edge: %q", line)
	return ""
}

func sampleDetail() gh.PullRequestDetail {
	ago := func(d time.Duration) time.Time { return time.Now().Add(-d) }

	return gh.PullRequestDetail{
		PullRequest: samplePR(),
		Body:        "Caps the backoff at 30s, matching the fetch timeout.",

		Labels: []gh.Label{{ID: "LA_1", Name: "bug"}},
		// With the node id, which the picker checks by: an assignee decoded
		// without one opens the picker with nobody selected.
		Assignees: []gh.Actor{{ID: "U_1", Login: "drucial"}},
		Reviewers: []gh.Reviewer{
			{Actor: gh.Actor{Login: "nkr"}, State: gh.ReviewStateChangesRequested},
			{Actor: gh.Actor{Login: "octobot"}, State: gh.ReviewStateApproved},
			// Marked as a team, which is what the decoder does with one: its
			// handle is built rather than sent, and no write may spell a login
			// with it. Without the flag it reads here as somebody with an
			// outstanding request, which the reviewer picker would then cancel.
			{Actor: gh.Actor{Login: "zen-octo/maintainers"}, Requested: true, Team: true},
		},
		Rollup: gh.CheckRollup{
			State: gh.CheckStateFailure,
			Checks: []gh.Check{
				{Name: "test", Workflow: "Rails Unit Tests", State: gh.CheckStateSuccess},
				{Name: "test", Workflow: "Rails Lint", State: gh.CheckStateFailure},
				{Name: "e2e", Workflow: "E2E Tests", State: gh.CheckStatePending},
				{Name: "codecov", State: gh.CheckStateSkipped},
			},
			Passed: 1, Failed: 1, Pending: 1, Skipped: 1,
		},
		Merge:    gh.MergeBlocked,
		BehindBy: 4,

		// The sample is the viewer's own open pull request, so the state menu
		// has both moves an open one takes and the Assignees section is theirs
		// to change. CanReopen is GitHub's answer for an open one: there is
		// nothing to reopen.
		Viewer: gh.ViewerActions{CanUpdate: true, CanClose: true, CanAssign: true},

		Timeline: []gh.TimelineItem{
			commented("octobot", ago(3*time.Hour), "Coverage held at 84.2%."),
			reviewed("REV_1", "nkr", gh.ReviewStateChangesRequested, ago(2*time.Hour),
				"Two things on the retry path."),
			{Kind: gh.TimelineForcePushed, Actor: gh.Actor{Login: "drucial"}, CreatedAt: ago(time.Hour)},
		},

		Threads: []gh.ReviewThread{
			// Two comments and a reply GitHub will take, so the ring has more
			// than one stop inside a card and the reply keys have a target.
			{ID: "RT_1", ReviewID: "REV_1", Path: "internal/gh/client.go", Line: 42, Side: gh.SideRight,
				CanReply: true, CanResolve: true,
				Hunk: &gh.Hunk{
					Header: "@@ -40,3 +40,4 @@",
					Lines: []gh.DiffLine{
						{Kind: gh.DiffContext, Old: 40, New: 40, Content: "\tfor {"},
						{Kind: gh.DiffRemoved, Old: 41, Content: "\t\ttime.Sleep(delay)"},
						{Kind: gh.DiffAdded, New: 41, Content: "\t\tdelay = min(delay*2, fetchTimeout)"},
					},
				},
				Comments: []gh.Comment{
					{Kind: gh.CommentThread, ID: "RC_1", Author: gh.Actor{Login: "nkr"},
						CreatedAt: ago(2 * time.Hour), Body: "This backs off forever."},
					{Kind: gh.CommentThread, ID: "RC_4", Author: gh.Actor{Login: "octobot"},
						CreatedAt: ago(90 * time.Minute), Body: "Seconded, the cap is the fix."},
				}},
			{ID: "RT_2", ReviewID: "REV_1", Path: "internal/store/store.go", Line: 88, Side: gh.SideLeft,
				IsResolved: true, CanReply: true, CanUnresolve: true,
				Comments: []gh.Comment{
					{Kind: gh.CommentThread, ID: "RC_2", Author: gh.Actor{Login: "nkr"},
						CreatedAt: ago(2 * time.Hour), Body: "Typo."},
					{Kind: gh.CommentThread, ID: "RC_3", Author: gh.Actor{Login: "drucial"},
						CreatedAt: ago(time.Hour), Body: "Fixed."},
				}},
			// A thread nobody may answer, so the keys have something to be inert
			// on. Unowned, so it renders at the end of the page.
			{ID: "RT_4", Path: "internal/tui/app/app.go", Line: 12, Side: gh.SideRight,
				Comments: []gh.Comment{
					{Kind: gh.CommentThread, ID: "RC_5", Author: gh.Actor{Login: "nkr"},
						CreatedAt: ago(time.Hour), Body: "Locked, so no reply."},
				}},
			// A second answerable thread, so a draft has somewhere else to not
			// turn up.
			{ID: "RT_5", Path: "internal/tui/keys/keys.go", Line: 7, Side: gh.SideRight,
				CanReply: true, CanResolve: true,
				Comments: []gh.Comment{
					{Kind: gh.CommentThread, ID: "RC_6", Author: gh.Actor{Login: "octobot"},
						CreatedAt: ago(time.Hour), Body: "Is r free after the move?"},
				}},
		},
	}
}

// commented and reviewed build the two timeline items that carry writing. The
// ids are here because a comment has one, not because a frame reads it.
func commented(who string, at time.Time, body string) gh.TimelineItem {
	return gh.TimelineItem{
		Kind: gh.TimelineComment, Actor: gh.Actor{Login: who}, CreatedAt: at,
		Comment: &gh.Comment{
			Kind: gh.CommentIssue, ID: "IC_" + who, Author: gh.Actor{Login: who},
			CreatedAt: at, Body: body,
		},
	}
}

func reviewed(id, who string, state gh.ReviewState, at time.Time, body string) gh.TimelineItem {
	return gh.TimelineItem{
		Kind: gh.TimelineReview, Actor: gh.Actor{Login: who}, CreatedAt: at, Review: state,
		Comment: &gh.Comment{
			Kind: gh.CommentReview, ID: id, Author: gh.Actor{Login: who},
			CreatedAt: at, Body: body,
		},
	}
}

// held wraps a detail the way the store hands one over.
func held(d gh.PullRequestDetail) store.Detail {
	return store.Detail{Detail: d, Status: store.StatusReady, Loaded: true}
}

func detailed(d store.Detail, width, height int) prview.Model {
	return press(opened(d, width, height), "2")
}

// opened is the screen as a reader meets it: the leading pane has the keys.
func opened(d store.Detail, width, height int) prview.Model {
	m := prview.New(theme.RosePineMoon, samplePR(), prview.RailPreference{}, colorizer())
	m.SetDetail(d)
	m.SetSize(width, height)
	return m
}

func TestTheConversationCarriesTheDescriptionAndEverythingSaidSince(t *testing.T) {
	out := stripANSI(detailed(held(sampleDetail()), 200, 60).View())

	for _, want := range []string{
		"drucial · opened this",
		"Caps the backoff at 30s",
		"octobot · commented",
		"Coverage held at 84.2%",
		"nkr · requested changes",
		"Two things on the retry path",
		"internal/gh/client.go:42",
		"This backs off forever",
		"drucial · force-pushed",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the conversation is missing %q", want)
		}
	}
}

// GitHub hides resolved threads by default, and on a heavily reviewed pull
// request the settled nits bury the live ones.
func TestAResolvedThreadCollapsesAndAnOpenOneDoesNot(t *testing.T) {
	out := stripANSI(detailed(held(sampleDetail()), 200, 60).View())

	if !strings.Contains(out, "✓ internal/store/store.go:88 · resolved") {
		t.Error("the resolved thread does not name itself as resolved")
	}
	if !strings.Contains(out, "▸ 2 comments") {
		t.Error("the resolved thread does not say what is behind it")
	}
	for _, body := range []string{"Typo.", "Fixed."} {
		if strings.Contains(out, body) {
			t.Errorf("the resolved thread rendered %q rather than staying collapsed", body)
		}
	}

	// The open one keeps its comments, which is the whole point of the split.
	if !strings.Contains(out, "This backs off forever") {
		t.Error("the open thread collapsed too")
	}
}

// A conversation with nothing in it yet is one block saying why. At the top of
// the pane it reads as the first thing said; in the middle of it, it reads as
// the page waiting.
func TestAConversationWithNothingInItCentresWhatItSaysInstead(t *testing.T) {
	tests := []struct {
		name string
		held store.Detail
		want string
		// short says the block is narrow enough to be centred across the
		// measure. A wrapped error fills it, so its left edge is the measure's
		// own gutter and there is nothing to centre.
		short bool
	}{
		{
			name:  "loading",
			held:  store.Detail{Status: store.StatusLoading},
			want:  "Loading the conversation",
			short: true,
		},
		{
			name: "failed",
			held: store.Detail{Status: store.StatusFailed, Err: errors.New("no such host")},
			want: "Could not load the conversation",
		},
	}

	const width, height = 140, 24

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			frame := detailed(tt.held, width, height).View()
			left, right := paneEdges(t, frame)

			lines := strings.Split(stripANSI(frame), "\n")
			at := -1
			for i, line := range lines {
				if strings.Contains(paneBody(line, left, right), tt.want) {
					at = i
					break
				}
			}
			if at < 0 {
				t.Fatalf("no %q in the frame\n%s", tt.want, stripANSI(frame))
			}

			// The pane's own borders bound the region the block is centred in,
			// less the blank row the pane opens with. Its bottom border is the
			// frame's last line.
			top := paneTopAt(frame)
			above, below := at-(top+2), (height-2)-at
			if above <= 0 || abs(above-below) > 1 {
				t.Errorf("%q has %d lines above it and %d below, want it centred in the pane\n%s",
					tt.want, above, below, stripANSI(frame))
			}

			if !tt.short {
				return
			}

			// Two centrings stack here, the block inside the measure and the
			// measure inside the pane, and each can spend its odd column on the
			// right. So the two sides can differ by two rather than one.
			body := paneBody(lines[at], left, right)
			lead := len(body) - len(strings.TrimLeft(body, " "))
			trail := len(body) - len(strings.TrimRight(body, " "))
			if lead == 0 || abs(lead-trail) > 2 {
				t.Errorf("%q sits %d in from the left and %d from the right, want it centred", tt.want, lead, trail)
			}
		})
	}
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

func TestTheBodyStatesReadAsThemselves(t *testing.T) {
	tests := []struct {
		name string
		held store.Detail
		want string
	}{
		{
			name: "nothing yet",
			held: store.Detail{Status: store.StatusLoading},
			want: "Loading the conversation",
		},
		{
			name: "never loaded and failed",
			held: store.Detail{Status: store.StatusFailed, Err: errors.New("no such host")},
			want: "Could not load the conversation: no such host",
		},
		{
			// A refetch that fails must not empty a screen that was reading
			// fine. The root raises a toast; the screen keeps what it had.
			name: "loaded, then a refetch failed",
			held: store.Detail{Detail: sampleDetail(), Status: store.StatusFailed,
				Loaded: true, Err: errors.New("no such host")},
			want: "Caps the backoff at 30s",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if out := stripANSI(detailed(tt.held, 200, 40).View()); !strings.Contains(out, tt.want) {
				t.Errorf("the body does not carry %q", tt.want)
			}
		})
	}
}

// One viewport serves all four tabs. Without a parked offset, switching to a
// short tab clamps it to zero and switching back lands at the top.
func TestScrollPositionSurvivesATabSwitch(t *testing.T) {
	m := press(detailed(held(sampleDetail()), 100, 12), "G")

	before := footerOf(t, m.View())
	if before == "" {
		t.Fatal("setup: the conversation fits, so there is nothing to park")
	}

	if after := footerOf(t, press(m, "]", "[").View()); after != before {
		t.Errorf("position = %q after leaving and coming back, want %q", after, before)
	}
}

// The pane opens on a blank line and closes on one. Scrolled to the end, the
// last line of a comment would otherwise sit against the bottom border and read
// as clipped.
func TestTheConversationEndsOnABlankLine(t *testing.T) {
	// Narrow enough that the rail is off, so every column belongs to the one
	// pane and a blank row is really blank.
	m := press(detailed(held(sampleDetail()), 100, 12), "G")

	lines := strings.Split(stripANSI(m.View()), "\n")
	if len(lines) < 3 {
		t.Fatalf("the frame is %d lines", len(lines))
	}

	last := lines[len(lines)-2]
	if strings.TrimSpace(strings.Trim(last, "│")) != "" {
		t.Errorf("the pane ends on %q, want a blank line above the border", last)
	}
}

// refreshed presses r and returns what it asked the root for.
func refreshed(t *testing.T, m prview.Model) prview.RefreshMsg {
	t.Helper()

	_, cmd := key(m, "s")
	if cmd == nil {
		t.Fatal("s asked for nothing")
	}
	asked := cmd()
	msg, ok := asked.(prview.RefreshMsg)
	if !ok {
		t.Fatalf("s produced %T, want a RefreshMsg", asked)
	}
	return msg
}

// The detail feeds three of the four tabs, so it always goes. The diff beside
// it is a second request, and asking for one the tab is not showing spends it
// on something nobody is looking at.
func TestRefreshAsksForTheDiffTheTabIsShowing(t *testing.T) {
	d := sampleDetail()
	d.Commits = sampleCommits()

	onCommit := press(detailed(held(d), 160, 40), "]")
	onCommit.SetCommitFiles("a3f91c2d5e", commitDiff(sampleFiles()))

	tests := []struct {
		name string
		m    prview.Model
		want prview.RefreshMsg
	}{
		{"conversation", detailed(held(d), 160, 40), prview.RefreshMsg{ID: "PR_412"}},
		{"commits", onCommit, prview.RefreshMsg{ID: "PR_412", SHA: "a3f91c2d5e"}},
		{"checks", press(detailed(held(d), 160, 40), "]", "]"), prview.RefreshMsg{ID: "PR_412"}},
		{"files", press(detailed(held(d), 160, 40), "]", "]", "]"), prview.RefreshMsg{ID: "PR_412", Files: true}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := refreshed(t, tt.m); got != tt.want {
				t.Errorf("r asked for %+v, want %+v", got, tt.want)
			}
		})
	}
}

// The Commits tab opens with nothing on the pane and a wait armed for the
// commit under the cursor. A refresh in that window has no diff to refetch.
func TestRefreshOnCommitsAsksForNothingBeforeADiffIsOnThePane(t *testing.T) {
	if got := refreshed(t, onCommits(160, 40)); got.SHA != "" {
		t.Errorf("r asked for %q, want no commit while the pane is still empty", got.SHA)
	}
}

func TestTheRailTakesTheDetailOnceItLands(t *testing.T) {
	out := stripANSI(detailed(held(sampleDetail()), 200, 40).View())

	for _, want := range []string{
		"Labels", "bug",
		"Assignees", "drucial",
		"Reviewers", "nkr",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the rail is missing %q", want)
		}
	}
}

// A rollup that says "failing" does not say which one, and that is the whole
// question a failing check raises.
func TestTheRailNamesEveryCheck(t *testing.T) {
	out := stripANSI(detailed(held(sampleDetail()), 200, 40).View())

	// Two jobs both called "test" are one row twice unless the workflow names
	// them apart. Every row takes the same mark; its color is the state.
	for _, want := range []string{
		"● Rails Unit Tests / test",
		"● Rails Lint / test",
		"● E2E Tests / e2e",
		"● codecov", // a status context has no workflow to name it with
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the rail is missing %q", want)
		}
	}
	// The counts said the same thing the names now say.
	if strings.Contains(out, "1 passed") {
		t.Error("the rail still carries the counts as well as the names")
	}

	// Every mark is the same shape, so the color is the whole of the meaning.
	marks := railMarks(t, detailed(held(sampleDetail()), 200, 44).View())
	for _, want := range []struct {
		state string
		color color.Color
	}{
		{state: "passing", color: theme.RosePineMoon.Success},
		{state: "running", color: theme.RosePineMoon.Warning},
		{state: "skipped", color: theme.RosePineMoon.Subtle},
	} {
		if !marks[fgSeq(want.color)] {
			t.Errorf("no %s check is marked in its own color", want.state)
		}
	}
}

// The branch is the second line of the header, and the rail has no room to
// spend saying it twice.
func TestTheRailLeavesTheBranchToTheHeader(t *testing.T) {
	frame := detailed(held(sampleDetail()), 200, 40).View()

	if strings.Contains(stripANSI(frame), "Branch") {
		t.Error("the rail still has a Branch section")
	}
	// It is still on screen, just not here.
	if rows := headerRows(t, frame); !strings.Contains(rows[1], "main ← fix-auth-retry") {
		t.Errorf("header line 1 = %q, want the branches", rows[1])
	}
}

// Thirty-odd columns is not enough to spend on the word "files".
func TestTheChangesRowIsOneLineMarkedWithAGlyph(t *testing.T) {
	frame := detailed(held(sampleDetail()), 200, 40).View()

	rows := railRows(t, frame)
	for i, row := range rows {
		if row != "Changes" {
			continue
		}
		if got := rows[i+1]; got != "+42 −7  3 \uea7b" {
			t.Errorf("changes row = %q, want the churn and the count on one line", got)
		}
		return
	}
	t.Fatalf("no Changes section in the rail: %q", rows)
}

// Labels take the theme's accent. GitHub's own hex is not fetched at all: it is
// chosen against a white browser page, so a pale label vanishes on a dark
// terminal and no theme can reach it.
func TestALabelTakesTheThemesAccent(t *testing.T) {
	if !strings.Contains(detailed(held(sampleDetail()), 200, 40).View(), fgSeq(theme.RosePineMoon.Accent)) {
		t.Error("the label is not in the theme's accent")
	}
}

func TestTheFrameStillFillsItsSizeWithADetailLoaded(t *testing.T) {
	sizes := []struct{ width, height int }{
		{width: 200, height: 40},
		{width: 160, height: 24},
		{width: 100, height: 20},
		{width: 60, height: 10},
	}

	for _, size := range sizes {
		name := fmt.Sprintf("%dx%d", size.width, size.height)
		t.Run(name, func(t *testing.T) {
			lines := strings.Split(detailed(held(sampleDetail()), size.width, size.height).View(), "\n")

			if len(lines) != size.height {
				t.Errorf("frame is %d lines, want %d", len(lines), size.height)
			}
			for i, line := range lines {
				if w := lipgloss.Width(line); w != size.width {
					t.Errorf("line %d is %d cells wide, want %d", i, w, size.width)
				}
			}
		})
	}
}

// footerOf reads the scroll counter out of the conversation pane's bottom
// border, which is the only place the position is on screen.
func footerOf(t *testing.T, frame string) string {
	t.Helper()

	lines := strings.Split(stripANSI(frame), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if !strings.HasPrefix(lines[i], "╰") {
			continue
		}
		digits := strings.Trim(lines[i], "╰╯─")
		return digits
	}
	return ""
}

// tokens is a body of distinct words, so a missing one names itself.
func tokens(prefix string, n int) string {
	words := make([]string, n)
	for i := range words {
		words[i] = fmt.Sprintf("%s%03d", prefix, i)
	}
	return strings.Join(words, " ")
}

// The pane clips overflow silently, so a frame that fills its width says
// nothing about the text inside it. Markdown rendered even a few columns too
// wide loses its tail on every line, and only the words prove otherwise.
func TestTheConversationWrapsToFitRatherThanBeingClipped(t *testing.T) {
	body, comment := tokens("body", 120), tokens("note", 60)

	d := sampleDetail()
	d.Body = body
	d.Threads[0].Comments[0].Body = comment

	out := stripANSI(detailed(held(d), 100, 60).View())

	for _, want := range []string{body, comment} {
		for _, word := range strings.Fields(want) {
			if !strings.Contains(out, word) {
				t.Fatalf("%q never reached the screen, so the text is being clipped", word)
			}
		}
	}
}

// Nothing on a screen with no conversation on it yet says anything is
// happening, so the glyph has to keep moving until there is.
func TestTheSpinnerRunsUntilThereIsSomethingToRead(t *testing.T) {
	m := detailed(store.Detail{Status: store.StatusLoading}, 120, 20)

	start := m.Init()
	if start == nil {
		t.Fatal("Init started no spinner")
	}

	before := stripANSI(m.View())
	m, next := m.Update(start())
	if next == nil {
		t.Fatal("the chain ended while there was still nothing to read")
	}
	if stripANSI(m.View()) == before {
		t.Error("the frame did not move, so nothing on screen says it is working")
	}

	// Once the conversation lands there is nothing left to wait on, and a
	// refetch behind it would be spinning over text the reader is already in.
	m.SetDetail(held(sampleDetail()))
	if _, cmd := m.Update(next()); cmd != nil {
		t.Error("the spinner kept ticking over a conversation that had landed")
	}
}

// measureGutters reads the first card's top border, which is drawn to exactly
// the measure. It returns the blank columns either side of it and its own
// width.
//
// This was the header rule until the header stopped drawing one: two
// horizontals a row apart read as a box that had come open. The card's border
// is the same measure and is on every conversation, which the rule was not.
func measureGutters(t *testing.T, frame string) (lead, measure, trail int) {
	t.Helper()

	left, right := paneEdges(t, frame)
	for _, line := range strings.Split(stripANSI(frame), "\n") {
		body := []rune(paneBody(line, left, right))
		// The top edge alone: a card's middle rule and its foot are the same
		// width, and any of the three would do, but one answer per frame is
		// what makes the reading stable.
		if !strings.Contains(string(body), "╭─") {
			continue
		}

		start, end := -1, -1
		for i, r := range body {
			if r == '╭' || r == '─' || r == '╮' {
				if start < 0 {
					start = i
				}
				end = i
			}
		}
		if start < 0 {
			continue
		}
		return start, end - start + 1, len(body) - end - 1
	}

	t.Fatal("no card border in the frame")
	return 0, 0, 0
}

// Prose set the full width of a wide terminal is a paragraph the eye loses its
// place in on every line.
func TestTheConversationIsSetToAMeasureAndCentred(t *testing.T) {
	lead, rule, trail := measureGutters(t, detailed(held(sampleDetail()), 200, 40).View())

	if lead == 0 || trail == 0 {
		t.Errorf("gutters = %d and %d, want the content held off both edges", lead, trail)
	}
	// The odd column goes to the right, so the two differ by at most one.
	if gap := trail - lead; gap < 0 || gap > 1 {
		t.Errorf("gutters = %d and %d, want them even", lead, trail)
	}

	_, wider, _ := measureGutters(t, detailed(held(sampleDetail()), 300, 40).View())
	if wider != rule {
		t.Errorf("the measure grew from %d to %d with the terminal", rule, wider)
	}
}

// Under the measure there is nothing to centre, and a gutter would only make a
// narrow pane narrower.
func TestANarrowPaneKeepsEveryColumn(t *testing.T) {
	lead, _, trail := measureGutters(t, detailed(held(sampleDetail()), 60, 20).View())

	if lead != 0 || trail != 0 {
		t.Errorf("gutters = %d and %d on a pane under the measure, want none", lead, trail)
	}
}

// The border is one line. This is the rest of the body held to the same measure,
// which takes text long enough to reach past it if nothing stopped it.
func TestNothingInTheBodyRunsPastTheMeasure(t *testing.T) {
	d := sampleDetail()
	d.Body = tokens("body", 200)
	d.Threads[0].Comments[0].Body = tokens("note", 100)

	assertWithinMeasure(t, detailed(held(d), 200, 60).View())
}

// The header is built by hand rather than by glamour, so nothing holds it to
// the frame unless this file does. A long branch name is what finds that out.
func TestALongHeaderHoldsItsMeasure(t *testing.T) {
	pr := samplePR()
	// Long enough to overrun the line's measure even with main leaving it every
	// column main does not want.
	pr.HeadRefName = "feature/eng-9547-marketing-and-dashboard-share-one-globalscss-so-base-element-styles-leak-across-both"

	d := sampleDetail()
	d.PullRequest = pr

	m := prview.New(theme.RosePineMoon, pr, prview.RailPreference{}, colorizer())
	m.SetDetail(held(d))
	m.SetSize(150, 30)

	assertWithinMeasure(t, m.View())

	// The tail is what goes: the key at the front is what names the pull
	// request, and the line stays on one row either way.
	out := stripANSI(m.View())
	if strings.Contains(out, "styles-leak-across-both") {
		t.Error("the branch ran past the measure on a frame with room for it")
	}
	if !strings.Contains(out, "main ← feature/eng-9547-marketing") {
		t.Errorf("the branch lost the front of its name rather than the tail:\n%s", out)
	}
}

// The number reads the same on both screens, and on the list it leads the row
// in the accent.
func TestTheNumberLeadsTheTitleInTheAccent(t *testing.T) {
	out := detailed(held(sampleDetail()), 200, 30).View()

	if !strings.Contains(out, "1;"+fgSeq(theme.RosePineMoon.Accent)+"m#412") {
		t.Error("the number does not lead the title in the accent")
	}
	if !strings.Contains(stripANSI(out), "#412 Fix the auth retry backoff loop") {
		t.Error("the number is not before the title")
	}
}

// A thread belongs to the review that opened it, and nothing else on the screen
// says so once the review's own box has closed.
func TestThreadsHangOffTheReviewThatOpenedThem(t *testing.T) {
	out := stripANSI(detailed(held(sampleDetail()), 200, 60).View())

	// The elbow meets the card's heading row, not its top border.
	if !strings.Contains(out, "├─│ internal/gh/client.go:42") {
		t.Error("the branch marker does not meet the thread's heading")
	}
	if !strings.Contains(out, "│ ╭") {
		t.Error("the rail does not run past the card's top border")
	}
	// The last thread closes the run, and its elbow meets its heading row the
	// same way an unresolved one's does.
	if !strings.Contains(out, "╰─│ ✓ internal/store/store.go:88") {
		t.Error("the last thread does not close the run")
	}
}

// Bot comments open with a hidden marker often enough that it matters: the
// marker renders to nothing but still costs the blank line after it.
func TestASegmentThatRendersToNothingLeavesNoGap(t *testing.T) {
	d := sampleDetail()
	d.Body = "<!-- linear-preview -->\n\nReview in Linear\n"

	frame := detailed(held(d), 200, 40).View()
	left, right := paneEdges(t, frame)
	lines := strings.Split(stripANSI(frame), "\n")

	for i, line := range lines {
		if !strings.Contains(line, "drucial · opened this") {
			continue
		}
		// The heading, then its rule, then the first line of the body.
		if got := strings.Trim(paneBody(lines[i+2], left, right), "│ "); got != "Review in Linear" {
			t.Errorf("first body line = %q, want the text with no gap above it", got)
		}
		return
	}
	t.Fatal("no description card on screen")
}

// A byline pressed against the comment under it reads as one paragraph.
func TestAThreadCommentIsSpacedFromItsByline(t *testing.T) {
	frame := detailed(held(sampleDetail()), 200, 40).View()
	left, right := paneEdges(t, frame)
	lines := strings.Split(stripANSI(frame), "\n")

	for i, line := range lines {
		if !strings.Contains(line, "nkr · said · ") {
			continue
		}
		if i+1 >= len(lines) {
			t.Fatal("the byline is the last line on screen")
		}
		// The card and the tree rail draw their own borders through the line;
		// what is left after them is the content.
		if got := strings.Trim(paneBody(lines[i+1], left, right), "│ "); got != "" {
			t.Errorf("line after the byline = %q, want a blank one", got)
		}
		return
	}
	t.Fatal("no thread byline on screen")
}

// GitHub collapses <details> in the browser, and a bot review pastes a table of
// every file it looked at.
func TestDetailsFoldToALineAndOpenOnTheKey(t *testing.T) {
	d := sampleDetail()
	// The summary is long on purpose: the fold line is built by hand, so
	// nothing wraps it and the card would clip it mid-word.
	d.Body = "The problem.\n\n<details>\n<summary>ENG-9547 Marketing and dashboard share one globals.css, so base element styles leak across both of them</summary>\n\n| a.go | did a thing |\n| b.go | did another |\n\n</details>\n"

	m := detailed(held(d), 200, 40)
	assertWithinMeasure(t, m.View())

	folded := stripANSI(m.View())
	if !strings.Contains(folded, "▸ ENG-9547 Marketing") || !strings.Contains(folded, "· 2 lines") {
		t.Error("the fold does not name what is behind it")
	}
	// Wrapped, not clipped: the tail of the summary is still on screen.
	if !strings.Contains(folded, "across both of them") {
		t.Error("the fold line was cut rather than wrapped")
	}
	if strings.Contains(folded, "did a thing") {
		t.Error("the folded table is on screen anyway")
	}

	// The key acts on the focused card, and the fold is in the description,
	// which is the card the cursor opens on.
	//
	// Pressed in sequence rather than from the same starting model each time.
	// What is unfolded is a map, which every copy of the model shares.
	m = press(m, "space")
	if !strings.Contains(stripANSI(m.View()), "did a thing") {
		t.Error("o did not open the fold")
	}

	m = press(m, "space")
	if !strings.Contains(stripANSI(m.View()), "▸ ENG-9547 Marketing") {
		t.Error("o a second time did not fold it back")
	}
}

// paneRight is the column the conversation pane's own right border sits in,
// read off the pane's top border.
//
// Scanning a content line for the first │ finds a card's border instead, and
// every assertion built on that only ever looks at the gutter, where there is
// nothing to find.
// paneEdges is where the conversation pane's own borders sit, as rune indices
// into a frame line. It is the last pane on the row rather than the first: the
// rail and the file column both lead it.
func paneEdges(t *testing.T, frame string) (left, right int) {
	t.Helper()

	// Rune indices, not bytes: the border runes are three bytes each.
	left, right = -1, -1
	for i, r := range []rune(stripANSI(paneTop(frame))) {
		switch r {
		case '╭':
			left = i
		case '╮':
			right = i
		}
	}
	if left < 0 || right < 0 {
		t.Fatalf("the frame carries no pane border: %q", stripANSI(paneTop(frame)))
	}
	return left, right
}

// paneBody is the conversation pane's interior on one frame line.
func paneBody(line string, left, right int) string {
	runes := []rune(line)
	if len(runes) <= right || left >= len(runes) || runes[left] != '│' {
		return ""
	}
	return string(runes[left+1 : right])
}

// assertWithinMeasure holds every line of the conversation inside the measure
// the rule sets. The pane clips overflow without a mark, so a frame that fills
// its width proves nothing on its own.
func assertWithinMeasure(t *testing.T, frame string) {
	t.Helper()

	lead, rule, _ := measureGutters(t, frame)
	left, right := paneEdges(t, frame)
	for i, line := range strings.Split(stripANSI(frame), "\n") {
		body := []rune(paneBody(line, left, right))
		if len(body) <= lead+rule {
			continue
		}
		if got := strings.TrimSpace(string(body[lead+rule:])); got != "" {
			t.Errorf("line %d runs %q past the measure", i, got)
		}
	}
}

// Text against the border reads as a rendering fault rather than as a box.
func TestACardKeepsItsTextOffTheBorder(t *testing.T) {
	frame := detailed(held(sampleDetail()), 200, 40).View()
	left, right := paneEdges(t, frame)

	cards := 0
	for i, line := range strings.Split(stripANSI(frame), "\n") {
		body := []rune(paneBody(line, left, right))

		// A content row of a card, rather than one of its own edges.
		start := strings.IndexRune(string(body), '│')
		if start < 0 || strings.ContainsAny(string(body), "╭├╰") {
			continue
		}
		cards++

		after := []rune(strings.TrimPrefix(string(body[start:]), "│"))
		if len(after) > 0 && after[0] != ' ' {
			t.Errorf("line %d puts %q against the card border", i, string(after[0]))
		}
	}
	if cards == 0 {
		t.Fatal("no card content on screen")
	}
}

// titleRow is the header line carrying the number.
func titleRow(t *testing.T, frame string) string {
	t.Helper()

	for _, row := range headerRows(t, frame) {
		if strings.Contains(row, "#412") {
			return row
		}
	}
	t.Fatal("no title on screen")
	return ""
}

// The churn is a fixed few cells and the title is not, so the title is the one
// that gives way.
func TestTheChurnSitsAtTheEndOfTheTitleLine(t *testing.T) {
	out := detailed(held(sampleDetail()), 200, 30).View()

	// The churn ends on the measure's own right edge, not wherever the title
	// happened to stop.
	if got := titleRow(t, out); !strings.HasSuffix(got, "+42 −7") {
		t.Errorf("title line = %q, want the churn pushed to the far edge", got)
	}
	if !strings.Contains(out, fgSeq(theme.RosePineMoon.Success)+"m+42") {
		t.Error("additions are not in the success color")
	}
	if !strings.Contains(out, fgSeq(theme.RosePineMoon.Error)+"m−7") {
		t.Error("deletions are not in the error color")
	}

	// It moved off the meta line rather than being copied onto the title. The
	// rail keeps its own Changes section; that one is not a duplicate.
	for _, row := range headerRows(t, out) {
		if strings.Contains(row, "drucial · main") && strings.Contains(row, "+42") {
			t.Errorf("meta line = %q, want the churn gone from it", row)
		}
	}
}

func TestALongTitleClipsRatherThanPushingTheChurnOff(t *testing.T) {
	pr := samplePR()
	pr.Title = strings.Repeat("a very long title ", 12)

	d := sampleDetail()
	d.PullRequest = pr

	m := prview.New(theme.RosePineMoon, pr, prview.RailPreference{}, colorizer())
	m.SetDetail(held(d))
	m.SetSize(200, 30)

	row := titleRow(t, m.View())
	if !strings.HasSuffix(row, "+42 −7") {
		t.Errorf("title line = %q, want the churn still at the end", row)
	}
	if !strings.Contains(row, "…") {
		t.Errorf("title line = %q, want the title marked where it was cut", row)
	}
}

// headerRows is every line above the panes, which is where the header sits: it
// used to close on a rule of its own, and the card's border a row under that
// read as a box that had come open.
//
// A trailing blank is dropped, so what comes back is the lines that carry
// something. Matching on their text instead breaks the moment a line is empty,
// which is one of the cases worth asserting.
func headerRows(t *testing.T, frame string) []string {
	t.Helper()

	var rows []string
	for _, line := range strings.Split(stripANSI(frame), "\n") {
		if strings.HasPrefix(line, "╭") {
			// The blank between the header and the panes belongs to neither.
			if n := len(rows); n > 0 && rows[n-1] == "" {
				rows = rows[:n-1]
			}
			return rows
		}
		rows = append(rows, strings.TrimSpace(line))
	}
	t.Fatal("no pane on screen to close the header")
	return nil
}

// stripRow is the tab strip: the last row of the header that carries anything.
// The header closes on a blank holding it off the pane borders, so the row
// above the first corner is that blank rather than the strip.
func stripRow(t *testing.T, frame string) string {
	t.Helper()

	var last string
	for _, line := range strings.Split(frame, "\n") {
		if strings.HasPrefix(stripANSI(line), "╭") {
			return last
		}
		if strings.TrimSpace(stripANSI(line)) != "" {
			last = line
		}
	}
	t.Fatal("no pane on screen to close the header")
	return ""
}

// currentTab is the tab the strip reads as current, found by the underline it
// alone carries. Matched on the attribute rather than on a rendered sequence:
// lipgloss writes one SGR run per rune for an underlined label, so there is no
// single escape standing in front of the whole name.
func currentTab(t *testing.T, frame string) string {
	t.Helper()

	var out strings.Builder
	row, under := stripRow(t, frame), false
	for len(row) > 0 {
		if !strings.HasPrefix(row, "\x1b[") {
			r := []rune(row)[0]
			if under {
				out.WriteRune(r)
			}
			row = row[len(string(r)):]
			continue
		}
		end := strings.Index(row, "m")
		if end < 0 {
			break
		}
		under = slices.Contains(strings.Split(row[2:end], ";"), "4")
		row = row[end+1:]
	}
	return strings.TrimSpace(out.String())
}

// What and how big, where the code is going, then a gap, then where the reader
// is standing.
func TestTheHeaderReadsAsTwoBlocks(t *testing.T) {
	rows := headerRows(t, detailed(held(sampleDetail()), 200, 30).View())

	// Two lines and four corners: what it is and how big, then where it is
	// going and how it is doing. Who opened it and when is on the status bar.
	// The strip closes the block, on the pane borders it switches.
	want := []string{
		"#412 Fix the auth retry backoff loop",
		"main ← fix-auth-retry",
		"",
		"Conversation",
	}
	if len(rows) != len(want) {
		t.Fatalf("header is %d lines, want %d: %q", len(rows), len(want), rows)
	}
	for i, w := range want {
		if w == "" && rows[i] != "" {
			t.Errorf("header line %d = %q, want it blank", i, rows[i])
		}
		if !strings.Contains(rows[i], w) {
			t.Errorf("header line %d = %q, want it to carry %q", i, rows[i], w)
		}
	}
}

// A tab name is never what gets cut. The counts are the droppable half of the
// strip, so they go first and every width the shell will draw still names all
// four tabs whole.
func TestTheStripDropsItsCountsBeforeATabName(t *testing.T) {
	// A busy pull request, because that is the one the rule is for: four counts
	// of one digit fit the narrowest frame the shell draws, and it is the third
	// digit on a long-running branch that puts the strip over.
	d := sampleDetail()
	for range 128 {
		d.Commits = append(d.Commits, sampleCommits()...)
	}

	for width := 56; width <= 200; width += 8 {
		strip := stripANSI(stripRow(t, detailed(held(d), width, 30).View()))
		for _, tab := range []string{"Conversation", "Commits", "Checks", "Files"} {
			if !strings.Contains(strip, tab) {
				t.Errorf("width %d: %q is cut: %q", width, tab, strip)
			}
		}
	}

	// And it is the counts the narrow frame gave up to do it.
	if narrow := stripANSI(stripRow(t, detailed(held(d), 56, 40).View())); strings.Contains(narrow, "(") {
		t.Errorf("the strip kept its counts where they did not fit: %q", narrow)
	}
	if wide := stripANSI(stripRow(t, detailed(held(d), 200, 40).View())); !strings.Contains(wide, "(24)") {
		t.Errorf("the strip carries no counts where they fit: %q", wide)
	}
}

// The mark is a cell every tab holds, not a prefix the current one gains. Drawn
// on the active tab alone, every tab to its right would step sideways each time
// the reader changed tab, which is a strip that moves under the key that moves
// through it.
func TestTheStripHoldsItsColumnsWhicheverTabIsCurrent(t *testing.T) {
	m := detailed(held(sampleDetail()), 200, 30)

	var first []int
	for _, tab := range []string{"Conversation", "Commits", "Checks", "Files"} {
		strip := stripANSI(stripRow(t, m.View()))

		// Columns rather than byte offsets: the mark is three bytes where the
		// space standing in for it is one, so a strip that never moved would
		// still measure differently on every tab.
		var at []int
		for _, name := range []string{"Conversation", "Commits", "Checks", "Files"} {
			at = append(at, len([]rune(strip[:strings.Index(strip, name)])))
		}
		if first == nil {
			first = at
		}
		if !slices.Equal(at, first) {
			t.Errorf("on %s the labels start at %v, want %v as on the first tab", tab, at, first)
		}
		m = press(m, "]")
	}
}

// A count that has not answered renders nothing. A zero claims the tab is
// empty, which is a different thing from unasked, and the two tabs that wait on
// the detail query are unasked for as long as it is out.
func TestAnUnansweredTabCountIsAbsentRatherThanZero(t *testing.T) {
	strip := stripANSI(stripRow(t, onOpen(200, 30).View()))

	if strings.Contains(strip, "(0)") {
		t.Errorf("the strip reads %q, want no count on what has not answered", strip)
	}
	// The two off the list row are there before the query is: the reader has
	// them the moment the screen opens.
	for _, want := range []string{"Conversation (24)", "Files (3)"} {
		if !strings.Contains(strip, want) {
			t.Errorf("the strip reads %q, want %q off the row", strip, want)
		}
	}
}

// Both panes are named, and the right one by what it holds rather than by the
// tab it is under: the strip a row above already says which tab this is.
func TestEachTabNamesBothOfItsPanes(t *testing.T) {
	m := detailed(held(sampleDetail()), 200, 40)
	m.SetFiles(loadedFiles(sampleFiles(), 0))

	for _, tt := range []struct{ tab, side, main string }{
		{"Conversation", "Details", "Feed"},
		{"Commits", "Commits", "Diff"},
		{"Checks", "Checks", "Log"},
		{"Files", "Files", "Diff"},
	} {
		top := stripANSI(paneTop(m.View()))
		if !strings.Contains(top, "[1]─"+tt.side) {
			t.Errorf("%s: the left pane reads %q, want %q", tt.tab, top, tt.side)
		}
		if !strings.Contains(top, "[2]─"+tt.main) {
			t.Errorf("%s: the right pane reads %q, want %q", tt.tab, top, tt.main)
		}
		m = press(m, "]")
	}
}

// The header names the pull request wherever the reader is standing. It used to
// be the first block of the conversation's own body, so the three tabs with a
// column had nothing on them saying which pull request they belonged to.
func TestTheHeaderIsOnEveryTab(t *testing.T) {
	m := detailed(held(sampleDetail()), 200, 40)

	// The rail is up on the Conversation and on none of the others, and the
	// header must not answer to that: a row it gained on the switch would move
	// every pane border under it.
	rows := -1

	for _, tab := range []string{"Conversation", "Commits", "Checks", "Files"} {
		frame := m.View()
		if !onTab(t, frame, tab) {
			t.Fatalf("the strip does not read %q as the current tab", tab)
		}

		head := headerRows(t, frame)
		if len(head) == 0 || !strings.Contains(head[0], "#412") {
			t.Errorf("%s carries no header: %q", tab, head)
		}
		if rows < 0 {
			rows = len(head)
		}
		if len(head) != rows {
			t.Errorf("%s has a %d-line header, want %d as on the Conversation", tab, len(head), rows)
		}
		m = press(m, "]")
	}
}

// How much it touches sits at the far edge of the title line: the file count
// marked with a glyph rather than the word, then the churn. The rail's own
// Changes row writes the same pair.
func TestTheTitleLineCountsTheFilesBeforeTheChurn(t *testing.T) {
	row := titleRow(t, detailed(held(sampleDetail()), 200, 30).View())

	if !strings.HasSuffix(row, "3   +42 −7") {
		t.Errorf("title line = %q, want the file count ahead of the churn", row)
	}
}

// The header is pinned to the frame rather than to the pane below it. Measured
// against the main pane it would start where that pane does, and a tab opening
// a column beside the diff would take it a column's width to the right.
func TestTheHeaderHoldsItsColumnAcrossTabs(t *testing.T) {
	m := detailed(held(sampleDetail()), 200, 40)
	m.SetFiles(loadedFiles(sampleFiles(), 0))

	startsAt := func(frame string) int {
		line := stripANSI(strings.Split(frame, "\n")[0])
		return len(line) - len(strings.TrimLeft(line, " "))
	}

	files := press(m, "]", "]", "]")
	if !strings.Contains(stripANSI(paneTop(files.View())), "Files") {
		t.Fatal("setup: the Files tab opened no column, so there is nothing to hold against")
	}

	if conv, at := startsAt(m.View()), startsAt(files.View()); conv != at {
		t.Errorf("the header starts at column %d on the Conversation and %d on Files, want it held", conv, at)
	}
}

// Above the panes rather than inside one, so paging the conversation cannot
// take it off the screen.
func TestTheHeaderDoesNotScrollWithTheConversation(t *testing.T) {
	m := press(detailed(held(sampleDetail()), 200, 24), "G")

	if rows := headerRows(t, m.View()); len(rows) == 0 || !strings.Contains(rows[0], "#412") {
		t.Errorf("the header scrolled away with the conversation: %q", rows)
	}
}

// The readout is who raised the pull request and how long ago, for the status
// bar. Compact, because the bar's left half is a line of key hints that runs
// most of the width and a clause spelled out is one clipped mid-handle.
func TestTheReadoutNamesWhoOpenedItAndWhen(t *testing.T) {
	tests := []struct {
		name    string
		created time.Time
		want    string
	}{
		{name: "days", created: time.Now().Add(-50 * time.Hour), want: "@drucial · 2d"},
		{name: "hours", created: time.Now().Add(-19 * time.Hour), want: "@drucial · 19h"},
		{name: "minutes", created: time.Now().Add(-34 * time.Minute), want: "@drucial · 34m"},
		{name: "moments", created: time.Now(), want: "@drucial · now"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := sampleDetail()
			d.CreatedAt = tt.created

			if got := detailed(held(d), 200, 30).Readout(); got != tt.want {
				t.Errorf("readout = %q, want %q", got, tt.want)
			}
		})
	}
}

// Either half can be missing: a deleted account has no login, and the row the
// list opens with carries no timestamp until the detail lands. Neither leaves a
// separator with nothing after it.
func TestTheReadoutDropsWhicheverHalfIsMissing(t *testing.T) {
	tests := []struct {
		name  string
		login string
		when  time.Time
		want  string
	}{
		{name: "no timestamp", login: "drucial", want: "@drucial"},
		{name: "no author", when: time.Now().Add(-50 * time.Hour), want: "2d"},
		{name: "neither", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := sampleDetail()
			d.CreatedAt = tt.when
			d.Author = gh.Actor{Login: tt.login}

			if got := detailed(held(d), 200, 30).Readout(); got != tt.want {
				t.Errorf("readout = %q, want %q", got, tt.want)
			}
		})
	}
}

// And the header no longer carries any of it. The line it was on is gone.
func TestTheHeaderLeavesTheOpenedByToTheBar(t *testing.T) {
	for _, row := range headerRows(t, detailed(held(sampleDetail()), 200, 30).View()) {
		if strings.Contains(row, "Opened") || strings.Contains(row, "@drucial") {
			t.Errorf("header line %q still carries who opened the pull request", row)
		}
	}
}

// railRows is the details column's own lines, which lead the row and stop at
// the conversation pane's left border.
func railRows(t *testing.T, frame string) []string {
	t.Helper()

	left, _ := paneEdges(t, frame)
	var rows []string
	for _, line := range strings.Split(stripANSI(frame), "\n") {
		runes := []rune(line)
		if left == 0 || len(runes) < left {
			continue
		}
		rows = append(rows, strings.Trim(string(runes[:left]), "│╭╮╰╯─ "+paint.BarGlyph))
	}
	return rows
}

// Checks runs to any length, so everything of a fixed size goes above it. The
// two rows under it are the exception: they are what you read last.
func TestChecksSitBelowEverythingOfAFixedSize(t *testing.T) {
	rows := railRows(t, detailed(held(sampleDetail()), 200, 40).View())

	at := -1
	for i, row := range rows {
		if strings.HasPrefix(row, "Checks") {
			at = i
			break
		}
	}
	if at < 0 {
		t.Fatalf("no Checks section in the rail: %q", rows)
	}

	// Nothing comes after it. Asserting what comes before instead passes the
	// moment Checks moves anywhere but the very top.
	for _, row := range rows[at+1:] {
		switch row {
		case "State", "Author", "Reviewers", "Assignees", "Labels", "Changes":
			t.Errorf("%q sits below Checks, where the long list pushes it off the bottom", row)
		}
	}
}

// The last two rows are what you read just before merging.
func TestTheRailEndsWithTheBaseAndTheMergeState(t *testing.T) {
	rows := railRows(t, detailed(held(sampleDetail()), 200, 44).View())

	want := []struct{ heading, value string }{
		{"Base", "4 commits behind main"},
		{"Merge", "Blocked"},
	}
	for _, w := range want {
		found := false
		for i, row := range rows {
			if row != w.heading {
				continue
			}
			found = true
			if got := rows[i+1]; got != w.value {
				t.Errorf("%s = %q, want %q", w.heading, got, w.value)
			}
		}
		if !found {
			t.Errorf("no %q section in the rail: %q", w.heading, rows)
		}
	}

	// Merge is the very last thing on the rail.
	var last string
	for _, row := range rows {
		if row != "" {
			last = row
		}
	}
	if last != "Blocked" {
		t.Errorf("the rail ends on %q, want the merge state", last)
	}
}

// GitHub's UNSTABLE means the commit status is not passing, and a check still
// running is not passing. Reading it as a failure reports a build that has not
// finished as a broken one, on the same screen as a header saying it is running.
func TestAnUnstableMergeReadsTheRollupRatherThanAssumingAFailure(t *testing.T) {
	tests := []struct {
		name   string
		checks gh.CheckState
		want   string
	}{
		{"running", gh.CheckStatePending, "Checks running"},
		{"queued", gh.CheckStateExpected, "Checks queued"},
		{"failed", gh.CheckStateFailure, "Checks failing"},
		// A commit status no check run produced leaves the rollup green while
		// GitHub still calls the merge unstable. There is nothing to wait for.
		{"green rollup", gh.CheckStateSuccess, "Checks failing"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := sampleDetail()
			d.Merge = gh.MergeUnstable
			d.Checks = tt.checks

			rows := railRows(t, detailed(held(d), 200, 44).View())
			for i, row := range rows {
				if row != "Merge" {
					continue
				}
				if got := rows[i+1]; got != tt.want {
					t.Errorf("merge row = %q, want %q", got, tt.want)
				}
				return
			}
			t.Fatalf("no Merge section in the rail: %q", rows)
		})
	}
}

// GitHub says "out-of-date". The number is the same answer with the size of the
// problem attached, and zero is the good news.
func TestABranchLevelWithItsBaseSaysSo(t *testing.T) {
	d := sampleDetail()
	d.BehindBy = 0

	rows := railRows(t, detailed(held(d), 200, 44).View())
	for i, row := range rows {
		if row != "Base" {
			continue
		}
		if got := rows[i+1]; got != "Up to date with main" {
			t.Errorf("base row = %q, want it to say the branch is level", got)
		}
		return
	}
	t.Fatalf("no Base section in the rail: %q", rows)
}

// The rail is a column. A wrapped name turns one check into two rows that read
// as two checks.
func TestALongCheckNameClipsRatherThanWrapping(t *testing.T) {
	d := sampleDetail()
	d.Rollup.Checks = []gh.Check{
		{Name: "test (ubuntu-latest, postgres 16)", Workflow: "Rails Integration Tests", State: gh.CheckStateSuccess},
	}

	rows := railRows(t, detailed(held(d), 200, 44).View())

	// Reviewers take the same mark, so the count has to start below the Checks
	// heading rather than at every dot on the rail.
	at := -1
	for i, row := range rows {
		if strings.HasPrefix(row, "Checks") {
			at = i
			break
		}
	}
	if at < 0 {
		t.Fatalf("no Checks section in the rail: %q", rows)
	}

	marked := 0
	for _, row := range rows[at+1:] {
		if !strings.HasPrefix(row, "●") {
			break
		}
		marked++
		if !strings.HasSuffix(row, "…") {
			t.Errorf("check row = %q, want it marked where it was cut", row)
		}
	}
	if marked != 1 {
		t.Errorf("the check takes %d rows, want 1", marked)
	}
}

// GitHub's reviewers panel is who has reviewed plus who was asked. A submitted
// review takes its author off the requests, so building it from requests alone
// drops whoever actually looked at it.
func TestEveryReviewerIsMarkedWithTheirVerdict(t *testing.T) {
	frame := detailed(held(sampleDetail()), 200, 44).View()

	rows := railRows(t, frame)
	at := -1
	for i, row := range rows {
		if row == "Reviewers" {
			at = i
			break
		}
	}
	if at < 0 {
		t.Fatalf("no Reviewers section in the rail: %q", rows)
	}

	want := []struct {
		row   string
		state string
		color color.Color
	}{
		{row: "● @nkr", state: "waiting on a change", color: theme.RosePineMoon.Error},
		{row: "● @octobot", state: "done with it", color: theme.RosePineMoon.Success},
		{row: "● @zen-octo/maintainers", state: "in flight", color: theme.RosePineMoon.Warning},
	}
	for i, w := range want {
		if got := rows[at+1+i]; got != w.row {
			t.Errorf("reviewer %d = %q, want %q", i, got, w.row)
		}
		// The mark is the verdict, so the color is the whole of the meaning.
		if got := markSGR(t, frame, w.row); got != fgSeq(w.color) {
			t.Errorf("%s is marked %s, want the %s color", w.row, got, w.state)
		}
	}
}

// Four colors, because a rail row has one cell to say it in. Someone who left
// unanswered questions and called it a comment is holding up the same thing as
// someone who asked for changes.
func TestAReviewerWithAnOpenThreadReadsAsWaiting(t *testing.T) {
	tests := []struct {
		name     string
		reviewer gh.Reviewer
		color    color.Color
	}{
		{
			name:     "commented with nothing outstanding",
			reviewer: gh.Reviewer{State: gh.ReviewStateCommented},
			color:    theme.RosePineMoon.Subtle,
		},
		{
			name:     "commented with a thread still open",
			reviewer: gh.Reviewer{State: gh.ReviewStateCommented, Unresolved: 2},
			color:    theme.RosePineMoon.Error,
		},
		{
			name:     "approved",
			reviewer: gh.Reviewer{State: gh.ReviewStateApproved},
			color:    theme.RosePineMoon.Success,
		},
		{
			name:     "asked and silent",
			reviewer: gh.Reviewer{},
			color:    theme.RosePineMoon.Subtle,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := sampleDetail()
			tt.reviewer.Actor = gh.Actor{Login: "solo"}
			d.Reviewers = []gh.Reviewer{tt.reviewer}

			frame := detailed(held(d), 200, 44).View()
			if got := markSGR(t, frame, "● @solo"); got != fgSeq(tt.color) {
				t.Errorf("marked %s, want %v", got, tt.color)
			}
		})
	}
}

// A bot login runs past the rail as readily as a workflow name does.
func TestALongReviewerNameClipsRatherThanWrapping(t *testing.T) {
	d := sampleDetail()
	d.Reviewers = []gh.Reviewer{{Actor: gh.Actor{Login: "zen-octo/copilot-pull-request-reviewers"}}}

	rows := railRows(t, detailed(held(d), 200, 44).View())
	for i, row := range rows {
		if row != "Reviewers" {
			continue
		}
		if got := rows[i+1]; !strings.HasSuffix(got, "…") {
			t.Errorf("reviewer row = %q, want it marked where it was cut", got)
		}
		if got := rows[i+2]; strings.HasPrefix(got, "●") {
			t.Errorf("the name wrapped onto %q rather than clipping", got)
		}
		return
	}
	t.Fatalf("no Reviewers section in the rail: %q", rows)
}

// The rail sits in from both borders and opens with a blank, the same as the
// conversation beside it.
func TestTheRailIsPaddedAndOpensWithABlankRow(t *testing.T) {
	frame := detailed(held(sampleDetail()), 200, 44).View()

	raw := railRaw(t, frame)
	if len(raw) < 3 {
		t.Fatalf("rail has %d lines, want a border, a blank and content", len(raw))
	}
	if got := strings.TrimRight(stripANSI(raw[1]), " │"); strings.TrimSpace(got) != "" {
		t.Errorf("rail line 1 = %q, want it blank", got)
	}

	for i, line := range raw[2:] {
		body := stripANSI(line)
		if strings.TrimSpace(strings.Trim(body, "│─╭╮╰╯")) == "" {
			continue
		}
		inner := strings.TrimSuffix(strings.TrimPrefix(body, "│"), "│")
		if !strings.HasPrefix(inner, " ") {
			t.Errorf("rail line %d has no left padding: %q", i+2, inner)
		}
		if !strings.HasSuffix(inner, " ") {
			t.Errorf("rail line %d has no right padding: %q", i+2, inner)
		}
	}
}

// markSGR is the foreground of the dot on the rail row carrying text. Every
// rail mark is the same shape, so the color is the whole of the meaning and the
// only thing worth asserting.
func markSGR(t *testing.T, frame, text string) string {
	t.Helper()

	for _, raw := range railRaw(t, frame) {
		if strings.Trim(stripANSI(raw), "│╭╮╰╯›─ "+paint.BarGlyph) != text {
			continue
		}
		seq, ok := rowMark(raw)
		if !ok {
			t.Fatalf("rail row %q carries no mark", text)
		}
		return seq
	}

	t.Fatalf("no rail row reading %q", text)
	return ""
}

// railMarks is the color of every marked row in the details column. Scoped to
// the rail: the conversation paints the same glyph on its event lines, and a
// frame-wide search answers with one of those instead.
func railMarks(t *testing.T, frame string) map[string]bool {
	t.Helper()

	out := map[string]bool{}
	for _, raw := range railRaw(t, frame) {
		if seq, ok := rowMark(raw); ok {
			out[seq] = true
		}
	}
	return out
}

// rowMark is the SGR a row's mark is painted in. The cell before it belongs to
// the focus marker, so the glyph opens its own styled run.
func rowMark(raw string) (string, bool) {
	at := strings.Index(raw, "m●")
	if at < 0 {
		return "", false
	}
	start := strings.LastIndex(raw[:at], "\x1b[")
	if start < 0 {
		return "", false
	}
	return raw[start+2 : at], true
}

// railRaw is the details column with its styling left on, cut where railRows
// cuts the stripped frame. Slicing on the first │ instead lands inside the
// conversation, where the same marks appear.
func railRaw(t *testing.T, frame string) []string {
	t.Helper()

	left, _ := paneEdges(t, frame)
	top := paneTopAt(frame)
	var rows []string

	for at, line := range strings.Split(frame, "\n") {
		// The header spans the whole frame, so it reaches into the rail's
		// columns without being the rail.
		if at < top {
			continue
		}
		// The rail leads the row, so its share of the line is everything before
		// the conversation's own left border.
		visible, i := 0, 0
		for i < len(line) && visible < left {
			if strings.HasPrefix(line[i:], "\x1b[") {
				end := strings.IndexByte(line[i:], 'm')
				if end < 0 {
					break
				}
				i += end + 1
				continue
			}
			_, size := utf8.DecodeRuneInString(line[i:])
			i += size
			visible++
		}
		if visible == left && left > 0 {
			rows = append(rows, line[:i])
		}
	}
	return rows
}

// Every login on the rail is written the way the header writes it.
func TestTheRailNamesPeopleAsHandles(t *testing.T) {
	rows := railRows(t, detailed(held(sampleDetail()), 200, 44).View())

	want := map[string]string{
		"Author":    "@drucial",
		"Assignees": "@drucial",
	}
	for heading, value := range want {
		found := false
		for i, row := range rows {
			if row != heading {
				continue
			}
			found = true
			if got := rows[i+1]; got != value {
				t.Errorf("%s = %q, want %q", heading, got, value)
			}
		}
		if !found {
			t.Errorf("no %q section in the rail: %q", heading, rows)
		}
	}
}

// eventKinds and eventLabels are two maps, so a kind can land in one and not
// the other. An entry that renders to nothing still costs the blank line the
// join puts after it.
func TestAnEventWithNoWordsForItLeavesNoGap(t *testing.T) {
	ago := func(d time.Duration) time.Time { return time.Now().Add(-d) }

	d := sampleDetail()
	d.Threads = nil
	// Between two comments, so the extra gap is between two cards rather than
	// at the end, where the pane's own padding would hide it.
	d.Timeline = []gh.TimelineItem{
		commented("octobot", ago(2*time.Hour), "First."),
		{Kind: "SOMETHING_GITHUB_ADDED_LATER", Actor: gh.Actor{Login: "drucial"}, CreatedAt: ago(time.Hour)},
		commented("nkr", ago(time.Minute), "Second."),
	}

	frame := detailed(held(d), 200, 44).View()
	left, right := paneEdges(t, frame)
	lines := strings.Split(stripANSI(frame), "\n")

	// Every gap, not the first: the unrendered event sits between the second
	// card and the third, and stopping at the first pair never reaches it.
	closed, gaps := -1, 0
	for i, line := range lines {
		body := paneBody(line, left, right)
		switch {
		case strings.Contains(body, "╰"):
			closed = i
		case closed >= 0 && strings.Contains(body, "╭"):
			gaps++
			if gap := i - closed - 1; gap != 1 {
				t.Errorf("%d blank lines between cards %d and %d, want 1", gap, gaps, gaps+1)
			}
			closed = -1
		}
	}
	if gaps < 2 {
		t.Fatalf("found %d gaps between cards, want the two either side of the event", gaps)
	}
}

// A review comment is about a line of code. Without the line the conversation
// is an assertion about something that is nowhere on the screen.
func TestAThreadShowsTheCodeItWasWrittenAgainst(t *testing.T) {
	out := stripANSI(detailed(held(sampleDetail()), 200, 80).View())

	anchor := strings.Index(out, "internal/gh/client.go:42")
	code := strings.Index(out, "delay = min(delay*2, fetchTimeout)")
	comment := strings.Index(out, "This backs off forever.")

	switch {
	case code < 0:
		t.Fatal("the thread shows no diff at all")
	case code < anchor:
		t.Error("the diff renders above the thread it belongs to")
	case comment > 0 && code > comment:
		t.Error("the diff renders under the comment rather than over it")
	}
}

// GitHub returns the whole hunk, which on a large change is a screenful, and
// the line the comment is about is the last one in it.
func TestALongThreadHunkIsCutToItsTail(t *testing.T) {
	d := sampleDetail()
	long := make([]gh.DiffLine, 0, 30)
	for i := 1; i <= 30; i++ {
		long = append(long, gh.DiffLine{Kind: gh.DiffContext, Old: i, New: i,
			Content: "line" + strconv.Itoa(i)})
	}
	d.Threads[0].Hunk = &gh.Hunk{Header: "@@ -1,30 +1,30 @@", Lines: long}

	out := stripANSI(detailed(held(d), 200, 200).View())
	if strings.Contains(out, "line1\n") {
		t.Error("the whole hunk rendered, want only its tail")
	}
	if !strings.Contains(out, "line30") {
		t.Error("the line the comment is about is missing")
	}
}
