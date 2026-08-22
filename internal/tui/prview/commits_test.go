package prview_test

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/praxis-labs-io/zen-octo/internal/gh"
	"github.com/praxis-labs-io/zen-octo/internal/store"
	"github.com/praxis-labs-io/zen-octo/internal/tui/prview"
	"github.com/praxis-labs-io/zen-octo/internal/tui/theme"
)

// sampleCommits covers what the column has to tell apart: the three check
// states, and an author GitHub has no account for.
func sampleCommits() []gh.Commit {
	ago := func(d time.Duration) time.Time { return time.Now().Add(-d) }

	return []gh.Commit{
		{SHA: "a3f91c2d5e", Short: "a3f91c2", Headline: "Cap the backoff",
			Author: gh.Actor{Login: "drucial"}, CommittedAt: ago(19 * time.Hour),
			Checks: gh.CheckStateSuccess},
		{SHA: "7b20ef4a11", Short: "7b20ef4", Headline: "Drop the count",
			Author: gh.Actor{Login: "nkr"}, CommittedAt: ago(18 * time.Hour),
			Checks: gh.CheckStateFailure},
		{SHA: "c1d8a04bb9", Short: "c1d8a04", Headline: "Fix the typo",
			AuthorName: "Drew White", CommittedAt: ago(17 * time.Hour),
			Checks: gh.CheckStatePending},
	}
}

// onCommits is the screen with a detail loaded, sitting on the Commits tab.
func onCommits(width, height int) prview.Model {
	d := sampleDetail()
	d.Commits = sampleCommits()
	return press(detailed(held(d), width, height), "]")
}

// settled stands in for a cursor that has stopped moving. It skips the wait
// itself, which tea.Tick spends in a sleep; the tests that have to prove the
// wait was armed at all use armed instead.
func settled(m prview.Model, sha string) (prview.Model, tea.Cmd) {
	return m.Update(prview.CommitSettleMsg{SHA: sha})
}

// armed runs the wait a key produced, the way the runtime would, and returns
// what it carried. It blocks for commitSettleDelay, which is the price of
// driving the arming gate through a key rather than stepping around it.
func armed(t *testing.T, cmd tea.Cmd) tea.Msg {
	t.Helper()
	if cmd == nil {
		t.Fatal("the key armed no wait at all")
	}
	return cmd()
}

// key drives one keypress and hands back whatever it armed.
func key(m prview.Model, k string) (prview.Model, tea.Cmd) {
	return m.Update(tea.KeyPressMsg{Code: rune(k[0]), Text: k})
}

// commitDiff is a commit's diff the way the store hands one over.
func commitDiff(files []gh.ChangedFile) store.Files {
	return store.Files{Files: files, Status: store.StatusReady, Loaded: true}
}

func TestTheCommitColumnNamesEveryCommit(t *testing.T) {
	out := stripANSI(onCommits(160, 24).View())

	for _, want := range []string{
		"● Cap the backoff",
		"a3f91c2 · @drucial · 19h",
		"● Drop the count",
		"7b20ef4 · @nkr · 18h",
		"● Fix the typo",
		"c1d8a04 · Drew White · 17h",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the column is missing %q", want)
		}
	}

	// The count is on the strip now, which can say all four where a column can
	// only ever say its own.
	if !strings.Contains(out, "Commits") {
		t.Error("the column is not titled")
	}
}

// The headline gets the top line to itself. The sha reads as metadata, so it
// sits with the author and the age on the line under it.
func TestTheCommitHeadlineHasTheTopLineToItself(t *testing.T) {
	column := columnLines(onCommits(160, 24).View())

	at := -1
	for i, line := range column {
		if strings.Contains(line, "Cap the backoff") {
			at = i
			break
		}
	}
	if at < 0 {
		t.Fatal("the column never named the first commit")
	}

	if strings.Contains(column[at], "a3f91c2") {
		t.Errorf("the headline line is %q, want the sha off it", column[at])
	}
	if !strings.Contains(column[at+1], "a3f91c2") {
		t.Errorf("the line under the headline is %q, want the sha on it", column[at+1])
	}
}

func TestACommitWithNoAccountFallsBackToTheNameGitRecorded(t *testing.T) {
	out := stripANSI(onCommits(160, 24).View())

	if !strings.Contains(out, "Drew White · 17h") {
		t.Error("a commit with no GitHub account left its author blank")
	}
	if strings.Contains(out, "@Drew White") {
		t.Error("a git name was written as a handle")
	}
}

