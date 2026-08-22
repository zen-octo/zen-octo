package prview_test

import (
	"strconv"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/praxis-labs-io/zen-octo/internal/gh"
	"github.com/praxis-labs-io/zen-octo/internal/tui/prview"
)

// Markers the fixture uses nowhere else. "Thumbs down" is a row in the list and
// appears on no card, so an assertion on it cannot pass off the rail behind the
// modal.
const (
	reactRow    = "Thumbs down"
	reactModal  = "React"
	thumbsUp    = "👍"
	rocketPill  = "🚀"
	confusedRow = "Confused"
)

// reactive is sampleDetail with reactions on it. The description and the
// timeline comment carry some, the review carries none, and the eight are
// offered on every block that will take the key.
func reactive() gh.PullRequestDetail {
	d := sampleDetail()
	d.Viewer.CanReact = true
	d.Reactions = []gh.Reaction{{Content: gh.ReactionRocket, Count: 3}}

	for _, item := range d.Timeline {
		if c := item.Comment; c != nil {
			c.CanReact = true
		}
	}
	d.Timeline[0].Comment.Reactions = []gh.Reaction{
		{Content: gh.ReactionThumbsUp, Count: 4, Viewer: true},
		{Content: gh.ReactionEyes, Count: 1},
	}

	for i := range d.Threads {
		for j := range d.Threads[i].Comments {
			d.Threads[i].Comments[j].CanReact = true
		}
	}
	d.Threads[0].Comments[0].Reactions = []gh.Reaction{
		{Content: gh.ReactionHeart, Count: 2},
	}
	return d
}

// reacting puts the ring on a card by step count over a detail with reactions.
func reacting(n int) prview.Model {
	return walked(detailed(held(reactive()), 200, 60), n)
}

// reactAsked presses a key and insists it asked the root to toggle a reaction.
func reactAsked(t *testing.T, m prview.Model, k string) prview.ReactMsg {
	t.Helper()

	got := asked(t, m, k)
	msg, ok := got.(prview.ReactMsg)
	if !ok {
		t.Fatalf("%s produced %T, want a ReactMsg", k, got)
	}
	return msg
}

// enter is the press that applies whatever the modal is left on.
func enter(m prview.Model) (prview.Model, tea.Cmd) {
	return m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
}

// The list is GitHub's eight, always all of them. One offering only what
// somebody had already given would be empty the first time it opened.
func TestReactOpensTheEight(t *testing.T) {
	out := stripANSI(press(reacting(2), "+").View())

	if !strings.Contains(out, reactModal) {
		t.Fatalf("+ opened no list:\n%s", out)
	}
	for _, want := range []string{"Thumbs up", reactRow, "Laugh", "Hooray",
		confusedRow, "Heart", "Rocket", "Eyes"} {
		if !strings.Contains(out, want) {
			t.Errorf("the list does not offer %q", want)
		}
	}
}

// The counts ride on the rows, so the reader can see what they are joining.
func TestTheListNamesWhatEachReactionAlreadyHas(t *testing.T) {
	out := stripANSI(press(reacting(2), "+").View())

	line := rowFor(t, out, "Thumbs up")
	if !strings.Contains(line, "4") {
		t.Errorf("the thumbs up row is %q, want the four it already has", line)
	}
	if got := rowFor(t, out, reactRow); strings.ContainsAny(got, "0123456789") {
		t.Errorf("a reaction nobody gave is numbered: %q", got)
	}
}

// GitHub's flag is the whole of the question, and a key that opens a write
// GitHub refuses is worse than one that does nothing.
func TestReactIsInertWhereGitHubSaysNo(t *testing.T) {
	// sampleDetail carries CanReact nowhere, which is what a viewer with no
	// write access to the repository gets back.
	m := walked(detailed(held(sampleDetail()), 200, 60), 2)

	if out := stripANSI(press(m, "+").View()); strings.Contains(out, reactRow) {
		t.Errorf("+ opened a list on a comment GitHub will not take one for:\n%s", out)
	}
}

// The list opens on what the viewer has already given, which is what makes
// enter with no movement a toggle rather than a change to whatever sorted
// first. Pressing it there takes the reaction back off.
func TestEnterOnAReactionAlreadyGivenTakesItBack(t *testing.T) {
	got := reactAsked(t, press(reacting(2), "+"), "enter")

	want := prview.ReactMsg{
		ID: "PR_412", SubjectID: "IC_octobot", CommentID: "IC_octobot",
		Content: gh.ReactionThumbsUp,
	}
	if got != want {
		t.Errorf("enter asked for %+v, want %+v", got, want)
	}
}

