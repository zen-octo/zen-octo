# Configuration

zen-octo reads `~/.zen-octo/config.yml`. `zen-octo config-path` prints the exact
path, and `ZEN_OCTO_CONFIG_DIR` overrides the directory.

There is no config file until you write one. Everything below has a default, so
a missing file is a working app rather than a prompt.

## Sections

A section is one tab in the list, a title and a raw GitHub search query. The
query is passed through as typed, so anything GitHub's search understands works
here.

```yaml
prSections:
  - title: My PRs
    filters: "is:open is:pr author:@me"
  - title: Needs My Review
    filters: "is:open is:pr review-requested:@me"
  - title: Involved
    filters: "is:open is:pr involves:@me -author:@me"
  - title: Recently Closed
    filters: "is:pr is:closed author:@me closed:>={{since:24h}} sort:updated-desc"

issueSections:
  - title: My Issues
    filters: "is:open is:issue author:@me"
  - title: Assigned
    filters: "is:open is:issue assignee:@me"
```

Those are the shipped defaults. Replacing `prSections` replaces all of them.

### Time tokens

`{{since:24h}}` becomes the RFC 3339 instant that long ago, which is the only
date shape GitHub's search takes. Anything Go's `ParseDuration` reads works:
`30m`, `12h`, `168h`. A duration it cannot parse is left in the query as typed
rather than silently becoming something else.

Note the `sort:updated-desc` on the closed section above. The limit is applied
before the list re-sorts, so without a sort it is relevance that decides which
twenty come back.

## Defaults

```yaml
defaults:
  prsLimit: 20
  issuesLimit: 20
```

How many rows a section fetches. The ceiling is 100.

## Theme

```yaml
theme: rose-pine-moon
syntaxTheme: ""
```

`theme` is the palette the chrome is drawn in. One ships today,
`rose-pine-moon`, which is also the default. A name that is not registered
degrades to the default rather than failing to start, so a typo leaves you with
a working app.

`syntaxTheme` is a separate question: the palette code is highlighted with. It
stays empty by default, because a theme already names the one that matches it.
Set it only to override that pairing.

## A bad config

A section that fails to validate is reported with its section named. The app
draws a notice line rather than refusing to start, so a config you are still
editing does not lock you out of the tool you are editing it for.