// The marker is the one cell the column spends on where a commit's checks got
// to, so the color is the whole of the signal.
func TestTheCheckMarkerTakesEachCommitsOwnState(t *testing.T) {
	out := onCommits(160, 24).View()

	th := theme.RosePineMoon
	for _, want := range []struct {
		name string
		seq  string
	}{
		{"passing", fgSeq(th.Success)},
		{"failing", fgSeq(th.Error)},
		{"running", fgSeq(th.Warning)},
	} {
		if !marked(out, want.seq) {
			t.Errorf("no %s commit marker in the column", want.name)
		}
	}
}

// marked reports whether a dot is painted in a foreground. The selected row
// carries a background in the same sequence, so the color is not always the
// last thing before the m.
func marked(frame, fg string) bool {
	return regexp.MustCompile(regexp.QuoteMeta(fg) + `(;[0-9;]+)?m●`).MatchString(frame)
}

func TestTheCursorStoppingAsksForTheCommitsDiff(t *testing.T) {
	m := press(onCommits(160, 24), "1", "j")

	_, cmd := settled(m, "7b20ef4a11")
	if cmd == nil {
		t.Fatal("the cursor settling produced no command, want a request for the diff")
	}

	msg, ok := cmd().(prview.NeedCommitMsg)
	if !ok {
		t.Fatalf("the cursor settling produced %T, want a NeedCommitMsg", cmd())
	}
	if msg.SHA != "7b20ef4a11" {
		t.Errorf("asked for %q, want the commit under the cursor", msg.SHA)
	}
}

func TestSettlingOnTheCommitAlreadyShowingAsksAgainForNothing(t *testing.T) {
	m, _ := settled(onCommits(160, 24), "a3f91c2d5e")
	m.SetCommitFiles("a3f91c2d5e", commitDiff(sampleFiles()))

	if _, cmd := settled(m, "a3f91c2d5e"); cmd != nil {
		t.Error("the commit already showing was asked for a second time")
	}
}

// The pane holds the commit it is showing until the store answers with the next
// one. A cached diff answers inside a frame, and clearing the pane to meet it
// puts a spinner on screen over a wait that never happened.
func TestAskingForACommitLeavesTheOneOnScreenAlone(t *testing.T) {
	m, _ := settled(onCommits(160, 24), "a3f91c2d5e")
	m.SetCommitFiles("a3f91c2d5e", commitDiff(sampleFiles()))

	m, _ = settled(press(m, "j"), "7b20ef4a11")

	out := stripANSI(m.View())
	if strings.Contains(out, "Loading the diff") {
		t.Error("the pane spun before the store had been asked")
	}
	if !strings.Contains(out, "internal/gh/client.go") {
		t.Error("the pane dropped the diff it was showing")
	}
}

// A commit that really is being fetched still spins. The store answers a request
// that is out as well as one it holds, and the loading state it sends is what
// the pane takes.
func TestACommitBeingFetchedSpins(t *testing.T) {
	m, _ := settled(onCommits(160, 24), "a3f91c2d5e")
	m.SetCommitFiles("a3f91c2d5e", store.Files{Status: store.StatusLoading})

	if !strings.Contains(stripANSI(m.View()), "Loading the diff") {
		t.Error("a commit with its request still out does not say so")
	}
}

// Every keypress arms a wait of its own, so walking three commits sets three of
// them. Only the one still naming the commit under the cursor may fetch: the
// rest are a branch the reader passed through on the way here.
func TestOnlyTheCommitTheCursorStoppedOnIsAskedFor(t *testing.T) {
	m := press(onCommits(160, 24), "1", "j", "j", "k")

	for _, stale := range []string{"a3f91c2d5e", "c1d8a04bb9"} {
		if _, cmd := settled(m, stale); cmd != nil {
			t.Errorf("a wait armed on %q fetched after the cursor moved off it", stale)
		}
	}

	if _, cmd := settled(m, "7b20ef4a11"); cmd == nil {
		t.Error("the commit the cursor stopped on was never asked for")
	}
}

