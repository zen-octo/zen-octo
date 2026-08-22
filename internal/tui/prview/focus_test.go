package prview_test

import (
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/praxis-labs-io/zen-octo/internal/gh"
	"github.com/praxis-labs-io/zen-octo/internal/tui/paint"
	"github.com/praxis-labs-io/zen-octo/internal/tui/prview"
	"github.com/praxis-labs-io/zen-octo/internal/tui/theme"
)

// The conversation in sampleDetail, in the order the ring walks it. The resolved
// thread is last and has no card, so it is not here; it is asserted on its own.
const (
	cardDescription = "drucial · opened this"
	cardComment     = "octobot · commented"
	cardReview      = "nkr · requested changes"
	cardThread      = "internal/gh/client.go:42"
	cardLocked      = "internal/tui/app/app.go:12"
	cardCompose     = "write a comment"
)

// A screen opens with its cursor on the description, and the first press of a
// motion key moves rather than arrives. What is lit is what says where the next
// key acts, so a screen with nothing lit is one whose keys name nowhere.
func TestAScreenOpensWithItsCursorOnTheDescription(t *testing.T) {
	m := detailed(held(sampleDetail()), 200, 60)

	if got := focusedCard(t, m.View()); !strings.HasPrefix(got, cardDescription) {
		t.Errorf("card %q is focused on open, want the description", got)
	}
	if got := focusedCard(t, press(m, "}").View()); !strings.HasPrefix(got, cardComment) {
		t.Errorf("the first brace focused %q, want it to have moved on", got)
	}
}

// The leading pane takes the keys on the way in to the screen. It is the one
// the reader navigates with and it is numbered first, so the number and the
// keyboard would otherwise disagree about where the eye lands.
func TestTheLeadingPaneHoldsTheKeysOnArrival(t *testing.T) {
	m := opened(held(sampleDetail()), 200, 44)

	if got := markedRailRow(t, m.View()); got == "" {
		t.Error("the rail leads the row and holds no cursor on open")
	}
	if got := focusedCard(t, m.View()); got != "" {
		t.Errorf("card %q is lit while the rail holds the keys", got)
	}
}

// Once, and not again. A reader changing tab has already chosen a pane, and
// handing the keys back to the rail on every press of the strip would make them
// ask for the page again each time they came round to it.
func TestTheStripLeavesTheKeysWhereTheReaderPutThem(t *testing.T) {
	// Off the rail and onto the page, then round the strip and back.
	m := press(opened(held(sampleDetail()), 200, 44), "l")
	if got := focusedCard(t, m.View()); !strings.HasPrefix(got, cardDescription) {
		t.Fatalf("setup: the page holds %q, want the description", got)
	}

	back := press(m, "]", "]", "]", "]")
	if got := markedRailRow(t, back.View()); got != "" {
		t.Errorf("the strip handed the keys back to the rail, which lit %q", got)
	}
	if got := focusedCard(t, back.View()); !strings.HasPrefix(got, cardDescription) {
		t.Errorf("the page came back holding %q, want the card the reader left on", got)
	}
}

// A frame that opens too narrow for a rail has no lead to take, and widening it
// is the terminal getting bigger rather than an arrival. Latched on having
// found a lead rather than on having arrived, the first widen past railMinFrame
// would take the keys off a reader who was already working in the only pane
// there was.
func TestWideningAFrameDoesNotTakeTheKeys(t *testing.T) {
	m := opened(held(sampleDetail()), 100, 40)
	if got := markedRailRow(t, m.View()); got != "" {
		t.Fatalf("setup: a frame under railMinFrame drew a rail cursor at %q", got)
	}
	if got := focusedCard(t, m.View()); !strings.HasPrefix(got, cardDescription) {
		t.Fatalf("setup: the only pane holds %q, want the description", got)
	}

	m.SetSize(200, 40)
	if got := markedRailRow(t, m.View()); got != "" {
		t.Errorf("widening handed the keys to the rail, which lit %q", got)
	}
	if got := focusedCard(t, m.View()); !strings.HasPrefix(got, cardDescription) {
		t.Errorf("the page holds %q after the widen, want the card the reader was on", got)
	}
}

