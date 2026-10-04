//go:build integration

// Requires a running podman machine:
//
//	export DOCKER_HOST="unix://$(podman machine inspect --format '{{.ConnectionInfo.PodmanSocket.Path}}')"
//	export TESTCONTAINERS_RYUK_DISABLED=true
package cache_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	tcminio "github.com/testcontainers/testcontainers-go/modules/minio"

	"github.com/the-algovn/tts-service/internal/cache"
)

// The official minio images are no longer published. Chainguard's runs as a
// non-root user, so the data dir must be one that user can write.
const minioImage = "cgr.dev/chainguard/minio@sha256:4cf4831a2bbcf13ddca09c1cbcc9faff716dd3c4247e0babc32864b8ee8e0034"

func TestS3GetMissAndRoundTrip(t *testing.T) {
	ctx := context.Background()

	c, err := tcminio.Run(ctx, minioImage, testcontainers.WithCmd("server", "/tmp/data"))
	testcontainers.CleanupContainer(t, c)
	require.NoError(t, err)

	endpoint, err := c.ConnectionString(ctx)
	require.NoError(t, err)

	s, err := cache.NewS3(cache.Config{
		Endpoint: endpoint, AccessKey: c.Username, SecretKey: c.Password, Bucket: "tts-cache", UseSSL: false,
	})
	require.NoError(t, err)
	require.NoError(t, s.EnsureBucket(ctx))
	require.NoError(t, s.EnsureBucket(ctx))

	// A cold cache is the normal state: a missing key is a miss, not an error.
	_, ok, err := s.Get(ctx, "tts/voxcpm/deadbeef.mp3")
	require.NoError(t, err)
	require.False(t, ok)

	require.NoError(t, s.Put(ctx, "tts/voxcpm/deadbeef.mp3", []byte("audio-bytes")))

	got, ok, err := s.Get(ctx, "tts/voxcpm/deadbeef.mp3")
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, []byte("audio-bytes"), got)
}
