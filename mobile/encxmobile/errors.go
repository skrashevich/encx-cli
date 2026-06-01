package encxmobile

import "github.com/skrashevich/encx-cli/encx"

// IsAntiSpamError reports whether err is an anti-spam (NotHumanRequest) challenge.
func IsAntiSpamError(err error) bool {
	return encx.IsAntiSpam(err)
}

// AntiSpamURLFromError returns the verification page URL when err is anti-spam.
func AntiSpamURLFromError(err error) string {
	return encx.AntiSpamURLFromError(err)
}
