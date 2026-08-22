# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this repo is

Drew's Go terminal client for GitHub, at `praxis-labs-io/zen-octo` (`origin`). It handles a pull request end to end without opening a browser: read it, discuss it, watch its CI, fix its metadata, merge it. Issues get the same treatment where it makes sense.

`docs/` holds everything a user reads: the guide, the keymap, configuration and
install. `docs/CONTRIBUTING.md` holds the checks, the boundaries and the test
conventions, and `README.md` is the front page linking in. Read those rather
than restating them here.

What stays below is what the code has to keep true and a reader never sees.
Change one and the other says the wrong thing: `docs/CONTRIBUTING.md` carries a
map from changed surface to the document describing it.

gh-dash is the reference for GitHub search-query shapes and section config, not for code. It runs on Bubble Tea v1, so its view code does not lift verbatim.

**`main` is the product branch.** Feature work flows ticket → branch → PR on `origin` (see Project Management).

Two things skip the PR and commit straight to `main`:

- Genuinely trivial tweaks. A typo, a one-liner.
- **Doc-only changes with no code.** Markdown, comments, `CLAUDE.md`, rules files. A PR for prose is ceremony.

A tracked pre-push hook rejects pushes to `main`, so an agent commits these and Drew pushes them. Don't reach for `--no-verify`.

The installed binary is built from here to `~/.local/bin/zen-octo`; **rebuild after changes or Drew keeps running the old code**:

```sh
make install
```

The repo moved to the `praxis-labs-io` org on 2026-08-18, so the module path is `github.com/praxis-labs-io/zen-octo`. A `v*` tag cuts a release: `.github/workflows/release.yml` builds the five targets, writes the checksums, and cuts it from `docs/release-notes/<tag>.md`, which has to be on `main` before the tag is. There is no Homebrew tap. The emptied `zen-octo` org is held to keep the name.

Anything published under Drew's name (PR bodies, issues, README) must be shown to him word-for-word before pushing. His voice: terse, considerate, stoic, no strong adverbs, no em-dashes.

## Conventions

@.claude/rules/code-quality.md

That file holds only the Go and Bubble Tea specifics. The principles and voice rules are global and load automatically; don't copy them in here, that only creates drift.

## Commands

```sh
make all              # lint (gofmt + mod-tidy + golangci-lint) + test + build
make test             # go test -race -coverprofile ./...
make lint             # includes gofmt check and go.mod tidiness
make fmt-fix          # gofmt -w .
make install          # build to ~/.local/bin/zen-octo
go test ./internal/gh/ -run TestName   # single test
```

Run checks directly, never through a pipe that swallows exit codes. `make lint | tail` reports success on failure.

### Lint version pin

CI pins golangci-lint to match the local brew version (`.github/workflows/ci.yml`). Keep the pin current with the local version, or CI and local runs stop agreeing.

### Git hooks

`.githooks/pre-push` is tracked and rejects pushes to `main`. `git config core.hooksPath .githooks` wires it up; the SessionStart hook does this on every session so a fresh clone is covered. Untracked `.git/hooks/` files don't survive a clone, which is why the hook lives here instead.

## Charm module paths

The Charm v2 line lives under `charm.land/*`, not `github.com/charmbracelet/*`. `github.com/charmbracelet/bubbletea/v2` does not resolve. Version numbers are the same across both paths.

```
charm.land/bubbletea/v2
charm.land/lipgloss/v2
charm.land/bubbles/v2
charm.land/glamour/v2
```

`github.com/charmbracelet/fang` (v1 line) keeps its github path and pulls an older beta of `charm.land/lipgloss/v2`. Requiring v2.0.5 directly upgrades past it; there is no two-lipgloss problem as long as nothing imports the github v2 path.

## Project Management

Work is tracked in Linear: Praxis Labs workspace, **Zen Octo** team (key `ZNO`, tickets `ZNO-###`), reached through the `linear-zen-octo` MCP server declared in `.mcp.json`. Address projects and statuses **by name, never a UUID**; ids don't survive workspace moves.

The bucket names are shared with other teams, so `save_issue` resolving a bare project name can land on another team's copy and fail the call. Pass the Zen Octo project id in that one argument when it does.

### Projects

Five long-running buckets. They never complete; every ticket belongs to exactly one. Each bucket's Linear description holds a `File here when:` test and a routing list, and those descriptions are the tiebreaker when a ticket could fit two:

- **Polish & Bugs**: bugs and rough edges in surfaces that already ship. The dogfood inbox.
- **Feature Backlog**: net-new capabilities. Ideas live here until promoted.
- **Performance and Code-Quality**: improves the code, no user-visible change.
- **Website**: the public site, its copy, its SEO.
- **Release & Distribution**: how the binary gets from `main` to a user and stays current.

A body of work big enough to need milestones gets its own finite epic project, named for what it delivers, completed and closed when it ships. **v1** is the current epic. An epic is a Linear Project, never a tracking issue. When an epic closes, follow-ups move to the matching bucket.

### Tickets

- Every ticket gets the team, exactly one project, a priority, and a status. No orphans.
- Create tickets as we go; never dump a full backlog up front.
- PR-sized scoping: 1 ticket = 1 branch = 1 PR as the rule of thumb.
- Keep descriptions lean: clear title, short goal and scope. No boilerplate acceptance criteria.
- Use Linear's generated branch name (`gitBranchName` from the MCP), never an invented one.
- Reference the ticket id in commits and the PR title/body so Linear auto-links.
- Status ladder: agent drives Backlog → Todo → In Progress. The GitHub integration owns In Review and Done; never write those by hand.

### Shipping

Feature-complete work ships via the global `ship-feature` skill: `make all` green, push, draft PR, Copilot + `/code-review`, triage with no tech debt, push then mark ready as separate actions. Manual invocation only.

**This repo carries no copy of it.** It did until 2026-08-11, so that a session cloning this repo alone would still have the skill. Manual propagation is what actually happened instead: the copy drifted, shadowed the live one, and spent over a week prescribing a Copilot review request that never worked. A stale copy is worse than an absent skill, because nothing about it reads as stale. A session that cannot see the global skill should say so rather than follow a copy nobody maintains.

### Specs and plans

Scratch, never committed. `docs/` describes only what is true today. Durable context lives in Linear project descriptions and tickets.

## Architecture

`cmd/zen-octo` is the entrypoint (fang over cobra). Everything else lives in `internal/`.

Package boundaries that matter:

- **`internal/gh` is the only package that touches the network.** It returns domain types, never raw API structs, so everything above it is testable against a fake. Two transports, two seams: `graphQLDoer` for everything, `restDoer` for the diff alone, because GraphQL has no field carrying a patch.
- **`internal/store` owns fetched state and what is owed a refetch.** Views read from it, they never fetch. It owns no clock: ordering is a counter and staleness is a mark, and every duration in this repo sits in the TUI layer, where the timers are.
- **`internal/tui/*` packages never import each other sideways.** Shared widgets live in `internal/tui/comp`.

Writes are optimistic: apply locally, toast, reconcile on response, revert on error. Every write path needs the revert branch, not just the happy one.

The shell divides the frame top-down: `app` owns the terminal size, subtracts the status bar, and calls `SetSize` on the screen that has focus, which sizes its own panes. **No component reads terminal dimensions or counts chrome lines.** A test asserts the rendered frame never exceeds the size it was given.

Under 56 by 23 the shell draws its size instead of a screen. **The height is a
cliff and the width is a judgment**, which is why only one of them is derived.
An overlay is clipped rather than scrolled, so the merge form at its tallest,
three methods with a branch to delete and a bypass to warn about, is twenty-one
rows that have to be there: with the status bar's one and the config notice's
one, twenty-three, and at twenty-two the form loses its own bottom border. A
test opens that form under a notice at exactly the floor, because a number
carrying a reason nothing checks is a number that drifts off it. Nothing has a
cliff in width. The form gives up its hint line and keeps its button, the
columns narrow, the rows shed cells, and 56 is where the rail still lands whole
and the form still names the keys it has taken. What settles it either way is
that the floor has to leave a drawer beside an editor alone: a floor set where
every control is comfortable would refuse the window this is most used in. It
names what it needs and drops what it is where the sentence will not fit, since
the size a reader can already see is the half worth losing. Nothing is torn down
under it: `resize` returns early, so a terminal dragged small and back is the
one it was with whatever was open still open, and the relayout a conversation
costs is not paid per step of a drag that renders no frame. **What is torn down
is the keyboard**, everything but the ways out: `ctrl+c`, the `esc` that closes
what is open, and `q` where nothing has the keys, which is the rule `q` already
answers to a line further down. The screen is still there under the message and still holds the keys, so
every one of them acts on a layout nobody can see: a picker takes a space and an
enter and writes the set it was left holding, which on the labels is every label
off the pull request, and the merge form's button is one blind enter from a
merge. Dropping them costs a reader nothing they could have aimed.

Every scrollable region owns its own `bubbles/v2/viewport`. Scroll state never sits on the root model.

Key bindings live in `internal/tui/keys`, declared once with their help text. The help view renders from the same declarations, and tests hold that nothing declared goes unlisted and nothing listed is invented. `docs/keys.md` is written from those same declarations, so a rebind moves three things or none.

