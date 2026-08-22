package app_test

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/praxis-labs-io/zen-octo/internal/gh"
	"github.com/praxis-labs-io/zen-octo/internal/tui/app"
)

// Below the floor a control goes missing with nothing said. The frame says the
// size instead, in both directions so a floor written as > fails here.
func TestTheFrameSaysWhenTheTerminalIsTooSmall(t *testing.T) {
	tests := []struct {
		width, height int
		message       bool
	}{
		{width: app.MinWidth, height: app.MinHeight, message: false},
		{width: app.MinWidth + 1, height: app.MinHeight + 1, message: false},
		{width: app.MinWidth - 1, height: app.MinHeight, message: true},
		{width: app.MinWidth, height: app.MinHeight - 1, message: true},
		{width: 20, height: 5, message: true},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("%dx%d", tt.width, tt.height), func(t *testing.T) {
			out := stripANSI(render(t, loaded(t, &fakeSearcher{prs: samplePRs()}, tt.width, tt.height)))

			need := fmt.Sprintf("needs %dx%d", app.MinWidth, app.MinHeight)
			if got := strings.Contains(out, need); got != tt.message {
				t.Fatalf("the size message is %v on the frame, want %v:\n%s", got, tt.message, out)
			}
			if !tt.message {
				return
			}

			// Named in full wherever there is room. A frame narrower than the
			// sentence keeps the size it needs and drops the size it is.
			want := fmt.Sprintf("the terminal is %dx%d, and this %s", tt.width, tt.height, need)
			if lipgloss.Width(want) <= tt.width && !strings.Contains(out, want) {
				t.Errorf("the message does not name both sizes:\n%s", out)
			}
		})
	}
}

// The message is the whole frame, so it fills it: a short one leaves the last
// frame's rows on the alt screen under it.
func TestTheSizeMessageFillsTheFrame(t *testing.T) {
	const width, height = 40, 10

	lines := strings.Split(render(t, loaded(t, &fakeSearcher{prs: samplePRs()}, width, height)), "\n")
	if len(lines) != height {
		t.Fatalf("the message is %d lines, want %d", len(lines), height)
	}
	for i, line := range lines {
		if got := lipgloss.Width(line); got != width {
			t.Errorf("line %d is %d cells wide, want %d", i, got, width)
		}
	}
}

// Nothing is thrown away under the message. A terminal dragged small and back
// is the terminal it was, with whatever was open still open.
func TestAPickerSurvivesADragBelowTheFloor(t *testing.T) {
	client := &fakeSearcher{prs: samplePRs()}
	m := openLabelPicker(t, client)

	m = settle(m, tea.WindowSizeMsg{Width: 30, Height: 8})
	if out := stripANSI(render(t, m)); !strings.Contains(out, "needs") {
		t.Fatalf("the frame below the floor is not the message:\n%s", out)
	}

	m = settle(m, tea.WindowSizeMsg{Width: 160, Height: 40})
	if out := stripANSI(render(t, m)); !strings.Contains(out, "space toggle") {
		t.Errorf("the picker did not come back with the terminal:\n%s", out)
	}
}

// serveBypassMerge stages the tallest merge form there is: blocked by a rule
// the viewer may override, with a head branch to delete and every method.
func (f *fakeSearcher) serveBypassMerge(id string) {
	f.serveMergeable(id)

	f.mu.Lock()
	defer f.mu.Unlock()
	held := f.details[id]
	held.Merge = gh.MergeBlocked
	held.Viewer.CanMergeAsAdmin = true
	f.details[id] = held
}

