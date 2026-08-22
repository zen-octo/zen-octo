# Install

## Requirements

- **[GitHub CLI](https://cli.github.com), authenticated.** zen-octo rides on
  `gh`'s token rather than asking for one of its own. If `gh auth status` is
  happy, so is zen-octo. Where a scope is missing it says which and prints the
  `gh auth refresh` line to fix it.
- **Nothing else**, for a released binary. Everything is pure Go and statically
  linked, so there is no libc to match.
- **Go 1.26.6 or later**, only if you are building it yourself.
- A terminal at least **56 by 23**. Under that the shell draws its size instead
  of a screen, so a drawer beside an editor still works and a window smaller
  than the merge form does not pretend to.

Releases carry macOS and Linux on arm64 and amd64, and Windows on amd64.

## Install

```sh
curl -fsSL https://raw.githubusercontent.com/praxis-labs-io/zen-octo/main/install.sh | sh
```

It downloads the binary for your platform and puts it in `~/.local/bin`. It is a
POSIX script, so Windows takes the `.zip` off the
[releases page](https://github.com/praxis-labs-io/zen-octo/releases) instead.

`INSTALL_DIR` overrides where it lands, and `VERSION` pins a release. Every
release carries a `checksums.txt` beside the archives.

### From a clone

```sh
git clone https://github.com/praxis-labs-io/zen-octo.git
cd zen-octo
make install
```

That builds this tree into `~/.local/bin/zen-octo`. Run it again after every
change or you keep running the old binary.

If `~/.local/bin` is not on your `PATH`:

```sh
export PATH="$HOME/.local/bin:$PATH"
```

## Running it

```sh
zen-octo
```

You land on the pull request list, in the first section your config declares.

There is one subcommand:

```sh
zen-octo config-path
```

It prints where config is read from, which is `~/.zen-octo/config.yml` unless
`ZEN_OCTO_CONFIG_DIR` says otherwise. [Configuration](configuration.md) covers
what goes in it.

`zen-octo --version` says what you are running.

## Upgrading

Re-run the installer, or from a clone:

```sh
git pull
make install
```

Nothing checks for updates and nothing phones home.
