package app_test

import (
	"errors"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/praxis-labs-io/zen-octo/internal/gh"
)

// moving opens the staged pull request with the rail focused and its cursor on
// the State row, which is the rail's first stop.
func moving(t *testing.T, client *fakeSearcher) tea.Model {
	t.Helper()

	client.serveDetail("PR_412", "Caps the backoff at 30s.")

	return press(loaded(t, client, 160, 40), "enter", "1")
}

// openStateMenu opens the rail's state menu. Nothing is fetched for it, unlike
// the label picker, so this is one key.
func openStateMenu(t *testing.T, client *fakeSearcher) tea.Model {
	t.Helper()

	m := press(moving(t, client), "enter")
	if out := stripANSI(render(t, m)); !strings.Contains(out, "Convert to draft") {
		t.Fatalf("the state menu did not open:\n%s", out)
	}
	return m
}

// The rail changing is the acknowledgement, the same way the optimistic comment
// is one for a comment.
func TestAStateChangeReadsOnTheRailBeforeItLands(t *testing.T) {
	client := &fakeSearcher{prs: samplePRs()}
	client.holdPosts()

	m := press(openStateMenu(t, client), "enter")

	if out := stripANSI(render(t, m)); !strings.Contains(out, "Draft") {
		t.Errorf("the rail does not read as a draft before the write landed:\n%s", out)
	}
	if got, want := client.stateWrites(), []string{"PR_412: DRAFT"}; !slices.Equal(got, want) {
		t.Errorf("sent %v, want the transition addressed to the pull request", got)
	}
}

// Close is the second item, so it takes a step down first.
func TestClosingSendsTheCloseTransition(t *testing.T) {
	client := &fakeSearcher{prs: samplePRs()}
	client.holdPosts()

	m := press(openStateMenu(t, client), "j", "enter")

	if got, want := client.stateWrites(), []string{"PR_412: CLOSE"}; !slices.Equal(got, want) {
		t.Errorf("sent %v, want a close", got)
	}
	if out := stripANSI(render(t, m)); !strings.Contains(out, "Closed") {
		t.Errorf("the rail does not read as closed before the write landed:\n%s", out)
	}
}

func TestAStateWriteThatLandsSaysSo(t *testing.T) {
	client := &fakeSearcher{prs: samplePRs()}

	m := press(openStateMenu(t, client), "enter")

	if !strings.Contains(lastLine(render(t, m)), "Converted to draft") {
		t.Errorf("status bar = %q, want the write reported", strings.TrimSpace(lastLine(render(t, m))))
	}
	if out := stripANSI(render(t, m)); !strings.Contains(out, "Draft") {
		t.Errorf("the rail went back after landing:\n%s", out)
	}
}

// Half the rail hangs off the state through fields the store cannot compute, so
// the write asks for the whole detail again once it settles.
func TestAStateWriteRefetchesTheDetail(t *testing.T) {
	client := &fakeSearcher{prs: samplePRs()}

	m := openStateMenu(t, client)
	before := len(client.opened())
	press(m, "enter")

	if got := len(client.opened()) - before; got != 1 {
		t.Errorf("the detail was fetched %d more times, want 1 after the write settled", got)
	}
}

// The list renders the row search returned, and a lifecycle change made here
// is the freshest thing this session has about it.
func TestClosingAPullRequestCorrectsTheRowBehindIt(t *testing.T) {
	client := &fakeSearcher{prs: samplePRs()}

	closed := press(openStateMenu(t, client), "j", "enter")

	out := stripANSI(render(t, press(closed, "esc")))
	group, ok := groupOf(t, out, "Fix auth retry")
	if !ok {
		t.Fatalf("the pull request left the list entirely:\n%s", out)
	}
	if group != "Closed" {
		t.Errorf("the row sits under %q, want it under Closed once the write landed:\n%s", group, out)
	}
}

// groupOf is the group header the named row sits under, which is what the list
// says about a pull request's lifecycle.
func groupOf(t *testing.T, frame, row string) (string, bool) {
	t.Helper()

	var group string
	for _, line := range strings.Split(frame, "\n") {
		for _, name := range []string{"Ready", "Draft", "Merged", "Closed"} {
			if strings.Contains(line, name) {
				group = name
			}
		}
		if strings.Contains(line, row) {
			return group, true
		}
	}
	return "", false
}

// The refetch borrows no refresh leg, so the toast that says what happened is
// the only one raised. A summary behind it would report the same action twice.
func TestAStateWriteRaisesOneToast(t *testing.T) {
	client := &fakeSearcher{prs: samplePRs()}

	m := press(openStateMenu(t, client), "enter")

	bar := lastLine(render(t, m))
	if strings.Contains(bar, "Refreshed") {
		t.Errorf("status bar = %q, want no refresh summary behind the write's own toast", strings.TrimSpace(bar))
	}
	if !strings.Contains(bar, "Converted to draft") {
		t.Errorf("status bar = %q, want the write reported", strings.TrimSpace(bar))
	}
}

