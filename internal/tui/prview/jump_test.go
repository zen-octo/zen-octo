package prview_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/praxis-labs-io/zen-octo/internal/gh"
	"github.com/praxis-labs-io/zen-octo/internal/store"
	"github.com/praxis-labs-io/zen-octo/internal/tui/prview"
)

// jumpHeight is short enough that the fixture's diff does not fit in the pane.
// A frame the whole diff fits inside cannot scroll at all, and every assertion
// about where a jump landed passes on it by accident.
const jumpHeight = 24

// jumping is the conversation on the card v is pressed from, with a diff
// already here.
func jumping(t *testing.T, n int) prview.Model {
	t.Helper()

	m := walked(detailed(held(sampleDetail()), 200, jumpHeight), n)
	m.SetFiles(loadedFiles(sampleFiles(), 0))
	return m
}

// onTab is whether the strip reads this tab as the current one.
func onTab(t *testing.T, frame, name string) bool {
	t.Helper()
	return currentTab(t, frame) == name
}

// landed is what every jump has to produce: the code the thread was written
// against on the screen with the card under it, and the file's own hunk heading
// scrolled away, so the pane is not just showing the top of the file.
//
// The marker is the line above the anchor. They are next to each other in the
// diff, so one on the screen puts the other there too.
func landed(t *testing.T, frame string) {
	t.Helper()

	// Rows of the pane, not lines of the frame: the header is pinned above it.
	top := paneTopAt(frame)
	code := lineOf(t, frame, "min(delay*2, fetchTimeout)") - top
	card := lineOf(t, frame, cardThread) - top

	if code >= card {
		t.Errorf("the code is on row %d and the card answering it on row %d, want the code above it:\n%s",
			code, card, stripANSI(frame))
	}
	if card > 8 {
		t.Errorf("the card opens on row %d, too far down the pane to be where the jump landed:\n%s",
			card, stripANSI(frame))
	}
	if strings.Contains(stripANSI(frame), "@@ -40,4 +40,5 @@") {
		t.Errorf("the diff opened on the file's own top rather than on the thread:\n%s", stripANSI(frame))
	}
}

// A card put on the top row takes the line it answers off the screen with it,
// which leaves the reader looking at a comment about code they cannot see. The
// reply box one tab over follows the same rule for the same reason.
func TestVOpensOnTheCodeTheThreadWasWrittenAgainst(t *testing.T) {
	m := press(jumping(t, tabThread), "v")

	if !onTab(t, m.View(), "Files") {
		t.Fatalf("v did not reach the Files tab:\n%s", stripANSI(m.View()))
	}
	landed(t, m.View())
}

// The diff costs a request of its own, so the first v on a cold tab asks for it
// and lands when it arrives.
func TestVFetchesTheDiffAndJumpsWhenItLands(t *testing.T) {
	m := walked(detailed(held(sampleDetail()), 200, jumpHeight), tabThread)

	next, cmd := key(m, "v")
	if cmd == nil {
		t.Fatal("v asked for nothing with no diff on the screen")
	}
	if got := cmd(); got != (prview.NeedFilesMsg{ID: "PR_412"}) {
		t.Fatalf("v produced %+v, want a request for the diff", got)
	}
	if out := stripANSI(next.View()); !strings.Contains(out, "Loading the diff") {
		t.Fatalf("the tab is not waiting on the diff:\n%s", out)
	}

	next.SetFiles(loadedFiles(sampleFiles(), 0))

	landed(t, next.View())
}

// A file inside a collapsed directory is in no row and no span, so there is
// nothing to point at and nothing to scroll to until every fold above it goes.
func TestVUnfoldsTheDirectoryAboveTheFile(t *testing.T) {
	// Down the column to the directory the file sits in, and fold it.
	folded := press(jumping(t, tabThread), "]", "]", "]", "1", "j", "j", "space")
	if strings.Contains(cursorFile(folded.View()), "client.go") {
		t.Fatal("setup: the cursor is on the file rather than the directory above it")
	}
	if strings.Contains(stripANSI(folded.View()), "This backs off forever.") {
		t.Fatal("setup: the folded directory is still showing the thread")
	}

	landed(t, press(folded, "[", "[", "[", "v").View())
}

