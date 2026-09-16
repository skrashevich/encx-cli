package main

// onboardingFileEnvVar relocates the state file; tests use it to stay out of the
// developer's real home directory.
const onboardingFileEnvVar = "ENCLI_ONBOARDING_FILE"

// onboardingState records that the first-run wizard has been through once. It
// exists so an operator who already configured encli is not walked through the
// three steps again on every -web start.
type onboardingState struct {
	Completed   bool   `json:"completed"`
	CompletedAt string `json:"completed_at,omitempty"`
	Skipped     bool   `json:"skipped,omitempty"`
}

// onboardingFile resolves the state path.
//
// The "onboarding" subdirectory is the part that matters; stateFilePath explains
// why. The consequence here is specific: a -web logout deleting this file would
// silently reopen the wizard on the next start.
func onboardingFile() string {
	return stateFilePath(onboardingFileEnvVar, "onboarding", "state.json")
}

// onboardingWhat is the operator-facing noun in this file's errors and in the
// warning about a relocated path.
const onboardingWhat = "onboarding state"

// loadOnboardingState reads the wizard's state. A missing file means the wizard
// has never finished, which is the normal state of a machine that is about to be
// walked through it.
func loadOnboardingState() (onboardingState, error) {
	return loadJSONState[onboardingState](onboardingFile(), onboardingWhat)
}

func saveOnboardingState(s onboardingState) error {
	return saveJSONState(onboardingFile(), onboardingFileEnvVar, onboardingWhat, s)
}

// resetOnboardingState makes the wizard required again.
func resetOnboardingState() error {
	return deleteJSONState(onboardingFile())
}