// The cursor moving arms a wait naming the commit it landed on. Driven through
// the key and its command rather than by handing the model the message, which
// is the only way the arming gate is exercised at all.
func TestMovingTheCursorArmsTheWaitForThatCommit(t *testing.T) {
	m, cmd := key(press(onCommits(160, 24), "1"), "j")

	msg, ok := armed(t, cmd).(prview.CommitSettleMsg)
	if !ok {
		t.Fatalf("the key armed %T, want a CommitSettleMsg", armed(t, cmd))
	}
	if msg.SHA != "7b20ef4a11" {
		t.Errorf("the wait names %q, want the commit the cursor landed on", msg.SHA)
	}
	if _, cmd := settled(m, msg.SHA); cmd == nil {
		t.Error("the wait it armed asked for nothing")
	}
}

// A wait armed on the way through the Commits tab runs out wherever the reader
// got to. Tabbing on within the settle window is one keypress at ordinary key
// repeat, and fetching then spends the request the whole debounce exists to save.
func TestAWaitThatRunsOutOnAnotherTabAsksForNothing(t *testing.T) {
	m := press(onCommits(160, 24), "1", "j")
	m = press(m, "]")

	if _, cmd := settled(m, "7b20ef4a11"); cmd != nil {
		t.Error("a wait that ran out after the reader left the tab fetched anyway")
	}
}

// A failed diff has no key that selects it any more, so the wait has to arm on
// the commit already showing. On a one-commit branch there is nowhere to walk
// to and back, and without this the error stays until the screen is closed.
func TestAFailedCommitArmsARetryWithNowhereToWalk(t *testing.T) {
	d := sampleDetail()
	d.Commits = sampleCommits()[:1]

	m := press(detailed(held(d), 160, 24), "]", "1")
	m, _ = settled(m, "a3f91c2d5e")
	m.SetCommitFiles("a3f91c2d5e", store.Files{Status: store.StatusFailed, Err: errors.New("no such host")})

	m, cmd := key(m, "j")
	msg, ok := armed(t, cmd).(prview.CommitSettleMsg)
	if !ok || msg.SHA != "a3f91c2d5e" {
		t.Fatalf("the key armed %v, want a retry of the failed commit", armed(t, cmd))
	}
	if _, cmd := settled(m, msg.SHA); cmd == nil {
		t.Error("the retry asked for nothing")
	}
}

// The tab can be opened before the detail query answers. The commits arrive
// with nothing armed to fetch the first one, so the arriving detail arms it.
func TestCommitsArrivingAfterTheTabArmTheirOwnFetch(t *testing.T) {
	d := sampleDetail()
	d.Commits = sampleCommits()

	m := press(detailed(store.Detail{Status: store.StatusLoading}, 160, 24), "]")
	cmd := m.SetDetail(held(d))

	msg, ok := armed(t, cmd).(prview.CommitSettleMsg)
	if !ok || msg.SHA != "a3f91c2d5e" {
		t.Fatalf("the arriving detail armed %v, want the first commit", armed(t, cmd))
	}
}

// Nothing painted yet is a wait, not an empty pane. A column full of commits
// beside a blank pane reads as a rendering fault rather than as a diff coming.
func TestThePaneSpinsThroughTheSettleWindow(t *testing.T) {
	out := stripANSI(onCommits(160, 24).View())

	if !strings.Contains(out, "Loading the diff") {
		t.Error("the pane sits blank while the wait runs")
	}
}

// One viewport serves all four tabs. A commit answering after the reader has
// tabbed on still takes the pane, but it must not scroll it: the offset it
// would reset belongs to whatever they are reading now.
func TestACommitLandingOffTabKeepsTheReadersPlace(t *testing.T) {
	m, _ := settled(onCommits(160, 40), "a3f91c2d5e")
	m = press(m, "[")
	m = press(m, "j", "j", "j", "j", "j", "j")

	before := m.View()
	m.SetCommitFiles("a3f91c2d5e", commitDiff(sampleFiles()))

	if m.View() != before {
		t.Error("a commit landing off the tab moved the pane the reader was on")
	}
}

// A retry asks for the commit already showing, so the answer never takes the
// pane. Clearing pending only on a take would latch it there and swallow every
// retry after the first.
func TestASecondRetryOfAFailedCommitStillAsks(t *testing.T) {
	d := sampleDetail()
	d.Commits = sampleCommits()[:1]
	failed := store.Files{Status: store.StatusFailed, Err: errors.New("no such host")}

	m := press(detailed(held(d), 160, 24), "]", "1")
	m, _ = settled(m, "a3f91c2d5e")
	m.SetCommitFiles("a3f91c2d5e", failed)

	m, cmd := settled(m, "a3f91c2d5e")
	if cmd == nil {
		t.Fatal("the first retry asked for nothing")
	}
	m.SetCommitFiles("a3f91c2d5e", store.Files{Status: store.StatusLoading})
	m.SetCommitFiles("a3f91c2d5e", failed)

	if _, cmd := settled(m, "a3f91c2d5e"); cmd == nil {
		t.Error("the second retry was swallowed: pending latched on the first")
	}
}

