package prview_test

import (
	"errors"
	"fmt"
	"image/color"
	"slices"
	"strconv"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/praxis-labs-io/zen-octo/internal/gh"
	"github.com/praxis-labs-io/zen-octo/internal/store"
	"github.com/praxis-labs-io/zen-octo/internal/tui/prview"
	"github.com/praxis-labs-io/zen-octo/internal/tui/theme"
)

// sampleFiles covers what the tab has to tell apart: nesting deep enough to
// fold, a rename, a file with no patch, and the lines sampleDetail's two review
// threads anchor to, one on each side of the diff.
func sampleFiles() []gh.ChangedFile {
	return []gh.ChangedFile{
		{
			Path: "internal/gh/client.go", Status: gh.FileModified, Additions: 2, Deletions: 1,
			Hunks: []gh.Hunk{{
				Header: "@@ -40,4 +40,5 @@ func New() (*Client, error) {",
				Lines: []gh.DiffLine{
					{Kind: gh.DiffContext, Old: 40, New: 40, Content: "\tfor {"},
					{Kind: gh.DiffRemoved, Old: 41, Content: "\t\ttime.Sleep(delay)"},
					{Kind: gh.DiffAdded, New: 41, Content: "\t\tdelay = min(delay*2, fetchTimeout)"},
					{Kind: gh.DiffAdded, New: 42, Content: "\t\ttime.Sleep(delay)"},
					{Kind: gh.DiffContext, Old: 42, New: 43, Content: "\t}"},
				},
			}},
		},
		{
			Path: "internal/store/store.go", Status: gh.FileModified, Additions: 0, Deletions: 1,
			Hunks: []gh.Hunk{{
				Header: "@@ -87,3 +87,2 @@",
				Lines: []gh.DiffLine{
					{Kind: gh.DiffContext, Old: 87, New: 87, Content: "// Begin marks one section in flight."},
					{Kind: gh.DiffRemoved, Old: 88, Content: "// It refuces a duplicate."},
					{Kind: gh.DiffContext, Old: 89, New: 88, Content: "}"},
				},
			}},
		},
		{
			Path: "internal/tui/prview/files.go", PreviousPath: "internal/tui/prview/diff.go",
			Status: gh.FileRenamed, Additions: 1, Deletions: 0,
			Hunks: []gh.Hunk{{
				Header: "@@ -1,2 +1,3 @@",
				Lines: []gh.DiffLine{
					{Kind: gh.DiffContext, Old: 1, New: 1, Content: "package prview"},
					{Kind: gh.DiffAdded, New: 2, Content: "const tabWidth = 4"},
				},
			}},
		},
		{
			Path: "docs/screenshot.png", Status: gh.FileModified,
			Omitted: "binary, or too large for GitHub to return a diff",
		},
	}
}

func loadedFiles(files []gh.ChangedFile, more int) store.Files {
	return store.Files{Files: files, MoreFiles: more, Status: store.StatusReady, Loaded: true}
}

// onFiles is the screen with a diff, sitting on the Files tab.
func onFiles(width, height int) prview.Model {
	m := detailed(held(sampleDetail()), width, height)
	m.SetFiles(loadedFiles(sampleFiles(), 0))
	return press(m, "]", "]", "]")
}

// The diff is rendered once and kept, so the two queries landing in either
// order have to reach the screen. The threads come from the detail and the
// lines they hang off from the diff.
func TestThreadsLandingAfterTheDiffStillRender(t *testing.T) {
	m := prview.New(theme.RosePineMoon, samplePR(), prview.RailPreference{}, colorizer())
	m.SetSize(200, 60)
	m.SetFiles(loadedFiles(sampleFiles(), 0))
	m = press(m, "]", "]", "]")

	if strings.Contains(stripANSI(m.View()), "This backs off forever.") {
		t.Fatal("setup: the thread is on screen before the detail landed")
	}

	m.SetDetail(held(sampleDetail()))
	if !strings.Contains(stripANSI(m.View()), "This backs off forever.") {
		t.Error("a thread arriving after the diff never reaches the screen")
	}
}

func TestTheFilesTabRendersTheDiff(t *testing.T) {
	out := stripANSI(onFiles(200, 50).View())

	for _, want := range []string{
		"internal/gh/client.go",
		"@@ -40,4 +40,5 @@ func New() (*Client, error) {",
		"delay = min(delay*2, fetchTimeout)",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the Files tab does not show %q", want)
		}
	}
}

func TestTheFileTreeRendersViewedStateGlyphs(t *testing.T) {
	files := []gh.ChangedFile{
		{Path: "a.go", Viewed: gh.FileUnviewed},
		{Path: "b.go", Viewed: gh.FileDismissed},
		{Path: "c.go", Viewed: gh.FileViewed},
	}
	m := detailed(held(sampleDetail()), 120, 30)
	m.SetFiles(loadedFiles(files, 0))
	out := stripANSI(press(m, "]", "]", "]").View())
	for _, want := range []string{"○ a.go", "⊙ b.go", "● c.go"} {
		if !strings.Contains(out, want) {
			t.Errorf("tree does not contain %q", want)
		}
	}
}

func TestMarkViewedTargetsTheTreeSelectionAndTheShownDiff(t *testing.T) {
	files := []gh.ChangedFile{
		{Path: "a.go", Viewed: gh.FileUnviewed},
		{Path: "b.go", Viewed: gh.FileViewed},
	}
	m := detailed(held(sampleDetail()), 120, 30)
	m.SetFiles(loadedFiles(files, 0))
	m = press(m, "]", "]", "]")

	m, cmd := m.Update(tea.KeyPressMsg{Code: 'm', Text: "m"})
	if got := cmd(); got != (prview.ToggleFileViewedMsg{ID: "PR_412", Path: "a.go", Viewed: true}) {
		t.Errorf("diff toggle = %#v", got)
	}
	if got := diffHeads(m.View()); !strings.Contains(got, "b.go") {
		t.Errorf("marking from the diff advanced to %q, want b.go", got)
	}

	m = press(m, "1")
	_, cmd = m.Update(tea.KeyPressMsg{Code: 'm', Text: "m"})
	if got := cmd(); got != (prview.ToggleFileViewedMsg{ID: "PR_412", Path: "b.go", Viewed: false}) {
		t.Errorf("tree toggle = %#v", got)
	}
}

func TestMarkViewedAdvancesTheTreeAndStopsAtTheLastFile(t *testing.T) {
	files := []gh.ChangedFile{
		{Path: "a.go", Viewed: gh.FileUnviewed},
		{Path: "b.go", Viewed: gh.FileDismissed},
	}
	m := detailed(held(sampleDetail()), 120, 30)
	m.SetFiles(loadedFiles(files, 0))
	m = press(m, "]", "]", "]", "1")

	m, cmd := m.Update(tea.KeyPressMsg{Code: 'm', Text: "m"})
	if got := cmd(); got != (prview.ToggleFileViewedMsg{ID: "PR_412", Path: "a.go", Viewed: true}) {
		t.Fatalf("first toggle = %#v", got)
	}
	m, cmd = m.Update(tea.KeyPressMsg{Code: 'm', Text: "m"})
	if got := cmd(); got != (prview.ToggleFileViewedMsg{ID: "PR_412", Path: "b.go", Viewed: true}) {
		t.Errorf("toggle after advance = %#v", got)
	}
	_, cmd = m.Update(tea.KeyPressMsg{Code: 'm', Text: "m"})
	if got := cmd(); got != (prview.ToggleFileViewedMsg{ID: "PR_412", Path: "b.go", Viewed: true}) {
		t.Errorf("toggle at the end = %#v", got)
	}
}

func TestMarkViewedFromTheDiffAdvancesFromTheShownFile(t *testing.T) {
	files := []gh.ChangedFile{
		{Path: "dir/a.go", Viewed: gh.FileUnviewed},
		{Path: "z.go", Viewed: gh.FileUnviewed},
	}
	m := detailed(held(sampleDetail()), 120, 30)
	m.SetFiles(loadedFiles(files, 0))
	m = press(m, "]", "]", "]", "1", "k", "2")

	m, cmd := m.Update(tea.KeyPressMsg{Code: 'm', Text: "m"})
	if got := cmd(); got != (prview.ToggleFileViewedMsg{ID: "PR_412", Path: "dir/a.go", Viewed: true}) {
		t.Fatalf("toggle = %#v", got)
	}
	if got := diffHeads(m.View()); !strings.Contains(got, "z.go") {
		t.Errorf("marking from the diff advanced to %q, want z.go", got)
	}
}

