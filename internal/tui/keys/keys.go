// Package keys declares every binding once, with its help text attached, so the
// help view renders from the same declaration Update matches on. A rebind moves
// both or neither.
package keys

import "charm.land/bubbles/v2/key"

// GlobalMap answers whatever has focus. The root model handles these before it
// delegates to a screen.
type GlobalMap struct {
	Help key.Binding
	Quit key.Binding
}

// ListMap is live while the pull request list has focus.
type ListMap struct {
	Up           key.Binding
	Down         key.Binding
	Top          key.Binding
	Bottom       key.Binding
	PageUp       key.Binding
	PageDown     key.Binding
	HalfPageUp   key.Binding
	HalfPageDown key.Binding
	NextSection  key.Binding
	PrevSection  key.Binding
	Open         key.Binding
	Refresh      key.Binding
}

// DetailMap is live on the pull request detail screen. The same movement keys
// serve every pane; focus decides what they move.
//
// FocusPane is one binding over the digits rather than one per pane, because
// the Files tab puts a third pane on screen and the panes are numbered by where
// they sit rather than by what they hold.
//
// FocusNext and FocusPrev own tab and shift+tab, which the tab strip used to
// answer to as well. The strip keeps the brackets: the ring is the key a reader
// reaches for many times on one pull request, and the strip a handful.
type DetailMap struct {
	Up           key.Binding
	Down         key.Binding
	Top          key.Binding
	Bottom       key.Binding
	PageUp       key.Binding
	PageDown     key.Binding
	HalfPageUp   key.Binding
	HalfPageDown key.Binding
	NextTab      key.Binding
	PrevTab      key.Binding
	NextFile     key.Binding
	PrevFile     key.Binding
	FocusNext    key.Binding
	FocusPrev    key.Binding
	PaneLeft     key.Binding
	PaneRight    key.Binding
	FocusPane    key.Binding
	ToggleRail   key.Binding
	Expand       key.Binding
	Refresh      key.Binding
	Back         key.Binding
}

// Global, List, and Detail are the declarations. Config-driven rebinding lands
// later; until then these are the only place a key is named.
var (
	Global = GlobalMap{
		Help: key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help")),
		Quit: key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
	}

	List = ListMap{
		Up:           key.NewBinding(key.WithKeys("k", "up"), key.WithHelp("k/↑", "up")),
		Down:         key.NewBinding(key.WithKeys("j", "down"), key.WithHelp("j/↓", "down")),
		Top:          key.NewBinding(key.WithKeys("g", "home"), key.WithHelp("g", "top")),
		Bottom:       key.NewBinding(key.WithKeys("G", "end"), key.WithHelp("G", "bottom")),
		PageUp:       key.NewBinding(key.WithKeys("pgup", "ctrl+b"), key.WithHelp("pgup", "page up")),
		PageDown:     key.NewBinding(key.WithKeys("pgdown", "ctrl+f"), key.WithHelp("pgdn", "page down")),
		HalfPageUp:   key.NewBinding(key.WithKeys("ctrl+u"), key.WithHelp("ctrl+u", "half page up")),
		HalfPageDown: key.NewBinding(key.WithKeys("ctrl+d"), key.WithHelp("ctrl+d", "half page down")),
		NextSection:  key.NewBinding(key.WithKeys("]", "tab"), key.WithHelp("]/tab", "next section")),
		PrevSection:  key.NewBinding(key.WithKeys("[", "shift+tab"), key.WithHelp("[", "prev section")),
		Open:         key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "open")),
		Refresh:      key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "refresh")),
	}

	Detail = DetailMap{
		Up:           key.NewBinding(key.WithKeys("k", "up"), key.WithHelp("k/↑", "up")),
		Down:         key.NewBinding(key.WithKeys("j", "down"), key.WithHelp("j/↓", "down")),
		Top:          key.NewBinding(key.WithKeys("g", "home"), key.WithHelp("g", "top")),
		Bottom:       key.NewBinding(key.WithKeys("G", "end"), key.WithHelp("G", "bottom")),
		PageUp:       key.NewBinding(key.WithKeys("pgup", "ctrl+b"), key.WithHelp("pgup", "page up")),
		PageDown:     key.NewBinding(key.WithKeys("pgdown", "ctrl+f"), key.WithHelp("pgdn", "page down")),
		HalfPageUp:   key.NewBinding(key.WithKeys("ctrl+u"), key.WithHelp("ctrl+u", "half page up")),
		HalfPageDown: key.NewBinding(key.WithKeys("ctrl+d"), key.WithHelp("ctrl+d", "half page down")),
		NextTab:      key.NewBinding(key.WithKeys("]"), key.WithHelp("]", "next tab")),
		PrevTab:      key.NewBinding(key.WithKeys("["), key.WithHelp("[", "prev tab")),
		NextFile:     key.NewBinding(key.WithKeys("}"), key.WithHelp("}", "next file")),
		PrevFile:     key.NewBinding(key.WithKeys("{"), key.WithHelp("{", "prev file")),
		FocusNext:    key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "focus next")),
		FocusPrev:    key.NewBinding(key.WithKeys("shift+tab"), key.WithHelp("shift+tab", "focus prev")),
		PaneLeft:     key.NewBinding(key.WithKeys("h", "left"), key.WithHelp("h/←", "pane left")),
		PaneRight:    key.NewBinding(key.WithKeys("l", "right"), key.WithHelp("l/→", "pane right")),
		FocusPane:    key.NewBinding(key.WithKeys("1", "2", "3"), key.WithHelp("1/2/3", "focus pane")),
		ToggleRail:   key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "toggle details")),
		Expand:       key.NewBinding(key.WithKeys("o"), key.WithHelp("o", "expand or collapse")),
		Refresh:      key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "refresh")),
		Back:         key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
	}
)

// ShortHelp is the one line the status bar carries.
func (k ListMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Down, k.Open, k.NextSection, k.Refresh, Global.Help, Global.Quit}
}

// FullHelp is the overlay. Every binding in the map appears here; a test holds
// that, so adding a binding without a home in the help fails the build.
func (k ListMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Up, k.Down, k.Top, k.Bottom},
		{k.PageUp, k.PageDown, k.HalfPageUp, k.HalfPageDown},
		{k.NextSection, k.PrevSection, k.Open, k.Refresh},
		{Global.Help, Global.Quit},
	}
}

// ShortHelp is the one line the status bar carries. Refresh is in the overlay
// only: a seventh hint pushes the line past the pull request number on the
// right at 100 columns, and the number is what says which one is on screen.
//
// FocusNext is in the overlay for a different reason. The bar is the same line
// on every tab, and the ring is only on the one without a column; hinting it
// beside a pane where it does nothing is worse than not hinting it at all.
func (k DetailMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Down, k.NextTab, k.Expand, k.ToggleRail, k.Back, Global.Help}
}

// FullHelp is the overlay.
func (k DetailMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Up, k.Down, k.Top, k.Bottom},
		{k.PageUp, k.PageDown, k.HalfPageUp, k.HalfPageDown},
		{k.NextTab, k.PrevTab, k.NextFile, k.PrevFile},
		{k.FocusNext, k.FocusPrev, k.Expand, k.ToggleRail},
		{k.PaneLeft, k.PaneRight, k.FocusPane},
		{k.Refresh, k.Back, Global.Help, Global.Quit},
	}
}
