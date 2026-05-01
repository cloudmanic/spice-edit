// =============================================================================
// File: internal/icons/icons.go
// Author: Spicer Matthews <spicer@cloudmanic.com>
// Created: 2026-04-30
// Copyright: 2026 Cloudmanic, LLC. All rights reserved.
// =============================================================================

// Package icons provides Nerd Font glyphs for the file tree along with
// a best-effort detector that decides whether the user's terminal is
// likely to render them. The contract is small on purpose: callers ask
// "should I show icons?" once at startup, and "what icon for this
// node?" per row.
//
// Two detection strategies, in order:
//
//  1. fc-list — if the system has fontconfig (most Linux installs and
//     macOS users with Homebrew fontconfig), grep its font listing for
//     anything that looks like a Nerd Font family name. This is fast
//     and authoritative when it's available.
//
//  2. Filesystem walk — fall back to scanning the standard font
//     install dirs (~/Library/Fonts, /Library/Fonts on macOS;
//     ~/.local/share/fonts, ~/.fonts, /usr/share/fonts on Linux) for
//     any *.ttf / *.otf whose filename contains "Nerd". Slower but
//     works on stock macOS where fc-list usually isn't installed.
//
// Neither strategy can tell whether the *terminal* is configured to
// render the font — only that the OS knows about it. That's why the
// editor pairs detection with a manual override in config.json: users
// who hit a false positive can flip icons:"off" and move on.
package icons

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/cloudmanic/spice-edit/internal/spiceconfig"
)

// Resolve maps a user's IconsMode preference to a concrete on/off
// decision, running detection iff the mode is "auto". Centralised so
// the App startup path stays readable: cfg + detect → bool.
func Resolve(mode spiceconfig.IconsMode) bool {
	switch mode {
	case spiceconfig.IconsOn:
		return true
	case spiceconfig.IconsOff:
		return false
	default:
		return Detect()
	}
}

// Detect reports whether a Nerd Font is installed on this system.
// Tries fc-list first (fast, accurate), falls back to a filesystem
// walk if fontconfig isn't present. Returns false on any error
// rather than propagating — the caller's only question is "icons
// or no icons", and the safe answer when we can't tell is "no".
func Detect() bool {
	if detectViaFcList() {
		return true
	}
	return detectViaFilesystem()
}

// detectViaFcList shells out to fc-list and looks for any family name
// containing "nerd font" or "nerdfont" (case-insensitive). The match
// is deliberately loose because Nerd Fonts ship under many family
// names ("Hack Nerd Font", "JetBrainsMono NF", "Mononoki Nerd Font
// Propo", etc.), and the only common substring is "Nerd".
//
// Returns false on any error — including fc-list not being installed —
// so the caller falls through to the filesystem walk.
func detectViaFcList() bool {
	if _, err := exec.LookPath("fc-list"); err != nil {
		return false
	}
	out, err := exec.Command("fc-list", ":", "family").Output()
	if err != nil {
		return false
	}
	low := strings.ToLower(string(out))
	return strings.Contains(low, "nerd font") || strings.Contains(low, "nerdfont")
}

// detectViaFilesystem walks the standard font install directories
// looking for any .ttf / .otf / .ttc whose filename contains "nerd"
// (case-insensitive). This is the fallback path for stock macOS,
// which doesn't ship fontconfig — Nerd Font installers drop their
// .ttf files straight into ~/Library/Fonts where this can find them.
//
// We stop at the first match: the question is binary, and walking
// the entire fonts tree on every editor start is overkill.
func detectViaFilesystem() bool {
	for _, dir := range fontDirs() {
		if dir == "" {
			continue
		}
		if found := walkForNerdFont(dir); found {
			return true
		}
	}
	return false
}

// fontDirs returns the OS-appropriate font search path. Order matters
// only for short-circuit speed (user-level dirs first, then system).
func fontDirs() []string {
	home, _ := os.UserHomeDir()
	switch runtime.GOOS {
	case "darwin":
		dirs := []string{}
		if home != "" {
			dirs = append(dirs, filepath.Join(home, "Library", "Fonts"))
		}
		dirs = append(dirs, "/Library/Fonts", "/System/Library/Fonts")
		return dirs
	case "linux":
		dirs := []string{}
		if home != "" {
			dirs = append(dirs,
				filepath.Join(home, ".local", "share", "fonts"),
				filepath.Join(home, ".fonts"),
			)
		}
		dirs = append(dirs,
			"/usr/local/share/fonts",
			"/usr/share/fonts",
		)
		return dirs
	default:
		// Windows etc — fall back to whatever fc-list said. Walking
		// %WINDIR%\Fonts isn't worth the platform-specific code path
		// when the user can flip icons:"on" by hand.
		return nil
	}
}