// The rail marks its cursor line the way the diff marks its row: a bar in the
// leading cell, in accent. One glyph for one fact, so crossing from the diff to
// the rail is not a second mark to learn.
func TestTheRailCursorCarriesTheBar(t *testing.T) {
	m := opened(held(sampleDetail()), 200, 44)

	on := markedRailRow(t, m.View())
	lit := ""
	for _, raw := range railRaw(t, m.View()) {
		if strings.Contains(stripANSI(raw), on) {
			lit = raw
			break
		}
	}
	if lit == "" {
		t.Fatalf("no rail row reading %q", on)
	}

	if !strings.Contains(stripANSI(lit), paint.BarGlyph) {
		t.Errorf("the rail's cursor line carries no bar: %q", stripANSI(lit))
	}
	if !strings.Contains(lit, fgSeq(theme.RosePineMoon.Accent)) {
		t.Error("the bar is not in the accent the diff draws its own in")
	}

	// A cell of gutter is left between the bar and the row it marks. Against it,
	// a dot or a glyph leading a name reads as one mark rather than as a row
	// that has been marked.
	if !strings.Contains(stripANSI(lit), paint.BarGlyph+" ") {
		t.Errorf("the content sits against the bar: %q", stripANSI(lit))
	}

	// It goes in the gutter every row already holds open, so nothing steps.
	// Counted over the rail rather than over the frame: the tab strip marks its
	// own current tab with the same glyph, a row above and outside every pane.
	bars := func(frame string) int {
		n := 0
		for _, row := range railRaw(t, frame) {
			row = stripANSI(row)
			if c := strings.Count(row, paint.BarGlyph); c > 1 {
				t.Errorf("row %q carries more than one bar", row)
			} else {
				n += c
			}
		}
		return n
	}
	if got := bars(m.View()); got != 1 {
		t.Errorf("the rail carries %d bars, want the one under the cursor", got)
	}

	// And it goes with the keys.
	if got := bars(press(m, "l").View()); got != 0 {
		t.Error("the bar is still on the rail once the page took the keys")
	}
}

// A cursor belongs to the pane the keys are going to. Two panes holding one
// says the keys go to both.
func TestTheRailGivesUpItsCursorWhenItGivesUpTheKeys(t *testing.T) {
	m := opened(held(sampleDetail()), 200, 44)
	if markedRailRow(t, m.View()) == "" {
		t.Fatal("setup: the rail has no cursor to give up")
	}

	page := press(m, "l")
	if got := markedRailRow(t, page.View()); got != "" {
		t.Errorf("rail row %q is still lit once the page took the keys", got)
	}
	if got := focusedCard(t, page.View()); !strings.HasPrefix(got, cardDescription) {
		t.Errorf("the page took the keys and lit %q, want the description", got)
	}

	if got := markedRailRow(t, press(page, "h").View()); got == "" {
		t.Error("the rail took the keys back and lit nothing")
	}
}

// walked steps the ring to the card a caller names. The counts every caller
// uses are from the top of the page, so a model left somewhere else goes
// through fromTop first: the ring stops at its ends and no longer laps round to
// the description.
// walked puts the ring on the nth card, counting from one. A screen opens with
// its cursor already on the first, so the nth is n-1 steps away.
func walked(m prview.Model, n int) prview.Model {
	// The leading pane holds the keys on arrival, and the cards are in the one
	// beside it. 2 is always that pane, and it is a no-op where there is only
	// one on the frame.
	m = press(m, "2")
	return press(m, strings.Fields(strings.Repeat("} ", max(0, n-1)))...)
}

// fromTop takes the page and the ring back to the first card, which is where
// the step counts are measured from. Esc used to do the ring half of it by
// dropping the focus; it leaves the screen now, so the way back is to walk it.
// The ring stops at its ends, so more steps than there are cards land on the
// first one and stay there.
func fromTop(m prview.Model) prview.Model {
	return press(press(m, "g"), strings.Fields(strings.Repeat("{ ", 40))...)
}

// The ring walks the cards in the order they were written, and stops at the
// end. Every card is a stop, replies included: a card the motion key walks past
// is one the reader can see and cannot reach, and crossing a heavily reviewed
// page is what the scroll keys are for.
func TestTheRingWalksTheCardsInOrder(t *testing.T) {
	m := detailed(held(sampleDetail()), 200, 60)

	want := []string{cardDescription, cardComment, cardReview, cardThread}
	for i, card := range want {
		got := focusedCard(t, walked(m, i+1).View())
		if !strings.HasPrefix(got, card) {
			t.Errorf("step %d focused %q, want %q", i+1, got, card)
		}
	}

	// The fifth is the reply hanging off that thread, which is a card of its own
	// and so a stop of its own.
	if got := focusedCard(t, walked(m, 5).View()); !strings.HasPrefix(got, "octobot · said") {
		t.Errorf("the fifth step focused %q, want the reply on the thread", got)
	}

	// The sixth is the resolved thread. It is a card like the rest, closed
	// rather than absent, so it takes the accent on its border the same way, and
	// the replies it is hiding are no stops while it is closed.
	if got := focusedCard(t, walked(m, 6).View()); !strings.HasPrefix(got, "✓ internal/store/store.go:88") {
		t.Errorf("the sixth step focused %q, want the resolved thread", got)
	}

	// The seventh and eighth are the threads no review owns, which render at the
	// end of the page in the order the query returned them.
	if got := focusedCard(t, walked(m, 7).View()); !strings.HasPrefix(got, cardLocked) {
		t.Errorf("the seventh step focused %q, want the unowned thread", got)
	}

	// The ninth is the comment box, which closes the conversation the way it
	// closes GitHub's page.
	if got := focusedCard(t, walked(m, 9).View()); !strings.HasPrefix(got, cardCompose) {
		t.Errorf("the ninth step focused %q, want the comment box", got)
	}

	// A page is deep enough that coming back round is the longest throw the key
	// can make, and it arrives at the end the reader walked away from.
	if got := focusedCard(t, walked(m, 10).View()); !strings.HasPrefix(got, cardCompose) {
		t.Errorf("a step past the last card focused %q, want it to stay on the comment box", got)
	}
}

