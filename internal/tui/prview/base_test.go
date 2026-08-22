package prview_test

import (
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/praxis-labs-io/zen-octo/internal/gh"
	"github.com/praxis-labs-io/zen-octo/internal/store"
	"github.com/praxis-labs-io/zen-octo/internal/tui/prview"
)

// repoBranches is what a search for nothing returns: newest first, which is the
// order internal/gh sorts them into. "main" is the fixture's own base and
// "fix-auth-retry" is its head.
func repoBranches(names ...string) store.Branches {
	if len(names) == 0 {
		names = []string{"develop", "main", "release/2.0", "fix-auth-retry"}
	}
	return store.Branches{Default: "main", Names: names, Status: store.StatusReady, Loaded: true}
}

// openBase walks to the Base row, presses enter, and answers the search the
// screen asks for. It returns the screen with the picker up.
func openBase(t *testing.T, b store.Branches) prview.Model {
	t.Helper()

	m := onRailRow(t, detailed(held(sampleDetail()), 200, 60), "4 commits behind main")

	_, cmd := key(m, "enter")
	if _, ok := runCmd(cmd).(prview.NeedBranchesMsg); !ok {
		t.Fatalf("enter on the Base row sent %T, want a NeedBranchesMsg", runCmd(cmd))
	}

	m, _ = key(m, "enter")
	m.SetBranches(b)
	return m
}

// The row says how far behind the branch is and is a stop on the ring, because
// retargeting changes the branch that number is measured against.
func TestEnterOnTheBaseRowAsksForTheRepositorysBranches(t *testing.T) {
	m := onRailRow(t, detailed(held(sampleDetail()), 200, 60), "4 commits behind main")

	got := asked(t, m, "enter")
	want := prview.NeedBranchesMsg{Repo: "zen-octo/zen-octo"}
	if got != want {
		t.Fatalf("enter sent %#v, want %#v", got, want)
	}
}

// The default is where most retargets land, so it is offered first whatever the
// search order says. Copilot is pinned in the reviewer picker for the same
// reason.
func TestTheBasePickerOffersTheDefaultBranchFirst(t *testing.T) {
	// The search answers develop, main, release/2.0, fix-auth-retry. Pinning the
	// default puts main first, ahead of the branch the search sorted newest.
	box := menuBox(t, openBase(t, repoBranches()), "Merge into")

	for _, row := range strings.Split(box, "\n") {
		if !strings.Contains(row, "main") && !strings.Contains(row, "develop") &&
			!strings.Contains(row, "release/") {
			continue
		}
		if !strings.Contains(row, "main") {
			t.Errorf("the first branch offered is not the default:\n%s", box)
		}
		return
	}
	t.Errorf("the picker offers no branches at all:\n%s", box)
}

// GitHub refuses a pull request onto its own head, so a row for it could only
// ever fail.
func TestTheBasePickerNeverOffersTheHeadBranch(t *testing.T) {
	box := menuBox(t, openBase(t, repoBranches()), "Merge into")

	if strings.Contains(box, "fix-auth-retry") {
		t.Errorf("the picker offers the head branch:\n%s", box)
	}
}

// Single select, opened on what is already set. Without the cursor starting
// there, enter on a picker opened by mistake retargets onto whatever sorted
// newest.
func TestTheBasePickerOpensOnTheBranchAlreadySet(t *testing.T) {
	m := openBase(t, repoBranches())

	if _, cmd := key(m, "enter"); runCmd(cmd) != nil {
		t.Errorf("enter with no movement sent %#v, want nothing", runCmd(cmd))
	}
}

func TestChoosingABranchAsksTheRootToRetarget(t *testing.T) {
	m := openBase(t, repoBranches())

	// Off the checked row and onto another. Which one it lands on is what the
	// message has to carry.
	m = press(m, "j")
	got := asked(t, m, "enter")

	want, ok := got.(prview.SetBaseMsg)
	if !ok {
		t.Fatalf("enter sent %T, want a SetBaseMsg", got)
	}
	if want.Base == "" || want.Base == "main" {
		t.Errorf("SetBaseMsg carries %q, want a branch other than the one already set", want.Base)
	}
	if want.ID != "PR_412" {
		t.Errorf("SetBaseMsg carries id %q, want PR_412", want.ID)
	}
}