// A diff answering for a commit the reader has walked back off must not paint:
// the card would name one commit while the column highlights another.
func TestADiffThatLandsAfterTheCursorWalksBackIsDropped(t *testing.T) {
	m, _ := settled(onCommits(160, 24), "a3f91c2d5e")
	m.SetCommitFiles("a3f91c2d5e", commitDiff(sampleFiles()))

	// Down to the second, ask for it, then back up before it answers.
	m, _ = settled(press(m, "j"), "7b20ef4a11")
	m = press(m, "k")
	m.SetCommitFiles("7b20ef4a11", commitDiff(sampleFiles()))

	// The full sha is the tell: the card spells it out, the column has room
	// only for the short one.
	out := stripANSI(m.View())
	if strings.Contains(out, "7b20ef4a11") {
		t.Error("the card names a commit the column is not pointing at")
	}
	if !strings.Contains(out, "a3f91c2d5e") {
		t.Error("the pane lost the commit the cursor is actually on")
	}
}

func TestTheCommitDiffRendersThroughTheFilesViewer(t *testing.T) {
	m, _ := settled(onCommits(160, 30), "a3f91c2d5e")
	m.SetCommitFiles("a3f91c2d5e", commitDiff(sampleFiles()))

	out := stripANSI(m.View())
	if !strings.Contains(out, "internal/gh/client.go") {
		t.Error("the commit's diff did not render its file heading")
	}
	if !strings.Contains(out, "delay = min(delay*2, fetchTimeout)") {
		t.Error("the commit's diff did not render its code")
	}
}

// A diff for a commit the cursor has moved on from must not land on the screen:
// the reader asked for a different one and is waiting on it.
func TestADiffForAnotherCommitIsDropped(t *testing.T) {
	m, _ := settled(onCommits(160, 24), "a3f91c2d5e")
	m.SetCommitFiles("7b20ef4a11", commitDiff(sampleFiles()))

	if strings.Contains(stripANSI(m.View()), "internal/gh/client.go") {
		t.Error("a diff for a commit that is not selected rendered anyway")
	}
}

func TestTheCommitDiffStatesReadAsThemselves(t *testing.T) {
	cases := []struct {
		name string
		held store.Files
		want string
	}{
		{name: "loading", held: store.Files{Status: store.StatusLoading}, want: "Loading the diff"},
		{name: "failed", held: store.Files{Status: store.StatusFailed, Err: errors.New("no such host")},
			want: "Could not load the diff: no such host"},
		{name: "empty", held: commitDiff(nil), want: "No files changed."},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m, _ := settled(onCommits(160, 24), "a3f91c2d5e")
			m.SetCommitFiles("a3f91c2d5e", c.held)

			if out := stripANSI(m.View()); !strings.Contains(out, c.want) {
				t.Errorf("the diff pane does not say %q", c.want)
			}
		})
	}
}

// The tab opens on content the way Files does, so landing on it asks for the
// commit the cursor is already pointing at.
func TestTheCommitsTabAsksForItsFirstDiffOnTheWayIn(t *testing.T) {
	d := sampleDetail()
	d.Commits = sampleCommits()

	m, _ := settled(press(detailed(held(d), 160, 24), "]"), "a3f91c2d5e")
	m.SetCommitFiles("a3f91c2d5e", commitDiff(sampleFiles()))

	if !strings.Contains(stripANSI(m.View()), "internal/gh/client.go") {
		t.Error("the tab opened without asking for the first commit's diff")
	}
}

// A pull request with no commits has nothing to point at. The column says so,
// and the pane beside it stays empty rather than saying it twice.
func TestAnEmptyCommitListLeavesThePaneEmpty(t *testing.T) {
	m := press(detailed(held(sampleDetail()), 160, 24), "]")

	out := stripANSI(m.View())
	if !strings.Contains(out, "No commits.") {
		t.Error("the column does not say the branch is empty")
	}
	if strings.Contains(out, "diff") {
		t.Error("the pane beside an empty column has something to say about a diff")
	}
}

