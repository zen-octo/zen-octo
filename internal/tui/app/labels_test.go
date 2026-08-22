package app_test

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/praxis-labs-io/zen-octo/internal/gh"
)

func repoLabelSet() []gh.Label {
	return []gh.Label{
		{ID: "LA_1", Name: "bug"},
		{ID: "LA_2", Name: "enhancement"},
	}
}

// labelling opens the staged pull request with the rail focused and its cursor
// on the row that already carries a label.
//
// The tab count is the rail's own order: the state row, the two add rows above
// the labels section, then the label itself. A change to that order fails the
// picker assertion in every test below rather than passing quietly.
func labelling(t *testing.T, client *fakeSearcher) tea.Model {
	t.Helper()

	client.serveDetail("PR_412", "Caps the backoff at 30s.")
	client.serveLabels("PR_412", repoLabelSet()[:1])
	client.serveRepoMeta(gh.RepoMeta{Labels: repoLabelSet()})

	return press(loaded(t, client, 160, 40), "enter", "1", "j", "j", "j")
}

func openLabelPicker(t *testing.T, client *fakeSearcher) tea.Model {
	t.Helper()

	m := press(labelling(t, client), "enter")
	if out := stripANSI(render(t, m)); !strings.Contains(out, "space toggle") {
		t.Fatalf("the label picker did not open:\n%s", out)
	}
	return m
}

func TestThePickerAsksTheRepositoryOnceForItsChoices(t *testing.T) {
	client := &fakeSearcher{prs: samplePRs()}

	m := openLabelPicker(t, client)
	m = press(m, "esc", "enter") // open it a second time

	if out := stripANSI(render(t, m)); !strings.Contains(out, "space toggle") {
		t.Fatalf("the picker did not open a second time:\n%s", out)
	}
	if got, want := client.metaCalls(), []string{"zen-octo/zen-octo"}; !slices.Equal(got, want) {
		t.Errorf("asked %v, want the repository read once and cached", got)
	}
}

// The rail changing is the acknowledgement, the same way the optimistic comment
// is one for a comment.
func TestALabelReadsOnTheRailBeforeItLands(t *testing.T) {
	client := &fakeSearcher{prs: samplePRs()}
	client.holdPosts()

	m := press(openLabelPicker(t, client), "down", "space", "enter")

	if out := stripANSI(render(t, m)); !strings.Contains(out, "enhancement") {
		t.Errorf("the new label is not on the rail before the write landed:\n%s", out)
	}
	if got, want := client.labelWrites(), []string{"PR_412: LA_1,LA_2"}; !slices.Equal(got, want) {
		t.Errorf("sent %v, want the whole set addressed to the pull request", got)
	}
}

func TestALabelWriteThatLandsSaysSo(t *testing.T) {
	client := &fakeSearcher{prs: samplePRs()}

	m := press(openLabelPicker(t, client), "down", "space", "enter")

	if !strings.Contains(lastLine(render(t, m)), "Labels updated") {
		t.Errorf("status bar = %q, want the write reported", strings.TrimSpace(lastLine(render(t, m))))
	}
	if out := stripANSI(render(t, m)); !strings.Contains(out, "enhancement") {
		t.Errorf("the label came off the rail after landing:\n%s", out)
	}
}

// The revert branch. Nothing was typed, so the fetched set going back on the
// rail is the whole of it, and the toast carries the reason.
func TestAFailedLabelWritePutsTheFetchedSetBack(t *testing.T) {
	client := &fakeSearcher{prs: samplePRs(), postErr: errors.New("502 Bad Gateway")}

	m := press(openLabelPicker(t, client), "down", "space", "enter")

	if out := stripANSI(render(t, m)); strings.Contains(out, "enhancement") {
		t.Errorf("the label stayed on the rail after the write failed:\n%s", out)
	}
	if !strings.Contains(lastLine(render(t, m)), "502 Bad Gateway") {
		t.Errorf("status bar = %q, want the reason on it", strings.TrimSpace(lastLine(render(t, m))))
	}
}

// A sync landing while a write is out must not put the old set back. The store
// holds the edit beside the fetched detail for exactly this.
func TestASyncDoesNotUndoALabelWriteStillInFlight(t *testing.T) {
	client := &fakeSearcher{prs: samplePRs()}
	client.holdPosts()

	m := press(press(openLabelPicker(t, client), "down", "space", "enter"), "s")

	if out := stripANSI(render(t, m)); !strings.Contains(out, "enhancement") {
		t.Errorf("the sync dropped a label whose write is still on its way:\n%s", out)
	}
}

// GitHub is the authority on what the pull request ended up carrying. A label
// deleted from the repository since the picker was filled comes back absent.
func TestTheRailTakesGitHubsAnswerRatherThanTheAsk(t *testing.T) {
	client := &fakeSearcher{prs: samplePRs()}
	// The repository no longer carries the label the picker offered, so the
	// write comes back without it.
	client.serveRepoMeta(gh.RepoMeta{Labels: repoLabelSet()})

	m := openLabelPicker(t, client)
	client.serveRepoMeta(gh.RepoMeta{Labels: repoLabelSet()[:1]})
	m = press(m, "down", "space", "enter")

	if out := stripANSI(render(t, m)); strings.Contains(out, "enhancement") {
		t.Errorf("the rail kept a label GitHub did not confirm:\n%s", out)
	}
}

