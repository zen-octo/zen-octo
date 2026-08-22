# Release notes

One file per release, `vX.Y.Z.md`, curated rather than generated.

`.github/workflows/release.yml` cuts the release with
`--notes-file docs/release-notes/<tag>.md`, and the release job checks out the
tag, so **the notes file has to be on `main` before the tag is cut**. A tag that
arrives first fails the run, and the tag is published by then.

No H1. `gh` titles the release from the tag, so one here repeats it.

The `release` skill writes these. See `.claude/skills/release/SKILL.md`.
