package prview_test

import (
	"slices"
	"strings"
	"testing"
	"unicode/utf8"

	"charm.land/lipgloss/v2"

	"github.com/praxis-labs-io/zen-octo/internal/gh"
	"github.com/praxis-labs-io/zen-octo/internal/store"
	"github.com/praxis-labs-io/zen-octo/internal/tui/prview"
)

// canAct is a viewer who owns an open pull request: both moves an open one
// takes, and nothing to reopen.
var canAct = gh.ViewerActions{CanUpdate: true, CanClose: true}

// stateDetail is the sample pull request sitting somewhere else in its
// lifecycle, with the viewer allowed whatever the case is testing.
func stateDetail(state gh.PRState, draft bool, v gh.ViewerActions) store.Detail {
	d := sampleDetail()
	d.State = state
	d.IsDraft = draft
	d.Viewer = v
	return held(d)
}

// onStateRow walks the rail to the State row, which is named by its glyph and
// its label rather than by the label alone, so it is matched by substring.
func onStateRow(t *testing.T, m prview.Model) prview.Model {
	t.Helper()

	// Back to the first control before walking down. The cursor stops at each
	// end rather than coming back round, so a caller already standing below the
	// State row would never reach it.
	m = press(m, "1")
	m = press(m, strings.Fields(strings.Repeat("k ", 30))...)
	for range 30 {
		if isStateRow(markedRailRow(t, m.View())) {
			return m
		}
		m = press(m, "j")
	}
	t.Fatal("the cursor never reached the State row")
	return m
}

// isStateRow reads a marked rail row as the state one: a lifecycle glyph, then
// the label PRStateLabel gives it.
//
// The label alone is not enough. MergeStateLabel renders a draft merge state as
// "Draft" as well, and the Merge row is a stop on the same ring, so a fixture
// carrying one would match there instead. The glyph tells them apart, and it is
// read as a private-use codepoint rather than copied out of badge.go: a glyph
// transcribed by hand is one that silently matches nothing.
func isStateRow(row string) bool {
	glyph, size := utf8.DecodeRuneInString(row)
	if glyph < 0xE000 || glyph > 0xF8FF {
		return false
	}
	return slices.Contains([]string{"Open", "Draft", "Closed", "Merged"}, strings.TrimSpace(row[size:]))
}

// reachesStateRow is whether the cursor ever stops on the State row at all.
func reachesStateRow(t *testing.T, m prview.Model) bool {
	t.Helper()

	m = press(m, "1")
	m = press(m, strings.Fields(strings.Repeat("k ", 30))...)
	for range 30 {
		if isStateRow(markedRailRow(t, m.View())) {
			return true
		}
		m = press(m, "j")
	}
	return false
}

// openStateMenu walks to the State row and opens what it holds. Nothing is
// fetched for it, unlike the label picker, so the modal is up on the way back.
func openStateMenu(t *testing.T, d store.Detail) prview.Model {
	t.Helper()

	m := onStateRow(t, detailed(d, 200, 60))
	m, _ = key(m, "enter")
	if !m.Capturing() {
		t.Fatal("enter on the State row opened nothing")
	}
	return m
}

// stateMenu cuts the State modal out of the frame it is composited over.
func stateMenu(t *testing.T, m prview.Model) string {
	t.Helper()
	return menuBox(t, m, "State")
}

// menuBox cuts the modal with this title out of the frame it is composited
// over.
//
// Asserting against the whole frame cannot say what a menu offers. The rail row
// and the header both read "Closed" on a closed pull request, so a search for
// "Close" across the frame finds one whether or not the menu holds it, and a
// handle in the Assignees picker reads in the conversation as readily as on the
// rail.
func menuBox(t *testing.T, m prview.Model, title string) string {
	t.Helper()

	frame := pickerFrame(m)
	lines := strings.Split(frame, "\n")
	corner := "╭─" + title

	top := -1
	for i, line := range lines {
		if strings.Contains(line, corner) {
			top = i
			break
		}
	}
	if top < 0 {
		t.Fatalf("no %s menu in the frame:\n%s", title, frame)
	}

	head := []rune(lines[top])
	left := runeIndex(head, corner)
	right := left + runeIndex(head[left:], "╮") + 1

	var out []string
	for _, line := range lines[top:] {
		runes := []rune(line)
		if right > len(runes) {
			break
		}
		row := string(runes[left:right])
		out = append(out, row)
		if strings.HasPrefix(row, "╰") {
			break
		}
	}
	return strings.Join(out, "\n")
}

