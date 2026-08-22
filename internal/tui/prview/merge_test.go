package prview_test

import (
	"regexp"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/praxis-labs-io/zen-octo/internal/gh"
	"github.com/praxis-labs-io/zen-octo/internal/store"
	"github.com/praxis-labs-io/zen-octo/internal/tui/prview"
	"github.com/praxis-labs-io/zen-octo/internal/tui/theme"
)

// mergeableDetail is the sample with a merge on offer: clean, with the head
// commit and the branch it sits on, and with GitHub's own commit message for
// each of the two methods that write one.
func mergeableDetail() gh.PullRequestDetail {
	d := sampleDetail()
	d.Merge = gh.MergeClean
	d.HeadRefOid = "9f1c2b7"
	d.HeadRefID = "REF_88"
	d.MergeCommit = gh.MergeMessage{
		Headline: "Merge pull request #412 from zen-octo/fix-auth-retry",
		Body:     "Fix auth retry",
	}
	d.SquashCommit = gh.MergeMessage{
		Headline: "Fix auth retry (#412)",
		Body:     "* Cap the backoff\n\n* Add a test",
	}
	return d
}

// mergeRepo is a repository that permits everything and deletes nothing itself.
func mergeRepo(methods gh.MergeMethods) store.Repo {
	r := loadedRepo()
	r.Meta.Methods = methods
	return r
}

func allMethods() gh.MergeMethods {
	return gh.MergeMethods{Merge: true, Squash: true, Rebase: true}
}

// openMergeOn walks to the Merge row of a given detail, presses enter, and
// answers the metadata the screen asks for. It returns the screen with the form
// up.
func openMergeOn(t *testing.T, d gh.PullRequestDetail, repo store.Repo) prview.Model {
	t.Helper()

	label, _ := mergeLabelOf(d)
	m := onRailRow(t, detailed(held(d), 200, 60), label)

	_, cmd := key(m, "enter")
	if _, ok := runCmd(cmd).(prview.NeedRepoMetaMsg); !ok {
		t.Fatalf("enter on the Merge row sent %T, want a NeedRepoMetaMsg", runCmd(cmd))
	}

	m, _ = key(m, "enter")
	m.SetRepo(repo)
	return m
}

func openMerge(t *testing.T) prview.Model {
	t.Helper()
	return openMergeOn(t, mergeableDetail(), mergeRepo(allMethods()))
}

// mergeLabelOf is what the Merge row says for a state, which is what onRailRow
// walks by.
func mergeLabelOf(d gh.PullRequestDetail) (string, bool) {
	switch d.Merge {
	case gh.MergeClean:
		return "Ready to merge", true
	case gh.MergeUnstable:
		return "Checks failing", true
	case gh.MergeBlocked:
		return "Blocked", true
	case gh.MergeBehind:
		return "Behind the base", true
	case gh.MergeConflicting:
		return "Conflicts", false
	case gh.MergeDraft:
		return "Draft", false
	}
	return "Checking", false
}

// formBox is the merge form cut out of the frame, the way menuBox cuts out a
// picker.
func formBox(t *testing.T, m prview.Model) string {
	t.Helper()
	return menuBox(t, m, "Merge #412")
}

// chosenMethod is whether a method is the one that will be used. Every method
// the repository allows is on the form, so its name being there says nothing:
// the tick beside it is what says it is chosen.
func chosenMethod(box, name string) bool {
	for _, row := range strings.Split(box, "\n") {
		if strings.Contains(row, name) {
			return strings.Contains(row, "✓")
		}
	}
	return false
}

