package cache

import (
	"bytes"
	"context"
	"errors"
	"io"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// Config locates an S3-compatible bucket: Endpoint is host[:port] without a
// scheme, UseSSL selects https, and the keys are static credentials.
type Config struct {
	Endpoint, AccessKey, SecretKey, Bucket string
	UseSSL                                 bool
}

// S3 is a Store backed by one bucket of an S3-compatible service.
type S3 struct {
	c      *minio.Client
	bucket string
}

// NewS3 returns an S3 store for cfg. It returns an error when the endpoint or
// credentials cannot form a client; it does not contact the server.
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

// Put stores data under key, replacing any existing object. It returns the
// error from the object store on failure.
func (s *S3) Put(ctx context.Context, key string, data []byte) error {
	_, err := s.c.PutObject(ctx, s.bucket, key, bytes.NewReader(data), int64(len(data)),
		minio.PutObjectOptions{ContentType: "application/octet-stream"})
	return err
}

// Delete removes key; a missing key is not an error.
func (s *S3) Delete(ctx context.Context, key string) error {
	return s.c.RemoveObject(ctx, s.bucket, key, minio.RemoveObjectOptions{})
}

// List returns every object key that starts with prefix.
func (s *S3) List(ctx context.Context, prefix string) ([]string, error) {
	var out []string
	for o := range s.c.ListObjects(ctx, s.bucket, minio.ListObjectsOptions{Prefix: prefix, Recursive: true}) {
		if o.Err != nil {
			return nil, o.Err
		}
		out = append(out, o.Key)
	}
	return out, nil
}