// The tab opens on something to read. A binary file has no body, and opening on
// one shows a reader an empty pane and a note.
func TestTheFilesTabOpensOnAFileWithADiff(t *testing.T) {
	if got := diffHeads(onFiles(200, 50).View()); !strings.Contains(got, "internal/gh/client.go") {
		t.Errorf("the tab opened on %q, want the first file with hunks", got)
	}
}

func TestAFileWithNoBodySaysWhyWhenItIsReached(t *testing.T) {
	m := showFile(t, onFiles(200, 50), "docs/screenshot.png")
	if !strings.Contains(stripANSI(m.View()), "binary, or too large for GitHub to return a diff") {
		t.Error("the omitted body is not explained")
	}
}

// The file column is what navigation hangs off, and a flat list of full paths
// is not a tree.
func TestTheFileColumnNestsThePathsAndFoldsASingleChildRun(t *testing.T) {
	out := stripANSI(onFiles(200, 50).View())

	for _, want := range []string{"internal/", "gh/", "store/", "tui/prview/", "docs/"} {
		if !strings.Contains(out, want) {
			t.Errorf("the tree does not show %q", want)
		}
	}
	// tui holds one directory, which holds one directory. Three rows and six
	// columns to say tui/prview is what makes a narrow column unreadable.
	if strings.Contains(out, "▾ tui/\n") {
		t.Error("tui/ printed on a row of its own instead of joining the run below it")
	}
}

// The churn belongs to the file's own heading in the diff. In the column
// beside it, it is the same number twice and the cells a nested path needs.
func TestTheChurnIsOnTheFileHeadingAndNotInTheColumn(t *testing.T) {
	m := onFiles(200, 50)

	head := false
	for _, line := range strings.Split(stripANSI(m.View()), "\n") {
		cells := strings.Split(line, "│")
		if len(cells) < 2 {
			continue
		}
		if strings.ContainsAny(cells[1], "+−") {
			t.Errorf("the file column carries churn: %q", strings.TrimSpace(cells[1]))
		}
		if strings.Contains(line, "internal/gh/client.go") && strings.Contains(line, "+2") {
			head = true
		}
	}

	if !head {
		t.Error("the file heading in the diff lost its churn along with the column's")
	}
}

func TestARenameShowsThePathItCameFrom(t *testing.T) {
	out := stripANSI(showFile(t, onFiles(200, 56), "internal/tui/prview/files.go").View())
	if !strings.Contains(out, "internal/tui/prview/diff.go → internal/tui/prview/files.go") {
		t.Error("the rename does not show the path it came from")
	}
}

// A line number that does not line up with the one above it is worse than none.
func TestTheGutterHoldsBothSidesLineNumbers(t *testing.T) {
	out := stripANSI(onFiles(200, 50).View())

	// The removed line has an old number and no new one; the added lines the
	// other way round.
	for _, want := range []string{"41    − ", "   41 + ", "40 40   "} {
		if !strings.Contains(out, want) {
			t.Errorf("the gutter does not show %q", want)
		}
	}
}

// A line of code folded onto a second row puts its tail under the gutter and
// every line below it out of step with its own number.
func TestALongCodeLineClipsRatherThanWraps(t *testing.T) {
	long := strings.Repeat("x", 400)
	files := []gh.ChangedFile{{
		Path: "a.go", Additions: 1,
		Hunks: []gh.Hunk{{Header: "@@ -1 +1 @@", Lines: []gh.DiffLine{
			{Kind: gh.DiffAdded, New: 1, Content: long},
		}}},
	}}

	m := detailed(held(sampleDetail()), 120, 30)
	m.SetFiles(loadedFiles(files, 0))
	m = press(m, "]", "]", "]")

	rows := 0
	for _, line := range strings.Split(m.View(), "\n") {
		if lipgloss.Width(line) != 120 {
			t.Fatalf("line %q is %d wide, want 120", stripANSI(line), lipgloss.Width(line))
		}
		if strings.Contains(stripANSI(line), "xxxx") {
			rows++
		}
	}
	if rows != 1 {
		t.Errorf("the long line covers %d rows, want 1", rows)
	}
}

// Reading a review means reading the comments against the lines they were
// written about, not scrolling back to the conversation for them.
func TestAReviewThreadRendersUnderTheLineItAnchorsTo(t *testing.T) {
	out := stripANSI(onFiles(200, 60).View())

	code := strings.Index(out, "delay = min(delay*2, fetchTimeout)")
	comment := strings.Index(out, "This backs off forever.")
	next := strings.Index(out, "internal/store/store.go")

	switch {
	case comment < 0:
		t.Fatal("the open thread is not on the Files tab at all")
	case comment < code:
		t.Error("the thread renders above the line it anchors to")
	case next > 0 && comment > next:
		t.Error("the thread renders under the wrong file")
	}
}

// A comment on a deleted line and one on an added line can carry the same
// number. Only the side tells them apart.
func TestAThreadOnTheLeftAnchorsToTheRemovedLine(t *testing.T) {
	out := stripANSI(showFile(t, onFiles(200, 60), "internal/store/store.go").View())

	removed := strings.Index(out, "// It refuces a duplicate.")
	resolved := strings.Index(out, "resolved")

	if removed < 0 || resolved < 0 {
		t.Fatal("the removed line or its thread is missing")
	}
	if resolved < removed {
		t.Error("the left-side thread did not follow the line it was written against")
	}
}

// An outdated thread anchors to a line the pull request has moved past.
// Dropping it loses the only record of what was asked.
func TestAThreadWithNoLineInTheDiffStillRenders(t *testing.T) {
	d := sampleDetail()
	d.Threads = append(d.Threads, gh.ReviewThread{
		Path: "internal/gh/client.go", Line: 900, Side: gh.SideRight, IsOutdated: true,
		Comments: []gh.Comment{{Author: gh.Actor{Login: "nkr"}, Body: "Long since moved."}},
	})

	m := detailed(held(d), 200, 60)
	m.SetFiles(loadedFiles(sampleFiles(), 0))
	m = press(m, "]", "]", "]")

	if !strings.Contains(stripANSI(m.View()), "Long since moved.") {
		t.Error("a thread with no line left in the diff vanished")
	}
}

// Two threads on one line put two stops side by side with no code between
// them, which is the one join that has no line of its own to hang a break on.
func TestTwoThreadsOnOneLineStackWithoutAGap(t *testing.T) {
	d := sampleDetail()
	d.Threads = append(d.Threads, gh.ReviewThread{
		ID: "RT_9", Path: "internal/gh/client.go", Line: 42, Side: gh.SideRight,
		Comments: []gh.Comment{{Author: gh.Actor{Login: "octobot"}, Body: "And it never logs."}},
	})

	m := detailed(held(d), 200, 60)
	m.SetFiles(loadedFiles(sampleFiles(), 0))
	m = press(m, "]", "]", "]")

	lines := strings.Split(stripANSI(m.View()), "\n")
	first := slices.IndexFunc(lines, func(l string) bool { return strings.Contains(l, "This backs off forever.") })
	second := slices.IndexFunc(lines, func(l string) bool { return strings.Contains(l, "And it never logs.") })
	if first < 0 || second < 0 {
		t.Fatal("both threads on the line are not on the frame")
	}

	// The second card's own top border, and the row that has to sit against it.
	top := second
	for top > first && !strings.Contains(lines[top], "╭") {
		top--
	}
	if !strings.Contains(lines[top-1], "╰") {
		t.Errorf("the second thread does not sit against the first:\n%s", strings.Join(lines[top-1:top+1], "\n"))
	}
}

