package prview_test

import (
	"slices"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"

	"github.com/praxis-labs-io/zen-octo/internal/gh"
	"github.com/praxis-labs-io/zen-octo/internal/store"
	"github.com/praxis-labs-io/zen-octo/internal/tui/prview"
	"github.com/praxis-labs-io/zen-octo/internal/tui/theme"
)

// repoLabels is the repository's whole set. The first is the one the fixture
// pull request already carries, so a picker over these opens with one checked.
func repoLabels() []gh.Label {
	return []gh.Label{
		{ID: "LA_1", Name: "bug"},
		{ID: "LA_2", Name: "enhancement"},
		{ID: "LA_3", Name: "documentation"},
	}
}

// repoUsers is who the repository will let you assign. The first is the one the
// fixture pull request already carries, and the author is among them: GitHub
// lists them as assignable and refuses them as a reviewer, which is the one
// difference between the two pickers built over this list.
func repoUsers() []gh.Actor {
	return []gh.Actor{
		{ID: "U_1", Login: "drucial"},
		{ID: "U_2", Login: "nkr"},
		{ID: "U_3", Login: "octobot"},
	}
}

// repoMentions is who can be named in a comment. Wider than repoUsers, the way
// the real connection is: sam has never touched this pull request and cannot be
// assigned to it, and octobot has no name set.
func repoMentions() []gh.Mention {
	return []gh.Mention{
		{Login: "drucial", Name: "Drew White"},
		{Login: "nkr", Name: "Nikita Rushmanov"},
		{Login: "octobot"},
		{Login: "sam", Name: "Sam Reed"},
	}
}

func loadedRepo() store.Repo {
	return store.Repo{
		Meta: gh.RepoMeta{
			Labels:   repoLabels(),
			Users:    repoUsers(),
			Mentions: repoMentions(),
			Methods:  gh.MergeMethods{Merge: true, Squash: true, Rebase: true},
		},
		Status: store.StatusReady,
		Loaded: true,
	}
}

// onRailRow tabs the rail until its cursor is on the row named, so a test names
// what it is acting on rather than counting tab presses to it.
func onRailRow(t *testing.T, m prview.Model, want string) prview.Model {
	t.Helper()

	// The rail is the second pane on the conversation tab. Back to the first
	// control before walking down, because the cursor stops at each end rather
	// than coming back round: a caller already standing below the row it wants
	// would never reach it, and one already on it would step off.
	m = press(m, "1")
	m = press(m, strings.Fields(strings.Repeat("k ", 30))...)

	for range 30 {
		if markedRailRow(t, m.View()) == want {
			return m
		}
		m = press(m, "j")
	}
	t.Fatalf("the cursor never reached the rail row %q", want)
	return m
}

// openPicker walks to a rail row, presses enter, and answers the metadata the
// screen asks for. It returns the screen with the picker up.
func openPicker(t *testing.T, row string) prview.Model {
	t.Helper()

	m := onRailRow(t, detailed(held(sampleDetail()), 200, 60), row)

	_, cmd := key(m, "enter")
	if _, ok := runCmd(cmd).(prview.NeedRepoMetaMsg); !ok {
		t.Fatalf("enter on %q sent %T, want a NeedRepoMetaMsg", row, runCmd(cmd))
	}

	m, _ = key(m, "enter")
	m.SetRepo(loadedRepo())
	return m
}

// pickerFrame is the rendered screen with the modal over it.
func pickerFrame(m prview.Model) string { return stripANSI(m.View()) }

func TestEnterOnALabelRowAsksForTheRepositorysChoices(t *testing.T) {
	m := onRailRow(t, detailed(held(sampleDetail()), 200, 60), "bug")

	got := asked(t, m, "enter")
	want := prview.NeedRepoMetaMsg{Repo: "zen-octo/zen-octo"}
	if got != want {
		t.Fatalf("enter sent %#v, want %#v", got, want)
	}
}

// The choices are the repository's, so a screen already holding them opens the
// picker without asking again.
func TestEnterOpensStraightAwayOnceTheChoicesAreHeld(t *testing.T) {
	m := onRailRow(t, detailed(held(sampleDetail()), 200, 60), "bug")
	m.SetRepo(loadedRepo())

	m, cmd := key(m, "enter")
	if got := runCmd(cmd); got != nil {
		t.Errorf("enter sent %T, want nothing", got)
	}
	if !strings.Contains(pickerFrame(m), "Labels") {
		t.Error("the picker did not open")
	}
}

func TestThePickerOpensOnTheLabelsAlreadyOnThePullRequest(t *testing.T) {
	frame := pickerFrame(openPicker(t, "bug"))

	for _, want := range []string{"bug", "enhancement", "documentation"} {
		if !strings.Contains(frame, want) {
			t.Errorf("the picker does not offer %q:\n%s", want, frame)
		}
	}

	// One checked, two not. The mark is the whole of what says which.
	if !strings.Contains(frame, "✓ bug") {
		t.Errorf("the label already on the pull request is not checked:\n%s", frame)
	}
	for _, off := range []string{"✓ enhancement", "✓ documentation"} {
		if strings.Contains(frame, off) {
			t.Errorf("%q is checked and should not be:\n%s", off, frame)
		}
	}
}