The braces are paragraph motion, the way they are in vim, and mean the same thing wherever the detail screen holds blocks: go to the next one. What a block is belongs to the tab, so the key walks the cards on the conversation, the hunks and the comments written against them on Files, and whole files on Commits, which has no ring. They were two separate keys once, for one intention, and whichever the reader pressed was inert on the tab the other worked on. The strip keeps `]` and `[`, where they do what they do on the list screen, and `tab` and `shift+tab` change the file on the tab that shows one at a time. `keys.Form` is where tab means something else, and it is its own map rather than part of `DetailMap`: a compose box or the merge form takes every key until it closes, so the two are never live together, and the braces a reader walks blocks with are text inside a textarea. It means one thing further in again under a mention list, where tab writes the handle rather than stepping to the button, which is the same intention one level down: go to the thing that finishes what is being written.

`y` copies a pull request's link and `O` opens it in a browser, on both screens
and on all four tabs. Neither reads the ring, because nothing under the pull
request carries a URL: the detail query fetches one field and no comment,
commit, check or file has a permalink to reach. So a lit card changes nothing
about what either key means, and a later key for a block's own link is a later
key rather than a second meaning for these two. On the list they sit below the
guard `enter` sits below, and that guard reads whether the rows are on the
screen rather than whether a fetch is out. A section that has never answered is
showing a block instead of them, and a failed one is showing its error, and in
neither case are the rows behind it what the key was pressed on. A reload is not
that: it keeps its rows, so the keys keep working over them and the status bar
carries the spinner the pane no longer does. `internal/link` is
where both leave the process. The clipboard is written natively and falls back
to OSC52, which is the transport that survives ssh; that failure is not
reported, because the only thing it says is that this machine has no `pbcopy`,
and an error beside a copy that worked reads as one that did not. The browser
goes through `go-gh`'s own resolver, so `GH_BROWSER` and gh's config answer here
the way they answer for `gh`, and its streams are discarded: a launcher printing
into the alt screen tears the frame open. A copy toasts because a clipboard is
invisible; a browser does not, because it takes the focus and says so itself.

The ring steps from its focus while that focus's own byline is on the screen,
and past that the braces are motion rather than a step and read the window
instead. A reader who scrolled has moved, and the block they left is not the one
to count from: stepping forward off it lands behind where they are reading and
stepping back lands a screen or more above it, which is each key moving the page
against itself. So each key re-enters from its own end of the window and walks
from there: `}` takes the first byline at or below the top row, `{` the last
card whole on the screen. Back stops at what is on the screen rather than
reaching past it for the block the top row sits inside, because a long review
with its last two lines on that row and three cards whole underneath is a screen
of travel on a key asked for one step, and the byline it arrives at is one
nobody pointed to; a card the reader can see entire is the one they mean, and
lighting it moves the page not at all. Only where nothing fits is the block
under the top row the answer, and there it is the only one there is: the window
is inside one long comment, and its own head is what `{` means in vim and the
one motion this screen had no way to make. `focusItem.headOn` is that test
and it is deliberately not `covers`: whether a key may act on a block and
whether that block is where the reader is standing are different questions, and
a ninety-line review with one line left on the top row answers them differently.
Forward takes the top row itself where the two tabs with no focus to
disambiguate take the row under it, because a screen not yet scrolled has the
description's byline on that row and skipping it would put the first card on the
page out of reach of the first press; nothing stalls on it, since the press
after lands on a byline and steps by index from there.

An `@` opening a word in either box draws that list under the caret. It declares no bindings of its own, because every key it wants is one `DetailMap` or `FormMap` already spells and a fifth field would collide in the reflection tests: `enter` and `tab` insert, the arrows move, `esc` closes the list alone. Each of those is a key that destroys something if it leaks through, which is why `mentionKey` runs ahead of both `composeKey` and `inlineKey` rather than beside them: a leaked `esc` closes the box and an edit's draft goes with it, a leaked `tab` blurs the textarea so the box then takes no keys at all, and a leaked `enter` breaks the line in half. The arrows are matched behind the printable guard `comp.Picker.Insert` uses, because `Up` and `Down` carry `k` and `j` and those are letters in a box.

The token is read from the buffer rather than from the keystroke, on `Line()` and `Column()`, which are a logical row and a rune index into it, so a start of line and a start of buffer are the same test. It scans back from the caret rather than forward from the `@`: a line with three handles on it has three tokens and two of them are finished. The `@` has to open a word, which is what keeps an address from dropping a list of logins over the line it is written on. The scan runs on the paste path as well as the key path, because a paste is its own message and never reaches `handleKey`, and a list left open after one shows a query the buffer no longer holds.

Inserting rebuilds the buffer back to front. `SetValue` is a `Reset` and an `InsertString`, which leaves the caret at the end of what it inserted rather than at the end of the buffer, and there is no exported setter for the cursor's row: the tail goes in first, the caret goes to the top, and the head is inserted in front of it so it comes to rest where the head ends. Nothing may read the caret until a render has sized the textarea again, because `Reset` scrolls the box to its top and neither call repositions.

The list is offered before anything is fetched, because the people on the pull request are known from the detail and are who a reply usually names. `mentionableUsers` rides on the repo-meta query, so the first `@` costs a request only where no picker has opened yet, and the note under the rows is what stops a short list of participants reading as everybody there is: on its way, or refused, or matched nobody, each in its own words. The one that had to be built for it is the refusal. `SetRepo` declines to act while `Capturing()` is true, and a compose box always is, so `refillMentions` runs on its first line ahead of every guard; and `repoMetaFailed` told the screen nothing at all, which made a dead fetch and an unasked one render identically, which is the exact failure a silent empty list is.

**The header is two lines and four corners.** The title and the number lead,
with what the pull request changes at the far edge; the branches lead the second,
with the lifecycle and the check rollup at the far edge of that. It was five
rows: a third line carried the state, who opened it and when, and a blank held
that apart from the two naming the pull request. Who opened it went to the status
bar, which is empty on that side most of the time, and the two blanks went with
the line. The one closing the block stays, because the pane border under it is
already a horizontal and a second one a row above reads as a box that has come
open, and the tab strip is what sits under it now.

The two halves of the second line are measured against each other rather than
each against the frame. `spread` gives the right half the width and clips the
left, and `branchLine` has already cut its names to the room it thought it had;
handed the frame, it cuts a name to a width the row does not have and `spread`
cuts it again, which puts an ellipsis after an ellipsis. So the status is
measured first and the branches are told what is left.

**The tab strip closes the header, on a row of its own above the panes.** It sat
in the main pane's top border, which was right while the strip travelled with
that pane and stopped being right once the rail moved left: navigation for the
whole screen was being drawn inside one of the two boxes it switches, and it
cost that pane its title outright, since `Tabs` wins over `Title` in
`topBorder`. It is not `comp.Pane`'s strip and cannot be. That one is set into a
border and separates its segments with border-coloured punctuation so the run
reads unbroken; this one sits on a bare row, where the same punctuation reads as
dashes. The list screen still draws its sections through `Pane.Tabs`, so the two
are two renderers on purpose, and the header's stays in `prview` with its one
caller.

**The current tab is underlined rather than marked.** A glyph would need a cell
every tab held open, or every tab to the right of the current one steps sideways
on the key that steps through them; an underline takes none, so the strip starts
level with the two rows above it. It is held off the pane borders by a blank of
its own, because a rule landing on a border reads as one the border grew rather
than as a mark on the tab it sits under. Lipgloss writes that underline one SGR
run per rune, so nothing may look for a single escape standing in front of a
whole label. The counts go before a name does: at the narrowest frame the shell will draw
the counted strip is a few cells over, and clipping there takes the tail off the
last tab, which may be the one the reader is standing on. Conversation and Files
count off the list row, so they are there before the detail query answers;
Commits and Checks wait for it and render bare meanwhile, because a zero claims
a tab is empty rather than unanswered. The parentheses are `list.badge`'s
spelling, which is the one this app already had.

**Both panes are named, and the right one by what it holds** rather than by the
tab it is under: Feed, Diff, Log, Diff, against Details, Commits, Checks, Files.
The left column gave its count up to the strip, which can say all four where a
column could only ever say its own, and the same number in two places is one of
them saying nothing.

**Every secondary pane is the left column, the rail included.** It sat on the
right until it was the only one that did, and the sidebar then changed sides on
a keypress that only changed which tab was on. Pane 1 is that column on every
tab and 2 is what it sits beside. `visiblePanes` is the one place that order is written down, because the
enum cannot carry it: the rail and the file column are exclusive and sit in the
same place, so stepping focus by adding one to the enum is right on whichever
kind of tab the order was written for and wrong on the other.

**The leading pane takes the keys on the way in, once.** It is the one the
reader navigates with, and it is numbered first because it is where the eye
lands. It happens on the first `layout` rather than in `New`, because which pane
leads is a question about a frame and `New` has not been given one, and it
latches on having found a second pane rather than on having run, since `layout`
runs again on every resize. The strip is left alone: a reader changing tab has
already chosen a pane, and handing the keys back on every press of it would make
them ask for the page again each time they came round. Commits and Checks still
take their column on arrival, which they did before any of this.

**A tab gives back the pane it was left on.** Focus is one field where the
scroll is four, and Commits and Checks take their column, so a round trip
through one of them used to come back on a pane nobody chose: the column leaves
the screen and `layout` puts the keys wherever is left. `panes` parks it per tab
beside `offsets`, and `paneNone` leads the enum so an unopened tab is the
slice's own zero. Those two take their column on arrival and only on arrival,
which is what that zero answers: a reader who walked off it once meant it. The
fallback lands on the leading pane rather than on the main one, because the rail
and the file column are exclusive and sit in the same place, and what a
departing column leaves behind is whatever stands where it stood.

