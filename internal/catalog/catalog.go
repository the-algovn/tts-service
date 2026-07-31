// Package catalog knows which voices exist, which backend serves each, and how
// a caller's voice id maps onto them.
package catalog

import "strings"

// ProviderGoogle is the default for an unnamespaced id.
const ProviderGoogle = "google"

type Voice struct {
	ID            string
	Label         string
	Provider      string
	Tier          string
	Gender        string
	FreeTierChars int64
}

// Resolve splits "provider:name". An id with no provider prefix resolves to
// google, because ids persisted before this service existed are bare Google
// voice names.
func Resolve(voiceID string) (provider, name string) {
	p, n, ok := strings.Cut(voiceID, ":")
	if !ok {
		return ProviderGoogle, voiceID
	}
	return p, n
}
