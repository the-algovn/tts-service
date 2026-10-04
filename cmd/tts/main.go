// tts: the shared Vietnamese text-to-speech service. Internal gRPC only --
// never registered at a gateway.
package main

import (
	"context"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/reflection"

	"github.com/the-algovn/gopkg/obs"
	ttsv1 "github.com/the-algovn/protos/gen/go/algovn/tts/v1"
	"github.com/the-algovn/tts-service/internal/backend"
	"github.com/the-algovn/tts-service/internal/cache"
	"github.com/the-algovn/tts-service/internal/config"
	"github.com/the-algovn/tts-service/internal/ttsserver"
	"github.com/the-algovn/tts-service/internal/voices"
)

// version is stamped at build time with -ldflags "-X main.version=<sha>".
var version = "dev"

const bucketWait = 30 * time.Second

// openBucket creates the bucket in the background when it is missing, so an
// unreachable MinIO delays nothing: the cache and the voice registry each
// degrade on their own, and a blocked startup would trip the liveness probe.
func openBucket(ctx context.Context, logger *slog.Logger, endpoint, bucket string) (*cache.S3, error) {
	s, err := cache.NewS3(cache.Config{
		Endpoint:  endpoint,
		AccessKey: config.Get("MINIO_ACCESS_KEY", ""),
		SecretKey: config.Get("MINIO_SECRET_KEY", ""),
		Bucket:    bucket,
		UseSSL:    config.Get("MINIO_USE_SSL", "false") == "true",
	})
	if err != nil {
		return nil, err
	}
	go func() {
		ectx, cancel := context.WithTimeout(ctx, bucketWait)
		defer cancel()
		if err := s.EnsureBucket(ectx); err != nil {
			logger.ErrorContext(ctx, "bucket unavailable", "bucket", bucket, "err", err)
		}
	}()
	return s, nil
}

func main() {
	obsCfg, err := obs.ConfigFromEnv("tts-service", version)
	if err != nil {
		slog.Error("observability config", "err", err)
		os.Exit(1)
	}
	shutdownObs, err := obs.Setup(context.Background(), obsCfg)
	if err != nil {
		slog.Error("observability setup", "err", err)
		os.Exit(1)
	}
	logger := slog.Default()
	defer func() { _ = shutdownObs(context.Background()) }()

	if err := config.Load(); err != nil {
		logger.Error("config load failed", "err", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	addr := config.Get("LISTEN_ADDR", ":9490")
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		logger.ErrorContext(ctx, "listen failed", "addr", addr, "err", err)
		os.Exit(1)
	}

	srv := grpc.NewServer(
		grpc.MaxRecvMsgSize(16<<20),
		grpc.StatsHandler(obs.ServerHandler()),
		grpc.ChainUnaryInterceptor(obs.UnaryServerInterceptor()),
	)
	backends := map[string]backend.Backend{"fake": backend.Fake{}}
	var store cache.Store
	if ep := config.Get("MINIO_ENDPOINT", ""); ep != "" {
		s3, err := openBucket(ctx, logger, ep, config.Get("MINIO_BUCKET", "tts-cache"))
		if err != nil {
			logger.ErrorContext(ctx, "minio setup failed", "err", err)
			os.Exit(1)
		}
		store = s3
	}

	var registry *voices.Registry
	var designer ttsserver.Designer
	if u := config.Get("VOXCPM_URL", ""); u != "" {
		ep := config.Get("MINIO_ENDPOINT", "")
		if ep == "" {
			logger.ErrorContext(ctx, "VOXCPM_URL needs MINIO_ENDPOINT for the voice registry")
			os.Exit(1)
		}
		vs, err := openBucket(ctx, logger, ep, config.Get("VOICES_BUCKET", "tts-voices"))
		if err != nil {
			logger.ErrorContext(ctx, "voice store setup failed", "err", err)
			os.Exit(1)
		}
		registry = voices.NewRegistry(vs, time.Now)
		parallel, err := strconv.Atoi(config.Get("VOXCPM_PARALLEL", "3"))
		if err != nil || parallel < 1 {
			logger.ErrorContext(ctx, "VOXCPM_PARALLEL must be a positive integer")
			os.Exit(1)
		}
		timeout, err := time.ParseDuration(config.Get("VOXCPM_CHUNK_TIMEOUT", "60s"))
		if err != nil || timeout <= 0 {
			logger.ErrorContext(ctx, "VOXCPM_CHUNK_TIMEOUT must be a positive duration", "err", err)
			os.Exit(1)
		}
		perChar, err := time.ParseDuration(config.Get("VOXCPM_CHAR_TIMEOUT", "2s"))
		if err != nil || perChar < 0 {
			logger.ErrorContext(ctx, "VOXCPM_CHAR_TIMEOUT must be a non-negative duration", "err", err)
			os.Exit(1)
		}
		vx := backend.NewVoxCPM(backend.VoxCPMConfig{BaseURL: u, Parallel: parallel,
			ChunkTimeout: timeout, CharTimeout: perChar}, registry)
		backends["voxcpm"] = vx
		designer = vx
	}

	ttsv1.RegisterTTSServiceServer(srv, ttsserver.New(ttsserver.Deps{
		Logger:   logger,
		Backends: backends,
		Cache:    store,
		Voices:   registry,
		Designer: designer,
	}))
	healthpb.RegisterHealthServer(srv, health.NewServer())
	reflection.Register(srv)

	go func() {
		<-ctx.Done()
		logger.InfoContext(ctx, "shutting down")
		srv.GracefulStop()
	}()

	logger.InfoContext(ctx, "tts-service listening", "addr", addr)
	if err := srv.Serve(lis); err != nil {
		logger.ErrorContext(ctx, "serve failed", "err", err)
		os.Exit(1)
	}
}