// walkForNerdFont returns true as soon as it finds a font file whose
// name contains "nerd". Errors during walk are treated as "didn't
// find anything in this subtree" — many font dirs have unreadable
// system entries we should skip rather than abort on.
func walkForNerdFont(root string) bool {
	if info, err := os.Stat(root); err != nil || !info.IsDir() {
		return false
	}
	found := false
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // skip unreadable entries, keep walking siblings
		}
		if d.IsDir() {
			return nil
		}
		name := strings.ToLower(d.Name())
		if !strings.Contains(name, "nerd") {
			return nil
		}
		switch filepath.Ext(name) {
		case ".ttf", ".otf", ".ttc":
			found = true
			return filepath.SkipAll
		}
		return nil
	})
	return found
}

// FolderClosed and FolderOpen are the two folder glyphs the file tree
// uses, paired with the existing chevron so the row reads as
// "▸  fileName" or "▾  folderName/" with proper indent. Exported so
// the renderer can use them directly without going through For().
const (
	FolderClosed = "" //  - generic closed folder (nf-fa-folder)
	FolderOpen   = "" //  - generic open folder (nf-fa-folder_open)
	FileDefault  = "" //  - generic file (nf-fa-file)
)

// extIcons maps lowercase file extensions (with leading dot) to their
// Nerd Font glyph. Coverage skews toward the languages and config
// formats this editor's users actually edit — there's no value in
// listing every Nerd Font file icon when the long tail falls back to
// FileDefault gracefully.
var extIcons = map[string]string{
	".go":         "", //  go
	".py":         "", //  python
	".js":         "", //  javascript
	".jsx":        "", //  react
	".ts":         "", //  typescript
	".tsx":        "", //  react
	".rs":         "", //  rust
	".c":          "", //  c
	".h":          "", //  c header
	".cpp":        "", //  c++
	".cc":         "", //  c++
	".hpp":        "", //  c++ header
	".java":       "", //  java
	".rb":         "", //  ruby
	".php":        "", //  php
	".html":       "", //  html5
	".htm":        "", //  html5
	".css":        "", //  css3
	".scss":       "", //  sass
	".sass":       "", //  sass
	".json":       "", //  json
	".yaml":       "", //  yaml
	".yml":        "", //  yaml
	".toml":       "", //  toml
	".md":         "", //  markdown
	".markdown":   "", //  markdown
	".sh":         "", //  shell
	".bash":       "", //  shell
	".zsh":        "", //  shell
	".fish":       "", //  shell
	".sql":        "", //  sql
	".png":        "", //  image
	".jpg":        "", //  image
	".jpeg":       "", //  image
	".gif":        "", //  image
	".svg":        "", //  image
	".webp":       "", //  image
	".txt":        "", //  text
	".log":        "", //  log
	".lock":       "", //  lock
	".gitignore":  "", //  git
	".gitconfig":  "", //  git
	".gitmodules": "", //  git
	".env":        "", //  gear
	".dockerfile": "", //  docker
	".mod":        "", //  go.mod / go.sum aliases live in nameIcons too
	".sum":        "",
	".vue":        "", //  vue
	".swift":      "", //  swift
	".kt":         "", //  kotlin
	".dart":       "", //  dart
	".lua":        "", //  lua
	".vim":        "", //  vim
}

// nameIcons handles full-filename matches that an extension lookup
// can't catch — Dockerfile, Makefile, etc. don't have extensions, and
// "go.mod" needs different treatment than a generic ".mod" file (we
// happen to map them to the same glyph anyway, but the principle holds
// for cases like "package.json" being a node package, not a json blob).
var nameIcons = map[string]string{
	"dockerfile":     "", //  docker
	"makefile":       "", //  makefile
	"gnumakefile":    "", //  makefile
	".gitignore":     "", //  git
	".gitattributes": "", //  git
	".gitmodules":    "", //  git
	".env":           "", //  gear
	"go.mod":         "", //  go
	"go.sum":         "", //  go
	"readme.md":      "", //  markdown (kept here so we can promote it later)
	"license":        "", //  license
	"license.md":     "", //  license
}

// For returns the Nerd Font glyph that best fits a file tree entry.
// The decision tree:
//
//  1. Folders use FolderOpen if expanded, FolderClosed otherwise.
//  2. Files match by full lowercase name first (so Makefile and
//     Dockerfile get their proper icons).
//  3. Then by extension.
//  4. Anything unmatched gets FileDefault.
//
// The signature takes the three node attributes the caller actually
// has rather than coupling this package to filetree.Node — keeps the
// dependency arrow pointing one way (filetree → icons, not the
// reverse) and makes the function trivially testable.
func For(name string, isDir, expanded bool) string {
	if isDir {
		if expanded {
			return FolderOpen
		}
		return FolderClosed
	}
	low := strings.ToLower(name)
	if g, ok := nameIcons[low]; ok {
		return g
	}
	if g, ok := extIcons[strings.ToLower(filepath.Ext(name))]; ok {
		return g
	}
	return FileDefault
}
