//go:build integration

// Requires a running podman machine:
//
//	export DOCKER_HOST="unix://$(podman machine inspect --format '{{.ConnectionInfo.PodmanSocket.Path}}')"
//	export TESTCONTAINERS_RYUK_DISABLED=true
package cache_test

import (
	"context"
	"testing"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	tcminio "github.com/testcontainers/testcontainers-go/modules/minio"

	"github.com/the-algovn/tts-service/internal/cache"
)

func TestS3GetMissAndRoundTrip(t *testing.T) {
	ctx := context.Background()

	c, err := tcminio.Run(ctx, "minio/minio:latest")
	testcontainers.CleanupContainer(t, c)
	require.NoError(t, err)

	endpoint, err := c.ConnectionString(ctx)
	require.NoError(t, err)

	const bucket = "tts-cache"
	mc, err := minio.New(endpoint, &minio.Options{
		Creds: credentials.NewStaticV4(c.Username, c.Password, ""), Secure: false,
	})
	require.NoError(t, err)
	require.NoError(t, mc.MakeBucket(ctx, bucket, minio.MakeBucketOptions{}))

	s, err := cache.NewS3(cache.Config{
		Endpoint: endpoint, AccessKey: c.Username, SecretKey: c.Password, Bucket: bucket, UseSSL: false,
	})
	require.NoError(t, err)

	// A cold cache is the normal state: a missing key is a miss, not an error.
	_, ok, err := s.Get(ctx, "tts/google/deadbeef.mp3")
	require.NoError(t, err)
	require.False(t, ok)

	require.NoError(t, s.Put(ctx, "tts/google/deadbeef.mp3", []byte("audio-bytes")))

	got, ok, err := s.Get(ctx, "tts/google/deadbeef.mp3")
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, []byte("audio-bytes"), got)
}
