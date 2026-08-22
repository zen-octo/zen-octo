package app_test

import (
	"errors"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/praxis-labs-io/zen-octo/internal/gh"
)

func repoUserSet() []gh.Actor {
	return []gh.Actor{
		{ID: "U_1", Login: "drucial"},
		{ID: "U_2", Login: "nkr"},
	}
}

// assigning opens the staged pull request with the rail focused and its cursor
// on the row that adds an assignee.
//
// The tab count is the rail's own order: the state row, the add-reviewer row,
// then this one. A change to that order fails the picker assertion in every
// test below rather than passing quietly.
func assigning(t *testing.T, client *fakeSearcher) tea.Model {
	t.Helper()

	client.serveDetail("PR_412", "Caps the backoff at 30s.")
	client.serveAssignees("PR_412", repoUserSet()[:1])
	client.serveRepoMeta(gh.RepoMeta{Users: repoUserSet()})

	return press(loaded(t, client, 160, 40), "enter", "1", "j", "j")
}

func openAssigneePicker(t *testing.T, client *fakeSearcher) tea.Model {
	t.Helper()

	m := press(assigning(t, client), "enter")
	if out := stripANSI(render(t, m)); !strings.Contains(out, "Assignees") {
		t.Fatalf("the assignee picker did not open:\n%s", out)
	}
	return m
}

// The rail changing is the acknowledgement, the same way the optimistic comment
// is one for a comment.
func TestAnAssigneeReadsOnTheRailBeforeItLands(t *testing.T) {
	client := &fakeSearcher{prs: samplePRs()}
	client.holdPosts()

	m := press(openAssigneePicker(t, client), "down", "space", "enter")

	if out := stripANSI(render(t, m)); !strings.Contains(out, "@nkr") {
		t.Errorf("the new assignee is not on the rail before the write landed:\n%s", out)
	}
	if got, want := client.assigneeWrites(), []string{"PR_412: U_1,U_2"}; !slices.Equal(got, want) {
		t.Errorf("sent %v, want the whole set addressed to the pull request", got)
	}
}

func TestAnAssigneeWriteThatLandsSaysSo(t *testing.T) {
	client := &fakeSearcher{prs: samplePRs()}

	m := press(openAssigneePicker(t, client), "down", "space", "enter")

	if !strings.Contains(lastLine(render(t, m)), "Assignees updated") {
		t.Errorf("status bar = %q, want the write reported", strings.TrimSpace(lastLine(render(t, m))))
	}
	if out := stripANSI(render(t, m)); !strings.Contains(out, "@nkr") {
		t.Errorf("the assignee came off the rail after landing:\n%s", out)
	}
}

// The revert branch. Nothing was typed, so the fetched set going back on the
// rail is the whole of it, and the toast carries the reason.
func TestAFailedAssigneeWritePutsTheFetchedSetBack(t *testing.T) {
	client := &fakeSearcher{prs: samplePRs(), postErr: errors.New("502 Bad Gateway")}

	m := press(openAssigneePicker(t, client), "down", "space", "enter")

	if out := stripANSI(render(t, m)); strings.Contains(out, "@nkr") {
		t.Errorf("the assignee stayed on the rail after the write failed:\n%s", out)
	}
	if !strings.Contains(lastLine(render(t, m)), "502 Bad Gateway") {
		t.Errorf("status bar = %q, want the reason on it", strings.TrimSpace(lastLine(render(t, m))))
	}
}

// A sync landing while a write is out must not put the old set back. The store
// holds the edit beside the fetched detail for exactly this.
func TestASyncDoesNotUndoAnAssigneeWriteStillInFlight(t *testing.T) {
	client := &fakeSearcher{prs: samplePRs()}
	client.holdPosts()

	m := press(press(openAssigneePicker(t, client), "down", "space", "enter"), "s")

	if out := stripANSI(render(t, m)); !strings.Contains(out, "@nkr") {
		t.Errorf("the sync dropped an assignee whose write is still on its way:\n%s", out)
	}
}

// Assigning changes nothing the store cannot already see, so it borrows no
// refetch. The reviewer write beside it is the one that needs one, and paying
// for a round trip here would put a second toast behind the one that already
// said what happened.
func TestAnAssigneeWriteDoesNotRefetchTheDetail(t *testing.T) {
	client := &fakeSearcher{prs: samplePRs()}

	m := openAssigneePicker(t, client)
	before := len(client.opened())

	press(m, "down", "space", "enter")

	if got := len(client.opened()); got != before {
		t.Errorf("the detail was fetched %d more times, want none", got-before)
	}
}