// A repository whose newest thirty branches do not include the one this pull
// request targets, and whose default is something else again. Without the union
// the picker opens with nothing checked, the cursor falls to the first row, and
// enter retargets onto whatever sorted newest.
func TestTheCurrentBaseIsOfferedWhenNeitherTheSearchNorTheDefaultCarriesIt(t *testing.T) {
	m := openBase(t, store.Branches{
		Default: "trunk",
		Names:   []string{"develop", "release/2.0"},
		Status:  store.StatusReady, Loaded: true,
	})

	if box := menuBox(t, m, "Merge into"); !strings.Contains(box, "main") {
		t.Errorf("the branch already set is not offered:\n%s", box)
	}
	if _, cmd := key(m, "enter"); runCmd(cmd) != nil {
		t.Errorf("enter with no movement sent %#v, want nothing", runCmd(cmd))
	}
}

// The default is pinned on the list a picker opens over and on no other. Once
// there is a search the reader is looking for something specific, and a row at
// the top they did not ask for is one enter can land on by accident.
//
// The default here matches the search, which is the only shape where a wrong
// pin is visible: one that does not match is hidden by the filter over it
// whether it was pinned or not.
func TestASearchDoesNotPinTheDefaultBranch(t *testing.T) {
	m := openBase(t, repoBranches("develop", "main", "release/2.0", "release/1.9",
		"feature/rail", "feature/base", "spike/glamour", "fix/scroll"))

	m = press(m, "r", "e", "l")
	m.SetBranches(store.Branches{
		Query: "rel", Default: "release/legacy",
		Names:  []string{"release/2.0", "release/1.9"},
		Status: store.StatusReady, Loaded: true,
	})

	if box := menuBox(t, m, "Merge into"); strings.Contains(box, "release/legacy") {
		t.Errorf("the default is offered above what the search matched:\n%s", box)
	}
}

// The filter is the search on this picker, and it does not run per keystroke: a
// word typed at speed sets a wait per letter and only the last one asks.
func TestTypingAsksForThatSearchOnceTheFilterSettles(t *testing.T) {
	m := openBase(t, repoBranches("develop", "main", "release/2.0", "release/1.9",
		"feature/rail", "feature/base", "spike/glamour", "fix/scroll"))

	var last tea.Msg
	for _, r := range []string{"r", "e", "l"} {
		var cmd tea.Cmd
		m, cmd = key(m, r)
		if cmd == nil {
			t.Fatalf("typing %q armed no wait", r)
		}
		last = runCmd(cmd)
	}

	settle, ok := last.(prview.BranchSettleMsg)
	if !ok {
		t.Fatalf("the wait carried %T, want a BranchSettleMsg", last)
	}
	if settle.Query != "rel" {
		t.Fatalf("the wait names %q, want the whole word typed", settle.Query)
	}

	_, cmd := m.Update(settle)
	got, ok := runCmd(cmd).(prview.NeedBranchesMsg)
	if !ok {
		t.Fatalf("the settled wait sent %T, want a NeedBranchesMsg", runCmd(cmd))
	}
	if got.Query != "rel" {
		t.Errorf("the search asks for %q, want %q", got.Query, "rel")
	}
}

// A wait the reader has typed past drops itself. Otherwise five letters are
// five requests and the last four are for searches nobody is running.
func TestAWaitForAFilterThatMovedOnAsksForNothing(t *testing.T) {
	m := openBase(t, repoBranches())

	if _, cmd := m.Update(prview.BranchSettleMsg{Query: "stale"}); runCmd(cmd) != nil {
		t.Errorf("a wait for a filter nobody is holding asked for %#v", runCmd(cmd))
	}
}