// The revert branch. Nothing was typed, so the fetched state going back on the
// rail is the whole of it, and the toast carries the reason and the move.
func TestAFailedStateWritePutsTheFetchedStateBack(t *testing.T) {
	client := &fakeSearcher{prs: samplePRs(), postErr: errors.New("403 Forbidden")}

	m := press(openStateMenu(t, client), "enter")

	if out := stripANSI(render(t, m)); strings.Contains(out, "Draft") {
		t.Errorf("the rail stayed a draft after the write failed:\n%s", out)
	}
	bar := lastLine(render(t, m))
	if !strings.Contains(bar, "403 Forbidden") {
		t.Errorf("status bar = %q, want the reason on it", strings.TrimSpace(bar))
	}
	if !strings.Contains(bar, "convert it to a draft") {
		t.Errorf("status bar = %q, want the move that failed named", strings.TrimSpace(bar))
	}
}

// A sync landing while a write is out must not put the old state back. The
// store holds the edit beside the fetched detail for exactly this.
func TestASyncDoesNotUndoAStateWriteStillInFlight(t *testing.T) {
	client := &fakeSearcher{prs: samplePRs()}
	client.holdPosts()

	m := press(press(openStateMenu(t, client), "enter"), "s")

	if out := stripANSI(render(t, m)); !strings.Contains(out, "Draft") {
		t.Errorf("the sync dropped a state change still on its way:\n%s", out)
	}
}

// GitHub is the authority on where the pull request ended up, not the ask.
func TestTheRailTakesGitHubsAnswerForTheState(t *testing.T) {
	client := &fakeSearcher{prs: samplePRs()}
	// GitHub says it closed rather than went to draft, which is what a race
	// against somebody working in the browser looks like.
	client.serveState("PR_412", gh.PRStateClosed, false)

	m := press(openStateMenu(t, client), "enter")

	if out := stripANSI(render(t, m)); !strings.Contains(out, "Closed") {
		t.Errorf("the rail kept the ask rather than GitHub's answer:\n%s", out)
	}
}

// The menu takes the keyboard the way the label picker does.
func TestQDoesNotQuitWhileTheStateMenuIsUp(t *testing.T) {
	client := &fakeSearcher{prs: samplePRs()}

	m := press(openStateMenu(t, client), "q")

	if out := stripANSI(render(t, m)); !strings.Contains(out, "Convert to draft") {
		t.Errorf("q reached the root and closed the menu:\n%s", out)
	}
}

// A detail fetch asked for before the write answers from the state the pull
// request was in beforehand. Taking it would put the close back on screen
// undone, and the permissions that come with it would leave the row inert.
//
// The answer is held back by hand rather than by a slow fake: the pump drops
// anything that does not answer within its own window, so a held response is
// the only way to make one land after something else.
func TestASyncInFlightDoesNotUndoALandedStateWrite(t *testing.T) {
	client := &fakeSearcher{prs: samplePRs()}

	// The sync has to start before the menu opens: a picker owns every key while
	// it is up, so s pressed over one syncs nothing.
	m := moving(t, client)

	m, stale := holdBack(m, keyMsg("s"), "detailFetchedMsg")
	if len(stale) == 0 {
		t.Fatal("the sync key started no detail fetch")
	}

	// Close while that response is still on its way.
	m = press(m, "enter", "j", "enter")
	if out := stripANSI(render(t, m)); !strings.Contains(out, "Closed") {
		t.Fatalf("the close never reached the rail:\n%s", out)
	}

	// The sync answers now, carrying the pull request from before the close.
	m = settle(m, stale...)

	if out := stripANSI(render(t, m)); !strings.Contains(out, "Closed") {
		t.Errorf("the stale response put the close back undone:\n%s", out)
	}
}

// The toast names the state the pull request landed in, not the move that was
// asked for. They part company when somebody moves it first.
func TestTheToastNamesWhereItLandedNotWhatWasAsked(t *testing.T) {
	client := &fakeSearcher{prs: samplePRs()}
	// Somebody closed it in the browser, so the draft conversion answers CLOSED.
	client.serveState("PR_412", gh.PRStateClosed, false)

	m := press(openStateMenu(t, client), "enter")

	bar := lastLine(render(t, m))
	if strings.Contains(bar, "Converted to draft") {
		t.Errorf("status bar = %q, want it not to claim a move that did not happen", strings.TrimSpace(bar))
	}
	if !strings.Contains(strings.ToLower(bar), "closed") {
		t.Errorf("status bar = %q, want the state it landed in", strings.TrimSpace(bar))
	}
}

// A sync pressed while the write's own refetch is out has to report when that
// refetch lands. The write records no refresh leg by design, so before this the
// key found a fetch in flight, refused to start one, recorded nothing, and the
// answer arrived unclaimed. The reader gets no spinner and no toast, and presses
// the key again.
func TestSyncWaitsOnTheRefetchAWriteStarted(t *testing.T) {
	client := &fakeSearcher{prs: samplePRs()}

	// Convert to draft, and hold back the refetch the write fires.
	m, refetch := holdBack(openStateMenu(t, client), keyMsg("enter"), "detailFetchedMsg")
	if len(refetch) == 0 {
		t.Fatal("the write started no refetch")
	}

	m = press(m, "s")
	m = settle(m, refetch...)

	if got := lastLine(render(t, m)); !strings.Contains(got, "Refreshed") {
		t.Errorf("status bar = %q, want the sync reported when the refetch landed", strings.TrimSpace(got))
	}
}
