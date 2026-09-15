//go:build windows

package clipboard

import (
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

var procGetClipboardData = user32.NewProc("GetClipboardData")

func TestCopyToSystemWindows(t *testing.T) {
	const want = "Hello from SpiceEdit 🌶️"

	if err := CopyToSystem(want); err != nil {
		t.Fatalf("CopyToSystem() error = %v", err)
	}

	if err := openClipboard(); err != nil {
		t.Fatalf("openClipboard() error = %v", err)
	}
	defer procCloseClipboard.Call()

	handle, _, callErr := procGetClipboardData.Call(uintptr(cfUnicodeText))
	if handle == 0 {
		t.Fatalf("GetClipboardData() failed: %v", windowsError(callErr))
	}

	ptr, _, callErr := procGlobalLock.Call(handle)
	if ptr == 0 {
		t.Fatalf("GlobalLock() failed: %v", windowsError(callErr))
	}
	defer procGlobalUnlock.Call(handle)

	got := windows.UTF16PtrToString((*uint16)(unsafe.Pointer(ptr)))

	if got != want {
		t.Fatalf("clipboard = %q, want %q", got, want)
	}
}

func TestCopyToSystemWindowsEmptyString(t *testing.T) {
	if err := CopyToSystem(""); err != nil {
		t.Fatalf("CopyToSystem(empty) error = %v", err)
	}

	if err := openClipboard(); err != nil {
		t.Fatalf("openClipboard() error = %v", err)
	}
	defer procCloseClipboard.Call()

	handle, _, callErr := procGetClipboardData.Call(uintptr(cfUnicodeText))
	if handle == 0 {
		t.Fatalf("GetClipboardData() failed: %v", windowsError(callErr))
	}

	ptr, _, callErr := procGlobalLock.Call(handle)
	if ptr == 0 {
		t.Fatalf("GlobalLock() failed: %v", windowsError(callErr))
	}
	defer procGlobalUnlock.Call(handle)

	got := windows.UTF16PtrToString((*uint16)(unsafe.Pointer(ptr)))
	if got != "" {
		t.Fatalf("clipboard = %q, want empty string", got)
	}
}
