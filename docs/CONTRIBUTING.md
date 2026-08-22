# Contributing

## Setup

```sh
git clone https://github.com/praxis-labs-io/zen-octo.git
cd zen-octo
git config core.hooksPath .githooks
make install
```

The hook wiring matters. `.githooks/pre-push` is tracked and rejects pushes to
`main`; untracked `.git/hooks/` files do not survive a clone, which is why it
lives in the repo. Do not reach for `--no-verify`.

`make install` builds this tree into `~/.local/bin/zen-octo`. Run it after every
change or you keep testing the old binary.

## The checks

`make all` is the gate. It runs lint, then tests, then the build.

| Command | Does |
| --- | --- |
| `make all` | lint, test, build. What has to be green |
| `make test` | `go test -race -coverprofile ./...` |
| `make lint` | gofmt, go.mod tidiness, golangci-lint |
| `make fmt-fix` | `gofmt -w .` |
| `make golden` | Regenerate the golden files |
| `go test ./internal/gh/ -run TestName` | A single test |

Run them directly, never through a pipe that swallows the exit code.
`make lint | tail` reports success on failure.

CI pins golangci-lint to match the local brew version, in
`.github/workflows/ci.yml`. Keep the pin current or CI and local runs stop
agreeing.

## Boundaries

These are the architecture, and breaking one is a review-stopper rather than a
nit.

- **`internal/gh` is the only package that touches the network.** It returns
  domain types, never raw API structs, so everything above it is testable
  against a fake. Two transports: GraphQL for everything, REST for the diff
  alone, because GraphQL has no field carrying a patch.
- **`internal/store` owns fetched state and what is owed a refetch.** Views read
  from it, they never fetch. It owns no clock: ordering is a counter and
  staleness is a mark, and every duration lives in the TUI layer where the
  timers are.
- **`internal/tui/*` packages never import each other sideways.** Shared widgets
  live in `internal/tui/comp`.
- **No component reads terminal dimensions or counts chrome lines.** `app` owns
  the size, subtracts the status bar, and calls `SetSize` downward.
- **Every scrollable region owns its own viewport.** Scroll state never sits on
  the root model.
- **Bindings are declared once** in `internal/tui/keys`, with their help text
  attached. The overlay renders from the same declarations, and tests hold that
  nothing declared goes unlisted and nothing listed is invented.

Writes are optimistic: apply locally, toast, reconcile on the response, revert
on error. Every write path needs the revert branch, not just the happy one.

## Docs that describe the code

Every user-facing surface has a document, and a change to the surface makes that
document wrong until somebody moves it. This is the map, read at merge time and
again before a release:

| Changed | Read |
| --- | --- |
| `internal/tui/keys/**` | [`keys.md`](keys.md) |
| `internal/tui/**` | [`guide.md`](guide.md), [`keys.md`](keys.md) |
| `internal/config/**` | [`configuration.md`](configuration.md) |
| `internal/gh/**`, `internal/store/**` | [`guide.md`](guide.md) |
| `Makefile`, `install.sh`, `.github/workflows/**` | [`install.md`](install.md), [`README.md`](../README.md) |
| the boundaries, the test conventions | this file |

`git diff --name-only <ref>..HEAD` gives the left column, so the set of documents
to check is derived rather than remembered.

A change nothing on this map covers is one of two things: a doc gap to fill, or
work no user sees. Say which rather than leaving it unanswered.

## Version numbers

Semver, and pre-1.0 while the shape can still move:

- **Minor** carries anything a user would notice. A new binding, a changed
  default, a tab that answers differently.
- **Patch** carries fixes and everything internal.
- **Major** waits for 1.0.

A published tag is permanent. It cannot be renumbered, and a release cut under
the wrong number stays wrong, so the version is confirmed before the tag is
pushed rather than inferred from the range.

## Tests

Tests ship in the same commit as the behaviour they verify, never a follow-up.

- Test through the real interface. Drive key messages and assert rendered
  frames, not model fields: a test that only reads internal state stays green
  while the thing it renders is broken.
- `internal/gh` is tested against a fake transport, which is what returning
  domain types buys.
- A test asserts the rendered frame never exceeds the size it was given.
- Golden files hold frames still. `make golden` regenerates them, and a golden
  that changed is a diff to read rather than a file to accept.

## Commits and pull requests

- Atomic and single-purpose. Implementation, cleanup and unrelated refactors are
  separate commits.
- Never commit a known-broken intermediate state.
- Terse messages describing intent.
- Feature work goes ticket, branch, PR. `main` is the product branch.
- Doc-only changes and genuinely trivial fixes skip the PR.

Agent-facing conventions and the reasoning behind the design live in
[`CLAUDE.md`](../CLAUDE.md).