// The row opens on a merge GitHub would take and states a fact on one it would
// refuse. A key that opens a form for a write that can only come back rejected
// is worse than no key.
func TestTheMergeRowIsAControlOnlyWhereThereIsAMergeToMake(t *testing.T) {
	tests := []struct {
		name  string
		state gh.MergeState
		admin bool
		want  bool
	}{
		{"clean", gh.MergeClean, false, true},
		// GitHub's own button merges this one: a red check no rule requires is
		// not a rule.
		{"checks failing", gh.MergeUnstable, false, true},
		{"blocked", gh.MergeBlocked, false, false},
		{"blocked, as an administrator", gh.MergeBlocked, true, true},
		{"behind", gh.MergeBehind, false, false},
		{"behind, as an administrator", gh.MergeBehind, true, true},
		// Nothing lifts a conflict, and nothing merges a draft. The
		// administrator flag makes no difference to either.
		{"conflicts", gh.MergeConflicting, true, false},
		{"draft", gh.MergeDraft, true, false},
		// GitHub has not worked it out yet, which is not the same answer as no.
		{"unknown", gh.MergeUnknown, true, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := mergeableDetail()
			d.Merge = tt.state
			d.Viewer.CanMergeAsAdmin = tt.admin

			label, _ := mergeLabelOf(d)
			m := press(detailed(held(d), 200, 60), "1")

			var reached bool
			for range 30 {
				m = press(m, "j")
				if markedRailRow(t, m.View()) == label {
					reached = true
					break
				}
			}
			if reached != tt.want {
				t.Errorf("the ring stops on the %q row = %v, want %v", label, reached, tt.want)
			}
		})
	}
}

// The lifecycle first. mergeStateStatus says what stands in the way of merging
// and has nothing to say once the merge has happened, so a merged pull request
// reading "Checking" would be the row at its least useful.
func TestAMergedPullRequestSaysSoAndTheRingWalksPast(t *testing.T) {
	d := mergeableDetail()
	d.State = gh.PRStateMerged
	d.Merge = gh.MergeUnknown

	out := stripANSI(detailed(held(d), 200, 60).View())
	if !strings.Contains(out, "Merged into main") {
		t.Errorf("the Merge row does not name where it landed:\n%s", out)
	}

	m := press(detailed(held(d), 200, 60), "1")
	for range 30 {
		m = press(m, "j")
		if strings.Contains(markedRailRow(t, m.View()), "Merged into") {
			t.Error("the ring stopped on a merged pull request's Merge row")
		}
	}
}

// A method the repository forbids is absent rather than greyed: there is
// nothing to be done about it from here.
func TestTheFormOffersOnlyTheMethodsTheRepositoryAllows(t *testing.T) {
	box := formBox(t, openMergeOn(t, mergeableDetail(),
		mergeRepo(gh.MergeMethods{Squash: true, Rebase: true})))

	if strings.Contains(box, "Create a merge commit") {
		t.Errorf("the form offers a method the repository forbids:\n%s", box)
	}
	for _, want := range []string{"Squash and merge", "Rebase and merge"} {
		if !strings.Contains(box, want) {
			t.Errorf("the form is missing %q:\n%s", want, box)
		}
	}
}

// GitHub's own message, per method, rather than one computed here: the
// repository decides whether a squash title is the pull request's or its single
// commit's.
func TestTheFormOpensHoldingGitHubsOwnCommitMessage(t *testing.T) {
	box := formBox(t, openMerge(t))

	if !strings.Contains(box, "Fix auth retry (#412)") {
		t.Errorf("the headline is not GitHub's own squash title:\n%s", box)
	}
	if !strings.Contains(box, "Cap the backoff") {
		t.Errorf("the message is not GitHub's own squash body:\n%s", box)
	}
}

// A merge commit and a squash want different sentences, so switching has to
// rewrite them. Carrying one into the other commits the wrong one.
//
// The squash headline is the shorter of the two, so this also holds the window
// the box shows: a longer value written into a field whose caret then lands
// inside the old window leaves that window where it was, and the box goes on
// showing exactly as many characters as the short one had.
func TestSwitchingMethodRewritesAnUntouchedHeadline(t *testing.T) {
	// The form opens on squash, so up is the merge commit.
	box := formBox(t, press(openMerge(t), "up"))

	if !strings.Contains(box, "Merge pull request #412") {
		t.Errorf("the headline did not follow the method:\n%s", box)
	}
}

// And must not rewrite one somebody has written. The words are theirs, and a
// method key is not an instruction to throw them away.
func TestSwitchingMethodKeepsAnEditedHeadline(t *testing.T) {
	// Onto the headline, type, then four tabs back round to the method rows,
	// where up is the merge commit.
	m := press(openMerge(t), "tab", "!", "tab", "tab", "tab", "tab", "up")

	box := formBox(t, m)
	if !chosenMethod(box, "Create a merge commit") {
		t.Fatalf("setup: the method never changed, so nothing was put at risk:\n%s", box)
	}
	if !strings.Contains(box, "!") {
		t.Errorf("the edit did not survive the method change:\n%s", box)
	}
	if strings.Contains(box, "Merge pull request #412") {
		t.Errorf("the method change overwrote what was typed:\n%s", box)
	}
}

