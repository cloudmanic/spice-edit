---
title: "Syntax Highlighting"
metaTitle: "Syntax Highlighting in SpiceEdit (Chroma)"
metaDescription: "SpiceEdit ships syntax highlighting for dozens of languages via Chroma — pure Go, no CGO, no tree-sitter setup. Tokyo Night palette baked in."
summary: "Pure-Go syntax highlighting via Chroma."
weight: 110
---

SpiceEdit ships syntax highlighting for dozens of languages out of the box, powered by [Chroma](https://github.com/alecthomas/chroma) — a pure-Go syntax tokenizer with no CGO, no tree-sitter setup, and no external grammars to install.

## How it works

When you open a file, SpiceEdit picks a Chroma lexer using these rules, in order:

1. **By filename.** `main.go` → Go lexer. `index.tsx` → TypeScript-with-JSX. `Dockerfile` → Dockerfile.
2. **By content.** When the filename doesn't match a known lexer, Chroma sniffs the first kilobyte for shebangs, XML prologues, and other magic markers.
3. **Fallback.** Plain text — no highlighting, no errors.

Tokens map onto the editor's Tokyo Night-inspired palette. Keywords, strings, numbers, comments, operators, types, function names, variables, and tags each get their own color, picked to be readable on the editor's dark background.

## Supported languages (the highlight-relevant subset)

Chroma supports hundreds of lexers; here's the subset most users will hit day-to-day:

**Systems / compiled.** Go, Rust, C, C++, C#, Java, Kotlin, Swift, Objective-C, Zig, Crystal.

**Scripting.** Python, Ruby, Perl, Lua, Bash, Zsh, Fish, PowerShell, sh.

**Web.** JavaScript, TypeScript, JSX, TSX, HTML, CSS, SCSS, Sass, Less, Vue, Svelte, Astro.

**Backend / dynamic.** PHP, Elixir, Erlang, Clojure, Scala, Haskell, OCaml, F#, R.

**Markup / data.** Markdown, JSON, YAML, TOML, XML, INI, CSV.

**Infra / config.** Dockerfile, Terraform, HCL, Nginx config, Apache config, Caddyfile, Makefile, CMake.

**Database.** SQL (Postgres, MySQL, SQLite, MSSQL flavors).

**Other.** Protocol Buffers, GraphQL, Thrift, JSON Schema, Diff/Patch.

If your language isn't on this list, search [Chroma's lexer catalog](https://github.com/alecthomas/chroma) — odds are it's there.

## The theme

Tokyo Night, baked in. There is no theming config — that's deliberate. A code editor that looks the same on every machine is one less thing to argue about in a PR review. The palette is defined in `internal/theme/theme.go` if you want to fork.

## Why Chroma instead of tree-sitter

Tree-sitter is great for structural editing — an LSP, an IDE, a linter that needs an AST. SpiceEdit doesn't do any of that. It needs a stream of tokens with colors, and Chroma delivers that in pure Go with no native dependencies. Adding tree-sitter would mean CGO, grammar packaging, per-language setup, and platform-specific build steps. None of that fits a "single static binary" promise.