// And stops at the other end the same way.
func TestTheRingStopsAtTheFirstCard(t *testing.T) {
	m := press(detailed(held(sampleDetail()), 200, 60), "}", "{")

	if got := focusedCard(t, m.View()); !strings.HasPrefix(got, cardDescription) {
		t.Fatalf("setup: focus landed on %q, want the first card", got)
	}
	if got := focusedCard(t, press(m, "{").View()); !strings.HasPrefix(got, cardDescription) {
		t.Errorf("a step before the first card focused %q, want it to stay put", got)
	}
}

// The brace walks the other way, and from nothing it takes the last card on
// screen rather than the first.
func TestTheRingWalksBack(t *testing.T) {
	m := detailed(held(sampleDetail()), 200, 60)

	back := focusedCard(t, press(m, "}", "{").View())
	if !strings.HasPrefix(back, cardDescription) {
		t.Errorf("the ring back focused %q, want the description", back)
	}
}

// Focus does not survive being scrolled out of the window. A reader who
// scrolled away has moved on, and hauling them back to the card they left is
// the one thing the ring must not do.
func TestTheRingReanchorsToWhatIsOnScreen(t *testing.T) {
	m := press(detailed(held(sampleDetail()), 200, 16), "}", "}", "}")
	if got := focusedCard(t, m.View()); !strings.HasPrefix(got, cardThread) {
		t.Fatalf("focus started on %q, want the thread card", got)
	}

	// Back to the top, which the thread is a long way below.
	top := press(m, "g")
	if strings.Contains(stripANSI(top.View()), cardThread) {
		t.Fatal("the thread is still on screen, so nothing was re-anchored")
	}

	got := focusedCard(t, press(top, "}").View())
	if !strings.HasPrefix(got, cardDescription) {
		t.Errorf("the ring focused %q, want the first card in the window", got)
	}
}

// A card scrolled to goes to the top of the window. Landed at the foot of it,
// the replies the card is worth reading for are all below the fold.
func TestTheRingScrollsACardToTheTopOfTheWindow(t *testing.T) {
	m := detailed(held(sampleDetail()), 200, 16)

	if strings.Contains(stripANSI(m.View()), cardThread) {
		t.Fatal("the thread is already on screen, so this proves nothing")
	}

	// Three presses reach the thread card, which is well below the fold: the
	// cursor opens on the description, so the first one moves off it.
	got, at := focusedCardAt(t, press(m, "}", "}", "}").View())
	if !strings.HasPrefix(got, cardThread) {
		t.Fatalf("focus landed on %q, want the thread card whole", got)
	}
	// Row zero is the pane's own border, so row one is the top of the window.
	if at != 1 {
		t.Errorf("the card's border landed on pane row %d, want row 1", at)
	}
}

// A card already on screen whole leaves the page alone. The highlight says
// where focus is, and scrolling under a reader who can see it is worse.
func TestTheRingDoesNotScrollACardAlreadyOnScreen(t *testing.T) {
	// Tall enough that the first two cards are both on screen whole, which is
	// the precondition the rule is about.
	m := detailed(held(sampleDetail()), 200, 60)

	// The line a fixed block sits on, rather than the whole frame: focus paints
	// a border and writes a hint into it, so the frames differ without the page
	// having moved at all.
	before := lineOf(t, m.View(), cardDescription)
	after := lineOf(t, press(m, "}", "}").View(), cardDescription)
	if before != after {
		t.Errorf("the description moved from line %d to %d to focus a card already on screen whole", before, after)
	}
}

// lineOf is the frame line a string landed on.
func lineOf(t *testing.T, frame, want string) int {
	t.Helper()

	for i, line := range strings.Split(stripANSI(frame), "\n") {
		if strings.Contains(line, want) {
			return i
		}
	}
	t.Fatalf("%q is not on the frame", want)
	return -1
}

// A card taller than the window pins to its top. Bottom-aligning it opens on
// the end of a comment with the line saying whose it is above the window.
func TestTheRingPinsACardTallerThanTheWindowToItsTop(t *testing.T) {
	d := sampleDetail()
	d.Body = strings.Repeat("The retry path backs off forever.\n\n", 20)

	m := detailed(held(d), 200, 20)
	if got := focusedCard(t, m.View()); !strings.HasPrefix(got, cardDescription) {
		t.Errorf("focus landed on %q, want the description with its heading on screen", got)
	}
}

