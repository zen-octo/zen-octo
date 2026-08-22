package app_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/praxis-labs-io/zen-octo/internal/gh"
	"github.com/praxis-labs-io/zen-octo/internal/tui/app"
)

// setDetailState moves the staged pull request the way somebody working in the
// browser would, so the next recheck answers with it.
func (f *fakeSearcher) setDetailState(id string, state gh.PRState) {
	f.mu.Lock()
	defer f.mu.Unlock()

	held := f.details[id]
	held.State = state
	f.details[id] = held
}

// A recheck is a question nobody asked, so it owes no account of itself. A
// spinner or a toast for one would report a fetch the reader never made.
func TestAPulseSaysNothingOnTheStatusBar(t *testing.T) {
	client := &fakeSearcher{prs: samplePRs()}
	client.serveDetail("PR_412", "Caps the backoff at 30s.")

	m := press(loaded(t, client, 160, 44), "enter")
	client.serveMergeable("PR_412")
	m = settle(m, app.MergeProbe("PR_412"))

	if got := client.pulsed(); len(got) != 1 {
		t.Fatalf("setup: %d rechecks reached the client, want the one the probe makes", len(got))
	}
	bar := lastLine(render(t, m))
	if strings.Contains(bar, "Refreshing") || strings.Contains(bar, "Refreshed") {
		t.Errorf("status bar = %q, want a recheck to pass without saying so", strings.TrimSpace(bar))
	}
}

// The screen keeps what it had. Nothing was waiting on the answer, so an error
// painted over a page the reader is reading fine is the loudest thing on it.
func TestAFailedPulseSaysNothingAndKeepsThePage(t *testing.T) {
	client := &fakeSearcher{prs: samplePRs()}
	client.serveDetail("PR_412", "Caps the backoff at 30s.")
	client.pulseErr = errors.New("502 Bad Gateway")

	m := press(loaded(t, client, 160, 44), "enter")
	m = settle(m, app.MergeProbe("PR_412"))

	out := stripANSI(render(t, m))
	if !strings.Contains(out, "Caps the backoff") {
		t.Errorf("the failed recheck took the conversation with it:\n%s", out)
	}
	if strings.Contains(lastLine(render(t, m)), "502") {
		t.Errorf("status bar = %q, want the failure kept quiet", strings.TrimSpace(lastLine(render(t, m))))
	}
}

// A write settling mid-flight makes the store drop the answer, and the probe
// has spent its one wait. Without another, the Merge row latches on "Checking".
func TestAPulseDroppedByAWriteIsAskedAgain(t *testing.T) {
	client := &fakeSearcher{prs: samplePRs()}
	client.serveDetail("PR_412", "Caps the backoff at 30s.")

	m := press(loaded(t, client, 160, 44), "enter")

	// The probe's recheck is held back, so a write can settle underneath it.
	m, held := holdBack(m, app.MergeProbe("PR_412"), "pulseFetched")
	if len(held) == 0 {
		t.Fatal("setup: the probe started no recheck")
	}

	// Convert to draft while that answer is on its way.
	m = press(m, "1", "enter", "enter")
	client.serveMergeable("PR_412")

	before := len(client.pulsed())
	m = settle(m, held...)

	if got := len(client.pulsed()); got <= before {
		t.Fatal("the dropped recheck was never asked for again")
	}
	if out := stripANSI(render(t, m)); strings.Contains(out, "Checking") {
		t.Errorf("the Merge row latched on Checking:\n%s", out)
	}
}

// The pulse carries the lifecycle, so a pull request merged elsewhere reaches
// the row behind the screen without the whole page being fetched for it.
func TestAPulseCorrectsTheRowBehindTheScreen(t *testing.T) {
	client := &fakeSearcher{prs: samplePRs()}
	client.serveDetail("PR_412", "Caps the backoff at 30s.")

	m := press(loaded(t, client, 160, 44), "enter")

	// Somebody merged it in the browser while the reader had it open.
	client.setDetailState("PR_412", gh.PRStateMerged)
	m = settle(m, app.MergeProbe("PR_412"))

	out := stripANSI(render(t, press(m, "esc")))
	group, ok := groupOf(t, out, "Fix auth retry")
	if !ok {
		t.Fatalf("the pull request left the list entirely:\n%s", out)
	}
	if group != "Merged" {
		t.Errorf("the row sits under %q, want it under Merged once the recheck landed:\n%s", group, out)
	}
}
