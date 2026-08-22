package prview_test

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"

	"github.com/praxis-labs-io/zen-octo/internal/gh"
	"github.com/praxis-labs-io/zen-octo/internal/tui/prview"
)

// Where the ring stops on the fixture, for the blocks the write keys act on.
const (
	tabDescription = 1
	tabComment     = 2
	tabReview      = 3
)

// writable is the fixture as the reader's own writing, with GitHub's
// permissions on it. The sample carries neither, which is somebody looking at a
// conversation they had no part in, so every key here would be inert without
// this.
//
// The review's own body is deletable and its comment is not, which is the pair
// the delete key has to tell apart: GitHub answers true on both and has a call
// for only one of them.
func writable() gh.PullRequestDetail {
	d := sampleDetail()

	for _, item := range d.Timeline {
		switch item.Kind {
		case gh.TimelineComment:
			mine(item.Comment)
			// Three paragraphs, so the card has a height an opening box could
			// change.
			item.Comment.Body += "\n\nThe cap holds through a retry storm.\n\nRunbook updated."
		case gh.TimelineReview:
			mine(item.Comment)
		}
	}

	for i := range d.Threads[0].Comments {
		mine(&d.Threads[0].Comments[i])
	}
	return d
}

// mine marks a comment as the reader's own, which GitHub will take a rewrite
// of. Both answers come from GitHub and both are needed: a maintainer may edit
// anybody's comment and this client offers that on nobody's.
func mine(c *gh.Comment) {
	c.ViewerDidAuthor, c.CanEdit, c.CanDelete = true, true, true
}

// editing walks the ring to a block and opens the box over it.
func editing(n int) prview.Model {
	return press(onWritable(n), "e")
}

func onWritable(n int) prview.Model {
	return walked(viewing(writable(), 200, 60), n)
}

// viewing is the screen with the viewer named, which is what the description
// reads to know whose writing it is: a pull request carries no viewerDidAuthor.
func viewing(d gh.PullRequestDetail, width, height int) prview.Model {
	m := detailed(held(d), width, height)
	m.SetViewer(gh.Actor{Login: "drucial"})
	return m
}

// The box goes where the words were, inside the card they were in. A comment
// being rewritten is not a new comment, and putting the box at the foot of the
// page would leave the old words on screen above it.
func TestTheEditBoxOpensInPlaceOfTheWordsItReplaces(t *testing.T) {
	out := stripANSI(editing(tabComment).View())

	head := strings.Index(out, "edit this comment")
	box := strings.Index(out, "Save")
	foot := strings.Index(out, "write a comment")

	switch {
	case box < 0:
		t.Fatalf("e opened no box:\n%s", out)
	case head < 0 || box < head:
		t.Error("the box is above the heading of the card it is inside")
	case foot >= 0 && box > foot:
		t.Error("the box is at the foot of the page rather than in the card")
	}

	// The heading says what is happening to the card, the way the box at the
	// foot of the page says what is being written there.
	if !strings.Contains(out, "edit this comment") {
		t.Errorf("the card does not say it is being edited:\n%s", out)
	}
	// The words are in the box rather than rendered under it.
	if !strings.Contains(out, "Coverage held at 84.2%") {
		t.Errorf("the box did not open on the comment's own words:\n%s", out)
	}
}

// The box is the height of the words it replaces, so opening one costs the
// single row its button takes and nothing more. A box of its own fixed height
// would shrink a long comment to a window onto itself and balloon a short one.
func TestTheEditBoxIsTheHeightOfTheWordsItReplaces(t *testing.T) {
	m := onWritable(tabComment)
	below := "nkr · requested changes"

	before := lineOf(t, m.View(), below)
	after := lineOf(t, press(m, "e").View(), below)

	if got := after - before; got != 1 {
		t.Errorf("opening the box moved the card below it by %d lines, want 1 for the button", got)
	}
}

