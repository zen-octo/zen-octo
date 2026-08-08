package prview

// focusKind is what a focusable thing is. An action key reads it to know what
// it has been handed: a reply belongs on a thread and nowhere else.
type focusKind int

const (
	// focusNone is the zero value, so an untouched ring is an unfocused one.
	// Every screen opens that way: the reader came to read, not to act.
	focusNone focusKind = iota
	focusDescription
	focusComment
	focusReview
	focusThread
	focusState
	focusReviewer
	focusAssignee
	focusLabel
	focusCheck
	focusMerge

	// The add rows sit under what they add to rather than among it. Each opens
	// a picker, so an action key reads the kind and knows it has a control
	// rather than a reviewer, an assignee or a label.
	focusAddReviewer
	focusAddAssignee
	focusAddLabel
)

// prose is whether this kind renders a markdown body, which is the only thing
// a fold key has to work on.
func (k focusKind) prose() bool {
	switch k {
	case focusDescription, focusComment, focusReview, focusThread:
		return true
	}
	return false
}

// focusKey names one focusable thing: what it is and which one.
//
// Which one is a place in the slice it came from, and for a comment or a review
// that slice is the timeline. Appending to it is safe, which is the common case
// and the only one a refresh usually produces. Reordering it is not: a rebase
// re-sorts commits into the list by date, and focus and whatever the reader
// unfolded then name the card that took the index. Comments carry no id of
// their own until ZNO-28 adds one, which is what a stable key needs.
type focusKey struct {
	kind  focusKind
	index int
}

// focusItem is a key with where it landed. start and lines are in the body the
// pane was handed, before the blank line every pane opens with.
type focusItem struct {
	focusKey
	start int
	lines int
}

// covers reports whether the item shows any part of itself in a window.
func (it focusItem) covers(top, height int) bool {
	return it.start < top+height && it.start+it.lines > top
}

// ring is the focus order of one pane. The items are rebuilt on every render;
// on survives it, because it names what it points at rather than where that
// landed on the screen.
type ring struct {
	items []focusItem

	// lead is what the tab put above the first item. The conversation opens
	// with its header block, and without this every item is that many lines out.
	lead int

	on focusKey
}

// reset empties the items for a fresh render, keeping the focus and the lead.
func (r *ring) reset() { r.items = r.items[:0] }

// add records one focusable block at the line it was written to.
func (r *ring) add(key focusKey, start, lines int) {
	r.items = append(r.items, focusItem{focusKey: key, start: start, lines: lines})
}

// focused reports whether a key is the one holding focus. A zero key is never
// focused, so a caller with nothing to name does not light the whole pane up.
func (r ring) focused(key focusKey) bool {
	return key.kind != focusNone && key == r.on
}

// index is where the focus sits in the current items, or minus one when the
// thing it names is no longer on the screen.
func (r ring) index() int {
	if r.on.kind == focusNone {
		return -1
	}
	for i, it := range r.items {
		if it.focusKey == r.on {
			return i
		}
	}
	return -1
}

// live is whether the focus is on the screen. Scrolled out of the window it is
// nothing the reader can see, so it is nothing for a key to act on either. This
// is the rule step re-anchors by, and every key that reads the focus holds to
// it: one that acted on a card off screen would move the page under a reader
// who had already left it.
func (r ring) live(top, height int) bool {
	at := r.index()
	return at >= 0 && r.items[at].covers(top, height)
}

// clear drops the focus, and reports whether there was one to drop. The screen
// reads that answer to decide whether esc backs out or only lets go.
func (r *ring) clear() bool {
	had := r.on.kind != focusNone
	r.on = focusKey{}
	return had
}

// step moves the focus one item and reports whether the ring took the key. top
// and height are the window the pane is showing, in the same lines the items
// were recorded in.
//
// Focus does not survive being scrolled out of the window. A reader who scrolled
// away has moved on, and the one thing the ring must not do is haul them back to
// a card they left behind. So it re-anchors to what is on the screen now.
func (r *ring) step(delta, top, height int) bool {
	if len(r.items) == 0 {
		r.on = focusKey{}
		return false
	}

	at := r.index()
	if at < 0 || !r.items[at].covers(top, height) {
		r.on = r.items[r.anchor(delta, top, height)].focusKey
		return true
	}

	r.on = r.items[(at+delta+len(r.items))%len(r.items)].focusKey
	return true
}

// anchor is where focus lands when there is none to move: the first item on the
// screen going forward, the last going back. A window between two items falls to
// whichever end it is nearer.
//
// On the screen means any part of it, not all of it. A card taller than the
// window is the one the reader is looking at, and a scan for the first item to
// begin below the top skips straight past it to the next one.
func (r ring) anchor(delta, top, height int) int {
	if delta < 0 {
		for i := len(r.items) - 1; i >= 0; i-- {
			if r.items[i].covers(top, height) {
				return i
			}
		}
		return 0
	}
	for i, it := range r.items {
		if it.covers(top, height) {
			return i
		}
	}
	return len(r.items) - 1
}

// show is the offset that brings the focused item onto a window of the given
// height. An item already on screen whole leaves the page where it is, because
// the highlight is signal enough and scrolling under a reader who can already
// see the thing is worse than not scrolling.
//
// Anything else goes to the top row rather than the shortest distance onto the
// screen. The shortest distance lands a card at the foot of the window, and
// what a card is worth reading for is the replies under it.
func (r ring) show(top, height int) int {
	at := r.index()
	if at < 0 || height <= 0 {
		return top
	}

	it := r.items[at]
	if it.start >= top && it.start+it.lines <= top+height {
		return top
	}
	return it.start
}