// The column and the pane beside it have to agree on which file is on screen.
func TestVMovesTheTreeCursorToTheFile(t *testing.T) {
	m := jumping(t, tabThread)
	if got := cursorFile(m.View()); got == "client.go" {
		t.Fatal("setup: the cursor is already on the file the thread is in")
	}

	if got := cursorFile(press(m, "v").View()); got != "client.go" {
		t.Errorf("the tree cursor is on %q, want the file the thread is in", got)
	}
}

// Switching to a tab that cannot show what was asked for, and saying so from
// there, is two moves to deliver one piece of bad news.
func TestVOnAFileTheDiffDoesNotCarrySaysSoAndStaysPut(t *testing.T) {
	next, cmd := key(jumping(t, tabLocked), "v")
	if cmd == nil {
		t.Fatal("v said nothing about a file that is not in the diff")
	}

	want := prview.ThreadNotInDiffMsg{Path: "internal/tui/app/app.go"}
	if got := cmd(); got != want {
		t.Fatalf("v produced %+v, want %+v", got, want)
	}
	if !onTab(t, next.View(), "Conversation") {
		t.Error("v left the conversation for a tab with nothing on it to show")
	}
}

// A thread GitHub gave no line is a comment on the file as a whole. It is drawn
// nowhere in the diff, so there is nowhere to take the reader.
func TestVOnAThreadWithNoLineDoesNothing(t *testing.T) {
	d := sampleDetail()
	d.Threads[3].Line = 0

	m := walked(detailed(held(d), 200, jumpHeight), tabOther)
	m.SetFiles(loadedFiles(sampleFiles(), 0))

	if got := asked(t, m, "v"); got != nil {
		t.Errorf("v asked for %+v on a thread with no line", got)
	}
}

func TestVIsInertWithNothingFocused(t *testing.T) {
	m := detailed(held(sampleDetail()), 200, jumpHeight)
	m.SetFiles(loadedFiles(sampleFiles(), 0))

	if got := asked(t, m, "v"); got != nil {
		t.Errorf("v asked for %+v with no card focused", got)
	}
}

// The reader moved on while the diff was out. Hauling the page to where they no
// longer are is the one thing every key on this screen refuses to do.
func TestAJumpTheReaderTabbedAwayFromIsDropped(t *testing.T) {
	m := press(walked(detailed(held(sampleDetail()), 200, jumpHeight), tabThread), "v")
	m = press(m, "[", "[", "[")

	m.SetFiles(loadedFiles(sampleFiles(), 0))

	if !onTab(t, m.View(), "Conversation") {
		t.Fatalf("the arriving diff pulled the reader back to the Files tab:\n%s", stripANSI(m.View()))
	}
}

// A diff that never arrived has nothing to land in, and the pane already says
// why. The jump has to let go of it, or the next diff to arrive lands late.
func TestAJumpWaitingOnADiffThatFailedIsDropped(t *testing.T) {
	m := press(walked(detailed(held(sampleDetail()), 200, jumpHeight), tabThread), "v")
	m.SetFiles(store.Files{Status: store.StatusFailed, Err: errors.New("network is down")})

	m.SetFiles(loadedFiles(sampleFiles(), 0))

	out := stripANSI(m.View())
	if !strings.Contains(out, "internal/gh/client.go") {
		t.Errorf("the diff did not open where it normally does, so a dead jump landed late:\n%s", out)
	}
}