// And on one the viewer is not in, the same press adds it.
func TestEnterOnAReactionNotGivenAddsIt(t *testing.T) {
	m := press(reacting(2), "+")

	// Down from thumbs up, which is where the list opened.
	m, _ = arrow(m, tea.KeyDown)

	_, cmd := enter(m)
	if cmd == nil {
		t.Fatal("enter sent nothing")
	}
	msg, ok := cmd().(prview.ReactMsg)
	if !ok {
		t.Fatalf("enter produced %T, want a ReactMsg", cmd())
	}
	if msg.Content != gh.ReactionThumbsDown || !msg.On {
		t.Errorf("enter asked for %+v, want thumbs down added", msg)
	}
}

// The description is not a comment. It is a field of the pull request, and the
// pull request is the node a reaction to it names.
func TestReactOnTheDescriptionAddressesThePullRequest(t *testing.T) {
	got := reactAsked(t, press(reacting(1), "+"), "enter")

	if got.SubjectID != "PR_412" {
		t.Errorf("SubjectID = %q, want the pull request's own node", got.SubjectID)
	}
	if got.CommentID != "" || got.ThreadID != "" {
		t.Errorf("msg = %+v, want no comment for the store to look for", got)
	}
}

// A reply is a card of its own, so the key reaches it the way every other write
// key does.
func TestReactReachesAReply(t *testing.T) {
	got := reactAsked(t, press(reacting(tabReply), "+"), "enter")

	if got.CommentID != "RC_4" || got.ThreadID != "RT_1" {
		t.Errorf("msg = %+v, want the reply inside its thread", got)
	}
	if !got.On {
		t.Error("a reply nobody has reacted to came back as a removal")
	}
}

// A thread's own card resolves to the comment that opened it, which is the one
// the write keys act on there.
func TestReactOnAThreadReachesTheCommentThatOpenedIt(t *testing.T) {
	got := reactAsked(t, press(reacting(tabThread), "+"), "enter")

	if got.CommentID != "RC_1" || got.ThreadID != "RT_1" {
		t.Errorf("msg = %+v, want the comment the thread was opened with", got)
	}
}

// The pills render on every card that has any, not on the lit one alone. A row
// that came and went with the focus would change a card's height on every press
// of the motion key.
func TestThePillsRenderOnACardNobodyIsOn(t *testing.T) {
	out := stripANSI(detailed(held(reactive()), 200, 60).View())

	if !strings.Contains(out, thumbsUp+" 4") {
		t.Errorf("the pills are missing from an unfocused card:\n%s", out)
	}
	if !strings.Contains(out, rocketPill+" 3") {
		t.Error("the description's own pills are missing")
	}
}

// The same page with the ring on the card, so the pills are not a thing the
// focus turns on.
func TestThePillsDoNotMoveWhenTheRingArrives(t *testing.T) {
	quiet := strings.Count(stripANSI(detailed(held(reactive()), 200, 60).View()), "\n")
	lit := strings.Count(stripANSI(reacting(2).View()), "\n")

	if quiet != lit {
		t.Errorf("the page is %d lines with the ring on the card and %d without", lit, quiet)
	}
}

// A block nobody has reacted to draws no row at all. An empty one would be a
// blank line under every comment on the page.
func TestACardWithNoReactionsDrawsNoRow(t *testing.T) {
	d := reactive()
	d.Timeline[0].Comment.Reactions = nil

	out := stripANSI(detailed(held(d), 200, 60).View())
	if strings.Contains(out, thumbsUp) {
		t.Errorf("a card with no reactions drew a pill:\n%s", out)
	}
}

// The card names the key, and only where GitHub will take the press. A key
// listed on a card it does nothing to is worse than one nobody found.
func TestTheCardNamesTheReactKey(t *testing.T) {
	if got := stripANSI(reacting(2).View()); !strings.Contains(got, "+ react") {
		t.Errorf("the focused card does not name the react key:\n%s", got)
	}

	m := walked(detailed(held(sampleDetail()), 200, 60), 2)
	if got := stripANSI(m.View()); strings.Contains(got, "+ react") {
		t.Error("a card GitHub will take no reaction for names the key")
	}
}

// Two toggles on one reaction settle in the order the responses arrive, which
// is not the order they were pressed. The row is inert until the first lands.
func TestAReactionStillOutTakesNoSecondPress(t *testing.T) {
	d := reactive()
	d.Timeline[0].Comment.Reactions = []gh.Reaction{
		{Content: gh.ReactionThumbsUp, Count: 4, Viewer: true, Pending: true},
	}

	m := press(walked(detailed(held(d), 200, 60), 2), "+")
	if _, cmd := enter(m); cmd != nil {
		t.Errorf("enter on a reaction already being written sent %+v", cmd())
	}
}