// Both floor numbers are this form's, so the worst one it draws has to fit at
// them. An overlay is clipped rather than scrolled, and the button goes last.
func TestTheTallestMergeFormFitsTheNarrowestFrame(t *testing.T) {
	client := &fakeSearcher{prs: samplePRs()}
	client.serveDetail("PR_412", "Caps the backoff at 30s.")
	client.serveBypassMerge("PR_412")
	client.serveRepoMeta(gh.RepoMeta{
		Methods: gh.MergeMethods{Merge: true, Squash: true, Rebase: true},
	})

	// Under a config notice, which is the row the floor's spare one is for.
	// Without it the form fits a shorter frame and 23 is a row too generous.
	cfg := testConfig()
	cfg.Theme = "rose-pine-dawn"
	sized := drive(t, app.New(cfg, client), tea.WindowSizeMsg{Width: app.MinWidth, Height: app.MinHeight})
	if !strings.Contains(stripANSI(render(t, sized)), "Unknown theme") {
		t.Fatal("setup: no config notice, so the frame is a row taller than this is measuring")
	}

	m := press(sized, "enter", "d", "1", "j", "j", "j", "j", "j")
	if out := stripANSI(render(t, m)); !strings.Contains(out, "Blocked") {
		t.Fatalf("the rail has no Merge row to stand on:\n%s", out)
	}

	out := stripANSI(render(t, press(m, "enter")))
	// "esc cancel" with them: a modal holding the keyboard and naming none of
	// its keys is the failure this whole floor is about, one control over.
	for _, want := range []string{"Rebase and merge", "Delete fix-auth after merging",
		"Bypasses branch protection", "esc cancel", "Merge"} {
		if !strings.Contains(out, want) {
			t.Errorf("the form is missing %q at %dx%d:\n%s", want, app.MinWidth, app.MinHeight, out)
		}
	}

	// Closed at the foot. One row short takes the border and not the button, so
	// a check that reads the rows alone passes over a form hanging open.
	lines := strings.Split(out, "\n")
	foot := strings.Join(lines[min(len(lines), footerRow(t, lines)+1):], "\n")
	if !strings.Contains(foot, "╯") {
		t.Errorf("the form has no bottom border at %dx%d:\n%s", app.MinWidth, app.MinHeight, out)
	}
}

// footerRow is the line the form's hints and its button share, which is the
// last thing it draws above its own border.
func footerRow(t *testing.T, lines []string) int {
	t.Helper()

	for i, line := range lines {
		if strings.Contains(line, "esc cancel") {
			return i
		}
	}
	t.Fatal("the form has no footer, so it is clipped further up than this measures")
	return 0
}

// A key under the message acts on a layout nobody can see, and one of them
// merges. Only the way out of what is open answers.
func TestKeysUnderTheMessageDoNotReachTheScreen(t *testing.T) {
	client := &fakeSearcher{prs: samplePRs()}
	m := settle(openLabelPicker(t, client), tea.WindowSizeMsg{Width: 30, Height: 8})

	// space toggles a label and enter writes the set. Neither may land here.
	m = press(m, " ", "enter")
	if got := client.labelWrites(); len(got) != 0 {
		t.Errorf("a label set was written from under the message: %v", got)
	}

	// esc is the exception, or a picker opened before the drag has no way out.
	m = settle(press(m, "esc"), tea.WindowSizeMsg{Width: 160, Height: 40})
	if out := stripANSI(render(t, m)); strings.Contains(out, "space toggle") {
		t.Errorf("esc under the message did not close the picker:\n%s", out)
	}
}

// The narrowest frame this client draws still reaches the rail, which is the
// only route to state, labels, reviewers, assignees and the base branch.
func TestTheRailReachesTheNarrowestFrame(t *testing.T) {
	client := &fakeSearcher{prs: samplePRs()}
	client.serveDetail("PR_412", "Caps the backoff at 30s.")

	m := press(loaded(t, client, app.MinWidth, app.MinHeight), "enter")
	if out := stripANSI(render(t, m)); !strings.Contains(out, "d details") {
		t.Fatalf("the bar does not name the key that opens the rail:\n%s", out)
	}
	// Whole, not a clipped one: the add row is the far end of the rail's width,
	// and a rail cut short of it is a control the reader cannot read.
	out := stripANSI(render(t, press(m, "d")))
	for _, want := range []string{"Reviewers", "+ Add reviewer"} {
		if !strings.Contains(out, want) {
			t.Errorf("the rail has no %q at %dx%d:\n%s", want, app.MinWidth, app.MinHeight, out)
		}
	}
}