**A cursor is landed wherever there is something to land it on**, and `esc`
leaves in one press. The conversation and the diff opened with nothing lit, so
the first press of a motion key was spent arriving rather than moving, and `esc`
let go of a card before it would leave. With a cursor everywhere there is always
something to let go of, so the reader who wanted the list would have paid two
presses every time: a cursor that always exists is not a thing to be dismissed.
The Checks search keeps its own leg, because `n` and `N` stay armed off it.
`landCursor` and `focusPane` report whether they placed one, since arriving
somewhere is a move: `walkDiff` stepped again on top of the landing and a brace
pressed from the file column skipped the first hunk of the file.

**`d` answers at every width the shell will draw, because the rail is the only
route to five writes**: state, labels, reviewers, assignees and the base branch.
It used to be refused below `railColumnFrom`, where the key set the preference
and rendered nothing, and a client in a drawer beside an editor was read-only
with the bar still offering the key. What that width decides now is where the
rail lands rather than whether it comes: above it a column, below it the same
pane painted over the left of the conversation. The frame under it is
untouched, so nothing is relaid out on the toggle and the words behind the rail
are covered rather than rewrapped; it goes on before the pickers, so the modal a
rail row opens still draws over it. It sits against the left edge and not
centred, because that is where the column would have been. **An overlaid rail steps
aside for a box**, on `Composing()` and not on `Capturing()`: a picker and the
merge form are drawn over the page and cover it themselves, where a compose or
reply box is drawn down the page and gets covered instead, losing the half of
itself that carries the button. There is no way back out of that one, since `d`
is a letter while a box has the keys and `esc` closes the box with the draft
inside it. A column stays put under the same box, because a column already made
the room and stepping aside there would rewrap the conversation around a box
that had the width it needed. The key hands the keys
over as it opens and takes them back as it closes, at both widths rather than
only the one that needed it: a reader reaching for a control is reaching to use
it, and a panel over a page that does not take the keys is `j` scrolling
something the reader cannot see. 120 is a separate question and unchanged: that
is where the rail comes up unasked, and between the two the reader asks.

The rail is the exception, and the braces are dead on it. Its rows are a list of controls rather than blocks of prose, so it answers to the movement keys the way the file column does: `railDriving` sends `j` and `k` to the cursor, and taking the pane lands the cursor on its first control rather than waiting to be pressed once before it will say where the keys go. Two things fall out of it being a list with facts in it. The cursor stops at each end rather than coming back round, which the conversation's ring does too: that one lapped once, on the argument that the ring is the whole of the content and there is nothing past the last card, but a real pull request is a page deep and the wrap is then the longest throw either key can make, arriving at the end the reader was walking away from. Both report the key untaken there, which is what lets the pane scroll to whatever sits under the last stop. And `g`, `G` and the page keys never move the cursor, because those go to the ends of a pane and the rail's ends are past its last stop.

**The rail's cursor line carries the bar as well as the fill**, which is a second mark the conversation's cards were refused. It earns it where they do not: the ring walks the rail's controls and steps over the headings between them, so the cursor lands on rows that are not neighbours, and a reader tracking it is looking for where it went rather than watching it move. The columns beside the other three tabs are flat, every row a stop, so the fill alone says everything there and none of them takes a bar. It is `paint.Lead` and `paint.BarGlyph`, the diff's own, in the cell `railGutter` already holds open: one glyph for one fact, and no row gains width by being the one under the cursor.

The bar's hints are the detail screen's own, built per tab from what that tab can do. The keymap is the same on all four and the tabs are not: Checks has no blocks to walk, Commits and Checks have nothing to expand, and the three with a column have no rail to toggle. A hint for a key that is inert under it is worse than no hint, since the reader presses it, nothing happens, and the line stops being worth reading. The same rule takes the hints off entirely while a picker or a form is up: it has the keys they name and carries a hint line of its own.

The status bar carries the hints on the left in every state. A toast or the refresh spinner lands on the right and wins a narrow line, which is the opposite of the readout that sits there otherwise: a toast may be the only account of a write that failed, and a key works whether or not it is on the line. `RenderMessage` is that priority, beside `Render`. The readout under those is the remaining budget while it is low enough to be worth reading, and under that whatever the screen in front of the reader has to say: on the detail, who raised the pull request and how long ago, as `@handle · 2d`. The budget outranks it, being a number that runs out where the other is a fact that does not change. Neither screen names *itself* there: the list's section is the current tab in the top border and the detail's pull request is in its own header, so both were spending the line on something already on the screen, and spending it on the side a toast lands on. Who opened it stopped being one of those when the header went to two lines. The compact form is the bar's alone, because the left half is a line of key hints running most of the width and a clause spelled out is one clipped mid-handle.

The State row's menu is built from where the pull request sits and from what
GitHub says the viewer may do to it, never from the word on the row: state and
draft are independent fields, a closed draft reads as "Draft", and the detail
query asks for `viewerCanUpdate`, `viewerCanClose` and `viewerCanReopen` so a
menu item never opens a write that comes back refused. A row with nothing to
offer states a fact and the ring walks past it, the way an empty Checks section
does, but only once the detail has landed: before that nothing is known, which
is not nothing being allowed, and dropping the key early moves every rail stop
by one the moment the answer arrives. A state write refetches the whole detail
once it settles, because the Merge row, the check rollup and the timeline all
hang off a field the store cannot compute. It borrows no refresh leg doing it,
or the sync's summary toast lands behind the one that already said what
happened.

Every control on the details rail answers to one key. Enter opens what the focused row holds, as a centred modal built from `comp.Over` and `comp.Modal`, and `comp.Picker` is the list inside it. The picker declares no bindings of its own: a widget package cannot reach sideways into `keys`, so it exposes verbs and `prview` decides which key means which. While one is up it owns the keyboard, which is what `Capturing` tells the root, and the order in `pickerKey` is load-bearing: the keys that can never be text go first, then the filter claims every printable one, then movement takes what is left. `space` is declared in `keys.Form` rather than in a map of its own, because the merge form's delete row reads it too and a row that checks and unchecks is one control whichever widget is holding it. A section is its picker: the rows already in it open the same modal as the add row under them, because the modal is where something comes off as well as goes on.

A tick in the Reviewers picker means a review is requested, never that somebody
is on the pull request. GitHub drops a reviewer from the requests the moment
they submit, and there is no call that un-submits one, so a reviewer who has
answered opens unchecked and ticking them asks again, which is what the
re-request button does in the browser. `Reviewer.Requested` is what says so, and
it is not the inverse of `State`: a re-requested reviewer holds a verdict and an
open request at once, the two connections overlap rather than partition on them,
and reading an empty state as "waiting" hides the request behind a key that can
only ask for it again. That write is the one place two calls are unavoidable:
the endpoint has no spelling meaning "these and nobody else", so the screen
sends a delta and the app cancels before it asks. Because it is a delta, the
picker has to list every outstanding request as well as the repository's own
users: `comp.Picker.Chosen` reports only ids it was given items for, so a
checked login with no item silently becomes a cancellation, and a review
requested of anyone past the hundredth assignable user would be dropped by a
reader who never saw them. A team requested for review is kept and never listed,
because `assignableUsers` holds none and a delta cannot remove what it could not
offer; `Reviewer.Team` is what the exclusion reads, rather than the slash in a
handle this client built itself. It is also the one failure path that refetches:
a write made of two calls can fail with the first applied, and reverting alone
would leave the rail claiming a request GitHub has already dropped.
Copilot answers to a different name in every direction and the two REST verbs
disagree with each other: `POST` takes `copilot-pull-request-reviewer[bot]` and
answers 200 while writing nothing to a bare `Copilot`; `DELETE` takes the bare
`Copilot` and 422s on the `[bot]` form, which it resolves to a Bot and then
rejects for not being a User; GraphQL reports `copilot-pull-request-reviewer`,
and that one is canonical everywhere above `internal/gh`. **The `POST` response
never lists the bot at all**, landed or not, so `requested_reviewers` cannot
tell a success from the silent no-op and reading it rejects every Copilot
request there is. That shipped once. The confirmation is a GraphQL
`reviewRequests` read, which is the only place a bot request is visible, and it
is what makes those two 200s distinguishable. Copilot cannot be discovered
either, so it is offered always. A reviewer write refetches the whole detail, since the endpoint reports
the outstanding requests alone and says nothing about who has already reviewed,
and requesting one rewrites the review decision the header renders. An assignee
write refetches nothing: it changes nothing the store cannot already see. The
Assignees rows are a control only while `viewerCanAssign` **and**
`viewerCanUpdate` both say so, because the permission to assign and the
permission to run the mutation that does it are different answers: a triage
collaborator is given the first and refused the second. There is no flag at all
for review requests, so those rows are ungated and the revert branch answers a
refusal.