// A review thread renders in the conversation and again inside the diff, and
// unfolding it is a fact about the thread rather than about the tab. The diff
// caches its file blocks, so the fold has to reach through that too.
func TestUnfoldingAThreadReachesTheDiff(t *testing.T) {
	d := sampleDetail()
	d.Threads[0].Comments[0].Body = "Look.\n\n<details>\n<summary>What it does</summary>\n\nIt retries forever.\n\n</details>\n"

	m := detailed(held(d), 200, 60)
	m.SetFiles(loadedFiles(sampleFiles(), 0))

	// The Files tab renders the thread against the line it hangs off.
	onFiles := press(m, "]", "]", "]")
	if !strings.Contains(stripANSI(onFiles.View()), "▸ What it does") {
		t.Fatal("the diff is not showing the thread's fold")
	}

	// Unfold it in the conversation: the fourth card is that thread, and K steps
	// the sub-cursor off its last comment onto the one holding the fold.
	m = press(m, "}", "}", "}", "K", "space")
	if !strings.Contains(stripANSI(m.View()), "It retries forever") {
		t.Fatal("o did not unfold the thread in the conversation")
	}

	if !strings.Contains(stripANSI(press(m, "]", "]", "]").View()), "It retries forever") {
		t.Error("the diff still shows the thread folded")
	}
}

// The note on an empty Checks section is not a row to stop on: there is
// nothing to do to a check that is not there. Sharing the first check's key
// would leave focus parked here to light up whatever arrived in its place.
func TestTheEmptyChecksNoteIsNotWalkable(t *testing.T) {
	d := sampleDetail()
	d.Rollup = gh.CheckRollup{}

	m := press(detailed(held(d), 200, 44), "h")
	if got := markedRailRow(t, m.View()); strings.Contains(got, "None yet") {
		t.Error("the cursor landed on the empty checks note")
	}

	seen := map[string]bool{}
	for i := range 14 {
		seen[markedRailRow(t, press(m, strings.Fields(strings.Repeat("j ", i))...).View())] = true
	}
	if seen["None yet"] {
		t.Error("the cursor walks the empty checks note")
	}
}

// A row with no state dot has two more cells for its name than one with a dot.
// Reserving for a mark that is not there clips a name that would have fitted.
func TestARowWithNoMarkKeepsTheCellsTheMarkWouldHaveTaken(t *testing.T) {
	d := sampleDetail()
	d.Assignees = []gh.Actor{{Login: strings.Repeat("a", 31)}}

	rows := railRows(t, detailed(held(d), 200, 44).View())
	for i, row := range rows {
		if row != "Assignees" {
			continue
		}
		if got := rows[i+1]; strings.HasSuffix(got, "…") {
			t.Errorf("assignee row = %q, want the name whole", got)
		}
		return
	}
	t.Fatalf("no Assignees section in the rail: %q", rows)
}

// tall is a screen whose description runs well past the pane, so the reader can
// be inside one card with its byline off the top.
func tall() prview.Model {
	d := sampleDetail()
	d.Body = strings.Repeat("The retry path backs off forever.\n\n", 20)

	return detailed(held(d), 200, 20)
}

// scrolledIn takes the page twelve lines into that description.
func scrolledIn(t *testing.T, m prview.Model) prview.Model {
	t.Helper()

	m = press(m, strings.Fields(strings.Repeat("j ", 12))...)
	if strings.Contains(stripANSI(m.View()), cardDescription) {
		t.Fatal("setup: the byline is still on screen, so this proves nothing")
	}
	return m
}

// Forward from inside a card is the next card, in one press. Taking the card
// the window is full of would light a byline the reader cannot see and haul the
// page up to it, which is the page moving against the key.
func TestTheBraceForwardFromInsideACardLeavesIt(t *testing.T) {
	m := press(scrolledIn(t, tall()), "}")

	if got := focusedCard(t, m.View()); !strings.HasPrefix(got, cardComment) {
		t.Errorf("the ring focused %q, want the card after the one the window is full of", got)
	}
	if strings.Contains(stripANSI(m.View()), cardDescription) {
		t.Error("} scrolled back up to the description's byline")
	}
}

// Back from inside a card is that card's own byline. It is what { means in vim,
// and the one motion this screen had no way to make.
func TestTheBraceBackFromInsideACardOpensOnItsByline(t *testing.T) {
	got, at := focusedCardAt(t, press(scrolledIn(t, tall()), "{").View())
	if !strings.HasPrefix(got, cardDescription) {
		t.Fatalf("the ring focused %q, want the card the window is full of", got)
	}
	if at != 1 {
		t.Errorf("the card's border landed on pane row %d, want the top of the window", at)
	}
}

