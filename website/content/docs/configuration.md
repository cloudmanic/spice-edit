---
title: "Configuration"
metaTitle: "SpiceEdit Configuration Files Reference"
metaDescription: "SpiceEdit reads a handful of small JSON files plus an optional per-project folder. The full reference, including XDG paths and what isn't configurable."
summary: "Editor config files, and what isn't configurable."
weight: 90
---

SpiceEdit avoids a config file on purpose. The behaviors that *can* be configured live in a handful of small JSON files and one project-level folder. Everything else is opinionated.

## `~/.config/spiceedit/config.json`

Top-level editor preferences. Optional — without it, every field uses its default. The schema is intentionally tiny and forward-compatible: unknown fields are ignored, so old binaries won't break on a future config.

```json
{
  "icons": "auto"
}
```

| Key     | Values                            | Default  | What it does |
| ------- | --------------------------------- | -------- | ------------ |
| `icons` | `"auto"` / `"on"` / `"off"`       | `"auto"` | Toggles the Nerd Font glyphs in the file tree. `auto` checks whether a Nerd Font is installed (via `fc-list` or by walking `~/Library/Fonts` / `~/.local/share/fonts`) and turns icons on iff one is found. Pick `on` if detection misses your install; pick `off` if the glyphs render as boxes in your terminal. |

The detector can only see whether the OS knows about the font — it can't tell whether your *terminal* is configured to render it. If icons turn on but show as "tofu" boxes, set `"icons": "off"` and either point your terminal at a Nerd Font or live without them.

### Installing a Nerd Font

The icons in the file tree come from [Nerd Fonts](https://www.nerdfonts.com/) — they're not part of stock system fonts. SpiceEdit needs **two** things in place:

1. A Nerd Font installed at the OS level (so SpiceEdit's detector sees it).
2. Your terminal emulator configured to render that font.

Without step 2, glyphs render as boxes ("tofu") even though detection says yes.

**macOS (Homebrew)** — pick any patched font and install it:

```sh
brew install --cask font-jetbrains-mono-nerd-font
# or: font-hack-nerd-font, font-fira-code-nerd-font, font-meslo-lg-nerd-font, etc.
```

Then point your terminal at it: in iTerm2, **Settings → Profiles → Text → Font** and pick the `Nerd Font` variant. In Terminal.app, **Settings → Profiles → Text → Font**. In Ghostty, set `font-family = "JetBrainsMono Nerd Font"` in `~/.config/ghostty/config`.

**Linux (Debian / Ubuntu)** — Nerd Fonts aren't in apt yet, so download a patched font and drop it in `~/.local/share/fonts`:

```sh
mkdir -p ~/.local/share/fonts
cd ~/.local/share/fonts
curl -fLo "JetBrainsMono.zip" \
  https://github.com/ryanoasis/nerd-fonts/releases/latest/download/JetBrainsMono.zip
unzip -o JetBrainsMono.zip
fc-cache -fv
```

Then set the font in your terminal — for GNOME Terminal: **Preferences → Profiles → Text → Custom font → JetBrainsMono Nerd Font**. For Alacritty / Kitty / Wezterm / Ghostty, edit the config file's `font` / `font-family` entry.

**Linux (Arch)** — patched fonts are in the official repos:

```sh
sudo pacman -S ttf-jetbrains-mono-nerd
# or any of: ttf-hack-nerd, ttf-firacode-nerd, ttf-meslo-nerd, etc.
```

**Verifying** — `fc-list | grep -i nerd` should print at least one line. If it does and SpiceEdit *still* shows boxes, the font is installed but your terminal isn't using it; fix the terminal's font setting.

## `~/.config/spiceedit/actions.json`

User-defined shell-out actions for the action menu. See [Custom actions](/docs/custom-actions/). Optional — without it, the menu shows only built-in actions.

```json
{
  "actions": [
    { "label": "Open on Laptop", "command": "scp \"$FILE\" laptop:~/Downloads/" }
  ]
}
```

## `~/.config/spiceedit/format-defaults.json`

Personal default formatters. Same schema as the project file. Never runs on its own — only used when SpiceEdit prompts you to "install" an entry into a project's `.spiceedit/format.json`. See [Format on save](/docs/format-on-save/).

```json
{
  "commands": {
    "go":  ["gofmt", "-w", "$FILE"],
    "py":  ["ruff", "format", "$FILE"]
  }
}
```

## `~/.config/spiceedit/format-trust.json`

Stores per-project answers to the format-on-save trust prompt and the install prompt. Managed by SpiceEdit — you don't edit this directly. Each entry records the project path, a SHA-256 hash of the project's `.spiceedit/format.json`, and the user's answer (or per-extension declines).

If a teammate pushes a new `.spiceedit/format.json`, the hash changes and SpiceEdit re-prompts on the next save. That's the security model in one sentence.

## `<project>/.spiceedit/format.json`

Per-project format-on-save config. Keys are file extensions (no leading dot); values are argv arrays. See [Format on save](/docs/format-on-save/).

```json
{
  "commands": {
    "go":  ["gofmt", "-w", "$FILE"],
    "ts":  ["prettier", "--write", "$FILE"]
  }
}
```

Commit it to share with your team, or add `.spiceedit/` to `.gitignore` to keep it personal. Both work.

## `~/.local/state/spiceedit/actions.log`

State, not config. Append-only log of every custom-action invocation. See [Custom actions](/docs/custom-actions/).

## XDG awareness

All paths above respect the XDG environment variables when set:

- `$XDG_CONFIG_HOME` — defaults to `~/.config`
- `$XDG_STATE_HOME` — defaults to `~/.local/state`

## What you can't configure

This is intentional. Don't ask for it.

- **Theme.** Tokyo Night-inspired palette, baked in. The whole editor is one colorway.
- **Keymap.** Esc-leader is the keymap. Adding a config file for it would defeat the entire point.
- **Plugins.** None. SpiceEdit is opinionated — that's the product.
- **Tab width / line endings.** Detected from the file's own contents on open.

If a behavior matters enough to configure, it should be obvious enough to be the default.
