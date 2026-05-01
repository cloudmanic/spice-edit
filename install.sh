#!/bin/sh
# =============================================================================
# File: install.sh
# Author: Spicer Matthews <spicer@cloudmanic.com>
# Created: 2026-04-30
# Copyright: 2026 Cloudmanic, LLC. All rights reserved.
# =============================================================================
#
# One-liner installer / upgrader for SpiceEdit. Detects the host OS and
# architecture, downloads the matching archive from the latest GitHub
# Release, extracts the static `spiceedit` binary, and drops it into
# ~/.local/bin (preferred) or /usr/local/bin. Re-running the script
# performs an upgrade — same flow, latest version.
#
# Usage:
#
#   curl -fsSL https://raw.githubusercontent.com/cloudmanic/spice-edit/main/install.sh | sh
#
# Override the install location:
#
#   curl -fsSL .../install.sh | INSTALL_DIR=/opt/bin sh
#
# Override the version (default is the latest GitHub Release):
#
#   curl -fsSL .../install.sh | VERSION=v0.0.13 sh
#
# Designed to run under POSIX `sh` so it works on Alpine / BusyBox /
# minimal SSH targets in addition to bash on a normal Linux box.

set -eu

REPO="cloudmanic/spice-edit"
BINARY="spiceedit"

# Pretty output when stderr is a terminal, plain otherwise so piping into
# logs doesn't end up with stray escape codes.
if [ -t 2 ]; then
	BOLD="$(printf '\033[1m')"
	DIM="$(printf '\033[2m')"
	GREEN="$(printf '\033[32m')"
	RED="$(printf '\033[31m')"
	YELLOW="$(printf '\033[33m')"
	RESET="$(printf '\033[0m')"
else
	BOLD=""
	DIM=""
	GREEN=""
	RED=""
	YELLOW=""
	RESET=""
fi

# info prints a step-prefixed line to stderr so it doesn't pollute any
# stdout the user might be capturing.
info() {
	printf '%s==>%s %s\n' "$GREEN" "$RESET" "$1" >&2
}

warn() {
	printf '%s==>%s %s\n' "$YELLOW" "$RESET" "$1" >&2
}

# fatal prints an error and exits non-zero. Used everywhere a precondition
# fails — the script should never silently fall through into a bad state.
fatal() {
	printf '%serror:%s %s\n' "$RED" "$RESET" "$1" >&2
	exit 1
}

# detect_os normalises uname -s into the lowercase token GoReleaser uses
# in archive names (linux, darwin). Anything else is an explicit error
# rather than a guess — we'd rather tell the user "not supported" than
# 404 on the download.
detect_os() {
	uname_s="$(uname -s 2>/dev/null || echo unknown)"
	case "$uname_s" in
		Linux) echo "linux" ;;
		Darwin) echo "darwin" ;;
		*) fatal "unsupported OS: $uname_s (only Linux and macOS are released)" ;;
	esac
}

# detect_arch maps uname -m onto GoReleaser's arch token (amd64 or arm64).
# x86_64 / amd64 are aliases; aarch64 / arm64 are aliases. Anything else
# (e.g. armv7, riscv64) is an explicit error — we don't ship those builds.
detect_arch() {
	uname_m="$(uname -m 2>/dev/null || echo unknown)"
	case "$uname_m" in
		x86_64|amd64) echo "amd64" ;;
		aarch64|arm64) echo "arm64" ;;
		*) fatal "unsupported architecture: $uname_m (only amd64 and arm64 are released)" ;;
	esac
}

# require_cmd ensures cmd is on PATH. We use it to assert tar plus one of
# curl / wget exists before doing anything else, so failures show up at
# the top of the run rather than after a half-finished download.
require_cmd() {
	command -v "$1" >/dev/null 2>&1 || fatal "$1 is required but not found on PATH"
}

# fetch downloads url to outfile using curl or wget (whichever is present).
# We try curl first because it's near-universal and gives clean redirect
# behaviour with -L; wget is the BusyBox / minimal-image fallback.
fetch() {
	url="$1"
	outfile="$2"
	if command -v curl >/dev/null 2>&1; then
		curl --fail --show-error --silent --location --output "$outfile" "$url"
	elif command -v wget >/dev/null 2>&1; then
		wget --quiet --output-document "$outfile" "$url"
	else
		fatal "need curl or wget to download release archives"
	fi
}

