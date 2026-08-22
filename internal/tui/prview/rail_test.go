package prview_test

import (
	"strconv"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"

	"github.com/praxis-labs-io/zen-octo/internal/tui/theme"
)

const (
	// narrowFrame is under the width the rail is worth a column of; wideFrame is
	// over it and under the width the rail comes up on its own.
	narrowFrame = 65
	wideFrame   = 100

	// railCells is the column a rail takes, whichever way it lands.
	railCells = 37
)

// The rail is the only route to five writes, so it answers d at every width.
// Where a column will not fit it lands over the left of the conversation.
func TestANarrowRailLandsOverTheConversation(t *testing.T) {
	m := detailed(held(sampleDetail()), narrowFrame, 30)
	if strings.Contains(stripANSI(m.View()), "Reviewers") {
		t.Fatalf("setup: the rail is already up at %d columns", narrowFrame)
	}

	before := strings.Split(stripANSI(m.View()), "\n")
	shown := press(m, "d")
	if !strings.Contains(stripANSI(shown.View()), "Reviewers") {
		t.Fatalf("d left the rail off at %d columns, where the client is read-only without it", narrowFrame)
	}

	// The conversation still reaches the frame's own edge, so it was covered
	// rather than narrowed.
	if _, got := paneEdges(t, shown.View()); got != narrowFrame-1 {
		t.Errorf("the conversation's right border is at %d, want %d: it gave up width to the rail",
			got, narrowFrame-1)
	}

	// Compared with the conversation holding the keys either way. Opening the
	// rail hands them over, and a card that is no longer the focus stops naming
	// the keys it answers to, which is a real difference and not a relayout.
	//
	// Covered, not relaid out: everything right of the rail is the frame it was.
	// The borders are exempt, since a second pane puts an index on the first.
	after := strings.Split(stripANSI(press(shown, "l").View()), "\n")
	if len(before) != len(after) {
		t.Fatalf("the frame is %d lines with the rail up and %d without", len(after), len(before))
	}
	for i := paneRow(t, before) + 1; i < len(before)-1; i++ {
		l, r := rightOf(before[i], railCells), rightOf(after[i], railCells)
		if l != r {
			t.Fatalf("line %d moved under the rail:\n%q\n%q", i, l, r)
		}
	}
}

// paneRow is the line the panes open on, which is the first with a corner.
func paneRow(t *testing.T, frame []string) int {
	t.Helper()

	for i, line := range frame {
		if strings.HasPrefix(line, "╭") {
			return i
		}
	}
	t.Fatal("no pane on the frame")
	return 0
}

// Every control is on the narrow rail. A rail that came up missing one would
// leave the write behind it as unreachable as no rail at all.
func TestTheNarrowRailCarriesEveryControl(t *testing.T) {
	// Tall, because a drawer down the side of an editor is: the rail scrolls
	// like any pane and this is asking what it holds, not what fits at once.
	out := stripANSI(press(detailed(held(sampleDetail()), narrowFrame, 45), "d").View())

	for _, want := range []string{"State", "Reviewers", "Assignees", "Labels", "Base"} {
		if !strings.Contains(out, want) {
			t.Errorf("the rail has no %s row at %d columns:\n%s", want, narrowFrame, out)
		}
	}
}

// Where a column does fit, it is a column. Covering a conversation that had the
// room to make way is spending the frame on nothing.
func TestARailWithRoomForAColumnTakesOne(t *testing.T) {
	shown := press(detailed(held(sampleDetail()), wideFrame, 30), "d")
	if !strings.Contains(stripANSI(shown.View()), "Reviewers") {
		t.Fatalf("setup: d left the rail off at %d columns", wideFrame)
	}

	// The rail leads the row, so it is the pane whose width paneEnd reads, and
	// the conversation takes what is left.
	if got := paneEnd(t, shown.View()); got != railCells {
		t.Errorf("the rail took %d columns, want %d", got, railCells)
	}
	if _, got := paneEdges(t, shown.View()); got != wideFrame-1 {
		t.Errorf("the conversation ends at %d, want it to fill out to %d", got, wideFrame-1)
	}
}

// A box is drawn down the page and not over it, so an overlaid rail covers the
// half of it carrying the button, with d a letter and esc the only way out.
func TestAnOverlaidRailStepsAsideForABox(t *testing.T) {
	m := press(detailed(held(sampleDetail()), narrowFrame, 30), "d", "c")
	if out := stripANSI(m.View()); strings.Contains(out, "Reviewers") {
		t.Errorf("the rail is still over the box being written in:\n%s", out)
	}
	if out := stripANSI(m.View()); !strings.Contains(out, "post") {
		t.Errorf("the box has no footer, so the rail is not what was covering it:\n%s", out)
	}

	// Back when the box is done, and without having to be asked for again.
	if out := stripANSI(press(m, "esc").View()); !strings.Contains(out, "Reviewers") {
		t.Errorf("the rail did not come back when the box closed:\n%s", out)
	}
}

// A column has made room for the box already, so it stays. Stepping aside there
// would rewrap the conversation around a box that had the width it needed.
func TestAColumnRailStaysUnderABox(t *testing.T) {
	m := press(detailed(held(sampleDetail()), wideFrame, 30), "d", "c")
	if out := stripANSI(m.View()); !strings.Contains(out, "Reviewers") {
		t.Errorf("the rail gave up its column for a box that fits beside it:\n%s", out)
	}
}

// The key that opens the rail is a reader reaching for a control, so it hands
// the keys over and takes them back. Both widths, since one is the bug's shape.
func TestOpeningTheRailFocusesIt(t *testing.T) {
	focused := fgSeq(theme.RosePineMoon.Accent)

	for _, width := range []int{narrowFrame, wideFrame} {
		t.Run(strconv.Itoa(width), func(t *testing.T) {
			m := press(detailed(held(sampleDetail()), width, 30), "d")
			if got := conversationBorder(t, m.View()); got == focused {
				t.Error("the rail opened and the conversation kept the keys")
			}
			if got := conversationBorder(t, press(m, "d").View()); got != focused {
				t.Error("the rail closed and took the keys with it")
			}
		})
	}
}

// rightOf is a line past its first n cells, which is the half an overlaid rail
// does not cover.
//
// Counted in cells rather than in runes. The rail is a column of the terminal,
// and one CJK character or emoji in a title puts a rune index short of the
// column it is meant to name, so the two halves compared against it would no
// longer be the same columns of the frame.
func rightOf(line string, n int) string {
	at := 0
	for i, r := range line {
		if at >= n {
			return line[i:]
		}
		at += lipgloss.Width(string(r))
	}
	return ""
}
