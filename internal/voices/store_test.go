package voices_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/the-algovn/tts-service/internal/voices"
)

func TestMemoryStoreContract(t *testing.T) {
	ctx := context.Background()
	s := voices.NewMemory()
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
}
