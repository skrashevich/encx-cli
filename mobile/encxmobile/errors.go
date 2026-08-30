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

// IsUndecodableAcceptedError reports whether err is an unreadable reply from a 2xx response, i.e.
// the request reached the engine and only the reply could not be parsed.
//
// A submitted answer in that state was already recorded, so resending it would duplicate it.
// Returns false for non-2xx bodies, where a proxy may have answered instead and the request may
// never have arrived — those must stay retryable.
func IsUndecodableAcceptedError(err error) bool {
	return encx.IsUndecodableAccepted(err)
}