// And it grows as the writing does, rather than scrolling inside a fixed frame.
func TestTheEditBoxGrowsWithWhatIsTypedIntoIt(t *testing.T) {
	m := press(onWritable(tabComment), "e")
	below := "nkr · requested changes"

	before := lineOf(t, m.View(), below)
	after := lineOf(t, typed(m, "\n\n").View(), below)

	if got := after - before; got != 2 {
		t.Errorf("two new lines moved the card below by %d lines, want 2", got)
	}
}

// The compose card holds whatever was typed into it. If the two shared one
// buffer, opening this box would throw a half-written comment away.
func TestOpeningTheEditBoxKeepsAHalfWrittenComment(t *testing.T) {
	m := typed(press(viewing(writable(), 200, 60), "c"), "half a thought")
	m = press(m, "esc")

	m = press(walked(fromTop(m), tabComment), "e")
	if !strings.Contains(stripANSI(m.View()), "Coverage held at 84.2%") {
		t.Fatal("the edit box did not open on the comment's own words")
	}

	// G, because the compose card closes a conversation longer than the window
	// and the box being open has scrolled it out of sight.
	out := stripANSI(press(m, "esc", "G").View())
	if !strings.Contains(out, "half a thought") {
		t.Errorf("opening the edit box took the comment being written:\n%s", out)
	}
}

// The box stops growing at the pane. Past that the card's foot goes off the
// screen, and the foot is where the control that saves the words is: a reader
// writing into something with no visible end has no way out but a chord nothing
// on screen has named.
func TestABoxNeverGrowsPastThePane(t *testing.T) {
	d := writable()
	for _, item := range d.Timeline {
		if item.Kind == gh.TimelineComment {
			item.Comment.Body = strings.Repeat("A line of it.\n\n", 40)
		}
	}

	m := press(walked(viewing(d, 200, 24), tabComment), "e")

	out := stripANSI(m.View())
	if !strings.Contains(out, "edit this comment") {
		t.Errorf("the card's heading is off the screen:\n%s", out)
	}
	if !strings.Contains(out, "Save") {
		t.Errorf("the card's foot is off the screen:\n%s", out)
	}
}

// The line being written is on the screen wherever the box is: under the cap
// the textarea scrolls inside itself, and above it the page follows the caret.
func TestTheCaretStaysOnTheScreenInALongComment(t *testing.T) {
	d := writable()
	for _, item := range d.Timeline {
		if item.Kind == gh.TimelineComment {
			item.Comment.Body = strings.Repeat("A line of it.\n\n", 40)
		}
	}

	// A window shorter than the comment, so the box opens taller than the pane
	// and the caret lands at the end of it.
	m := press(walked(viewing(d, 200, 24), tabComment), "e")
	m = typed(m, "the caret is here")

	if out := stripANSI(m.View()); !strings.Contains(out, "the caret is here") {
		t.Errorf("the line being written is off the screen:\n%s", out)
	}
}

// The compose card grows the same way, and its caret is kept the same way.
func TestTheCommentBoxGrowsAndKeepsItsCaretOnTheScreen(t *testing.T) {
	m := press(viewing(writable(), 200, 24), "c")
	m = typed(m, strings.Repeat("A line of it.\n", 30)+"the caret is here")

	if out := stripANSI(m.View()); !strings.Contains(out, "the caret is here") {
		t.Errorf("the line being written is off the screen:\n%s", out)
	}
}

// esc leaves the comment as it was. Nothing was written, so nothing is saved,
// and the card goes back to rendering its markdown.
func TestEscapeClosesTheEditBoxAndLeavesTheComment(t *testing.T) {
	m := onWritable(tabComment)
	before := m.View()

	if after := press(press(m, "e"), "esc").View(); after != before {
		t.Errorf("esc left the card changed:\n%s", stripANSI(after))
	}
}