// The add row and a label row open the same picker. Removing is unchecking, so
// there is no second mode for the add row to be.
func TestTheAddRowOpensTheSamePicker(t *testing.T) {
	frame := pickerFrame(openPicker(t, "+ Add label"))

	if !strings.Contains(frame, "Labels") {
		t.Fatalf("the add row did not open the picker:\n%s", frame)
	}
	if !strings.Contains(frame, "✓ bug") {
		t.Errorf("the picker opened without the label already on the pull request:\n%s", frame)
	}
}

func TestCheckingALabelAndApplyingAsksForTheWholeSet(t *testing.T) {
	m := openPicker(t, "bug")

	m = press(m, "down")
	m = press(m, " ") // check enhancement

	got, ok := asked(t, m, "enter").(prview.SetLabelsMsg)
	if !ok {
		t.Fatalf("enter sent %T, want a SetLabelsMsg", asked(t, m, "enter"))
	}

	if got.ID != "PR_412" {
		t.Errorf("ID = %q, want PR_412", got.ID)
	}
	names := make([]string, 0, len(got.Labels))
	for _, l := range got.Labels {
		names = append(names, l.Name)
	}
	if want := []string{"bug", "enhancement"}; !slices.Equal(names, want) {
		t.Errorf("labels = %q, want %q", names, want)
	}
}

// Unchecking the last label is a write, not a no-op. The rail has to be able to
// go empty.
func TestUncheckingEveryLabelAsksForAnEmptySet(t *testing.T) {
	m := openPicker(t, "bug")

	m = press(m, " ") // uncheck bug

	got, ok := asked(t, m, "enter").(prview.SetLabelsMsg)
	if !ok {
		t.Fatal("enter did not ask for a write")
	}
	if len(got.Labels) != 0 {
		t.Errorf("labels = %v, want none", got.Labels)
	}
}

// Applying a picker nobody changed is how a reader backs out of one they opened
// by mistake. It should cost neither a request nor a toast.
func TestApplyingAnUnchangedPickerWritesNothing(t *testing.T) {
	m := openPicker(t, "bug")

	if got := asked(t, m, "enter"); got != nil {
		t.Errorf("an unchanged picker sent %T, want nothing", got)
	}
}

func TestEscClosesThePickerAndLeavesTheScreenWhereItWas(t *testing.T) {
	m := openPicker(t, "bug")

	m, cmd := key(m, "esc")
	if got := runCmd(cmd); got != nil {
		t.Errorf("esc in a picker sent %T, want nothing", got)
	}

	frame := pickerFrame(m)
	if strings.Contains(frame, "space toggle") {
		t.Errorf("the picker is still up:\n%s", frame)
	}
	// Esc closed the modal and nothing else. Backing out of the screen as well
	// would take the reader somewhere they did not ask to go.
	if !strings.Contains(frame, "Details") {
		t.Error("the detail screen went away with the picker")
	}
}

// A picker owns the keyboard. A key that reached the page underneath would
// scroll it out from behind the modal.
func TestThePickerSwallowsKeysMeantForTheScreen(t *testing.T) {
	m := openPicker(t, "bug")

	for _, k := range []string{"]", "d", "s"} {
		if got := asked(t, m, k); got != nil {
			t.Errorf("%q reached the screen behind the picker and sent %T", k, got)
		}
	}
}

func TestCapturingIsTrueWhileAPickerIsUp(t *testing.T) {
	m := detailed(held(sampleDetail()), 200, 60)
	if m.Capturing() {
		t.Fatal("Capturing is true with nothing open")
	}

	if !openPicker(t, "bug").Capturing() {
		t.Error("Capturing is false with a picker up")
	}
}