# resolve_version picks the version to install. If the user passed VERSION,
# trust it as-is (allowing pinned installs). Otherwise we follow GitHub's
# /releases/latest redirect — that's a single HTTP call with no API rate
# limit, no JSON parsing, and works behind corporate caches.
resolve_version() {
	if [ -n "${VERSION:-}" ]; then
		echo "$VERSION"
		return
	fi
	url="https://github.com/${REPO}/releases/latest"
	# -I to fetch headers only; grep the Location: line and take its tail
	# segment, which is the tag (e.g. /releases/tag/v0.0.13 -> v0.0.13).
	if command -v curl >/dev/null 2>&1; then
		header="$(curl --silent --location --head "$url" 2>/dev/null \
			| grep -i '^location:' | tail -n 1)"
	else
		# wget --max-redirect=0 prints the would-be redirect to stderr.
		header="$(wget --max-redirect=0 --server-response --output-document=/dev/null "$url" 2>&1 \
			| grep -i 'Location:' | tail -n 1)"
	fi
	tag="${header##*/}"
	tag="$(printf '%s' "$tag" | tr -d '\r\n')"
	if [ -z "$tag" ]; then
		fatal "could not resolve latest version from $url"
	fi
	echo "$tag"
}

# pick_install_dir chooses where to drop the binary, honoring INSTALL_DIR
# when set. Default preference: ~/.local/bin (no sudo, in PATH on most
# modern distros), falling back to /usr/local/bin (needs sudo, in PATH
# everywhere).
pick_install_dir() {
	if [ -n "${INSTALL_DIR:-}" ]; then
		echo "$INSTALL_DIR"
		return
	fi
	if [ -d "$HOME/.local/bin" ] || mkdir -p "$HOME/.local/bin" 2>/dev/null; then
		echo "$HOME/.local/bin"
		return
	fi
	echo "/usr/local/bin"
}

# install_binary moves the extracted binary into the chosen directory.
# When the directory isn't writable, we re-run the move under sudo if it's
# available — most "system" install dirs need root but we don't want to
# silently fail with a permission error.
install_binary() {
	src="$1"
	dest_dir="$2"
	dest="$dest_dir/$BINARY"

	if [ -w "$dest_dir" ] || ([ ! -e "$dest_dir" ] && mkdir -p "$dest_dir" 2>/dev/null); then
		mv "$src" "$dest"
		chmod +x "$dest"
		return
	fi
	if command -v sudo >/dev/null 2>&1; then
		warn "writing to $dest_dir requires sudo"
		sudo mkdir -p "$dest_dir"
		sudo mv "$src" "$dest"
		sudo chmod +x "$dest"
		return
	fi
	fatal "cannot write to $dest_dir and sudo is not available"
}

# warn_if_not_in_path nudges the user to fix their PATH if we just
# installed somewhere they can't run from. The most common case is a
# fresh ~/.local/bin on an older shell rc.
warn_if_not_in_path() {
	dir="$1"
	case ":$PATH:" in
		*":$dir:"*) return ;;
	esac
	warn "$dir is not on your \$PATH — add this to your shell rc:"
	printf '\n    %sexport PATH="%s:\$PATH"%s\n\n' "$BOLD" "$dir" "$RESET" >&2
}

main() {
	require_cmd tar

	os="$(detect_os)"
	arch="$(detect_arch)"
	version="$(resolve_version)"
	# Strip a leading 'v' so the version slot in the archive name matches
	# GoReleaser's {{ .Version }} (which is bare). Tags ARE prefixed v.
	bare_version="${version#v}"

	archive="${BINARY}_${bare_version}_${os}_${arch}.tar.gz"
	url="https://github.com/${REPO}/releases/download/${version}/${archive}"

	info "Installing ${BINARY} ${version} (${os}/${arch})"
	info "  source: ${url}"

	tmp="$(mktemp -d)"
	# Clean up on any exit, including failure paths — we don't want to
	# leave half-extracted archives in /tmp littering the filesystem.
	trap 'rm -rf "$tmp"' EXIT INT TERM

	fetch "$url" "$tmp/$archive" \
		|| fatal "download failed (was the release published with this archive name?)"

	tar -xzf "$tmp/$archive" -C "$tmp" \
		|| fatal "extraction failed (archive may be corrupt)"

	if [ ! -f "$tmp/$BINARY" ]; then
		fatal "archive did not contain a $BINARY binary"
	fi

	dest_dir="$(pick_install_dir)"
	info "Installing to ${dest_dir}/${BINARY}"
	install_binary "$tmp/$BINARY" "$dest_dir"

	info "Done. ${BOLD}${dest_dir}/${BINARY}${RESET}${DIM} (${version})${RESET}"
	warn_if_not_in_path "$dest_dir"
}

main "$@"
