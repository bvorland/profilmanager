//go:build windows

package cli

import "golang.org/x/sys/windows"

// initConsole switches the Windows console to UTF-8 (code page 65001) so
// that box-drawing characters, emoji, and non-ASCII labels render correctly
// instead of CP850/CP437 mojibake like "ΓöÇΓöÇ" for "──".
//
// Errors are swallowed: if we can't set the code page (redirected stdout,
// unusual host, missing privileges), the worst case is the pre-fix mojibake
// — never a crash. Both input and output code pages are set so that
// arguments containing non-ASCII (e.g. profile labels with emoji) round-trip
// cleanly back to Go.
//
// x/sys/windows doesn't expose SetConsoleOutputCP / SetConsoleCP directly,
// so we resolve them from kernel32 at runtime.
var (
	modKernel32         = windows.NewLazySystemDLL("kernel32.dll")
	procSetConsoleOutCP = modKernel32.NewProc("SetConsoleOutputCP")
	procSetConsoleInCP  = modKernel32.NewProc("SetConsoleCP")
)

const cpUTF8 = 65001

func initConsole() {
	_, _, _ = procSetConsoleOutCP.Call(uintptr(cpUTF8))
	_, _, _ = procSetConsoleInCP.Call(uintptr(cpUTF8))
}