// runeIndex is strings.Index counted in runes, which is what slicing a frame by
// column needs: every border in it is multi-byte and one cell wide.
func runeIndex(haystack []rune, needle string) int {
	at := strings.Index(string(haystack), needle)
	if at < 0 {
		return -1
	}
	return len([]rune(string(haystack)[:at]))
}

// The menu is built from where the pull request sits and what the viewer may
// do to it, never from the word on the row: a closed draft reads as "Draft" and
// takes none of a draft's moves.
func TestTheStateMenuOffersOnlyTheMovesAvailable(t *testing.T) {
	tests := []struct {
		name   string
		state  gh.PRState
		draft  bool
		viewer gh.ViewerActions
		want   []string
		absent []string
	}{
		{
			name: "open and ready", state: gh.PRStateOpen, viewer: canAct,
			want:   []string{"Convert to draft", "Close"},
			absent: []string{"Ready for review", "Reopen"},
		},
		{
			name: "open and draft", state: gh.PRStateOpen, draft: true, viewer: canAct,
			want:   []string{"Ready for review", "Close"},
			absent: []string{"Convert to draft", "Reopen"},
		},
		{
			name: "closed", state: gh.PRStateClosed,
			viewer: gh.ViewerActions{CanReopen: true},
			want:   []string{"Reopen"},
			absent: []string{"Close", "Convert to draft", "Ready for review"},
		},
		{
			// Closed and still a draft. The row says "Draft", and the only move
			// it has is the one a closed pull request has.
			name: "closed draft", state: gh.PRStateClosed, draft: true,
			viewer: gh.ViewerActions{CanUpdate: true, CanReopen: true},
			want:   []string{"Reopen"},
			absent: []string{"Ready for review", "Close"},
		},
		{
			// Write access to the branch but not to the pull request's own
			// lifecycle. Only the move GitHub says it will take.
			name: "may update but not close", state: gh.PRStateOpen,
			viewer: gh.ViewerActions{CanUpdate: true},
			want:   []string{"Convert to draft"},
			absent: []string{"Close"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			menu := stateMenu(t, openStateMenu(t, stateDetail(tt.state, tt.draft, tt.viewer)))

			for _, want := range tt.want {
				if !strings.Contains(menu, want) {
					t.Errorf("the menu does not offer %q:\n%s", want, menu)
				}
			}
			for _, absent := range tt.absent {
				if strings.Contains(menu, absent) {
					t.Errorf("the menu offers %q, which this state cannot take:\n%s", absent, menu)
				}
			}
		})
	}
}

// Nothing moves a merged pull request, so the row states a fact and the ring
// walks past it the way it does an empty Checks section.
func TestTheStateRowIsNotAStopOnAMergedPullRequest(t *testing.T) {
	m := detailed(stateDetail(gh.PRStateMerged, false, canAct), 200, 60)

	if reachesStateRow(t, m) {
		t.Error("tab stops on the State row of a merged pull request")
	}
}

// A reader with no write access can move none of them, and a row that stops the
// cursor to do nothing is worse than one it walks past.
func TestTheStateRowIsNotAStopWithoutPermission(t *testing.T) {
	m := detailed(stateDetail(gh.PRStateOpen, false, gh.ViewerActions{}), 200, 60)

	if reachesStateRow(t, m) {
		t.Error("tab stops on the State row with nothing the viewer may do")
	}
}