// A rebase writes no commit of its own and GitHub ignores both fields, so a box
// holding a message there is a lie about what is going to be committed.
func TestARebaseDropsTheCommitMessageEntirely(t *testing.T) {
	// Squash first, then down to rebase.
	m := press(openMerge(t), "down")

	box := formBox(t, m)
	for _, gone := range []string{"Headline", "Message", "Fix auth retry (#412)"} {
		if strings.Contains(box, gone) {
			t.Errorf("a rebase still renders %q:\n%s", gone, box)
		}
	}

	// And tab does not stop on them either: one press should reach the delete
	// row rather than a field that is not there.
	got, ok := merged(t, press(m, "tab", "tab"), "enter")
	if !ok {
		t.Fatalf("two tabs did not reach the button on a rebase form")
	}
	if got.Options.Method != gh.MergeMethodRebase {
		t.Errorf("method = %q, want REBASE", got.Options.Method)
	}
	if got.Options.Headline != "" || got.Options.Body != "" {
		t.Errorf("a rebase carried a commit message: %+v", got.Options)
	}
}

// merged presses a key and insists it asked for a merge.
func merged(t *testing.T, m prview.Model, k string) (prview.MergeMsg, bool) {
	t.Helper()

	got, ok := asked(t, m, k).(prview.MergeMsg)
	return got, ok
}

func TestPressingMergeAsksTheRootWithEverythingItNeeds(t *testing.T) {
	// tab three times from the method: headline, message, delete, button.
	got, ok := merged(t, press(openMerge(t), "tab", "tab", "tab", "tab"), "enter")
	if !ok {
		t.Fatal("enter on the button asked for no merge")
	}

	if got.ID != "PR_412" {
		t.Errorf("ID = %q, want PR_412", got.ID)
	}
	if got.Options.Method != gh.MergeMethodSquash {
		t.Errorf("method = %q, want SQUASH", got.Options.Method)
	}
	if got.Options.Headline != "Fix auth retry (#412)" {
		t.Errorf("headline = %q, want GitHub's own", got.Options.Headline)
	}
	// The commit the reader was looking at. Without it a push that landed while
	// they read the diff is merged unseen.
	if got.Options.ExpectedHeadOid != "9f1c2b7" {
		t.Errorf("ExpectedHeadOid = %q, want the head commit", got.Options.ExpectedHeadOid)
	}
	// Checked by default, so the branch goes with the merge.
	if got.RefID != "REF_88" {
		t.Errorf("RefID = %q, want the head branch", got.RefID)
	}
}

// In the headline enter is not a merge. A key that lands a pull request from a
// half-written commit message is the worst thing on this screen.
func TestEnterInTheHeadlineDoesNotMerge(t *testing.T) {
	// Not nil: a text field answers a key with its own caret command. The one
	// thing it must not answer with is a merge.
	if got := asked(t, press(openMerge(t), "tab"), "enter"); got != nil {
		if _, merged := got.(prview.MergeMsg); merged {
			t.Error("enter in the headline merged the pull request")
		}
	}
}

// The branch is the reader's to keep. Unticking has to reach the message, or
// the checkbox is decoration.
func TestUntickingTheDeleteRowKeepsTheBranch(t *testing.T) {
	m := press(openMerge(t), "tab", "tab", "tab", " ")

	got, ok := merged(t, press(m, "tab"), "enter")
	if !ok {
		t.Fatal("enter on the button asked for no merge")
	}
	if got.RefID != "" {
		t.Errorf("RefID = %q, want nothing once the box is unticked", got.RefID)
	}
}

// GitHub deletes the branch itself a moment after the merge, and a second call
// racing that fails on a ref already gone: an error toast about a thing that
// worked.
func TestTheFormOffersNoDeleteWhereTheRepositoryDoesItself(t *testing.T) {
	methods := allMethods()
	methods.DeleteOnMerge = true

	box := formBox(t, openMergeOn(t, mergeableDetail(), mergeRepo(methods)))
	if strings.Contains(box, "after merging") {
		t.Errorf("the form offers a delete the repository is going to make anyway:\n%s", box)
	}
}