// litHunk is the @@ row the ring is on, read off the fill the cursor paints.
func litHunk(frame string) string {
	fill := bgSeq(theme.RosePineMoon.SelectedBackground)
	for _, line := range strings.Split(frame, "\n") {
		if strings.Contains(line, fill) && strings.Contains(stripANSI(line), "@@") {
			return strings.TrimSpace(stripANSI(line))
		}
	}
	return ""
}

// The diff draws the same cards the conversation does, and without a ring not
// one of them can be replied to, resolved or reacted to.
func TestTheBracesWalkTheHunksAndCardsOfADiff(t *testing.T) {
	m := onFiles(200, 50)
	if got := litHunk(m.View()); got != "" {
		t.Fatalf("the tab opened with %q already lit, want nothing", got)
	}

	m = press(m, "}")
	if got := litHunk(m.View()); !strings.Contains(got, "@@ -40,4 +40,5 @@") {
		t.Fatalf("the first brace lit %q, want the file's own hunk", got)
	}

	m = press(m, "}")
	if got := focusedCard(t, m.View()); !strings.Contains(got, "internal/gh/client.go:42") {
		t.Errorf("the second brace lit %q, want the thread written against the hunk", got)
	}
	if litHunk(m.View()) != "" {
		t.Error("the hunk is still lit with the card lit as well")
	}

	if got := litHunk(press(m, "{").View()); !strings.Contains(got, "@@ -40,4 +40,5 @@") {
		t.Errorf("the brace back landed on %q, want the hunk again", got)
	}
}

// The pinned heading costs the pane two rows and arrives with the diff, which
// is the real order: the tab asks for the diff as it opens.
func TestADiffLandingOnTheOpenTabSizesThePane(t *testing.T) {
	files := []gh.ChangedFile{longFile("a.go", 200)}

	// Opened first, the diff arriving after.
	late := press(detailed(held(sampleDetail()), 100, 24), "]", "]", "]")
	late.SetFiles(loadedFiles(files, 0))

	// And the other way round, which was always sized right.
	early := detailed(held(sampleDetail()), 100, 24)
	early.SetFiles(loadedFiles(files, 0))
	early = press(early, "]", "]", "]")

	if got, want := scrollReadout(late.View()), scrollReadout(early.View()); got != want {
		t.Errorf("the diff landing on the open tab reads %q, want %q", got, want)
	}

	// And the end of it is reachable rather than clipped past the last row. The
	// tab opens on the column, so the pane is taken before it is scrolled.
	if out := stripANSI(press(late, "2", "G").View()); !strings.Contains(out, "case 199:") {
		t.Error("the last line of the diff cannot be scrolled to")
	}
}

// scrollReadout is the N/M a pane draws in its bottom border once its content
// outruns it.
func scrollReadout(frame string) string {
	for _, line := range strings.Split(stripANSI(frame), "\n") {
		if !strings.Contains(line, "╯") || !strings.Contains(line, "/") {
			continue
		}
		cut := strings.LastIndex(line, "─")
		return strings.Trim(line[strings.LastIndex(line[:cut], "─")+3:], "─╯ ")
	}
	return ""
}

// A refetch moves the cursor after the rows are rebuilt, and the pane draws
// whatever was named before that.
func TestARefetchLeavesTheCursorAndThePaneOnOneFile(t *testing.T) {
	// Reading screenshot.png with the cursor parked on the docs/ row above it,
	// which is row zero and so the one a refetch moves.
	m := press(showFile(t, onFiles(200, 50), "docs/screenshot.png"), "1", "g")
	if got := cursorFile(m.View()); !strings.Contains(got, "docs/") {
		t.Fatalf("setup: the cursor is on %q, want the directory row", got)
	}

	m.SetFiles(loadedFiles(sampleFiles(), 0))

	if got, want := cursorFile(m.View()), diffHeads(m.View()); !strings.Contains(want, strings.TrimSpace(got)) {
		t.Errorf("the column is on %q and the pane is drawing %q", got, want)
	}
}

// A refetch answering with nothing leaves a body of one line, and the stops the
// last render put in the ring have nowhere to be.
func TestADiffThatComesBackEmptyDropsItsStops(t *testing.T) {
	m := press(onFiles(200, 50), "}", "}")
	if focusedCard(t, m.View()) == "" {
		t.Fatal("setup: nothing is lit to be left behind")
	}

	m.SetFiles(loadedFiles(nil, 0))

	// A stop nothing drew still answers esc, so the reader presses it, the
	// screen swallows it, and nothing on the frame says why.
	_, cmd := m.Update(escape())
	if cmd == nil {
		t.Error("esc was swallowed by a stop left over from the diff that went")
	}
}

// The stops are the last tab's until the next one writes its own, and a tab
// that answers with a note instead of blocks never writes any.
func TestATabShowingANoteDropsTheLastTabsStops(t *testing.T) {
	m := detailed(store.Detail{}, 200, 50)
	m.SetFiles(loadedFiles(sampleFiles(), 0))

	// No detail, so the diff carries no threads and a hunk is the only stop.
	onFiles := press(press(m, "]", "]", "]"), "}")
	if litHunk(onFiles.View()) == "" {
		t.Fatal("setup: nothing is lit on the Files tab to leave behind")
	}

	// Back to a conversation that has not landed, so the pane is one note.
	back := press(onFiles, "]")
	if _, cmd := back.Update(escape()); cmd == nil {
		t.Error("esc was swallowed on a tab whose ring was never rebuilt")
	}
}

// twoHunks is a file whose diff jumps, which is the only shape a gap between
// hunks shows up in. sampleFiles carries one hunk per file.
func twoHunks() []gh.ChangedFile {
	return []gh.ChangedFile{{
		Path: "a.go", Status: gh.FileModified, Additions: 2,
		Hunks: []gh.Hunk{
			{Header: "@@ -1,2 +1,3 @@", Lines: []gh.DiffLine{
				{Kind: gh.DiffContext, Old: 1, New: 1, Content: "package a"},
				{Kind: gh.DiffAdded, New: 2, Content: "const x = 1"},
			}},
			{Header: "@@ -40,2 +41,3 @@ func New() {", Lines: []gh.DiffLine{
				{Kind: gh.DiffContext, Old: 40, New: 41, Content: "\tfor {"},
				{Kind: gh.DiffAdded, New: 42, Content: "\t\tbreak"},
			}},
		},
	}}
}

// A hunk is a jump to somewhere else in the file, and run against the line
// above it the two read as one stretch of code that never was.
func TestHunksAreSeparatedByABlankLine(t *testing.T) {
	m := detailed(held(sampleDetail()), 100, 24)
	m.SetFiles(loadedFiles(twoHunks(), 0))
	m = press(m, "]", "]", "]")

	lines := strings.Split(stripANSI(m.View()), "\n")
	second := slices.IndexFunc(lines, func(l string) bool { return strings.Contains(l, "@@ -40,2 +41,3 @@") })
	if second < 1 {
		t.Fatal("the second hunk is not on the frame")
	}

	if body := diffRow(lines[second-1]); strings.TrimSpace(body) != "" {
		t.Errorf("the second hunk sits straight under %q", strings.TrimSpace(body))
	}
	if body := diffRow(lines[second-2]); !strings.Contains(body, "const x = 1") {
		t.Errorf("the gap is more than one row: %q", strings.TrimSpace(body))
	}
}

// diffRow is what a frame row holds inside the diff pane's own borders.
func diffRow(line string) string {
	cells := strings.Split(line, "│")
	if len(cells) < 4 {
		return ""
	}
	return cells[len(cells)-2]
}

// One file is in the pane, so the stop after a file's last one is in the next
// file. A key dying at each file boundary would send the reader to the column.
func TestTheBracesCrossFromOneFileToTheNext(t *testing.T) {
	m := press(onFiles(200, 50), "}", "}", "}")

	// Past the reply, which is the last stop in the first file.
	m = press(m, "}")
	if got := diffHeads(m.View()); !strings.Contains(got, "internal/store/store.go") {
		t.Fatalf("the brace past the last stop left the pane on %q", got)
	}
	if got := litHunk(m.View()); !strings.Contains(got, "@@ -87,3 +87,2 @@") {
		t.Errorf("crossing landed on %q, want the new file's first hunk", got)
	}

	// And back, onto the last stop of the file it came from rather than its head.
	m = press(m, "{")
	if got := diffHeads(m.View()); !strings.Contains(got, "internal/gh/client.go") {
		t.Fatalf("the brace back left the pane on %q", got)
	}
	if got := focusedCard(t, m.View()); !strings.Contains(got, "octobot · said · 1h") {
		t.Errorf("crossing back landed on %q, want the last stop of that file", got)
	}
}

