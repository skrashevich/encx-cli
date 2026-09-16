package main

import (
	"slices"
	"testing"
)

// webRun records what the stubbed web runner was asked to do.
type webRun struct {
	calls int
	args  []string
}

// stubGUILaunch replaces the detector and the web runner for one test.
func stubGUILaunch(t *testing.T, gui bool) *webRun {
	t.Helper()
	oldDetector, oldRunner := guiDetector, webRunner
	t.Cleanup(func() { guiDetector, webRunner = oldDetector, oldRunner })

	run := &webRun{}
	guiDetector = func() bool { return gui }
	webRunner = func(flagArgs []string) {
		run.calls++
		run.args = flagArgs
	}
	return run
}

// A double-click has no command and no terminal: the web UI takes over instead of
// flashing the usage text in a console window that closes immediately.
func TestAutoStartWebIfGUIStartsWebOnGUILaunch(t *testing.T) {
	run := stubGUILaunch(t, true)

	if !autoStartWebIfGUI(nil) {
		t.Fatal("autoStartWebIfGUI(nil) = false, want true on a GUI launch")
	}
	if run.calls != 1 {
		t.Errorf("web runner called %d times, want 1", run.calls)
	}
}

// Flags without a subcommand are still not a command, so they are forwarded to
// the web UI rather than dropped.
func TestAutoStartWebIfGUIForwardsFlags(t *testing.T) {
	run := stubGUILaunch(t, true)
	args := []string{"-debug", "-web-addr", "127.0.0.1:9999"}

	if !autoStartWebIfGUI(args) {
		t.Fatal("autoStartWebIfGUI = false, want true on a GUI launch")
	}
	if !slices.Equal(run.args, args) {
		t.Errorf("web runner got %v, want %v", run.args, args)
	}
}

// Run from a shell, the caller keeps the usage text and the exit code.
func TestAutoStartWebIfGUIDeclinesFromTerminal(t *testing.T) {
	run := stubGUILaunch(t, false)

	if autoStartWebIfGUI(nil) {
		t.Error("autoStartWebIfGUI(nil) = true, want false when a terminal is attached")
	}
	if run.calls != 0 {
		t.Errorf("web runner called %d times, want 0", run.calls)
	}
}

// An automation whose parent is a GUI process looks exactly like a double-click,
// so it needs a way to keep the old exit-immediately behaviour.
func TestAutoStartWebIfGUIHonoursTheKillSwitch(t *testing.T) {
	run := stubGUILaunch(t, true)
	t.Setenv("ENCLI_NO_AUTO_WEB", "1")

	if autoStartWebIfGUI(nil) {
		t.Error("autoStartWebIfGUI(nil) = true, want false when ENCLI_NO_AUTO_WEB is set")
	}
	if run.calls != 0 {
		t.Errorf("web runner called %d times, want 0", run.calls)
	}
}