// Back re-enters at the foot of the window, on the last card whole on the
// screen. A long comment with its tail on the top row and cards whole
// underneath is a screen of travel on a key asked for one step, and it lands on
// a byline nobody pointed at.
func TestTheBraceBackReentersOnTheLastCardWholeOnScreen(t *testing.T) {
	d := sampleDetail()
	d.Body = strings.Repeat("The retry path backs off forever.\n\n", 12)

	// The description's tail on the top rows, two cards whole beneath it, and
	// the thread under the second one cut off by the foot of the window.
	m := press(detailed(held(d), 200, 40), strings.Fields(strings.Repeat("j ", 12))...)
	if strings.Contains(stripANSI(m.View()), cardDescription) {
		t.Fatal("setup: the description's byline is still on screen")
	}
	// The reply hanging off that thread is below the fold, so the thread's own
	// card is cut off and the review above it is the last one whole.
	if strings.Contains(stripANSI(m.View()), "octobot · said") {
		t.Fatal("setup: the thread is whole on screen, so it is the card { should take")
	}

	before := lineOf(t, m.View(), cardReview)
	after := press(m, "{")

	// Not the description, which the top row sits inside and which is a screen
	// of travel away.
	if got := focusedCard(t, after.View()); !strings.HasPrefix(got, cardReview) {
		t.Errorf("{ landed on %q, want the last card whole on the screen", got)
	}
	if now := lineOf(t, after.View(), cardReview); now != before {
		t.Errorf("the page moved from line %d to %d to light a card already on screen whole", before, now)
	}
}

// A focus scrolled until its own byline is off the top is no longer where the
// reader is standing, so the brace stops stepping from it. Stepping would land
// a screen or more above the window, on a card they left.
func TestAFocusScrolledOffItsBylineStopsBeingTheStep(t *testing.T) {
	m := tall()
	if got := focusedCard(t, m.View()); !strings.HasPrefix(got, cardDescription) {
		t.Fatalf("setup: focus landed on %q, want the description", got)
	}

	// Not the card before the description, which the ring would step to.
	if got := focusedCard(t, press(scrolledIn(t, m), "{").View()); !strings.HasPrefix(got, cardDescription) {
		t.Errorf("{ landed on %q, want the byline of the card the reader is in", got)
	}
}

// The ring's lines sit one below the viewport's, and converting between them
// has to be reversible. Clamping one way and not the other moves the page.
func TestTheRingAtTheTopOfAScrollablePaneDoesNotMoveThePage(t *testing.T) {
	m := detailed(held(sampleDetail()), 200, 24)

	row := func(frame string) string {
		lines := strings.Split(stripANSI(frame), "\n")
		return strings.TrimSpace(strings.Trim(lines[paneTopAt(frame)+1], "│ "))
	}
	if got := row(m.View()); got != "" {
		t.Fatalf("the pane opens on %q, want its blank line", got)
	}
	if got := row(press(m, "}").View()); got != "" {
		t.Errorf("after a step the pane opens on %q, want it not to have moved", got)
	}
}

// Focus scrolled out of the window is nothing the reader can see, so esc means
// the screen rather than the highlight they cannot find.
func TestEscBacksOutWhenTheFocusIsOffScreen(t *testing.T) {
	m := press(detailed(held(sampleDetail()), 200, 16), "}", "G")
	if strings.Contains(stripANSI(m.View()), cardDescription) {
		t.Fatal("the focused card is still on screen, so this proves nothing")
	}

	_, cmd := m.Update(escape())
	if cmd == nil {
		t.Fatal("esc was swallowed by a focus off the screen")
	}
	if _, ok := cmd().(prview.BackMsg); !ok {
		t.Errorf("esc sent %T, want a BackMsg", cmd())
	}
}

// Nor does o act on it. Unfolding a card out of sight moves the page back to
// somewhere the reader already left.
func TestOLeavesThePageAloneWhenTheFocusIsOffScreen(t *testing.T) {
	d := sampleDetail()
	d.Body = "Look.\n\n<details>\n<summary>Hidden</summary>\n\nThe secret.\n\n</details>\n"

	m := press(detailed(held(d), 200, 16), "}", "G")

	before := stripANSI(m.View())
	if before != stripANSI(press(m, "space").View()) {
		t.Error("o acted on a card off the screen")
	}
}

// One pane answers the keys, so one pane paints. Two lit at once says both do.
func TestOnlyThePaneHoldingTheKeysPaintsItsFocus(t *testing.T) {
	m := detailed(held(sampleDetail()), 200, 44)
	if got := focusedCard(t, m.View()); !strings.HasPrefix(got, cardDescription) {
		t.Fatalf("focus started on %q, want the description", got)
	}

	rail := press(m, "h", "}")
	if got := focusedCard(t, rail.View()); got != "" {
		t.Errorf("card %q is lit while the rail holds the keys", got)
	}
	if markedRailRow(t, rail.View()) == "" {
		t.Fatal("the rail row is not painted at all")
	}

	if got := markedRailRow(t, press(rail, "l").View()); got != "" {
		t.Errorf("rail row %q is lit while the conversation holds the keys", got)
	}
}

