---
title: "Configuration"
metaTitle: "SpiceEdit Configuration Files Reference"
metaDescription: "SpiceEdit reads at most three JSON files plus an optional per-project folder. The full reference, including XDG paths and what isn't configurable."
summary: "All three configuration files, and what isn't configurable."
weight: 90
---

SpiceEdit avoids a config file on purpose. The behaviors that *can* be configured live in three small files and one project-level folder. Everything else is opinionated.

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
