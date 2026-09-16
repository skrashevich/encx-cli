//go:build windows

package main

import (
	"syscall"
	"unsafe"
)

var (
	kernel32                  = syscall.NewLazyDLL("kernel32.dll")
	procGetConsoleProcessList = kernel32.NewProc("GetConsoleProcessList")
)

// launchedFromGUI reports whether encli was started outside an existing terminal
// — a double-click in Explorer, a shortcut, or any parent that is not a shell.
func launchedFromGUI() bool {
	return guiFromConsoleProcessCount(consoleProcessCount())
}

// guiFromConsoleProcessCount maps the number of processes attached to our console
// onto the GUI verdict. Exactly one means Windows allocated a console for this
// launch and nobody else is on it; started from cmd.exe or PowerShell the shell
// shares the console, which puts at least two processes on it.
//
// Zero deliberately does NOT count as a GUI launch: it means there is no console
// object at all, which is what a terminal that does not use one reports — MSYS2
// and Git Bash (mintty) run on pipes, and so does Wine. Those are terminals, so
// they keep the usage text.
func guiFromConsoleProcessCount(n int) bool {
	return n == 1
}

// consoleProcessCount returns how many processes share this process's console, or
// 0 when there is no console. Two slots are enough: GetConsoleProcessList returns
// the total count even when the buffer is too small to hold every identifier.
func consoleProcessCount() int {
	if err := procGetConsoleProcessList.Find(); err != nil {
		// LazyProc.Call panics on a missing export; a Windows without this call
		// simply keeps the usage text.
		return 0
	}
	var pids [2]uint32
	n, _, _ := procGetConsoleProcessList.Call(uintptr(unsafe.Pointer(&pids[0])), uintptr(len(pids)))
	return int(n)
}