// A fork's head is somebody else's branch, and deleting it from here is the one
// refusal worth making without being asked to.
func TestTheFormOffersNoDeleteForAForksHead(t *testing.T) {
	d := mergeableDetail()
	d.CrossRepository = true

	box := formBox(t, openMergeOn(t, d, mergeRepo(allMethods())))
	if strings.Contains(box, "after merging") {
		t.Errorf("the form offers to delete a contributor's own branch:\n%s", box)
	}
}

// A branch already gone has nothing to delete, and deleteRef takes an id rather
// than a name: without one there is nothing to address the call to.
func TestTheFormOffersNoDeleteWithoutABranch(t *testing.T) {
	d := mergeableDetail()
	d.HeadRefID = ""

	box := formBox(t, openMergeOn(t, d, mergeRepo(allMethods())))
	if strings.Contains(box, "after merging") {
		t.Errorf("the form offers to delete a branch that is not there:\n%s", box)
	}
}

// The ordinary case is offered, and this is the test that says so. It exists
// because the first version of this gate read viewerCanDeleteHeadRef, which
// GitHub answers false on every open pull request whatever the account holds:
// every check around it passed and the row never appeared once in a real
// session. A row that could never be offered needs a test that it is.
func TestTheFormOffersTheDeleteOnAnOrdinaryPullRequest(t *testing.T) {
	box := formBox(t, openMerge(t))

	if !strings.Contains(box, "Delete fix-auth-retry after merging") {
		t.Errorf("the form does not offer to delete the head branch at all:\n%s", box)
	}
}

// The one merge here that overrides a rule somebody set on purpose. A form that
// looks identical to the ordinary one hides that.
func TestABypassMergeSaysSo(t *testing.T) {
	d := mergeableDetail()
	d.Merge = gh.MergeBlocked
	d.Viewer.CanMergeAsAdmin = true

	box := formBox(t, openMergeOn(t, d, mergeRepo(allMethods())))
	if !strings.Contains(box, "Bypasses branch protection on main") {
		t.Errorf("a blocked merge does not say it is overriding one:\n%s", box)
	}
}

func TestAnOrdinaryMergeDoesNotClaimToBypassAnything(t *testing.T) {
	if box := formBox(t, openMerge(t)); strings.Contains(box, "Bypasses") {
		t.Errorf("a clean merge claims to override a rule:\n%s", box)
	}
}

// GitHub writes no commit with no subject, so the button says it is not ready
// rather than taking the press and coming back refused.
func TestTheButtonIsInertWithNoHeadline(t *testing.T) {
	// Onto the headline, clear it, then to the button.
	m := press(openMerge(t), "tab")
	for range 30 {
		m = press(m, "ctrl+u")
	}

	if got := asked(t, press(m, "tab", "tab", "tab"), "enter"); got != nil {
		t.Errorf("the button merged with an empty headline, sending %T", got)
	}
}

// esc backs out of the form and writes nothing, which is how a reader leaves
// one they opened by mistake.
func TestEscapeClosesTheFormWithoutMerging(t *testing.T) {
	m := press(openMerge(t), "esc")

	if got := asked(t, m, "enter"); got != nil {
		t.Errorf("a key after esc sent %T, want the form gone", got)
	}
	if strings.Contains(stripANSI(m.View()), "Rebase and merge") {
		t.Error("the form is still on the screen after esc")
	}
}

// The form owns the keyboard while it is up, so the root has to stand aside:
// q is a letter in a commit message.
func TestTheFormCapturesTheKeyboard(t *testing.T) {
	if !openMerge(t).Capturing() {
		t.Error("the form is up and the screen does not report capturing")
	}
}

