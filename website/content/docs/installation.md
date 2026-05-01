---
title: "Installation"
metaTitle: "Install SpiceEdit on Mac, Linux, and Windows"
metaDescription: "Install SpiceEdit on macOS via Homebrew, on Linux with a one-line curl script, or on Windows from a GitHub Release. No runtime, no setup steps."
summary: "Install SpiceEdit on macOS, Linux, or Windows."
weight: 10
---

SpiceEdit ships as a single static Go binary. There is no runtime, no node, no language server, no setup. Pick the path that matches your platform.

## macOS (Homebrew)

The Homebrew formula lives in this repo's `Formula/` directory. Tap it by URL — there's no separate `homebrew-tap` repo to remember:

```sh
brew tap cloudmanic/spice-edit https://github.com/cloudmanic/spice-edit
brew install cloudmanic/spice-edit/spice-edit
```

Both Apple Silicon (`arm64`) and Intel (`amd64`) builds are published on every release. Homebrew picks the right one.

### Updating

```sh
brew update
brew upgrade cloudmanic/spice-edit/spice-edit
```

### Uninstalling

```sh
brew uninstall cloudmanic/spice-edit/spice-edit
brew untap cloudmanic/spice-edit
```

## Linux (one-line install script)

The fastest way onto a Linux box — including remote SSH targets and Alpine images:

```sh
curl -fsSL https://raw.githubusercontent.com/cloudmanic/spice-edit/main/install.sh | sh
```

The script detects your OS and architecture (`linux/amd64`, `linux/arm64`, `darwin/amd64`, `darwin/arm64`), downloads the matching archive from the latest GitHub Release, extracts the `spiceedit` binary, and drops it into `~/.local/bin` if writable, otherwise `/usr/local/bin`. Re-run the same command to upgrade.

It's plain POSIX `sh` — no bash, no curl-isms. It needs `tar` and one of `curl` or `wget`. Override behavior with environment variables:

```sh
# Pin a specific version.
curl -fsSL https://raw.githubusercontent.com/cloudmanic/spice-edit/main/install.sh | VERSION=v0.0.23 sh

# Install to a custom directory.
curl -fsSL https://raw.githubusercontent.com/cloudmanic/spice-edit/main/install.sh | INSTALL_DIR=/opt/bin sh
```

## Linux (manual binary)

If you don't trust pipe-to-sh, grab the archive yourself from the [GitHub Releases](https://github.com/cloudmanic/spice-edit/releases) page:

```sh
# Replace VERSION and ARCH (amd64 or arm64) to taste.
curl -L -O https://github.com/cloudmanic/spice-edit/releases/download/v0.0.23/spiceedit_0.0.23_linux_amd64.tar.gz
tar -xzf spiceedit_0.0.23_linux_amd64.tar.gz
mv spiceedit ~/.local/bin/
```

## Windows

Windows builds (`amd64` only — no arm64 yet) ship as a zipped binary on every GitHub Release.

1. Download `spiceedit_<version>_windows_amd64.zip` from the [Releases page](https://github.com/cloudmanic/spice-edit/releases).
2. Unzip it.
3. Move `spiceedit.exe` to a directory on your `PATH` — `C:\Users\<you>\AppData\Local\Programs\spiceedit\` is a fine choice.
4. Open Windows Terminal and run `spiceedit`.

There is no installer or Chocolatey package yet. The binary works in Windows Terminal, ConEmu, and WSL.

## Build from source

For the masochists, the contributors, and anyone behind a corporate firewall:

```sh
git clone https://github.com/cloudmanic/spice-edit.git
cd spice-edit
make install   # builds and installs to $GOPATH/bin
```

Requires Go 1.22 or later. CGO is off by default — the build is fully static.

## Uninstall (manual)

The install script doesn't write anywhere except the binary destination. To remove SpiceEdit, delete the binary:

```sh
rm ~/.local/bin/spiceedit
# or wherever you installed it
```

Optionally clean up its config and state directories:

```sh
rm -rf ~/.config/spiceedit ~/.local/state/spiceedit
```