// Every styled run ends in a reset that clears the background with it, so a row
// painted as one string would carry its selection only as far as the first
// token. Both lines of the row have to hold it the whole way across.
func TestTheSelectedCommitIsPaintedCellByCellAcrossBothLines(t *testing.T) {
	m := onCommits(160, 24)
	seq := bgSeq(theme.RosePineMoon.SelectedBackground)

	var painted []string
	for _, line := range strings.Split(m.View(), "\n") {
		if strings.Contains(line, seq) {
			painted = append(painted, line)
		}
	}

	if len(painted) != 2 {
		t.Fatalf("%d lines carry the selection, want the two of one row", len(painted))
	}
	for i, line := range painted {
		if count := strings.Count(line, seq); count < 2 {
			t.Errorf("line %d paints the selection %d times, want it cell by cell", i, count)
		}
	}
}

// The cursor walks rows, and a row is two lines. An offset that lands between
// them opens the column on a row's second line with its sha cut off above.
//
// The odd height is the one that catches it: an even one lands on a boundary by
// accident. The list runs past the window on both so the scroll is a real one
// rather than a clamp to the end, which lands on a boundary by accident too.
//
// The heights are the frame's, and the header takes five rows off the top of
// it, so these are the pane heights that were 6 and 7.
func TestTheCommitCursorScrollsAWholeRowAtATime(t *testing.T) {
	for _, height := range []int{11, 12} {
		t.Run(strconv.Itoa(height), func(t *testing.T) {
			d := sampleDetail()
			d.Commits = append(sampleCommits(), sampleCommits()...)
			m := press(detailed(held(d), 160, height), "]", "1", "j", "j")

			column := columnLines(m.View())
			if len(column) < 4 {
				t.Fatalf("the column rendered %d lines, want two whole rows", len(column))
			}

			// The window opens on a row's first line, not the meta line under it.
			if !strings.Contains(column[0], "Drop the count") {
				t.Errorf("the column opens on %q, want the top of a row", column[0])
			}
			if !strings.Contains(column[1], "7b20ef4") {
				t.Errorf("the second line is %q, want the sha under its headline", column[1])
			}
			if !strings.Contains(column[2], "Fix the typo") || !strings.Contains(column[3], "Drew White") {
				t.Error("the cursor's row is not on screen whole")
			}
		})
	}
}

// Review threads are written against the pull request's head. The same line
// number in an older commit is different code, so a commit's diff hangs none of
// them: a comment about the final diff under a line it was never about reads as
// a comment about that line.
func TestACommitDiffCarriesNoReviewThreads(t *testing.T) {
	m, _ := settled(onCommits(160, 40), "a3f91c2d5e")
	m.SetCommitFiles("a3f91c2d5e", commitDiff(sampleFiles()))

	out := stripANSI(m.View())
	for _, gone := range []string{"This backs off forever.", "Typo.", "Fixed."} {
		if strings.Contains(out, gone) {
			t.Errorf("the commit's diff carries the review comment %q", gone)
		}
	}
}

// The column has room for a short sha and a headline. Everything else about the
// commit goes above its diff, where there is width for it.
func TestTheSelectedCommitIsNamedAboveItsDiff(t *testing.T) {
	d := sampleDetail()
	d.Commits = sampleCommits()
	d.Commits[0].Body = "The retry loop had no ceiling, so a dead endpoint backed off forever."

	m, _ := settled(press(detailed(held(d), 160, 40), "]"), "a3f91c2d5e")
	m.SetCommitFiles("a3f91c2d5e", commitDiff(sampleFiles()))
	out := stripANSI(m.View())

	for _, want := range []string{
		"Cap the backoff",
		"The retry loop had no ceiling",
		"a3f91c2d5e",
		"@drucial · 19h",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the card is missing %q", want)
		}
	}

	card := strings.Index(out, "The retry loop had no ceiling")
	first := strings.Index(out, "internal/gh/client.go")
	if card < 0 || first < 0 || card > first {
		t.Error("the card is not above the first file")
	}
}

// A commit written with no body is its headline alone. The card still carries
// the sha and the author, which is what the column could not fit.
func TestTheCardHoldsUpWithNoMessageBody(t *testing.T) {
	m, _ := settled(onCommits(160, 40), "a3f91c2d5e")
	m.SetCommitFiles("a3f91c2d5e", commitDiff(sampleFiles()))

	out := stripANSI(m.View())
	if !strings.Contains(out, "a3f91c2d5e") {
		t.Error("the card lost the full sha")
	}
	if !strings.Contains(out, "internal/gh/client.go") {
		t.Error("the diff under the card is gone")
	}
}