// The chord sends the write. It carries what the box holds, addressed to the
// comment by node id and kind: the kind is what picks the mutation.
func TestTheEditKeySendsTheNewWordsForTheFocusedComment(t *testing.T) {
	m := editing(tabComment)
	m = typed(m, " Updated.")

	_, cmd := chord(m)
	msg, ok := runCmd(cmd).(prview.EditCommentMsg)
	if !ok {
		t.Fatalf("the chord asked for %T, want an EditCommentMsg", runCmd(cmd))
	}

	if msg.CommentID != "IC_octobot" {
		t.Errorf("CommentID = %q, want the focused comment", msg.CommentID)
	}
	if msg.Kind != gh.CommentIssue {
		t.Errorf("Kind = %q, want an issue comment", msg.Kind)
	}
	if msg.ThreadID != "" {
		t.Errorf("ThreadID = %q, want none on a top-level comment", msg.ThreadID)
	}
	if !strings.HasSuffix(msg.Body, "Updated.") {
		t.Errorf("Body = %q, want what the box was left holding", msg.Body)
	}
}

// A review's own words go through a third mutation, so the kind has to be the
// one the detail carried rather than the one a comment usually is.
func TestEditingAReviewSendsTheReviewKind(t *testing.T) {
	m := typed(editing(tabReview), "!")

	_, cmd := chord(m)
	msg, ok := runCmd(cmd).(prview.EditCommentMsg)
	if !ok {
		t.Fatalf("the chord asked for %T, want an EditCommentMsg", runCmd(cmd))
	}
	if msg.Kind != gh.CommentReview {
		t.Errorf("Kind = %q, want a review", msg.Kind)
	}
	if msg.CommentID != "REV_1" {
		t.Errorf("CommentID = %q, want the review", msg.CommentID)
	}
}

// Every card is a stop, so the write keys act on the comment the card the ring
// is on holds: a thread's own card holds the comment it was opened with, and a
// reply holds itself.
func TestEditingAThreadTakesTheCommentTheSubCursorIsOn(t *testing.T) {
	m := typed(editing(tabThread), "!")

	_, cmd := chord(m)
	msg, ok := runCmd(cmd).(prview.EditCommentMsg)
	if !ok {
		t.Fatalf("the chord asked for %T, want an EditCommentMsg", runCmd(cmd))
	}

	if msg.CommentID != "RC_1" {
		t.Errorf("CommentID = %q, want the comment that opened the thread", msg.CommentID)
	}
	if msg.ThreadID != "RT_1" {
		t.Errorf("ThreadID = %q, want the thread it sits in", msg.ThreadID)
	}
	if msg.Kind != gh.CommentThread {
		t.Errorf("Kind = %q, want a review comment", msg.Kind)
	}

	// The reply is a stop of its own, and the key follows the ring onto it.
	_, cmd = chord(typed(press(onWritable(tabThread), "}", "e"), "!"))
	if msg, _ := runCmd(cmd).(prview.EditCommentMsg); msg.CommentID != "RC_4" {
		t.Errorf("CommentID = %q on the reply, want the reply itself", msg.CommentID)
	}
}

// The description is not a comment to GitHub, so it goes through the mutation
// that writes the pull request.
func TestEditingTheDescriptionSendsAPullRequestWrite(t *testing.T) {
	m := typed(editing(tabDescription), " Rewritten.")

	_, cmd := chord(m)
	msg, ok := runCmd(cmd).(prview.SetBodyMsg)
	if !ok {
		t.Fatalf("the chord asked for %T, want a SetBodyMsg", runCmd(cmd))
	}
	if !strings.HasSuffix(msg.Body, "Rewritten.") {
		t.Errorf("Body = %q, want what the box was left holding", msg.Body)
	}
}

// The permission is GitHub's answer. Without it the key does nothing, rather
// than opening a box on a write that comes back refused.
func TestEditIsInertWhereGitHubSaysTheViewerMayNot(t *testing.T) {
	m := walked(detailed(held(sampleDetail()), 200, 60), tabComment)

	before := m.View()
	if after := press(m, "e").View(); after != before {
		t.Errorf("e opened a box on a comment the viewer may not edit:\n%s", stripANSI(after))
	}
}

