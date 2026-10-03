package voices_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/the-algovn/tts-service/internal/cache"
	"github.com/the-algovn/tts-service/internal/voices"
)

func TestMemoryStoreContract(t *testing.T) { runStoreContract(t, voices.NewMemory()) }

func runStoreContract(t *testing.T, s voices.Store) {
	ctx := context.Background()
	require.NoError(t, s.Put(ctx, "voices/a/x", []byte("1")))
	require.NoError(t, s.Put(ctx, "voices/b/x", []byte("2")))
	require.NoError(t, s.Put(ctx, "other/c", []byte("3")))

	keys, err := s.List(ctx, "voices/")
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"voices/a/x", "voices/b/x"}, keys)

	require.NoError(t, s.Delete(ctx, "voices/a/x"))
	_, ok, err := s.Get(ctx, "voices/a/x")
	require.NoError(t, err)
	require.False(t, ok)
	require.NoError(t, s.Delete(ctx, "voices/missing"))
	_, ok, err = s.Get(ctx, "voices/never")
	require.NoError(t, err)
	require.False(t, ok)
	got, ok, err := s.Get(ctx, "voices/b/x")
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, []byte("2"), got)
}

var _ voices.Store = (*cache.S3)(nil)