The Base row is a control while `viewerCanUpdate` says so **and** the pull
request is not merged, which is two questions rather than one: the flag stays
true on a merged pull request, because its title and body are still editable,
and GitHub refuses the base change anyway. Its branches are the one picker whose
choices are a search rather than a cache. `refs` takes an `orderBy` and ignores
it on `refs/heads`, so alphabetical is the only order GitHub will apply, and it
pages before this client sees anything: sorting the page by each ref's
`committedDate` orders it and cannot choose it. **A branch outside the page is
reached by narrowing the search and never by scrolling**, which is what the
picker is built around. The filter is the search, going over the wire as
`refs(query:)`, a case-insensitive substring exactly the way `comp.Picker`
matches, debounced through the pair `armCommit` and `settleCommit` already use
so a word typed at speed costs one request. Thirty at a time, with what the
search left out named beside the title. The repository's default branch is
offered first and the current base is always offered, whatever the head is
called: a fork's head carries a name and not a repository, so a contributor's
`main` merging into this one matches the head filter, and a picker that opens
with nothing checked puts the cursor on row one and makes enter a retarget onto
whatever sorted first. Neither is pinned once something has been typed. A
retarget refetches the whole detail and marks the diff stale, and the diff waits
for the detail rather than going with it, because the changed-file count its
overflow line is measured against arrives with it. The row reads "Retargeting to
develop" while the write is out, and "Merging into develop" once it has landed
with nothing counted yet; those are two states rather than one because the
confirming refetch can fail, and a row that latched on the first would report a
finished write as in flight for the rest of the session. `gh.BehindUnknown` is
the count meanwhile, since zero is already spoken for: it means up to date.

The Merge row is the one control that opens a form rather than a picker: a
method, a commit message, a branch to delete, and a button. It is a control on a
clean pull request and on one whose checks are failing, because GitHub's own
button merges the second: a red check no rule requires is not a rule. Blocked
and behind go together and only under `viewerCanMergeAsAdmin`, because they are
the same refusal, a protection rule standing in the way and a flag that lifts
it; the form says `Bypasses branch protection on main` when it is doing that,
which is the one merge here overriding something somebody set on purpose.
Nothing lifts a conflict and nothing merges a draft. Nothing merges an unknown
either, but that one is a wait rather than an answer: GitHub computes
mergeability lazily and the query that reads it is what starts the computation,
so a pull request nothing has looked at recently opens on "Checking" and the row
is inert. One probe is armed for that, on the first landing in a session alone,
which is what keeps it to a single extra request rather than a loop. It re-asks
as a pulse rather than as a page.

**The commit message is GitHub's own, per method**, from
`viewerMergeHeadlineText` and `viewerMergeBodyText`: the repository decides
whether a squash title is the pull request's or its single commit's, and nothing
on this side reconstructs that. Switching method rewrites whichever field the
reader has not touched, because a merge commit and a squash want different
sentences. A rebase answers empty to both, which is GitHub saying it writes no
commit of its own, so the form drops the two fields from the render and from the
ring rather than sending text that gets discarded. `expectedHeadOid` is the
commit that was on screen, snapshotted when the form opens: a refetch landing
behind the modal must not change which commit gets merged, and a branch that
moved comes back refused in GitHub's own words rather than merged unseen.

Deleting the head branch is a second call and it cannot undo the first, so it
runs off the back of the merge's answer and its failure toasts alone. It is not
made at all where the repository sets `deleteBranchOnMerge`: GitHub deletes the
branch itself a moment later, and a call racing that fails on a ref already gone,
which is an error about a thing that worked. There the row is absent rather than
ticked. `deleteRef` takes the ref's node id and no name, which is why the detail
asks for `headRef { id }`; it is null once the branch is gone. Success says
nothing, because that toast lands a beat behind the merge's own and would take
the status bar off the more important of the two.

**`viewerCanDeleteHeadRef` does not answer whether the viewer may delete the
head branch.** It is false on every open pull request, for a repository
administrator as readily as for a stranger, and turns true the moment the pull
request closes. The only time a merge form can be open is while it is open, so a
row gated on that flag never appears once, in any session, for anybody. That
shipped as far as the runbook. There is no field that does answer it: `Ref` has
no viewer permission at all. So the row is ungated, on the same terms a review
request is, and the delete's own failure is what reports a refusal. The one case
refused up front is `isCrossRepository`, a head living in a fork, because
deleting a contributor's branch is worth declining without being asked to.

`MergeEdit` settles against `fieldState` rather than a field of its own. A merge
is a lifecycle move, so a close and a merge in flight together settle
last-held-wins the way two lifecycle writes do, and the fold marks the detail
`StateWriting` for free. It moves the state and leaves `mergeStateStatus` alone,
because the row reads the lifecycle first: a merged pull request says "Merged
into main" whatever GitHub last answered about what stands in the way.

A refused merge is the one revert here that refetches, because the refusal is
evidence about the screen rather than about the pull request. The commonest one
is the head having moved since the detail was fetched, and there the fetched row
is the very thing that lost the merge: putting "Ready to merge" straight back
says the branch is as it was and invites the same press again. `EditRevertedStale`
is what both it and the reviewer write go through, dropping the edit and marking
the fetch in flight stale so an answer asked for before the failure cannot be
believed. Every other write is all or nothing, and reverting one of those says
the pull request never moved, which needs no request to confirm.

The Files tab walks the same ring the conversation does, and it is the same
ring. `pageRing` is whatever the main pane is drawing, because one tab draws
into it at a time, where the rail is beside the page and needs a cursor of its
own. `mainRing()` is the value twin `focusRing()` cannot be, since taking
`&m.pageRing` off a value receiver addresses a copy, and `ringTab()` is what
both of them ask. Its stops are hunks and comment cards, and there is no file
stop: one file is in the pane and its heading is pinned above the scroll, so a
stop on it would be a stop on the one thing that never moves. A hunk is named by
its own `@@` header under its path and never by its place in the slice, so a
push that rewrites the file leaves the key naming nothing, which is what
re-entering from the window already answers. It lights as a fill across the
heading rather than as a badge, and only while the cursor is on the heading
itself: the badge slot is for a state a hunk carries whether or not the cursor
is on it, and filling every code row under it would beat the added and removed
tints and stop the diff reading as a diff. A brace pressed
from the file column takes the pane with it, because the blocks it names are in
the pane, and the column keeps `tab` and `shift+tab` for the coarser move. A
brace at the end of a file's stops crosses into the next one, forward to its
head and back to its foot, because the pane holds one file and the stop after
the last is in a file nothing has drawn yet. Past the last file and before the
first it stays put, which is what both ends of every ring here do.

**`j` and `k` move a row cursor rather than the window.** It is the ring's focus
and how far into the code under it the reader has walked, never a cursor of its
own: a block the motion key lights and a row the motion key moves from would be
two claims about where the next key acts. Off the end of a block it steps to the
next by index rather than by re-entering from the window, because a reader
standing on a row has not scrolled away from anything, and `ring.advance` is
that step beside the `step` the braces read the window with. The last block of a
file is a boundary, the way both ends of every ring here are: `tab` crosses to
the next file and the cursor does not, or the column is left behind. It reports
the key untaken there rather than swallowing it, which is the rule the rail
already answers to: those keys go to the ends of a pane, and a boundary that ate
them left the last block's own code below the fold with no way down to it.

**Which block a stretch of code belongs to is the render's answer, not a second
derivation.** One stop draws several: a review thread is its card, every reply
hanging off it, and the box when one is open, so the code under it hangs beneath
the last of those rather than beneath the card that opened the thread. Credited
to the card, `j` steps over every reply on the page and `k` off a reply walks
back *down* the screen onto the row below it. `drawnFile.rows` is what the pass
that drew them writes down and the next key reads, because deriving it again in
the movement code means spelling out where a thread's stops end in two places
and one of them going stale. The pass reads its own measurements as it goes and
never that map, which is the one it is in the middle of rebuilding. The walk is
held against the block it was counted in, so a fold that takes those rows takes
it with them, and a brace naming a block lands on that block's own head.

The row carries a bar in its leading cell, in accent. The tint alone is a change
of shade a reader loses on a page of them, and the cell is one every row already
holds open, so nothing shifts as the cursor passes. A card takes neither the
fill nor the bar: its border already lights, and a second mark on one block says
nothing the first did not. It gives that border up once the cursor walks into
the code below it, the way a hunk heading gives up its fill, since a lit card and
a barred row are two claims about where the reader is standing. The hints ride
inside the border either way, so nothing changes height as it passes.

**`|` puts the two sides in two columns.** A run of removals pairs against the
run of additions after it, one row each, and the shorter side draws a blank
rather than shifting up, which would put the two columns out of step for the
rest of the file. Context takes both. One gutter serves each half, so the rule
between them sits centre, and `paint.Half` pads to exactly its width where
`paint.Line` leaves that to the pane: a half a column short walks the other one
in and out down the page.

It is what the reader asked for and not always what they get. Under a minimum of
source per column the two halves clip away more than they show, so the key
refuses and the toast says how many columns short the pane is. `m.split` is the
answer and `splitting()` is what is drawn, which is what lets a terminal
shrinking under a split pane fall back to unified and come back on widening with
no second press. The mode is on the block's own `blockState` rather than its
key: it replaces the block it retires instead of keeping one painted per mode
for the rest of the run.

**It is the body's mode and never the model's past `filesBody`.** `renderDiff`
draws the Commits tab as well, and that tab never splits: a heading, a lit row
and the column a walk is counted in all read `diffBody.split`, which is written
once from `splitting()` on the way in. Read off the model instead, `|` on Files
moved every heading on the Commits tab three cells left of the source under it,
because `HalfColumn` is one number column in where `CodeColumn` is two. That
shipped as far as a runbook.

