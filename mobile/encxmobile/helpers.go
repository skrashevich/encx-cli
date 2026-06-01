package encxmobile

import "github.com/skrashevich/encx-cli/encx"

// LoginErrorText returns a human-readable login error description.
func LoginErrorText(code int64) string {
	return encx.LoginErrorText(int(code))
}

// EventText returns a human-readable game event status description.
func EventText(code int64) string {
	return encx.EventText(int(code))
}