// The pills go under the words, and cardBoxAt measures down to an open box. A
// row added above one would move the box's first cell without moving the
// arithmetic that follows the caret, and the page would scroll to the wrong
// line on every keystroke.
func TestPillsDoNotMoveAnOpenBox(t *testing.T) {
	// The viewer's own comment, since e only opens on one. sampleDetail's
	// timeline comment is octobot's, so this is the description, which carries
	// pills of its own in the fixture.
	withPills := boxOffset(t, press(walked(viewing(reactive(), 200, 60), 1), "e").View())

	bare := reactive()
	bare.Reactions = nil
	withoutPills := boxOffset(t, press(walked(viewing(bare, 200, 60), 1), "e").View())

	if withPills != withoutPills {
		t.Errorf("the box opens %d lines under its heading with pills and %d without",
			withPills, withoutPills)
	}
}

// boxOffset is how far the box's button sits below the card heading it opened
// on, which is the arithmetic a pill row could break.
func boxOffset(t *testing.T, frame string) int {
	t.Helper()

	lines := strings.Split(stripANSI(frame), "\n")
	head, box := -1, -1
	for i, line := range lines {
		if head < 0 && strings.Contains(line, "edit this description") {
			head = i
		}
		if strings.Contains(line, "Save") {
			box = i
			break
		}
	}
	if head < 0 || box < 0 {
		t.Fatalf("no box opened on the description:\n%s", frame)
	}
	return box - head
}

// The pills go while a box is over the block, the way the card's byline does.
// The box carries its own button on its last row, and pills under that read as
// a comment to react to rather than as one being written.
func TestThePillsGoWhileTheBoxIsOverTheBlock(t *testing.T) {
	m := press(walked(viewing(reactive(), 200, 60), 1), "e")

	out := stripANSI(m.View())
	if !strings.Contains(out, "Save") {
		t.Fatalf("e opened no box:\n%s", out)
	}
	if strings.Contains(out, rocketPill) {
		t.Errorf("the description's pills are under the box's own button:\n%s", out)
	}
}

// And they come back once it closes. The reaction was never touched.
func TestThePillsComeBackWhenTheBoxCloses(t *testing.T) {
	m := press(press(walked(viewing(reactive(), 200, 60), 1), "e"), "esc")

	if out := stripANSI(m.View()); !strings.Contains(out, rocketPill+" 3") {
		t.Errorf("the pills did not come back after the box closed:\n%s", out)
	}
}

// j and k walk the list. Eight rows is exactly the length that earns a filter,
// and a filter claims every printable key ahead of movement: left on, j narrows
// a list of eight fixed names to nothing, because none of them holds one.
func TestJAndKWalkTheReactionList(t *testing.T) {
	m := press(press(reacting(2), "+"), "j")

	if out := stripANSI(m.View()); strings.Contains(out, "No match") {
		t.Fatalf("j filtered the list instead of walking it:\n%s", out)
	}

	got := reactAsked(t, m, "enter")
	if got.Content != gh.ReactionThumbsDown {
		t.Errorf("j then enter asked for %v, want the second row", got.Content)
	}
}

// A card clips what overflows it, silently and mid-cell, so a row of pills
// wider than the card would lose its last ones with nothing to say they were
// there. It folds at pill boundaries instead: a line costs a row and keeps
// every glyph with its number.
func TestAWideRowOfPillsFoldsRatherThanClipping(t *testing.T) {
	d := reactive()
	all := make([]gh.Reaction, 0, len(gh.ReactionOrder))
	for i, c := range gh.ReactionOrder {
		all = append(all, gh.Reaction{Content: c, Count: i + 11})
	}
	d.Timeline[0].Comment.Reactions = all

	// Narrow enough that eight pills cannot sit on one line. At 60 they can,
	// and the card clips nothing, so a fixture at the width the other tests use
	// would prove none of this.
	for _, width := range []int{50, 44} {
		t.Run(strconv.Itoa(width), func(t *testing.T) {
			out := stripANSI(detailed(held(d), width, 40).View())

			for _, r := range all {
				want := reactionGlyphFor(r.Content) + " " + strconv.Itoa(r.Count)
				if !strings.Contains(out, want) {
					t.Errorf("the card lost %q off the end of its pill row:\n%s", want, out)
				}
			}
		})
	}
}