// The list is replaced under the filter rather than the picker being rebuilt,
// so what was typed survives the answer landing.
func TestASearchLandingKeepsWhatWasTyped(t *testing.T) {
	m := openBase(t, repoBranches("develop", "main", "release/2.0", "release/1.9",
		"feature/rail", "feature/base", "spike/glamour", "fix/scroll"))

	m = press(m, "r", "e", "l")
	m.SetBranches(store.Branches{
		Query: "rel", Default: "main",
		Names:  []string{"release/2.0", "release/1.9"},
		Status: store.StatusReady, Loaded: true,
	})

	box := menuBox(t, m, "Merge into")
	if strings.Contains(box, "Type to filter") {
		t.Errorf("the answer landing cleared the filter:\n%s", box)
	}
	if !strings.Contains(box, "release/2.0") {
		t.Errorf("the picker did not take the search's answer:\n%s", box)
	}
}

// A search that matched more than it returned says so. Silence there reads as a
// repository with thirty branches.
func TestASearchWithMoreMatchesSaysSo(t *testing.T) {
	b := repoBranches()
	b.More = 36

	if box := menuBox(t, openBase(t, b), "Merge into"); !strings.Contains(box, "36 more") {
		t.Errorf("the picker does not say what the search left out:\n%s", box)
	}
}

// retargeting is a detail mid-write: the branch moved, nothing has counted it.
func retargeting(writing bool) store.Detail {
	d := sampleDetail()
	d.BaseRefName = "develop"
	d.BehindBy = gh.BehindUnknown

	out := held(d)
	out.BaseWriting = writing
	return out
}

// The old number was measured against a branch this pull request no longer
// targets, so there is nothing honest to put in its place until the refetch
// answers.
func TestTheBaseRowSaysNothingAboutACountItDoesNotHave(t *testing.T) {
	for _, c := range []struct {
		name    string
		writing bool
		want    string
	}{
		{"the write is out", true, "Retargeting to develop"},
		{"the write has landed", false, "Merging into develop"},
	} {
		t.Run(c.name, func(t *testing.T) {
			out := stripANSI(detailed(retargeting(c.writing), 200, 60).View())
			if !strings.Contains(out, c.want) {
				t.Errorf("the rail is missing %q:\n%s", c.want, out)
			}
			if strings.Contains(out, "behind develop") {
				t.Errorf("the rail counts commits against a branch nobody compared:\n%s", out)
			}
		})
	}
}

// A merged pull request has no head branch, so nothing compared it to its base.
// "Merging into" is the retarget's word and would report a write nobody made.
func TestTheBaseRowNamesTheBaseWithNothingToCompare(t *testing.T) {
	d := sampleDetail()
	d.State = gh.PRStateMerged
	d.BehindBy = gh.BehindNoHead

	out := stripANSI(detailed(held(d), 200, 60).View())
	if !strings.Contains(out, "Based on main") {
		t.Errorf("the rail is missing %q:\n%s", "Based on main", out)
	}
	for _, wrong := range []string{"behind main", "Merging into main", "Up to date with main"} {
		if strings.Contains(out, wrong) {
			t.Errorf("the rail says %q about a comparison that never ran:\n%s", wrong, out)
		}
	}
}

// railStops is every row tab lands on while walking the rail, which is what
// says whether a row is a control or a fact.
//
// The rail has to be focused first. Walking the conversation's own ring instead
// reaches none of these rows and reports every one of them as a fact.
func railStops(t *testing.T, d gh.PullRequestDetail) []string {
	t.Helper()

	m := press(detailed(held(d), 200, 60), "1")
	var out []string
	for range 30 {
		m = press(m, "j")
		row := markedRailRow(t, m.View())
		if slices.Contains(out, row) {
			break
		}
		out = append(out, row)
	}
	// The State row is not it: it carries a glyph, and it is not a stop on a
	// merged pull request, which is one of the cases below.
	if !slices.Contains(out, "+ Add label") {
		t.Fatalf("the walk never reached the rail at all: %q", out)
	}
	return out
}

// A merged pull request cannot be retargeted and GitHub refuses the write.
// viewerCanUpdate stays true on one, because its title and body are still
// editable, so the state is what this has to read.
func TestTheBaseRowIsAFactOnAMergedPullRequest(t *testing.T) {
	d := sampleDetail()
	d.State = gh.PRStateMerged

	if stops := railStops(t, d); slices.Contains(stops, "4 commits behind main") {
		t.Errorf("tab stopped on the Base row of a merged pull request: %q", stops)
	}
}

