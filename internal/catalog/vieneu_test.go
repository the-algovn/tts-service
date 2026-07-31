package catalog_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/the-algovn/tts-service/internal/catalog"
)

func TestVieNeuVoicesHasFourteenWellFormedEntries(t *testing.T) {
	voices := catalog.VieNeuVoices()
	require.Len(t, voices, 14)

	for _, v := range voices {
		require.True(t, strings.HasPrefix(v.ID, "vieneu:"), "id %q missing vieneu: prefix", v.ID)
		require.NotEmpty(t, v.Label, "id %q has empty label", v.ID)
		require.Contains(t, []string{"MALE", "FEMALE"}, v.Gender, "id %q has bad gender %q", v.ID, v.Gender)
		require.Equal(t, int64(0), v.FreeTierChars, "id %q should have zero free tier chars", v.ID)
	}
}

// The id carries a space and Vietnamese diacritics -- the thing most likely
// to break in Resolve's naive strings.Cut split. Prove the round trip rather
// than assume it.
func TestResolveRoundTripsVieNeuIDWithDiacriticsAndSpace(t *testing.T) {
	provider, name := catalog.Resolve("vieneu:Phạm Tuyên")
	require.Equal(t, "vieneu", provider)
	require.Equal(t, "Phạm Tuyên", name)
}
