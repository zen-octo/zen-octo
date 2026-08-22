# Guide

zen-octo handles a pull request end to end without opening a browser: read it,
discuss it, watch its CI, fix its metadata, merge it. Issues get the same
treatment where it makes sense.

The keymap is in [keys](keys.md), the config file in
[configuration](configuration.md), and installing it in [install](install.md).

## The list

You open on a list of pull requests, divided into sections. Each section is a
GitHub search query you wrote, so what is on screen is whatever you told it to
watch: yours, the ones wanting your review, the ones you are involved in.
`]` and `[` walk the sections, `⏎` opens a row, `s` refetches.

A section that has never answered shows a block rather than rows, and a failed
one shows its error. A reload keeps its rows: the spinner moves to the status
bar and the keys keep working on what is already there.

`y` copies a pull request's link and `O` opens it in a browser. Both work on the
list and on every tab of the detail screen.

## The pull request

Four tabs, walked with `]` and `[`.

**Conversation** is the description and every comment and review under it, as
cards. `}` and `{` walk them.

**Files** is the diff. `tab` and `shift+tab` change file, `}` and `{` walk the
hunks and the comments written against them, and `m` marks a file viewed. `|`
puts the two sides in two columns, where `h` and `l` step between them.

**Commits** lists them, walked whole rather than by hunk.

**Checks** is CI. `/` searches a job's log, `n` and `N` walk the matches, `f`
jumps to the first failure, and `r` reruns a job.

Whatever the tab, `}` and `{` mean the same thing: go to the next block. What a
block is belongs to the tab.

## Talking

`c` writes a comment. On Files it is scoped to what the cursor is on, so a
comment lands on the line you are reading rather than the file as a whole.

`r` replies to the focused card, `R` quotes it first. `e` edits your own, `D`
deletes one behind a confirm, and `x` resolves or unresolves a thread. `+` opens
GitHub's eight reactions over the block and toggles the one you pick.

`ctrl+⏎` posts. `ctrl+e` opens `$EDITOR` for anything longer than a line, which
is the escape hatch for writing prose in a box that is not your editor.

`v` on a review comment shows it in the diff, which is the jump from the
conversation to the code it was written against.

Writes are optimistic. What you did appears immediately, and if the API refuses
it the change is reverted and a toast says so.

## The details rail

`d` opens a rail down the side carrying the five things you would otherwise open
a browser to change: state, labels, reviewers, assignees and the base branch.

Every row answers to `⏎`, which opens a picker as a centred modal. The picker
owns the keyboard while it is up: the keys that can never be text go first, the
filter claims every printable one, and movement takes what is left.

The rail is a list of controls rather than blocks of prose, so `j` and `k` walk
its rows and the braces are dead on it. Its cursor stops at each end rather than
wrapping.

`d` answers at every width the shell will draw, because the rail is the only
route to those five writes. What the width decides is where it lands: wide
enough and it is a column, narrower and it is painted over the conversation. A
client in a drawer beside an editor is not read-only.

## Merging

The merge form offers the methods the repository allows, with the commit message
GitHub itself would use for each. It is not a message zen-octo invents: the
repository decides the headline and body per method, and the form shows what you
are actually about to write.

Where the head branch can be deleted the form offers that too, and where a
protection would be bypassed it says so before the button rather than after.

## Sizing

Under 56 by 23 the shell draws its size instead of a screen. The height is where
the merge form stops fitting; the width is where the rail stops landing whole.

Nothing is torn down under it, so a terminal dragged small and back is the one
it was with whatever was open still open. What is dropped is the keyboard,
everything but the ways out: a picker still holding a set would otherwise write
it on a blind `⏎`, and the merge form's button is one blind `⏎` from a merge.
