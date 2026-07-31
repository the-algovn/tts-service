package cache

import (
	"bytes"
	"context"
	"errors"
	"io"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

type Config struct {
	Endpoint, AccessKey, SecretKey, Bucket string
	UseSSL                                 bool
}

type S3 struct {
	c      *minio.Client
	bucket string
}

func NewS3(cfg Config) (*S3, error) {
	c, err := minio.New(cfg.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
		Secure: cfg.UseSSL,
	})
	if err != nil {
		return nil, err
	}
	return &S3{c: c, bucket: cfg.Bucket}, nil
}

// Get treats a missing object as a miss, not an error -- a cold cache is the
// normal state, not a failure. GetObject itself only fails on malformed
// bucket/object names; a missing key only surfaces once the object is read,
// as a minio.ErrorResponse with Code == NoSuchKey.
func (s *S3) Get(ctx context.Context, key string) ([]byte, bool, error) {
	obj, err := s.c.GetObject(ctx, s.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, false, err
	}
	defer obj.Close()
	data, err := io.ReadAll(obj)
	if err != nil {
		var resp minio.ErrorResponse
		if errors.As(err, &resp) && resp.Code == minio.NoSuchKey {
			return nil, false, nil
		}
		return nil, false, err
	}
	return data, true, nil
}

func (s *S3) Put(ctx context.Context, key string, data []byte) error {
	_, err := s.c.PutObject(ctx, s.bucket, key, bytes.NewReader(data), int64(len(data)),
		minio.PutObjectOptions{ContentType: "application/octet-stream"})
	return err
}