// A tab gives back the pane it was left on. Focus is one field where the scroll
// is four, and Commits takes the column on arrival, so a round trip through it
// used to come back on whatever layout was left holding: the column goes off
// screen on the way back and the keys fell to the page.
func TestATabGivesBackThePaneItWasLeftOn(t *testing.T) {
	var (
		lit  = fgSeq(theme.RosePineMoon.Accent)
		idle = fgSeq(theme.RosePineMoon.BorderSubtle)
	)

	// Left alone, the rail leads and keeps the keys across the round trip.
	m := opened(held(sampleDetail()), 200, 40)
	if got := conversationBorder(t, m.View()); got != idle {
		t.Fatalf("setup: the page holds the keys on arrival, want the rail")
	}
	if got := conversationBorder(t, press(m, "]", "[").View()); got != idle {
		t.Error("the round trip took the keys off the rail")
	}

	// And a reader who chose the page keeps that instead: the tab gives back
	// what it was left on, not the pane that leads.
	page := press(m, "2")
	if got := conversationBorder(t, page.View()); got != lit {
		t.Fatalf("setup: 2 did not put the keys on the page")
	}
	if got := conversationBorder(t, press(page, "]", "[").View()); got != lit {
		t.Error("the round trip took the keys off the page")
	}
}

// Commits and Checks take their column on arrival and only on arrival. A reader
// who walked off it once meant it, and coming back to a column they left is the
// strip handing the keys back on a key that only changes what is on screen.
func TestACommitsColumnIsTakenOnArrivalAndNotAgain(t *testing.T) {
	idle := fgSeq(theme.RosePineMoon.BorderSubtle)

	m := press(opened(held(sampleDetail()), 200, 40), "]")
	if got := conversationBorder(t, m.View()); got != idle {
		t.Fatal("setup: Commits did not take its column on arrival")
	}

	// Off the column, away, and back.
	m = press(m, "2")
	if got := conversationBorder(t, press(m, "[", "]").View()); got == idle {
		t.Error("Commits took its column again on a tab the reader had left")
	}
}

// The strip is ] and [ here and on the list screen, and nothing else reaches
// it: the braces walk blocks and tab is the file key on the tab with files.
//
// It is on no pane now, so the other half of what this once asserted is free:
// the strip cannot be moved across the screen by anything, only through.
func TestOnlyTheBracketsMoveTheTabStrip(t *testing.T) {
	m := detailed(held(sampleDetail()), 160, 24)

	if got := currentTab(t, press(m, "]").View()); got != "Commits" {
		t.Errorf("] moved to %q, want Commits", got)
	}
	if got := currentTab(t, press(m, "[").View()); got != "Files" {
		t.Errorf("[ wrapped to %q, want Files", got)
	}
	for _, k := range []string{"}", "{", "tab", "shift+tab"} {
		if got := currentTab(t, press(m, k).View()); got != "Conversation" {
			t.Errorf("%q moved off the Conversation tab, to %q", k, got)
		}
	}
}

// Esc leaves, first press, with a card lit. Letting go used to come first, and
// with a cursor landed on every screen that would be a key that never leaves:
// there is always something to let go of now, so the reader wanting the list
// would pay two presses every time.
func TestEscBacksOutWithACardFocused(t *testing.T) {
	m := press(detailed(held(sampleDetail()), 200, 60), "}")
	if got := focusedCard(t, m.View()); got == "" {
		t.Fatal("setup: no card is focused")
	}

	_, cmd := m.Update(escape())
	if cmd == nil {
		t.Fatal("esc did not back out while a card was focused")
	}
	if _, ok := cmd().(prview.BackMsg); !ok {
		t.Errorf("esc sent %T, want a BackMsg", cmd())
	}
}

// A tab with a column shows no ring. Focus held over from the conversation is
// invisible there, and swallowing esc for it strands the reader on the screen.
func TestEscBacksOutFromATabWithNoRing(t *testing.T) {
	m := press(detailed(held(sampleDetail()), 200, 60), "}", "]")

	_, cmd := m.Update(escape())
	if cmd == nil {
		t.Fatal("esc on the Commits tab did not back out")
	}
	if _, ok := cmd().(prview.BackMsg); !ok {
		t.Errorf("esc sent %T, want a BackMsg", cmd())
	}
}

// The rail is a list of controls rather than blocks, so it takes the movement
// keys the file column takes. Its rows have no border to take the accent, so
// the row itself is painted, the way the column paints its own.
func TestTheRailCursorWalksItsRowsOnTheMovementKeys(t *testing.T) {
	m := press(detailed(held(sampleDetail()), 200, 44), "h")

	// The cursor is there with the focus. The state row leads: it is the first
	// thing in the column and the first thing there is anything to do to.
	if got := markedRailRow(t, m.View()); !strings.Contains(got, "Open") {
		t.Errorf("taking the rail marked %q, want the state row", got)
	}
	for _, k := range []string{"j", "down"} {
		if got := markedRailRow(t, press(m, k).View()); got != "@nkr" {
			t.Errorf("%q marked %q, want the first reviewer", k, got)
		}
	}

	// Nothing in the conversation is lit while the focus is in the rail.
	if got := focusedCard(t, m.View()); got != "" {
		t.Errorf("card %q is lit while the rail holds the focus", got)
	}
}

