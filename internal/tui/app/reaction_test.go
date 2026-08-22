package app_test

import (
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/praxis-labs-io/zen-octo/internal/gh"
)

// serveReactable stages a thread whose one comment GitHub will take a reaction
// for, with one somebody else already gave.
func (f *fakeSearcher) serveReactable(id string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.details == nil {
		f.details = make(map[string]gh.PullRequestDetail)
	}
	held := f.details[id]
	held.Threads = []gh.ReviewThread{{
		ID: "RT_1", Path: "internal/gh/client.go", Line: 42,
		Side: gh.SideRight, CanReply: true, CanResolve: true,
		Comments: []gh.Comment{{
			Kind: gh.CommentThread, ID: "RC_1", Author: gh.Actor{Login: "nkr"},
			Body: "This backs off forever.", CanReact: true,
			Reactions: []gh.Reaction{{Content: gh.ReactionHeart, Count: 1}},
		}},
	}}
	f.details[id] = held
}

// reactingOn opens the staged pull request, puts the ring on its one thread and
// opens the reaction list. Two tabs, because the ring walks the description
// first.
func reactingOn(t *testing.T, client *fakeSearcher) tea.Model {
	t.Helper()

	client.serveDetail("PR_412", "Caps the backoff at 30s.")
	client.serveReactable("PR_412")

	return press(loaded(t, client, 160, 40), "enter", "2", "}", "+")
}

// The pill moves before GitHub has seen it, which is the whole of what
// optimistic means, and the write goes out addressed to the comment's node.
func TestAReactionShowsBeforeItLands(t *testing.T) {
	client := &fakeSearcher{prs: samplePRs()}
	client.holdPosts()

	m := press(reactingOn(t, client), "enter")

	if out := stripANSI(render(t, m)); !strings.Contains(out, "👍 1") {
		t.Errorf("the pill is not on the card yet:\n%s", out)
	}
	if got := client.reactions(); len(got) != 1 || got[0] != "RC_1 THUMBS_UP true" {
		t.Errorf("sent %v, want the reaction addressed to the comment", got)
	}
}

// GitHub's answer is the subject's whole set, so the count it reports replaces
// the one the press guessed.
func TestALandedReactionTakesGitHubsCount(t *testing.T) {
	client := &fakeSearcher{prs: samplePRs()}

	m := press(reactingOn(t, client), "enter")

	if out := stripANSI(render(t, m)); !strings.Contains(out, "👍 9") {
		t.Errorf("the card kept the optimistic count:\n%s", out)
	}
}

// It says nothing. The pill is already on the card, and a toast per reaction
// would spend the status bar on the smallest write on this screen.
func TestALandedReactionRaisesNoToast(t *testing.T) {
	client := &fakeSearcher{prs: samplePRs()}

	m := press(reactingOn(t, client), "enter")

	if got := strings.TrimSpace(lastLine(render(t, m))); strings.Contains(got, "eact") {
		t.Errorf("status bar = %q, want the reaction to land quietly", got)
	}
}

// The revert branch. Nothing was typed, so the pill going back is the whole of
// it, and the toast carries the reason.
func TestAFailedReactionGoesBack(t *testing.T) {
	client := &fakeSearcher{prs: samplePRs(), postErr: errors.New("502 Bad Gateway")}

	m := press(reactingOn(t, client), "enter")

	out := stripANSI(render(t, m))
	if strings.Contains(out, "👍") {
		t.Errorf("the pill stayed on the card after the write failed:\n%s", out)
	}
	if !strings.Contains(out, "❤️ 1") {
		t.Errorf("the reaction that was already there went with it:\n%s", out)
	}
	if !strings.Contains(lastLine(render(t, m)), "502 Bad Gateway") {
		t.Errorf("status bar = %q, want the reason on it", strings.TrimSpace(lastLine(render(t, m))))
	}
}

// A sync landing while a reaction is out must not take the pill off. The store
// holds it beside the fetched detail for exactly this.
func TestASyncDoesNotUndoAReactionStillInFlight(t *testing.T) {
	client := &fakeSearcher{prs: samplePRs()}
	client.holdPosts()

	m := press(press(reactingOn(t, client), "enter"), "s")

	if out := stripANSI(render(t, m)); !strings.Contains(out, "👍 1") {
		t.Errorf("the sync dropped a reaction still on its way:\n%s", out)
	}
}