// The message box is what gives way on a short terminal. The compositor clips
// what will not fit, and what sits at the foot of this modal is the only way to
// merge on a terminal that cannot send the chord.
func TestTheFormKeepsItsButtonOnAShortTerminal(t *testing.T) {
	for _, height := range []int{60, 30, 24, 20, 19} {
		m := onRailRow(t, detailed(held(mergeableDetail()), 200, height), "Ready to merge")
		m, _ = key(m, "enter")
		m.SetRepo(mergeRepo(allMethods()))

		box := formBox(t, m)
		if !strings.Contains(box, "Squash and merge") {
			t.Fatalf("at %d rows the form did not open:\n%s", height, box)
		}

		// The footer row carries both, so finding the hint anywhere is not
		// enough: the button is on the same line and to the right of it.
		rows := strings.Split(strings.TrimRight(box, "\n"), "\n")
		var footer string
		for _, row := range rows {
			if strings.Contains(row, "esc cancel") {
				footer = row
			}
		}
		if footer == "" {
			t.Errorf("at %d rows the footer is off the bottom:\n%s", height, box)
			continue
		}
		if !strings.Contains(footer, "Merge") {
			t.Errorf("at %d rows the button is clipped off its row: %q", height, footer)
		}

		// And the modal closes. The compositor clips from the bottom, so one row
		// too tall takes the border and leaves a box that reads as still going.
		// The button survives that, which is why it is not the thing to assert.
		if last := rows[len(rows)-1]; !strings.Contains(last, "╰") || !strings.Contains(last, "╯") {
			t.Errorf("at %d rows the modal is clipped and never closes: %q", height, last)
		}
	}
}

// The chord merges from wherever the reader is standing, including out of the
// commit message, which is the whole reason it exists: in there enter is a
// newline and the button is four tabs away.
func TestTheChordMergesFromTheCommitMessage(t *testing.T) {
	// Onto the message, where enter cannot mean merge.
	m := press(openMerge(t), "tab", "tab")

	_, cmd := chord(m)
	got, ok := runCmd(cmd).(prview.MergeMsg)
	if !ok {
		t.Fatalf("the chord sent %T, want a MergeMsg", runCmd(cmd))
	}
	if got.Options.Method != gh.MergeMethodSquash {
		t.Errorf("method = %q, want the one the form was left on", got.Options.Method)
	}
}

// Enter presses whatever the row holds, and on the delete row that is the
// checkbox. Space is the other way to it, and a reader who reaches for enter
// everywhere else should not find one row that ignores it.
func TestEnterOnTheDeleteRowTogglesIt(t *testing.T) {
	m := press(openMerge(t), "tab", "tab", "tab", "enter")

	got, ok := merged(t, press(m, "tab"), "enter")
	if !ok {
		t.Fatal("enter on the button asked for no merge")
	}
	if got.RefID != "" {
		t.Errorf("RefID = %q, want nothing once enter has unticked the box", got.RefID)
	}
}

// On a method row the cursor being there is what chose it, so there is nothing
// left for enter to confirm. It moves on rather than sitting dead.
func TestEnterOnAMethodRowMovesOn(t *testing.T) {
	// Enter from the method row, then type: the keys have to have arrived in
	// the headline for that to show up.
	m := press(openMerge(t), "enter", "!")

	box := formBox(t, m)
	if !strings.Contains(box, "Fix auth retry (#412)!") {
		t.Errorf("enter on the method row did not move on to the headline:\n%s", box)
	}
}

// Tab walks the form and shift+tab walks it back, which is what they mean
// everywhere else on this screen.
func TestShiftTabWalksTheFormBackwards(t *testing.T) {
	// One step back from the method row wraps onto the button.
	m, _ := openMerge(t).Update(tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})

	got, ok := merged(t, m, "enter")
	if !ok {
		t.Fatal("shift+tab did not reach the button, so enter merged nothing")
	}
	if got.ID != "PR_412" {
		t.Errorf("ID = %q, want the pull request on screen", got.ID)
	}
}

// Squash is what the form opens on, and where the repository forbids it the
// first method it does allow. A default that is not on offer opens a form whose
// tick is on nothing.
func TestTheFormFallsBackWhenSquashIsForbidden(t *testing.T) {
	box := formBox(t, openMergeOn(t, mergeableDetail(),
		mergeRepo(gh.MergeMethods{Merge: true, Rebase: true})))

	if !chosenMethod(box, "Create a merge commit") {
		t.Errorf("the form did not fall back to the first method allowed:\n%s", box)
	}
}

