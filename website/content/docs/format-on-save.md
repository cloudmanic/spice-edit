---
title: "Format on Save"
metaTitle: "Format on Save — SpiceEdit's Per-Project Config"
metaDescription: "Drop .spiceedit/format.json in your repo to map extensions to formatter argv. SpiceEdit prompts before running anything and re-prompts when configs change."
summary: "Per-project format-on-save with a trust prompt."
weight: 70
---

SpiceEdit can run a formatter on every save — `gofmt`, `prettier`, `php-cs-fixer`, `pint`, `ruff`, anything you want — but the feature is **off by default** and only fires for projects that opt in. Cloning a stranger's repo never silently rewrites their files.

## The config file

Create `.spiceedit/format.json` in your project root:

```json
{
  "commands": {
    "go":  ["gofmt", "-w", "$FILE"],
    "php": ["php-cs-fixer", "fix", "$FILE", "--quiet"],
    "py":  ["ruff", "format", "$FILE"],
    "js":  ["prettier", "--write", "$FILE"],
    "ts":  ["prettier", "--write", "$FILE"],
    "rs":  ["rustfmt", "$FILE"]
  }
}
```

Schema:

- **Keys** are file extensions, **without** the leading dot.
- **Values** are argv arrays. They're handed to `exec` directly — no shell, no injection surface. If you genuinely need a shell, use `["sh", "-c", "..."]`.
- **`$FILE`** in any argument is replaced with the absolute path of the file being saved.

Real-world examples:

```json
{
  "commands": {
    "go":   ["gofmt", "-w", "$FILE"],
    "go":   ["goimports", "-w", "$FILE"],
    "php":  ["pint", "--quiet", "$FILE"],
    "py":   ["ruff", "format", "$FILE"],
    "rb":   ["rubocop", "-A", "$FILE"],
    "tf":   ["terraform", "fmt", "$FILE"],
    "json": ["prettier", "--write", "$FILE"],
    "yml":  ["prettier", "--write", "$FILE"]
  }
}
```

## The trust prompt

The first time SpiceEdit would run a formatter from a new or edited `.spiceedit/format.json`, you get a Yes / No modal:

> **Trust this project's formatter?**
> Allow `.spiceedit/format.json` to run formatters on save?

- **Yes** — SpiceEdit runs the configured formatters silently from then on.
- **No** — SpiceEdit never runs them in this project, until the config changes.

The remembered answer (along with a SHA-256 hash of the config it applies to) lives in `~/.config/spiceedit/format-trust.json`. The hash is the security trick: a teammate can't push a "v2" of the config that runs `rm -rf` and have your editor silently honor it. The hash mismatch re-prompts you.

## What happens on save

1. Save writes the file to disk first. A broken or slow formatter never blocks the save.
2. SpiceEdit looks up the file's extension in `format.json`. No match → done.
3. The configured command runs in a goroutine. The UI keeps responding; you can keep typing.
4. When the formatter finishes, SpiceEdit reloads the buffer — but only if you haven't typed anything since saving. If you did, your in-flight edits win and a status flash tells you the on-disk file was reformatted.
5. If the configured binary isn't installed, it's a silent no-op. You don't have to install everyone's formatter to clone a repo.

## Sharing vs. ignoring

Two reasonable patterns. Both work — SpiceEdit doesn't care.

- **Commit `.spiceedit/format.json`** so everyone on the team gets the same format-on-save behavior. Best for monorepos.
- **Add `.spiceedit/` to `.gitignore`** if developers prefer their own setups. Each person's local copy can configure whatever formatters they like.

## Personal defaults — the install prompt

You can list your favorite formatters once globally in `~/.config/spiceedit/format-defaults.json` (same shape as the project file):

```json
{
  "commands": {
    "go":  ["gofmt", "-w", "$FILE"],
    "php": ["pint", "--quiet", "$FILE"],
    "py":  ["ruff", "format", "$FILE"]
  }
}
```

These never run on their own. Instead, when you save a file in a project where:

1. The project's `.spiceedit/format.json` is missing or has no entry for that file's extension, **and**
2. Your global defaults *do* have an entry for that extension,

SpiceEdit asks once: **"Add `gofmt` for `.go` to `.spiceedit/format.json`?"**

- **Yes** — merges the entry into the project's config (creating `.spiceedit/format.json` if it didn't exist), auto-trusts the resulting file, and runs the formatter on the save you just made.
- **No / Esc** — remembered per-extension in the trust file. SpiceEdit won't ask again about that extension in this project until you edit the project config manually.

This keeps your personal preferences out of repos that don't want them, while making it one click to opt a project in.