// A reply is a card of its own in the diff as it is in the conversation. One
// the motion key walks past is one the reader can see and cannot answer.
func TestTheBracesStopOnAReplyInTheDiff(t *testing.T) {
	m := press(onFiles(200, 50), "}", "}", "}")

	if got := focusedCard(t, m.View()); !strings.Contains(got, "octobot · said · 1h") {
		t.Fatalf("the brace past the thread lit %q, want the reply hanging off it", got)
	}

	// x and v mean the thread and not an answer to it, so neither is named here.
	lines := strings.Split(stripANSI(m.View()), "\n")
	footer := footerRow(t, lines, headingRow(t, lines, "octobot · said · 1h"))
	if !strings.Contains(footer, "r reply") {
		t.Errorf("the reply holding the keys names none of them: %q", footer)
	}
	for _, key := range []string{"x resolve", "x unresolve", "in diff"} {
		if strings.Contains(footer, key) {
			t.Errorf("the reply names %q, which acts on the thread and not on it: %q", key, footer)
		}
	}
}

// The cursor moving is not the pane moving: from a directory row the next file
// row is the file already drawn, and crossing onto it walks it a second time.
func TestCrossingFromADirectoryRowStillReachesTheNextFile(t *testing.T) {
	// Up onto the gh/ row, which keeps the pane on the file under it.
	m := press(onFiles(200, 50), "1", "k")
	if got := cursorFile(m.View()); !strings.Contains(got, "gh/") {
		t.Fatalf("setup: the cursor is on %q, want the directory above the file", got)
	}
	if got := diffHeads(m.View()); !strings.Contains(got, "internal/gh/client.go") {
		t.Fatalf("setup: the pane is drawing %q, want the first file", got)
	}

	// Off the end of client.go: its hunk, its thread, its reply, then across.
	m = press(m, "}", "}", "}", "}")
	if got := diffHeads(m.View()); !strings.Contains(got, "internal/store/store.go") {
		t.Errorf("the brace off the last stop left the pane on %q", got)
	}
}

// Past the last file it stays put. Both ends of every ring here are boundaries.
func TestTheBracesStopAtTheEndsOfTheDiff(t *testing.T) {
	// docs/screenshot.png sorts first and GitHub sent no body for it, so it is
	// also the file with no stop to land on.
	first := press(onFiles(200, 50), "}", "{", "{", "{")
	if got := diffHeads(first.View()); !strings.Contains(got, "docs/screenshot.png") {
		t.Errorf("the brace back off the first file moved to %q", got)
	}

	last := press(onFiles(200, 50), "}", "}", "}", "}", "}", "}", "}", "}", "}")
	if got := diffHeads(last.View()); !strings.Contains(got, "internal/tui/prview/files.go") {
		t.Errorf("the brace past the last file moved to %q", got)
	}
}

// A card in a diff answers the line above it, so topping it scrolls away the
// one thing the reader is reading the comment about.
func TestACardInTheDiffOpensBelowTheCodeItAnswers(t *testing.T) {
	// Short enough that the card cannot sit on screen whole, so the jump has to
	// choose where to put it.
	lines := strings.Split(stripANSI(press(onFiles(200, 14), "}", "}").View()), "\n")

	card := slices.IndexFunc(lines, func(l string) bool { return strings.Contains(l, "╭─") && !strings.HasPrefix(l, "╭") })
	if card < 0 {
		t.Fatal("the card the braces landed on is not on the frame")
	}

	above := strings.Join(lines[:card], "\n")
	if !strings.Contains(above, "time.Sleep(delay)") {
		t.Errorf("the card was topped and the line it answers went with it:\n%s", above)
	}
}

// v means take me to the diff, and inside one there is nowhere left to go. A
// footer naming a key that does nothing is worse than a footer without it.
func TestACardInTheDiffNamesItsKeysAndNotTheJump(t *testing.T) {
	m := press(onFiles(200, 50), "}", "}")

	lines := strings.Split(stripANSI(m.View()), "\n")
	footer := footerRow(t, lines, headingRow(t, lines, "internal/gh/client.go:42"))

	for _, want := range []string{"r reply", "x resolve"} {
		if !strings.Contains(footer, want) {
			t.Errorf("the card in the diff does not name %q: %q", want, footer)
		}
	}
	if strings.Contains(footer, "in diff") {
		t.Errorf("the card in the diff still offers the jump into it: %q", footer)
	}
}

// The key goes as quiet as the footer. Left live it re-enters the tab it is
// already on, which drags the column's cursor to a file it is already showing.
func TestVDoesNothingOnTheFilesTab(t *testing.T) {
	// The cursor parked off the file being read, so a jump has somewhere to move
	// it and doing nothing is visible.
	m := press(onFiles(200, 50), "1", "k", "}", "}")
	if got := cursorFile(m.View()); !strings.Contains(got, "gh/") {
		t.Fatalf("setup: the cursor is on %q, want the directory row", got)
	}

	after, cmd := key(m, "v")
	if cmd != nil {
		t.Errorf("v produced %T on the tab it takes a thread to", cmd())
	}
	if got := cursorFile(after.View()); !strings.Contains(got, "gh/") {
		t.Errorf("v moved the cursor to %q on the tab it is named nowhere on", got)
	}
}

// Replying is the whole point of the ring, and the box belongs under the card
// it answers rather than at the foot of a page the diff does not have.
func TestReplyOpensABoxInTheDiff(t *testing.T) {
	m := press(onFiles(200, 50), "}", "}", "r")

	out := stripANSI(m.View())
	if !strings.Contains(out, "Leave a reply") {
		t.Fatal("r opened no reply box in the diff")
	}
	if !strings.Contains(out, "Post") {
		t.Error("the box in the diff is missing the button that sends it")
	}
	if !strings.Contains(out, "This backs off forever.") {
		t.Error("the box covered the comment it answers")
	}
}

// The popup is drawn at the caret, and the caret's line and column come from
// the box's own offsets. A diff dropping those leaves the list nowhere to open.
func TestAnAtInADiffBoxOpensTheMentionList(t *testing.T) {
	m := press(onFiles(200, 60), "}", "}", "r")
	m.SetRepo(loadedRepo())

	m, _ = typing(m, "@")
	if out := stripANSI(m.View()); !strings.Contains(out, "Nikita Rushmanov") {
		t.Errorf("the mention list never opened over a box in the diff:\n%s", out)
	}
}

// esc leaves the screen with a card lit, on this tab as on the conversation.
func TestEscBacksOutWithACardLitInTheDiff(t *testing.T) {
	m := press(onFiles(200, 50), "}", "}")
	if focusedCard(t, m.View()) == "" {
		t.Fatal("setup: nothing is lit")
	}

	_, cmd := m.Update(escape())
	if cmd == nil {
		t.Fatal("esc did not leave the screen with a card in the diff lit")
	}
	if _, ok := cmd().(prview.BackMsg); !ok {
		t.Errorf("esc sent %T, want a BackMsg", cmd())
	}
}

// tab is the file key here and the strip stays on ] and [, which is the half of
// the swap that is easy to drop.
func TestTheFileKeyLeavesTheTabStrip(t *testing.T) {
	m := onFiles(200, 50)

	for _, k := range []string{"tab", "shift+tab"} {
		if got := currentTab(t, press(m, k).View()); got != "Files" {
			t.Errorf("%q moved the tab strip, to %q", k, got)
		}
	}
}

