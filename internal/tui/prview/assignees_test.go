package prview_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/praxis-labs-io/zen-octo/internal/gh"
	"github.com/praxis-labs-io/zen-octo/internal/store"
	"github.com/praxis-labs-io/zen-octo/internal/tui/prview"
)

// openAssignees opens the Assignees picker from the add row under the section.
func openAssignees(t *testing.T) prview.Model {
	t.Helper()
	return openPicker(t, "+ Add assignee")
}

// assigneeLogins is the logins a SetAssigneesMsg carries, in the order it
// carries them.
func assigneeLogins(msg prview.SetAssigneesMsg) []string {
	out := make([]string, 0, len(msg.Assignees))
	for _, a := range msg.Assignees {
		out = append(out, a.Login)
	}
	return out
}

// The section is the picker, so the row that adds and the rows already there
// both open it. A reader pointing at somebody to take them off should not have
// to walk down to the add row first.
func TestEnterOnAnAssigneeOpensTheSamePickerAsTheAddRow(t *testing.T) {
	for _, row := range []string{"@drucial", "+ Add assignee"} {
		t.Run(row, func(t *testing.T) {
			m := openPicker(t, row)
			if got := menuBox(t, m, "Assignees"); !strings.Contains(got, "@nkr") {
				t.Errorf("the picker did not open over the repository's people:\n%s", got)
			}
		})
	}
}

// The set already on the pull request opens checked, so applying an untouched
// picker is a no-op rather than a write that clears it.
func TestTheAssigneePickerOpensOnWhoIsAlreadyAssigned(t *testing.T) {
	got := menuBox(t, openAssignees(t), "Assignees")

	for _, line := range strings.Split(got, "\n") {
		if strings.Contains(line, "@drucial") && !strings.Contains(line, "✓") {
			t.Errorf("the assignee already on the pull request is not checked:\n%s", got)
		}
		if strings.Contains(line, "@nkr") && strings.Contains(line, "✓") {
			t.Errorf("somebody who is not assigned opened checked:\n%s", got)
		}
	}
}

func TestCheckingAnAssigneeAndApplyingAsksForTheWholeSet(t *testing.T) {
	m := openAssignees(t)

	m = press(m, "down") // onto @nkr, under the checked @drucial
	m = press(m, " ")

	got, ok := asked(t, m, "enter").(prview.SetAssigneesMsg)
	if !ok {
		t.Fatalf("enter sent %T, want a SetAssigneesMsg", asked(t, m, "enter"))
	}

	if got.ID != "PR_412" {
		t.Errorf("ID = %q, want PR_412", got.ID)
	}
	if want := []string{"drucial", "nkr"}; !slices.Equal(assigneeLogins(got), want) {
		t.Errorf("assignees = %q, want %q", assigneeLogins(got), want)
	}
	// The node id is what updatePullRequest takes, and the login alone would
	// come back rejected.
	if got.Assignees[1].ID != "U_2" {
		t.Errorf("assignees[1].ID = %q, want the node id", got.Assignees[1].ID)
	}
}

// Unchecking the last one is a real write. It is how a reader clears the
// section, and skipping it would leave the row on screen with nothing to say
// why.
func TestUncheckingEveryAssigneeAsksForAnEmptySet(t *testing.T) {
	m := press(openAssignees(t), " ") // the cursor opens on the checked @drucial

	got, ok := asked(t, m, "enter").(prview.SetAssigneesMsg)
	if !ok {
		t.Fatalf("enter sent %T, want a SetAssigneesMsg", asked(t, m, "enter"))
	}
	if len(got.Assignees) != 0 {
		t.Errorf("assignees = %q, want none", assigneeLogins(got))
	}
}

// Applying an untouched picker is how a reader backs out of one they opened by
// mistake, and it should cost neither a request nor a toast.
func TestApplyingAnUnchangedAssigneePickerWritesNothing(t *testing.T) {
	if got := asked(t, openAssignees(t), "enter"); got != nil {
		t.Errorf("an untouched picker sent %T, want nothing", got)
	}
}

// Both lists are a first page, and applying replaces the whole set. Somebody
// the picker never listed is somebody nobody could keep checked, so leaving
// them out would unassign them with nothing on screen to say so.
func TestTheAssigneePickerListsSomebodyTheRepositoryPageMissed(t *testing.T) {
	d := sampleDetail()
	d.Assignees = append(d.Assignees, gh.Actor{ID: "U_9", Login: "ghost"})

	m := onRailRow(t, detailed(held(d), 200, 60), "+ Add assignee")
	m, _ = key(m, "enter")
	m.SetRepo(loadedRepo())

	box := menuBox(t, m, "Assignees")
	if !strings.Contains(box, "@ghost") {
		t.Fatalf("an assignee the repository's page did not reach is missing:\n%s", box)
	}
	for _, line := range strings.Split(box, "\n") {
		if strings.Contains(line, "@ghost") && !strings.Contains(line, "✓") {
			t.Errorf("the extra assignee is listed but not checked, so applying would drop them:\n%s", box)
		}
	}
}

// Nobody can be assigned where GitHub says the viewer may not. A row offering a
// write it will refuse is worse than a row stating a fact.
func TestTheAssigneeSectionIsInertWithoutPermission(t *testing.T) {
	d := sampleDetail()
	d.Viewer.CanAssign = false

	frame := stripANSI(detailed(held(d), 200, 60).View())
	if strings.Contains(frame, "+ Add assignee") {
		t.Error("the add row is offered to a viewer who cannot assign")
	}
	// The people already on it still read, because that is a fact rather than
	// an offer.
	if !strings.Contains(frame, "@drucial") {
		t.Error("the assignees themselves came off the rail with the add row")
	}
}

func TestTheRingWalksPastTheAssigneesWithoutPermission(t *testing.T) {
	d := sampleDetail()
	d.Viewer.CanAssign = false

	m := press(detailed(held(d), 200, 60), "1")
	for range 20 {
		m = press(m, "j")
		if got := markedRailRow(t, m.View()); got == "@drucial" || got == "+ Add assignee" {
			t.Fatalf("the ring stopped on %q, which the viewer cannot change", got)
		}
	}
}

// Before the detail lands nothing is known about what the viewer may do, which
// is not the same as nothing being allowed. Dropping the rows early would move
// every stop under them the moment the answer came.
func TestTheAssigneeRowsKeepTheirKeysBeforeTheDetailLands(t *testing.T) {
	// Unloaded, which is every false the zero value carries: no permissions,
	// and no answer about permissions either.
	loading := store.Detail{Status: store.StatusLoading}

	if !strings.Contains(stripANSI(detailed(loading, 200, 60).View()), "+ Add assignee") {
		t.Error("the add row went before the detail said anything about permissions")
	}
}