// A comment already answering for a write is not one to open. Two rewrites out
// at once settle in whatever order the responses arrive.
func TestEditIsInertOnACommentAlreadyBeingWritten(t *testing.T) {
	d := writable()
	for _, item := range d.Timeline {
		if item.Kind == gh.TimelineComment {
			item.Comment.Editing = true
		}
	}

	m := walked(viewing(d, 200, 60), tabComment)
	before := m.View()
	if after := press(m, "e").View(); after != before {
		t.Errorf("e opened a box on a comment with a write already out:\n%s", stripANSI(after))
	}
}

// A maintainer may rewrite anybody's comment as far as GitHub is concerned.
// This client offers that on nobody's: the words are somebody else's, and
// editing them would put them under their name.
func TestNeitherKeyTouchesSomebodyElsesComment(t *testing.T) {
	d := writable()
	for _, item := range d.Timeline {
		if item.Kind == gh.TimelineComment {
			item.Comment.ViewerDidAuthor = false
		}
	}

	m := walked(viewing(d, 200, 60), tabComment)
	before := m.View()

	if after := press(m, "e").View(); after != before {
		t.Errorf("e opened a box on somebody else's comment:\n%s", stripANSI(after))
	}
	if after := press(m, "D").View(); after != before {
		t.Errorf("D opened a confirm over somebody else's comment:\n%s", stripANSI(after))
	}
	if out := stripANSI(before); strings.Contains(out, "D delete") {
		t.Error("the card names keys it does not answer to")
	}
}

// The description is not a comment and carries no viewerDidAuthor, so whose it
// is comes from the login. A pull request somebody else opened is theirs.
func TestTheDescriptionIsOnlyEditableByWhoeverOpenedIt(t *testing.T) {
	d := writable()
	d.Author = gh.Actor{Login: "nkr"}

	m := walked(viewing(d, 200, 60), tabDescription)
	before := m.View()

	if after := press(m, "e").View(); after != before {
		t.Errorf("e opened a box on somebody else's description:\n%s", stripANSI(after))
	}
}

// The key reads the ring, and a tab with no ring focuses nothing for it to
// read. The conversation always has a cursor now, so Commits is where the
// refusal is still visible.
func TestEditNeedsARingToRead(t *testing.T) {
	m := press(viewing(writable(), 200, 60), "]")

	if after := press(m, "e").View(); after != m.View() {
		t.Error("e opened a box on a tab with no ring")
	}
}

// D asks first. The cursor opens on the answer that changes nothing, so enter
// with no movement closes the modal and writes nothing.
func TestDeleteAsksBeforeItWrites(t *testing.T) {
	m := press(onWritable(tabComment), "D")

	out := stripANSI(m.View())
	if !strings.Contains(out, "Delete this comment?") {
		t.Fatalf("D opened no confirm:\n%s", out)
	}

	m, cmd := pressed(m, "enter")
	if msg := runCmd(cmd); msg != nil {
		t.Errorf("enter on the first row asked for %T, want nothing", msg)
	}
	if strings.Contains(stripANSI(m.View()), "Delete this comment?") {
		t.Error("the confirm is still up after an answer")
	}
	// The comment is still there, which is the whole of what cancelling means.
	if !strings.Contains(stripANSI(m.View()), "Coverage held at 84.2%") {
		t.Error("the comment went with a cancelled delete")
	}
}

// Confirming is a second, deliberate key: down onto the row that says delete,
// then enter.
func TestConfirmingTheDeleteSendsTheWrite(t *testing.T) {
	m := press(onWritable(tabComment), "D", "j")

	_, cmd := pressed(m, "enter")
	msg, ok := runCmd(cmd).(prview.DeleteCommentMsg)
	if !ok {
		t.Fatalf("the confirm asked for %T, want a DeleteCommentMsg", runCmd(cmd))
	}

	if msg.CommentID != "IC_octobot" {
		t.Errorf("CommentID = %q, want the focused comment", msg.CommentID)
	}
	if msg.Kind != gh.CommentIssue {
		t.Errorf("Kind = %q, want an issue comment", msg.Kind)
	}
}

// esc backs out of the confirm the way it backs out of every other modal.
func TestEscapeClosesTheDeleteConfirm(t *testing.T) {
	m := onWritable(tabComment)
	before := m.View()

	if after := press(m, "D", "esc").View(); after != before {
		t.Errorf("esc left the confirm changed:\n%s", stripANSI(after))
	}
}