// Before the detail lands nothing is known about what the viewer may do, which
// is not the same as nothing being allowed. Dropping the row early would move
// every rail stop by one the moment the answer arrived.
func TestTheStateRowIsAStopBeforeTheDetailLands(t *testing.T) {
	if !reachesStateRow(t, screen(200, 60)) {
		t.Error("tab walks past the State row before the detail has landed")
	}
}

// It is inert until then, though: openRailPicker refuses a detail not loaded.
func TestEnterOnTheStateRowDoesNothingBeforeTheDetailLands(t *testing.T) {
	m := onStateRow(t, screen(200, 60))

	m, cmd := key(m, "enter")
	if m.Capturing() {
		t.Error("enter opened a menu over a detail that has not landed")
	}
	if got := runCmd(cmd); got != nil {
		t.Errorf("enter sent %T, want nothing", got)
	}
}

// The state menu is built from the detail already on screen, so it costs no
// round trip. Only the label picker asks the root for anything.
func TestEnterOnTheStateRowOpensWithoutAsking(t *testing.T) {
	m := onStateRow(t, detailed(held(sampleDetail()), 200, 60))

	m, cmd := key(m, "enter")
	if got := runCmd(cmd); got != nil {
		t.Errorf("enter on the State row sent %T, want nothing fetched", got)
	}
	if !m.Capturing() {
		t.Error("the state menu did not open")
	}
}

func TestPickingAStateAsksTheRootToWriteIt(t *testing.T) {
	tests := []struct {
		name  string
		state gh.PRState
		draft bool
		steps []string
		want  gh.PRTransition
	}{
		{name: "convert to draft", state: gh.PRStateOpen, want: gh.TransitionDraft},
		{name: "close", state: gh.PRStateOpen, steps: []string{"j"}, want: gh.TransitionClose},
		{name: "ready for review", state: gh.PRStateOpen, draft: true, want: gh.TransitionReady},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := openStateMenu(t, stateDetail(tt.state, tt.draft, canAct))
			m = press(m, tt.steps...)

			got, ok := asked(t, m, "enter").(prview.SetStateMsg)
			if !ok {
				t.Fatalf("enter sent %T, want a SetStateMsg", asked(t, m, "enter"))
			}
			if got.To != tt.want {
				t.Errorf("To = %q, want %q", got.To, tt.want)
			}
			if got.ID != "PR_412" {
				t.Errorf("ID = %q, want the pull request on screen", got.ID)
			}
		})
	}
}

// Reopen is the one move a closed pull request has, and it comes off a menu
// built from a different branch of the rules.
func TestPickingReopenAsksForIt(t *testing.T) {
	m := openStateMenu(t, stateDetail(gh.PRStateClosed, false, gh.ViewerActions{CanReopen: true}))

	got, ok := asked(t, m, "enter").(prview.SetStateMsg)
	if !ok {
		t.Fatal("enter on a closed pull request's menu sent no SetStateMsg")
	}
	if got.To != gh.TransitionReopen {
		t.Errorf("To = %q, want %q", got.To, gh.TransitionReopen)
	}
}

// Esc is layered: it closes the menu before it means anything else.
func TestEscClosesTheStateMenuBeforeItMeansBack(t *testing.T) {
	m := openStateMenu(t, stateDetail(gh.PRStateOpen, false, canAct))

	m, cmd := key(m, "esc")
	if m.Capturing() {
		t.Error("esc left the menu up")
	}
	if got := runCmd(cmd); got != nil {
		t.Errorf("esc sent %T, want the key spent on the menu", got)
	}
	if strings.Contains(pickerFrame(m), "Convert to draft") {
		t.Error("the menu is still drawn after esc")
	}
}

// Two items is well under the threshold that earns a filter row, so the menu
// has none and every key is a binding rather than a character.
func TestTheStateMenuHasNoFilterRow(t *testing.T) {
	if menu := stateMenu(t, openStateMenu(t, stateDetail(gh.PRStateOpen, false, canAct))); strings.Contains(menu, "Type to filter") {
		t.Errorf("the state menu shows a filter row:\n%s", menu)
	}
}

