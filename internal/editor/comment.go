// =============================================================================
// File: internal/editor/comment.go
// Author: Spicer Matthews <spicer@cloudmanic.com>
// Created: 2026-05-14
// Copyright: 2026 Cloudmanic, LLC. All rights reserved.
// =============================================================================

package editor

import (
	"path/filepath"
	"strings"
)

// lineCommentByExt maps common source file extensions to their single-line
// comment marker. Block-comment-only languages are intentionally omitted.
var lineCommentByExt = map[string]string{
	".adb":        "--",
	".ads":        "--",
	".bash":       "#",
	".c":          "//",
	".cc":         "//",
	".clj":        ";",
	".cljs":       ";",
	".cmake":      "#",
	".conf":       "#",
	".cpp":        "//",
	".cs":         "//",
	".csh":        "#",
	".cxx":        "//",
	".dart":       "//",
	".el":         ";",
	".elm":        "--",
	".erl":        "%",
	".ex":         "#",
	".exs":        "#",
	".env":        "#",
	".fish":       "#",
	".go":         "//",
	".h":          "//",
	".hpp":        "//",
	".hs":         "--",
	".ini":        ";",
	".java":       "//",
	".jl":         "#",
	".js":         "//",
	".jsx":        "//",
	".kt":         "//",
	".kts":        "//",
	".less":       "//",
	".lua":        "--",
	".mjs":        "//",
	".mk":         "#",
	".mm":         "//",
	".php":        "//",
	".pl":         "#",
	".pm":         "#",
	".ps1":        "#",
	".py":         "#",
	".r":          "#",
	".rb":         "#",
	".rs":         "//",
	".sass":       "//",
	".scala":      "//",
	".scss":       "//",
	".sh":         "#",
	".sql":        "--",
	".swift":      "//",
	".toml":       "#",
	".ts":         "//",
	".tsx":        "//",
	".vim":        "\"",
	".yaml":       "#",
	".yml":        "#",
	".zsh":        "#",
	".dockerfile": "#",
	".gitignore":  "#",
}

// LineCommentPrefix returns the single-line comment marker for path. The
// boolean is false for file types that do not have a safe line-comment syntax.
func LineCommentPrefix(path string) (string, bool) {
	base := strings.ToLower(filepath.Base(path))
	switch base {
	case "dockerfile", "containerfile", "makefile", "gnumakefile", "rakefile", "gemfile", "justfile":
		return "#", true
	case "cmakelists.txt":
		return "#", true
	}
	if prefix, ok := lineCommentByExt[base]; ok {
		return prefix, true
	}
	prefix, ok := lineCommentByExt[strings.ToLower(filepath.Ext(base))]
	return prefix, ok
}

// ToggleLineComment comments or uncomments the selected lines. It returns
// ok=false when the active file type has no known line-comment marker.
func (t *Tab) ToggleLineComment() (changed bool, ok bool) {
	if t == nil || t.IsImage() || t.Buffer == nil {
		return false, false
	}
	prefix, ok := LineCommentPrefix(t.Path)
	if !ok {
		return false, false
	}
	start, end := t.commentLineRange()
	if !hasNonBlankLine(t.Buffer.Lines, start, end) {
		return false, true
	}
	uncomment := t.linesAreCommented(start, end, prefix)

	t.pushUndo(undoGroupStructural)
	for i := start; i <= end; i++ {
		line := t.Buffer.Lines[i]
		if strings.TrimSpace(line) == "" {
			continue
		}
		if uncomment {
			t.Buffer.Lines[i] = uncommentLine(line, prefix)
			continue
		}
		t.Buffer.Lines[i] = commentLine(line, prefix)
	}
	t.Cursor = t.Buffer.Clamp(t.Cursor)
	t.Anchor = t.Buffer.Clamp(t.Anchor)
	t.Dirty = true
	t.StyleStale = true
	t.cursorMoved = true
	return true, true
}

// commentLineRange returns the inclusive line range touched by the current
// selection, or the cursor line when there is no selection.
func (t *Tab) commentLineRange() (int, int) {
	if !t.HasSelection() {
		line := t.Buffer.Clamp(t.Cursor).Line
		return line, line
	}
	start, end := PosOrdered(t.Anchor, t.Cursor)
	start = t.Buffer.Clamp(start)
	end = t.Buffer.Clamp(end)
	if end.Col == 0 && end.Line > start.Line {
		end.Line--
	}
	return start.Line, end.Line
}

// linesAreCommented reports whether every non-blank line in the range already
// starts with prefix, either at column zero or after indentation.
func (t *Tab) linesAreCommented(start, end int, prefix string) bool {
	for i := start; i <= end; i++ {
		line := t.Buffer.Lines[i]
		if strings.TrimSpace(line) == "" {
			continue
		}
		if !lineHasCommentPrefix(line, prefix) {
			return false
		}
	}
	return true
}

// hasNonBlankLine reports whether any line in the inclusive range has content.
func hasNonBlankLine(lines []string, start, end int) bool {
	for i := start; i <= end; i++ {
		if strings.TrimSpace(lines[i]) != "" {
			return true
		}
	}
	return false
}

// commentLine inserts prefix at column zero, leaving the line's existing
// indentation untouched after the marker.
func commentLine(line, prefix string) string {
	return prefix + " " + line
}

// uncommentLine removes prefix, plus one following space if present, from
// column zero or from after indentation for lines toggled by older builds.
func uncommentLine(line, prefix string) string {
	if strings.HasPrefix(line, prefix) {
		rest := strings.TrimPrefix(line, prefix)
		rest = strings.TrimPrefix(rest, " ")
		return rest
	}
	indent, rest := splitIndent(line)
	rest = strings.TrimPrefix(rest, prefix)
	rest = strings.TrimPrefix(rest, " ")
	return indent + rest
}

// lineHasCommentPrefix reports whether line starts with prefix at column zero
// or after indentation.
func lineHasCommentPrefix(line, prefix string) bool {
	if strings.HasPrefix(line, prefix) {
		return true
	}
	_, rest := splitIndent(line)
	return strings.HasPrefix(rest, prefix)
}

// splitIndent separates leading horizontal whitespace from the rest of a line.
func splitIndent(line string) (string, string) {
	i := 0
	for i < len(line) && (line[i] == ' ' || line[i] == '\t') {
		i++
	}
	return line[:i], line[i:]
}