// The viewport clamps to its own content, so a thread in the last file cannot
// reach the top row. It still has to be on the screen.
func TestAJumpIntoTheLastFileLandsWithTheThreadOnScreen(t *testing.T) {
	d := sampleDetail()
	d.Threads[3].Path = "internal/tui/prview/files.go"
	d.Threads[3].Line = 2

	m := walked(detailed(held(d), 200, jumpHeight), tabOther)
	m.SetFiles(loadedFiles(sampleFiles(), 0))

	out := stripANSI(press(m, "v").View())
	if !strings.Contains(out, "Is r free after the move?") {
		t.Errorf("the thread in the last file is nowhere on the frame:\n%s", out)
	}
}

// Folding a directory takes the file being read off the tree, and the pane has
// to find another. The jump has to reach back into it, unfolding on the way.
func TestFoldingADirectoryTakesItsThreadOffTheDiffAndTheJumpPutsItBack(t *testing.T) {
	m := press(jumping(t, tabThread), "v")
	landed(t, m.View())

	folded := press(m, "1", "k", "space")
	if strings.Contains(stripANSI(folded.View()), "This backs off forever.") {
		t.Fatal("setup: the folded directory is still showing the thread")
	}

	// The ring is still on the thread, so the conversation needs no walking.
	landed(t, press(folded, "[", "[", "[", "v").View())
}

// A diff that failed is asked for again rather than landed on. The pane carries
// no retry of its own, and pressing v is the reader asking to see the code.
func TestVAsksAgainForADiffThatFailed(t *testing.T) {
	m := walked(detailed(held(sampleDetail()), 200, jumpHeight), tabThread)

	// The tab has been opened once already and the fetch came back empty.
	m = press(m, "]", "]", "]")
	m.SetFiles(store.Files{Status: store.StatusFailed, Err: errors.New("502 Bad Gateway")})
	m = press(m, "[", "[", "[")

	next, cmd := key(m, "v")
	if cmd == nil {
		t.Fatal("v asked for nothing against a diff that failed")
	}
	if got := cmd(); got != (prview.NeedFilesMsg{ID: "PR_412"}) {
		t.Fatalf("v produced %+v, want another request for the diff", got)
	}

	next.SetFiles(loadedFiles(sampleFiles(), 0))
	landed(t, next.View())
}

// tallFiles is a diff whose tree does not fit the column, which is what makes
// the cursor's own scroll position observable.
func tallFiles() []gh.ChangedFile {
	files := make([]gh.ChangedFile, 0, 31)
	for i := range 30 {
		files = append(files, gh.ChangedFile{
			Path: fmt.Sprintf("internal/gh/a%02d.go", i), Status: gh.FileModified, Additions: 1,
			Hunks: []gh.Hunk{{
				Header: "@@ -1,1 +1,2 @@",
				Lines: []gh.DiffLine{
					{Kind: gh.DiffContext, Old: 1, New: 1, Content: "package gh"},
				},
			}},
		})
	}
	return append(files, sampleFiles()[0])
}

// The column can only be scrolled once it holds the rows. Scrolled against the
// tree as it was folded, the offset clamps and the cursor lands off screen.
func TestVLeavesTheTreeCursorOnScreenAfterUnfolding(t *testing.T) {
	m := walked(detailed(held(sampleDetail()), 200, jumpHeight), tabThread)
	m.SetFiles(loadedFiles(tallFiles(), 0))

	// Up to the one directory and fold it, which takes every file out of the
	// tree. The cursor opens on the first file, one row under it.
	folded := press(m, "]", "]", "]", "1", "k", "space")
	if got := selectedRow(folded.View()); !strings.Contains(got, "internal/gh/") {
		t.Fatalf("setup: the fold landed on %q rather than the directory", strings.TrimSpace(got))
	}
	if strings.Contains(stripANSI(folded.View()), "a00.go") {
		t.Fatal("setup: the directory did not fold")
	}

	back := press(folded, "[", "[", "[", "v")
	if got := cursorFile(back.View()); got != "client.go" {
		t.Errorf("the tree cursor reads %q, want it on the file and on the screen:\n%s",
			got, stripANSI(back.View()))
	}
}