// reactionGlyphFor is the emoji a card draws for a reaction. It is spelled out
// here rather than reached for in the package, because a test sharing the map
// under test would pass on a map that had gone wrong.
func reactionGlyphFor(c gh.ReactionContent) string {
	return map[gh.ReactionContent]string{
		gh.ReactionThumbsUp:   "👍",
		gh.ReactionThumbsDown: "👎",
		gh.ReactionLaugh:      "😄",
		gh.ReactionHooray:     "🎉",
		gh.ReactionConfused:   "😕",
		gh.ReactionHeart:      "❤️",
		gh.ReactionRocket:     "🚀",
		gh.ReactionEyes:       "👀",
	}[c]
}

// A reaction being taken back sits at zero while its write is out, and a pill
// reading "0" would say nobody gave it. The dot is what says the count is not
// known yet. Only a slow network shows this by eye, which is what makes it a
// test rather than a thing to look at.
func TestAReactionBeingTakenBackReadsAsPending(t *testing.T) {
	d := reactive()
	d.Timeline[0].Comment.Reactions = []gh.Reaction{
		{Content: gh.ReactionThumbsUp, Viewer: true, Pending: true},
	}

	out := stripANSI(detailed(held(d), 200, 60).View())
	if !strings.Contains(out, thumbsUp+" ·") {
		t.Errorf("a reaction on its way out does not say so:\n%s", out)
	}
	if strings.Contains(out, thumbsUp+" 0") {
		t.Error("a reaction still being written reads as one nobody gave")
	}
}

// Threads render in the diff as well as in the conversation, through the same
// card, so the pills go with them.
func TestPillsRenderOnAThreadInTheDiff(t *testing.T) {
	d := reactive()
	m := detailed(held(d), 200, 60)
	m.SetFiles(loadedFiles(sampleFiles(), 0))

	// The Files tab, which is three to the right of the conversation.
	out := stripANSI(press(m, "]", "]", "]").View())
	if !strings.Contains(out, "❤️ 2") {
		t.Errorf("the thread's pills are missing from the diff:\n%s", out)
	}
}

// esc closes the list and writes nothing, the way it does over every picker.
func TestEscapeClosesTheListWithoutWriting(t *testing.T) {
	m := press(reacting(2), "+")

	m, cmd := m.Update(escape())
	if cmd != nil {
		t.Errorf("esc sent %+v", cmd())
	}
	if out := stripANSI(m.View()); strings.Contains(out, reactRow) {
		t.Error("esc left the list up")
	}
}

// A pill is the only two-cell glyph on this screen, and a card pads its content
// out to its own width from a measurement. One cell out and the border walks in
// on every row that holds pills, which a fixture of plain text can never show.
func TestPillsDoNotBreakTheFrameWidth(t *testing.T) {
	for _, size := range []struct{ width, height int }{
		{width: 200, height: 40},
		{width: 100, height: 24},
		{width: 60, height: 16},
	} {
		name := strconv.Itoa(size.width) + "x" + strconv.Itoa(size.height)
		t.Run(name, func(t *testing.T) {
			frame := detailed(held(reactive()), size.width, size.height).View()

			for i, line := range strings.Split(frame, "\n") {
				if w := lipgloss.Width(line); w != size.width {
					t.Errorf("line %d is %d cells wide, want %d: %q", i, w, size.width, line)
				}
			}
		})
	}
}

// The same question with the list up, which is where the glyphs sit next to
// each other rather than one per row.
func TestTheReactionListDoesNotBreakTheFrameWidth(t *testing.T) {
	frame := press(reacting(2), "+").View()

	for i, line := range strings.Split(frame, "\n") {
		if w := lipgloss.Width(line); w != 200 {
			t.Errorf("line %d is %d cells wide, want 200: %q", i, w, line)
		}
	}
}

// rowFor is the line of a rendered frame holding a marker.
// The picker's own cells, not the whole frame line. A modal is drawn over the
// screen rather than into it, so the line it lands on still carries the rail and
// the page either side of it, and the rail's Changes row is a column of digits
// that a test asking whether a row is numbered would find.
func rowFor(t *testing.T, frame, marker string) string {
	t.Helper()

	for _, line := range strings.Split(frame, "\n") {
		at := strings.Index(line, marker)
		if at < 0 {
			continue
		}
		// The modal's border either side of the text it holds.
		open := strings.LastIndex(line[:at], "│")
		shut := strings.Index(line[at:], "│")
		if open < 0 || shut < 0 {
			return line
		}
		return line[open : at+shut]
	}
	t.Fatalf("no line holds %q:\n%s", marker, frame)
	return ""
}