// The cursor stops at each end of the rail rather than coming back round. It
// wrapped once, on a ring whose step is modular, so one k on the first control
// hauled the reader to the bottom of a pane they had just arrived at.
func TestTheRailCursorStopsAtItsEnds(t *testing.T) {
	// Short enough that the rail runs past the frame, so a wrap would be a jump
	// the reader could see rather than a move inside one screen.
	m := press(detailed(held(sampleDetail()), 200, 20), "h")

	first := markedRailRow(t, m.View())
	if !strings.Contains(first, "Open") {
		t.Fatalf("setup: landed on %q, want the state row", first)
	}
	if got := markedRailRow(t, press(m, "k").View()); got != first {
		t.Errorf("k on the first control marked %q, want it held on %q", got, first)
	}

	walkDown := press(m, strings.Fields(strings.Repeat("j ", 30))...)
	last := markedRailRow(t, walkDown.View())
	if got := markedRailRow(t, press(walkDown, "j").View()); got != last {
		t.Errorf("past the last control the cursor marked %q, want it held on %q", got, last)
	}

	// And nothing is stranded below it: the rail's own last row is on screen.
	if !strings.Contains(stripANSI(walkDown.View()), "Blocked") {
		t.Errorf("the rail's last row is off screen with the cursor at its end:\n%s", stripANSI(walkDown.View()))
	}
}

// The braces are block motion, and the rail has no blocks. Leaving them live on
// it as well would give one pane two ways to do the same thing.
func TestTheBracesDoNothingOnTheRail(t *testing.T) {
	m := press(detailed(held(sampleDetail()), 200, 44), "h")

	for _, k := range []string{"}", "{"} {
		if got := press(m, k).View(); got != m.View() {
			t.Errorf("%q moved something on the rail", k)
		}
	}
}

// The add row closes the Reviewers section rather than sitting under the
// section heading or at the end of the column.
func TestTheAddReviewerRowFollowsTheReviewers(t *testing.T) {
	rows := railRows(t, detailed(held(sampleDetail()), 200, 44).View())

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

	// Three reviewers in the fixture, then the row that adds a fourth.
	if got := rows[at+4]; got != "+ Add reviewer" {
		t.Errorf("the row after the reviewers is %q, want the add row", got)
	}

	// Four steps past the state row: three reviewers, then the add row.
	m := press(detailed(held(sampleDetail()), 200, 44), "h")
	if got := markedRailRow(t, press(m, "j", "j", "j", "j").View()); got != "+ Add reviewer" {
		t.Errorf("the fourth step marked %q, want the add row", got)
	}
}

// The rail sections the reader can act on are walkable and the rest are not,
// so the ring does not stop on the churn or on a merge that cannot be made.
func TestTheRingSkipsTheRailRowsThereIsNothingToDoTo(t *testing.T) {
	m := press(detailed(held(sampleDetail()), 200, 44), "h")

	seen := map[string]bool{}
	for i := range 16 {
		seen[markedRailRow(t, press(m, strings.Fields(strings.Repeat("j ", i))...).View())] = true
	}

	// The state row leads with a glyph, so it is read by what it says.
	reached := func(text string) bool {
		for row := range seen {
			if strings.Contains(row, text) {
				return true
			}
		}
		return false
	}

	for _, want := range []string{
		"Open", "@nkr", "@drucial", "bug", "Rails Unit Tests / test",
		"+ Add reviewer", "+ Add assignee", "+ Add label", "behind main",
	} {
		if !reached(want) {
			t.Errorf("the ring never reached the %q row", want)
		}
	}

	// The churn only reports. The merge row is blocked, and this reader is not
	// an administrator, so there is no merge to open a form for; the base beside
	// it says how far behind the branch is and is still a control, because
	// retargeting is a change to the branch that number is measured against.
	for _, skip := range []string{"+42", "Blocked"} {
		if reached(skip) {
			t.Errorf("the ring stopped on %q, which there is nothing to do to", skip)
		}
	}
}

// Focus names the card, not the place it sat in. A rebase re-sorts commits into
// the timeline by date, and the reader comes back to the comment they left.
func TestFocusHoldsThroughAReorderedTimeline(t *testing.T) {
	m := press(detailed(held(sampleDetail()), 200, 60), "}")
	if got := focusedCard(t, m.View()); !strings.HasPrefix(got, cardComment) {
		t.Fatalf("one step focused %q, want %q", got, cardComment)
	}

	m.SetDetail(held(reordered(sampleDetail())))

	if got := focusedCard(t, m.View()); !strings.HasPrefix(got, cardComment) {
		t.Errorf("the reorder moved focus to %q, want %q still", got, cardComment)
	}
}