The cursor is in one column and only that column lights. Lit across both, a
reader on a rewritten line has nothing saying which side the next key takes, and
the rule stays dark or the lit block runs a cell past its column. A heading
spans the pane and takes its bar at the pane edge, belonging to neither. `h` and
`l` step into it, ahead of the pane they already move between: `h` from the head
column goes to the base and again to the file column, and a pane drawing one
column has nowhere to step and gives the focus up on the first press. The digits
stay an absolute jump, because a badge drawn in a border names the frame it is
drawn in and there are three frames whatever the mode.

**A column step is taken on three conditions and not one.** The tab has to be
Files, because `splitting()` reads a remembered file and a pane width and both
outlive a tab change: ungated, `h` on the Commits pane was swallowed as a column
step, and the reader met the moved column on their next walk in the diff rather
than on the key they pressed. The pane has to be the one the columns are in. And
the cursor has to be down in the code, because on a block the two columns draw
the same frame: the key was taken, nothing changed, and the file column sat one
press further away than it looked. That is the rule `d` already answers to, one
key over. Where the step does happen the walk clamps to what the render just
measured, and a block the new column has no rows in clamps to 0, which is the
cursor back on the block's own head. Left alone it named a row nothing draws:
`rowAt` answers -1 so no bar is painted, `walkedInto` reads the raw cursor so
the heading gives up its fill too, and `j` cannot recover it, since
`cursorShown()` is false and the ring steps to a different block.

`|` pressed before the diff has landed is held rather than refused. The diff is
a second request, so `]` to Files and `|` straight after is the common case, and
a silent refusal there is a key that needs pressing twice for a reason the
reader cannot see. The reader asked for a mode, not for a fact about request
ordering. Too narrow when the files do land is the resize fallback's case and
answers the way that one does, silently and reversibly.

**Whether a column step took is the render's answer too.** `walkColumn` walks a
block the focused column has no rows in in the other one, so on a file with
every line on one side the column moves and the bar does not: the key was taken,
the frame was identical, and the file column was a second press away, which is
the thing three conditions were put on this key to stop. `diffBody.columns` is
the walked column per block, twin to `rows` and written by the same pass, and
`stepColumn` reads it twice: once to refuse a step to the column already walked,
and once after drawing to put `m.column` back where nothing moved. No field on
the model can answer this, because the column asked for and the column drawn are
exactly what differ.

**A block the focused column has no rows in at all is walked in the other one.**
That is a different question from the row rule below, and it answers what the
row rule cannot: a newly added file has every line on one side, and the column
outlives the file it was chosen in, so a reader who had stepped to the base
reached one, pressed `j` at code plainly on the screen, and the client answered
with nothing. The column is a side of the diff to read rather than a claim about
where a file has content. `walkColumn` is the one place that decides it, so the
count the walk reads and the column the bar is painted in cannot disagree.

**A block the focused column has no rows in at all is walked in the other one.**
That is a different question from the row rule below, and it answers what the
row rule cannot: a newly added file has every line on one side, and the column
outlives the file it was chosen in, so a reader who had stepped to the base
reached one, pressed `j` at code plainly on the screen, and the client answered
with nothing. The column is a side of the diff to read rather than a claim about
where a file has content. `walkColumn` is the one place that decides it, so the
count the walk reads and the column the bar is painted in cannot disagree.

A row the focused column has no line on is not a row the cursor can sit on, so
walking the head column of a deletion-only block steps over it: `run.rowAt`
counts only the rows that column has, which is the same rule that keeps the walk
inside its own block. So the count a block offers belongs to a column as well as
to a block, and it is the render that measures it: `stepColumn` draws again
before it returns, which is what keeps the number the next key reads a number
about the column that key is in. The mode lasts the run and nothing stores it; a
default belongs with the reader's other preferences.

`canCompose` split in two for it. It still means the conversation, because the
compose card is drawn on one tab only; `canAct` is the wider question the keys
reading the ring ask, and `answerable` asks it. `replyBody` keeps `canCompose`
on top of that, since what it opens is that card. `v` is the one key inert here:
`jumpable` answers false on the tab, which takes it off the footer, rather than
a flag threaded through three renders to say the same thing twice. `showInDiff`
refuses on the tab itself and not on `jumpable`, because that one is also false
for a file the diff does not carry, and there the key owes a toast rather than
silence. And a card in a diff answers the line above
it, so `showFocus` sends it through `jumpTop` rather than to the top row, which
is the rule the jump from the conversation already follows; a hunk heading is
its own beginning and goes to the top row.

A file's block caches the painted code and nothing else. Tokenising is what a
block costs, so the runs of code are what is kept, and the hunk headings and the
thread cards are drawn again every frame and spliced between them. A run holds
its rows rather than the string it joins to, and each row keeps the `paint.Line`
it was painted from: lighting the one the cursor is on is then a paint, where
breaking the run at the cursor would retire the block and tokenise the file
again on every press of `j`. Focus goes in
neither `blockKey` nor `blockState`: in the state it retires the whole cache on
every step of the ring, and in the key it leaves a full rendered file in the map
for every stop the reader walked, with no rule for taking one out. A fold is out
of `blockState` for the same reason: what it changes is a thread card, and a
card is drawn again anyway, so counting folds only threw the map away and
re-tokenised the file for a keypress that could not reach it.

`ring.reset` lets its slice go rather than reusing it. `View` is reached through
value receivers all the way down, so a reused backing array is one the
conversation and the diff write different stops into, and whichever copy is kept
is holding the other's.

A thread card holds the code, the anchor, and the comment that opened the
thread. Everything answering that comment hangs off it on `branch`, the same
rail the threads themselves hang off the review on, and the box a reader is
writing in is the last card on it: an answer belongs where the answers are, and
the rail's corner has to land on it or the run closes above the thing it is
pointing at. The rail opens no line of its own, so a child sits against its
parent. Every other pair of blocks on the page has a blank line between them
because they are separate things; these two are one thing and what hangs off it,
and the gap read as the replies belonging to the page. That is three levels on a review's own thread, which is the deepest
the screen goes. A reply keeps its frame at every width. The body wraps to
whatever the card leaves it, so a narrow pane costs rows rather than words, and
a reply that dropped its border would take the elbow off its byline with it.

**Every card is a ring stop, replies included**, and exactly one is lit at a
time. A card the motion key walks past is one the reader can see and cannot
reach, and lighting two at once is two claims about where a press lands. There
was a second cursor inside a thread once, on `J` and `K`, from when a thread was
one card with its comments stacked in it; a reply is a card now, so the ring is
the only cursor and those keys are gone. The argument for them was that stopping
on every reply makes a heavily reviewed page a chore to cross, and that is what
`ctrl+d` and `ctrl+f` are for: crossing a page is a scroll, not a focus walk.

The two cards answer to different keys, because a reply is an answer to the code
comment and not the code comment. `x` settles a thread and `v` goes to the line
it was written against; neither is a thing an answer has, so both are inert on a
reply and named on neither its footer nor the help. `threadOnRing` is what
refuses them, and `threadHolding` is the looser lookup `r`, `R`, `e` and `D` read
instead: those mean whichever card is under them, and a reply to a reply is a
reply to the thread, which is the only reply GitHub has.

`e` rewrites the block the ring is on and `D` removes it, behind a confirm: a
delete is the one write here GitHub will not undo. `Comment.Kind` picks the
mutation, because one comment type up here is three of them down there. The
description is not one of the three: it is a field of the pull request, so it
goes through `updatePullRequest` and settles in the `Edit` queue. The box is the
reply box, opened on a `focusKey`, and that key is the whole of the difference:
a thread's hangs a card under it, a comment's draws the box where its words
were. **`viewerCanDelete` is true on a submitted review and no call deletes
one**, so `D` is absent there and the client refuses the kind rather than
trusting the flag.

`+` opens GitHub's eight over the same block and toggles the one chosen. It is
the one write key here that never asks whose writing it is: reacting to your own
comment is something GitHub's page offers, so `CanReact` is the whole of the
question, and it reaches the description too, which the other two do not. One
pair of calls covers all four subjects, because `addReaction` takes a node id
and no kind where an edit takes three documents; `Reactable` covers the pull
request as readily as a comment, and offering the key on every card but the
first would make it read as broken on the one the reader meets first. **GitHub
answers with all eight groups on every subject**, nearly all at zero, so the ones
nobody gave are dropped in `internal/gh` or every card grows a row of six empty
pills. The row renders on every card that has one rather than on the lit one
alone: a row that came and went with the focus would change a card's height on
each press of the motion key, which is the same reason the hints ride inside the
bottom border. It goes under the words, and that is what keeps `cardBoxAt`
honest, since that measures down to an open box and anything below one is free.
It goes entirely while a box is over the block, the way the card's byline does:
the box's last row is its own button, so pills under that read as a comment to
react to rather than as one being written, and what a card spends around a box
is a constant that a pill row is not in. The row folds at pill boundaries rather
than running to one line, because a card clips what overflows it silently and
mid-cell: eight of them do not fit a narrow pane, and a fixture with two pills
on a hundred-column frame proves none of that.