// The cursor walks rows and a row is two lines, so a page of the column is half
// the lines the pane holds. Paged by the line count instead, every press clears
// a screenful of commits the reader never sees.
func TestPagingTheCommitColumnMovesByRows(t *testing.T) {
	d := sampleDetail()
	d.Commits = manyCommits(40)
	m := press(detailed(held(d), 160, 24), "]", "1")

	before := shownHeadlines(m.View())
	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyPgDown})
	after := shownHeadlines(m.View())

	if len(before) == 0 || len(after) == 0 {
		t.Fatalf("the column showed %d rows then %d", len(before), len(after))
	}
	if before[0] == after[0] {
		t.Fatal("page down did not move the column")
	}

	// A page moves the window by what it holds. Anything more and the commits
	// in between never appear on screen at all.
	if at := indexOf(before, after[0]); at < 0 {
		t.Errorf("the window jumped from %q to %q, skipping every commit between",
			before[len(before)-1], after[0])
	}
}

// manyCommits is a branch long enough to scroll, each row telling itself apart
// from the rest.
func manyCommits(n int) []gh.Commit {
	out := make([]gh.Commit, 0, n)
	for i := range n {
		out = append(out, gh.Commit{
			SHA:         fmt.Sprintf("%010d", i),
			Short:       fmt.Sprintf("%07d", i),
			Headline:    "Commit number " + strconv.Itoa(i),
			Author:      gh.Actor{Login: "drucial"},
			CommittedAt: time.Now().Add(-time.Duration(n-i) * time.Hour),
		})
	}
	return out
}

// shownHeadlines is the commit headlines on screen, in order.
func shownHeadlines(frame string) []string {
	var out []string
	for i, line := range columnLines(frame) {
		if i%2 == 0 && strings.TrimSpace(line) != "" {
			out = append(out, strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "●")))
		}
	}
	return out
}

func indexOf(lines []string, want string) int {
	for i, line := range lines {
		if line == want {
			return i
		}
	}
	return -1
}

// One viewport serves the file column and the commit column, and their rows are
// different heights. An offset the tree left behind opens the commit column on
// a row's second line, with its headline cut off above the window.
func TestSwitchingTabsOpensTheCommitColumnOnARow(t *testing.T) {
	for _, height := range []int{9, 10, 11, 12, 13} {
		t.Run(strconv.Itoa(height), func(t *testing.T) {
			d := sampleDetail()
			d.Commits = manyCommits(40)

			m := detailed(held(d), 160, height)
			m.SetFiles(store.Files{Files: sampleFiles(), Status: store.StatusReady, Loaded: true})

			// Into the file tree, down it far enough to scroll, then round to
			// Commits.
			m = press(m, "]", "]", "]", "1")
			for range 9 {
				m = press(m, "j")
			}
			m = press(m, "]", "]")

			lines := columnLines(m.View())
			if len(lines) == 0 {
				t.Fatal("the commit column rendered nothing")
			}
			if !strings.Contains(lines[0], "●") {
				t.Errorf("the column opens on %q, want the top of a row", lines[0])
			}
		})
	}
}

// The column drives the diff beside it, so it takes focus on the way in rather
// than making the reader ask for it first.
func TestTheCommitsTabOpensWithTheColumnFocused(t *testing.T) {
	d := sampleDetail()
	d.Commits = sampleCommits()

	// j moves the cursor when the column has focus, and scrolls the pane beside
	// it when it does not. Which commit settles is the tell.
	_, cmd := settled(press(detailed(held(d), 160, 24), "]", "j"), d.Commits[1].SHA)
	if cmd == nil {
		t.Fatal("no diff was asked for, so j never reached the column")
	}

	msg, ok := cmd().(prview.NeedCommitMsg)
	if !ok {
		t.Fatalf("settling yielded %T, want a request for a commit's diff", cmd())
	}
	if msg.SHA != d.Commits[1].SHA {
		t.Errorf("asked for %q, want the second commit", msg.SHA)
	}
}

func TestBraceWalksTheFilesInACommitDiff(t *testing.T) {
	m, _ := settled(onCommits(160, 12), "a3f91c2d5e")
	m.SetCommitFiles("a3f91c2d5e", commitDiff(sampleFiles()))

	// The first } lands on the first file, since the pane opens on the blank
	// line above it. The second is the one that moves a file.
	first := stripANSI(press(m, "}").View())
	second := stripANSI(press(m, "}", "}").View())
	if first == second {
		t.Fatal("} did not move the commit's diff a file on")
	}

	if back := stripANSI(press(m, "}", "}", "{").View()); back != first {
		t.Error("{ did not come back to the file } left")
	}
}

