//go:build integration

// Requires a running podman machine, see internal/cache/s3_test.go.
package voices_test

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

func TestS3StoreContract(t *testing.T) {
	ctx := context.Background()
	c, err := tcminio.Run(ctx, "minio/minio:latest")
	testcontainers.CleanupContainer(t, c)
	require.NoError(t, err)
	endpoint, err := c.ConnectionString(ctx)
	require.NoError(t, err)

	const bucket = "tts-voices"
	mc, err := minio.New(endpoint, &minio.Options{
		Creds: credentials.NewStaticV4(c.Username, c.Password, ""), Secure: false,
	})
	require.NoError(t, err)
	require.NoError(t, mc.MakeBucket(ctx, bucket, minio.MakeBucketOptions{}))

	s, err := cache.NewS3(cache.Config{
		Endpoint: endpoint, AccessKey: c.Username, SecretKey: c.Password, Bucket: bucket,
	})
	require.NoError(t, err)
	runStoreContract(t, s)
}
