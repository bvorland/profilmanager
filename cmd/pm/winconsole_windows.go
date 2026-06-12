//go:build windows

package main

import "syscall"

func init() {
	kernel32 := syscall.NewLazyDLL("kernel32.dll")

	setConsoleOutputCP := kernel32.NewProc("SetConsoleOutputCP")
	if r1, _, _ := setConsoleOutputCP.Call(65001); r1 == 0 {
		return
	}

	setConsoleCP := kernel32.NewProc("SetConsoleCP")
	if r1, _, _ := setConsoleCP.Call(65001); r1 == 0 {
		return
	}
}