The list is the one picker here that turns its own filter off. Eight rows is
exactly `pickerFilterFrom`, whose stated reason is that a shorter list is
already on screen whole, and eight is under `pickerRows`, so this list sits in
the gap between the number and the reason. The filter claims every printable key
ahead of movement, which is right where a filter is worth having and wrong here:
no reaction's name holds a `j`, so `j` narrowed the list to nothing rather than
walking it.

A reaction taken back leaves its group at zero and the group stays on the card
while the write is out. It is the only thing a second press can read, and the
row is inert on a marked one, on the terms a resolve sets out: two toggles on
one reaction settle in the order the responses arrive, which is not the order
they were pressed. The list is opened on what the viewer has already given,
which inverts `comp.Picker`'s own reason for doing that: there `enter` with no
movement is a no-op, and here it takes the reaction off. That is what a toggle
is. Nothing toasts on success, because the pill is already on the card and a
toast per reaction spends the status bar on the smallest write on the screen.

Every write the rail makes has a timeline event behind it, and the detail query
asks for all of them: a write nobody can see happen reads as one that did not
land. They arrive one per label and one per person, so the conversation folds a
run of them back into the one line, on `TimelineItem.Subject`, which is the
single field carrying a label's name, a handle, or a branch. An event whose
subject GitHub nulled is dropped rather than rendered as a verb with nothing
after it. The fold is not the one a push uses: a run needs the same actor and
one minute, where a rebase written over a week is still one push. Two review
requests for the same person an hour apart are two things somebody did, and
folding on kind alone reads as "requested reviews from Copilot and Copilot",
which is what PR #20's own history produces.

Built so far: `cmd/zen-octo`, `internal/config`, `internal/gh`, `internal/store`, `internal/version`, and `internal/tui/{app,comp,keys,list,prview,theme}`. The rest lands milestone by milestone; see the **v1** project in Linear.

A section's filter goes to GitHub as the user wrote it, and `internal/config`
owns the one piece of grammar this client adds: `{{since:24h}}` renders to the
RFC 3339 instant that long ago, in UTC. GitHub's date qualifiers take an
absolute instant and nothing relative, and the search returns `createdAt` and
`updatedAt` and neither `mergedAt` nor `closedAt`, so a window filtered on this
side would spend the limit before the filter ran and show an empty tab on a busy
week. `fetchSection` expands it inside the command rather than around it, so the
window is measured when the request goes out: a session left open all day would
otherwise keep asking about a day that ended hours ago. A bare date would lean
on GitHub's own reading of it and the boundary is the whole point, so the bound
is written full and ending in `Z`. A duration that will not parse is refused by
`validate` with its section named, except in the shipped defaults, which `Load`
answers with before it validates anything and a test pins instead. Search
indexing lags up to a minute, so a pull request merged seconds ago is missing
until the next sync; that is what a section called recent is.

Recently Closed is one section rather than two because `closed:` covers merged
and closed together and the list already renders them apart, under their own
group headers. It carries `sort:updated-desc` because the limit is applied
before the rows reach this side, so without it GitHub's relevance order decides
which twenty come back.

**A merged pull request has no head branch, and the detail query compares
against one.** GitHub answers with the whole pull request, `compare` null, and a
`NOT_FOUND` scoped to `node.baseRef.compare`; go-gh decodes the payload into the
caller's struct before it reads the errors array, so reading that array as fatal
threw away a pull request already in hand and made every merged one unopenable.
`deletedHeadRef` tolerates that one path, on `GraphQLError.Match`, which answers
false unless every error in the array is that one: a real failure arriving
beside it is still a failure. The count then goes to `gh.BehindNoHead` rather
than `BehindUnknown`, which the rail reads as a retarget that has landed and
would render "Merging into main" over a write nobody made. The row says "Based
on main" instead, naming the base and claiming nothing about the distance to it.
The fake had to change with it: one that answers with a body or an error, never
both, cannot produce the shape this bug lives in.

`internal/store` holds the viewer's login, asked for once at startup, pull request sections, one detail per pull request opened, one diff per pull request whose Files tab was opened, and one diff per commit the cursor settled on in the Commits tab. The two per-pull-request caches are keyed the same and filled separately: the diff costs a second request, so it waits until the tab is asked for. The commit cache is keyed by sha instead, because a commit's diff is the same wherever it is opened from. It follows the cursor on a debounce rather than on every keystroke: the cursor has to sit still for `commitSettleDelay` before its commit is asked for, so walking a long branch costs one request rather than one per commit passed through. Issue sections need their own domain type, query, and row shape, and land with ZNO-15.

All three of those grow with use, so all three are bounded: twenty-five pull
requests, twenty-five diffs, forty commit diffs, evicted least recently read
first. Read rather than fetched, which is two different orders: both diff caches
answer a second open from what they hold and fetch nothing, so ordering on the
fetch drops the commit a reader keeps walking back to on a long branch and keeps
the forty they passed once. `UseFiles` and `UseCommitFiles` are the two reads
that say so. A detail on a heavily reviewed pull request is megabytes and a branch
walked in the Commits tab fetches one diff per commit the cursor rests on, so
a day of reading held hundreds of megabytes it would never open again. The bound
is entries rather than bytes, which bounds memory only against a typical entry:
sizing each one wants either a length method per domain type, silently
under-counting the first field somebody adds, or a counting transport in
`internal/gh`, and neither is worth building for a cache policy.

The stamp saying which is oldest is `max(seen)+1` read from the map, never a
counter on the `Store`, and `newCache` builds the maps rather than the first
write. Both are the same reason `nextSeq` carries a comment and `pulsing` has a
test: half this store's writers run on a copy of the model, so a map built there
and an int incremented there are dropped with the copy. A slice of keys in
fetch order has the defect too.

Nothing is dropped while a fetch or a write is out for it, and never the key
just written. `Detail` folds a write over the held detail at read time, so
folding one over an evicted detail folds it over nothing and puts an optimistic
comment on a pull request with no id; and the entry just written is the most
recently fetched thing there is, which makes it the one an eviction must never
choose, though it is the only candidate on a cache where everything else is
pinned. Where everything is pinned the cache goes over its cap instead. A pin
outranks a cap.

Every eviction takes its debts with it, or the maps beside the caches are the
same leak one field over. A dropped detail drops `staleFetch`, `staleTimeline`,
`stalePulse` and its `rowSeq` stamp; a dropped diff drops `staleFiles`, which is
keyed by pull request and so belongs to that cache rather than to the detail.
The `rowSeq` one buys nothing but the space: `restoreRows` reads the folded row,
and an evicted detail has none to fold, so the stamp was already claiming a
correction it could no longer make. What a caller has to notice is that a diff
now outlives the detail behind it, since every open puts a detail and only a
Files tab puts a diff: `correctFiles` reads a number off that detail and 404s on
`repos//pulls/0` without one, which is why it tests the id the way
`correctTimeline` does. The sections themselves are not part of any of this:
that slice is one entry per configured tab holding at most `PRsLimit` rows, and
it never grows.

A detail is not the only question this asks about a pull request. `gh.Pulse` is
the small one beside it: the lifecycle, the review decision, mergeability,
`updatedAt`, `headRefOid` and the head commit's check rollup. It asks for no
branch comparison, which is what lets a merged pull request answer it cleanly
where the detail query survives only through `deletedHeadRef`.
`rollupSelection` and `rollupNode` are shared by the two documents rather than
written twice, so one of them changing shape cannot leave the other decoding
the old one.

**What makes it cheap is the payload, not the point.** Every query this client
sends costs one point of the five thousand an hour: a section search, a detail,
a pulse, measured against the real documents. GitHub bills the requests needed
to fulfil a call's connections, divided by a hundred with a minimum of one, and
the five hundred thousand nodes a call may name is a separate ceiling that
nothing here comes near. Reading the node count as the price says the detail
query costs fifty-six, and that number was wrong wherever it was written down.
What the two documents really differ by is what comes back: on a pull request
with two hundred and twenty-five review threads, four and a third megabytes
against three hundred and thirteen bytes. So the budget was never what stopped
this being asked often. Bandwidth is, and the relayout a landed answer costs.

`PulseApplied` writes those fields over the held detail one at a time, where
`DetailApplied` replaces the struct. The timeline, threads, reviewers,
permissions, `BehindBy` and the merge messages are not on the wire, and a zero
written over any of them empties a page for a refresh nobody asked for. A moved
`headRefOid` is the one thing on it that says somebody pushed, so it marks the
diff stale; the fold ends in `syncRow`, so a pull request merged in the browser
reaches the row behind the screen for no extra request.

Two maps carry it, `pulsing` and `stalePulse`, and they are on the `Store`
rather than on `Detail` for a reason worth keeping: `DetailApplied` builds a
fresh `Detail{}`, so a flag written there is dropped by the next fetch without
anything failing. `StateWriting` and `BaseWriting` are already derived at read
time rather than stored, and staleness already lives in maps beside these, so
this is where per-pull-request bookkeeping goes. Ordering runs both ways. A
pulse never starts under a full fetch, because that fetch answers everything the
pulse would and answers it later. The reverse has to stay open, since `s`, a
write's correction and the probe all run while a pulse is out, so `BeginDetail`
marks the pulse stale and the overtaken answer is dropped. `markStale` marks it
too, on its own first line rather than at each of its twelve call sites: a write
that invalidates a fetch invalidates a pulse, and making that structural is what
stops the thirteenth write path forgetting. Timestamps cannot do this job, since
a check turning green and a `mergeStateStatus` settling both leave `updatedAt`
where it was, so two responses can carry the same instant and disagree.

