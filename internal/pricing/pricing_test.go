package pricing_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/the-algovn/tts-service/internal/pricing"
)

func TestTierOf(t *testing.T) {
	cases := map[string]string{
		"vi-VN-Chirp3-HD-Aoede": "chirp3-hd",
		"vi-VN-Neural2-A":       "neural2",
		"vi-VN-Wavenet-B":       "wavenet",
		"vi-VN-Standard-A":      "standard",
		"anything-else":         "standard",
	}
	for name, want := range cases {
		require.Equal(t, want, pricing.TierOf(name), "voice %q", name)
	}
}

func TestVoxCPMIsFree(t *testing.T) {
	require.Equal(t, 0.0, pricing.CostUSD("voxcpm", "v_aaaaaaaaaaaa", 1000))
	require.Equal(t, int64(0), pricing.FreeTierChars("voxcpm", "v_aaaaaaaaaaaa"))
}

func TestCostUSDByTier(t *testing.T) {
	// One million characters makes the per-1M rate readable directly.
	require.InDelta(t, 30.0, pricing.CostUSD("google", "vi-VN-Chirp3-HD-Aoede", 1_000_000), 1e-9)
	require.InDelta(t, 16.0, pricing.CostUSD("google", "vi-VN-Neural2-A", 1_000_000), 1e-9)
	require.InDelta(t, 4.0, pricing.CostUSD("google", "vi-VN-Wavenet-B", 1_000_000), 1e-9)
	require.InDelta(t, 4.0, pricing.CostUSD("google", "vi-VN-Standard-A", 1_000_000), 1e-9)
}

// Self-hosted and fake synthesis cost nothing, whatever the voice is called.
func TestCostUSDIsZeroForFreeProviders(t *testing.T) {
	require.Zero(t, pricing.CostUSD("vieneu", "vi-VN-Wavenet-B", 1_000_000))
	require.Zero(t, pricing.CostUSD("fake", "vi-VN-Chirp3-HD-Aoede", 1_000_000))
}

// The allowance is advisory metadata; the service is stateless and never
// subtracts it from cost.
func TestFreeTierChars(t *testing.T) {
	require.EqualValues(t, 4_000_000, pricing.FreeTierChars("google", "vi-VN-Wavenet-B"))
	require.EqualValues(t, 4_000_000, pricing.FreeTierChars("google", "vi-VN-Standard-A"))
	require.EqualValues(t, 1_000_000, pricing.FreeTierChars("google", "vi-VN-Neural2-A"))
	require.EqualValues(t, 1_000_000, pricing.FreeTierChars("google", "vi-VN-Chirp3-HD-Aoede"))
	require.EqualValues(t, 0, pricing.FreeTierChars("vieneu", "whatever"))
}
