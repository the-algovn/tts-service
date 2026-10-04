// Package backend holds the speech providers. A backend turns text into encoded
// audio bytes and says what container it produced; caching and format
// conversion all live outside it.
package backend

import "context"

type Backend interface {
	Synthesize(ctx context.Context, text, voiceName string, rate float64) (data []byte, ext string, err error)
}
