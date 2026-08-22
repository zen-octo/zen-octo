package prview

// The tests in this package rather than beside it, because neither reaches a
// frame. editorCommand reads the environment and returns a command line, and
// wrappedRows is arithmetic over a width the page never states. Exporting
// either so a black-box test could call it would be widening the package for
// the test's convenience.

import (
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/praxis-labs-io/zen-octo/internal/gh"
	"github.com/praxis-labs-io/zen-octo/internal/store"
	"github.com/praxis-labs-io/zen-octo/internal/tui/syntax"
	"github.com/praxis-labs-io/zen-octo/internal/tui/theme"
)

// editorFixture is a detail screen with one answerable thread on it.
func editorFixture(t *testing.T) Model {
	t.Helper()

	now := time.Now()
	rev := gh.Comment{Kind: gh.CommentReview, ID: "REV_1", Author: gh.Actor{Login: "nkr"}, CreatedAt: now}
	d := gh.PullRequestDetail{
		PullRequest: gh.PullRequest{ID: "PR_1", Number: 1, Title: "t", Repository: "o/r"},
		Body:        "desc",
		Timeline: []gh.TimelineItem{
			{Kind: gh.TimelineReview, Actor: gh.Actor{Login: "nkr"}, CreatedAt: now, Comment: &rev},
		},
		Threads: []gh.ReviewThread{{
			ID: "RT_1", ReviewID: "REV_1", Path: "a.go", Line: 1, Side: gh.SideRight, CanReply: true,
			Comments: []gh.Comment{
				{Kind: gh.CommentThread, ID: "RC_1", Author: gh.Actor{Login: "nkr"}, CreatedAt: now, Body: "one"},
			},
		}},
	}

	syn, _ := syntax.New(theme.RosePineMoon.Syntax)
	m := New(theme.RosePineMoon, d.PullRequest, RailPreference{}, syn)
	m.SetDetail(store.Detail{Detail: d, Status: store.StatusReady, Loaded: true})
	m.SetSize(200, 60)
	// The rail leads on arrival; these tests are about the page beside it.
	m = pressKeys(m, "2")
	return m
}

func pressKeys(m Model, keys ...string) Model {
	for _, k := range keys {
		m, _ = m.Update(tea.KeyPressMsg{Code: rune(k[0]), Text: k})
	}
	return m
}

var seqs = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func stripSeqs(s string) string { return seqs.ReplaceAllString(s, "") }

// The editor is the reader's, in the order every other terminal program reads
// them, and vi when they have named none.
func TestTheEditorIsTheOneTheReaderNamed(t *testing.T) {
	tests := []struct {
		name     string
		visual   string
		editor   string
		wantName string
		wantArgs []string
	}{
		{name: "neither set", wantName: "vi"},
		{name: "EDITOR", editor: "nvim", wantName: "nvim"},
		{name: "VISUAL wins", visual: "hx", editor: "nvim", wantName: "hx"},
		{name: "arguments come with it", editor: "code -w", wantName: "code", wantArgs: []string{"-w"}},
		{name: "blank is unset", editor: "   ", wantName: "vi"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("VISUAL", tt.visual)
			t.Setenv("EDITOR", tt.editor)

			name, args := editorCommand()
			if name != tt.wantName {
				t.Errorf("editor = %q, want %q", name, tt.wantName)
			}
			if !slices.Equal(args, tt.wantArgs) {
				t.Errorf("args = %q, want %q", args, tt.wantArgs)
			}
		})
	}
}

// The editor's text goes to the box that opened it. editorDoneMsg is unexported,
// so a black-box test cannot deliver one; the assertion is still on the frame.
func TestTheEditorWritesBackToTheBoxThatOpenedIt(t *testing.T) {
	tests := []struct {
		name  string
		open  []string
		want  string
		other string
	}{
		{
			name:  "a reply box",
			open:  []string{"}", "}", "r"},
			want:  "write a reply",
			other: "write a comment",
		},
		{
			name:  "the compose card",
			open:  []string{"c"},
			want:  "write a comment",
			other: "write a reply",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := pressKeys(editorFixture(t), tt.open...)
			if !m.Composing() {
				t.Fatal("no box has the keyboard")
			}

			m, _ = m.Update(editorDoneMsg{body: "written elsewhere\n"})
			frame := stripSeqs(m.View())

			if !strings.Contains(frame, "written elsewhere") {
				t.Fatalf("the editor's text is nowhere on the page:\n%s", frame)
			}

			// The text has to be under the box that opened the editor, which is
			// the one whose heading comes last before it.
			at := strings.Index(frame, "written elsewhere")
			mine := strings.LastIndex(frame[:at], tt.want)
			theirs := strings.LastIndex(frame[:at], tt.other)
			if mine < 0 || theirs > mine {
				t.Errorf("the text landed under %q rather than %q:\n%s", tt.other, tt.want, frame)
			}
		})
	}
}

// The editor opens on whatever the box already holds, so a draft survives the
// round trip instead of being replaced by what comes back.
func TestTheEditorOpensOnTheBoxsOwnWords(t *testing.T) {
	path, err := draftFile("half an answer")
	if err != nil {
		t.Fatalf("draftFile: %v", err)
	}
	defer func() { _ = os.Remove(path) }()

	out, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading the draft back: %v", err)
	}
	if string(out) != "half an answer" {
		t.Errorf("the editor would open on %q, want the words already in the box", out)
	}
}

// The box is sized by this, so a count that folds anywhere the textarea does
// not leaves it a row short of its own writing and scrolling where it was
// supposed to grow.
func TestWrappedRowsFoldsWhereTheTextareaFolds(t *testing.T) {
	cases := []struct {
		name  string
		text  string
		width int
		want  int
	}{
		// A character count fits these in two rows. No two of them share one:
		// each is more than half the width, and a word is not cut in half to
		// fill a line.
		{"words too long to pair", "aaaaaa bbbbbb cccccc", 10, 3},
		{"a word longer than the width", "aaaaaaaaaaaaaaa", 10, 2},
		{"a line that fills the width", "aaaaaaaaaa", 10, 1},
		{"nothing", "", 10, 1},
		{"a blank line is still a row", "one\n\ntwo", 10, 3},
		// No width to fold at yet, which is every call before the first render.
		{"no width", "one\ntwo", 0, 2},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := wrappedRows(c.text, c.width); got != c.want {
				t.Errorf("wrappedRows(%q, %d) = %d, want %d", c.text, c.width, got, c.want)
			}
		})
	}
}
