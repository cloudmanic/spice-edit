// =============================================================================
// File: internal/clipboard/clipboard.go
// =============================================================================

// Package clipboard provides platform-specific access to the host system
// clipboard.
//
// Unix-like systems use OSC 52 so clipboard copy continues to work over
// terminal/SSH boundaries. Native Windows builds use the Win32 clipboard API
// so copied text is available directly to Windows applications such as
// Notepad, Windows Terminal, and Herdr.
package clipboard