// An unfold is keyed the same way, so a reorder does not hand it to whichever
// card took the place.
func TestAnUnfoldHoldsThroughAReorderedTimeline(t *testing.T) {
	folded := func() gh.PullRequestDetail {
		d := sampleDetail()
		d.Timeline[0].Comment.Body = "Look.\n\n<details>\n<summary>Hidden</summary>\n\nThe secret.\n\n</details>\n"
		return d
	}

	m := press(detailed(held(folded()), 200, 60), "}", "space")
	if !strings.Contains(stripANSI(m.View()), "The secret.") {
		t.Fatal("o did not unfold the comment")
	}

	m.SetDetail(held(reordered(folded())))

	out := stripANSI(m.View())
	if strings.Contains(out, "▸ Hidden") {
		t.Error("the comment renders folded after the reorder")
	}
	if !strings.Contains(out, "The secret.") {
		t.Error("the reorder folded the comment back up")
	}
}

// reordered swaps the comment and the review, which is the shape a refetch
// after a rebase comes back in.
func reordered(d gh.PullRequestDetail) gh.PullRequestDetail {
	d.Timeline[0], d.Timeline[1] = d.Timeline[1], d.Timeline[0]
	return d
}

// The rail keys on the row's own name. A label added above the one the reader
// is pointing at leaves the cursor where it was.
func TestRailFocusHoldsThroughAnInsertedLabel(t *testing.T) {
	m := press(detailed(held(sampleDetail()), 200, 44), "h")

	// Seven steps past the state row: three reviewers and their add row, the
	// assignee and its add row, then the one label.
	m = press(m, strings.Fields(strings.Repeat("j ", 7))...)
	if got := markedRailRow(t, m.View()); got != "bug" {
		t.Fatalf("seven steps marked %q, want the label", got)
	}

	d := sampleDetail()
	d.Labels = append([]gh.Label{{Name: "docs"}}, d.Labels...)
	m.SetDetail(held(d))

	if got := markedRailRow(t, m.View()); got != "bug" {
		t.Errorf("the new label moved the cursor to %q, want %q still", got, "bug")
	}
}

// A frame with a card focused is still exactly the size it was given.
func TestAFocusedFrameFillsItsSizeExactly(t *testing.T) {
	sizes := []struct{ width, height int }{
		{width: 200, height: 40},
		{width: 160, height: 24},
		{width: 100, height: 20},
		{width: 60, height: 10},
	}

	for _, size := range sizes {
		t.Run(fmt.Sprintf("%dx%d", size.width, size.height), func(t *testing.T) {
			m := press(detailed(held(sampleDetail()), size.width, size.height), "}", "}", "}")
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

func escape() tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: tea.KeyEscape, Text: "esc"}
}

// focusedCard is the heading of the card whose border is in the accent, or ""
// when no card holds the focus. The border is the only signal there is, and
// reading the heading under it proves the card is on the screen whole.
func focusedCard(t *testing.T, frame string) string {
	t.Helper()

	head, _ := focusedCardAt(t, frame)
	return head
}

// focusedCardAt is the same, with the row of the pane the card's top border
// landed on. Row zero is the pane's own top border, so a card at the top of the
// window sits on row one.
func focusedCardAt(t *testing.T, frame string) (string, int) {
	t.Helper()

	accent := fgSeq(theme.RosePineMoon.Accent)
	lines := strings.Split(frame, "\n")
	top := paneTopAt(frame)

	for i, line := range lines {
		// A line opening with a corner is a pane's own border, not a card's.
		at := strings.Index(line, "╭")
		if at < 0 || i+1 >= len(lines) || strings.HasPrefix(stripANSI(line), "╭") {
			continue
		}
		start := strings.LastIndex(line[:at], "\x1b[")
		if start < 0 || !strings.HasPrefix(line[start+2:], accent) {
			continue
		}
		return cardHeading(stripANSI(lines[i+1]), stripANSI(line)), i - top
	}
	return "", -1
}

// cardHeading is the heading row read out of the card whose top border is on
// the line above it. The card sits at some column inside the pane, with the
// thread rail to its left and the details pane to its right, so it is cut out
// by the column its own border is in.
func cardHeading(row, border string) string {
	col := utf8.RuneCountInString(border[:strings.Index(border, "╭")])

	runes := []rune(row)
	if col+1 >= len(runes) {
		return ""
	}

	inner := string(runes[col+1:])
	if end := strings.Index(inner, "│"); end >= 0 {
		inner = inner[:end]
	}
	return strings.TrimSpace(inner)
}

// markedRailRow is the text of the rail row painted as the cursor line.
func markedRailRow(t *testing.T, frame string) string {
	t.Helper()

	for _, raw := range railRaw(t, frame) {
		if !strings.Contains(raw, bgSeq(theme.RosePineMoon.SelectedBackground)) {
			continue
		}
		return strings.TrimSpace(strings.Trim(stripANSI(raw), "│● "+paint.BarGlyph))
	}
	return ""
}
