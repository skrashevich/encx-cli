//go:build !windows

package main

// launchedFromGUI is a Windows-only concern: elsewhere encli is launched from a
// shell, so a run without a subcommand keeps printing usage.
func launchedFromGUI() bool { return false }