func TestTheRailIsOffOnTheCommitsTab(t *testing.T) {
	for _, width := range []int{200, 160, 120} {
		if strings.Contains(stripANSI(onCommits(width, 24).View()), "Reviewers") {
			t.Errorf("the rail is on screen at %d columns", width)
		}
	}
}

// The column narrows before it goes, and goes at the width the pane beside it
// stops fitting its own tab strip. Below that the two of them render wider than
// the terminal they were handed.
func TestTheCommitColumnHidesOnANarrowFrame(t *testing.T) {
	for _, width := range []int{160, 100, 70} {
		if !strings.Contains(stripANSI(onCommits(width, 24).View()), "a3f91c2") {
			t.Errorf("the column is gone at %d columns", width)
		}
	}
	for _, width := range []int{69, 60, 40, 20} {
		if strings.Contains(stripANSI(onCommits(width, 24).View()), "a3f91c2") {
			t.Errorf("the column is still on screen at %d columns", width)
		}
	}
}

// The column opens on a row rather than between two. A window that holds an odd
// number of lines is the one that catches it: the offset the cursor asks for at
// the end of the list is one the viewport clamps back off the boundary, and the
// row on the top line loses its headline above the window.
func TestTheCommitColumnOpensOnAWholeRow(t *testing.T) {
	for _, height := range []int{10, 11, 12, 13} {
		t.Run(strconv.Itoa(height), func(t *testing.T) {
			d := sampleDetail()
			d.Commits = append(sampleCommits(), sampleCommits()...)
			m := press(detailed(held(d), 160, height), "]", "1", "G")

			lines := columnLines(m.View())
			if len(lines) < 2 {
				t.Fatalf("the column rendered %d lines, want a row", len(lines))
			}
			for i, line := range lines {
				// An odd window holds a whole number of rows and a spare line,
				// which the pane pads out under them.
				if strings.TrimSpace(line) == "" {
					break
				}
				if headline := strings.Contains(line, "●"); headline != (i%2 == 0) {
					t.Errorf("line %d is %q, want the column on a row boundary", i, line)
				}
			}
		})
	}
}