// The form is the same width over every pull request. The branch name is the
// one variable-length thing on it, and measured into the width it drags the
// modal out over the conversation behind it.
func TestALongBranchNameDoesNotWidenTheForm(t *testing.T) {
	short := mergeableDetail()
	short.HeadRefName = "fix"

	long := mergeableDetail()
	long.HeadRefName = "feature/zno-48-m4-merge-from-the-rail-and-then-some-more-besides"

	got := formBox(t, openMergeOn(t, long, mergeRepo(allMethods())))
	want := formBox(t, openMergeOn(t, short, mergeRepo(allMethods())))

	if wide, narrow := boxWidth(got), boxWidth(want); wide != narrow {
		t.Errorf("the form is %d columns over a long branch and %d over a short one", wide, narrow)
	}

	// Truncated rather than dropped: the row still says what it does, and the
	// branch still starts with enough of itself to recognise.
	if !strings.Contains(got, "after merging") {
		t.Errorf("the row lost the words saying what it does:\n%s", got)
	}
	if !strings.Contains(got, "Delete feature/zno-48") {
		t.Errorf("the branch lost the head of its name:\n%s", got)
	}
}

// boxWidth is how wide a cut-out modal is, in columns.
func boxWidth(box string) int {
	var wide int
	for _, row := range strings.Split(box, "\n") {
		wide = max(wide, len([]rune(stripANSI(row))))
	}
	return wide
}

// The chosen method is the one that will be used, so it is the one that reads.
// At equal weight the tick is the only thing carrying the answer, and it is two
// cells wide.
//
// It reads the colour off the frame rather than the cut-out box, because the
// box is cut from a frame with the escapes already stripped out of it.
func TestOnlyTheChosenMethodIsNotMuted(t *testing.T) {
	frame := openMerge(t).View()

	faint, primary := fgSeq(theme.RosePineMoon.Subtle), fgSeq(theme.RosePineMoon.Text)

	if got := colorBefore(t, frame, "Squash and merge"); got != primary {
		t.Errorf("the chosen method renders in %s, want the primary colour %s", got, primary)
	}
	for _, muted := range []string{"Create a merge commit", "Rebase and merge"} {
		if got := colorBefore(t, frame, muted); got != faint {
			t.Errorf("%q renders in %s, want it muted at %s", muted, got, faint)
		}
	}

	// The heading names its block the way the boxes below name theirs, so it
	// carries a pane title's weight rather than a caption's.
	if got := colorBefore(t, frame, "Method"); got != primary {
		t.Errorf("the Method heading renders in %s, want the box titles' %s", got, primary)
	}
}

// colorBefore is the foreground colour a run of text is rendered in: the last
// one set before it and never reset in between.
func colorBefore(t *testing.T, frame, needle string) string {
	t.Helper()

	at := strings.Index(frame, needle)
	if at < 0 {
		t.Fatalf("%q is nowhere in the frame", needle)
	}

	var color string
	for _, m := range sgr.FindAllStringSubmatch(frame[:at], -1) {
		for _, part := range strings.Split(m[1], ";") {
			if part == "0" {
				color = ""
			}
		}
		// The five parts of the colour and no more. Lipgloss packs bold into the
		// same escape, and a heading that is bold as well as coloured must not
		// read as a different colour for it.
		if at := strings.Index(m[1], "38;2;"); at >= 0 {
			if parts := strings.Split(m[1][at:], ";"); len(parts) >= 5 {
				color = strings.Join(parts[:5], ";")
			}
		}
	}
	return color
}

var sgr = regexp.MustCompile(`\x1b\[([0-9;]*)m`)

// The boxes are sized when the form opens and when the screen resizes, never
// while rendering: render is reached from View through value receivers, so a
// width set there is set on a copy and thrown away. A headline that never
// learns its width renders from its first character and never scrolls, which
// leaves the caret off the box and every keystroke past the edge invisible.
func TestALongHeadlineScrollsAsItIsTyped(t *testing.T) {
	d := mergeableDetail()
	d.SquashCommit.Headline = "Fix the auth retry backoff loop so it stops hammering the endpoint (#412)"

	m := press(openMergeOn(t, d, mergeRepo(allMethods())), "tab")

	before := formBox(t, m)
	after := formBox(t, press(m, "X", "Y", "Z"))

	if before == after {
		t.Errorf("typing changed nothing on screen, so the field never scrolled:\n%s", after)
	}
	if !strings.Contains(after, "XYZ") {
		t.Errorf("the typed characters are nowhere on screen:\n%s", after)
	}
}