// The tree belongs to the column and the blocks to the pane. A fold in the
// pane leaves the tree alone and keeps the hunk heading as the way back in.
func TestTheFoldKeyCollapsesTheLitHunk(t *testing.T) {
	// The cursor left on the gh/ row, which is the only kind the tree folds.
	m := press(onFiles(200, 50), "1", "k", "}")
	if litHunk(m.View()) == "" {
		t.Fatal("setup: no hunk is lit for the key to land on")
	}
	if !strings.Contains(stripANSI(m.View()), "▾ gh/") {
		t.Fatal("setup: the directory the cursor is on is not open")
	}

	folded := press(m, "space")
	out := stripANSI(folded.View())
	if !strings.Contains(out, " @@ -40,4 +40,5 @@") {
		t.Error("the folded hunk lost its closed heading")
	}
	for _, hidden := range []string{"time.Sleep(delay)", "This backs off forever."} {
		if strings.Contains(out, hidden) {
			t.Errorf("the folded hunk still shows %q", hidden)
		}
	}
	if !strings.Contains(out, "▾ gh/") {
		t.Error("folding the hunk changed the tree")
	}

	opened := stripANSI(press(folded, "space").View())
	for _, restored := range []string{" @@ -40,4 +40,5 @@", "time.Sleep(delay)", "This backs off forever."} {
		if !strings.Contains(opened, restored) {
			t.Errorf("opening the hunk did not restore %q", restored)
		}
	}
}

func TestFoldingOneHunkLeavesTheNextOneOpen(t *testing.T) {
	files := []gh.ChangedFile{{
		Path: "two.go", Status: gh.FileModified, Additions: 2,
		Hunks: []gh.Hunk{
			{Header: "@@ -1 +1 @@", Lines: []gh.DiffLine{{Kind: gh.DiffAdded, New: 1, Content: "const first = 1"}}},
			{Header: "@@ -10 +10 @@", Lines: []gh.DiffLine{{Kind: gh.DiffAdded, New: 10, Content: "const second = 2"}}},
		},
	}}

	m := detailed(held(sampleDetail()), 120, 30)
	m.SetFiles(loadedFiles(files, 0))
	out := stripANSI(press(m, "]", "]", "]", "}", "space").View())

	if strings.Contains(out, "const first = 1") {
		t.Error("the folded hunk still shows its code")
	}
	if !strings.Contains(out, "const second = 2") {
		t.Error("folding the first hunk hid the second")
	}
	if !strings.Contains(out, " @@ -1 +1 @@") || !strings.Contains(out, " @@ -10 +10 @@") {
		t.Error("the two hunk headings do not report their own fold states")
	}
}

func TestTheRingSkipsThreadsInsideAFoldedHunk(t *testing.T) {
	m := press(onFiles(200, 50), "}", "space", "}")

	if got := litHunk(m.View()); !strings.Contains(got, "@@ -87,3 +87,2 @@") {
		t.Errorf("the ring landed on %q, want the next file's hunk", got)
	}
	if !strings.Contains(diffHeads(m.View()), "internal/store/store.go") {
		t.Error("the ring did not cross to the next file")
	}
}

// A push makes every hunk new reading, even where GitHub gives one the same
// heading. A refresh on the same commit keeps the reader's place.
func TestAChangedHeadOpensFoldedHunks(t *testing.T) {
	d := sampleDetail()
	d.HeadRefOid = "before"
	m := detailed(held(d), 200, 50)
	m.SetFiles(loadedFiles(sampleFiles(), 0))
	m = press(m, "]", "]", "]", "}", "space")

	m.SetDetail(held(d))
	m.SetFiles(loadedFiles(sampleFiles(), 0))
	if out := stripANSI(m.View()); !strings.Contains(out, " @@ -40,4 +40,5 @@") {
		t.Error("a refresh on the same head opened the folded hunk")
	}

	d.HeadRefOid = "after"
	m.SetDetail(held(d))
	m.SetFiles(loadedFiles(sampleFiles(), 0))
	out := stripANSI(m.View())
	if !strings.Contains(out, " @@ -40,4 +40,5 @@") {
		t.Error("a changed head left the hunk folded")
	}
	for _, restored := range []string{"time.Sleep(delay)", "This backs off forever."} {
		if !strings.Contains(out, restored) {
			t.Errorf("the changed head did not reveal %q", restored)
		}
	}
}

func TestFoldingADirectoryTakesItsFilesOutOfTheTree(t *testing.T) {
	m := press(onFiles(200, 50), "1", "g", "j", "j", "space")

	out := stripANSI(m.View())
	if strings.Contains(out, "▾ internal/ ") {
		t.Error("the directory still reads as open")
	}
	if strings.Contains(out, "client.go") {
		t.Error("folding internal/ left its files on screen")
	}
}

func TestSelectingAFileScrollsTheDiffToIt(t *testing.T) {
	// A frame short enough that the last file is well past the first screen.
	m := press(onFiles(200, 16), "1")
	if strings.Contains(stripANSI(m.View()), "const tabWidth = 4") {
		t.Fatal("the last file is already on screen; the test proves nothing")
	}

	m = press(m, "G")
	if !strings.Contains(stripANSI(m.View()), "const tabWidth = 4") {
		t.Error("selecting the file did not scroll the diff to it")
	}
}

// The keys that move further than a line have to move the cursor too. A file
// column scrolled away from its own cursor answers nothing.
func TestTheFileColumnKeepsItsCursorUnderTheJumpKeys(t *testing.T) {
	m := press(onFiles(200, 16), "1", "G")

	out := stripANSI(m.View())
	last := strings.Index(out, "files.go")
	if last < 0 {
		t.Fatal("the last file is not in the tree")
	}
	if !strings.Contains(cursorFile(m.View()), "files.go") {
		t.Errorf("the cursor is on %q, want the last file", cursorFile(m.View()))
	}

	if !strings.Contains(cursorFile(press(m, "g").View()), "docs/") {
		t.Error("g did not take the cursor back to the top")
	}
}

// The cursor says which file the diff is showing, which is the question the
// column exists to answer whether or not the keys are pointed at it.
func TestTheFileCursorStaysPaintedWithFocusOnTheDiff(t *testing.T) {
	m := press(onFiles(200, 40), "1", "g", "j", "j", "j")
	if !strings.Contains(cursorFile(m.View()), "gh/") {
		t.Fatal("the cursor is not where the test put it")
	}

	if !strings.Contains(cursorFile(press(m, "2").View()), "gh/") {
		t.Error("focusing the diff took the cursor off the file column")
	}
}

// selectedRow is the whole frame line carrying the selection background.
// The fill is looked for inside the column rather than anywhere on the line.
// The diff beside it carries a fill of its own now that the tab opens with a
// hunk lit, and it lands on whichever row of the tree it happens to sit beside:
// taking the first painted line on the frame answered with the wrong pane.
func selectedRow(frame string) string {
	for _, line := range strings.Split(frame, "\n") {
		at := strings.Index(line, selectionSeq())
		if at < 0 {
			continue
		}
		// The column's own right border, which the fill sits inside of.
		if edge := nthIndex(line, "│", 2); edge >= 0 && at < edge {
			return line
		}
	}
	return ""
}

// nthIndex is where the nth occurrence of sep starts, or -1.
func nthIndex(s, sep string, n int) int {
	at := 0
	for range n {
		i := strings.Index(s[at:], sep)
		if i < 0 {
			return -1
		}
		at += i + len(sep)
	}
	return at - len(sep)
}

// cursorFile is the file column's share of that line. The frame spans two
// panes, and the whole line lets the diff beside the tree answer for it.
func cursorFile(frame string) string {
	cells := strings.Split(stripANSI(selectedRow(frame)), "│")
	if len(cells) < 2 {
		return ""
	}
	return strings.TrimSpace(cells[1])
}

// The rail is about the pull request rather than the change, the tree wants a
// column, and a diff between the two is a gutter and a fragment.
func TestTheRailIsOffOnTheFilesTabAtEveryWidth(t *testing.T) {
	tests := []struct {
		width int
		tree  bool
	}{
		{width: 220, tree: true},
		{width: 160, tree: true},
		{width: 120, tree: true},
		{width: 80, tree: true},
		{width: 60, tree: false},
	}

	for _, tt := range tests {
		m := detailed(held(sampleDetail()), tt.width, 40)
		m.SetFiles(loadedFiles(sampleFiles(), 0))
		out := stripANSI(press(m, "]", "]", "]").View())

		if strings.Contains(out, "Details") {
			t.Errorf("width %d: the rail is on screen", tt.width)
		}
		// Read off the border rather than off the title alone: the strip a row
		// above names the tab Files too, and it is there at every width.
		if got := strings.Contains(out, "[1]─Files"); got != tt.tree {
			t.Errorf("width %d: tree on screen = %v, want %v", tt.width, got, tt.tree)
		}
	}
}

