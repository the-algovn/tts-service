package catalog_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/the-algovn/tts-service/internal/catalog"
)

func TestResolveNamespacedID(t *testing.T) {
	provider, name := catalog.Resolve("google:vi-VN-Wavenet-B")
	require.Equal(t, "google", provider)
	require.Equal(t, "vi-VN-Wavenet-B", name)
}

// Bare ids are already persisted in the radio station row and in RACE_VOICE_ID.
// They must keep working after deploy without a migration.
func TestResolveBareIDDefaultsToGoogle(t *testing.T) {
	provider, name := catalog.Resolve("vi-VN-Neural2-A")
	require.Equal(t, "google", provider)
	require.Equal(t, "vi-VN-Neural2-A", name)
}

func TestResolveVieNeu(t *testing.T) {
	provider, name := catalog.Resolve("vieneu:v3-turbo-female")
	require.Equal(t, "vieneu", provider)
	require.Equal(t, "v3-turbo-female", name)
}
