//go:build !windows

package main

import "testing"

// Outside Windows the auto-start must never trigger: `encli` with no command
// still prints usage and exits 1.
func TestLaunchedFromGUIIsFalseOffWindows(t *testing.T) {
	if launchedFromGUI() {
		t.Error("launchedFromGUI() = true, want false on non-Windows platforms")
	}
}