func TestTheFrameFillsItsSizeExactlyOnTheCommitsTab(t *testing.T) {
	sizes := []struct{ width, height int }{
		{width: 200, height: 40},
		{width: 160, height: 24},
		{width: 100, height: 20},
		{width: 60, height: 10},
		{width: 40, height: 10},
		{width: 30, height: 12},
		{width: 20, height: 8},
	}

	for _, size := range sizes {
		t.Run(fmt.Sprintf("%dx%d", size.width, size.height), func(t *testing.T) {
			m, _ := settled(onCommits(size.width, size.height), "a3f91c2d5e")
			m.SetCommitFiles("a3f91c2d5e", commitDiff(sampleFiles()))

			lines := strings.Split(m.View(), "\n")
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

// A run of pushes is headed by its count and then spelled out, one commit to a
// row. The header alone says a branch moved without saying what landed on it.
func TestARunOfPushesNamesEveryCommitUnderIt(t *testing.T) {
	run := sampleCommits()
	run[1].Author = run[0].Author

	d := sampleDetail()
	d.Commits = run
	d.Timeline = []gh.TimelineItem{
		commented("nkr", time.Now().Add(-20*time.Hour), "Looks close."),
		commitItem(run[0]),
		commitItem(run[1]),
	}

	out := stripANSI(detailed(held(d), 160, 30).View())
	if !strings.Contains(out, "drucial · pushed 2 commits · 18h") {
		t.Error("the run is not headed by its count")
	}
	for _, want := range []string{
		"a3f91c2  Cap the backoff",
		"7b20ef4  Drop the count",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the run is missing %q", want)
		}
	}

	// The run sits under its own header, not above it.
	head := strings.Index(out, "pushed 2 commits")
	first := strings.Index(out, "a3f91c2  Cap the backoff")
	if head < 0 || first < 0 || head > first {
		t.Error("the commits are not under the line that counts them")
	}
}

// A lone push already names its commit on the header line, so a row under it
// would say the same thing twice.
func TestALonePushHasNoRowUnderIt(t *testing.T) {
	d := sampleDetail()
	d.Commits = sampleCommits()[:1]
	d.Timeline = []gh.TimelineItem{commitItem(d.Commits[0])}

	out := stripANSI(detailed(held(d), 160, 30).View())
	if strings.Count(out, "a3f91c2") != 1 {
		t.Errorf("a lone push named its sha %d times, want once", strings.Count(out, "a3f91c2"))
	}
}

func TestALonePushNamesItsShaAndHeadline(t *testing.T) {
	d := sampleDetail()
	d.Commits = sampleCommits()[:1]
	d.Timeline = []gh.TimelineItem{commitItem(d.Commits[0])}

	out := stripANSI(detailed(held(d), 160, 30).View())
	if !strings.Contains(out, "drucial · pushed a3f91c2 Cap the backoff · 19h") {
		t.Error("a lone push did not name its commit")
	}
}

// Crediting one person for someone else's commits is worse than crediting
// nobody, so a mixed run drops the name and keeps the count.
func TestARunByMoreThanOnePersonNamesNobody(t *testing.T) {
	d := sampleDetail()
	d.Commits = sampleCommits()
	d.Timeline = []gh.TimelineItem{commitItem(d.Commits[0]), commitItem(d.Commits[1])}

	out := stripANSI(detailed(held(d), 160, 30).View())
	if !strings.Contains(out, "● pushed 2 commits") {
		t.Error("a mixed run did not drop the author")
	}
	if strings.Contains(out, "drucial · pushed") || strings.Contains(out, "nkr · pushed") {
		t.Error("a mixed run credited one of its authors with the lot")
	}
}

func commitItem(c gh.Commit) gh.TimelineItem {
	return gh.TimelineItem{
		Kind:      gh.TimelineCommit,
		Actor:     c.Author,
		CreatedAt: c.CommittedAt,
		Commit:    &c,
	}
}

// columnLines is the left column's rows, with the borders and the pane beside
// it cut away.
func columnLines(frame string) []string {
	var out []string
	for _, line := range strings.Split(stripANSI(frame), "\n") {
		cells := []rune(line)
		if len(cells) < 2 || cells[0] != '│' {
			continue
		}
		// Indexed by rune rather than by byte: the rows carry marks and dots
		// that run to three bytes, and a byte offset lands past the border.
		for i, r := range cells[1:] {
			if r == '│' {
				out = append(out, string(cells[1:1+i]))
				break
			}
		}
	}
	return out
}

// The row cursor belongs to the Files tab. A commit's diff draws through the
// same renderer, and a bar there would point at a row no key can act on.
func TestTheCommitDiffTakesNoRowCursor(t *testing.T) {
	m, _ := settled(onCommits(200, 24), "a3f91c2d5e")
	m.SetCommitFiles("a3f91c2d5e", commitDiff(sampleFiles()))
	m = press(m, "j", "j", "j")

	if !strings.Contains(stripANSI(m.View()), "delay = min(delay*2, fetchTimeout)") {
		t.Fatal("setup: the commit's diff is not on the screen")
	}
	if got := barredRow(m.View()); got != "" {
		t.Errorf("the commit diff barred %q, want no cursor at all", got)
	}
}

// The split is the body's mode and not the model's. The Commits tab draws every
// file unified whatever the Files tab was left on, and a heading indented to a
// half's source column sits three cells left of the code it introduces.
func TestTheCommitsHeadingKeepsItsIndentWhileFilesIsSplit(t *testing.T) {
	heading := func(split bool) string {
		t.Helper()

		d := sampleDetail()
		d.Commits = sampleCommits()
		m := detailed(held(d), 160, 40)
		m.SetFiles(loadedFiles(sampleFiles(), 0))

		m = press(m, "]", "]", "]")
		if split {
			m = press(m, "|")
		}
		m = press(m, "[", "[")

		m, _ = settled(m, "a3f91c2d5e")
		m.SetCommitFiles("a3f91c2d5e", commitDiff(sampleFiles()))
		m, _ = settled(m, "a3f91c2d5e")

		for _, line := range strings.Split(stripANSI(m.View()), "\n") {
			if strings.Contains(line, "@@ -40,4 +40,5 @@") {
				return strings.TrimRight(line, " │")
			}
		}
		t.Fatal("the commit's diff drew no @@ heading")
		return ""
	}

	if on, off := heading(true), heading(false); on != off {
		t.Errorf("the Commits heading moved because Files is split:\n split %q\n plain %q", on, off)
	}
}