// The two side columns never share a frame, so the only place a difference
// between them shows is in the jump when you tab from one to the other.
func TestTheFileColumnAndTheRailAreTheSameWidth(t *testing.T) {
	m := detailed(held(sampleDetail()), 200, 40)
	m.SetFiles(loadedFiles(sampleFiles(), 0))

	// Both lead their row, so both are the leftmost pane and measure the same
	// way. The rail used to close the row instead and had to be measured back
	// from the frame's right edge.
	rail := paneEnd(t, m.View())
	tree := paneEnd(t, press(m, "]", "]", "]").View())

	if tree != rail {
		t.Errorf("file column is %d wide and the rail is %d, want them equal", tree, rail)
	}
}

// paneEnd is where the leftmost pane's right border sits, which is its width.
func paneEnd(t *testing.T, frame string) int {
	t.Helper()
	top := stripANSI(paneTop(frame))
	at := strings.Index(top, "╮")
	if at < 0 {
		t.Fatalf("no pane corner in %q", top)
	}
	return lipgloss.Width(top[:at]) + 1
}

// The column is the only navigation the tab has, so it narrows rather than
// disappearing once the diff has taken its measure.
func TestTheFileColumnNarrowsBeforeItHides(t *testing.T) {
	widths := map[int]int{220: 37, 120: 37, 100: 24, 80: 24}

	for frame, want := range widths {
		m := detailed(held(sampleDetail()), frame, 40)
		m.SetFiles(loadedFiles(sampleFiles(), 0))

		got := paneEnd(t, press(m, "]", "]", "]").View())
		if got != want {
			t.Errorf("frame %d: file column is %d wide, want %d", frame, got, want)
		}
	}
}

// The rail belongs to the conversation, and stepping aside for the diff must
// not read as having been turned off.
func TestTheRailComesBackWhenTheTabDoes(t *testing.T) {
	m := detailed(held(sampleDetail()), 130, 40)
	m.SetFiles(loadedFiles(sampleFiles(), 0))

	if !strings.Contains(stripANSI(m.View()), "Details") {
		t.Fatal("the rail is not up on the conversation at 130 columns")
	}
	m = press(m, "]", "]", "]")
	if strings.Contains(stripANSI(m.View()), "Details") {
		t.Fatal("the rail did not step aside on the Files tab")
	}
	if !strings.Contains(stripANSI(press(m, "]").View()), "Details") {
		t.Error("the rail did not come back when the tab moved on")
	}
}

func TestTheFrameFillsItsSizeExactlyOnTheFilesTab(t *testing.T) {
	sizes := []struct{ width, height int }{
		{200, 60}, {160, 40}, {144, 30}, {120, 24}, {100, 20}, {70, 16}, {60, 12}, {40, 8},
	}

	for _, size := range sizes {
		m := detailed(held(sampleDetail()), size.width, size.height)
		m.SetFiles(loadedFiles(sampleFiles(), 3))
		frame := press(m, "]", "]", "]").View()

		lines := strings.Split(frame, "\n")
		if len(lines) != size.height {
			t.Errorf("%dx%d: %d lines, want %d", size.width, size.height, len(lines), size.height)
		}
		for i, line := range lines {
			if lipgloss.Width(line) != size.width {
				t.Errorf("%dx%d: line %d is %d wide, want %d",
					size.width, size.height, i, lipgloss.Width(line), size.width)
			}
		}
	}
}

// The panes are numbered by where they sit, so the digits have to follow what
// is on screen rather than what each pane holds.
func TestThePanesAreNumberedLeftToRight(t *testing.T) {
	top := stripANSI(paneTop(onFiles(200, 40).View()))

	tree := strings.Index(top, "[1]")
	diff := strings.Index(top, "[2]")

	if tree < 0 || diff < 0 {
		t.Fatalf("want two numbered panes, got %q", top)
	}
	if tree > diff {
		t.Error("the pane numbers do not run left to right")
	}
	if strings.Contains(top, "[3]") {
		t.Error("a third pane is numbered on a tab with two")
	}
}

func TestFocusingTheDiffLeavesTheCursorWhereItWas(t *testing.T) {
	m := press(onFiles(200, 40), "1", "g")
	before := cursorFile(m.View())

	if got := cursorFile(press(m, "2").View()); got != before {
		t.Errorf("focusing the diff moved the cursor to %q, want it left on %q",
			strings.TrimSpace(got), strings.TrimSpace(before))
	}
}

// Leaving the Files tab takes the tree with it, and the movement keys cannot
// keep driving a pane that is no longer on screen.
func TestFocusLeavesTheTreeWhenTheTabDoes(t *testing.T) {
	m := detailed(held(sampleDetail()), 200, 20)
	m.SetFiles(loadedFiles(sampleFiles(), 0))

	m = press(m, "]", "]", "]", "1") // to Files, focus the tree
	m = press(m, "]")                // round to the conversation

	before := footerOf(t, m.View())
	if after := footerOf(t, press(m, "j", "j", "j").View()); after == before {
		t.Errorf("the conversation did not scroll: footer stayed at %q", before)
	}
}

// A digit with no pane behind it does nothing rather than focusing something
// that is not there.
func TestADigitPastTheLastPaneIsIgnored(t *testing.T) {
	m := detailed(held(sampleDetail()), 200, 20)

	before := footerOf(t, m.View())
	m = press(m, "3", "j", "j", "j")
	if footerOf(t, m.View()) == before {
		t.Error("3 moved focus off the conversation on a screen with two panes")
	}
}

// Tall enough for the note to be on the first frame. The threads in the diff
// carry replies, and a reply is a card, so the page below the last file is
// deeper than a window of sixty rows.
func TestOverflowIsReportedRatherThanDropped(t *testing.T) {
	m := detailed(held(sampleDetail()), 200, 80)
	m.SetFiles(loadedFiles(sampleFiles(), 3))

	if !strings.Contains(stripANSI(press(m, "]", "]", "]").View()), "3 more files on GitHub") {
		t.Error("the files the page did not reach go unreported")
	}
}

// The reason a file has no diff is a sentence, and it is the only thing in the
// block. Cut without a mark it reads as the whole reason.
func TestTheOmittedReasonIsMarkedRatherThanCut(t *testing.T) {
	m := detailed(held(sampleDetail()), 75, 30)
	m.SetFiles(loadedFiles(sampleFiles(), 0))

	out := stripANSI(press(m, "]", "]", "]").View())
	if strings.Contains(out, "return a di") && !strings.Contains(out, "return a diff") {
		t.Errorf("the omitted reason was cut without a mark:\n%s", out)
	}
}

// The pane clips a heading that does not fit, and it clips mid-cell without a
// mark. A count losing its last digit that way still reads as a count.
func TestAChurnTooWideToFitIsMarkedRatherThanCut(t *testing.T) {
	files := sampleFiles()
	files[0].Additions, files[0].Deletions = 12345, 67890

	m := detailed(held(sampleDetail()), 18, 20)
	m.SetFiles(loadedFiles(files, 0))

	out := stripANSI(press(m, "]", "]", "]").View())
	if strings.Contains(out, "−6789") && !strings.Contains(out, "−67890") {
		t.Errorf("the deletion count was cut without a mark:\n%s", out)
	}
}

func TestTheDiffSpinsUntilItLands(t *testing.T) {
	m := detailed(held(sampleDetail()), 200, 40)
	m.SetFiles(store.Files{Status: store.StatusLoading})

	if !strings.Contains(stripANSI(press(m, "]", "]", "]").View()), "Loading the diff") {
		t.Error("the Files tab says nothing while the diff is on its way")
	}
}