Nothing asked for any of it until a beat did. `pollTickMsg` is one self-arming
tick at `pollBeat`, armed in `Init` and in its own handler and nowhere else:
one chain with one starting point, which is the whole defence against two of
them running at double the rate. The probe next door can avoid that by arming
only on a state transition, and the spinner by tagging, because both are chains
that end; this one never ends, so it cannot be re-armed from anywhere. It
carries the instant it fired, and every interval here is a comparison against
that rather than a clock read inside `Update`, which is what lets a test fire a
beat from any point in time it likes. **Only the screen in front of the reader
polls**: the detail's own pull request, or the one section the tab strip is on,
never both and never a tab nobody is looking at. A beat under `Capturing()`
asks for nothing at all and still arms the next, because a chain that ended
where it found no work would never restart.

Freshness is measured from when something last answered rather than from when
it was asked, and the stamps live on the root beside the two refresh trackers,
not in the store. A failure stamps too: a beat that could not reach GitHub costs
one interval rather than being retried on every beat after it, which is also
what answers a pulse that failed leaving the Merge row on "Checking". **Only
what has already answered is polled at all**, which is why a zero stamp is never
due: `Init` fetches every section without calling `Begin`, and a first beat
reading a zero as due would fire a duplicate of a request still in flight.

`pollBeat` is also the interval for a pull request that is still moving, which is
checks running or a mergeability GitHub has not worked out; `pollIdle` is
everything else and the list. The list cannot usefully go faster: search indexing
lags up to a minute, so asking twice inside one returns the same rows. Neither
can the budget be what stops this. Every query costs one point of five thousand
an hour, and a beat on each screen at these intervals is under a fifth of it, so
the minimum-interval floor and the backpressure earlier drafts called for guard a
threat that does not exist.

**What does cost is rendering, and that is why `PulseApplied` reports whether
anything moved.** `prview.SetDetail` drops both render caches and relayouts, 7
to 12.7ms and 27ms a keystroke with a compose box open, and it has no early-out
of its own. Pushed on every beat that would be a hitch every beat for a pull
request sitting still, so `pulseSettled` pushes nothing where nothing moved.
`pulseMoved` sits beside the fold and compares the same fields it writes, which
is what stops a ninth being written and left uncompared. A pulse the store
dropped reports the same false and means it: it wrote nothing. None of this
shows in a frame, since the same detail draws the same page; arming the commit
debounce is the one place calling `SetDetail` can be told from not calling it,
and that is where the test stands.

A moved `updatedAt` is the other half. It is the only thing on this wire that
reports a comment, a review or a label, and it carries none of them, so it marks
`staleTimeline` and only a full fetch pays that debt. `correctTimeline` spends it
behind `ShowsTimeline()`, because the conversation is the only tab any of it
reaches and the page is megabytes; the debt keeps until the reader goes there,
and the beat is what notices when they do. It is `staleFiles` one field over,
down to being marked unconditionally and cleared where the fetch it was for
lands. It goes out as `fetchPage` rather than through `correctDetail`, and that
is two differences rather than one. **`DetailFailed` leaves the debt standing**,
so nothing else would stop a beat re-sending a megabyte query every five seconds
against a broken network; the poller stamps the failure and costs it an interval,
which is `stampSection`'s rule one screen over. And a failure reaching
`detailSettled` toasts "Could not refresh" over a page nobody asked to have
refreshed, so `pageFailedMsg` ends at the store and says nothing.

**A store write made on a model nobody keeps is a write that did not happen**,
and the two halves of one can land differently: `store.Begin` stamps the section
into a slice and takes the number from `s.seq`, an int, so on a copy the stamp
lands and the counter does not. The next write then shares its number, and
`restoreRows` reads `rowSeq <= began` as the write being older than the fetch and
throws it away. That is why `pollSectionDue` hands the model back where
`pollDetail` needs only a command: everything `BeginPulse` and `BeginDetail`
touch is a map, and maps survive. `nextSeq` is the only write here with that
shape, and it is commented as such.

A polled section needs a failure of its own. `Failed` puts a section into its
error state and the list renders that **instead of** its rows, so a background
poll going through it would replace a list the reader is reading fine with a
message about a request they never made. `PollFailed` ends the flight and keeps
everything held, which is `PulseFailed`'s argument one screen over; ending it is
the other half of the job, since a section left loading would have `Begin` refuse
every poll after it. What it puts back is whichever answer the section last had
rather than Ready: `Applied` clears the error, so one still held means the last
real answer failed and the reader was shown it, and clearing that would take the
report off the screen with nothing having succeeded. Nothing else about a poll is visible: `spinning` is false
for a section that has already loaded, so a reload draws no spinner, and neither
path takes a refresh leg, so the bar neither spins nor speaks.

Unless a sync adopts it. `Begin` refuses a section already in flight and
`refresh` used to drop the ones it refused, which the beat turned from an edge
into the common case: a poll holds the section on screen every half minute, so
`s` pressed inside one refreshed every tab except the one being read and toasted
a count that included it. It waits on the answer already coming instead, the way
`refreshDetail` waits on a detail a write asked for. That is what makes the
failure two answers rather than one. `PollFailed` is the quiet one; an adopted
flight gets `Failed`, because the reader did press the key, and a summary reading
the quiet one would report a failure as a pass. The same hole ran one screen over
on `pageFailedMsg` and ran worse: it reached the store and never the leg, so a
refresh that adopted a background page never ended at all and the bar spun for
the rest of the session. All three legs adopt now, the two diffs included, since
one leg behaving differently from the others is worse than either behaviour: `r`
pressed while a diff is still on its way named the pull request and never the
diff it was waiting on.

Beside those it keeps one set of choices per repository, keyed by `owner/name`: the labels, the assignable users, the mentionable users, the branches, and which merge methods the repository allows. They belong to the repository rather than to any pull request, so they outlive the screen that asked and are fetched once. `BeginRepoMeta` refuses one already loaded as well as one in flight; `InvalidateRepoMeta` is what lets a sync reach them.

The two lists of people are two connections because they are two sets. `assignableUsers` is who may be given the pull request or asked for a review; `mentionableUsers` is the wider one, everybody who has taken part, which is who an answer is usually addressed to. `gh.Mention` is its own type rather than an `Actor` with a name on it: an `Actor` carries a node id because the lists a picker writes back are addressed by one, a mention is inserted as text and has none, and typing it as an `Actor` would let a list of people with no id compile straight into `assigneeChoices` and be matched on the id every one of them is missing.

A detail is the same row search returned, fetched later: `internal/gh` builds the two field for field the same way, down to counting comments as the conversation plus its review threads. So `syncRow` writes a landed detail back over every section carrying that pull request, and a lifecycle change made on the rail corrects the list behind it with no request of its own. It reads the folded row rather than being handed one, on the terms `restoreRows` reads one: a write applied here and not yet answered for is on the screen, and a response that knows nothing about it must not take it off the list. Taking the row from the caller put that decision at five call sites, and the one added last got it wrong. Which section a pull request belongs to is a search query and stays GitHub's answer; the row's own fields are all this reaches, so a closed pull request keeps its place in an `is:open` tab until the next fetch and sits under the Closed group meanwhile.

Two of a reviewer's fields are derived rather than fetched, and the rail colours them: `Unresolved` and `Threads`, which is how a reviewer who only commented still reads as blocking while their threads are open, and how a changes-requested review whose every thread has been dealt with goes amber instead of red. Nothing on the wire carries either, so anything moving a thread here has to recompute them or the mark sits where the last fetch left it. `gh.RecountThreads` is the one implementation, called at the end of the detail's own parse and again by the store wherever threads change, reading `ReviewThread.ReviewID` against the timeline's review items rather than the response it came in. It runs on any thread change rather than on a resolve alone, because a comment deleted takes its thread with it. Both callers clone the panel first, on the terms the threads are cloned.

It also holds the writes still in flight, keyed by pull request and folded in on the way out of `Detail`: a comment onto the timeline, a reply into the review thread it answers, a resolve over the thread it settles, a reaction over the block it was given to, and an `Edit` over the metadata it replaces. Beside the fetched detail rather than inside it: a refetch replaces a timeline wholesale, and one fetched before the mutation answered is not evidence the mutation failed. Written in, an optimistic comment would vanish on the next refresh with nothing to say why. A thread the refetch no longer carries has nowhere to hang a reply, and the reply waits out of sight rather than the store inventing a thread GitHub did not send.

Folding a reply clones both slice levels. A thread's comments are their own slice, still the held one after the threads are cloned, and a thread with spare capacity takes the append in place: a detail already handed out then changes under whoever is holding it, which on this screen is a rendered conversation. A resolve needs the outer clone alone, and it folds through the same one the reply used: cloning again from the held slice throws the reply away. It writes the state and never the permissions, because a locally flipped `CanUnresolve` offers a key that opens a write GitHub rejects. It marks the thread pending, and the key goes inert on a marked one: two resolves out at once settle in the order the responses arrive, which is not the order they were pressed.

