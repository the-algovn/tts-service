// Package voices is the registry of runtime-created self-hosted voices: one
// reference clip and one metadata record per voice, in an object store.
package voices

import (
	"bytes"
	"context"
	"errors"
	"io"
	"sort"
	"strings"
	"sync"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"github.com/the-algovn/tts-service/internal/cache"
)

// Store is the blob store the registry persists to. Get reports a missing key
// as ok=false, not an error; Delete of a missing key is not an error.
type Store interface {
	Get(ctx context.Context, key string) ([]byte, bool, error)
	Put(ctx context.Context, key string, data []byte) error
	Delete(ctx context.Context, key string) error
	List(ctx context.Context, prefix string) ([]string, error)
}

// Memory is an in-process Store for tests and keyless dev.
type Memory struct {
	mu sync.Mutex
	m  map[string][]byte
}

// NewMemory returns an empty in-process Store.
func NewMemory() *Memory { return &Memory{m: map[string][]byte{}} }

// Get returns a copy of the value at key; ok is false when absent.
func (s *Memory) Get(_ context.Context, key string) ([]byte, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.m[key]
	return append([]byte(nil), v...), ok, nil
}

// Put stores a copy of data at key.
func (s *Memory) Put(_ context.Context, key string, data []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[key] = append([]byte(nil), data...)
	return nil
}

// Delete removes key; a missing key is not an error.
func (s *Memory) Delete(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.m, key)
	return nil
}

// List returns the sorted keys that start with prefix.
func (s *Memory) List(_ context.Context, prefix string) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []string
	for k := range s.m {
		if strings.HasPrefix(k, prefix) {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out, nil
}

// S3 is a MinIO-backed Store.
type S3 struct {
	c      *minio.Client
	bucket string
}

// NewS3 connects to the bucket named in cfg. It does not create the bucket;
// the MinIO provisioning job does.
func NewS3(cfg cache.Config) (*S3, error) {
	c, err := minio.New(cfg.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
		Secure: cfg.UseSSL,
	})
	if err != nil {
		return nil, err
	}
	return &S3{c: c, bucket: cfg.Bucket}, nil
}

// Get returns the object at key; ok is false when it does not exist.
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

// Put writes data to key, replacing any existing object.
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