func TestAFailedDiffSaysWhy(t *testing.T) {
	m := detailed(held(sampleDetail()), 200, 40)
	m.SetFiles(store.Files{Status: store.StatusFailed, Err: errors.New("context deadline exceeded")})

	out := stripANSI(press(m, "]", "]", "]").View())
	if !strings.Contains(out, "Could not load the diff: context deadline exceeded") {
		t.Error("a failed diff reads as an empty one")
	}
}

func TestAPullRequestWithNoFilesSaysSo(t *testing.T) {
	m := detailed(held(sampleDetail()), 200, 40)
	m.SetFiles(loadedFiles(nil, 0))

	if !strings.Contains(stripANSI(press(m, "]", "]", "]").View()), "No files changed.") {
		t.Error("an empty diff renders as a blank pane")
	}
}

// Selection is baked into every cell of the row. Wrapping a joined row instead
// paints only its first cell, because every styled run ends in a reset that
// clears the background with it.
func TestTheSelectedFileIsPaintedCellByCell(t *testing.T) {
	m := press(onFiles(200, 40), "1")

	selected := selectedRow(m.View())
	if selected == "" {
		t.Fatal("no row carries the selection background")
	}
	if got := strings.Count(selected, selectionSeq()); got < 3 {
		t.Errorf("the selection appears %d times on the row, want it on every cell", got)
	}
}

func selectionSeq() string {
	r, g, b, _ := theme.RosePineMoon.SelectedBackground.RGBA()
	return fmt.Sprintf("48;2;%d;%d;%d", r>>8, g>>8, b>>8)
}

// A changed line is read as a block, not a character at a time. The tint has to
// run the whole width, and it is painted per cell because every styled run ends
// in a reset that clears the background with it.
func TestAChangedLineIsTintedEdgeToEdge(t *testing.T) {
	frame := onFiles(200, 50).View()

	added, removed := "", ""
	for _, line := range strings.Split(frame, "\n") {
		switch {
		case strings.Contains(line, bgSeq(theme.RosePineMoon.AddedBackground)) && added == "":
			added = line
		case strings.Contains(line, bgSeq(theme.RosePineMoon.RemovedBackground)) && removed == "":
			removed = line
		}
	}

	if added == "" || removed == "" {
		t.Fatal("no line carries an added or removed background")
	}
	for _, tt := range []struct {
		name string
		line string
		seq  string
	}{
		{"added", added, bgSeq(theme.RosePineMoon.AddedBackground)},
		{"removed", removed, bgSeq(theme.RosePineMoon.RemovedBackground)},
	} {
		if got := strings.Count(tt.line, tt.seq); got < 5 {
			t.Errorf("the %s tint appears %d times, want it on every cell", tt.name, got)
		}
		// The frame is three panes wide, so the tint covers the diff's own
		// width rather than the line's. Anything short of that is a hole.
		if got := lipgloss.Width(tinted(tt.line, tt.seq)); got < 100 {
			t.Errorf("the %s tint covers %d cells, want it running to the border", tt.name, got)
		}
	}
}

// A context line has no tint to run out, so it must not be filled: the trailing
// spaces would be indistinguishable from a change with no color.
func TestAContextLineIsNotTinted(t *testing.T) {
	frame := stripANSI(onFiles(200, 50).View())

	for _, line := range strings.Split(frame, "\n") {
		if !strings.Contains(line, "40 40   ") {
			continue
		}
		if strings.Contains(line, bgSeq(theme.RosePineMoon.AddedBackground)) {
			t.Error("a context line came back tinted")
		}
		return
	}
	t.Fatal("the context line is not on screen")
}

// A run of hunks with nothing between them reads as one file. The box is what
// says where one ends.
// A file is not a thing to act on, so it gets a heading and no box. Boxing one
// put a review thread in the diff three borders deep.
func TestTheFileHeadingIsPinnedAboveTheDiff(t *testing.T) {
	lines := strings.Split(stripANSI(onFiles(200, 50).View()), "\n")

	head := -1
	for i, line := range lines {
		if strings.Contains(line, "internal/gh/client.go") && strings.Contains(line, "+2") {
			head = i
			break
		}
	}

	if head < 0 {
		t.Fatal("no heading carries the path and the churn")
	}
	if strings.Contains(lines[head-1], "╭─[2]") == false {
		t.Errorf("the heading is not the first row of the pane, over it is %q", lines[head-1])
	}
	if !strings.Contains(lines[head+1], "├─") {
		t.Errorf("the heading is not ruled off from the diff, under it is %q", lines[head+1])
	}
}

// Scrolling the diff leaves the heading where it is. A reader sixty lines into
// a file still has to be told which file it is, and the heading is the only
// thing on the frame that says so.
func TestTheFileHeadingSurvivesScrolling(t *testing.T) {
	files := sampleFiles()
	files[0] = longFile("internal/gh/client.go", 60)

	m := detailed(held(sampleDetail()), 200, 20)
	m.SetFiles(loadedFiles(files, 0))
	m = press(m, "]", "]", "]", "2")
	m = press(m, strings.Fields(strings.Repeat("j ", 40))...)

	if strings.Contains(stripANSI(m.View()), "@@ -1,60 +1,60 @@") {
		t.Fatal("setup: the diff never scrolled past its own hunk heading")
	}
	if got := diffHeads(m.View()); !strings.Contains(got, "internal/gh/client.go") {
		t.Errorf("the heading scrolled away, the pane names %q", got)
	}
}

// tinted is the text on a line the given background does cover. It walks the
// SGR runs rather than the text, which is the only way to tell a painted cell
// from a bare one.
func tinted(line, seq string) string {
	var out strings.Builder
	painted := false

	for len(line) > 0 {
		i := strings.IndexByte(line, 0x1b)
		if i < 0 {
			if painted {
				out.WriteString(line)
			}
			break
		}
		if painted {
			out.WriteString(line[:i])
		}

		end := strings.IndexByte(line[i:], 'm')
		if end < 0 {
			break
		}
		painted = strings.Contains(line[i:i+end], seq)
		line = line[i+end+1:]
	}
	return out.String()
}

func bgSeq(c color.Color) string {
	r, g, b, _ := c.RGBA()
	return fmt.Sprintf("48;2;%d;%d;%d", r>>8, g>>8, b>>8)
}

// Reading a diff is reading one file after another, and doing that by the line
// takes as many keystrokes as the file is long.
func TestTabJumpsAWholeFileFromEitherPane(t *testing.T) {
	for _, focus := range []string{"1", "2"} {
		// The column opens on the first file, so no jump to the top first: g on
		// the tree would land on the directory above it.
		m := press(onFiles(200, 20), focus)

		want := []string{"client.go", "store.go", "files.go"}
		for i, file := range want {
			if got := cursorFile(m.View()); !strings.Contains(got, file) {
				t.Fatalf("pane %s, jump %d: cursor on %q, want %q", focus, i, got, file)
			}
			// The diff follows the column, whichever one the keys are aimed at.
			// The last file cannot reach the top of the window, only the screen.
			if heads := diffHeads(m.View()); !strings.Contains(heads, file) {
				t.Errorf("pane %s: the diff does not show %q, only %q", focus, file, heads)
			}
			m = press(m, "tab")
		}

		// Past the last file it stays put rather than wrapping.
		if got := cursorFile(m.View()); !strings.Contains(got, "files.go") {
			t.Errorf("pane %s: tab past the last file moved to %q", focus, got)
		}
	}
}

func TestShiftTabWalksBackUpTheFiles(t *testing.T) {
	m := press(onFiles(200, 20), "2", "tab", "tab")
	if got := cursorFile(m.View()); !strings.Contains(got, "files.go") {
		t.Fatalf("setup: cursor on %q, want the last file", got)
	}

	for _, want := range []string{"store.go", "client.go", "screenshot.png"} {
		m = press(m, "shift+tab")
		if got := cursorFile(m.View()); !strings.Contains(got, want) {
			t.Fatalf("shift+tab landed on %q, want %q", got, want)
		}
	}

	if got := cursorFile(press(m, "shift+tab").View()); !strings.Contains(got, "screenshot.png") {
		t.Errorf("shift+tab past the first file moved to %q", got)
	}
}

