---
title: "Running over SSH"
metaTitle: "SpiceEdit over SSH — tmux, Zellij, OSC 52"
metaDescription: "SpiceEdit was built for SSH workflows. tmux, Zellij, OSC 52 clipboard, install on a remote box, leave it running, reconnect tomorrow. Real workflow."
summary: "The workflow SpiceEdit was built for."
weight: 100
---

This is the workflow SpiceEdit was built for. Most terminal editors treat SSH as a degraded mode of the local case. SpiceEdit is the other way around — every design decision optimizes for editing on a remote box from inside tmux or Zellij.

## The setup

On your laptop:

```sh
ssh remote-box                # or whatever host alias you use
tmux attach || tmux           # attach to or start a tmux session
spiceedit ~/code/some-project
```

That's it. SpiceEdit is now running on the remote box, sharing the SSH channel with the rest of your terminal traffic.

## Install on the remote box

The one-liner installer works on every Linux you'll ever SSH into — Alpine, BusyBox, Debian, Ubuntu, RHEL, Arch:

```sh
curl -fsSL https://raw.githubusercontent.com/cloudmanic/spice-edit/main/install.sh | sh
```

Plain POSIX `sh`. No bash. No GLIBC version pin — the binary is fully static, CGO-off. Drops `spiceedit` into `~/.local/bin` (or `/usr/local/bin` if `~/.local/bin` isn't writable).

## Why tmux / Zellij is the killer combo

You SSH into a box. You attach to tmux. Half your panes have agents working. One pane has SpiceEdit. You disconnect, get on a plane, reconnect six hours later from a different machine, and everything is exactly where you left it. The agents are still there. SpiceEdit is still there. The cursor is in the line you were last editing.

That workflow is impossible with a desktop IDE. It's annoying with VS Code Remote SSH (which spawns a node process on the host). It's the default with SpiceEdit, because there's no state to lose — it's a TUI inside tmux.

## Clipboard over SSH (OSC 52)

Highlight text in SpiceEdit. The text ends up in your laptop's clipboard. Even though SpiceEdit is running three layers deep — your laptop, SSH, tmux, the editor.

The mechanism is OSC 52, a terminal escape sequence every modern emulator honors:

- iTerm2
- WezTerm
- Kitty
- Alacritty
- Ghostty
- gnome-terminal
- macOS Terminal (the default)
- Windows Terminal

SpiceEdit writes OSC 52 to `/dev/tty` directly (not stdout — that would race tcell's renderer). When `$TMUX` is set, the sequence is wrapped in tmux's passthrough escape so it reaches the outer terminal even with `set-clipboard off`.

If your laptop's clipboard isn't picking up copies, check your terminal's "Allow apps to read/write clipboard" setting — most terminals require an opt-in for OSC 52 writes. iTerm2 calls it "Applications in terminal may access clipboard." kitty has a `clipboard_control` directive. Check [troubleshooting](/docs/troubleshooting/) for specifics.

## Paste

Paste *into* SpiceEdit doesn't use OSC 52 — most terminals refuse to expose the system clipboard to a TUI for security reasons, and SpiceEdit deliberately doesn't try.

Instead, paste from your laptop's clipboard the way you'd paste anywhere else: Cmd+V (macOS), Ctrl+Shift+V (Linux terminals), or right-click → Paste. The terminal delivers the text as keypresses, and SpiceEdit handles them as normal input.

For paste *within* SpiceEdit — copy here, paste here — use the action menu's Copy / Paste entries. SpiceEdit keeps an internal clipboard alongside the system one.

## A practical workflow

1. SSH to a dev box every Monday morning.
2. `tmux attach` — your sessions are still there.
3. In one pane: `spiceedit ~/code/api`.
4. In other panes: agents, build watchers, log tails.
5. Click around files in SpiceEdit. Make small edits. `Esc s` to save.
6. Highlight an error message in the log pane. Cmd+V it into ChatGPT on your laptop, get a fix.
7. Highlight the fix in your terminal. Paste it into SpiceEdit. Save. The build watcher reloads.
8. Disconnect when you're done. Tomorrow, reconnect, attach, keep going.

That's the whole product.