// The modal is composited over the frame, so it must not change its size.
func TestThePickerDoesNotGrowTheFrame(t *testing.T) {
	sizes := []struct{ width, height int }{
		{width: 200, height: 40},
		{width: 160, height: 24},
		{width: 130, height: 20},
	}

	for _, size := range sizes {
		m := onRailRow(t, detailed(held(sampleDetail()), size.width, size.height), "bug")
		m.SetRepo(loadedRepo())
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

// The picker reads the same as the rows it writes.
func TestThePickerColorsLabelsFromTheTheme(t *testing.T) {
	if !strings.Contains(openPicker(t, "bug").View(), fgSeq(theme.RosePineMoon.Accent)) {
		t.Error("the picker does not color its labels from the theme")
	}
}

// Enter on a rail row with no picker behind it does nothing. A check is
// something a workflow runs rather than something to put on the pull request,
// so the row is walkable and there is nothing to open on it.
func TestEnterOnARowWithNoPickerDoesNothing(t *testing.T) {
	m := onRailRow(t, detailed(held(sampleDetail()), 200, 60), "Rails Unit Tests / test")

	if got := asked(t, m, "enter"); got != nil {
		t.Errorf("enter on a check sent %T, want nothing", got)
	}
}

// Nothing opens before the detail answers. The rail draws from the list row
// then, and its label section is empty.
func TestNoPickerOpensBeforeTheDetailLands(t *testing.T) {
	m := press(screen(200, 60), "1")
	m.SetRepo(loadedRepo())

	m = press(m, "j")
	if got := asked(t, m, "enter"); got != nil {
		t.Errorf("enter before the detail landed sent %T, want nothing", got)
	}
}

func TestTheFilterRowAppearsOnlyOnAListWorthFiltering(t *testing.T) {
	short := pickerFrame(openPicker(t, "bug"))
	if strings.Contains(short, "Type to filter") {
		t.Errorf("a three-label picker shows a filter row:\n%s", short)
	}

	m := onRailRow(t, detailed(held(sampleDetail()), 200, 60), "bug")
	m.SetRepo(store.Repo{Meta: gh.RepoMeta{Labels: manyLabels(12)}, Status: store.StatusReady, Loaded: true})
	m, _ = key(m, "enter")

	if long := pickerFrame(m); !strings.Contains(long, "Type to filter") {
		t.Errorf("a twelve-label picker shows no filter row:\n%s", long)
	}
}

func manyLabels(n int) []gh.Label {
	out := make([]gh.Label, n)
	for i := range out {
		name := "label-" + string(rune('a'+i))
		out[i] = gh.Label{ID: "LA_" + name, Name: name}
	}
	return out
}

// The rail paints its cursor only while it has the keys. With a picker up the
// keys are the picker's, and two lit rows say they go to both.
func TestTheRailKeepsItsCursorPaintedUnderThePicker(t *testing.T) {
	m := openPicker(t, "bug")

	if got := markedRailRow(t, m.View()); got != "bug" {
		t.Errorf("the rail cursor is on %q, want it still on the row the picker was opened from", got)
	}
}

func TestTheSelectedBackgroundIsTheThemes(t *testing.T) {
	if theme.RosePineMoon.SelectedBackground == nil {
		t.Fatal("the theme has no selected background for the picker to paint with")
	}
}

// The repository's label page and the pull request's are both a first page, and
// applying replaces the whole set. A label the picker never listed is one
// nobody could keep checked, so leaving it out of the choices would delete it.
func TestALabelOutsideTheRepositoryPageSurvivesAnApply(t *testing.T) {
	d := sampleDetail()
	d.Labels = append(d.Labels, gh.Label{ID: "LA_OFFPAGE", Name: "page-two"})

	m := onRailRow(t, detailed(held(d), 200, 60), "bug")
	m.SetRepo(loadedRepo()) // LA_OFFPAGE is not in the repository's page
	m, _ = key(m, "enter")

	if frame := pickerFrame(m); !strings.Contains(frame, "✓ page-two") {
		t.Fatalf("the off-page label is not offered and checked:\n%s", frame)
	}

	m = press(m, "down", " ") // check one more, leaving page-two alone

	got, ok := asked(t, m, "enter").(prview.SetLabelsMsg)
	if !ok {
		t.Fatal("enter did not ask for a write")
	}
	var names []string
	for _, l := range got.Labels {
		names = append(names, l.Name)
	}
	if !slices.Contains(names, "page-two") {
		t.Errorf("labels = %q, want the off-page label kept rather than deleted", names)
	}
}

// The chosen set is in the repository's order and the pull request's is in its
// own. Neither query asks for an ordering, so comparing by position calls an
// untouched picker a change and fires the write the check exists to prevent.
func TestAnUntouchedPickerWritesNothingWhenTheOrdersDiffer(t *testing.T) {
	repo := []gh.Label{
		{ID: "LA_1", Name: "bug"},
		{ID: "LA_2", Name: "enhancement"},
	}

	d := sampleDetail()
	// The pull request lists the same two the other way round.
	d.Labels = []gh.Label{repo[1], repo[0]}

	m := onRailRow(t, detailed(held(d), 200, 60), "enhancement")
	m.SetRepo(store.Repo{Meta: gh.RepoMeta{Labels: repo}, Status: store.StatusReady, Loaded: true})
	m, _ = key(m, "enter")

	if got := asked(t, m, "enter"); got != nil {
		t.Errorf("an untouched picker sent %T because the two lists are ordered differently", got)
	}
}

// A metadata fetch is a round trip. The reader may have started writing by the
// time it lands, and the picker answers keys ahead of the box.
func TestAWaitingPickerDoesNotOpenOverAComposeBox(t *testing.T) {
	m := onRailRow(t, detailed(held(sampleDetail()), 200, 60), "bug")

	m, _ = key(m, "enter") // asks for the repository, picker is waiting
	m = press(m, "1", "c") // walk to the conversation and start a comment
	if !m.Composing() {
		t.Fatal("the compose box did not take the keyboard")
	}

	m.SetRepo(loadedRepo())

	if frame := pickerFrame(m); strings.Contains(frame, "space toggle") {
		t.Errorf("the picker dropped over the compose box:\n%s", frame)
	}
	if !m.Composing() {
		t.Error("the picker took the keyboard from the compose box")
	}
}