// longFile is a file whose diff runs well past a test window, so the reader can
// be inside it with its heading off the top.
func longFile(path string, lines int) gh.ChangedFile {
	body := make([]gh.DiffLine, lines)
	for i := range body {
		body[i] = gh.DiffLine{Kind: gh.DiffContext, Old: i + 1, New: i + 1, Content: fmt.Sprintf("\tcase %d:", i)}
	}
	return gh.ChangedFile{
		Path:  path,
		Hunks: []gh.Hunk{{Header: fmt.Sprintf("@@ -1,%d +1,%d @@", lines, lines), Lines: body}},
	}
}

func TestTabFromADirectoryEntersItRatherThanSkippingIt(t *testing.T) {
	// docs/, screenshot.png, internal/, gh/, client.go
	m := press(onFiles(200, 20), "1", "g", "j", "j")
	if got := cursorFile(m.View()); !strings.Contains(got, "internal/") {
		t.Fatalf("setup: cursor on %q, want the internal/ directory", got)
	}

	if got := cursorFile(press(m, "tab").View()); !strings.Contains(got, "client.go") {
		t.Errorf("tab from internal/ landed on %q, want the first file inside it", got)
	}
}

// diffHeads is every file heading the diff pane has on screen. A heading
// carries the churn; the same path in the file column does not.
// diffHeads is the file the diff pane is drawing, read off the pinned heading
// above its rule. One file per pane, so there is only ever one.
func diffHeads(frame string) string {
	lines := strings.Split(stripANSI(frame), "\n")
	for i, line := range lines {
		if !strings.Contains(line, "├─") || i == 0 {
			continue
		}
		cells := strings.Split(lines[i-1], "│")
		if len(cells) < 2 {
			return ""
		}
		return strings.TrimSpace(cells[len(cells)-2])
	}
	return ""
}

// showFile walks the tree until the diff pane is drawing a named file, the way
// a reader looking for one does. The bound is generous: it only has to exceed
// the rows any fixture here builds.
func showFile(t *testing.T, m prview.Model, path string) prview.Model {
	t.Helper()
	m = press(m, "g")
	for range 200 {
		if strings.Contains(diffHeads(m.View()), path) {
			return m
		}
		m = press(m, "j")
	}
	t.Fatalf("the tree never reached %s, stopped on %q", path, diffHeads(m.View()))
	return m
}

// bigDiff is a pull request the size of a real refactor. Rendering one file
// tokenises the whole of it, so the cost the diff pays per keystroke is what
// this measures.
func bigDiff(files, lines int) []gh.ChangedFile {
	out := make([]gh.ChangedFile, files)
	for i := range out {
		hunk := gh.Hunk{Header: fmt.Sprintf("@@ -1,%d +1,%d @@", lines, lines)}
		for n := 1; n <= lines; n++ {
			kind := gh.DiffContext
			if n%7 == 0 {
				kind = gh.DiffAdded
			}
			hunk.Lines = append(hunk.Lines, gh.DiffLine{
				Kind: kind, Old: n, New: n,
				Content: fmt.Sprintf("\tfor i := range items { total += items[i].weight * %d }", n),
			})
		}
		out[i] = gh.ChangedFile{
			Path:      fmt.Sprintf("internal/pkg%02d/service.go", i),
			Status:    gh.FileModified,
			Additions: lines / 7,
			Hunks:     []gh.Hunk{hunk},
		}
	}
	return out
}

// Walking the file column repaints the diff. Every file rendering again for a
// cursor that moved one row is what made a large pull request unreadable.
func BenchmarkMoveTheCursorOnALargeDiff(b *testing.B) {
	m := detailed(held(sampleDetail()), 200, 50)
	m.SetFiles(loadedFiles(bigDiff(60, 200), 0))
	m = press(m, "]", "]", "]", "1")

	for b.Loop() {
		m = press(m, "j")
	}
}

// The spans the jump keys read outlive a tab switch, and one viewport serves
// every tab, so a stale one scrolls the conversation to a diff offset.
func TestTheBraceWalksTheRingOnTheConversationWhateverTheDiffRecorded(t *testing.T) {
	loaded := func() prview.Model {
		m := detailed(held(sampleDetail()), 200, 30)
		m.SetFiles(loadedFiles(sampleFiles(), 0))
		return m
	}

	// Round the tabs once so the diff renders and records its spans.
	toured := press(loaded(), "]", "]", "]", "]")

	for _, k := range []string{"}", "{"} {
		if got, want := press(toured, k).View(), press(loaded(), k).View(); got != want {
			t.Errorf("%q on the conversation does not match a screen that never opened the diff", k)
		}
	}
}

// The rail is suppressed on the Files tab whatever the user asked for, so the
// key cannot read the screen to decide what to do next.
func TestTogglingTheRailOnFilesDoesNotUndoItOnTheConversation(t *testing.T) {
	m := detailed(held(sampleDetail()), 200, 40)
	m.SetFiles(loadedFiles(sampleFiles(), 0))

	m = press(m, "d")
	if strings.Contains(stripANSI(m.View()), "Details") {
		t.Fatal("setup: d did not hide the rail")
	}

	// Out to Files, press it there, and back.
	m = press(m, "]", "]", "]", "d", "[", "[", "[")

	if strings.Contains(stripANSI(m.View()), "Details") {
		t.Error("d on the Files tab turned the hidden rail back on")
	}
	if m.Rail().On {
		t.Error("the rail preference handed to the next screen was flipped too")
	}
}

func TestOutdatedThreadsHoldTheirOrder(t *testing.T) {
	d := sampleDetail()
	for _, at := range []int{9001, 9002, 9003, 9004} {
		d.Threads = append(d.Threads, gh.ReviewThread{
			Path: "internal/gh/client.go", Line: at, Side: gh.SideRight, IsOutdated: true,
			Comments: []gh.Comment{{
				Author: gh.Actor{Login: "nkr"},
				Body:   fmt.Sprintf("stray thread %d", at),
			}},
		})
	}

	// Each build renders the diff from scratch. Ranging the map is ordered at
	// random per range, so one model repainting is not the test: the same
	// comments have to come out the same way every time they are rendered.
	var want []int
	for i := range 20 {
		m := detailed(held(d), 200, 66)
		m.SetFiles(loadedFiles(sampleFiles(), 0))
		m = press(m, "]", "]", "]")

		got := strayOrder(m.View())
		if len(got) != 4 {
			t.Fatalf("build %d: %d stray threads on screen, want 4", i, len(got))
		}
		if want == nil {
			want = got
			continue
		}
		if !slices.Equal(got, want) {
			t.Fatalf("build %d rendered the stray threads as %v, want %v", i, got, want)
		}
	}
}

// strayOrder is the line numbers of the outdated threads, top to bottom.
func strayOrder(frame string) []int {
	var out []int
	for _, line := range strings.Split(stripANSI(frame), "\n") {
		if at := strings.Index(line, "stray thread "); at >= 0 {
			n, err := strconv.Atoi(strings.TrimSpace(line[at+13:])[:4])
			if err == nil {
				out = append(out, n)
			}
		}
	}
	return out
}

// One tick chain serves every tab. Answering only for the tab in front of the
// reader ended it the moment they tabbed away from a diff still in flight, and
// coming back found a glyph that never moved again for the whole request.
func TestTheDiffSpinnerSurvivesLeavingTheTab(t *testing.T) {
	m := detailed(held(sampleDetail()), 120, 20)
	m.SetFiles(store.Files{Status: store.StatusLoading})

	next := m.Init()
	if next == nil {
		t.Fatal("Init started no spinner")
	}

	// Out to Files, which is where the diff is being waited on, then back to
	// the conversation while it is still coming.
	m = press(m, "]", "]", "]", "[", "[", "[")

	// A tick command holds one timer and delivers once, so each has to be
	// called exactly once.
	m, next = m.Update(next())
	if next == nil {
		t.Fatal("the chain ended while the diff was still in flight")
	}

	m = press(m, "]", "]", "]")
	before := stripANSI(m.View())

	m, next = m.Update(next())
	if next == nil {
		t.Fatal("the chain ended after returning to the Files tab")
	}
	if stripANSI(m.View()) == before {
		t.Error("the glyph is frozen while the diff is still coming")
	}
}
