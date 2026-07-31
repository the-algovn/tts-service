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

// Memory is the test double for S3, and S3 never aliases -- it decodes fresh
// bytes off the network on every Get. If Memory aliased caller slices, a test
// could pass against the double while the same code corrupted a shared
// buffer against real MinIO. So neither Put nor a returned Get result may
// share backing storage with the entry.
func TestMemoryStoreDoesNotAliasCallerSlices(t *testing.T) {
	ctx := context.Background()
	m := cache.NewMemory()

	original := []byte("audio")
	require.NoError(t, m.Put(ctx, "k", original))
	original[0] = 'X' // mutate the slice after Put

	got, ok, err := m.Get(ctx, "k")
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, []byte("audio"), got, "Put must copy, not alias the caller's slice")

	got[0] = 'Y' // mutate the slice returned by Get

	got2, ok, err := m.Get(ctx, "k")
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, []byte("audio"), got2, "Get must copy, not alias the stored slice")
}