// And the same after a resize, which is the other moment the room the boxes get
// changes.
//
// The frame has to narrow far enough to squeeze the form below its floor, or
// the field keeps the width it already had and a resize that never reached it
// looks identical to one that did.
func TestTheFormFollowsAResize(t *testing.T) {
	d := mergeableDetail()
	d.SquashCommit.Headline = "Fix the auth retry backoff loop so it stops hammering the endpoint (#412)"

	m := openMergeOn(t, d, mergeRepo(allMethods()))

	wide := boxWidth(formBox(t, m))
	m.SetSize(48, 40)
	narrow := boxWidth(formBox(t, m))
	if narrow >= wide {
		t.Fatalf("setup: the form is %d columns after the resize and %d before, so nothing narrowed", narrow, wide)
	}

	// Onto the headline and type. A field still holding the old width renders a
	// window wider than the box, and the box clips the end of it off: the caret
	// and everything typed at it land outside what is drawn.
	if after := formBox(t, press(m, "tab", "X", "Y", "Z")); !strings.Contains(after, "XYZ") {
		t.Errorf("what was typed is off the edge of the box the resize left:\n%s", after)
	}
}

// GitHub answers CLEAN on a closed pull request as readily as on an open one,
// and a close applied here moves the state and leaves the merge status alone.
// Reading the status by itself keeps a live control on a pull request nothing
// is going to merge.
func TestAClosedPullRequestOffersNoMerge(t *testing.T) {
	for _, state := range []gh.PRState{gh.PRStateClosed, gh.PRStateMerged} {
		t.Run(string(state), func(t *testing.T) {
			d := mergeableDetail()
			d.State = state

			m := press(detailed(held(d), 200, 60), "1")
			for range 30 {
				m = press(m, "j")
				if row := markedRailRow(t, m.View()); strings.Contains(row, "Ready to merge") {
					t.Fatalf("the ring stopped on a live merge control for a %s pull request", state)
				}
			}
		})
	}
}

// The optimistic merge moves the state under the row that started it. Without
// the write guard the key vanishes between the press and the answer, taking the
// ring stop out from under the reader standing there.
func TestAMergeInFlightKeepsItsRowOnTheRing(t *testing.T) {
	d := mergeableDetail()
	d.State = gh.PRStateMerged // as the optimistic fold leaves it

	writing := held(d)
	writing.StateWriting = true

	m := press(detailed(writing, 200, 60), "1")

	var reached []string
	for range 30 {
		m = press(m, "j")
		reached = append(reached, markedRailRow(t, m.View()))
	}

	var merge, base bool
	for _, row := range reached {
		merge = merge || strings.Contains(row, "Merged into") || strings.Contains(row, "Ready to merge")
		base = base || strings.Contains(row, "main")
	}
	if !merge {
		t.Errorf("the Merge row left the ring while its own write was out: %q", reached)
	}
	if !base {
		t.Errorf("the Base row left the ring while a lifecycle write was out: %q", reached)
	}
}

// A paste is not a keypress. It arrives as its own message, and the form owns
// the keyboard whenever it is up, so a reader pasting a commit body into it
// otherwise sees nothing happen and merges with GitHub's default.
func TestPastingReachesTheCommitMessage(t *testing.T) {
	m := press(openMerge(t), "tab", "tab") // onto the message
	m, _ = m.Update(tea.PasteMsg{Content: "pasted from somewhere else"})

	box := formBox(t, m)
	if !strings.Contains(box, "pasted from somewhere else") {
		t.Errorf("the paste never reached the message box:\n%s", box)
	}
	// And it counts as the reader's own words, so a method switch must not
	// write over it.
	if box := formBox(t, press(m, "tab", "tab", "tab", "up")); !strings.Contains(box, "pasted from") {
		t.Errorf("a method change threw away what was pasted:\n%s", box)
	}
}