// A single-select menu applies the row it is on rather than a checked set, and
// says so.
func TestTheStateMenuNamesItsOwnKeys(t *testing.T) {
	menu := stateMenu(t, openStateMenu(t, stateDetail(gh.PRStateOpen, false, canAct)))

	if !strings.Contains(menu, "⏎ pick") {
		t.Errorf("the menu does not name enter as the pick:\n%s", menu)
	}
	if strings.Contains(menu, "space toggle") {
		t.Errorf("the menu offers a toggle it does not have:\n%s", menu)
	}
}

// The modal is composited over the frame, so it must not change its size.
func TestTheStateMenuDoesNotGrowTheFrame(t *testing.T) {
	sizes := []struct{ width, height int }{
		{width: 200, height: 40},
		{width: 160, height: 24},
		{width: 130, height: 20},
	}

	for _, size := range sizes {
		m := onStateRow(t, detailed(stateDetail(gh.PRStateOpen, false, canAct), size.width, size.height))
		m, _ = key(m, "enter")

		lines := strings.Split(m.View(), "\n")
		if len(lines) != size.height {
			t.Errorf("%dx%d: frame is %d lines, want %d", size.width, size.height, len(lines), size.height)
		}
		for i, line := range lines {
			if w := lipgloss.Width(line); w != size.width {
				t.Errorf("%dx%d: line %d is %d cells, want %d", size.width, size.height, i, w, size.width)
			}
		}
	}
}

// The store moves the state and never the permissions, so for the length of the
// round trip a freshly closed pull request still carries the CanReopen GitHub
// gave for an open one. Believing it drops the row the reader is standing on,
// and the rail cursor goes with it.
func TestTheStateRowKeepsTheCursorThroughAWrite(t *testing.T) {
	m := onStateRow(t, detailed(stateDetail(gh.PRStateOpen, false, canAct), 200, 60))

	if got := markedRailRow(t, m.View()); !isStateRow(got) {
		t.Fatalf("the cursor is on %q before the write, want the State row", got)
	}

	// The optimistic close, exactly as the root applies it: the new state, the
	// permissions from before it, and the write still out.
	closing := stateDetail(gh.PRStateClosed, false, canAct)
	closing.StateWriting = true
	m.SetDetail(closing)

	if got := markedRailRow(t, m.View()); !isStateRow(got) {
		t.Errorf("the cursor left the State row mid-write, onto %q", got)
	}
}

// Once the write answers and the refetch brings the real permissions, a row
// with nothing left to offer goes back to stating a fact.
func TestTheStateRowLetsGoOnceTheWriteHasAnswered(t *testing.T) {
	m := onStateRow(t, detailed(stateDetail(gh.PRStateOpen, false, canAct), 200, 60))

	// Closed, permissions caught up, nothing offered, and nothing in flight.
	m.SetDetail(stateDetail(gh.PRStateClosed, false, gh.ViewerActions{}))

	if got := markedRailRow(t, m.View()); isStateRow(got) {
		t.Errorf("the State row is still the cursor with nothing to offer: %q", got)
	}
}

// A metadata response outlives the ask that started it. By the time it lands
// the reader may have opened a menu that needed no fetch, and dropping the
// label picker over that one would change the choices under their hands between
// one key and the next.
func TestRepoMetaNeverOpensAPickerOverAnOpenOne(t *testing.T) {
	m := onRailRow(t, detailed(held(sampleDetail()), 200, 60), "bug")

	// The label picker is asked for. Nothing opens: its choices are a round trip
	// away.
	m, _ = key(m, "enter")
	if m.Capturing() {
		t.Fatal("the label picker opened before its choices arrived")
	}

	// The reader moves on to the State row, whose menu needs no fetch.
	m = onStateRow(t, m)
	m, _ = key(m, "enter")
	if !m.Capturing() {
		t.Fatal("the state menu did not open")
	}

	m.SetRepo(loadedRepo())

	if menu := stateMenu(t, m); !strings.Contains(menu, "Convert to draft") {
		t.Errorf("the metadata response replaced the open menu:\n%s", pickerFrame(m))
	}
}
