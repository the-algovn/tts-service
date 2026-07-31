package cache_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/the-algovn/tts-service/internal/cache"
)

// Every input that changes the audio must change the key.
func TestKeyVariesWithEveryInput(t *testing.T) {
	base := cache.Key("google", "vi-VN-Wavenet-B", 1.0, "mp3", "xin chào")

	require.NotEqual(t, base, cache.Key("vieneu", "vi-VN-Wavenet-B", 1.0, "mp3", "xin chào"))
	require.NotEqual(t, base, cache.Key("google", "vi-VN-Wavenet-D", 1.0, "mp3", "xin chào"))
	require.NotEqual(t, base, cache.Key("google", "vi-VN-Wavenet-B", 1.2, "mp3", "xin chào"))
	require.NotEqual(t, base, cache.Key("google", "vi-VN-Wavenet-B", 1.0, "wav", "xin chào"))
	require.NotEqual(t, base, cache.Key("google", "vi-VN-Wavenet-B", 1.0, "mp3", "tạm biệt"))
}

func TestKeyIsStable(t *testing.T) {
	a := cache.Key("google", "vi-VN-Wavenet-B", 1.0, "mp3", "xin chào")
	b := cache.Key("google", "vi-VN-Wavenet-B", 1.0, "mp3", "xin chào")
	require.Equal(t, a, b)
}

// Vietnamese text can arrive precomposed or with combining diacritics. The two
// are the same words and must not synthesize -- or bill -- twice.
func TestKeyNormalizesUnicode(t *testing.T) {
	precomposed := "chào" // U+00E0
	decomposed := "chào" // a + combining grave
	require.NotEqual(t, precomposed, decomposed, "test inputs must differ byte-wise")

	require.Equal(t,
		cache.Key("google", "v", 1.0, "mp3", precomposed),
		cache.Key("google", "v", 1.0, "mp3", decomposed))
}

func TestMemoryStoreRoundTrips(t *testing.T) {
	ctx := context.Background()
	m := cache.NewMemory()

	_, ok, err := m.Get(ctx, "k")
	require.NoError(t, err)
	require.False(t, ok)

	require.NoError(t, m.Put(ctx, "k", []byte("audio")))

	got, ok, err := m.Get(ctx, "k")
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, []byte("audio"), got)
}
