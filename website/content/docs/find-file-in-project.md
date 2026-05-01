---
title: "Find File in Project"
metaTitle: "Fuzzy File Finder in SpiceEdit (Esc p)"
metaDescription: "Esc p opens SpiceEdit's fuzzy file finder. Indexes 50,000 files in ~150ms via git ls-files, with fzy-style ranking and gitignore-aware fallback walks."
summary: "Fuzzy-find any file in the project with Esc p."
weight: 60
---

`Esc p` (or **Find file in project** from the `≡` menu) opens a centered fuzzy file finder over every non-ignored file in the project.

```
┌ Find file                                                    esc ┐
│  app.go                                              50/12345    │
│  internal/app/app.go                                             │
│  internal/app/app_test.go                                        │
│  internal/finder/score.go                                        │
│  ...                                                             │
└──────────────────────────────────────────────────────────────────┘
```

## Fuzzy ranking

The matcher is fzy-style. Each query character must appear in the path in order, case-insensitive. Score is positive for any hit; higher is better. The bonuses, in order of weight:

- **Consecutive runs (+30)** — unbroken matches outrank scattered ones. Typing `app` finds `app.go` before `application_helper.go`.
- **Word boundaries (+20)** — matches starting after `/`, `_`, `-`, `.`, or space outrank matches mid-word. `tab` finds `cmd/tab.go` before `command-table.go`.
- **Basename (+15)** — matches inside the file's basename outrank matches in directory names. `tab` finds `tab.go` before `tabs/foo.go`.
- **First character (+10)** — query starting at position 0 of the path gets a small bump.
- **Gap penalty (-1 per gap)** — every unmatched character between matches costs a point.

The result is what your fingers expect: type the beginning of the basename, get the file you wanted. Type a few scattered letters, get a reasonable list anyway.

## Gitignore handling

Two strategies, picked automatically:

- **Git repos.** SpiceEdit shells out to `git ls-files --cached --others --exclude-standard -z` once. One fork, around 150 ms on a 50,000-file repo, and gitignore comes for free — Git already knows.
- **Non-git projects.** A `filepath.Walk` with the `go-gitignore` library reading any `.gitignore` it finds, plus a hardcoded ignore list (`.git`, `.hg`, `.svn`, `node_modules`, `vendor`, `__pycache__`, `.venv`, `dist`, `build`, `.next`, `.cache`, `.DS_Store`).

The index is capped at 200,000 entries. If you have a project larger than that, the matcher will operate on the first 200k files seen — practically never an issue.

## Indexing cadence

The index is built in a background goroutine at startup, so the modal opens with results in hand the first time you press `Esc p`. After that:

- The index refreshes every 10 seconds, on the same cadence as the file tree.
- The index also refreshes immediately after any create / rename / delete inside the editor — there is no perceptible lag between adding a file and finding it.
- Concurrent rebuild requests coalesce. A second `Rebuild` call while one is in flight is a no-op.

## Navigation

| Key            | Action                                  |
| -------------- | --------------------------------------- |
| Type           | Filter the result list.                 |
| `↑` / `↓`      | Move the highlighted row.               |
| `Enter`        | Open the highlighted file.              |
| `Esc`          | Dismiss the modal.                      |
| Mouse hover    | Highlight a row.                        |
| Mouse click    | Open a row.                             |

The result count shows in the top-right corner of the modal: `<visible>/<total>`.