A reaction is a fourth kind of write rather than a `CommentWrite` carrying one, because that type's delete branch takes a thread away with its last comment and its edit branch marks the comment `Editing`, and a reaction does neither. It reaches one field further out as well: the description carries reactions and has no comment to name, so an empty `CommentID` is what says the subject is the pull request. Its fold rebuilds the set in GitHub's own order rather than appending, or a reaction given here moves sideways the moment the answer lands. **It settles the one group the write moved and never the whole set the payload carries**, even though the payload carries all of it. Two toggles on one subject answer in whatever order the network gives them, and each response is a snapshot of the subject as GitHub had it when it handled that call: taking either one whole lets the older snapshot land last and delete a reaction the other one added. Writing the group this write is answering for is order-independent, because no two writes on a subject share a content while the first is still out. It costs the head start the whole set would have given on somebody else's reaction, which is worth less than a pill that vanishes.

An `Edit` settles by writing GitHub's answer into the held detail and then dropping itself, and the answer is stale only against a later write on the same field: `editField` is what keeps a label set landing mid-lifecycle-change from being thrown away. The reviewer panel is the exception, and `dropEdit` hands the write back for it. There is no answer worth taking, because the endpoint reports the outstanding requests and nothing about who has already reviewed, so the write's own optimistic panel is promoted into the held detail instead. Dropping it and waiting for the refetch would put the fetched panel back for the length of a round trip, which reads as the write undoing itself.

Code is highlighted from a Chroma style named by the theme (`Theme.Syntax`), overridable with `syntaxTheme` in config. `internal/tui/comp.Syntax` returns colored tokens rather than rendered text: Chroma's own terminal formatter writes resets that would tear a row's background open.

## Rendering traps

Each of these looks like working code and produces a broken frame.

- **A pull request's state and its draft flag are two fields, and a closed one carries both.** GitHub never clears the flag, so reading it ahead of the state labels a closed draft "Draft" and marks it as waiting for somebody to pick up. Read the lifecycle first; the flag is what reopening gives back.
- **Every styled cell ends in a full SGR reset**, which clears the background along with the foreground. A row background has to be set per cell; wrapping a joined row paints only the first one.
- **`lipgloss.Canvas.Compose` ignores a layer's position** and draws every layer at the origin. Compositing an overlay needs `lipgloss.NewCompositor`.
- **`Style.Width` wraps before it clips.** Truncating to a column width means clipping explicitly first, or one long title becomes two rows.
- **`viewport.EnsureVisible` is not a scroll-to-cursor.** It acts only once the line is already outside the window, then puts it on the top row. A cursor moving down a row at a time jumps a whole page and then sits still for the rest of it. Move the offset by hand.
- **The shortest scroll onto the screen is the wrong one.** Bringing a block into view by the minimum distance lands it at the foot of the window with everything under it below the fold, and a block taller than the window opens on its last line with its heading above the top. Move it to the top row instead, and leave it where it is when it already fits on screen whole. A fixture whose blocks are all shorter than the window proves none of this.
- **A pane clips overflow silently.** A row wider than the pane loses its trailing columns mid-cell with no ellipsis, and a width test still passes because the pane fills its line. The row has to fit before the pane sees it.
- **Glamour output belongs to the width it was rendered at.** It pads every line out to that width, so the viewport has to be handed exactly the same number or soft wrap puts every line onto two. Caching by body alone repaints the previous width's wrap.
- **A viewport offset is a line, and a row is not.** Once rows are two lines and group headers are one, scroll arithmetic that lands on the row it wants opens the window on that row's second line with its title cut off above. Round the offset up to the next item boundary. A test at an even content height proves nothing: the arithmetic lands on boundaries by accident there.
- **Rounding the offset is not enough if the window is not a whole number of rows.** At the end of a list the viewport clamps to its own last offset, and against an odd height that clamp lands back between two lines. Size the viewport down to a multiple of the row height; the pane pads the spare line back.
- **A key that moves by a page is counting lines, and a cursor is counting rows.** Handing the pane height straight to a two-line-row column moves the cursor twice as far as the window does, so every press skips a screenful that never appears on screen.
- **Soft wrap and a line-number gutter cannot both be on.** One long line of code folds onto a second row, and every line under it is then one further out of step with the number beside it. Turn `SoftWrap` off and clip, and only ever measure a diff at a width where something overflows.
- **A lexer carries state across lines.** Highlighting a diff line by line comes apart on the first multi-line string. Tokenise the whole file, and tokenise the two sides of the diff separately, or the lexer is reading a file that holds both halves of every change.
- **A single newline is a line break in a GitHub comment and a space in CommonMark.** Glamour follows the spec, so two lines somebody typed arrive as one and the comment reads differently here from the browser it was written in. `glamour.WithPreservedNewLines()` is the switch.
- **Soft wrap costs half the price of setting a viewport's content, and the conversation has nothing for it to fold.** Every block is wrapped to `bodyWidth` before the viewport sees it. Leaving it on spends 12.7ms against 7.0ms on a hundred-and-forty-comment thread, which is a per-keystroke bill once a comment is being written into the page.
- **A text box inside a scrolling pane rebuilds the whole page on every keystroke.** Cards are re-bordered one by one, so a long thread costs 27ms a character with the markdown cache hitting perfectly. Nothing around the box can change while it has the keyboard, so build the head and the tail once and join a fresh box between them.
- **The page splits at the outermost block holding the box, not at the block that holds it.** A review renders its own card and every thread it opened as one string with a branch gutter down the side, so cutting between two of them means splicing `├─`, `│ ` and `╰─` back together at the right variant. Cut either side of the whole review instead.
- **Scrolling the shortest distance is right wherever a box is involved, and wrong everywhere else.** A key that lands on a block is taking the reader somewhere, so the block goes to the top row. A box is different twice over. A character typed is not taking them anywhere, and hauling the page for it is the worse of the two wrongs. Opening one is worse still: the box sits under what it answers, so moving it to the top row scrolls the thread off the screen and leaves the reader writing a reply to something they can no longer see. The caret is the whole of the arithmetic, and the block holding the box is not consulted: the reply box hangs off a thread and an edit can be the comment that opened one, and neither is a ring stop, so a scroll that goes looking for the block does nothing in exactly the two cases the box is nested. It follows the row under the caret rather than the caret's own, because the button that sends the words sits directly beneath the box, and a foot one line below the fold leaves the reader writing into something with no visible end. The same button is why a box is capped to the pane, and the cap is the render site's to name: the comment that opened a thread pays for two cards, and one that spent the pane as though it were a card of its own would push the deeper foot off the screen. A reply is a card of its own, so it pays for one.
- **A text input sized during a render is sized on a copy.** `View` is reached through value receivers all the way down, so a `SetWidth` there is thrown away with the copy, and the real widget keeps a width of zero: it renders from the first character, never scrolls, and every keystroke past the visible edge is invisible while the caret sits off the box. Size the fields when the thing opens and when the screen resizes, never while drawing.
- **A text input recomputes its visible window only when the caret leaves it.** Writing a longer value and then putting the caret inside the window the old one had leaves that window exactly where it was, so the box goes on showing as many characters as the short value did. Ending first and coming back is what forces the recompute. A fixture whose two values are the same length proves none of this.
- **A box that has just opened is a journey, and the caret is not where it ends.** The caret opens on the box's first row, so a scroll that follows it leaves the rest of the writing area, the button and the border below the fold, and the reader is writing into something with no visible end. Opening lands the foot; typing follows the caret. `showOpenedBox` beside `showCaret` is that split, and neither ever scrolls past the caret's own row, because a box taller than the window can only show one end.
- **A write that changes a card's height has to put it back on the screen, and only where it was whole to begin with.** Unresolving opens a collapsed thread into its card, its code and every reply hanging off it, and that growth arrives through the store rather than under the key: `space` re-shows the focus itself and `x` has no equivalent, so the thread grew off the bottom and sat there. `SetDetail` asks before and after. Asking only after would haul a reader who had scrolled somewhere else back to the focus they left.
- **A block that answers the line above it cannot go to the top row either.** The rule holds past the compose box. A review thread in the diff hangs under the code it was written against, so a jump that tops the card scrolls that code away and lands the reader on a comment about something they cannot see. Open a few lines above it instead, and never above the file's own heading, which reads as the wrong file until the eye finds the border.
- **A caret's column is two different numbers.** `Column()` counts runes into the logical line and `LineInfo().CharOffset` counts cells across the screen. Detection wants the first and placement wants the second, and swapping them is invisible until a comment holds an emoji or a line of CJK, at which point anything drawn at the caret sits somewhere else entirely.
- **A block's own indent is not the indent it was drawn at.** `boxAt` is a line relative to its block and `boxCol` has to be a column relative to it, threaded through the same sites and gaining `treeGutter` at every rail it hangs off. A column measured once on the compose card is two cells wrong for a reply and four or six for a reply under a review's thread, and the page body carries a centring gutter on top of that which is tens of columns wide on a wide terminal.
- **An overlay anchored at the caret drifts if it is anchored at the caret.** A popup answering a word wants the word's first cell, not the caret's, or it steps right once per keystroke while the reader is reading it. Anchor on what the list is about and let the caret run.
- **`comp.Over` centres, and centring is not a special case of placing.** A positioned overlay has to clip to the frame before it clamps, or one wider than the pane is measured at its uncut width and pushed off the right edge; and it owes the same trailing-space re-pad, because the compositor trims every line and the pinned header's lines end in padding rather than in a border rune. Clamp to the pane rather than the frame, or a popup hangs over the rail beside it.
