<!--
  File: README.md
  Author: Spicer Matthews <spicer@cloudmanic.com>
  Created: 2026-04-29
  Copyright: 2026 Cloudmanic, LLC. All rights reserved.
-->

# SpiceEdit

> An opinionated, **mouse-first** terminal code editor for SSH workflows.

SpiceEdit is a single-binary code editor that runs inside your terminal but
behaves like a tiny VS Code: a file tree on the left, tabs across the top,
syntax highlighting in the middle, a status bar at the bottom — and it's
all driven by the **mouse**, not arcane keystrokes.

It's built for the workflow most "modern" terminal editors ignore: SSHing
into a remote box from inside `tmux` / `zellij`, opening a project, clicking
around files like a normal human, copying and pasting through your local
clipboard, and getting back to work.

## Why does this exist?

Vim and friends are wonderful if you've spent years memorizing them. Most
terminal editors assume you have. SpiceEdit doesn't.

The goals, in order:

1. **Mouse-first.** Click a file to open it. Click a tab to switch.
   Click-and-drag to select text. Scroll wheel actually scrolls.
   Drag the splitter to resize the sidebar. Right-click (or click the
   `≡` icon, or double-tap `Esc`) for the action menu.
2. **No hot-key archaeology.** Save, save & close, quit — they all live
   in a centered modal you open with one gesture. No `Ctrl+` shortcuts
   that fight `tmux`, your shell, or your terminal emulator.
3. **SSH-friendly.** Copy uses OSC 52 escape sequences with a tmux
   passthrough wrapper, so highlighting text on a remote box still
   ends up in your local Mac clipboard.
4. **One static binary.** No runtime, no plugin manager, no config
   directory full of YAML. Drop it on a server and run it.
5. **Looks reasonable.** Tokyo Night-inspired palette out of the box,
   syntax highlighting via [chroma](https://github.com/alecthomas/chroma)
   (no CGO, no tree-sitter setup).

## Features

- **VS Code-shaped layout** — file tree on the left, tab bar across the
  top, editor in the middle, status bar at the bottom.
- **Mouse-driven everything** — click to place cursor, drag to select,
  scroll wheel scrolls, double-click selects a word, drag past the edge
  to auto-scroll a selection.
- **Syntax highlighting** for dozens of languages via Chroma.
- **Action menu** opened with the `≡` icon, right-click, or double-tap
  `Esc`. Keyboard navigation works too — arrow keys + `Enter`.
- **Live file tree** — auto-refreshes every 10 seconds so files added
  or removed from disk show up without you doing anything.
- **External change detection** — if a file on disk changes underneath
  an open clean buffer, the editor reloads it; if your buffer is dirty,
  you get a heads-up; if the file is deleted, the tab is flagged once.
- **Toggleable, draggable sidebar** — show/hide the file tree from the
  menu, or drag the splitter to resize it.
- **Clipboard over SSH** — OSC 52, including a `tmux` passthrough so
  copy works from inside a tmux session on a remote host.
- **Single binary, no CGO** — cross-compiled for macOS, Linux, and
  Windows on amd64 and arm64.

## Install

### macOS / Linux (Homebrew)

The Homebrew formula is published into this repo's `Formula/` directory.
Tap it by URL (no `homebrew-*` repo naming convention required), then
install:

```sh
brew tap cloudmanic/spice-edit https://github.com/cloudmanic/spice-edit
brew install cloudmanic/spice-edit/spice-edit
```

### Updating

When a new release ships, refresh the tap and upgrade:

```sh
brew update
brew upgrade cloudmanic/spice-edit/spice-edit
```

### Uninstalling

```sh
brew uninstall cloudmanic/spice-edit/spice-edit
brew untap cloudmanic/spice-edit
```

### Other platforms

Pre-built binaries for Linux, macOS, and Windows (amd64 + arm64) are
attached to every [GitHub Release](https://github.com/cloudmanic/spice-edit/releases).
Download the archive for your OS/arch, extract it, and drop the
`spiceedit` binary somewhere on your `$PATH`.

### From source

```sh
git clone https://github.com/cloudmanic/spice-edit.git
cd spice-edit
make install        # builds and installs to $GOPATH/bin
```

## Usage

```sh
spiceedit              # opens the current directory
spiceedit ~/code/app   # opens a specific project root
```

Then:

- Click a file in the tree to open it.
- Click a tab to switch, click the `×` to close it.
- Click `≡` (top-left), right-click anywhere, or double-tap `Esc`
  for the action menu (Save, Save & Close, Show/Hide Sidebar, Quit, …).
- Drag the splitter between the sidebar and editor to resize.
- Click and drag in the editor to select; drag past the top or bottom
  edge to auto-scroll the selection.

## Project layout

```
.
├── main.go                   # Entry point — parses optional rootDir arg
├── internal/
│   ├── app/                  # Event loop, layout, menu modal, splitter
│   ├── editor/               # Buffer, tab, cursor, syntax highlighting
│   ├── filetree/             # Lazy directory tree with identity-preserving refresh
│   ├── clipboard/            # OSC 52 clipboard with tmux passthrough
│   ├── theme/                # Tokyo Night-inspired palette
│   └── version/              # Single-line version constant
├── .github/workflows/        # Auto-release pipeline
├── .goreleaser.yml           # Cross-compile + brew formula config
├── Formula/                  # Homebrew formula (written by CI)
└── Makefile
```

## Development

```sh
make run          # build and run against the current directory
make build        # build to ./bin/spiceedit
make build-linux  # cross-compile a linux/amd64 binary
make tidy         # go mod tidy
make clean        # rm -rf bin
```

## Releases

Releases are fully automated. Every push to `main`:

1. Reads `internal/version/version.go`.
2. If that file was hand-edited in the pushed commit, the version is
   used as-is (this is how you bump major or minor: edit the constant
   manually). Otherwise the patch number is auto-bumped and committed
   back to `main` with `[skip ci]`.
3. Tags `v<x.y.z>` and pushes the tag.
4. [GoReleaser](https://goreleaser.com/) cross-compiles for
   linux/darwin/windows × amd64/arm64, attaches archives to a GitHub
   Release, and pushes an updated formula into `Formula/spice-edit.rb`
   on this same repo.

No PAT, no separate tap repo — the default workflow `GITHUB_TOKEN` is
enough since the formula lives in the source repo.

## License

MIT — see [LICENSE](LICENSE).

Copyright © 2026 Cloudmanic, LLC.