// viewerCanDelete comes back true on a submitted review and there is no call
// that deletes one. A key that opens a confirm on a write GitHub cannot take is
// worse than one that does nothing.
func TestDeleteIsInertOnAReviewsOwnWords(t *testing.T) {
	m := onWritable(tabReview)

	before := m.View()
	if after := press(m, "D").View(); after != before {
		t.Errorf("D opened a confirm over a review body:\n%s", stripANSI(after))
	}
}

// The description cannot be deleted at all: it is a field of the pull request
// rather than something somebody said.
func TestDeleteIsInertOnTheDescription(t *testing.T) {
	m := onWritable(tabDescription)

	before := m.View()
	if after := press(m, "D").View(); after != before {
		t.Errorf("D opened a confirm over the description:\n%s", stripANSI(after))
	}
}

// A card names the keys it answers to and no others.
func TestTheCardNamesTheWriteKeysItAnswersTo(t *testing.T) {
	out := stripANSI(onWritable(tabComment).View())
	if !strings.Contains(out, "e edit") || !strings.Contains(out, "D delete") {
		t.Errorf("the focused card names neither write key:\n%s", out)
	}

	// The review has one of the two, so the line has to differ.
	review := stripANSI(onWritable(tabReview).View())
	if !strings.Contains(review, "e edit") {
		t.Error("the review card does not name the key that edits it")
	}
	if strings.Contains(review, "D delete") {
		t.Error("the review card names a delete key that does nothing")
	}

	// Nothing named where GitHub says no.
	plain := stripANSI(walked(detailed(held(sampleDetail()), 200, 60), tabComment).View())
	if strings.Contains(plain, "e edit") || strings.Contains(plain, "D delete") {
		t.Errorf("a card the viewer may not write to names the keys anyway:\n%s", plain)
	}
}

// The box carries its own hints, so the card's line goes: two lines of keys
// over one card would name keys that are not live.
func TestTheCardsHintsGiveWayToTheBoxs(t *testing.T) {
	out := stripANSI(editing(tabComment).View())

	// "e edit" would match inside the box's own "ctrl+e editor", so the delete
	// key is what says whether the card's line is still there.
	if strings.Contains(out, "D delete") {
		t.Error("the card still names the keys it answered to before the box opened")
	}
	// Discard rather than done: nothing is kept, and the key that drops a
	// rewrite says so before it is pressed.
	if !strings.Contains(out, "esc discard") {
		t.Errorf("the box does not name the key that closes it:\n%s", out)
	}
	if !strings.Contains(out, "⏎ save") {
		t.Errorf("the box does not name the key that saves it:\n%s", out)
	}
}

// The frame is the size it was given, box or no box. A card that grows a
// textarea inside it is the one place a block changes height without the page
// being rebuilt around it.
func TestTheFrameHoldsItsSizeWithAnEditBoxOpen(t *testing.T) {
	for _, size := range []struct{ width, height int }{
		{200, 60}, {160, 24}, {100, 20},
	} {
		m := press(walked(detailed(held(writable()), size.width, size.height), tabComment), "e")

		lines := strings.Split(m.View(), "\n")
		if len(lines) != size.height {
			t.Errorf("%dx%d rendered %d lines, want %d",
				size.width, size.height, len(lines), size.height)
		}
		for i, line := range lines {
			if w := lipgloss.Width(line); w > size.width {
				t.Errorf("%dx%d line %d is %d wide, want at most %d",
					size.width, size.height, i, w, size.width)
			}
		}
	}
}

// A key the box does not answer is text, the way it is in the compose card:
// the screen stands aside while a box has the keyboard.
func TestTheEditBoxTakesEveryOtherKeyAsText(t *testing.T) {
	m := typed(editing(tabComment), "q")

	if !strings.Contains(stripANSI(m.View()), "Save") {
		t.Error("q closed the box rather than being typed into it")
	}

	// And the screen has it back once the box is closed.
	if _, cmd := pressed(press(m, "esc"), "q"); runCmd(cmd) != nil {
		t.Error("the screen answered q while the box still had the keyboard")
	}
}

