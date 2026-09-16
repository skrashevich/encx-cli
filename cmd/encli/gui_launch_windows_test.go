//go:build windows

package main

import "testing"

// A double-clicked encli.exe owns its console alone; started from cmd.exe or
// PowerShell the shell is attached too, and that run must keep the CLI usage.
// A console-less run (mintty, Wine, a detached parent) is not a double-click
// either — GetConsoleProcessList reports 0 there while a human is watching.
func TestGUIFromConsoleProcessCount(t *testing.T) {
	tests := []struct {
		name  string
		count int
		want  bool
	}{
		{"no console at all", 0, false},
		{"only encli on the console", 1, true},
		{"shell shares the console", 2, false},
		{"pipeline of shells", 5, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := guiFromConsoleProcessCount(tt.count); got != tt.want {
				t.Errorf("guiFromConsoleProcessCount(%d) = %v, want %v", tt.count, got, tt.want)
			}
		})
	}
}

// A test binary is never a double-click: either it has no console (0) or the go
// test runner that spawned it is attached too (>= 2). Both verdicts are "not a
// GUI launch", so this also proves the syscall does not report a bare 1 for a
// process that was started from a parent.
func TestLaunchedFromGUIIsFalseUnderGoTest(t *testing.T) {
	n := consoleProcessCount()
	if n == 1 {
		t.Errorf("consoleProcessCount() = 1 under go test, want 0 (no console) or >= 2 (runner attached)")
	}
	if launchedFromGUI() {
		t.Errorf("launchedFromGUI() = true under go test with console process count %d, want false", n)
	}
}