func TestAFailedRepositoryReadSaysSoAndOpensNoPicker(t *testing.T) {
	client := &fakeSearcher{prs: samplePRs(), metaErr: errors.New("403 Forbidden")}

	m := press(labelling(t, client), "enter")

	out := stripANSI(render(t, m))
	if strings.Contains(out, "space toggle") {
		t.Errorf("a picker opened over choices that never arrived:\n%s", out)
	}
	if !strings.Contains(lastLine(render(t, m)), "403 Forbidden") {
		t.Errorf("status bar = %q, want the reason on it", strings.TrimSpace(lastLine(render(t, m))))
	}
}

// The root stands aside while a picker is up, the same way it does for a
// comment box. q is a letter in a filter.
func TestQDoesNotQuitWhileAPickerIsUp(t *testing.T) {
	client := &fakeSearcher{prs: samplePRs()}

	m := press(openLabelPicker(t, client), "q")

	if out := stripANSI(render(t, m)); !strings.Contains(out, "space toggle") {
		t.Errorf("q reached the root and closed the picker:\n%s", out)
	}
}

// The cache is keyed by repository and the screen is handed only its own. A
// second pull request in another repository must get that repository's labels,
// never the ones already cached for the first.
//
// The mismatch this guards against — a response landing after the reader has
// moved to another repository — is not reachable here: the harness drains every
// command before the next key, so no request is ever still in flight. This
// covers the routing; the guard itself is one line in repoMetaLanded.
func TestEachRepositoryGetsItsOwnChoices(t *testing.T) {
	client := &fakeSearcher{prs: []gh.PullRequest{
		{
			ID: "PR_412", Number: 412, Title: "Fix auth retry", Repository: "zen-octo/zen-octo",
			Author: gh.Actor{Login: "drucial"}, State: gh.PRStateOpen, BaseRefName: "main",
			HeadRefName: "fix-auth", UpdatedAt: time.Now().Add(-2 * time.Hour),
		},
		{
			ID: "PR_9", Number: 9, Title: "Other repo", Repository: "zen-octo/website",
			Author: gh.Actor{Login: "drucial"}, State: gh.PRStateOpen, BaseRefName: "main",
			HeadRefName: "copy", UpdatedAt: time.Now().Add(-3 * time.Hour),
		},
	}}
	client.serveDetail("PR_412", "Caps the backoff at 30s.")
	client.serveDetail("PR_9", "Rewrites the landing copy.")
	client.serveRepoMetaFor("zen-octo/zen-octo", gh.RepoMeta{Labels: repoLabelSet()})
	client.serveRepoMetaFor("zen-octo/website", gh.RepoMeta{Labels: []gh.Label{{ID: "LA_W", Name: "seo"}}})

	// The list's own sort decides which opens first, so each step names the pull
	// request it landed on rather than assuming an order.
	m := press(loaded(t, client, 160, 40), "enter", "1", "j", "j", "j", "enter")
	first := stripANSI(render(t, m))
	if !strings.Contains(first, "#9 Other repo") {
		t.Fatalf("the list opened a different pull request first:\n%s", first)
	}
	if !strings.Contains(first, "seo") {
		t.Fatalf("the website picker does not carry the website's labels:\n%s", first)
	}
	if strings.Contains(first, "enhancement") {
		t.Errorf("the website picker is showing the other repository's labels:\n%s", first)
	}

	m = press(m, "esc", "esc", "esc", "j", "enter", "1", "j", "j", "j", "enter")

	second := stripANSI(render(t, m))
	if !strings.Contains(second, "#412") {
		t.Fatalf("the second open landed on the wrong pull request:\n%s", second)
	}
	if !strings.Contains(second, "enhancement") {
		t.Errorf("the second repository's picker does not carry its own labels:\n%s", second)
	}
	if strings.Contains(second, "seo") {
		t.Errorf("the second repository's picker is showing the first repository's labels:\n%s", second)
	}
	if got, want := len(client.metaCalls()), 2; got != want {
		t.Errorf("asked for metadata %d times, want %d, one per repository", got, want)
	}
}

// Nothing else drops the repository's choices, so without the sync hook a label
// created in the browser stays out of the picker for the rest of the session.
func TestSyncingLetsThePickerSeeANewLabel(t *testing.T) {
	client := &fakeSearcher{prs: samplePRs()}

	m := openLabelPicker(t, client)
	m = press(m, "esc")

	// A third label appears in the repository, and the reader presses s.
	client.serveRepoMeta(gh.RepoMeta{Labels: append(repoLabelSet(), gh.Label{ID: "LA_3", Name: "docs"})})
	m = press(m, "s", "enter")

	if out := stripANSI(render(t, m)); !strings.Contains(out, "docs") {
		t.Errorf("the picker does not offer a label added since the last fetch:\n%s", out)
	}
	if got := client.metaCalls(); len(got) != 2 {
		t.Errorf("asked for metadata %d times, want the sync to have dropped the first answer", len(got))
	}
}