// The words survive a write that failed, which is the one thing on this screen
// that cannot be fetched again.
func TestAFailedEditPutsTheWordsBackInTheBox(t *testing.T) {
	m := editing(tabComment)
	m = typed(m, " Updated.")
	m, _ = chord(m)

	// The card is back to what GitHub has while the write is out and after it
	// fails; the box is what carries the words.
	m.RestoreEdit("IC_octobot", "Coverage held at 84.2%. Updated.")

	out := stripANSI(m.View())
	if !strings.Contains(out, "Updated.") {
		t.Errorf("the words did not come back:\n%s", out)
	}
	if !strings.Contains(out, "Save") {
		t.Error("the box did not reopen on the comment")
	}
}

// esc says discard and discards. A reply's words are held against the thread
// they answer, because esc there is how a reader looks at the code above what
// they are writing. An edit's are not: the comment is on GitHub and can be read
// again, and words held here would open over one that had moved on since and go
// back as the newer of the two.
func TestEscapeThrowsAwayWhatWasTypedIntoAnEditBox(t *testing.T) {
	m := press(typed(press(onWritable(tabComment), "e"), " Discard me."), "esc")

	out := stripANSI(press(m, "e").View())
	if strings.Contains(out, "Discard me.") {
		t.Errorf("the box reopened on words esc said it would discard:\n%s", out)
	}
	if !strings.Contains(out, "Coverage held at 84.2%") {
		t.Errorf("the box did not reopen on the comment as GitHub has it:\n%s", out)
	}
}

// A comment inside a thread pays for two cards rather than one, and a box that
// spent the pane as though it were a card of its own would push the button off
// the bottom of the deeper of them.
func TestABoxInsideAThreadKeepsItsButtonOnTheScreen(t *testing.T) {
	d := writable()
	for i := range d.Threads[0].Comments {
		d.Threads[0].Comments[i].Body = strings.Repeat("A line of it.\n\n", 40)
	}

	m := press(walked(viewing(d, 200, 24), tabThread), "e")

	if out := stripANSI(m.View()); !strings.Contains(out, "Save") {
		t.Errorf("the box's button is off the screen:\n%s", out)
	}
}

// And the caret keeps it there. A box grows as it is typed into, so the button
// under it is the first thing to go over the edge, and the reader is then
// writing into something with no visible end.
func TestTypingInAThreadKeepsTheButtonOnTheScreen(t *testing.T) {
	d := writable()
	for i := range d.Threads[0].Comments {
		d.Threads[0].Comments[i].Body = strings.Repeat("A line of it.\n\n", 40)
	}

	m := typed(press(walked(viewing(d, 200, 24), tabThread), "e"), "\nthe caret is here")

	out := stripANSI(m.View())
	if !strings.Contains(out, "the caret is here") {
		t.Errorf("the line being written is off the screen:\n%s", out)
	}
	if !strings.Contains(out, "Save") {
		t.Errorf("the button went off the screen as the box grew:\n%s", out)
	}
}

// A comment already on GitHub goes back as it stands, where a new one is sent
// trimmed. Four spaces at the front of a comment are a code block, and a key
// pressed to fix a typo further down must not reflow somebody's markdown on the
// way past.
func TestAnEditKeepsTheWhitespaceAroundTheWordsItSends(t *testing.T) {
	d := writable()
	for _, item := range d.Timeline {
		if item.Kind == gh.TimelineComment {
			item.Comment.Body = "    indented := true"
		}
	}

	m := typed(press(walked(viewing(d, 200, 60), tabComment), "e"), " // and stays")
	_, cmd := chord(m)

	msg, ok := runCmd(cmd).(prview.EditCommentMsg)
	if !ok {
		t.Fatalf("the chord asked for %T, want an EditCommentMsg", runCmd(cmd))
	}
	if msg.Body != "    indented := true // and stays" {
		t.Errorf("Body = %q, want the leading indent kept", msg.Body)
	}
}