// This is the one form whose key ends the pull request, so a hint naming that
// key from a row where it does something else is the worst thing the footer can
// say. Enter merges on the button alone: on the method row it steps to the next
// row, on the delete row it ticks the box, and in a text field it belongs to
// the field.
//
// The fixture's terminal cannot send the chord, so the two text rows point at
// the button. That is also what keeps "⏎ merge" readable as an assertion here:
// the chord hint spells ctrl+⏎ and would match it as a substring.
func TestTheHintNamesAKeyThatWorksFromTheRowItIsOn(t *testing.T) {
	tests := []struct {
		row  string
		want string
	}{
		{row: "the method", want: "j/k method"},
		{row: "the headline", want: "tab to the button to merge"},
		{row: "the message", want: "tab to the button to merge"},
		{row: "delete the branch", want: "space toggle"},
		{row: "the button", want: "⏎ merge"},
	}

	m := openMerge(t)
	for i, tt := range tests {
		if i > 0 {
			m = press(m, "tab")
		}

		box := formBox(t, m)
		if !strings.Contains(box, tt.want) {
			t.Errorf("on %s the hint does not name %q:\n%s", tt.row, tt.want, box)
		}
		// Every row but the button had this, and pressing it moved the form
		// instead of merging.
		if tt.want != "⏎ merge" && strings.Contains(box, "⏎ merge") {
			t.Errorf("on %s the hint says enter merges, and it does not:\n%s", tt.row, box)
		}
	}
}

// The hint changes with the row and the modal must not change with it, or the
// box jumps under the reader as they tab through.
func TestTheHintDoesNotResizeTheForm(t *testing.T) {
	m := openMerge(t)

	want := boxWidth(formBox(t, m))
	for i := range 4 {
		m = press(m, "tab")
		if got := boxWidth(formBox(t, m)); got != want {
			t.Errorf("after %d tabs the form is %d columns, want %d", i+1, got, want)
		}
	}
}

// The fetch is a round trip and tab is free the whole time it is out, so an
// answer landing late must not drop a modal over whatever row the reader walked
// to. It matters most for the merge form, which owns every key once it is up.
func TestARepositoryAnswerDoesNotOpenOverAnotherRow(t *testing.T) {
	for _, tt := range []struct{ name, row, walkTo string }{
		{"the merge form", "Ready to merge", "+ Add reviewer"},
		{"a label picker", "+ Add label", "+ Add assignee"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			m := onRailRow(t, detailed(held(mergeableDetail()), 200, 60), tt.row)

			// Ask, then walk away while the fetch is out.
			m, _ = key(m, "enter")
			m = onRailRow(t, m, tt.walkTo)
			m.SetRepo(mergeRepo(allMethods()))

			if out := stripANSI(m.View()); strings.Contains(out, "╭─Merge #412") ||
				strings.Contains(out, "╭─Labels") {
				t.Errorf("a modal opened over the row the reader walked to:\n%s", out)
			}
			if m.Capturing() {
				t.Error("the screen is capturing the keyboard for a modal nobody asked for here")
			}
		})
	}
}

// And still opens for a reader who stayed put, which is the whole point of the
// deferred ask.
func TestARepositoryAnswerStillOpensWhereTheReaderStayed(t *testing.T) {
	m := onRailRow(t, detailed(held(mergeableDetail()), 200, 60), "Ready to merge")

	m, _ = key(m, "enter")
	m.SetRepo(mergeRepo(allMethods()))

	if !strings.Contains(stripANSI(m.View()), "Squash and merge") {
		t.Error("the form did not open for a reader still standing on the row")
	}
}

// The base is the other variable-length name on the form, and left unclipped it
// widens the modal the same way the branch name did.
func TestALongBaseNameDoesNotWidenTheBypassWarning(t *testing.T) {
	d := mergeableDetail()
	d.Merge = gh.MergeBlocked
	d.Viewer.CanMergeAsAdmin = true
	d.BaseRefName = "release/2026.08-the-long-lived-integration-branch-nobody-renamed"

	got := boxWidth(formBox(t, openMergeOn(t, d, mergeRepo(allMethods()))))
	want := boxWidth(formBox(t, openMerge(t)))

	if got != want {
		t.Errorf("the form is %d columns over a long base and %d over a short one", got, want)
	}
}