// No write access, no control. The row states a fact the way an empty Checks
// section does.
func TestTheBaseRowIsAFactWithoutWriteAccess(t *testing.T) {
	d := sampleDetail()
	d.Viewer.CanUpdate = false

	if stops := railStops(t, d); slices.Contains(stops, "4 commits behind main") {
		t.Errorf("tab stopped on the Base row a reader cannot write to: %q", stops)
	}
}

// The other half of both gates: with write access on an open pull request the
// row is a control, so the two tests above cannot pass by never reaching it.
func TestTheBaseRowIsAControlOnAnOpenPullRequest(t *testing.T) {
	if stops := railStops(t, sampleDetail()); !slices.Contains(stops, "4 commits behind main") {
		t.Errorf("tab never stopped on the Base row: %q", stops)
	}
}

// A pull request from a fork carries the head's name and not its repository, so
// a contributor's main merging into this one matches the head filter by name
// alone. Dropped, the picker opens with nothing checked and enter retargets onto
// whatever sorted first.
func TestAForkPullRequestStillOffersTheBaseItsHeadIsNamedAfter(t *testing.T) {
	d := sampleDetail()
	d.HeadRefName = "main" // the fork's own main
	d.BaseRefName = "main"

	m := onRailRow(t, detailed(held(d), 200, 60), "4 commits behind main")
	m, _ = key(m, "enter")
	m.SetBranches(repoBranches("develop", "release/2.0"))

	if box := menuBox(t, m, "Merge into"); !strings.Contains(box, "main") {
		t.Errorf("the branch this pull request targets is not offered:\n%s", box)
	}
	if _, cmd := key(m, "enter"); runCmd(cmd) != nil {
		t.Errorf("enter with no movement sent %#v, want nothing", runCmd(cmd))
	}
}

// A search answering is not something the reader did. Moving onto a row while
// the request is out and having it reanchor underneath sends the write to
// whichever branch the new list sorted first.
func TestASearchLandingLeavesTheCursorOnTheRowItWasOn(t *testing.T) {
	m := openBase(t, repoBranches("develop", "main", "release/2.0", "release/1.9",
		"feature/rail", "feature/base", "spike/glamour", "fix/scroll"))

	m = press(m, "down") // off main, onto develop
	m.SetBranches(store.Branches{
		Default: "main",
		Names:   []string{"release/2.0", "develop", "main", "feature/rail"},
		Status:  store.StatusReady, Loaded: true,
	})

	got := asked(t, m, "enter")
	msg, ok := got.(prview.SetBaseMsg)
	if !ok {
		t.Fatalf("enter sent %T, want a SetBaseMsg", got)
	}
	if msg.Base != "develop" {
		t.Errorf("the answer moved the cursor: enter wrote %q, want develop", msg.Base)
	}
}

// The rail keeping focus is not the reader still standing on Base. Enter starts
// the search and the ring is free the whole time it is out.
func TestASearchLandingAfterWalkingAwayOpensNothing(t *testing.T) {
	m := onRailRow(t, detailed(held(sampleDetail()), 200, 60), "4 commits behind main")
	m, _ = key(m, "enter")

	// Up, because Base is the last control on this rail and the cursor stops
	// there rather than coming back round.
	m = press(m, "k")
	m.SetBranches(repoBranches())

	if out := stripANSI(m.View()); strings.Contains(out, "Merge into") {
		t.Errorf("the modal dropped over a row the reader had walked to:\n%s", out)
	}
}

// Labels asked for, then Base asked for. The repository answers first and must
// not open the picker waiting on branches it has not been handed.
func TestRepoMetaLandingDoesNotOpenTheBranchPicker(t *testing.T) {
	m := onRailRow(t, detailed(held(sampleDetail()), 200, 60), "bug")
	m, _ = key(m, "enter") // want = pickLabels

	m = onRailRow(t, m, "4 commits behind main")
	m, _ = key(m, "enter") // want = pickBase

	m.SetRepo(loadedRepo())

	if out := stripANSI(m.View()); strings.Contains(out, "Merge into") {
		t.Errorf("repo metadata opened a picker waiting on a branch search:\n%s", out)
	}
}
