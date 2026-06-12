//go:build !windows

package cli

// initConsole is a no-op on non-Windows platforms; their consoles are
// UTF-8 by default. See console_windows.go for the Windows implementation.
func initConsole() {}
